# Nezha WebShell API 文档

## 概述

Nezha监控系统提供了基于WebSocket的WebShell功能，允许用户通过浏览器远程访问服务器终端。该功能使用gRPC和WebSocket技术实现前后端通信。

## API端点

### 1. 创建终端会话

**端点**: `POST /api/v1/terminal`  
**认证**: 需要Bearer Token  
**描述**: 创建一个新的终端会话

#### 请求参数

```json
{
  "protocol": "string (可选)",
  "server_id": "uint64 (必填)"
}
```

| 参数名 | 类型 | 必填 | 描述 |
|--------|------|------|------|
| `protocol` | string | 否 | 协议类型，暂未使用 |
| `server_id` | uint64 | 是 | 目标服务器的ID |

#### 响应格式

**成功响应 (200)**:
```json
{
  "success": true,
  "data": {
    "session_id": "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx",
    "server_id": 1,
    "server_name": "Web服务器-01"
  }
}
```

**错误响应**:
```json
{
  "success": false,
  "error": "错误信息"
}
```

#### 可能的错误

| 错误信息 | 说明 |
|----------|------|
| `server not found or not connected` | 服务器未找到或未连接 |
| `permission denied` | 权限不足 |
| `unauthorized` | 未授权访问 |

#### 示例代码

**JavaScript/TypeScript**:
```javascript
// 创建终端会话
async function createTerminal(serverId) {
  const response = await fetch('/api/v1/terminal', {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      'Authorization': `Bearer ${localStorage.getItem('token')}`
    },
    body: JSON.stringify({
      server_id: serverId
    })
  });
  
  const result = await response.json();
  
  if (result.success) {
    return result.data;
  } else {
    throw new Error(result.error);
  }
}
```

**cURL**:
```bash
curl -X POST /api/v1/terminal \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer YOUR_TOKEN" \
  -d '{
    "server_id": 1
  }'
```

### 2. WebSocket终端流

**端点**: `GET /api/v1/ws/terminal/{session_id}`  
**协议**: WebSocket  
**认证**: 需要Bearer Token (通过query参数或cookie)  
**描述**: 建立与指定终端会话的WebSocket连接

#### URL参数

| 参数名 | 类型 | 必填 | 描述 |
|--------|------|------|------|
| `session_id` | string | 是 | 由创建终端API返回的会话ID |

#### WebSocket消息格式

**数据流类型**:
- **二进制消息**: 终端输出数据
- **文本消息**: 用户输入的命令
- **Ping消息**: 保活心跳 (每10秒自动发送)

**消息处理逻辑**:
- 发送文本消息到服务器 = 执行命令
- 接收二进制消息 = 终端输出显示
- 自动处理Ping/Pong保活

#### 连接生命周期

1. **建立连接**: WebSocket握手
2. **认证验证**: 检查session_id有效性
3. **流启动**: 建立与Agent的双向数据流
4. **数据传输**: 实时双向通信
5. **连接关闭**: 清理资源

#### 示例代码

**JavaScript WebSocket客户端**:
```javascript
class NezhaTerm {
  constructor(sessionId, token) {
    this.sessionId = sessionId;
    this.token = token;
    this.ws = null;
  }
  
  connect() {
    // 构建WebSocket URL
    const protocol = location.protocol === 'https:' ? 'wss:' : 'ws:';
    const url = `${protocol}//${location.host}/api/v1/ws/terminal/${this.sessionId}?token=${this.token}`;
    
    this.ws = new WebSocket(url);
    
    // 连接建立
    this.ws.onopen = () => {
      console.log('Terminal connected');
    };
    
    // 接收终端输出
    this.ws.onmessage = (event) => {
      if (event.data instanceof Blob) {
        // 二进制数据 - 终端输出
        const reader = new FileReader();
        reader.onload = () => {
          const output = new Uint8Array(reader.result);
          this.displayOutput(output);
        };
        reader.readAsArrayBuffer(event.data);
      }
    };
    
    // 连接关闭
    this.ws.onclose = () => {
      console.log('Terminal disconnected');
    };
    
    // 连接错误
    this.ws.onerror = (error) => {
      console.error('Terminal error:', error);
    };
  }
  
  // 发送用户输入
  sendInput(data) {
    if (this.ws && this.ws.readyState === WebSocket.OPEN) {
      this.ws.send(data);
    }
  }
  
  // 显示终端输出
  displayOutput(data) {
    // 将二进制数据转换为文本并显示
    const text = new TextDecoder().decode(data);
    document.getElementById('terminal').textContent += text;
  }
  
  // 关闭连接
  close() {
    if (this.ws) {
      this.ws.close();
    }
  }
}

