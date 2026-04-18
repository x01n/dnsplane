# DNSPlane 完整变更日志

> 本文档按时间倒序记录从项目初始化至今的所有提交：更新内容、修复内容、提交时间。
>
> **版本约定**（从 v1.0.0 起执行）：
> - 🟥 漏洞修复 → **小版本号** patch +1（例 `1.0.3 → 1.0.4`）
> - 🟩 新增功能 → **大版本号** major +1（例 `1.x.x → 2.0.0`）
>
> 未版本化阶段（项目初始化至 v1.0.0 前）历史提交见文末"早期未版本化历史"小节。

---

## v1.0.10 — 修复 domainId/subId 类型，覆盖 v1.0.9 漏掉的命名

- **提交**：_待分配_
- **时间**：2026-04-18
- **类型**：🟥 Fix（类型）

**[问题]**
v1.0.9 只覆盖 `(id: number)` 模式，但 `updateRecord/deleteRecord/setRecordStatus/lookup`
等签名用的是 `(domainId: number, recordId: string, ...)`，命名不同。
CI 跑到下一个文件继续报：
```
./app/(dashboard)/dashboard/domains/[id]/client.tsx:371:40
Type error: Argument of type 'string' is not assignable to parameter of type 'number'.
const res = selectedRecord
  ? await domainApi.updateRecord(domainId, selectedRecord.RecordId, data)
```

**[修复]**
`lib/api.ts` 中 `domainId: number` → `domainId: number | string` 共 4 处。
`recordId: string` 不变（已是 string）。

---

## v1.0.9 — 修复关 ignoreBuildErrors 后暴露的 id: number 类型不匹配

- **提交**：`301421f`
- **时间**：2026-04-18
- **类型**：🟥 Fix（类型严格化）

**[问题]**
v1.0.8 后 CI 跑到新错误：
```
./app/(dashboard)/dashboard/domains/[id]/client.tsx:247:44
Type error: Argument of type 'string' is not assignable to parameter of type 'number'.
const res = await domainApi.getLines(domainId)
```
URL 路径参数 `domainId` 在 client 侧来自 `useParams`（永远是 `string`），
但 `lib/api.ts` 中 47 处 API 方法签名声明 `id: number`。
JavaScript 模板串字符串拼接对两种类型都正确，但 TypeScript 严格模式下报错。

**[修复]**
`lib/api.ts` 批量替换：
- `(id: number,` → `(id: number | string,`
- `(id: number)` → `(id: number | string)`
- `permId: number` → `permId: number | string`
共 47 处。URL path 参数本质上是字符串，两种类型都合理。

---

## v1.0.8 — 修复 CNB SCA 扫描发现的依赖 CVE + CI build 错误

- **提交**：`59dacf8`
- **时间**：2026-04-18
- **类型**：🟥 Fix（依赖安全 + 构建）

**[CI 修复]**
v1.0.7 移除 `next.config.ts` 的 `ignoreBuildErrors` 后立即暴露隐藏类型错误：
`app/(dashboard)/dashboard/cert/page.tsx:608` 调用 `api.getToken()` 但
未在文件顶部 import `api`，CI build 失败：`Cannot find name 'api'`。
补 import：`import { api, certApi, ... } from '@/lib/api'`。

**[依赖 CVE 修复]** CNB 软件成分分析（SCA）扫描及 npm audit 报：

| 漏洞 | 影响 | 修复 |
|---|---|---|
| **CVE-2026-23864** React Server Components DoS | react@19.2.3 | 升 react / react-dom → **19.2.4** |
| **GHSA-q4gf-8mx6-v5v3** Next.js DoS via Server Components（high） | next@16.0.0-beta.0 ~ 16.2.2 | 升 next / eslint-config-next → **16.2.4** |

**验证结果**
- `npm audit --omit=dev` → **0 vulnerabilities**
- `govulncheck ./... -mode=source` → **No vulnerabilities found**

> 注：项目 `next.config.ts` 设 `output: 'export'` 走静态导出，运行时
> 不暴露 Server Function / Server Components 端点，原本不直接受影响；
> 但 SCA 工具基于 dependency 版本判定，即便运行时不触发，依赖版本号本身
> 仍会被全网公开扫描结果列为高危条目。统一升到补丁版本以消除显示。

---

