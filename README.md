# DNSPlane - Go版DNS管理系统

基于原PHP版dnsmgr重构的Go语言实现，支持多平台DNS管理、SSL证书申请部署、容灾切换等功能。

## 功能特性

- **多平台DNS管理**: 支持阿里云、腾讯云、华为云、Cloudflare等15+平台
- **证书申请**: 集成ACME协议，支持Let's Encrypt、ZeroSSL等
- **证书部署**: 支持SSH、本地、CDN等多种部署方式
- **容灾切换**: 支持ping/tcp/http检测，自动故障切换
- **多用户管理**: 支持用户权限分配
- **现代化UI**: 基于shadcn/ui构建的响应式界面

## 项目结构

```
dns/
├── main/                    # Go后端
│   ├── main.go             # 入口文件
│   ├── go.mod              # Go依赖
│   ├── internal/
│   │   ├── api/            # API层
│   │   │   ├── handler/    # 请求处理器
│   │   │   ├── middleware/ # 中间件
│   │   │   └── router.go   # 路由配置
│   │   ├── cert/           # 证书模块
│   │   │   ├── acme/       # ACME客户端
│   │   │   └── deploy/     # 部署适配器
│   │   ├── config/         # 配置管理
│   │   ├── database/       # 数据库层
│   │   ├── dns/            # DNS模块
│   │   │   └── providers/  # DNS服务商适配器
│   │   ├── models/         # 数据模型
│   │   └── monitor/        # 容灾监控
│   └── web/out/            # 前端静态文件
└── web/                     # Next.js前端
    ├── app/                # 页面
    ├── components/         # 组件
    └── lib/                # 工具库
```

## 快速开始

### 1. 安装依赖

```bash
# Go后端依赖
cd dns/main
go mod tidy

# 前端依赖
cd ../web
bun install  # 或 npm install
```

### 2. 构建前端

```bash
cd dns/web
bun run build  # 静态文件将输出到 ../main/web/out
```

### 3. 运行后端

```bash
cd dns/main
go run .
```

服务将在 http://localhost:8080 启动

### 4. 首次安装

访问 http://localhost:8080 会自动跳转到安装页面，设置管理员账户。

## 配置文件

创建 `config.json`:

```json
{
  "server": {
    "port": 8080,
    "host": "0.0.0.0",
    "mode": "release"
  },
  "database": {
    "driver": "sqlite",
    "file_path": "data/dnsplane.db"
  },
  "jwt": {
    "secret": "your-secret-key",
    "expire_hour": 24
  }
}
```

## 支持的DNS平台

| 平台 | 类型 | 备注/状态 |
|------|------|--------|
| 阿里云 | aliyun | ✅ 完整支持 |
| 腾讯云 | dnspod | ✅ 完整支持 |
| Cloudflare | cloudflare | ✅ 完整支持 |
| 华为云 | huawei | 待实现 |
| 百度云 | baidu | 待实现 |
| 火山引擎 | huoshan | 待实现 |

## 支持的证书渠道

| 渠道 | 类型 | 说明 |
|------|------|------|
| Let's Encrypt | letsencrypt | ✅ ACME v2 |
| ZeroSSL | zerossl | ✅ 需要EAB |

## 支持的部署方式

- SSH远程部署
- 本地文件部署
- 阿里云CDN
- 腾讯云CDN
- 宝塔面板

## API接口

所有API以 `/api` 为前缀，需要Bearer Token认证。

### 认证
- `POST /api/login` - 登录
- `POST /api/install` - 安装
- `GET /api/install/status` - 安装状态

### 账户管理
- `GET /api/accounts` - 账户列表
- `POST /api/accounts` - 创建账户
- `PUT /api/accounts/:id` - 更新账户
- `DELETE /api/accounts/:id` - 删除账户

### 域名管理
- `GET /api/domains` - 域名列表
- `POST /api/domains` - 添加域名
- `GET /api/domains/:id/records` - 解析记录

### 容灾切换
- `GET /api/monitor/tasks` - 任务列表
- `POST /api/monitor/tasks` - 创建任务

## 编译发布

```bash
# 构建前端
cd dns/web
bun run build

# 编译后端（包含前端静态文件）
cd ../main
go build -o dnsplane .

# 运行
./dnsplane -config config.json
```

## 开发说明

- Go版本: 1.23+
- Node.js版本: 20+ (或使用Bun)
- 数据库: SQLite (默认) / MySQL

## License

MIT License