// 使用示例
async function startTerminal(serverId) {
  try {
    // 1. 创建终端会话
    const session = await createTerminal(serverId);
    
    // 2. 建立WebSocket连接
    const terminal = new NezhaTerm(session.session_id, localStorage.getItem('token'));
    terminal.connect();
    
    // 3. 处理用户输入
    document.getElementById('input').addEventListener('keypress', (e) => {
      if (e.key === 'Enter') {
        const command = e.target.value + '\n';
        terminal.sendInput(command);
        e.target.value = '';
      }
    });
    
  } catch (error) {
    console.error('Failed to start terminal:', error);
  }
}
```

**使用xterm.js的完整实现**:
```javascript
import { Terminal } from 'xterm';
import { FitAddon } from 'xterm-addon-fit';

class NezhaWebShell {
  constructor(containerId) {
    this.container = document.getElementById(containerId);
    this.terminal = new Terminal({
      cursorBlink: true,
      fontSize: 14,
      fontFamily: 'Monaco, Menlo, "Ubuntu Mono", monospace'
    });
    
    this.fitAddon = new FitAddon();
    this.terminal.loadAddon(this.fitAddon);
    
    this.ws = null;
  }
  
  async connect(serverId, token) {
    try {
      // 创建终端会话
      const session = await this.createSession(serverId, token);
      
      // 挂载终端到DOM
      this.terminal.open(this.container);
      this.fitAddon.fit();
      
      // 建立WebSocket连接
      await this.connectWebSocket(session.session_id, token);
      
      // 绑定用户输入事件
      this.bindEvents();
      
    } catch (error) {
      this.terminal.write(`\r\n连接失败: ${error.message}\r\n`);
    }
  }
  
  async createSession(serverId, token) {
    const response = await fetch('/api/v1/terminal', {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        'Authorization': `Bearer ${token}`
      },
      body: JSON.stringify({ server_id: serverId })
    });
    
    const result = await response.json();
    if (!result.success) {
      throw new Error(result.error);
    }
    
    return result.data;
  }
  
  connectWebSocket(sessionId, token) {
    return new Promise((resolve, reject) => {
      const protocol = location.protocol === 'https:' ? 'wss:' : 'ws:';
      const url = `${protocol}//${location.host}/api/v1/ws/terminal/${sessionId}?token=${token}`;
      
      this.ws = new WebSocket(url);
      
      this.ws.onopen = () => {
        resolve();
      };
      
      this.ws.onmessage = (event) => {
        if (event.data instanceof Blob) {
          const reader = new FileReader();
          reader.onload = () => {
            const data = new Uint8Array(reader.result);
            this.terminal.write(data);
          };
          reader.readAsArrayBuffer(event.data);
        }
      };
      
      this.ws.onclose = () => {
        this.terminal.write('\r\n\r\n连接已断开\r\n');
      };
      
      this.ws.onerror = (error) => {
        reject(error);
      };
    });
  }
  
  bindEvents() {
    // 处理用户输入
    this.terminal.onData((data) => {
      if (this.ws && this.ws.readyState === WebSocket.OPEN) {
        this.ws.send(data);
      }
    });
    
    // 处理窗口大小变化
    window.addEventListener('resize', () => {
      this.fitAddon.fit();
    });
  }
  
  disconnect() {
    if (this.ws) {
      this.ws.close();
    }
    this.terminal.dispose();
  }
}

// 使用示例
const webshell = new NezhaWebShell('terminal-container');
webshell.connect(1, 'your-jwt-token');
```

## 技术架构

### 数据流图

```
浏览器 <--WebSocket--> Dashboard <--gRPC--> Agent <--PTY--> Shell
   |                      |                    |              |
   |                      |                    |              |
用户输入 ──────────────────┴────────────────────┴─────────────> 命令执行
   ↑                                                           |
   |                                                           ↓
