# API参考文档

<cite>
**本文档引用的文件**
- [router.go](file://main/internal/api/router.go)
- [auth.go](file://main/internal/api/middleware/auth.go)
- [apikey.go](file://main/internal/api/middleware/apikey.go)
- [sign.go](file://main/internal/api/middleware/sign.go)
- [auth_handler.go](file://main/internal/api/handler/auth.go)
- [account_handler.go](file://main/internal/api/handler/account.go)
- [domain_handler.go](file://main/internal/api/handler/domain.go)
- [monitor_handler.go](file://main/internal/api/handler/monitor.go)
- [user_handler.go](file://main/internal/api/handler/user.go)
- [cert_handler.go](file://main/internal/api/handler/cert.go)
- [logs_handler.go](file://main/internal/api/handler/logs.go)
- [dashboard_handler.go](file://main/internal/api/handler/dashboard.go)
- [request_log_handler.go](file://main/internal/api/handler/request_log.go)
- [oauth_handler.go](file://main/internal/api/handler/oauth.go)
- [quota_handler.go](file://main/internal/api/handler/quota.go)
</cite>

## 目录
1. [简介](#简介)
2. [项目结构](#项目结构)
3. [核心组件](#核心组件)
4. [架构概览](#架构概览)
5. [详细组件分析](#详细组件分析)
6. [依赖关系分析](#依赖关系分析)
7. [性能考虑](#性能考虑)
8. [故障排除指南](#故障排除指南)
9. [结论](#结论)

## 简介

DNSPlane是一个基于Go语言开发的DNS管理平台，提供了完整的DNS域名管理、证书管理、监控告警等功能。本文档详细记录了系统的RESTful API接口规范，包括认证机制、账户管理、域名管理、监控告警等核心功能模块。

## 项目结构

DNSPlane采用典型的三层架构设计：

```mermaid
graph TB
subgraph "API层"
Router[路由器]
Handler[处理器]
end
subgraph "中间件层"
Auth[认证中间件]
Sign[签名中间件]
APIKey[API密钥中间件]
end
subgraph "业务逻辑层"
Account[账户管理]
Domain[域名管理]
Monitor[监控告警]
Certificate[证书管理]
User[用户管理]
end
subgraph "数据访问层"
Database[数据库]
Cache[缓存]
end
Router --> Handler
Handler --> Auth
Handler --> Sign
Handler --> APIKey
Handler --> Account
Handler --> Domain
Handler --> Monitor
Handler --> Certificate
Handler --> User
Account --> Database
Domain --> Database
Monitor --> Database
Certificate --> Database
User --> Database
Database --> Cache
```

**图表来源**
- [router.go:14-162](file://main/internal/api/router.go#L14-L162)
- [auth.go:124-199](file://main/internal/api/middleware/auth.go#L124-L199)

**章节来源**
- [router.go:1-275](file://main/internal/api/router.go#L1-L275)

## 核心组件

### 认证机制

DNSPlane采用双重认证机制：
1. **Cookie认证**：使用AES-GCM加密的HttpOnly Cookie存储访问令牌
2. **Bearer Token认证**：通过Authorization头传递JWT访问令牌

### 数据加密传输

系统支持端到端加密传输，使用混合加密方案：
- **对称加密**：AES-256-GCM
- **非对称加密**：RSA公钥加密
- **签名验证**：HMAC-SHA256

### API版本控制

系统采用路径版本控制策略，在API路径中明确标识版本号，确保向后兼容性。

**章节来源**
- [auth.go:25-87](file://main/internal/api/middleware/auth.go#L25-L87)
- [sign.go:13-69](file://main/internal/api/middleware/sign.go#L13-L69)

## 架构概览

```mermaid
sequenceDiagram
participant Client as 客户端
participant Router as 路由器
participant Auth as 认证中间件
participant Handler as 处理器
participant DB as 数据库
Client->>Router : HTTP请求
Router->>Auth : 验证Cookie和Token
Auth->>Auth : 解密Cookie
Auth->>Auth : 验证JWT令牌
Auth->>Handler : 通过认证
Handler->>DB : 数据库操作
DB-->>Handler : 返回结果
Handler-->>Client : 加密响应
```

**图表来源**
- [router.go:21-159](file://main/internal/api/router.go#L21-L159)
- [auth.go:124-199](file://main/internal/api/middleware/auth.go#L124-L199)

## 详细组件分析

### 认证模块

#### 登录接口

**HTTP方法**: POST  
**URL模式**: `/api/login`  
**功能**: 用户登录认证

**请求参数**:
- username: 用户名 (必填)
- password: 密码 (必填)
- captcha_id: 验证码ID (可选)
- captcha_code: 验证码 (可选)
- totp_code: 两步验证码 (可选)

**响应格式**:
```json
{
  "code": 0,
  "msg": "登录成功",
  "data": {
    "token": "JWT访问令牌",
    "user": {
      "id": 1,
      "username": "admin",
      "level": 1
    }
  }
}
```

**错误码**:
- 401: 未登录
- 403: 账户被禁用
- 404: 用户不存在

#### 注销接口

**HTTP方法**: POST  
**URL模式**: `/api/logout`  
**功能**: 用户注销

**响应格式**:
```json
{
  "code": 0,
  "msg": "退出成功"
}
```

#### 两步验证

**启用两步验证**:
- GET `/api/user/totp/status` - 查询状态
- POST `/api/user/totp/enable` - 生成密钥
- POST `/api/user/totp/verify` - 验证并启用
- POST `/api/user/totp/disable` - 禁用

**章节来源**
- [auth_handler.go:67-149](file://main/internal/api/handler/auth.go#L67-L149)
- [auth_handler.go:272-414](file://main/internal/api/handler/auth.go#L272-L414)

### 账户管理模块

#### DNS账户管理

**获取账户列表**:
- GET `/api/accounts` - 获取所有DNS账户
- POST `/api/accounts` - 创建新账户
- PUT `/api/accounts/:id` - 更新账户
- DELETE `/api/accounts/:id` - 删除账户
- POST `/api/accounts/:id/check` - 检测连接
- GET `/api/accounts/:id/domains` - 获取域名列表

**请求参数**:
```json
{
  "type": "aliyun",
  "name": "阿里云账户",
  "config": {
    "AccessKeyId": "<YOUR_ALIYUN_ACCESS_KEY_ID>",
    "AccessKeySecret": "<YOUR_ALIYUN_ACCESS_KEY_SECRET>"
  },
  "remark": "生产环境账户"
}
```

> 字段名与 `main/internal/dns/providers/aliyun/aliyun.go` 中 `ConfigField` 声明一致（`AccessKeyId` / `AccessKeySecret`）；不同 DNS 服务商的 `config` 键名不同，请以 `GET /api/dns/providers` 返回的 `config[].key` 为准。

**响应格式**:
```json
{
  "code": 0,
  "data": [
    {
      "id": 1,
      "uid": 1,
      "type": "aliyun",
      "type_name": "阿里云",
      "name": "阿里云账户",
      "config": "{}",
      "remark": "生产环境账户",
      "created_at": "2023-01-01T00:00:00Z"
    }
  ]
}
```

**章节来源**
- [account_handler.go:85-126](file://main/internal/api/handler/account.go#L85-L126)
- [account_handler.go:184-237](file://main/internal/api/handler/account.go#L184-L237)

### 域名管理模块

#### 域名操作

**获取域名列表**:
- GET `/api/domains` - 获取域名列表
- POST `/api/domains` - 创建域名
- PUT `/api/domains/:id` - 更新域名
- DELETE `/api/domains/:id` - 删除域名
- POST `/api/domains/sync` - 同步域名

**域名记录管理**:
- GET `/api/domains/:id/records` - 获取解析记录
- POST `/api/domains/:id/records` - 创建记录
- PUT `/api/domains/:id/records/:recordId` - 更新记录
- DELETE `/api/domains/:id/records/:recordId` - 删除记录
- POST `/api/domains/:id/records/:recordId/status` - 设置记录状态

**请求参数示例**:
```json
{
  "account_id": "1",
  "name": "example.com"
}
```

**响应格式**:
```json
{
  "code": 0,
  "data": {
    "total": 10,
    "list": [
      {
        "id": 1,
        "aid": 1,
        "name": "example.com",
        "third_id": "D123456",
        "record_count": 10,
        "remark": "",
        "expire_time": "2024-01-01T00:00:00Z",
        "created_at": "2023-01-01T00:00:00Z",
        "account_name": "阿里云账户",
        "account_type": "aliyun",
        "type_name": "阿里云"
      }
    ]
  }
}
```

**章节来源**
- [domain_handler.go:79-196](file://main/internal/api/handler/domain.go#L79-L196)
- [domain_handler.go:548-728](file://main/internal/api/handler/domain.go#L548-L728)

### 监控告警模块

#### 监控任务管理

**获取监控任务**:
- GET `/api/monitor/tasks` - 获取任务列表
- POST `/api/monitor/tasks` - 创建任务
- PUT `/api/monitor/tasks/:id` - 更新任务
- DELETE `/api/monitor/tasks/:id` - 删除任务
- POST `/api/monitor/tasks/:id/toggle` - 切换任务状态
- POST `/api/monitor/tasks/:id/switch` - 手动切换

**批量操作**:
- POST `/api/monitor/tasks/batch` - 批量创建任务

**监控日志**:
- GET `/api/monitor/tasks/:id/logs` - 获取任务日志
- GET `/api/monitor/overview` - 获取监控概览
- GET `/api/monitor/status` - 获取监控状态

**请求参数示例**:
```json
{
  "domain_id": 1,
  "rr": "@",
  "record_id": "R123456",
  "type": 0,
  "main_value": "192.168.1.1",
  "backup_value": "10.0.0.1",
  "check_type": 0,
  "frequency": 60,
  "cycle": 3,
  "timeout": 2
}
```

**响应格式**:
```json
{
  "code": 0,
  "data": {
    "task_count": 10,
    "active_count": 8,
    "healthy_count": 7,
    "faulty_count": 1,
    "switch_count": 5,
    "fail_count": 2,
    "avg_uptime": 99.95,
    "run_time": "2023-01-01 12:00:00",
    "run_count": 1000,
    "run_state": 1
  }
}
```

**章节来源**
- [monitor_handler.go:106-155](file://main/internal/api/handler/monitor.go#L106-L155)
- [monitor_handler.go:208-263](file://main/internal/api/handler/monitor.go#L208-L263)
- [monitor_handler.go:528-603](file://main/internal/api/handler/monitor.go#L528-L603)

### 证书管理模块

#### 证书账户管理

**证书账户操作**:
- GET `/api/cert/accounts` - 获取证书账户列表
- POST `/api/cert/accounts` - 创建证书账户
- PUT `/api/cert/accounts/:id` - 更新账户
- DELETE `/api/cert/accounts/:id` - 删除账户

**证书订单管理**:
- GET `/api/cert/orders` - 获取订单列表
- POST `/api/cert/orders` - 创建订单
- POST `/api/cert/orders/:id/process` - 处理订单
- DELETE `/api/cert/orders/:id` - 删除订单
- GET `/api/cert/orders/:id/log` - 获取订单日志
- GET `/api/cert/orders/:id/detail` - 获取订单详情
- GET `/api/cert/orders/:id/download` - 下载证书
- POST `/api/cert/orders/:id/auto` - 切换自动续期

**证书部署管理**:
- GET `/api/cert/deploys` - 获取部署列表
- POST `/api/cert/deploys` - 创建部署
- PUT `/api/cert/deploys/:id` - 更新部署
- DELETE `/api/cert/deploys/:id` - 删除部署
- POST `/api/cert/deploys/:id/process` - 处理部署

**请求参数示例**:
```json
{
  "account_id": 1,
  "domains": ["example.com", "*.example.com"],
  "key_type": "RSA",
  "key_size": "2048",
  "is_auto": true
}
```

**响应格式**:
```json
{
  "code": 0,
  "data": {
    "id": 1,
    "domains": ["example.com", "*.example.com"],
    "key_type": "RSA",
    "key_size": "2048",
    "status": 3,
    "issuer": "Let's Encrypt",
    "issue_time": "2023-01-01T00:00:00Z",
    "expire_time": "2023-03-31T00:00:00Z",
    "is_auto": true,
    "fullchain": "-----BEGIN CERTIFICATE-----...",
    "private_key": "-----BEGIN PRIVATE KEY-----..."
  }
}
```

**章节来源**
- [cert_handler.go:23-51](file://main/internal/api/handler/cert.go#L23-L51)
- [cert_handler.go:155-223](file://main/internal/api/handler/cert.go#L155-L223)
- [cert_handler.go:669-737](file://main/internal/api/handler/cert.go#L669-L737)

### 用户管理模块

#### 用户操作

**用户管理**:
- GET `/api/users` - 获取用户列表
- POST `/api/users` - 创建用户
- PUT `/api/users/:id` - 更新用户
- DELETE `/api/users/:id` - 删除用户
- POST `/api/users/:id/reset-apikey` - 重置API密钥
- POST `/api/users/:id/send-reset` - 发送重置邮件
- POST `/api/users/:id/reset-totp` - 重置TOTP

**用户权限**:
- GET `/api/users/:id/permissions` - 获取用户权限
- POST `/api/users/:id/permissions` - 添加权限
- PUT `/api/users/:id/permissions/:permId` - 更新权限
- DELETE `/api/users/:id/permissions/:permId` - 删除权限

**系统配置**:
- GET `/api/system/config` - 获取系统配置
- POST `/api/system/config` - 更新系统配置

**请求参数示例**:
```json
{
  "username": "testuser",
  "password": "password123",
  "email": "test@example.com",
  "level": 1,
  "is_api": false,
  "permissions": ""
}
```

**响应格式**:
```json
{
  "code": 0,
  "data": {
    "total": 10,
    "list": [
      {
        "id": 1,
        "username": "admin",
        "email": "admin@example.com",
        "level": 3,
        "is_api": true,
        "api_key": "abc123def456",
        "status": 1,
        "permissions": "",
        "totp_open": false,
        "reg_time": "2023-01-01T00:00:00Z",
        "last_time": "2023-01-01T12:00:00Z"
      }
    ]
  }
}
```

**章节来源**
- [user_handler.go:23-45](file://main/internal/api/handler/user.go#L23-L45)
- [user_handler.go:56-98](file://main/internal/api/handler/user.go#L56-L98)

### 系统监控模块

#### 仪表板统计

**系统统计**:
- GET `/api/dashboard/stats` - 获取仪表板统计
- POST `/api/system/mail/test` - 测试邮件通知
- POST `/api/system/telegram/test` - 测试Telegram通知
- POST `/api/system/webhook/test` - 测试Webhook通知
- POST `/api/system/cache/clear` - 清除缓存

**代理测试**:
- POST `/api/system/proxy/test` - 测试代理服务器

**定时任务**:
- GET `/api/system/task/status` - 获取任务状态
- GET `/api/system/cron` - 获取定时任务配置
- POST `/api/system/cron` - 更新定时任务配置

**请求参数示例**:
```json
{
  "type": "http",
  "host": "proxy.example.com",
  "port": 8080,
  "user": "username",
  "pass": "password"
}
```

**响应格式**:
```json
{
  "code": 0,
  "msg": "代理连接成功",
  "data": {
    "latency": 150,
    "status": 200
  }
}
```

**章节来源**
- [dashboard_handler.go:42-129](file://main/internal/api/handler/dashboard.go#L42-L129)
- [dashboard_handler.go:287-365](file://main/internal/api/handler/dashboard.go#L287-L365)

### 日志管理模块

#### 请求日志

**请求日志管理**:
- POST `/api/request-logs/list` - 获取请求日志列表
- POST `/api/request-logs/detail` - 获取请求详情
- POST `/api/request-logs/error` - 获取错误详情
- POST `/api/request-logs/stats` - 获取统计信息
- POST `/api/request-logs/clean` - 清理日志

**域名日志**:
- GET `/api/domains/:id/logs` - 获取域名操作日志

**系统日志**:
- GET `/api/logs` - 获取系统日志
- GET `/api/logs/:id` - 获取日志详情

**请求参数示例**:
```json
{
  "page": 1,
  "page_size": 20,
  "keyword": "login",
  "is_error": "1",
  "method": "POST",
  "start_date": "2023-01-01",
  "end_date": "2023-01-02"
}
```

**响应格式**:
```json
{
  "code": 0,
  "data": {
    "total": 100,
    "list": [
      {
        "id": 1,
        "request_id": "req123",
        "error_id": "err456",
        "user_id": 1,
        "username": "admin",
        "method": "POST",
        "path": "/api/login",
        "ip": "127.0.0.1",
        "user_agent": "Mozilla/5.0...",
        "status_code": 200,
        "duration": 150,
        "is_error": false,
        "error_msg": "",
        "created_at": "2023-01-01T12:00:00Z"
      }
    ]
  }
}
```

**章节来源**
- [request_log_handler.go:99-129](file://main/internal/api/handler/request_log.go#L99-L129)
- [logs_handler.go:27-87](file://main/internal/api/handler/logs.go#L27-L87)

### OAuth集成模块

#### OAuth认证

**OAuth提供商**:
- GET `/api/auth/oauth/providers` - 获取OAuth提供商列表
- GET `/api/auth/oauth/:provider/login` - 跳转到OAuth授权页面
- GET `/api/auth/oauth/:provider/callback` - OAuth回调处理

**用户绑定**:
- GET `/api/user/oauth/bindings` - 获取OAuth绑定列表
- POST `/api/user/oauth/bind-url` - 获取绑定URL
- POST `/api/user/oauth/unbind` - 解绑OAuth

**请求参数示例**:
```json
{
  "provider": "github"
}
```

**响应格式**:
```json
{
  "code": 0,
  "data": {
    "url": "https://github.com/login/oauth/authorize?client_id=...&state=..."
  }
}
```

**章节来源**
- [oauth_handler.go:54-58](file://main/internal/api/handler/oauth.go#L54-L58)
- [oauth_handler.go:312-347](file://main/internal/api/handler/oauth.go#L312-L347)

## 依赖关系分析

```mermaid
graph TD
subgraph "外部依赖"
Gin[Gin框架]
JWT[JWT库]
GORM[GORM ORM]
Redis[Redis缓存]
end
subgraph "内部模块"
Router[路由器]
Auth[认证中间件]
Handler[处理器]
DB[数据库]
Cache[缓存]
end
Router --> Gin
Auth --> JWT
Handler --> GORM
DB --> Redis
Handler --> DB
Handler --> Cache
```

**图表来源**
- [auth.go:21-22](file://main/internal/api/middleware/auth.go#L21-L22)
- [router.go:3-11](file://main/internal/api/router.go#L3-L11)

### API密钥认证

系统支持API密钥认证机制，适用于自动化脚本和第三方集成：

**认证头要求**:
- X-API-UID: 用户ID
- X-API-Timestamp: 时间戳
- X-API-Sign: HMAC-SHA256签名

**签名算法**:
```
signature = HMAC-SHA256(
  user_id + "\n" + timestamp + "\n" + method + "\n" + path,
  api_key
)
```

**章节来源**
- [apikey.go:44-105](file://main/internal/api/middleware/apikey.go#L44-L105)

## 性能考虑

### 缓存策略

系统采用多层次缓存机制：
1. **认证缓存**: 用户信息缓存，TTL 30秒
2. **DNS账户缓存**: 账户列表缓存，TTL 60秒
3. **请求日志缓存**: 统计数据缓存，TTL 60秒

### 连接池管理

- **数据库连接池**: 最大连接数100，空闲连接10
- **Redis连接池**: 最大连接数50，空闲连接5
- **HTTP客户端连接池**: 最大空闲连接100，每主机最大空闲连接10

### 异步处理

系统对耗时操作采用异步处理：
- 证书申请异步处理
- 监控任务异步执行
- 日志写入异步处理

## 故障排除指南

### 常见错误码

| 错误码 | 描述 | 处理建议 |
|--------|------|----------|
| 400 | 参数错误 | 检查请求参数格式和必填项 |
| 401 | 未认证 | 检查Cookie和Authorization头 |
| 403 | 权限不足 | 确认用户权限级别 |
| 404 | 资源不存在 | 检查ID是否正确 |
| 429 | 请求过于频繁 | 实现指数退避重试 |
| 500 | 服务器内部错误 | 查看服务器日志 |

### 认证问题排查

1. **Cookie失效**: 检查HTTPS环境和SameSite设置
2. **Token过期**: 使用刷新令牌获取新访问令牌
3. **签名验证失败**: 确认时间戳在允许范围内（±5分钟）

### 性能优化建议

1. **批量操作**: 使用批量API减少请求次数
2. **缓存利用**: 合理使用缓存避免重复查询
3. **分页处理**: 对大数据集使用分页查询
4. **连接复用**: 复用HTTP连接池

**章节来源**
- [auth.go:469-547](file://main/internal/api/middleware/auth.go#L469-L547)
- [quota_handler.go:10-18](file://main/internal/api/handler/quota.go#L10-L18)

## 结论

DNSPlane提供了完整的DNS管理API解决方案，具有以下特点：

1. **安全性**: 多层认证机制、数据加密传输、严格的权限控制
2. **可靠性**: 异步处理、缓存优化、错误重试机制
3. **可扩展性**: 模块化设计、插件化架构、API版本控制
4. **易用性**: 完整的文档、示例代码、错误处理

开发者可以根据具体需求选择合适的认证方式和API接口，构建稳定可靠的DNS管理应用。