## v1.0.7 — 第四轮深度审计：cert 全线 IDOR / SystemConfig 提权 / 通知 SSRF / 邮件 CRLF + 前端类型守卫与并发安全

- **提交**：`b664c13`
- **时间**：2026-04-18
- **类型**：🟥 Fix（关键安全）

第四轮前后端深度审计结果：后端 11 项 + 前端 8 项；本次修 9 项关键。

### 🔴 后端 Critical

| 编号 | 修复 |
|------|------|
| **R-1** cert.go 全线 IDOR | 任意已登录用户可 `GET /api/cert/orders` 列他人订单、`?type=key` 下载他人证书私钥；`POST /process` 强签他人订单烧 LE 配额。新增 `cert_authz.go`：`requireCertAccountOwner` / `requireCertOrderOwner` 助手；`scopeCertAccountQuery` / `scopeCertOrderQuery` 列表 UID 过滤；应用到 `GetCertAccounts/CreateCertAccount/UpdateCertAccount/DeleteCertAccount/GetCertOrders/CreateCertOrder/ProcessCertOrder/GetCertOrderLog/GetCertOrderDetail/DownloadCertOrder/DeleteCertOrder/ToggleCertOrderAuto` 共 12 个 handler |
| **R-2** SystemConfig 凭据泄露 + 提权 | `GET/POST /api/system/config` 此前无任何鉴权，普通用户可读 mail_password / tgbot_token / webhook_url / oauth_secret 等全部凭据，并写入 site_url 钓鱼 magic-link / 关闭 captcha / 改 webhook_url 触发 SSRF。两 handler 加 `requireAdmin` |
| **R-3** SetRecordStatus 越权 | 与 CreateRecord/UpdateRecord/DeleteRecord 不一致，缺 CheckDomainPermission；任意拥有任一域名读权限的用户可对全站任意记录调启停 → DoS。补 `middleware.CheckDomainPermission(userID, level, domainID)` |

### 🟡 后端 High

| 编号 | 修复 |
|------|------|
| **R-4** 邮件头 CRLF 注入 | `EmailNotifier.Send` 对 `Subject/From/FromName/To` 字段做 `SanitizeMailHeader`，含 `\r\n\x00` 直接拒绝。叠加 R-2 后任意用户可注入 `Bcc:` 钓鱼跳板 |
| **R-5** Webhook/Discord/Bark/WeChat SSRF | 新增 `notify/safe_url.go` 的 `ValidateOutboundURL`：协议白名单 + 拒绝私网/回环/链路本地/CGNAT/IMDS。Webhook / Discord / Bark / WechatWork 4 个 notifier 出站前校验。Telegram URL 硬编码 `api.telegram.org` 安全 |
| **R-6** ProcessCertOrder 重复签发竞态 | 原 `order.Status = 1; Save(&order)` 非原子，并发 N 次 process 触发 N 个 ACME goroutine 烧 LE 配额。改 CAS：`UPDATE WHERE id=? AND status!=1`，`RowsAffected=0` 拒绝。手动 + 自动续期路径双修 |
| **R-7** 邮件发送 goroutine 堆积 DoS | `enqueueNotifyMail` 引入 32 槽信号量 `mailSendSem`；公开接口 (forgot-password / send-code / magic-link) 即便突破 verify rate limit，并发邮件 SMTP dial 也不会无限堆积导致 FD/内存耗尽 |

### 🟡 前端 High

| 编号 | 修复 |
|------|------|
| **H-2** `ignoreBuildErrors` | 删除 `next.config.ts` 的 `typescript.ignoreBuildErrors`；类型错误重新成为 CI 拦截器，避免 XSS / 原型污染 / 越权类型混淆隐患悄悄到达运行时 |
| **M-3** `currentAesKey` 全局并发覆盖 | `lib/crypto.ts` 移除 `currentAesKey` 全局变量；`hybridEncrypt` 不再写入；`decryptResponse(resp, aesKey?)` 缺 aesKey 直接拒解，避免并发请求串台导致信息泄露 |

### 未本轮处理（说明）