终端显示 <─────────────────────────────────────────────────── 命令输出
```

### 组件说明

1. **前端WebSocket客户端**: 处理用户交互和终端显示
2. **Dashboard WebSocket服务**: 处理WebSocket连接和流管理
3. **gRPC流服务**: 在Dashboard和Agent间传输数据
4. **Agent终端处理**: 在服务器上执行实际的shell命令

### 安全特性

- **JWT认证**: 所有API调用需要有效的JWT Token
- **权限检查**: 验证用户对目标服务器的访问权限
- **会话隔离**: 每个终端会话有独立的UUID
- **连接超时**: 防止僵尸连接占用资源
- **自动清理**: 连接断开时自动清理相关资源

## 错误处理

### 常见错误码

| HTTP状态码 | 错误类型 | 处理建议 |
|------------|----------|----------|
| 401 | 未授权 | 检查JWT Token是否有效 |
| 403 | 权限不足 | 检查用户对服务器的访问权限 |
| 404 | 会话不存在 | 重新创建终端会话 |
| 500 | 服务器错误 | 检查服务器连接状态 |

### WebSocket错误处理

```javascript
ws.onerror = (error) => {
  console.error('WebSocket error:', error);
  
  // 自动重连逻辑
  setTimeout(() => {
    if (ws.readyState === WebSocket.CLOSED) {
      connectWebSocket();
    }
  }, 3000);
};

ws.onclose = (event) => {
  if (event.code !== 1000) { // 非正常关闭
    console.warn('Connection closed unexpectedly:', event.code, event.reason);
    
    // 显示重连选项
    showReconnectOption();
  }
};
```

## 性能优化

### 1. 连接池管理
- 限制同时连接数
- 自动清理空闲连接
- 连接超时设置

### 2. 数据传输优化
- 使用二进制WebSocket消息减少开销
- 缓冲池复用减少GC压力
- 心跳保活机制

### 3. 前端优化
- 虚拟滚动处理大量输出
- 节流用户输入防止频繁发送
- 离屏渲染提升性能

## 部署注意事项

### 1. 反向代理配置

**Nginx配置示例**:
```nginx
location /api/v1/ws/ {
    proxy_pass http://nezha-backend;
    proxy_http_version 1.1;
    proxy_set_header Upgrade $http_upgrade;
    proxy_set_header Connection "upgrade";
    proxy_set_header Host $host;
    proxy_set_header X-Real-IP $remote_addr;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto $scheme;
    
    # WebSocket超时设置
    proxy_read_timeout 86400;
    proxy_send_timeout 86400;
}
```

### 2. 防火墙配置
- 确保WebSocket端口可访问
- 配置适当的超时时间
- 启用keep-alive机制

### 3. 监控和日志
- 记录连接建立和断开事件
- 监控连接数和资源使用
- 设置异常告警机制

## 开发调试

### 启用调试模式

在配置文件中设置：
```yaml
debug: true
```

调试模式下的特性：
- 详细的WebSocket日志
- 允许本地CORS访问
- Swagger API文档可用

### 测试工具

**WebSocket测试**:
```bash
# 使用wscat测试WebSocket连接
npm install -g wscat
wscat -c "ws://localhost:8008/api/v1/ws/terminal/session-id" \
  -H "Authorization: Bearer your-token"
```

**API测试**:
```bash
# 测试创建终端API
curl -X POST http://localhost:8008/api/v1/terminal \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer your-token" \
  -d '{"server_id": 1}'
```

## 常见问题

### Q1: WebSocket连接失败怎么办？
A: 检查以下几点：
- JWT Token是否有效
- 服务器是否在线
- 网络连接是否正常
- 防火墙设置是否正确

### Q2: 终端显示乱码怎么解决？
A: 确保：
- 客户端使用UTF-8编码
- 正确处理二进制WebSocket消息
- xterm.js等终端库配置正确

### Q3: 如何实现终端大小自适应？
A: 使用xterm.js的FitAddon插件：
```javascript
import { FitAddon } from 'xterm-addon-fit';
const fitAddon = new FitAddon();
terminal.loadAddon(fitAddon);
fitAddon.fit();
```

### Q4: 连接断开后如何自动重连？
A: 实现重连逻辑：
```javascript
function reconnect() {
  setTimeout(() => {
    createSession().then(connectWebSocket);
  }, 3000);
}
```

---

*本文档基于Nezha监控系统v1.0版本编写，如有更新请参考最新的API文档。* 