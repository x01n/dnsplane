# 证书管理API

<cite>
**本文档引用的文件**
- [router.go](file://main/internal/api/router.go)
- [cert.go](file://main/internal/api/handler/cert.go)
- [cert_deploy.go](file://main/internal/api/handler/cert_deploy.go)
- [cert_authz.go](file://main/internal/api/handler/cert_authz.go)
- [cname.go](file://main/internal/api/handler/cname.go)
- [schedule.go](file://main/internal/api/handler/schedule.go)
- [quota.go](file://main/internal/api/handler/quota.go)
- [dashboard.go](file://main/internal/api/handler/dashboard.go)
- [sign.go](file://main/internal/api/middleware/sign.go)
- [router.go (apiv1)](file://main/internal/apiv1/router.go)
- [routes_batch1.go](file://main/internal/apiv1/routes_batch1.go)
- [auth.go](file://main/internal/apiv1/auth.go)
- [errors.go](file://main/internal/apiv1/errors.go)
- [bridge.go](file://main/internal/apiv1/bridge.go)
- [adapt.go](file://main/internal/apiv1/adapt.go)
- [scope.go](file://main/internal/apiv1/scope.go)
- [domain.go (apiv1)](file://main/internal/apiv1/domain.go)
- [quota_gate.go](file://main/internal/apiv1/quota_gate.go)
- [interface.go](file://main/internal/cert/interface.go)
- [registry.go](file://main/internal/cert/registry.go)
- [acme.go](file://main/internal/cert/acme/acme.go)
- [config.go](file://main/internal/cert/deploy/config.go)
- [models.go](file://main/internal/models/models.go)
- [hooks.go](file://main/internal/models/hooks.go)
- [task_runner.go](file://main/internal/service/task_runner.go)
- [api.ts](file://web/lib/api.ts)
</cite>

## 目录

1. [简介](#简介)
2. [项目结构](#项目结构)
3. [核心组件](#核心组件)
4. [架构总览](#架构总览)
5. [详细组件分析](#详细组件分析)
6. [依赖关系分析](#依赖关系分析)
7. [性能考虑](#性能考虑)
8. [故障排查指南](#故障排查指南)
9. [结论](#结论)
10. [附录](#附录)

## 简介
本文是 DNSPlane 证书域的接口参考，覆盖「证书账户 → 签发订单 → 部署任务 → 部署器元数据 → CNAME 托管 → 定时切换 → 任务观测」七族 HTTP 接口，并同时给出两条调用链的契约。

| 维度 | 控制台链 `/api` | 对外链 `/api/v1` |
| --- | --- | --- |
| 认证 | Cookie `_t` + Bearer 双比对 | `X-API-UID` / `X-API-Timestamp` / `X-API-Sign` HMAC-SHA256 |
| 方法 | GET 与 POST 混用，37 条含路径参数 | 全部 POST，无路径参数 |
| 响应 | `{code, msg, data}`，`code` 仅 `0` / `-1` | `{code, request_id, data}` / `{code, request_id, message}` |
| 错误码 | 业务错误 HTTP 200 + `code:-1` | 分段码，错误映射真实 HTTP 状态 |
| CSRF | 写请求强校验 | 不挂载 |
| 模块闸门 | 仅部分 handler 调用 | 路由级 `requireModuleMW("cert"/"deploy")` |

两条链**复用同一批 handler**，字段与业务语义一致，差异集中在认证、参数来源、模块闸门与错误码族四处。行号均来自工作区实读，`file://` 路径相对仓库根，密钥令牌一律脱敏为 `{占位}`；两条链的通用差异与错误码族说明见《API参考文档》，ACME 协议与部署器实现细节见《证书管理功能》各篇。

## 项目结构
| 层次 | 文件 | 关键行号 | 职责 |
| --- | --- | --- | --- |
| 控制台路由 | `main/internal/api/router.go` | `:185-203`、`:245`、`:276-279` | 证书账户/订单/部署/提供商/CNAME 共 22 条 |
| 控制台路由 | `main/internal/api/router.go` | `:266-271`、`:263-265` | 定时切换 6 条 + 任务观测 3 条 |
| 对外路由 | `main/internal/apiv1/routes_batch1.go` | `:30-66` | 证书 25 条（账户 4 + 提供商 1 + 订单 8 + 部署 5 + 部署账户 6 + 类型 1） |
| 对外路由 | `main/internal/apiv1/routes_batch1.go` | `:97-102` | 定时切换 6 条 |
| HTTP 层 | `main/internal/api/handler/cert.go` | `:71-1397` | 账户、订单、签发、下载、部署任务、提供商 |
| HTTP 层 | `main/internal/api/handler/cert_deploy.go` | `:29-324`、`:1039` | 部署账户 7 条 + 部署器类型目录 |
| HTTP 层 | `main/internal/api/handler/cert_deploy.go` | `:331-1031` | 已实现但**未注册路由**的部署族 handler（10 个） |
| HTTP 层 | `main/internal/api/handler/cname.go` | `:26-179` | CNAME 托管验证区 4 条 |
| HTTP 层 | `main/internal/api/handler/schedule.go` | `:44-259` | 定时切换 6 条 |
| 归属校验 | `main/internal/api/handler/cert_authz.go` | `:32-115` | 三类 owner 校验 + 三类列表 scope |
| 配额 | `main/internal/api/handler/quota.go` | `:182`、`:240`、`:269` | `certs` / `deploys` / `accounts` / `schedules` 四维 |
| 领域层 | `main/internal/cert/registry.go` | `:23-34` | 证书与部署双视图注册表 |
| 领域层 | `main/internal/cert/acme/acme.go` | `:577-977` | ACME v2 全流程 |
| 调度层 | `main/internal/service/task_runner.go` | `:89-454` | 续期扫描、部署执行、锁兜底 |
| 数据层 | `main/internal/models/models.go` | `:209-310` | 5 张证书表 + 定时任务表 |

## 核心组件
| 组件 | 位置 | 对外契约 | 说明 |
| --- | --- | --- | --- |
| 证书账户 handler | `cert.go:71` | `GET /api/cert/accounts?deploy=` | 同接口按 `deploy` 切证书/部署视图 |
| 证书订单 handler | `cert.go:354` | `POST /api/cert/orders` | 建单即校验标识与校验方式约束 |
| 签发触发 handler | `cert.go:454` | `POST /api/cert/orders/:id/process` | CAS 占位后异步执行，HTTP 立即返回 |
| 自动续期入口 | `cert.go:552` | 非 HTTP | 由 `service` 钩子回调，同 CAS 流程 |
| 部署任务 handler | `cert.go:1197` | `POST /api/cert/deploys` | 双重 owner 校验 + `local` 管理员限制 |
| 部署器目录 handler | `cert_deploy.go:1039` | 仅 v1 | 三类分组返回全部部署器表单元数据 |
| 提供商元数据 | `cert.go:1387` | `GET /api/cert/providers` | `{cert, deploy}` 两张动态表单定义 |
| CNAME 托管区 | `cname.go:26` 起 | `/api/cert/cnames` 族 | 写操作仅管理员 |
| 任务观测 | `dashboard.go:352` | `GET /api/system/task/status` | 自动续期与定时任务统计 |
| 配额闸门（v1） | `quota_gate.go:36` | 路由中间件 | 证书族复用 handler 内闸门，不重复挂 |

```mermaid
flowchart TB
    subgraph CON["控制台链"]
        C1["Auth + Permission"] --> C2["cert / cname / schedule handler"]
    end
    subgraph OPE["对外链"]
        O1["APIKeyAuth"] --> O2["paramAlias 桥接"] --> O3["AdaptResponse"]
    end
    subgraph DOM["领域层"]
        D1["cert 注册表"] --> D2["ACMEClient"]
        D1 --> D3["deploy 注册中心"]
    end
    S1["TaskRunner 每 5 分钟"] --> S2["续期钩子回调"]
    C2 --> DOM
    O3 --> C2
    S2 --> C2
    C2 --> DB[("cert_accounts / cert_orders / cert_deploys / cert_cnames")]
    D3 --> DB
```

**图表来源**
- `main/internal/api/router.go:95-104`
- `main/internal/apiv1/router.go:24-47`
- `main/internal/apiv1/routes_batch1.go:30-66`
- `main/internal/service/task_runner.go:89-113`

## 架构总览
一次完整的「申请 → 签发 → 部署」跨四个阶段，其中只有前两阶段由 HTTP 同步驱动：

```mermaid
sequenceDiagram
    participant C as 调用方
    participant H as cert.go handler
    participant P as ACMEProvider
    participant D as DNS Provider
    participant CA as ACME CA
    participant T as TaskRunner
    C->>H: POST /api/cert/accounts 建账户
    C->>H: POST /api/cert/orders 建订单
    H->>H: CheckQuota(certs) 与账户归属校验
    H-->>C: code=0 data.id（尚未签发）
    C->>H: POST /api/cert/orders/:id/process
    H->>H: CAS 占位 status=1
    H-->>C: code=0 证书申请已开始处理
    H->>P: CreateOrder(domains)
    P->>CA: newAccount 与 newOrder
    P-->>H: 待写入的挑战记录集合
    H->>D: EnsureChallengeRecord 写 TXT
    H->>CA: AuthOrder 后轮询 GetAuthStatus
    H->>P: FinalizeOrder
    P-->>H: FullChain 与 PrivateKey
    H->>H: status=3 写回订单
    C->>H: POST /api/cert/deploys/:id/process
    H->>D: deploy.GetProvider 并 Deploy
    T->>T: 每 5 分钟扫描到期订单
    T->>H: 钩子回调 TriggerCertOrderProcessing
```

**图表来源**
- `main/internal/api/handler/cert.go:354-822`
- `main/internal/api/handler/cert.go:1197-1385`
- `main/internal/cert/acme/acme.go:577-977`
- `main/internal/service/task_runner.go:89-210`

## 详细组件分析
### 证书账户与订单签发接口
证书账户是 CA 凭据容器。同一张 `cert_accounts` 表用 `is_deploy` 区分证书账户与部署账户，`GET /api/cert/accounts` 靠查询参数 `deploy=1` 切换视图。

| 方法 | 路径 | Handler | 归属校验 | 响应载荷 |
| --- | --- | --- | --- | --- |
| GET | `/api/cert/accounts` | `cert.go:71` | 列表 scope（`:87`） | `data` 为数组 |
| POST | `/api/cert/accounts` | `cert.go:152` | 强制 `uid = 当前`（`:162`） | `data.id` |
| POST | `/api/cert/accounts/:id` | `cert.go:179` | `requireCertAccountOwner` | 仅 `msg` |
| POST | `/api/cert/accounts/:id/delete` | `cert.go:206` | `requireCertAccountOwner` | 仅 `msg` |

请求体由 `CreateCertAccountRequest`（`cert.go:144-150`）定义，`type` / `name` / `config` 三项`binding:"required"`：

```json
{"type": "letsencrypt", "name": "LE 生产账户", "config": {"email": "ops@example.com"}, "remark": "主站点", "is_deploy": false}
```

```json
{ "code": 0, "msg": "创建成功", "data": { "id": 12 } }
```

列表逐行返回 `id` / `type` / `type_name` / `name` / `config` / `remark` / `is_deploy` / `created_at`（`cert.go:127-141`）。`type_name` 与 `/api/cert/providers` 同源解析（`:98-114`），两表都未登记时回退部署配置表。`config` 是**解密后的明文凭据串**：模型层的 `json:"-"` 被 handler 显式重组绕过。列表结果走 60 秒缓存（`:79`），缓存键按 UID 维度区分。

删除账户**不检查也不清理**关联订单与部署任务，因此先删账户会让后续签发落到「证书账户不存在」失败分支（`cert.go:481-485`）。

`POST /api/cert/orders` 只做参数落库，真正的 ACME 交互由 `process` 异步触发。

| 方法 | 路径 | Handler | 关键校验 | 同步性 |
| --- | --- | --- | --- | --- |
| GET | `/api/cert/orders` | `cert.go:217` | 非管理员按账户 UID 过滤（`:228`） | 读，**无分页** |
| POST | `/api/cert/orders` | `cert.go:354` | `CheckQuota(certs)`（`:356`）+ 账户归属 | 写 |
| POST | `/api/cert/orders/:id/process` | `cert.go:454` | 订单归属 + CAS（`:536-542`） | **异步** |
| POST | `/api/cert/orders/:id/delete` | `cert.go:990` | 订单归属 | 级联删 `cert_domains` |
| GET | `/api/cert/orders/:id/log` | `cert.go:1001` | 订单归属 | `data` 为**字符串** |
| GET | `/api/cert/orders/:id/detail` | `cert.go:1029` | 订单归属 | 含私钥 |
| GET | `/api/cert/orders/:id/download` | `cert.go:1068` | 订单归属（`:1073`） | 二进制 |
| POST | `/api/cert/orders/:id/auto` | `cert.go:1140` | 订单归属 | 取反 `is_auto` |

建单请求体（`CreateCertOrderRequest`，`cert.go:256-263`）：

```json
{"account_id": 12, "domains": ["example.com", "*.example.com", "198.51.100.7"], "key_type": "RSA", "key_size": "2048", "is_auto": true, "challenge_type": "dns-01"}
```

三项落库前约束：`validateCertOrderDomains`（`:265`）限长度 ≤253、禁空白、纯 IP 放行；`detectCertOrderKind`（`:287`）把结果写为 `order_kind` = `dns` / `ip` / `mixed`；`validateChallengeChoice`（`:321`）禁止纯 IP 用 `dns-01`、通配符用 `http-01`。`challenge_type` 先经 `normalizeCertChallengeType`（`:309`）归一，`dns` / `dns01` / `dns-01` 等价。

`process` 的幂等保护最强：`cert.go:535-542` 用一条带条件的 UPDATE 做 CAS，只有 `status != 1`或 `updated_at` 早于 5 分钟前才能占位，抢不到即返回 `{"code":-1,"msg":"订单正在处理中，请勿重复提交"}`。成功响应无进度信息，必须回查详情或日志接口。

详情响应字段清单来自 `cert.go:1047-1065` 的显式组装：

```json
{"code": 0, "data": {"id": 88, "domains": ["example.com", "*.example.com"], "order_kind": "dns", "challenge_type": "dns-01", "key_type": "RSA", "key_size": "2048", "status": 3, "issuer": "Let's Encrypt", "issue_time": "2026-10-10T14:02:11+08:00", "expire_time": "2027-01-08T14:02:11+08:00", "is_auto": true, "fullchain": "-----BEGIN CERTIFICATE-----\n{证书链占位}\n-----END CERTIFICATE-----\n", "private_key": "-----BEGIN PRIVATE KEY-----\n{私钥占位}\n-----END PRIVATE KEY-----\n", "dns_info": ""}}
```

下载接口用查询参数 `type`（**不是 `format`**，`cert.go:1070`）切换输出：`fullchain` → PEM（`:1098-1100`）；`key` → 私钥文件（`:1102-1104`）；缺省 → 内存组 ZIP（`:1106` 起）。未签发时返回 `{"code":-1,"msg":"证书尚未签发"}`（`:1079-1082`）。日志接口把 `error` / `dns` / `info` 三段拼成单个字符串返回（`:1012-1026`），无内容时为「暂无日志信息」。

**章节来源**
- `main/internal/api/handler/cert.go:60-214`
- `main/internal/api/handler/cert_authz.go:32-73`
- `main/internal/models/models.go:209-222`
- `main/internal/api/handler/cert.go:217-553`
- `main/internal/api/handler/cert.go:990-1158`
- `main/internal/api/handler/cert_authz.go:46-84`

### 订单状态机与自动续期
```mermaid
stateDiagram-v2
    [*] --> Pending
    Pending --> Processing: CreateCertOrder with is_auto
    Pending --> Processing: ProcessCertOrder CAS
    Processing --> Issued: FinalizeOrder ok
    Processing --> WaitingManual: runAutoAuth false
    WaitingManual --> Processing: manual reprocess
    Processing --> ErrProvider: status -1
    Processing --> ErrCreateOrder: status -2
    Processing --> ErrAuthTrigger: status -3
    Processing --> ErrAuthTimeout: status -4
    Processing --> ErrFinalize: status -5
    Processing --> ErrLockStale: status -6
    Issued --> Pending: checkCertRenewal reset
    ErrProvider --> Pending: manual retry
    ErrCreateOrder --> Pending: manual retry
    ErrAuthTrigger --> Pending: manual retry
    ErrAuthTimeout --> Pending: manual retry
    ErrFinalize --> Pending: manual retry
    ErrLockStale --> Pending: manual retry
```

**图表来源**
- `main/internal/api/handler/cert.go:443-819`
- `main/internal/service/task_runner.go:153-210`
- `main/internal/service/task_runner.go:421-454`

| 状态值 | 实际含义 | 赋值点 | 备注 |
| --- | --- | --- | --- |
| `0` | 待处理 / 续期复位 | 建单默认、`task_runner.go:198` | 模型注释 pending |
| `1` | 签发协程运行中 | `cert.go:443`、`:538`、`:622` | 前端按此值 5 秒轮询 |
| `2` | 已验证 | **无赋值点** | 不可观测 |
| `3` | 已签发 | `cert.go:810` | 唯一成功终态 |
| `4` | 已吊销 | **无赋值点** | 无吊销入口 |
| `-1` | 获取 provider 失败 | `cert.go:509`、`:518`、`:596` | 通知文案「购买证书失败」 |
| `-2` | `CreateOrder` 失败 | `cert.go:659` | 文案与语义一致 |
| `-3` | CA 验证触发失败 | `cert.go:756` | 文案「添加 DNS 失败」有偏差 |
| `-4` | 验证轮询超时 | `cert.go:782` | 至多 10 轮 × 10 秒 |
| `-5` | `FinalizeOrder` 失败 | `cert.go:796` | 文案「验证订单失败」有偏差 |
| `-6` | 订单锁超时被释放 | `task_runner.go:445` | 仅锁兜底路径产生 |
| `-7` | 签发失败 | **无赋值点** | 通知映射中的死条目 |

自动续期由后台心跳驱动，HTTP 只提供开关：

| 环节 | 位置 | 参数 |
| --- | --- | --- |
| 心跳周期 | `task_runner.go:89`、`:41` | 启动后 30 秒首跑，之后每 5 分钟 |
| 候选筛选 | `task_runner.go:153` | `is_auto AND status=3 AND expire_time < now + N 天` |
| 复位与派发 | `task_runner.go:198-210` | status→0、is_lock→false，再回调钩子 |
| 无 HTTP 续期入口 | `cert.go:552` | 与 `ProcessCertOrder` 同流程、同 CAS |
| 部署联动 | `task_runner.go:217` | 仅当 `issue_time` 变晚才重部署 |
| 部署重试 | `task_runner.go:244`、`:23` | 间隔 `{60,300,600,1800,3600}` 秒 |
| 锁兜底 | `task_runner.go:421` | 部署锁 10 分钟、订单锁 15 分钟，补发失败通知 |

**章节来源**
- `main/internal/api/handler/cert.go:443-819`
- `main/internal/service/task_runner.go:23-210`
- `main/internal/service/task_runner.go:421-454`
- `main/internal/models/models.go:224-253`

### 部署任务、部署账户与部署器类型接口
部署任务把「一张已签发证书」与「一个部署账户」绑定，控制台链共 5 条：

| 方法 | 路径 | Handler | 校验链 |
| --- | --- | --- | --- |
| GET | `/api/cert/deploys` | `cert.go:1160` | 列表按 `cert_deploys.uid` scope（`:1171`） |
| POST | `/api/cert/deploys` | `cert.go:1197` | `CheckQuota(deploys)` → 账户 owner → 订单 owner → local 限制 |
| POST | `/api/cert/deploys/:id` | `cert.go:1266` | 部署 owner + local 限制 |
| POST | `/api/cert/deploys/:id/delete` | `cert.go:1301` | 部署 owner |
| POST | `/api/cert/deploys/:id/process` | `cert.go:1315` | 部署 owner |

创建请求体（`cert.go:1202-1207`）的 `config` 为自由结构，字段名由部署器表单元数据决定：

```json
{"account_id": 21, "order_id": 88, "config": {"domainList": ["example.com"], "cert_name": "{证书名称占位}", "restart_cmd": "nginx -s reload"}, "remark": "主站 CDN"}
```

校验顺序本身即安全设计：`cert.go:1213-1222` 的注释说明归属校验必须**早于** `checkLocalDeployAllowed`（`:1250`），否则他人 `account_id` 可被探测账户类型形成布尔预言机。

`ProcessCertDeploy`（`:1315-1385`）是**同步**执行，直接调用`provider.Deploy(context.Background(), fullchain, privateKey, config)`，失败返回 HTTP 200 + `{"code":-1,"msg":"部署失败: ..."}`（`:1377`）。该路径**不写** `log_content` 与 `status`，只更新 `updated_at`（`:1382`）。带完整日志与状态回写的`DeployProcess`（`cert_deploy.go:723`）等 10 个 handler 已在 `cert_deploy.go:331-1031` 实现且带权限校验，但 `router.go` 与 `routes_batch1.go` **均未注册**，属当前不可达代码。

列表响应内嵌 `models.CertDeploy` 导出字段并追加 `account_name` 与 `order_domains`；`config` / `info` 因 `json:"-"`（`models.go:269`、`:283`）不出现在载荷中。

部署账户族 7 个 handler 定义在 `cert_deploy.go:29-324` 与 `:1039`，**只经对外链暴露**（`routes_batch1.go:60-66`），控制台链无注册。每条以`requireUserModule(c, "deploy")` 开场（`:30`、`:63`、`:119`）。

| v1 路径 | Handler | 请求体 | 响应 data |
| --- | --- | --- | --- |
| `/api/v1/cert/deploy-accounts/list` | `cert_deploy.go:29` | 无必填 | 数组：`id`/`type`/`type_name`/`class`/`name`/`remark`/`created_at` |
| `/api/v1/cert/deploy-accounts/detail` | `:217` | `id` | `{id,type,type_name,name,config,remark}` |
| `/api/v1/cert/deploy-accounts/create` | `:62` | `type`/`name`/`config` | `{id}` |
| `/api/v1/cert/deploy-accounts/update` | `:118` | `id`/`name`/`config`/`remark` | 无 |
| `/api/v1/cert/deploy-accounts/delete` | `:170` | `id` | 无 |
| `/api/v1/cert/deploy-accounts/check` | `:269` | `id` | 无，结果异步 |
| `/api/v1/cert/deploy-types` | `:1039` | 无 | `{class_names, types}` |

与证书账户删除不同，部署账户删除**会**检查关联任务并拒绝（`:187-193`）：

```json
{ "code": 4003, "request_id": "req_{占位}", "message": "该账户下还有 2 个部署任务，请先删除相关任务" }
```

创建走 `CheckQuota(c, "accounts")`（`:78`），并用 `deploy.GetDeployConfig(req.Type)` 校验类型（`:88-91`），未注册类型直接拒绝。连通性检测为异步：`:311-323` 用 `utils.SafeGo` 起 30 秒超时的`provider.Check(ctx)`，HTTP 立即回「提交成功」，结果只写进程日志，**无回查端点**。

`DeployTypes` 按 `deploy/config.go:7-9` 的三个分类常量分组：

| class | 分类名 | 典型部署器 | 认证要素 |
| --- | --- | --- | --- |
| `1` | 自建系统 | 宝塔、NPM、Kangle、SafeLine、1Panel | 面板 API Key / 账号密码 |
| `2` | 云服务商 | 阿里云 CDN/DCDN/ESA、腾讯云 CDN/TEO、华为云、AWS | AK/SK 或 SecretId/Key |
| `3` | 服务器 | `ssh`、`ftp`、`local`、S3 兼容存储 | 密码或私钥、进程权限 |

每项含 `type`/`name`/`icon`/`desc`/`note`/`inputs`/`task_inputs`/`task_note` 八个键（`cert_deploy.go:1052-1061`）：`inputs` 描述**账户级**凭据字段，`task_inputs` 描述**任务级**参数（目标路径、证书名、重启命令），两者分别渲染成前端动态表单。

**章节来源**
- `main/internal/api/handler/cert.go:1160-1385`
- `main/internal/api/handler/cert_authz.go:86-115`
- `main/internal/api/handler/cert_deploy.go:331-1031`
- `main/internal/models/models.go:263-288`
- `main/internal/api/handler/cert_deploy.go:29-324`
- `main/internal/api/handler/cert_deploy.go:1039-1069`
- `main/internal/apiv1/routes_batch1.go:60-66`
- `main/internal/cert/deploy/config.go:6-49`

### 提供商元数据接口
`GET /api/cert/providers`（`cert.go:1387-1397`）是证书域动态表单的唯一来源，返回两张视图：

| 视图 | 来源函数 | 判据 | 用途 |
| --- | --- | --- | --- |
| `cert` | `cert.GetCertProviderConfigs()` | `IsDeploy == false` | 证书账户表单与签发渠道下拉 |
| `deploy` | `deploy.MergeDeployProviderConfigsForAPI(cert.GetDeployProviderConfigs())` | `IsDeploy == true` 后与 `AllDeployConfigs` 合并 | 部署账户与部署任务表单 |

元素结构为 `ProviderConfig`（`cert/interface.go:82-92`）：`type` / `name` / `icon` / `note` / `config` / `deploy_config` / `deploy_note` / `cname` / `is_deploy`。字段项 `ConfigField`（`:95-109`）的 `show` / `validator` / `min` / `max` 是前端条件渲染与客户端校验依据。注册表的短路规则值得注意：`registry.go:31-33` 在 `config.Type == ""` 且已有配置时**只补工厂、不覆盖表单**，因此子包 `init` 与 `providers.go` 的注册顺序会影响最终表单项。

**章节来源**
- `main/internal/api/handler/cert.go:1387-1397`
- `main/internal/cert/interface.go:82-115`
- `main/internal/cert/registry.go:23-34`

### CNAME 托管验证接口
CNAME 托管区把签发域的 DNS-01 验证委托到另一已接入域名，共 4 条：

| 方法 | 路径 | Handler | 权限 | 备注 |
| --- | --- | --- | --- | --- |
| GET | `/api/cert/cnames` | `cname.go:26` | `requireUserModule("cert")` | 分页默认 20、上限 200（`:39-41`） |
| POST | `/api/cert/cnames` | `:64` | `checkAdmin`（`:65`） | 同域名唯一（`:77-81`） |
| POST | `/api/cert/cnames/:id/delete` | `:115` | `checkAdmin` | body `id` 优先，回落路径参数（`:127-129`） |
| POST | `/api/cert/cnames/:id/verify` | `:154` | `checkAdmin` | **不做真实 DNS 校验** |

创建请求体三字段均 `binding:"required"`（`:52-56`）：

```json
{ "domain": "example.com", "did": "17", "rr": "_acme-challenge.example.com" }
```

`did` 是字符串形态的域名 ID，`ParseUint` 失败即回「无效的域名ID」（`:83-87`）。列表响应 `{total, list}`，元素为 `models.CertCNAME`：`id`/`domain`/`did`/`rr`/`status`/`created_at`（`models.go:290-297`）。

`VerifyCertCNAME` 只有两行有效逻辑（`:177-178`）：读到记录后直接把 `status` 置 1 并回「验证成功」，不发起任何 DNS 查询。**不得把该接口成功当作 CNAME 已生效的证据**——真实生效只在签发阶段暴露。

**章节来源**
- `main/internal/api/handler/cname.go:16-179`
- `main/internal/models/models.go:290-297`

### 定时切换与任务观测接口
定时切换与证书无直接关系，但归入同一功能域。控制台 6 条（`router.go:266-271`）、v1 6 条（`routes_batch1.go:97-102`），模块键均为 `domain`。

| 方法 | 路径 | Handler | 备注 |
| --- | --- | --- | --- |
| GET | `/api/schedule/tasks` | `schedule.go:44` | 分页默认 20，无上限收敛 |
| POST | `/api/schedule/tasks` | `:81` | `CheckQuota(schedules)` 先于模块判定；策略重复即拒（`:102-106`） |
| POST | `/api/schedule/tasks/batch` | `:223` | `ids[]` + `act` |
| POST | `/api/schedule/tasks/:id` | `:122` | 需 `:id` |
| POST | `/api/schedule/tasks/:id/delete` | `:175` | 空 ID 显式拒绝（`:181-184`）+ `ensureDomainWrite` |
| POST | `/api/schedule/tasks/:id/toggle` | `:197` | `?active` 走查询串（`:216`） |

字段命名沿用旧式无分隔小写（`scheduleTaskPayload`，`schedule.go:16-30`）：

```json
{"did": 17, "rr": "www", "recordid": "{云端记录ID占位}", "type": 0, "cycle": 1, "switchtype": 1, "switchdate": "2026-10-20", "switchtime": "02:30", "value": "198.51.100.9", "line": "默认", "remark": "夜间切换", "recordinfo": "", "active": true}
```

v1 侧两条限制：`tasks/toggle` 的 `active` 取自 `c.Query`，而子组只挂 `paramAlias`（不搬运查询串），因此 body 传 `active` **无效**，实际按「取反当前值」处理；`tasks/list` 与 `tasks/create` 未挂桥接子组，经 handler 内 `BindDecryptedData` 读 body，参数正常送达。

| 方法 | 路径 | Handler | 响应 data 键 |
| --- | --- | --- | --- |
| GET | `/api/system/task/status` | `dashboard.go:352` | `schedule{total,active,last_time}`、`cert_auto`、`domain_notice`、`record_sync` |
| GET | `/api/system/cron` | `:382` | cron 开关与下次执行时间集合 |
| POST | `/api/system/cron` | `:483` | 无（仅 `msg`） |
| GET | `/api/dashboard/stats` | `:124` | 含 `certorder_status_*` / `certdeploy_status_*` 计数 |

`GetTaskStatus` 对各表直接 `Count`（`:353-380`）：`cert_auto` 即 `cert_orders` 中 `is_auto = true` 的行数，`schedule.last_time` 读 `sys_configs` 的 `schedule_time` 键。该接口在 v1 侧**未迁移**。

**章节来源**
- `main/internal/api/handler/schedule.go:16-259`
- `main/internal/apiv1/routes_batch1.go:91-103`
- `main/internal/apiv1/bridge.go:36-119`
- `main/internal/api/handler/dashboard.go:124`
- `main/internal/api/handler/dashboard.go:352-380`
- `main/internal/api/handler/dashboard.go:382-483`

### 权限与配额矩阵
证书域鉴权分四层，这是同一动作在不同入口给出不同错误码的根因：

| 层 | 控制台实现 | v1 实现 | 失败输出 |
| --- | --- | --- | --- |
| 身份 | `middleware.Auth` | `apiv1/auth.go:44` `APIKeyAuth` | 控制台 401 / v1 `1001`-`1006` |
| 模块 | 仅 CNAME 列表与部署账户族调 `requireUserModule` | 路由级 `requireModuleMW`（`domain.go:75`） | v1 `3001` |
| 归属 | `cert_authz.go:32/46/96` 三函数 + 三 scope | 同函数复用 | 控制台 `code:-1`，映射后 `3002` |
| 配额 | `CheckQuota`（`quota.go:182`） | 复用 handler 内闸门 | 控制台文案，v1 折叠为 `2001` |

| 入口 | 归属校验 | 模块校验 | 配额校验 |
| --- | --- | --- | --- |
| `GetCertAccounts` | 列表 scope | 无 | 无 |
| `CreateCertAccount` | 强制 `uid = 当前` | 无 | 无 |
| `Update/DeleteCertAccount` | 账户 owner | 无 | 无 |
| `GetCertOrders` | UID 过滤 | 无 | 无 |
| `CreateCertOrder` | 账户 owner | 无 | `certs` |
| `ProcessCertOrder` | 订单 owner + CAS | 无 | 无 |
| `DownloadCertOrder` | 订单 owner | 无 | 无 |
| `CreateCertDeploy` | 账户 + 订单 owner | 无 | `deploys` |
| `ProcessCertDeploy` | 部署 owner | 无 | 无 |
| `GetCertCNAMEs` | 无属主概念 | `cert` | 无 |
| `Create/Delete/VerifyCertCNAME` | `checkAdmin` | 无 | 无 |
| `DeployAccount*` 7 条 | 逐条 UID 比对 | `deploy` | 仅 create 走 `accounts` |

配额四维度与统计口径（`quota.go:240-312`）：

| 维度键 | 中文名 | 归属判据 | 触发接口 |
| --- | --- | --- | --- |
| `certs` | SSL 证书 | `cert_orders` JOIN `cert_accounts` 按账户 `uid` | `CreateCertOrder` |
| `deploys` | 证书部署任务 | `cert_deploys.uid` | `CreateCertDeploy` |
| `accounts` | DNS 账户（部署账户复用） | `accounts.uid` | `DeployAccountAdd` |
| `schedules` | 定时切换任务 | 任务 JOIN 域名 JOIN 账户按 `accounts.uid` | `CreateScheduleTask` |

取值约定 `-1` 不限、`0` 禁止、正数为上限（`quota.go:25-27`）。配额**只挡创建**，重复签发不受限，反复 `process` 同一订单可无限消耗 CA 侧配额。

**章节来源**
- `main/internal/api/handler/cert_authz.go:30-115`
- `main/internal/api/handler/quota.go:25-312`
- `main/internal/apiv1/quota_gate.go:9-47`
- `main/internal/apiv1/auth.go:44-114`

## 依赖关系分析
```mermaid
erDiagram
    USERS ||--o{ CERT_ACCOUNTS : owns
    CERT_ACCOUNTS ||--o{ CERT_ORDERS : issues
    CERT_ACCOUNTS ||--o{ CERT_DEPLOYS : target
    CERT_ORDERS ||--o{ CERT_DOMAINS : contains
    CERT_ORDERS ||--o{ CERT_DEPLOYS : deployed_by
    CERT_ORDERS ||--o{ CERT_LOGS : logs
    CERT_DOMAINS ||--o{ CERT_CNAMES : delegates
```

**图表来源**
- `main/internal/models/models.go:209-300`
- `main/internal/api/handler/cert_authz.go:20-23`

### 数据表、加密边界与中间件依赖
| 表 | 模型 | 加密列 | 明文列 | 关键索引 |
| --- | --- | --- | --- | --- |
| `cert_accounts` | `models.go:209` | `config`、`ext`（`hooks.go:35-36`） | 其余 | `uid`、软删 `deleted_at` |
| `cert_orders` | `models.go:224` | `private_key`、`info`（`hooks.go:38-44`） | `fullchain`、`dns` | `idx_cert_orders_auto_scan(is_auto,status,expire_time)` |
| `cert_domains` | `models.go:255` | 无 | 全部 | `oid` |
| `cert_deploys` | `models.go:263` | `config`、`info`（`hooks.go:46-52`） | `log_content` | `idx_cert_deploys_scan(active,status,is_lock)` |
| `cert_cnames` | `models.go:290` | 无 | 全部 | 无索引 |
| `cert_logs` | 位于 LogDB | 无 | 全部 | `idx_cert_logs_order_id` |

| 差异点 | 控制台 | v1 | 依据 |
| --- | --- | --- | --- |
| CSRF | 写请求强校验 | 不挂载 | `apiv1/router.go:24-34` |
| CORS 位置 | `RequestTrace` 之前 | `RequestTrace` 之后 | `router.go:44` 对比 `apiv1/router.go:27-29` |
| 参数来源 | `c.Param` + `c.Query` + body | 只有 body，靠 `paramAlias` 回填 | `bridge.go:78-103` |
| 载荷加密 | `aes_key` 存在时加密 | 恒明文 | `sign.go:142-158` 按 `auth_type` 短路 |
| 响应改写 | 无 | `AdaptResponse` 包裹后续写出 | `adapt.go:32` |

| 外部依赖 | 用途 | 失败表现 |
| --- | --- | --- |
| ACME CA 目录 | `acme.go:288` `getDirectory` | `status=-2` |
| DNS 服务商 API | 挑战记录写入 | `status=-3` / `-4`，`dns_info` 有明细 |
| 公共 DNS（UDP 53） | 签发前 TXT 预检 | 回退 DoH，不阻断 |
| 部署器上游 API | `provider.Deploy` | 同步 `code:-1`，异步走重试 |
| Redis（可选） | 请求日志与缓存 | 3 秒 Ping 超时降级内存 |

**章节来源**
- `main/internal/models/models.go:209-300`
- `main/internal/models/hooks.go:35-52`
- `main/internal/apiv1/router.go:24-47`
- `main/internal/apiv1/adapt.go:32`
- `main/internal/apiv1/bridge.go:36-119`
- `main/internal/api/middleware/sign.go:142-158`
- `main/internal/api/router.go:44-104`

## 性能考虑
| 现象 | 位置 | 影响 | 建议 |
| --- | --- | --- | --- |
| 订单列表无分页 | `cert.go:231` 直接 `Find` | 全表连同 `fullchain` 一次下发，响应体线性膨胀 | 客户端增量缓存，勿轮询全表 |
| 部署列表无分页 | `cert.go:1172` | 同上，且每行追加 `order_domains` 需二次查询 | 同上 |
| 账户列表 60 秒缓存 | `cert.go:79` | 改账户后短暂读到旧视图 | 写路径已调 `BustCertAccountsList` |
| 签发协程无上下文超时 | `cert.go` 异步入口用 `context.Background()` | ACME 退避可达小时级，协程存活不可控 | 以状态轮询代替长连接等待 |
| 验证轮询固定 10 轮 × 10 秒 | `cert.go:765-790` | 慢传播域 100 秒后误判 `-4` | 失败后重触发 `process` 而非重建订单 |
| 订单日志整串重写 | `cert.go:626-631` | 每条日志读改写全量 `info`；`fmt.Printf` 直写 stdout 绕过轮转 | 避免高频回调追加日志 |
| CAS 依赖 `updated_at` | `cert.go:535-538` | 5 分钟内的 `status=1` 订单被硬拒重复提交 | 排障先等超时再重触发 |
| 下载内存组 ZIP | `cert.go:1106` 起 | 大证书链瞬时占用双倍内存 | 高并发下载需限流 |

全仓非测试代码的入口限流只有登录失败计数、`quicklogin` 与邮箱验证码三处，**`/api/v1/cert/*` 上没有任何限流中间件**，配额闸门只约束创建数量、不约束请求速率。调用方须自行实现指数退避与幂等控制。

**章节来源**
- `main/internal/api/handler/cert.go:79`
- `main/internal/api/handler/cert.go:217-253`
- `main/internal/api/handler/cert.go:535-542`
- `main/internal/api/handler/cert.go:626-631`
- `main/internal/api/handler/cert.go:765-790`
- `main/internal/api/handler/quota.go:182-206`

## 故障排查指南
### 错误码对照与常见问题定位
控制台链只有 `0` / `-1` 两值（HTTP 另有 400/401/403/404/429/500）；v1 码来自分段表（`errors.go:18-63`），证书域相关子集如下：

| 码 | 常量 | 默认文案 | HTTP | 证书域触发面 |
| --- | --- | --- | --- | --- |
| `1001` | `CodeUnauthorized` | 缺少 API 凭据 | 401 | 三个签名头任一为空 |
| `1002` | `CodeTimestampInvalid` | 请求时间戳已失效 | 401 | 超出 ±5 分钟窗口（`auth.go:37`、`:69`） |
| `1003` | `CodeSignMismatch` | 签名验证失败 | 401 | 路径或方法拼写不一致 |
| `1004` | `CodeAPIKeyMissing` | 该用户未启用 API 访问 | 401 | `users.is_api` 为假 |
| `2001` | `CodeParamInvalid` | 参数不合法 | 400 | **未命中映射规则的兜底**，含配额超限 |
| `2002` | `CodeParamMissing` | 缺少必填参数 | 400 | `binding:"required"` 前置判定 |
| `2003` | `CodeBodyUnreadable` | 请求体解析失败 | 400 | JSON 非法或超 64 KiB 被截断 |
| `3001` | `CodeModuleDenied` | 无该功能模块的访问权限 | 400 | 缺 `cert` 或 `deploy` 授权 |
| `3002` | `CodeDomainDenied` | 无该域名的操作权限 | 400 | 控制台所有权失败折叠至此 |
| `3005` | `CodeAdminRequired` | 该接口仅管理员可用 | 400 | v1 `adminOnly` |
| `4001` | `CodeResourceNotFound` | 目标资源不存在 | 404 | 「订单不存在」落此而非 `4005` |
| `4003` | `CodeAccountNotFound` | DNS 账户不存在 | 404 | 「证书账户不存在」命中「账户」 |
| `4006` | `CodeTaskNotFound` | 任务不存在 | 404 | 「部署任务不存在」 |
| `4007` | `CodeResourceConflict` | 资源已存在 | 409 | 「该域名CNAME代理已存在」 |
| `5003` | `CodeProviderUnsupported` | 当前服务商不支持该能力 | 502 | 「证书提供商未实现」 |
| `5006` | `CodeUpstreamTimeout` | 上游服务响应超时 | 504 | 文案含「超时」 |

`4005 CodeCertOrderNotFound` 与 `5005 CodeCertIssueFailed` 在当前代码中**没有可达触发路径**（证书族失败都经 `mapConsoleError` 按文案重映射，`adapt.go:120`），属预留码，客户端不得假设会收到。

| 症状 | 判据 | 根因位置 | 处置 |
| --- | --- | --- | --- |
| 「订单正在处理中，请勿重复提交」 | HTTP 200 | `cert.go:539-541` CAS 抢锁失败 | 等 `updated_at` 超 5 分钟再触发 |
| `status` 长期为 `1` | `cert.go:538` 置位后协程未落地 | 重启前 `resetStuckOrders` 未执行 | 重启进程复位，或超时后重触发 |
| `status=-3` 但文案说 DNS 失败 | 文案与赋值点错位 | `cert.go:756` 实为 CA 验证触发失败 | 查 `dns_info` 与服务商 API 配额 |
| `status=-4` | 10 轮轮询超时 | `cert.go:782` | 确认 TXT 传播，必要时换 `dns-01` |
| 详情无 `private_key` | 字段 `omitempty` | `models.go:247` | 未签发时该键整体缺失，非权限问题 |
| 下载得到 ZIP | `?type` 未命中分支 | `cert.go:1096-1105` | 参数名是 `type`，不是 `format` |
| v1 传 `deploy` 过滤无效 | body 未搬运为 query | `routes_batch1.go:30` 只挂 `paramAlias` | 改用查询串或本地过滤 |
| v1 返回 `2001` 但语义是权限 | 文案不在映射表内 | `adapt.go:120` 兜底 | 以 `message` 为辅助判据 |
| 部署账户 `check` 无结果 | 异步且无回查端点 | `cert_deploy.go:311-323` | 查进程日志 `[DeployAccountCheck]` |
| 删除证书账户后订单全失败 | 无级联检查 | `cert.go:206` 起仅软删 | 先删订单再删账户 |
| `local` 部署被拒 | 非管理员 | `cert.go:1250-1263` | 改用 SSH 或提权 |

v1 调用样例（签名串为 `uid`、`timestamp`、`METHOD`、`PATH` 四段换行拼接，`PATH` 含 `/api/v1`前缀且不含查询串，见 `auth.go:93`）：

```bash
UID=3
API_PATH=/api/v1/cert/orders/list
TS=$(date +%s)
MSG="${UID}"$'\n'"${TS}"$'\n'"POST"$'\n'"${API_PATH}"
SIGN=$(printf '%s' "$MSG" | openssl dgst -sha256 -hmac "{API_KEY占位}" -hex | awk '{print $2}')
curl -sS -X POST "https://{部署域名占位}${API_PATH}" \
  -H "Content-Type: application/json" \
  -H "X-API-UID: ${UID}" -H "X-API-Timestamp: ${TS}" -H "X-API-Sign: ${SIGN}" -d '{}'
```

签名**不绑定请求体**、不含 nonce，±5 分钟窗口内更换 body 的签名依然有效，集成方须自行保证传输层安全与请求体完整性。

**章节来源**
- `main/internal/apiv1/errors.go:18-63`
- `main/internal/apiv1/adapt.go:120`
- `main/internal/apiv1/auth.go:37-114`
- `main/internal/apiv1/auth.go:93`
- `main/internal/apiv1/routes_batch1.go:30`
- `main/internal/api/handler/cert.go:535-541`
- `main/internal/api/handler/cert.go:756-790`
- `main/internal/api/handler/cert.go:1096-1105`
- `main/internal/api/handler/cert_deploy.go:311-323`

## 结论
1. **一条 handler 链、两个门面**：控制台 22 条 + v1 25 条证书接口复用同一批函数，差异全在中间件层，
   字段名与业务语义可共用同一份模型
2. **同步接口只有两个**：`CreateCertOrder` 与 `ProcessCertDeploy`；签发触发与部署账户连通性检测均为异步，
   后者甚至无回查端点，调用方必须以状态回读为唯一真相来源
3. **权限分四层、错误码分两族**：同一含义可能同时出现 `3002` 与 `3005`，客户端分支需辅以 `message`
4. **敏感数据出口多**：账户列表、订单列表、订单详情、下载四处都会带出凭据或私钥，
   其中订单列表无分页地内联 `fullchain`，集成时应按需只读详情接口

已知纸面能力（集成规划应排除）：吊销与取消在 `cert.Provider` 接口已实现但无 HTTP 入口（`acme.go:1032`、`:1050`）；`cert_deploy.go:331-1031` 的 10 个部署管理 handler 未注册路由。

## 附录
### 控制台证书域端点全表
| Method | Path | Handler | 配额 |
| --- | --- | --- | --- |
| GET | `/api/cert/accounts` | `cert.go:71` | 无 |
| POST | `/api/cert/accounts` | `cert.go:152` | 无 |
| POST | `/api/cert/accounts/:id` | `cert.go:179` | 无 |
| POST | `/api/cert/accounts/:id/delete` | `cert.go:206` | 无 |
| GET | `/api/cert/orders` | `cert.go:217` | 无 |
| POST | `/api/cert/orders` | `cert.go:354` | `certs` |
| POST | `/api/cert/orders/:id/process` | `cert.go:454` | 无 |
| POST | `/api/cert/orders/:id/delete` | `cert.go:990` | 无 |
| GET | `/api/cert/orders/:id/log` | `cert.go:1001` | 无 |
| GET | `/api/cert/orders/:id/detail` | `cert.go:1029` | 无 |
| GET | `/api/cert/orders/:id/download` | `cert.go:1068` | 无 |
| POST | `/api/cert/orders/:id/auto` | `cert.go:1140` | 无 |
| GET | `/api/cert/deploys` | `cert.go:1160` | 无 |
| POST | `/api/cert/deploys` | `cert.go:1197` | `deploys` |
| POST | `/api/cert/deploys/:id` | `cert.go:1266` | 无 |
| POST | `/api/cert/deploys/:id/delete` | `cert.go:1301` | 无 |
| POST | `/api/cert/deploys/:id/process` | `cert.go:1315` | 无 |
| GET | `/api/cert/providers` | `cert.go:1387` | 无 |
| GET | `/api/cert/cnames` | `cname.go:26` | 无 |
| POST | `/api/cert/cnames` | `cname.go:64` | 无 |
| POST | `/api/cert/cnames/:id/delete` | `cname.go:115` | 无 |
| POST | `/api/cert/cnames/:id/verify` | `cname.go:154` | 无 |
| GET | `/api/schedule/tasks` | `schedule.go:44` | 无 |
| POST | `/api/schedule/tasks` | `schedule.go:81` | `schedules` |
| POST | `/api/schedule/tasks/batch` | `schedule.go:223` | 无 |
| POST | `/api/schedule/tasks/:id` | `schedule.go:122` | 无 |
| POST | `/api/schedule/tasks/:id/delete` | `schedule.go:175` | 无 |
| POST | `/api/schedule/tasks/:id/toggle` | `schedule.go:197` | 无 |
| GET | `/api/system/task/status` | `dashboard.go:352` | 无 |
| GET | `/api/system/cron` | `dashboard.go:382` | 无 |
| POST | `/api/system/cron` | `dashboard.go:483` | 无 |
| GET | `/api/dashboard/stats` | `dashboard.go:124` | 无 |

**章节来源**
- `main/internal/api/router.go:185-203`
- `main/internal/api/router.go:245`
- `main/internal/api/router.go:263-279`

### 对外 v1 证书域端点全表
| Path | Handler | 模块 | 桥接键 | 备注 |
| --- | --- | --- | --- | --- |
| `/api/v1/cert/accounts/list` | `cert.go:71` | `cert` | `account_id` | `deploy` 过滤需走查询串 |
| `/api/v1/cert/accounts/create` | `cert.go:152` | `cert` | 无 | 只读 body |
| `/api/v1/cert/accounts/update` | `cert.go:179` | `cert` | `account_id` |  |
| `/api/v1/cert/accounts/delete` | `cert.go:206` | `cert` | `account_id` |  |
| `/api/v1/cert/providers` | `cert.go:1387` | `cert` | 无 |  |
| `/api/v1/cert/orders/list` | `cert.go:217` | `cert` | `order_id` | 无分页 |
| `/api/v1/cert/orders/create` | `cert.go:354` | `cert` | 无 | 配额 `certs` |
| `/api/v1/cert/orders/process` | `cert.go:454` | `cert` | `order_id` | 异步 |
| `/api/v1/cert/orders/delete` | `cert.go:990` | `cert` | `order_id` |  |
| `/api/v1/cert/orders/log` | `cert.go:1001` | `cert` | `order_id` | `data` 为字符串 |
| `/api/v1/cert/orders/detail` | `cert.go:1029` | `cert` | `order_id` | 含私钥 |
| `/api/v1/cert/orders/download` | `cert.go:1068` | `cert` | `order_id` | 二进制原样透出 |
| `/api/v1/cert/orders/auto` | `cert.go:1140` | `cert` | `order_id` |  |
| `/api/v1/cert/deploys/list` | `cert.go:1160` | `deploy` | `deploy_id` | 无分页 |
| `/api/v1/cert/deploys/create` | `cert.go:1197` | `deploy` | 无 | 配额 `deploys` |
| `/api/v1/cert/deploys/update` | `cert.go:1266` | `deploy` | `deploy_id` |  |
| `/api/v1/cert/deploys/delete` | `cert.go:1301` | `deploy` | `deploy_id` |  |
| `/api/v1/cert/deploys/process` | `cert.go:1315` | `deploy` | `deploy_id` | 同步 |
| `/api/v1/cert/deploy-accounts/list` | `cert_deploy.go:29` | `deploy` | 无 | 仅 v1 |
| `/api/v1/cert/deploy-accounts/detail` | `cert_deploy.go:217` | `deploy` | 无 |  |
| `/api/v1/cert/deploy-accounts/create` | `cert_deploy.go:62` | `deploy` | 无 | 配额 `accounts` |
| `/api/v1/cert/deploy-accounts/update` | `cert_deploy.go:118` | `deploy` | 无 |  |
| `/api/v1/cert/deploy-accounts/delete` | `cert_deploy.go:170` | `deploy` | 无 | 有关联任务则拒 |
| `/api/v1/cert/deploy-accounts/check` | `cert_deploy.go:269` | `deploy` | 无 | 异步无回查 |
| `/api/v1/cert/deploy-types` | `cert_deploy.go:1039` | `deploy` | 无 | 仅 v1 |
| `/api/v1/schedule/tasks/list` | `schedule.go:44` | `domain` | 无 |  |
| `/api/v1/schedule/tasks/create` | `schedule.go:81` | `domain` | 无 |  |
| `/api/v1/schedule/tasks/update` | `schedule.go:122` | `domain` | `id` |  |
| `/api/v1/schedule/tasks/delete` | `schedule.go:175` | `domain` | `id` |  |
| `/api/v1/schedule/tasks/toggle` | `schedule.go:197` | `domain` | `id` | body `active` 无效 |
| `/api/v1/schedule/tasks/batch` | `schedule.go:223` | `domain` | 无 |  |

**章节来源**
- `main/internal/apiv1/routes_batch1.go:30-66`
- `main/internal/apiv1/routes_batch1.go:91-102`
- `main/internal/apiv1/bridge.go:36-45`

### 口径路径映射与前端封装差异
集成请求必须使用实际路由，下表把常用简写映射到工作区路径。

| 简写 | 实际路径 | 说明 |
| --- | --- | --- |
| `/api/certs` | `GET/POST /api/cert/orders` | 订单列表与建单 |
| `/api/certs/:id` | `GET /api/cert/orders/:id/detail` | 无裸 `:id` 详情路由 |
| `/api/certs/apply` | `POST /api/cert/orders` + `.../process` | 拆为建单与触发两步 |
| `/api/certs/renew` | `POST /api/cert/orders/:id/auto` + `process` | 无独立 renew 端点 |
| `/api/cert-accounts` | `/api/cert/accounts`；v1 `/api/v1/cert/deploy-accounts/*` | 同表按 `is_deploy` 分视图 |
| `/api/deploy-targets` | `/api/cert/providers`、`/api/v1/cert/deploy-types` | 部署器元数据 |
| `/api/deploy-tasks` | `/api/cert/deploys` 族 | 控制台 5 条 |
| `/api/deploy-accounts` | `/api/v1/cert/deploy-accounts/*` | 控制台链未注册 |
| `/api/schedules` | `/api/schedule/tasks` 族 | 单数 `schedule` |

`web/lib/api.ts:609` 起的 `certApi` 有四处与后端不同形，前端已兜底但封装具误导性：

| 封装方法 | 期望 | 后端实际 | 结论 |
| --- | --- | --- | --- |
| `getOrders` | `{total, list}` | `cert.go:253` 返回数组 | 前端双兼容，`total` 恒缺 |
| `getOrderLog` | `{log: string}` | `cert.go:1026` `data` 为字符串 | 前端有 `typeof` 兜底 |
| `downloadOrder(id, format)` | 查询串 `format` | `cert.go:1070` 读 `type` | 参数名不一致 |
| `processOrder(id, reset)` | body `{reset}` | `ProcessCertOrder` 不读该字段 | 传参无效 |
| `toggleOrderAuto(id, is_auto)` | body `{is_auto}` | `cert.go:1150` 取反当前值 | 传参无效 |

**章节来源**
- `main/internal/api/router.go:185-279`
- `main/internal/apiv1/routes_batch1.go:30-102`
- `web/lib/api.ts:609-663`
- `main/internal/api/handler/cert.go:253`
- `main/internal/api/handler/cert.go:1026`
- `main/internal/api/handler/cert.go:1150`

### 术语表
| 术语 | 含义 |
| --- | --- |
| 证书账户 | `cert_accounts` 中 `is_deploy = false` 的行，承载 CA 凭据 |
| 部署账户 | 同表 `is_deploy = true` 的行，承载目标平台凭据 |
| 签发渠道 | `cert` 注册表中非部署项，如 `letsencrypt`、`aliyun_cert`、`tencent` |
| 部署器 | `deploy.AllDeployConfigs` 登记的实现键，如 `aliyun_cdn`、`btpanel`、`ssh` |
| CAS 占位 | 用带条件的 UPDATE 影响行数判断是否抢到签发权 |
| 参数桥接 | v1 把 body 标量回填为路径参数的中间件 |
| 折叠码 | 控制台中文文案经 `mapConsoleError` 映射后的 v1 分段码 |

**章节来源**
- `main/internal/models/models.go:209-300`
- `main/internal/cert/deploy/config.go:6-33`
- `main/internal/apiv1/bridge.go:36-119`
- `main/internal/apiv1/adapt.go:120`
