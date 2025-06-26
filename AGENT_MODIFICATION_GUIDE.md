# Nezha Agent 端修改指南

## 概述

为了支持服务器自动注册时指定自定义名称和分组，需要对Agent端进行以下修改：

1. 扩展配置文件格式
2. 修改gRPC连接逻辑，发送额外的metadata
3. 添加配置验证和错误处理
4. 保持向后兼容性

## 1. 配置文件修改

### 1.1 现有配置格式
```yaml
# 当前的agent配置文件
server: "your-dashboard.com:5555"
secret: "your-secret-key"
# 其他现有配置...
```

### 1.2 新的配置格式
```yaml
# 扩展后的agent配置文件
server: "your-dashboard.com:5555"
secret: "your-secret-key"

# 新增字段 - 服务器标识（替代随机UUID）
client_id: "my-web-server-01"        # 可选，如果不设置则自动生成

# 新增字段 - 服务器自定义信息
server_name: "Web服务器-生产环境"      # 可选，服务器显示名称
server_group: "生产环境服务器"         # 可选，服务器分组名称

# 其他现有配置保持不变...
```

### 1.3 配置字段说明

| 字段名 | 类型 | 必填 | 说明 | 示例 |
|--------|------|------|------|------|
| `client_id` | string | 否 | 服务器唯一标识符，1-64个字符，如果不设置则使用硬件信息生成 | `web-server-01` |
| `server_name` | string | 否 | 服务器显示名称，如果不设置则自动生成友好名称 | `Web服务器-生产环境` |
| `server_group` | string | 否 | 服务器分组名称，必须是已存在的分组 | `生产环境服务器` |

## 2. 代码结构修改

### 2.1 配置结构体扩展

**文件位置**：通常在 `config/config.go` 或类似文件中

```go
// 在现有的Config结构体中添加新字段
type Config struct {
    // 现有字段
    Server string `yaml:"server"`
    Secret string `yaml:"secret"`
    
    // 新增字段
    ClientID    string `yaml:"client_id"`     // 服务器唯一标识符
    ServerName  string `yaml:"server_name"`   // 服务器显示名称  
    ServerGroup string `yaml:"server_group"`  // 服务器分组名称
    
    // 其他现有字段...
}
```

### 2.2 配置验证逻辑

**文件位置**：配置加载模块

```go
// 添加配置验证函数
func (c *Config) Validate() error {
    // 现有验证逻辑...
    
    // 验证客户端ID格式
    if c.ClientID != "" {
        if len(c.ClientID) > 64 {
            return fmt.Errorf("client_id 长度不能超过64个字符")
        }
        // 可选：添加字符格式验证
        if !isValidClientID(c.ClientID) {
            return fmt.Errorf("client_id 包含无效字符，只允许字母、数字、连字符和下划线")
        }
    }
    
    return nil
}

// 客户端ID格式验证（可选实现）
func isValidClientID(id string) bool {
    // 正则表达式：只允许字母、数字、连字符、下划线
    matched, _ := regexp.MatchString(`^[a-zA-Z0-9_-]+$`, id)
    return matched
}
```

### 2.3 客户端ID生成逻辑

**文件位置**：配置初始化模块

```go
// 如果没有设置client_id，生成一个基于硬件信息的标识符
func (c *Config) generateClientID() {
    if c.ClientID != "" {
        return // 已经设置了，不需要生成
    }
    
    // 方案1：基于MAC地址生成
    if id := generateIDFromMAC(); id != "" {
        c.ClientID = id
        return
    }
    
    // 方案2：基于主机名生成
    if hostname, err := os.Hostname(); err == nil && hostname != "" {
        c.ClientID = sanitizeHostname(hostname)
        return
    }
    
    // 方案3：生成随机ID（保持向后兼容）
    c.ClientID = generateRandomID()
}

// 基于MAC地址生成ID
func generateIDFromMAC() string {
    interfaces, err := net.Interfaces()
    if err != nil {
        return ""
    }
    
    for _, iface := range interfaces {
        if iface.Flags&net.FlagUp != 0 && iface.HardwareAddr != nil {
            mac := iface.HardwareAddr.String()
            if mac != "" {
                // 将MAC地址转换为友好格式：aa:bb:cc:dd:ee:ff -> aabbccddee
                return strings.ReplaceAll(mac, ":", "")[:10] + "-host"
            }
        }
    }
    return ""
}

// 清理主机名，确保符合ID格式
func sanitizeHostname(hostname string) string {
    // 移除不允许的字符，保留字母数字和连字符
    reg := regexp.MustCompile(`[^a-zA-Z0-9-]`)
    cleaned := reg.ReplaceAllString(hostname, "-")
    
    // 确保长度不超过限制
    if len(cleaned) > 60 {
        cleaned = cleaned[:60]
    }
    
    return cleaned
}

// 生成随机ID（向后兼容）
func generateRandomID() string {
    // 生成8位随机字符串
    const charset = "abcdefghijklmnopqrstuvwxyz0123456789"
    b := make([]byte, 8)
    for i := range b {
        b[i] = charset[rand.Intn(len(charset))]
    }
    return string(b) + "-agent"
}
```

