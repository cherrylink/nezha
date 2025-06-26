package rpc

import (
	"context"
	"log"
	"strings"

	petname "github.com/dustinkirkland/golang-petname"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"

	"github.com/nezhahq/nezha/model"
	"github.com/nezhahq/nezha/service/singleton"
)

type authHandler struct {
	ClientSecret string
	ClientUUID   string
}

func (a *authHandler) Check(ctx context.Context) (uint64, error) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return 0, status.Errorf(codes.Unauthenticated, "获取 metaData 失败")
	}

	var clientSecret string
	if value, ok := md["client_secret"]; ok {
		clientSecret = strings.TrimSpace(value[0])
	}

	if clientSecret == "" {
		return 0, status.Error(codes.Unauthenticated, "客户端认证失败")
	}

	ip, _ := ctx.Value(model.CtxKeyRealIP{}).(string)

	singleton.UserLock.RLock()
	userId, ok := singleton.AgentSecretToUserId[clientSecret]
	if !ok {
		singleton.UserLock.RUnlock()
		model.BlockIP(singleton.DB, ip, model.WAFBlockReasonTypeAgentAuthFail, model.BlockIDgRPC)
		return 0, status.Error(codes.Unauthenticated, "客户端认证失败")
	}
	singleton.UserLock.RUnlock()

	model.UnblockIP(singleton.DB, ip, model.BlockIDgRPC)

	var clientUUID string
	if value, ok := md["client_uuid"]; ok {
		clientUUID = strings.TrimSpace(value[0])
	}

	// 验证客户端标识符不为空且长度合理（1-64个字符）
	if clientUUID == "" || len(clientUUID) > 64 {
		return 0, status.Error(codes.Unauthenticated, "客户端标识符不合法，必须为1-64个字符")
	}

	clientID, hasID := singleton.ServerShared.UUIDToID(clientUUID)
	if !hasID {
		// 获取可选的服务器名称
		var serverName string
		if value, ok := md["server_name"]; ok {
			serverName = strings.TrimSpace(value[0])
		}

		// 如果没有指定名称，使用生成的名称
		if serverName == "" {
			serverName = petname.Generate(2, "-")
		}

		// 获取可选的服务器分组名称
		var serverGroupID uint64
		if value, ok := md["server_group_name"]; ok {
			groupName := strings.TrimSpace(value[0])
			if groupName != "" {
				// 通过分组名称查找分组
				var serverGroup model.ServerGroup
				if err := singleton.DB.Where("name = ? AND user_id = ?", groupName, userId).First(&serverGroup).Error; err != nil {
					if err == gorm.ErrRecordNotFound {
						// 如果普通用户找不到分组，尝试查找是否是管理员访问其他用户的分组
						singleton.UserLock.RLock()
						userInfo, exists := singleton.UserInfoMap[userId]
						singleton.UserLock.RUnlock()

						if exists && userInfo.Role == model.RoleAdmin {
							// 管理员可以使用任意分组，查找所有用户的分组
							if err := singleton.DB.Where("name = ?", groupName).First(&serverGroup).Error; err != nil {
								if err == gorm.ErrRecordNotFound {
									return 0, status.Error(codes.Unauthenticated, "指定的服务器分组不存在")
								}
								return 0, status.Error(codes.Unauthenticated, "查询服务器分组失败")
							}
						} else {
							return 0, status.Error(codes.Unauthenticated, "指定的服务器分组不存在或无权限访问")
						}
					} else {
						return 0, status.Error(codes.Unauthenticated, "查询服务器分组失败")
					}
				}

				serverGroupID = serverGroup.ID
			}
		}

		// 创建服务器记录
		s := model.Server{
			UUID: clientUUID,
			Name: serverName,
			Common: model.Common{
				UserID: userId,
			},
		}

		// 使用事务确保数据一致性
		err := singleton.DB.Transaction(func(tx *gorm.DB) error {
			// 创建服务器记录
			if err := tx.Create(&s).Error; err != nil {
				return err
			}

			// 如果指定了分组，创建分组关联记录
			if serverGroupID > 0 {
				serverGroupServer := model.ServerGroupServer{
					Common: model.Common{
						UserID: userId,
					},
					ServerGroupId: serverGroupID,
					ServerId:      s.ID,
				}
				if err := tx.Create(&serverGroupServer).Error; err != nil {
					return err
				}
			}

			return nil
		})

		if err != nil {
			return 0, status.Error(codes.Unauthenticated, err.Error())
		}

		// 记录服务器自动注册日志
		if serverGroupID > 0 {
			// 获取分组名称用于日志记录
			var groupName string
			if value, ok := md["server_group_name"]; ok {
				groupName = strings.TrimSpace(value[0])
			}
			log.Printf("NEZHA>> 自动注册服务器: UUID=%s, Name=%s, Group=%s (ID:%d), UserID=%d",
				clientUUID, serverName, groupName, serverGroupID, userId)
		} else {
			log.Printf("NEZHA>> 自动注册服务器: UUID=%s, Name=%s, UserID=%d",
				clientUUID, serverName, userId)
		}

		model.InitServer(&s)
		singleton.ServerShared.Update(&s, clientUUID)

		clientID = s.ID
	}

	return clientID, nil
}
