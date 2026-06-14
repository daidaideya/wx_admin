# WX Admin - 微信授权管理后台

基于 docker-wx 协议层的微信授权管理面板，支持多设备扫码登录、用户管理、心跳保活等功能。

## ✨ 功能特性

### 用户管理
- 📱 多设备扫码登录（iPad / Android Pad / Windows / Mac / Car）
- 👥 在线用户管理（重登、唤醒、心跳、退出、删除）
- 📋 批量操作（批量登录、唤醒、退出、删除）
- 🔄 Redis 用户同步

### 心跳保活
- ❤️ 自动心跳（可配置间隔时间）
- 💓 手动心跳
- 🔋 长连接心跳支持

### 系统监控
- 📊 系统状态（CPU、内存、运行时间、在线账号）
- 📝 实时日志流（SSE）
- 📥 日志一键下载
- ⚙️ 日志配置（最大条数、保留天数）

### 代理支持
- 🌐 SOCKS5 代理配置
- 🔐 代理认证（用户名/密码）

## 🚀 快速开始

### 方式一：直接运行

```bash
# 确保 wechatReal08 已经在 8061 端口运行
cd wx-admin
go build -o wx-admin.exe .
./wx-admin.exe
```

### 方式二：Docker 单独部署

```bash
# 构建镜像
docker build -t wx-admin .

# 运行容器
docker run -d -p 8022:8022 \
  -e WX_API=http://你的后端地址:8061 \
  -e ADMIN_TOKEN=你的密码 \
  -e REDIS_ADDR=你的Redis地址:6379 \
  --name wx-admin \
  wx-admin
```

### 方式三：Docker Compose 一键部署

```bash
# 一键启动所有服务（包括后端和 Redis）
docker-compose up -d

# 查看日志
docker-compose logs -f

# 停止服务
docker-compose down
```

### Docker Compose 服务说明

| 服务 | 端口 | 说明 |
|------|------|------|
| `wx-admin` | 8022 | 管理面板 |
| `wx-backend` | 8061 | 微信协议后端 |
| `redis` | 6379 | Redis 数据库 |

### 构建后端镜像

在使用 Docker Compose 之前，需要先构建 wechatReal08 镜像：

```bash
# 进入 wechatReal08 目录
cd ../wechatReal08

# 构建镜像
docker build -t wechatreal08 .

# 返回 wx-admin 目录
cd ../wx-admin
```

### Redis 配置说明

Docker 环境下，wechatReal08 需要连接 Redis。请确保 `wechatReal08/conf/app.conf` 中的 Redis 配置正确：

```ini
# Docker 环境下使用 redis 作为主机名
redislink = redis:6379
redispass = ""
redisdbnum = 0
```

**注意**：`redisdbnum` 必须设置为 `0`，因为 wx-admin 默认使用 DB 0。

### 单独运行后端（可选）

如果不想使用 Docker Compose，可以单独运行后端：

```bash
# 运行 Redis
docker run -d -p 6379:6379 --name redis redis:7-alpine

# 运行 wechatReal08
docker run -d -p 8061:8061 \
  -v $(pwd)/wechatReal08/conf:/app/conf \
  --name wx-backend \
  wechatreal08

# 运行 wx-admin
docker run -d -p 8022:8022 \
  -e WX_API=http://host.docker.internal:8061 \
  -e REDIS_ADDR=host.docker.internal:6379 \
  --name wx-admin \
  wx-admin
```

### 环境变量配置

创建 `.env` 文件进行配置：

```env
# 管理面板配置
PORT=8022
WX_API=http://wx-backend:8061
ADMIN_TOKEN=your_secure_password
REDIS_ADDR=redis:6379

# 时区配置
TZ=Asia/Shanghai
```

## ⚙️ 环境变量

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `PORT` | `8022` | 监听端口 |
| `WX_API` | `http://127.0.0.1:8061` | 后端服务地址 |
| `ADMIN_TOKEN` | `admin123` | 管理后台密码 |
| `REDIS_ADDR` | `127.0.0.1:6379` | Redis 地址 |

## 📡 API 接口

### 系统管理

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/system/info` | 获取系统信息 |
| GET | `/system/status` | 获取系统状态 |
| GET | `/system/logs` | 获取系统日志 |
| GET | `/system/logs/stream` | SSE 日志流 |
| GET | `/system/log/config` | 获取日志配置 |
| POST | `/system/log/config` | 保存日志配置 |
| POST | `/system/activate` | 激活码验证 |

### 用户管理

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/v1/wx/user/status` | 获取用户列表 |
| POST | `/api/v1/wx/user/sync` | 从 Redis 同步用户 |
| POST | `/api/v1/wx/user/database` | 获取用户缓存信息 |
| POST | `/api/v1/wx/user/delete` | 删除用户 |
| POST | `/api/v1/wx/user/heartbeat` | 用户心跳 |