## 3. gRPC连接逻辑修改

### 3.1 metadata构建

**文件位置**：gRPC客户端连接模块

```go
import (
    "google.golang.org/grpc/metadata"
    // 其他导入...
)

// 修改现有的连接建立逻辑
func (client *Client) establishConnection() error {
    // 构建基础metadata
    md := metadata.New(map[string]string{
        "client_secret": client.config.Secret,
        "client_uuid":   client.config.ClientID,
    })
    
    // 添加可选的服务器名称
    if client.config.ServerName != "" {
        md.Set("server_name", client.config.ServerName)
    }
    
    // 添加可选的服务器分组
    if client.config.ServerGroup != "" {
        md.Set("server_group_name", client.config.ServerGroup)
    }
    
    // 创建带有metadata的context
    ctx := metadata.NewOutgoingContext(context.Background(), md)
    
    // 使用新的context建立连接
    return client.connectWithContext(ctx)
}
```

### 3.2 连接错误处理

**文件位置**：错误处理模块

```go
// 增强错误处理，识别服务器注册相关错误
func handleConnectionError(err error) {
    if err == nil {
        return
    }
    
    // 解析gRPC状态错误
    if st, ok := status.FromError(err); ok {
        switch st.Message() {
        case "客户端标识符不合法，必须为1-64个字符":
            log.Printf("错误：客户端ID格式不正确，请检查配置文件中的 client_id 字段")
            
        case "指定的服务器分组不存在":
            log.Printf("错误：指定的服务器分组不存在，请检查配置文件中的 server_group 字段")
            
        case "指定的服务器分组不存在或无权限访问":
            log.Printf("错误：无权限访问指定的服务器分组，请联系管理员")
            
        case "查询服务器分组失败":
            log.Printf("错误：服务器分组查询失败，请检查网络连接")
            
        default:
            log.Printf("连接错误：%v", st.Message())
        }
    } else {
        log.Printf("连接失败：%v", err)
    }
}
```

## 4. 配置文件示例

### 4.1 最小配置
```yaml
# 最小配置 - 仅必需字段
server: "dashboard.example.com:5555"
secret: "your-secret-key"
```

### 4.2 完整配置
```yaml
# 完整配置 - 包含所有新字段
server: "dashboard.example.com:5555"
secret: "your-secret-key"

# 服务器标识 - 推荐设置
client_id: "web-server-prod-01"

# 服务器信息 - 可选设置
server_name: "生产环境Web服务器"
server_group: "生产环境"

# 其他现有配置保持不变
# ...
```

### 4.3 批量部署配置模板
```yaml
# 模板：web服务器
server: "dashboard.example.com:5555"
secret: "your-secret-key"
client_id: "web-${HOSTNAME}"
server_name: "Web服务器-${ENVIRONMENT}"
server_group: "${ENVIRONMENT}环境"

# 模板：数据库服务器  
server: "dashboard.example.com:5555"
secret: "your-secret-key"
client_id: "db-${HOSTNAME}"
server_name: "数据库服务器-${ENVIRONMENT}"
server_group: "数据库集群"
```

## 5. 向后兼容性

### 5.1 兼容性保证
- **现有配置文件**：无需修改即可继续工作
- **现有行为**：不设置新字段时，行为与之前完全一致
- **自动生成**：client_id会自动生成，确保唯一性

### 5.2 迁移建议
```go
// 配置迁移检查
func (c *Config) checkMigration() {
    if c.ClientID == "" {
        log.Printf("提示：建议在配置文件中设置 client_id 以便于服务器识别")
        log.Printf("提示：可以设置 server_name 来自定义服务器显示名称")
        log.Printf("提示：可以设置 server_group 来自动加入服务器分组")
    }
}
```

## 6. 测试建议