- **R-8** LookupRecord 时序泄露 — 信息泄露级别低，下次合并修
- **R-9** GetMonitorOverview SQL 拼接结构脆弱 — 当前不可注入，结构性弱点
- **R-10** 错误信息泄露内部细节 — 散布于 ~20 处，需要系统性脱敏，单独 PR 处理
- **R-11** quic-go / sqlite 等依赖版本提示 — 等下游无 break 再升
- 前端 H-1 React/Next CVE — 引用的 CVE 号未经独立验证，待官方公告确认后单独升级
- 前端 M-1/M-2/M-4/M-5、L-1/L-2/L-3 — 非关键，下次集中清理

---

## v1.0.6 — 补 cnb:read-file 的 exports 映射

- **提交**：`b374fcf`
- **时间**：2026-04-18
- **类型**：🟥 Fix

**[修复]**
`.cnb.yml` 中 `cnb:read-file` stage 仅声明了 `filePath` 未声明 `exports` 映射，
导致 `.version.env` 内的 `VERSION` / `RELEASE_TAG` 根本没有被注入环境变量。
`git:release` 的 `options.tag: ${RELEASE_TAG}` 因而展开为空串，日志输出
`tagname is empty` 并直接跳过创建 Release。

参考 [docs.cnb.cool](https://docs.cnb.cool/zh/build/internal-steps.html)
官方示例：`cnb:read-file` 必须同时声明 `exports:` 子字段做
`源key: 目标ENV` 的映射；填加：

```yaml
- name: export release env
  type: cnb:read-file
  options:
    filePath: .version.env
  exports:
    VERSION: VERSION
    RELEASE_TAG: RELEASE_TAG
```

---

## v1.0.5 — 修复 CNB 流水线 cnb:read-file 参数名 + 新增 Chanlog.md

- **提交**：`be8bab4`
- **时间**：2026-04-18
- **类型**：🟥 Fix + 🟩 Docs

**[修复]**
- `.cnb.yml` 中 `cnb:read-file` 插件参数键由 `file` 改为 `filePath`（插件要求）。v1.0.4 的 `master.push` 会报：
  `Parameter check failed: cnb:read-file data must have required property 'filePath'. parameter value: undefined`

**[文档]**
- 新增 `docs/Chanlog.md`：按时间倒序归档从项目初始化至今 27 条提交的修复/功能内容

---

## v1.0.4 — CI 自动刷新 Docker/Release + 中危收尾

- **提交**：`156e21c`
- **时间**：2026-04-18 01:22:17 +0800
- **类型**：🟥 Fix + 🟩 Feat

**[CI/CD 升级]**（Feat）
`master.push` 不再只上传 commit 附件，改为基于 `VERSION` 文件的完整发布流：

- pipeline A：编译 amd64/arm64 → changelog → `git:release` (`overlying=true`) upsert 到 `v<VERSION>` 发布 → `cnbcool/attachments` 上传 tar.gz + SHA256SUMS
- pipeline B：多架构 Docker buildx → 推 `:v<VERSION>` 与 `:latest` 到 `${CNB_DOCKER_REGISTRY}/${CNB_REPO_SLUG_LOWERCASE}`
- VERSION 通过 `. ./.version.env` 在 stage 间传递 `RELEASE_TAG`，同一 VERSION 多次 push 只更新描述/镜像，不产生重复 Release

**[安全修复]** 第三轮审计剩余中危 3 项

| 编号 | 修复内容 |
|------|---------|
| M-1 TOTP 重放 | 新增 `VerifyTOTPCodeWithCounter` 返回匹配 counter；handler 将 `(sha256(secret)[:8], counter)` 写入 cache 90s，同窗口第二次即拒"验证码已使用" |
| M-3 管理员层级 | `UpdateUser` 拒绝修改等级 ≥ 自己的其他用户与把他人 level 提升至同级以上；`DeleteUser` 阻断同级互删 |
| M-7 login_fail IP 分桶 | IPv4 归一到 `/24`，IPv6 归一到 `/64`，内存缓存模式下攻击者撑爆 map 的 DoS 通道被封闭 |

---

## v1.0.3 — 第三轮深度审计修复

- **提交**：`cdda231`
- **时间**：2026-04-18 01:14:20 +0800
- **类型**：🟥 Fix

**扫描结果**：🔴 1 + 🟡 4 + 🟢 7；本次修 **C-1 / H-1 / H-2 / H-3 / H-4 / M-5 / M-6** 共 7 项。

**🔴 Critical**

| 编号 | 修复内容 |
|------|---------|
| C-1 用户管理零认证提权 | 普通用户可 `POST /api/users/:id` 改 level=2 即成管理员。新增 `requireAdmin` 助手，补到 `GetUsers/CreateUser/UpdateUser/DeleteUser/GetUserPermissions/AddUserPermission/UpdateUserPermission/DeleteUserPermission/ResetAPIKey/AdminSendResetEmail/AdminResetTOTP` 全部接口 |

**🟡 High**

| 编号 | 修复内容 |
|------|---------|
| H-1 部署路径遍历 | 新增 `servers/path_guard.go` 的 `sanitizeRemotePath` / `sanitizeLocalPath`；SSH / FTP / local 三个部署器接入校验，拒绝 `..` 上溯、控制字符、非绝对路径 |
| H-2 登录计数器竞态 | 原 `GetJSON+自增+SetJSON` 非原子，并发失败请求有 lost-update；改 `cache.C.Incr`（Redis INCR 原子，memoryCache mutex 兜底） |
| H-3 local 部署 RCE | `CreateCertDeploy / UpdateCertDeploy` 对 `CertAccount.Type == "local"` 强制 `isAdmin`；本地部署 `restart_cmd` 以服务进程权限执行，等同服务端 RCE |
| H-4 JWT 算法混淆 | `ParseToken` 的 keyfunc 显式断言 `*jwt.SigningMethodHMAC`，拒绝 `alg=none` 或未来 RS256 降级攻击面 |

**🟢 Medium**

| 编号 | 修复内容 |
|------|---------|
| M-5 禁用账户不计入失败计数 | 禁用账户登录路径也 `noteLoginFailure`，避免通过"失败计数未增长"推断账户状态 |
| M-6 TestProxy SSRF | 代理测试 host 加入私网/回环/链路本地拒绝，防管理员误触发 SSRF 穿云厂商 IMDS |

---

## v1.0.2 — 前端安全专项审计

- **提交**：`61f5ac1`
- **时间**：2026-04-18 01:01:38 +0800
- **类型**：🟥 Fix

**扫描结果**：前端 🔴 4 + 🟡 5 + 🟢 3；本次修 **H-1 / H-2 / H-4 / M-1 / M-2 / M-4** 共 6 项。

**🔴 High**

| 编号 | 修复内容 |
|------|---------|
| H-1 签名密钥仍读 refresh_token | `lib/crypto.ts` 的 `deriveSignKey` 不再从 localStorage 读 `refresh_token`；改为 `SHA-256(access_token + secret_token)`；`getSignTokens()` 移除 `refreshToken` 字段 |
| H-2 magic-login Open Redirect | redirect 白名单：仅允许以 `/` 开头且非 `//` 起始的站内相对路径 |
| H-4 URL 查询串泄露 token | 彻底移除从 `?token=&refresh_token=` 读凭据的兼容路径（`layout.tsx` 与 `oauth-callback.ts`），仅保留 URL fragment `#` 作为 OAuth 载体 |

**🟡 Medium**

| 编号 | 修复内容 |
|------|---------|
| M-1 cert 下载绕过 ApiClient | `cert/page.tsx` 改用 `api.getToken()` 统一 Token 来源；format 通过 `URLSearchParams` 构造；Blob `revokeObjectURL` 延后 1s 避免 click() 竞争 |
| M-2 OAuth 绑定 URL 注入 | `profile/page.tsx handleBind` 仅允许 `https://` 协议，阻断 `javascript:` / `data:` URI |
| M-4 captcha siteKey 注入面 | host 清理 `innerHTML=''` → `replaceChildren()`；新增 `isValidSiteKey` 做 `[A-Za-z0-9_-]` 长度 1-128 的格式校验 |

**未修说明**
- H-3 三家 captcha CDN 脚本 SRI：动态版本 CDN 无官方哈希发布，由后端 CSP script-src 纵深防御
- M-3 HKDF 签名派生：需前后端协同升级签名报文
- M-5 前端路由守卫后端二次校验：架构性改动，排后续

---

## v1.0.1 — 后端低危收尾 + VERSION 文件基线

- **提交**：`07b5492`
- **时间**：2026-04-18 00:55:25 +0800
- **类型**：🟥 Fix + 🟩 Feat（版本约定）

**[版本约定]**（Feat）
新增 `VERSION` 文件（仓库根）作为构建默认版本源。漏洞修复 → 小版本号 +patch；新功能 → 大版本号 +major。基线 1.0.0 起步，本次 patch 到 1.0.1。

**[修复]**

| 编号 | 修复内容 |
|------|---------|
| L-1 Refresh JTI 收紧 | 缓存就绪但条目缺失时不再放行（原兼容逻辑放大 Replay 攻击窗口）；仅 `cache.C==nil`（未初始化）时服务降级放行 |
| L-2 登录时序泄露 | 所有失败分支（密码错/账户禁用/锁定/TOTP 错）统一 `loginDelayJitter()` 注入 50~150ms 抖动 |
| L-4 API Key 升级 | `generateAPIKey` 16 → 32 字节（128bit → 256bit），HMAC-SHA256 签名密钥达到哈希输出上限；`models.User.APIKey` 列扩至 `size:64`，AutoMigrate 自动拉伸 |

**[构建脚本]**
`scripts/build.sh` 改为优先读 `VERSION` 文件（环境变量 `VERSION` 仍最高优先）。

---

## v1.0.0（基线）— 安全审计 14 项 + 汉化

> v1.0.0 建立于多次基础改造后的 commit `d14559a`。下列提交均发生在 v1.0.0 之前，属于基线构建阶段，未单独打 tag，仅以时间顺序列出。

### 2026-04-18 00:49:40 · 安全审计 14 项修复 + 残留英文汉化

- **提交**：`d14559a`
- **类型**：🟥 Fix

首轮安全审计：🔴 6 + 🟡 8 + 🟢 1 共 15 项中 14 项修复。

**🔴 High（6 项）**

| 编号 | 内容 |
|------|-----|
| H-1 默认管理员凭据 | 移除 `initAdmin` 硬编码 `admin/admin123`，强制走 `/api/install` |
| H-2 SSH 主机密钥校验 | 新增 `host_key` 配置字段；支持 known_hosts 或 `"algo base64"` 两段式；未配置时 Warn 日志不中断 |
| H-3 HTTP 监控 TLS | `InsecureSkipVerify` 默认关闭；`DMTask` 新增 `AllowInsecureTLS` 供高级场景显式勾选 |
| H-4 弱密码策略 | min=6 → min=8；Install/Register/ChangePassword/ResetPassword/CreateUser 接入 `ValidatePasswordStrength` |
| H-5 无暴力破解防护 | Login 加入 IP+用户名双维度失败计数，15 分钟窗口 5 次锁定 + 429 |
| H-6 ResetToken 明文落库 | 改用 `sha256(token)` 指纹落库，规避 GORM `Updates(map)` 绕过 `BeforeSave` 钩子 |

**🟡 Medium（8 项）**

| 编号 | 内容 |
|------|-----|
| M-1 前端 refresh_token 明存 | 停止把 `refresh_token` 写入 localStorage，仅走 HttpOnly Cookie |
| M-2 XSS 面缩减 | `account-form` 用 `textContent` + DOM API 替代 `innerHTML` |
| M-3 监控 SSRF | `CheckURL` 引入 `validateCheckURL`：协议白名单 + 拒绝私网/链路本地/组播/CGNAT/IMDS |
| M-4 OAuth state 迁移 | 进程内 map → `cache.C`（TTL 自动过期），多实例安全、修复内存泄漏 DoS |
| M-5 config 文件权限 | `config.json` 0644 → 0600；目录 0755 → 0700 |
| M-6 bcrypt 成本 | 10 → 12（OWASP 2024 推荐） |
| M-7 — | 跳过（暂记为增强项，v1.0.4 才完成） |
| M-8 CSP 头 | `SecurityHeaders` 新增 `Content-Security-Policy` |

**🟢 Low（1 项修 / 4 项保留）**

| 编号 | 内容 |
|------|-----|
| L-5 goroutine 兜底 | `Monitor.dispatchTasks` 改用 `utils.SafeGoWithName` |

**汉化残留**
- 翻译：`router.go` / `crypto.go` panic 消息；`acme.go` "unsupported key type"；`ucloud.go` CSR/DV 错误；`huoshan.go` / `spaceship.go` "api error" 前缀；`dialog.tsx` / `sheet.tsx` sr-only "Close" → "关闭"

---

### 2026-04-18 00:24:32 · CI changelog 基准点 fallback

- **提交**：`20f3865`
- **类型**：🟥 Fix
- `cnbcool/changelog` 内部调 `git describe --tags --abbrev=0 ${TAG}^`，基准 tag 不在祖先链时 fatal 退出。改为自实现 shell：有上游 tag → `PREV_TAG..CURRENT_TAG`；无上游 tag → 从仓库初始列到当前 tag；写入 `CHANGELOG_PARTIAL.md`

---

### 2026-04-18 00:13:44 · 修复 CNB_TAG_NAME 未定义

- **提交**：`42f3a23`
- **类型**：🟥 Fix
- CNB 流水线环境变量里没有 `CNB_TAG_NAME`，只有 `CNB_BRANCH`（tag_push 事件下值为 tag 名）。`set -eux` + 未定义变量触发错误，`build all arches` / `buildx` 阶段报错退出
- `.gitignore` 追加 `.cnb-token.local`

---

### 2026-04-18 00:01:06 · 参照 CNB 官方示例重写流水线

- **提交**：`f668f88`
- **类型**：🟩 Feat
- 参考 `cnb.cool/examples/ecosystem/golang-build` 与 `docker-buildx-multi-platform-example`
- `master.push`：linux amd64+arm64 二进制 + SHA256 上传为 commit 附件
- `$.tag_push` pipeline A：编译 + tar.gz + `cnbcool/changelog` → 拼接 Artifacts 段（含 Docker pull + SHA-256）→ `git:release`（latest=true, descriptionFromFile）→ `cnbcool/attachments` 上传
- `$.tag_push` pipeline B：`docker buildx + rootlessBuildkitd` 多架构镜像
- `$.web_trigger`：手动触发补发 `manual-<branch>-<sha>` tag

---

### 2026-04-17 23:55:46 · 修复 CNB 基础镜像路径

- **提交**：`119c9cd`
- **类型**：🟥 Fix
- 原 `docker.cnb.cool/cnb/{golang,node,docker}` 在 CNB 镜像源不存在，改为 `docker.io/library/…` 公共 Hub 全限定路径

---

### 2026-04-17 23:46:27 · 新增 CNB 自动化流水线与本地多架构构建脚本

- **提交**：`6f89682`
- **类型**：🟩 Feat
- `.cnb.yml`：push 触发交叉编译；`tag_push` 推多架构 Docker；`web_trigger` 手动出图
- `scripts/build.sh`：本地一键编译前端 + 多架构后端
- `scripts/docker-build.sh`：`docker buildx` 多架构镜像一键构建/推送
- `Dockerfile`：改 `-mod=vendor` 离线构建；新增 `VERSION` ARG 注入 `main.Version`

---

### 2026-04-17 23:28:17 · 修正阿里云账户示例凭据与字段名

- **提交**：`c43db1c`
- **类型**：🟥 Fix（docs）
- docs 原示例用 AWS 公开占位值 + snake_case 字段名，与 `aliyun.go` 中 `AccessKeyId / AccessKeySecret` 实际声明不一致；替换为 `<YOUR_...>` 尖括号占位，并补注以 `/api/dns/providers` 返回为准

---

### 2026-04-17 23:11:20 · 删除 .github 与 .qoder 目录

- **提交**：`384eac1`
- **类型**：🟩 Chore
- `.github/workflows/build.yml`：CNB 不使用 GitHub Actions
- `.qoder/`：代码分析工具生成目录，文档已迁移到 `docs/`

---

### 2026-04-17 23:02:41 · 升级 Go 1.26.2 + 依赖 vendor 化 + 安全加固

- **提交**：`a56ce1e`
- **类型**：🟩 Feat + 🟥 Fix

**[升级]**
- Go 1.25 → **1.26.2**（`go.mod`、`Dockerfile` 同步）
- 依赖 vendor 化：`go mod vendor` 生成 `main/vendor`，离线可编译

**[安全加固]**
- 字段级 **AES-256-GCM** 加密（`internal/crypto`）覆盖 7 个模型敏感列，启动自动迁移历史明文
- JWT secret 默认值自动替换为随机 32 字节 hex；支持 `DNSPLANE_JWT_SECRET` / `DNSPLANE_MASTER_KEY` 环境变量
- 请求日志 body/headers 脱敏（password/token/apikey 等掩码）
- CSRF double-submit cookie（`GET /api/csrf` 签发、`X-CSRF-Token` 校验）+ 前端 `api.ts` 拦截器

**[文档]**
`.qoder/repowiki/zh/content` → 工作区 `docs/`（82 个 md）

---

## 早期未版本化历史（v0.x 阶段）

### 2026-04-10 21:51:55 · 修复工作流构建问题 (2)

- **提交**：`1f35590` — tag `v1.0.2`（原始 GitHub tag，已推到 CNB）
- **类型**：🟥 Fix

### 2026-04-10 20:55:25 · 修复工作流构建问题 (1)

- **提交**：`668caf4` — tag `v1.0.1`（原始 GitHub tag）
- **类型**：🟥 Fix

### 2026-04-10 08:46:02 · 细节优化

- **提交**：`74a2da2`
- **类型**：🟩 Feat

### 2026-04-07 22:50:30 · 细节优化 (3)

- **提交**：`16016a1`
- **类型**：🟥 Fix

### 2026-04-07 14:47:15 · 细节优化 (2)

- **提交**：`536c28c`
- **类型**：🟥 Fix

### 2026-04-07 14:44:06 · 完善功能与特性

- **提交**：`4860982`
- **类型**：🟩 Feat

### 2026-04-06 12:38:12 · 升级依赖规避漏洞

- **提交**：`6e72b53`
- **类型**：🟥 Fix（依赖安全）

### 2026-04-06 12:28:31 · 细节优化

- **提交**：`8f66154`
- **类型**：🟩 Feat

### 2026-04-06 10:34:20 · 完善提供商支持 + IP 证书申请

- **提交**：`14b4277`
- **类型**：🟩 Feat
- 新增 IP 类型证书申请支持（ACME IP）

### 2026-04-06 09:34:31 · 完善后端证书部署 + 前后端一致性 + 补全文档

- **提交**：`1bd87e4`
- **类型**：🟩 Feat

### 2026-04-06 07:27:10 · 前端单阶段构建 + Go 交叉编译

- **提交**：`f69b7b5`
- **类型**：🟥 Fix
- 避免多架构 `npm` 超时；前端构建收归 `BUILDPLATFORM` 单次执行

### 2026-04-05 23:46:59 · CI/Docker 多架构改用 webpack

- **提交**：`b7c0edb`
- **类型**：🟥 Fix
- Turbopack 在 armv7 WASM 有问题，回退 webpack

### 2026-04-05 23:35:05 · 扩展多架构二进制 + `-buildvcs=false`

- **提交**：`8917f68` — tag `v1.0.0`（原始 GitHub tag）
- **类型**：🟩 Feat

### 2026-04-05 23:32:46 · 初始化推送

- **提交**：`a419d16`
- **类型**：🟩 Feat
- 项目初始化（基于原 PHP 版 dnsmgr 重构的 Go 实现）

---

## 版本里程碑速查

| 版本 | 提交 SHA | 日期 | 亮点 |
|------|---------|------|------|
| **v1.0.4** | `156e21c` | 2026-04-18 01:22 | CI 自动 Docker/Release + M-1/M-3/M-7 |
| **v1.0.3** | `cdda231` | 2026-04-18 01:14 | 深度审计 7 项（含 C-1 提权关键修复） |
| **v1.0.2** | `61f5ac1` | 2026-04-18 01:01 | 前端安全 6 项 |
| **v1.0.1** | `07b5492` | 2026-04-18 00:55 | 低危 3 项 + VERSION 文件基线 |
| **v1.0.0** | `d14559a` | 2026-04-18 00:49 | 首轮安全审计 14 项 |
| v0（基线） | `a419d16`~`1f35590` | 2026-04-05 ~ 2026-04-10 | 项目初始化 + 多架构 CI 打磨 |

---

## 统计（截至 v1.0.4）

- **总提交数**：26
- **安全修复（🟥 Fix）**：共 **30+** 条目，覆盖 OWASP Top 10 大部分类别
- **新增功能（🟩 Feat）**：主要是 CI/CD 流水线、VERSION 版本化约定、vendor 依赖本地化
- **已处理等级分布**：🔴 Critical 1 / 🟡 High ≥ 14 / 🟢 Medium+Low ≥ 15

## 贡献者

- 若溪 `<phg@live.com>` — 代码作者
- Claude Opus 4.7 `<noreply@anthropic.com>` — AI 辅助贡献