### 登录管理

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/v1/wx/login/devices` | 获取设备类型列表 |
| POST | `/api/v1/wx/login/qrcode` | 获取登录二维码 |
| POST | `/api/v1/wx/login/status` | 检查登录状态 |
| POST | `/api/v1/wx/login/again` | 重新登录 |
| POST | `/api/v1/wx/login/awake` | 唤醒登录 |
| POST | `/api/v1/wx/login/logout` | 退出登录 |

### 心跳控制

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/v1/wx/heartbeat/status` | 获取心跳状态 |
| POST | `/api/v1/wx/heartbeat/toggle` | 开关自动心跳 |

### 工具

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/api/v1/wx/tools/update/proxy` | 更新代理配置 |

## 🔌 与后端 API 对应关系

| WX Admin API | 后端 API | 功能 |
|---|---|---|
| `/api/v1/wx/login/qrcode` | `/api/Login/LoginGetQR*` | 获取登录二维码 |
| `/api/v1/wx/login/status` | `/api/Login/LoginCheckQR` | 检查登录状态 |
| `/api/v1/wx/login/again` | `/api/Login/LoginTwiceAutoAuth` | 重新登录 |
| `/api/v1/wx/login/awake` | `/api/Login/LoginAwaken` | 唤醒登录 |
| `/api/v1/wx/login/logout` | `/api/Login/LogOut` | 退出登录 |
| `/api/v1/wx/user/heartbeat` | `/api/Login/HeartBeat` | 心跳 |
| `/api/v1/wx/tools/update/proxy` | `/api/Tools/setproxy` | 设置代理 |

## 📱 支持的设备类型

| 设备类型 | Key | 后端接口 | 说明 |
|---|---|---|---|
| 安卓Pad | `pad` | `/api/Login/LoginGetQRPad` | 安卓平板 |
| 安卓Pad(绕验证码) | `padx` | `/api/Login/LoginGetQRPadx` | 安卓平板绕过验证码 |
| iPad | `ipad` | `/api/Login/LoginGetQR` | iPad 设备 |
| iPad(绕验证码) | `ipadx` | `/api/Login/LoginGetQRx` | iPad 绕过验证码 |
| Mac | `mac` | `/api/Login/LoginGetQRMac` | MacBook |
| Car | `car` | `/api/Login/LoginGetQRCar` | 车机设备 |
| Windows | `win` | `/api/Login/LoginGetQRWin` | Windows 客户端 |
| Windows UWP | `winuwp` | `/api/Login/LoginGetQRWinUwp` | Windows UWP 绕验证码 |
| Windows 统一版 | `winunified` | `/api/Login/LoginGetQRWinUnified` | Windows 统一版 |

## 🏗️ 架构

```
浏览器 ──→ WX Admin (8022) ──→ wechatReal08 (8061) ──→ 微信服务器
              │
              ├── 前端: Vue 3 + Material Design
              ├── 后端: Go + Gin (反向代理 + 用户管理)
              └── 代理 wechatReal08 的 Login/User/Tools API
```

## 📁 项目结构

```
wx-admin/
├── main.go              # 主程序
├── go.mod               # Go 模块文件
├── go.sum               # Go 依赖校验
├── Dockerfile           # Docker 构建文件
├── docker-compose.yml   # Docker Compose 配置
├── README.md            # 项目说明
├── conf/                # 配置文件
├── static/              # 前端静态文件
│   ├── index.html       # 主页面
│   ├── css/
│   │   └── app.css      # 样式文件
│   └── js/
│       ├── app.js       # 前端逻辑
│       └── vue.global.prod.js  # Vue 3
└── wx-admin.exe         # 编译后的可执行文件
```

## 🔧 配置说明

### 日志配置

在系统设置页面可以配置：

- **最大日志条数**：默认 5000 条，超过后自动截断旧日志
- **最大保留天数**：默认 3 天，超过后自动清理

### 心跳配置

- **自动心跳**：默认开启，每 150 秒（2.5分钟）自动发送心跳
- **心跳间隔**：可在 30-600 秒之间调整
- **心跳类型**：
  - 普通心跳：`/api/Login/HeartBeat`
  - 长连接心跳：`/api/Login/HeartBeatLong`

## 📝 日志格式

```
[06-14 16:04:29] INFO 操作信息
[06-14 16:04:29] WARN 警告信息
[06-14 16:04:29] ERROR 错误信息
```

### 日志下载

点击系统状态页面的"下载"按钮，可下载格式化的日志文件：
- 文件名：`wx-admin-logs-YYYY-MM-DD.txt`
- 格式：每行一条日志，包含时间、级别、消息

## 🛠️ 开发

### 依赖

- Go 1.21+
- Redis 6.0+

### 编译

```bash
go build -o wx-admin.exe .
```

### 运行

```bash
./wx-admin.exe
```

## 📄 许可证

MIT License

## 🤝 贡献

欢迎提交 Issue 和 Pull Request！

## 📞 支持

如有问题，请提交 Issue 或联系开发者。