### 6.1 单元测试
```go
func TestConfigValidation(t *testing.T) {
    tests := []struct {
        name   string
        config Config
        hasErr bool
    }{
        {
            name: "有效的客户端ID",
            config: Config{
                ClientID: "valid-client-id",
            },
            hasErr: false,
        },
        {
            name: "过长的客户端ID",
            config: Config{
                ClientID: strings.Repeat("a", 65),
            },
            hasErr: true,
        },
        {
            name: "空配置应该通过",
            config: Config{},
            hasErr: false,
        },
    }
    
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            err := tt.config.Validate()
            if (err != nil) != tt.hasErr {
                t.Errorf("Validate() error = %v, hasErr %v", err, tt.hasErr)
            }
        })
    }
}
```

### 6.2 集成测试场景
1. **默认行为测试**：不设置新字段，确保与之前行为一致
2. **自定义ID测试**：设置client_id，验证服务器正确识别
3. **分组测试**：设置server_group，验证自动加入分组
4. **错误处理测试**：设置无效分组，验证错误信息正确返回
5. **权限测试**：测试普通用户和管理员的分组访问权限

## 7. 部署注意事项

### 7.1 配置管理
- **环境变量支持**：考虑支持环境变量覆盖配置
- **配置模板**：提供不同环境的配置模板
- **批量部署**：支持配置文件模板化批量生成

### 7.2 监控和日志
```go
// 添加连接成功日志
func logSuccessfulRegistration(config *Config) {
    log.Printf("服务器注册成功")
    log.Printf("  - 客户端ID: %s", config.ClientID)
    if config.ServerName != "" {
        log.Printf("  - 服务器名称: %s", config.ServerName)
    }
    if config.ServerGroup != "" {
        log.Printf("  - 服务器分组: %s", config.ServerGroup)
    }
}
```

## 8. 实施步骤建议

1. **第一阶段**：实现配置结构体扩展和验证逻辑
2. **第二阶段**：实现gRPC metadata发送逻辑
3. **第三阶段**：添加错误处理和日志
4. **第四阶段**：测试和文档更新
5. **第五阶段**：发布和用户迁移指导

## 9. 完整的代码示例

### 9.1 配置文件加载
```go
package config

import (
    "fmt"
    "os"
    "regexp"
    "strings"
    "gopkg.in/yaml.v2"
)

type Config struct {
    Server      string `yaml:"server"`
    Secret      string `yaml:"secret"`
    ClientID    string `yaml:"client_id"`
    ServerName  string `yaml:"server_name"`
    ServerGroup string `yaml:"server_group"`
}

func LoadConfig(path string) (*Config, error) {
    data, err := os.ReadFile(path)
    if err != nil {
        return nil, err
    }
    
    var config Config
    if err := yaml.Unmarshal(data, &config); err != nil {
        return nil, err
    }
    
    // 生成客户端ID（如果未设置）
    config.generateClientID()
    
    // 验证配置
    if err := config.Validate(); err != nil {
        return nil, err
    }
    
    return &config, nil
}
```

### 9.2 gRPC客户端修改
```go
package client

import (
    "context"
    "google.golang.org/grpc"
    "google.golang.org/grpc/metadata"
)

type Client struct {
    config *config.Config
    conn   *grpc.ClientConn
}

func (c *Client) Connect() error {
    // 构建metadata
    md := metadata.New(map[string]string{
        "client_secret": c.config.Secret,
        "client_uuid":   c.config.ClientID,
    })
    
    if c.config.ServerName != "" {
        md.Set("server_name", c.config.ServerName)
    }
    
    if c.config.ServerGroup != "" {
        md.Set("server_group_name", c.config.ServerGroup)
    }
    
    // 创建context
    ctx := metadata.NewOutgoingContext(context.Background(), md)
    
    // 建立连接
    conn, err := grpc.DialContext(ctx, c.config.Server, grpc.WithInsecure())
    if err != nil {
        return handleConnectionError(err)
    }
    
    c.conn = conn
    return nil
}
```

## 10. 常见问题解答

### Q1: 如果不设置新字段会怎样？
A: Agent会继续正常工作，行为与之前完全一致。client_id会自动生成，server_name会使用默认生成的友好名称。

### Q2: client_id 可以重复吗？
A: 不可以。client_id必须唯一，如果重复会导致服务器记录冲突。

### Q3: 分组不存在时会发生什么？
A: Agent会连接失败并显示相应错误信息，需要先在Dashboard中创建对应分组。

### Q4: 普通用户可以使用管理员创建的分组吗？
A: 不可以。普通用户只能使用自己创建的分组，除非是管理员用户。

### Q5: 可以动态修改这些配置吗？
A: 目前需要重启Agent才能生效。未来版本可能会支持动态配置重载。

---

*本文档适用于 Nezha Monitoring Agent 的扩展开发，确保在实施过程中保持向后兼容性和良好的用户体验。* 