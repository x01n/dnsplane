# DNSPlane 变更日志

> 本文按版本**倒序**记录 DNSPlane 的问题、修复、验证、安全加固与文档变更。
>
> **证据口径**
> - 所有短 SHA、时间、规模均由 `git log` / `git show --shortstat` 在当前仓库实测得到，命令清单见文末《取证与复验命令》；文件数以 `--shortstat` 为准（`--numstat` 不计二进制文件，两者在 `a419d16`、`a56ce1e` 等有差异）
> - 所有源码行号对应**工作区当前文件**；工作区存在 293 项未提交改动（已暂存 289 项：`A ` 37、`D ` 74、`M ` 175、`MM` 3；另有 ` M` 1、`??` 3），行号不可与 `HEAD` 版本互换引用
> - 密钥、令牌、口令、连接串一律以 `{占位}` 表示，本文不复述任何真实凭据值
> - 历史条目沿用仓库既有 `docs/Chanlog.md` 的问题编号（C / R / H / M / L / F），本次重建仅按新规范重排格式，并对可复核项重新取证
>
> **版本约定**（随仓库根 `VERSION` 文件确立，引入提交 `07b5492`，主题标 `(1.0.1)`）
> - 漏洞修复 → patch 位 +1（例 `1.0.3 → 1.0.4`）
> - 新增功能 → major 位 +1（例 `1.x.x → 2.0.0`）

---

## 版本编号口径先读

仓库同时存在三套版本号编号方式，且两两冲突。本文正文以**口径 A** 为主线，另两套在对应条目内显式标注。

| 口径 | 载体 | 当前值 / 范围 | 实测依据 |
|---|---|---|---|
| A：提交主题标签 | 提交主题末尾的 `(1.0.N)` | `(1.0.1)` 至 `(1.0.11)` | `git log --pretty=%s`，共 11 条 |
| B：`VERSION` 文件 | 仓库根 `VERSION` | `1.0.11` | `cat VERSION`；首次由 `07b5492` 引入 |
| C：Git 标签 | `git tag` | 仅 `v1.0.0` 至 `v1.0.3` | `git for-each-ref refs/tags` |

三套口径的关键错位：

- 口径 A 与口径 B 一致：`(1.0.11)` 提交 `72e15a9` 把 `VERSION` 写为 `1.0.11`，此后 `VERSION` 未再递增
- 口径 C 与口径 A **指向完全不同的提交**：`v1.0.1` 是 `668caf4`，而主题标签 `(1.0.1)` 是 `07b5492`
- 口径 C 的最新标签 `v1.0.3` 落在 `0aa078e`（2026-05-01），时间上**晚于**口径 A 的 `v1.0.11`（2026-04-18）
- `v1.0.0` 标签指向 `a671019`，该提交不在 `master` 历史内（详见《版本号与 Git 标签口径冲突》）
- 后端 `GetSystemInfo` 硬编码返回 `"1.0.0"`（`main/internal/api/handler/dashboard.go:457`），与三套口径都不一致

---

## v1.0.11 — 199 个 TS 类型错误清零，本地构建跑通

- **提交**：`72e15a9`
- **时间**：2026-04-18
- **类型**：Fix（前端类型严格化）
- **规模或模块**：7 个文件，+228 / −17 行；`web/lib/api.ts`（+170 / −2）为主体，另含 4 个 dashboard 页面 / 组件（`monitor`、`request-logs`、`settings`、`settings/auth-control-settings`）与 `docs/Chanlog.md`、`VERSION` 各 1 行

**[问题]**

v1.0.10 之后 `npx tsc --noEmit` 报 **199 个**类型错误。这些错误自 v1.0.7 移除 `typescript.ignoreBuildErrors` 起持续暴露，是长期关闭类型门禁期间积累的存量问题，不是新引入的回归。

**[修复]**

类型定义补全集中在 `web/lib/api.ts`（本次实测该文件 1713 行）：

| 对象 | 补全内容 |
|---|---|
| `Domain` | 增 `perm_sub` |
| `WhoisInfo` | 增 `created_date` / `expiry_date` 别名 |
| `MonitorTask` | 增约 20 个字段 |
| `MonitorOverview` | 增 5 个字段 |
| `SystemConfig` | 增 50+ 系统设置键，并加 `[key: string]: any` 索引签名 |
| `CronConfig` | 增 4 个字段 |
| 新增导出 | `TaskStatus`、`authControlApi` |
| 方法补全 | `logApi.clean`、`totpApi.disable`、`totpApi.regenerateRecovery`、`authApi.getBehavioralCaptcha` 族、`systemApi.testDiscord` 族 |

业务代码修复：`web/app/(dashboard)/dashboard/monitor/page.tsx` 补默认值与签名兼容；`request-logs/page.tsx` 防 `JSON.parse` 空值；`settings/page.tsx` 类型强制；`settings/auth-control-settings.tsx` 泛型收窄。

**[验证]**

仓库既有记录为「`npx tsc --noEmit` 清零，本地 build 跑通」。本次文档重建在 2026-10-10 的工作区快照上重新复验：`npx tsc --noEmit` 退出码 0，无输出。该结果覆盖 v1.0.11 至未提交增量之间的全部前端改动，不能单独归因于 `72e15a9`。

---

## v1.0.10 — 补齐 domainId/subId 命名，修复 v1.0.9 漏改

- **提交**：`74432ec`
- **时间**：2026-04-18
- **类型**：Fix（前端类型）
- **规模或模块**：3 个文件，+29 / −6 行；`web/lib/api.ts`（+4 / −4）、`docs/Chanlog.md`（+24 / −1）、`VERSION`（`git show --numstat 74432ec`）

**[问题]**

v1.0.9 只按 `id` 这一种命名做了 `number → number \| string` 放宽，`domainId` 与 `subId` 两种命名被遗漏，仍有类型不匹配残留。

**[修复]**

`domainId: number` → `number \| string` 共 4 处，覆盖 `domainId` 与 `subId` 两类命名。

---

## v1.0.9 — 修复关闭 ignoreBuildErrors 后暴露的 id 类型不匹配

- **提交**：`301421f`
- **时间**：2026-04-18
- **类型**：Fix（前端类型严格化）
- **规模或模块**：3 个文件，+74 / −48 行；`web/lib/api.ts`（+46 / −46）、`docs/Chanlog.md`（+27 / −1）、`VERSION`（`git show --numstat 301421f`）

**[问题]**

v1.0.7 移除 `typescript.ignoreBuildErrors` 后，CI 构建因类型错误失败。根因是后端 ID 在部分路径下以字符串返回，而前端签名声明为 `number`。

**[修复]**

`id: number` → `number \| string`，修复 47 处 API 签名类型。属于契约对齐而非放宽：以返回值真实形态为准修正声明。

---

## v1.0.8 — 依赖 CVE 升级 + cert 页面构建修复

- **提交**：`59dacf8`
- **时间**：2026-04-18
- **类型**：Fix（依赖安全 + 构建）
- **规模或模块**：5 个文件，+170 / −80 行；`web/package.json`（+4 / −4）、`web/package-lock.json`（+133 / −73）、`web/app/(dashboard)/dashboard/cert/page.tsx`（+1 / −1）、`docs/Chanlog.md`、`VERSION`

**[安全加固]**

| 漏洞 | 受影响版本 | 处置 |
|---|---|---|
| React Server Components DoS | `react@19.2.3` | 升级 `react` / `react-dom` → `19.2.4` |
| Next.js DoS via Server Components | `next@16.0.0-beta.0 ~ 16.2.2` | 升级 `next` / `eslint-config-next` → `16.2.4` |

当前工作区实测版本：Next.js `16.2.4`、React `19.2.4`（`web/package.json`）。

**[修复]**

修复 cert 页面 import 缺失导致的构建失败。

---

## v1.0.7 — 第四轮深度审计：证书越权 / 系统配置提权 / 通知 SSRF / 邮件头注入

- **提交**：`b664c13`
- **时间**：2026-04-18
- **类型**：Fix（关键安全）
- **规模或模块**：11 个文件，+385 / −39 行；后端 7 个（`api/handler/{auth,cert,cert_authz,domain,user}.go`、`notify/{notify,safe_url}.go`），前端 2 个（`web/lib/crypto.ts`、`web/next.config.ts`），另有 `VERSION` 与 `docs/Chanlog.md`

**[安全加固] 后端 Critical**

编号口径：上一版本文档的后端表用 `R-1`~`R-3` / `H-1`~`H-4`，前端表又独立复用了 `H-2` 与 `M-3`，两组编号在同一条目内直接撞号。本文按作用域加 `后端-` / `前端-` 前缀消歧，序号本身沿用原始审计记法。

| 编号 | 问题 |
|---|---|
| 后端-R1 | `cert.go` 全线越权（IDOR）：任意已登录用户可列他人证书订单，经 `?type=key` 下载他人证书私钥；`POST /process` 可强签他人订单，烧掉 Let's Encrypt 配额 |
| 后端-R2 | `GET` / `POST /api/system/config` 无任何鉴权：普通用户可读 `mail_password`、`tgbot_token`、`webhook_url`、`oauth_secret` 等全部凭据，并可写入 `site_url` 用于钓鱼 magic-link、关闭 captcha、改 `webhook_url` 触发 SSRF |
| 后端-R3 | `SetRecordStatus` 缺 `CheckDomainPermission`，与增删改三个接口不一致，拥有任一域名读权限的用户可对全站记录调启停 |

处置：新增 `main/internal/api/handler/cert_authz.go`，提供 `requireCertAccountOwner`（`:32`）、`requireCertOrderOwner`（`:46`）与 `scopeCertAccountQuery`（`:68`）、`scopeCertOrderQuery`（`:76`）列表过滤助手，应用于账户 / 订单链 10 个 handler（实测 `b664c13` 版本）；`SystemConfig` 两个 handler 加 `requireAdmin`（`main/internal/api/handler/account.go:43`）；`SetRecordStatus` 补 `middleware.CheckDomainPermission`，实测现落位于 `main/internal/api/handler/domain.go:1155`。

**[安全加固] 后端 High**

| 编号 | 问题 | 处置 |
|---|---|---|
| 后端-H1 | `EmailNotifier.Send` 未过滤邮件头控制字符，`Subject` / `From` / `FromName` / `To` 可注入 CRLF | `SanitizeMailHeader` 拒绝含 `\r\n\x00` 的字段（`main/internal/notify/safe_url.go:79`） |
| 后端-H2 | Webhook / Discord / Bark / WeChatWork 四个 notifier 出站无协议与地址校验，可作 SSRF 跳板 | `ValidateOutboundURL`（`main/internal/notify/safe_url.go:15`）：协议白名单 + 拒绝私网 / 回环 / 链路本地 / CGNAT / IMDS |
| 后端-H3 | `ProcessCertOrder` 的 `Status` 判定与写入非原子，并发 N 次 process 触发 N 个 ACME goroutine | 改 CAS：`UPDATE WHERE id=? AND status!=1`，`RowsAffected=0` 即拒绝；手动与自动续期双路径同修 |
| 后端-H4 | 邮件发送 goroutine 无上限，公开接口可堆积 SMTP dial 耗尽文件描述符 | `enqueueNotifyMail` 引入 32 槽信号量 |

**[安全加固] 前端 High**

| 编号 | 问题 | 处置 |
|---|---|---|
| 前端-H2 | `next.config.ts` 的 `typescript.ignoreBuildErrors` 静默吞掉类型错误，XSS / 原型污染 / 越权类型混淆可穿越到运行时 | 删除该开关，类型错误重新成为 CI 拦截器 |
| 前端-M3 | `lib/crypto.ts` 的 `currentAesKey` 全局变量在并发请求下互相覆盖，导致响应串台 | 移除全局变量，`hybridEncrypt` 不再写入，`decryptResponse` 缺 `aesKey` 直接拒解 |

**[验证]**

本轮修复引入的类型门禁直接导致 v1.0.9 至 v1.0.11 三个连续 patch 版本。本次复验：`main/internal/notify/safe_url.go`、`main/internal/api/handler/cert_authz.go` 均存在于工作区并被编译引用，`go build ./...` 退出码 0。

**[验证] 一项历史口径与实测不符（须如实登记）**

| 项 | 历史口径 | 实测 |
|---|---|---|
| cert 族鉴权覆盖 | 助手「应用到 cert 族 12 个 handler」 | `b664c13` 版本 `cert.go` 中调用 4 个账户 / 订单助手的 handler 为 **10** 个：`CreateCertOrder`、`DeleteCertAccount`、`DeleteCertOrder`、`DownloadCertOrder`、`GetCertAccounts`、`GetCertOrderDetail`、`GetCertOrderLog`、`ProcessCertOrder`、`ToggleCertOrderAuto`、`UpdateCertAccount`；当前工作区因补入部署链而升至 **15** 个（新增 `GetCertDeploys`、`CreateCertDeploy`、`UpdateCertDeploy`、`DeleteCertDeploy`、`ProcessCertDeploy`） |

另一处需更正上一版本文档的归属错误：`cert_authz.go` 的 `requireCertDeployOwner`（`:96`）与 `scopeCertDeployQuery`（`:110`）**不属于** v1.0.7，实测 `git show b664c13:main/internal/api/handler/cert_authz.go` 与 `git show HEAD:...` 均只有 4 个助手（`:32`、`:46`、`:68`、`:76`），后 2 个仅存在于当前工作区的已暂存改动中。相应调用点为 `cert.go:1171`、`:1270`、`:1306`、`:1320`。

---

## v1.0.6 — 补 cnb:read-file 的 exports 映射

- **提交**：`b374fcf`
- **时间**：2026-04-18
- **类型**：Fix（CI）
- **规模或模块**：3 个文件，+39 / −4 行；`.cnb.yml`（+9 / −2）、`docs/Chanlog.md`（+29 / −1）、`VERSION`。`.cnb.yml` 已在 `66f4799` 删除

**[修复]**

补 `cnb:read-file` 的 `exports` 映射，`git:release` 的 `tagname` 不再为空。

**[问题]**

发布流水线读不到 tag 名称，导致 Release 以空 tag 创建。属流水线插件参数契约问题，不影响产物代码。

---

## v1.0.5 — 修复 cnb:read-file 参数名 + 新增本文档

- **提交**：`be8bab4`
- **时间**：2026-04-18
- **类型**：Fix（CI）+ Docs
- **规模或模块**：3 个文件，+374 / −3 行；`docs/Chanlog.md`（+368）、`.cnb.yml`（+5 / −2）、`VERSION`

**[修复]**

修正 `cnb:read-file` 的参数名。

**[文档]**

新增完整变更日志 `docs/Chanlog.md`，即本文档的第一版：`git show --numstat be8bab4` 实测该文件 +368 / −0，占本次 +374 增量的主体；另两行为 `.cnb.yml`（+5 / −2）与 `VERSION`（+1 / −1）。

---

## v1.0.4 — CI 自动发布 Docker + Release，第三轮审计中危收尾

- **提交**：`156e21c`
- **时间**：2026-04-18
- **类型**：Feat + Fix（安全）
- **规模或模块**：5 个文件，+248 / −36 行；`.cnb.yml`（+146 / −26）、`main/internal/api/handler/auth.go`、`main/internal/api/handler/user.go`、`main/internal/utils/totp.go`、`VERSION`

**[修复] CNB 发布流**

`master.push` 不再只上传 commit 附件，改为基于 `VERSION` 文件的完整发布流：pipeline A 编译 amd64 / arm64 → changelog → `git:release` upsert 到 `v<VERSION>` → 上传 `tar.gz` 与 `SHA256SUMS`；pipeline B 多架构 Docker buildx → 推 `:v<VERSION>` 与 `:latest`。同一 `VERSION` 多次 push 只更新描述与镜像，不产生重复 Release。

注：`.cnb.yml` 已在 `66f4799` 删除，当前唯一在用 CI 为 GitHub Actions `.github/workflows/build.yml`。本条为历史记录。

**[安全加固] 第三轮审计剩余中危 3 项**

| 编号 | 问题 | 处置 |
|---|---|---|
| M-1 TOTP 重放 | 同一时间窗内 TOTP 码可重复使用 | 新增 `VerifyTOTPCodeWithCounter` 返回匹配 counter（`main/internal/utils/totp.go:70`）；handler 把 `(sha256(secret)[:8], counter)` 写入缓存 90 秒，同窗口第二次即拒 |
| M-3 管理员层级 | 同级或上级账号可互相改级、互删 | `UpdateUser` 拒绝修改等级 ≥ 自己的其他用户、拒绝把他人提升至同级以上；`DeleteUser` 阻断同级互删 |
| M-7 login_fail IP 分桶 | 以完整 IP 为键，可用海量 IP 撑爆 map 造成 DoS | IPv4 归一到 `/24`，IPv6 归一到 `/64` |

编号沿用第三轮审计原始记法，非连号。`M-5` / `M-6` 已在 `cdda231`（v1.0.3）处置；`M-2` / `M-4` 在仓库既有变更日志中无对应条目，**状态未知**——不推断其已修复或已废弃。

---

## v1.0.3 — 第三轮深度审计：7 项高危 + 中危

- **提交**：`cdda231`
- **时间**：2026-04-18
- **类型**：Fix（安全）
- **规模或模块**：11 个文件，+218 / −11 行；`main/internal/api/handler/{account,auth,cert,dashboard,user}.go`、`main/internal/api/middleware/auth.go`、`main/internal/cert/deploy/servers/{ftp,local,ssh,path_guard}.go`、`VERSION`

**[安全加固] Critical**

用户管理零认证提权：普通用户可 `POST /api/users/:id` 把 `level` 改成 2 即成管理员。

处置：新增 `requireAdmin` 助手（`main/internal/api/handler/account.go:43`），补到 `GetUsers` / `CreateUser` / `UpdateUser` / `DeleteUser` / `GetUserPermissions` / `AddUserPermission` / `UpdateUserPermission` / `DeleteUserPermission` / `ResetAPIKey` / `AdminSendResetEmail` / `AdminResetTOTP` 全部接口。

**[安全加固] High**

| 编号 | 问题 | 处置 |
|---|---|---|
| H-1 | 证书部署路径遍历 | 新增 `main/internal/cert/deploy/servers/path_guard.go` 的 `sanitizeRemotePath`（`:22`）/ `sanitizeLocalPath`（`:27`），SSH / FTP / local 三部署器接入，拒绝 `..` 上溯与控制字符 |
| H-2 | 登录失败计数器竞态 | 改 `cache.C.Incr`：Redis `INCR` 原子，内存实现以 mutex 兜底 |
| H-3 | local 部署远程命令执行 | `CreateCertDeploy` / `UpdateCertDeploy` 对 `CertAccount.Type == "local"` 强制 `isAdmin`；`restart_cmd` 以服务进程权限执行，等同服务端 RCE |
| H-4 | JWT 算法混淆 | `ParseToken` 的 keyfunc 显式断言 `*jwt.SigningMethodHMAC`，拒绝 `alg=none` 与 RS256 降级 |

**[安全加固] Medium**

| 编号 | 问题 | 处置 |
|---|---|---|
| M-5 | 禁用账户不计入失败计数，可据此推断账户状态 | 禁用账户登录路径同样 `noteLoginFailure` |
| M-6 | `TestProxy` 可 SSRF 穿透云厂商 IMDS | 代理测试 host 加入私网 / 回环 / 链路本地拒绝 |

---

## v1.0.2 — 前端安全专项审计：6 项漏洞

- **提交**：`61f5ac1`
- **时间**：2026-04-18
- **类型**：Fix（前端安全）
- **规模或模块**：8 个文件，+94 / −56 行；`web/lib/crypto.ts`、`web/lib/oauth-callback.ts`、`web/components/auth-captcha-fields.tsx`、`web/app/(auth)/magic-login/totp/page.tsx`、`web/app/(dashboard)/dashboard/{cert,profile}/page.tsx`、`web/app/(dashboard)/layout.tsx`、`VERSION`

**[安全加固] High**

| 编号 | 问题 | 处置 |
|---|---|---|
| H-1 | 签名密钥仍从 `localStorage` 读 `refresh_token` | `deriveSignKey` 改为 `SHA-256(access_token + secret_token)`；`getSignTokens()` 移除 `refreshToken` 字段 |
| H-2 | magic-login 开放重定向 | `redirect` 白名单：仅允许以 `/` 开头且非 `//` 起始的站内相对路径 |
| H-4 | URL 查询串泄露 token | 移除 `?token=&refresh_token=` 兼容路径，仅保留 URL fragment `#` 作为 OAuth 载体 |

**[安全加固] Medium**

| 编号 | 问题 | 处置 |
|---|---|---|
| M-1 | cert 下载绕过 ApiClient 直接取凭据 | 改用 `api.getToken()` 统一 Token 来源；`URLSearchParams` 构造参数；Blob `revokeObjectURL` 延后 1 秒 |
| M-2 | OAuth 绑定 URL 注入 | 仅允许 `https://` 协议，阻断 `javascript:` 与 `data:` |
| M-4 | captcha `siteKey` 注入面 | `innerHTML=''` 改 `replaceChildren()`；新增 `isValidSiteKey` 做 `[A-Za-z0-9_-]` 长度 1-128 校验 |

**[问题] 本轮明确未修**

| 编号 | 未修原因 |
|---|---|
| H-3 | 三家 captcha CDN 脚本的 SRI：动态版本 CDN 无官方哈希发布，改由后端 CSP `script-src` 纵深防御 |
| M-3 | HKDF 签名派生需前后端协同升级签名报文 |
| M-5 | 前端路由守卫的后端二次校验属架构性改动 |

注：`lib/crypto.ts` 的 HKDF 派生链与请求签名链，在 2026-10-10 的深度分析中被证实**前后端两侧均为死代码**（`.wiki-context/analysis-auth.md`）。上述 M-3 的「需协同升级」前提已不成立，属待清理项而非待修复项。

---

## v1.0.1 — 后端低危收尾 + VERSION 文件基线

- **提交**：`07b5492`
- **时间**：2026-04-18
- **类型**：Fix + Feat
- **规模或模块**：6 个文件，+46 / −7 行；`VERSION`（新建 1 行）、`main/internal/api/handler/auth.go`、`user.go`、`main/internal/api/middleware/auth.go`、`main/internal/models/models.go`、`scripts/build.sh`

**[文档] 版本约定落地**

新增仓库根 `VERSION` 文件作为构建默认版本源，约定漏洞修复 → patch +1、新功能 → major +1，基线从 `1.0.0` 起步。当前内容为 `1.0.11`。

实测细节：`VERSION` 在 `72e15a9` 为 `1.0.11\n`，在 `66f4799` 变为 `1.0.11\r\n`（行尾由 LF 改为 CRLF，版本号数值未变），工作区当前仍为 CRLF。

**[安全加固] Low**

| 编号 | 处置 |
|---|---|
| L-1 | Refresh JTI 收紧：缓存就绪但条目缺失时不再放行，仅 `cache.C == nil` 时服务降级放行 |
| L-2 | 登录时序泄露：所有失败分支统一注入 50~150 ms 抖动 |
| L-4 | API Key 从 16 字节升至 32 字节，`models.User.APIKey` 列扩至 `size:64`，AutoMigrate 自动拉伸 |

---

## v1.0.0 — 安全审计 14 项修复 + 残留英文汉化（版本约定基线）

- **提交**：`d14559a`
- **时间**：2026-04-18
- **类型**：Fix（安全）+ 国际化
- **规模或模块**：23 个文件，+400 / −115 行；后端覆盖 `api/handler/{auth,oauth,user}.go`、`api/middleware/auth.go`、`api/router.go`、`cert/{acme/acme.go,deploy/servers/ssh.go,huoshan,ucloud}`、`config`、`crypto/{crypto,migrate}.go`、`database`、`dns/providers/spaceship`、`models`、`monitor/{check,monitor}.go`；新增 `main/internal/monitor/ssrf.go`（+94 行，本提交引入）；前端 `web/components/{account-form,ui/dialog,ui/sheet}.tsx` 与 `web/lib/api.ts`

**[安全加固]**

一轮审计 14 项修复 + 残留英文汉化。本提交为「提交主题打 `(1.0.N)` 标签」这一惯例的起点之前的基线条目：主题本身未带版本后缀，版本号由 `(1.0.1)` 起始的后续提交倒推确定。

**[问题] 口径提示**

本条目存在两个易被误读的点：

- 提交日期为 2026-04-18，而仓库首个提交 `a419d16` 是 2026-04-05，「v1.0.0」不等于项目起点
- Git 标签 `v1.0.0` 指向 `a671019`，与本条目的 `d14559a` 无关，且 `a671019` 不在 `master` 历史中

紧随其后的 CI 相关修复：`20f3865` 为 changelog 阶段加基准点 `try/catch` fallback，`42f3a23` 修复 `CNB_TAG_NAME` 未定义导致 `set -u` 报错。

---

## v1.0.11 之后的提交（2026-04-18 → 2026-10-09）

以下 6 个提交在 `72e15a9` 之后进入 `master`，但主题未打 `(1.0.N)` 标签、`VERSION` 亦停留在 `1.0.11`，因此**不构成新版本**。按任务要求从 `git log` 补齐。

| # | 提交 | 时间 | 作者 | 类型 | 规模（`git show --shortstat`） |
|---|---|---|---|---|---|
| 1 | `7d1f96f` | 2026-04-18 | 若溪 | Docs | 1 文件，+1 / −1 |
| 2 | `7e6e2c3` | 2026-04-18 | chanhanzhan | Merge | 5854 文件，+7824497 / −710 |
| 3 | `0aa078e` | 2026-05-01 | chanhanzhan | Feat | 5769 文件，+5541 / −7821633（标签 `v1.0.3`） |
| 4 | `820d426` | 2026-05-12 | chanhanzhan | Feat | 18 文件，+3701 / −241 |
| 5 | `74189be` | 2026-07-19 | chanhanzhan | Chore | 252 文件，+70521 / −63692（origin 分支头） |
| 6 | `66f4799` | 2026-10-09 | chanhanzhan | Feat | 326 文件，+68030 / −41360（`HEAD`） |

**[文档]** `7d1f96f` 把 `docs/Chanlog.md` 中 v1.0.11 的提交 SHA 占位回填为 `72e15a9`，1 行改动。

**[修复]** `7e6e2c3` 合并 im-ph 的 PR #2，正文「漏洞修复」。合并统计取自 `git show`；`git log --shortstat` 对 merge 提交默认不产出统计，两者口径不同。

**[修复]** `0aa078e` 主题「完善功能设置，增加 cloudflare 相关功能」。百万级行数全部来自撤销 vendor：实测该提交触及 5710 个 `main/vendor/` 路径（删除），非 vendor 文件仅 59 个，覆盖 `api/handler`、`cert/deploy/{panels,providers}`、`crypto`、`database/maintenance.go`、`dbcache`、`monitor`、`notify`、`dns/providers/{qingcloud,technitium}` 与前端。同时打了 Git 标签 `v1.0.3`。

**[修复]** `820d426` 新增 Cloudflare 自定义主机名与 Tunnel 的后端与前端：`main/internal/api/handler/cloudflare.go`、`main/internal/service/cloudflare_enhance.go`、`web/app/(dashboard)/dashboard/cloudflare/{hostnames,tunnels}`、新增 `web/app/(dashboard)/dashboard/schedule/page.tsx`；另有 `main/internal/verify/code.go`、`main/internal/dns/providers/technitium/technitium.go`、`main/internal/api/handler/dashboard.go`。

**[修复]** `74189be` 的主题「Update SVG icons and sync script formatting」**严重低估了实际范围**。实测拆解：

- 252 个文件中 153 个在 `main/internal`，19 个在 `main/internal/api/handler/`；按状态拆为 15 个新增（`A`）+ 237 个修改（`M`）
- 主体是全库行尾转换：`main/internal/api/handler/auth.go` 在 `74189be^` 与 `74189be` 的 `od -c` 首字节分别为 `\n` 与 `\r\n`；`main/internal/api/handler/cert_authz.go` 与 `main/internal/api/middleware/apikey.go` 虽出现在文件清单里，但 `git show -w --numstat` 对它们**无输出**，即纯行尾转换、零逻辑改动
- 忽略空白后真实改动为 **67 个文件，+7907 / −1078 行**（`git show -w --shortstat`）
- 15 个新增文件中 9 个为后端实现：`main/internal/api/handler/{acceleration,dns_check,domain_category}.go`、`main/internal/cert/deploy/providers/{aliyun_ddoscoo,aliyun_esa_saas,aliyun_ga}.go`、`main/internal/dns/providers/dnsmgr/dnsmgr.go`、`main/internal/dns/providers/henet/henet.go`、`main/internal/service/acceleration.go`
- 忽略空白后的主要逻辑增量：`main/internal/api/handler/acceleration.go`（+909）、`main/internal/service/acceleration.go`（+498）、`main/internal/dns/providers/dnsmgr`（+467）、`main/internal/dns/providers/henet`（+455）、`main/internal/api/handler/cloudflare.go`（+449 / −23）、`web/app/(dashboard)/dashboard/settings/page.tsx`（+394 / −154）

结论：审查该提交必须带 `-w`，否则 252 文件的行尾噪音会淹没真实改动。

**[修复]** `66f4799` 为 `HEAD`，主题是 CI 与文档的整体切换：删除 `.cnb.yml`（−360 行）、新增 `.github/workflows/build.yml`（+208 行），改动 `Dockerfile`、`.dockerignore`、`.gitignore`、`README.md`、`VERSION`（仅行尾），并重写 `docs/**`；忽略空白后为 **245 个文件，+30527 / −3857 行**（`git show -w --shortstat`）。`VERSION` 数值保持 `1.0.11`，未随本次发布递增。

---

## 未提交工作区增量（截至 2026-10-10，尚未取得提交 SHA）

- **提交**：未知（尚未 `git commit`，`git status --porcelain` 实测 293 项：`A ` 37、`D ` 74、`M ` 175、`MM` 3、` M` 1、`??` 3）
- **时间**：2026-10-10（本文重建时点；生成期间工作区正被并发改写，计数会继续变动）
- **类型**：Feat + Fix（安全）+ Docs
- **规模或模块**：已暂存改动 289 个文件，+50268 / −70967 行（`git diff --cached --numstat`），其中 `main` + `web` 为 206 个文件、+44084 / −34941 行，其余为 `docs/**` 重写；新增 `/api/v1` 独立包 14 个实现文件 + 6 个测试文件，新增 handler 族 4 个实现文件 + 10 个测试文件

**[问题]**

上一版本文档把本批改动记为「v1.0.12」。经复核该版本号在仓库中**不存在任何依据**：`VERSION` 为 `1.0.11`、`git tag` 最新为 `v1.0.3`、无任何提交主题含 `(1.0.12)`，且本批改动全部未提交。按「SHA 必须来自 `git log` 实测」的口径，本节不冠版本号，改为按模块记录。

**[修复] 部署族所有权校验补全**

`cert_authz.go` 已实现 `requireCertAccountOwner` / `requireCertOrderOwner` 覆盖证书账户与订单两族，但 `CertDeploy` 的三个写入口与一个查询入口没有对应校验。`CertDeploy.Config` 存的是证书外送目标，越权修改等于把他人私钥改送到攻击者可控地址；越权触发 `process` 更会直接把证书链与私钥外送。

| 入口 | 校验 | 位置（工作区当前行号） |
|---|---|---|
| `GetCertDeploys` | `scopeCertDeployQuery` 补 UID 过滤 | `main/internal/api/handler/cert.go:1171`（函数定义 `:1160`） |
| `CreateCertDeploy` | `requireCertAccountOwner` + `requireCertOrderOwner` | `main/internal/api/handler/cert.go:1216`、`:1220`（函数定义 `:1197`） |
| `UpdateCertDeploy` | `requireCertDeployOwner` | `main/internal/api/handler/cert.go:1270`（函数定义 `:1266`） |
| `DeleteCertDeploy` | `requireCertDeployOwner` | `main/internal/api/handler/cert.go:1306`（函数定义 `:1301`） |
| `ProcessCertDeploy` | `requireCertDeployOwner` | `main/internal/api/handler/cert.go:1320`（函数定义 `:1315`） |

新增 `requireCertDeployOwner`（`main/internal/api/handler/cert_authz.go:96`）与 `scopeCertDeployQuery`（`:110`）。两者**不复用**订单链的所有权判定：`CertDeploy` 自带 `uid` 列，其 `aid` 指向部署账户而非证书账户，走订单链会判错属主。

**[修复] /api/v1 独立端点族落地**

新增 `main/internal/apiv1/` 包 14 个实现文件：`router.go`、`routes_batch1.go`、`routes_batch2.go`、`adapt.go`、`auth.go`、`bridge.go`、`domain.go`、`errors.go`、`params.go`、`record.go`、`record_sync.go`、`response.go`、`scope.go`、`util.go`，配套 6 个测试文件（`body_and_message_test.go`、`bridge_test.go`、`route_delivery_test.go`、`routes_batch2_test.go`、`scope_test.go`、`skeleton_test.go`）。另新增 handler 族 4 个实现文件 `main/internal/api/handler/batch_authz.go`（批量记录越权校验）、`plan.go`、`default_plan.go`、`subscription.go`，配套 10 个测试文件，以及 `main/internal/models/quota.go`、`main/internal/service/record_sync.go`，前端新增 `web/app/(dashboard)/dashboard/plans/page.tsx`。

**[验证]**

2026-10-10 在当前工作区实测：

| 命令 | 工作目录 | 结果 |
|---|---|---|
| `go build ./...` | `main/` | 退出码 0 |
| `go vet ./internal/api/handler/` | `main/` | 退出码 0 |
| `go test ./... -count=1` | `main/` | 退出码 0；14 个包 `ok`，45 个包 `no test files`，无 FAIL |
| `npx tsc --noEmit` | `web/` | 退出码 0，无输出 |

**[问题] 上一版本文档的「未解决」项已消解**

上一版记录 `TestUpdateCertDeployRejectsStranger` 因夹具与攻击 payload 同用 `attacker.example.com` 而恒失败。实测当前 `main/internal/api/handler/cert_deploy_authz_test.go`（366 行）已把种子 `Config` 改为 `{"host":"origin.example.com"}`（`:96`），并在 `:86-88` 留下说明该假阴性成因的注释；攻击 payload 仍为 `attacker.example.com`（`:298`），断言在 `:309`。`go test -run 'TestCreateCertDeploy\|TestGetCertDeploys\|TestUpdateCertDeploy\|TestDeleteCertDeploy\|TestProcessCertDeploy'` 退出码 0。

**[问题] 本批增量仍待处理的取证结论（未修复，仅登记）**

以下条目来自 2026-10-10 的深度分析产物，尚未对应任何提交：

| 域 | 待处理结论 | 产物 |
|---|---|---|
| 证书 | ACME 账户密钥从未持久化：`Register` 返回值被丢弃（`main/internal/cert/acme/acme.go:583`，`if _, err := c.Register(ctx); err != nil`） | `.wiki-context/analysis-cert.md` |
| 证书 | PEM 下载把私钥明文写入 `request_logs.response` | `.wiki-context/analysis-cert.md` |
| 扩展 | `main/internal/cert/ucloud` 未被任何包导入，`go list -deps` 证实不在二进制内，445 行签发实现的 `init()` 永不执行 | `.wiki-context/analysis-ext.md` |
| 监控 | 新库 `AutoMigrate` 缺 `last_error` / `backup_health` / `fault_time` / `recover_time` 列，合并 UPDATE 报错被丢弃 | `.wiki-context/analysis-monitor.md` |
| 权限 | 行为验证码存在类型混淆绕过；`user_o_auths` 令牌因表名不匹配而明文落库 | `.wiki-context/analysis-auth.md` |
| API | `OPTIONS` 预检返回 404 且无任何 CORS 头 | `.wiki-context/analysis-api.md` |
| 部署 | `GetSystemInfo` 硬编码 `"version": "1.0.0"`（`main/internal/api/handler/dashboard.go:457`），不读 `VERSION` | `.wiki-context/analysis-deploy.md` |

**[文档]**

`git status` 实测 74 项 `D ` 全部为 `docs/**` 下待重生的旧文档，37 项 `A ` 中含本文档在内的新版顶层文档。仓库当前 `git ls-files docs | wc -l` = 9。

---

## 早期未版本化历史（带 `(1.0.N)` 标签的提交之前，2026-04-05 → 2026-04-18）

以下 22 个提交在基线条目 `d14559a` 之前进入历史，主题未标注版本号。按提交时间倒序，规模取自 `git show --shortstat`。

| 时间 | 提交 | 类型 | 主题 | 规模 |
|---|---|---|---|---|
| 2026-04-18 | `20f3865` | ci(cnb) | changelog 阶段加基准点 try/catch fallback | 1 文件，+29 / −4 |
| 2026-04-18 | `42f3a23` | ci(cnb) | 修复 `CNB_TAG_NAME` 未定义导致 `set -u` 报错 | 2 文件，+16 / −13 |
| 2026-04-18 | `f668f88` | ci | 参照 CNB 官方示例重写流水线，接入 release / changelog / attachments 插件 | 1 文件，+124 / −81 |
| 2026-04-17 | `119c9cd` | ci | 修复 CNB 基础镜像路径 | 1 文件，+6 / −6 |
| 2026-04-17 | `6f89682` | ci | 新增 CNB 自动化流水线与本地多架构构建脚本 | 4 文件，+305 / −3 |
| 2026-04-17 | `c43db1c` | docs | 修正阿里云账户示例的错误占位凭据与字段名 | 1 文件，+4 / −2 |
| 2026-04-17 | `384eac1` | chore | 删除 `.github` 与 `.qoder` 目录 | 2 文件，+0 / −193 |
| 2026-04-17 | `a56ce1e` | chore | 升级 Go 1.26.2 + 依赖 vendor 化 + 安全加固 | 5806 文件，+7821876 / −154 |
| 2026-04-10 | `1f35590` | fix | 修复工作流构建问题（标签 `v1.0.2`） | 1 文件，+31 / −84 |
| 2026-04-10 | `668caf4` | fix | 修复工作流构建问题（标签 `v1.0.1`） | 1 文件，+78 / −13 |
| 2026-04-10 | `74a2da2` | feat | 细节优化 | 26 文件，+969 / −349 |
| 2026-04-07 | `16016a1` | fix | 细节优化 | 1 文件，+47 / −5 |
| 2026-04-07 | `536c28c` | fix | 细节优化 | 1 文件，+15 / −3 |
| 2026-04-07 | `4860982` | feat | 细节优化，完善一些功能与特性 | 29 文件，+1169 / −319 |
| 2026-04-06 | `6e72b53` | fix | 升级依赖版本避免漏洞遭到利用 | 2 文件，+84 / −81 |
| 2026-04-06 | `8f66154` | feat | 细节优化 | 30 文件，+3645 / −926 |
| 2026-04-06 | `14b4277` | feat | 完善提供商支持，新增 IP 证书申请支持 | 9 文件，+560 / −94 |
| 2026-04-06 | `1bd87e4` | feat | 完善后端证书部署实现，完善前后端一致性，补全文档 | 124 文件，+37479 / −540 |
| 2026-04-06 | `f69b7b5` | fix(docker) | 前端单阶段构建 + Go 交叉编译，避免多架构 npm 超时 | 1 文件，+14 / −4 |
| 2026-04-05 | `b7c0edb` | fix(web) | CI / Docker 多架构构建使用 webpack，避免 armv7 WASM Turbopack 错误 | 1 文件，+1 / −1 |
| 2026-04-05 | `8917f68` | ci | 扩展多架构二进制与 `-buildvcs=false` | 2 文件，+14 / −2 |
| 2026-04-05 | `a419d16` | feat | 初始化推送 | 300 文件，+74215 / −0（其中 50 个二进制） |

**[问题]** 两条巨型提交的规模数字不可直接用于工作量评估：

- `a56ce1e`（+7821876）为 vendor 目录首次入库，实测触及 5710 个 `main/vendor/` 路径
- `0aa078e`（−7821633）为同一批 vendor 目录的删除；现行 CI 的注释与之互证——`.github/workflows/build.yml:3-4`：「依赖由 `actions/setup-go` 的模块缓存承担，仓库不再携带 `main/vendor`」

---

## 版本号与 Git 标签口径冲突

**实测标签清单**（`git for-each-ref refs/tags --format='%(refname:short)\|%(objectname:short)\|%(creatordate:short)'`）：

| 标签 | 指向提交 | 提交主题 | 日期 | 是否在 `master` 历史内 |
|---|---|---|---|---|
| `v1.0.0` | `a671019` | ci: 扩展多架构二进制与 `-buildvcs=false` | 2026-04-05 | 否（孤立提交） |
| `v1.0.1` | `668caf4` | fix: 修复工作流构建问题 | 2026-04-10 | 是 |
| `v1.0.2` | `1f35590` | fix: 修复工作流构建问题 | 2026-04-10 | 是 |
| `v1.0.3` | `0aa078e` | feat: 完善功能设置，增加 cloudflare 相关功能 | 2026-05-01 | 是 |

**[问题]** 三处口径冲突及可验证的解释：

1. `v1.0.0` → `a671019` 不在任何分支：`git branch -a --contains a671019` 输出为空，`git merge-base --is-ancestor a671019 HEAD` 退出码 1。其父提交为 `dc7ecbb`，亦不在 `master` 上。但 `git diff --stat 8917f68 a671019` **输出为空**，即两提交树完全一致、主题与日期相同——判定为 rebase / amend 后遗留的重复提交，标签落在了改写前的旧对象上（解释为推断，树一致为实测）。
2. 标签与提交主题标签指向不同提交：`v1.0.1` 是 `668caf4`，主题标签 `(1.0.1)` 是 `07b5492`；`v1.0.2` 是 `1f35590`，主题标签 `(1.0.2)` 是 `61f5ac1`；`v1.0.3` 是 `0aa078e`，主题标签 `(1.0.3)` 是 `cdda231`。
3. 标签数落后于 `VERSION`：Git 标签只到 `v1.0.3`，`VERSION` 已到 `1.0.11`；`v1.0.4` 至 `v1.0.11` 从未在 Git 中打标，其版本号仅由提交主题后缀与 `VERSION` 文件维持。

**[安全加固]** 由冲突 3 得出的实际风险：`git checkout v1.0.3` 取到的是 2026-05-01 的 cloudflare 功能提交，不含 v1.0.4 至 v1.0.11 的任何一项安全修复（其中含 cert 族 IDOR、系统配置零鉴权、路径遍历、JWT 算法混淆）。任何按 Git 标签回溯的部署都必须显式改为按提交 SHA 指定。

**[文档]** 建议后续统一为单一口径：以 `VERSION` + 与提交主题一致的 Annotated Tag 为准，且 tag 必须在 `master` 可达；同时让 `GetSystemInfo` 读取 `VERSION` 而非硬编码。以上为**建议，未实施**。

---

## 版本里程碑速查

| 版本 | 提交 | 时间 | 主题 | 规模 |
|---|---|---|---|---|
| 未提交增量 | 未知 | 2026-10-10 | `/api/v1` 族落地、部署族鉴权补全、文档重建 | 已暂存 289 项（+50268 / −70967） |
| v1.0.11 之后 | `66f4799` | 2026-10-09 | CI 由 CNB 切换 GitHub Actions、文档重写 | 326 文件（−w 245 文件） |
| v1.0.11 之后 | `74189be` | 2026-07-19 | 全库 LF → CRLF；新增一键加速、dnsmgr / henet 服务商、阿里云三类部署器 | 252 文件（−w 67 文件） |
| v1.0.11 之后 | `820d426` | 2026-05-12 | Cloudflare 主机名与 Tunnel、定时任务页 | 18 文件 |
| v1.0.11 之后 | `0aa078e` | 2026-05-01 | 撤销 vendor + cloudflare 功能（标签 `v1.0.3`） | 5769 文件（非 vendor 59） |
| v1.0.11 之后 | `7e6e2c3` | 2026-04-18 | 合并 PR #2 | merge |
| v1.0.11 之后 | `7d1f96f` | 2026-04-18 | 回填本文档 SHA | 1 文件 |
| v1.0.11 | `72e15a9` | 2026-04-18 | 199 个 TS 类型错误清零 | 7 文件 |
| v1.0.10 | `74432ec` | 2026-04-18 | `domainId` / `subId` 类型 | 3 文件 |
| v1.0.9 | `301421f` | 2026-04-18 | `id` 类型 47 处 | 3 文件 |
| v1.0.8 | `59dacf8` | 2026-04-18 | React / Next CVE 升级 | 5 文件 |
| v1.0.7 | `b664c13` | 2026-04-18 | 第四轮深度审计 | 11 文件 |
| v1.0.6 | `b374fcf` | 2026-04-18 | CI exports 映射 | 3 文件 |
| v1.0.5 | `be8bab4` | 2026-04-18 | CI 参数名 + 本文档建立 | 3 文件 |
| v1.0.4 | `156e21c` | 2026-04-18 | CNB 自动发布 + 中危收尾 | 5 文件 |
| v1.0.3 | `cdda231` | 2026-04-18 | 第三轮审计 7 项 | 11 文件 |
| v1.0.2 | `61f5ac1` | 2026-04-18 | 前端安全 6 项 | 8 文件 |
| v1.0.1 | `07b5492` | 2026-04-18 | 低危收尾 + `VERSION` 基线 | 6 文件 |
| v1.0.0 | `d14559a` | 2026-04-18 | 审计 14 项 + 汉化（基线） | 23 文件 |

注：速查表中「v1.0.11 之后」六行不是版本条目，仅为补齐的时间线；`0aa078e` 一行的 `v1.0.3` 指 Git 标签，与主题标签 `(1.0.3)` 提交 `cdda231` 不是同一提交。

---

## 统计

| 项 | 数值 | 口径与实测命令 |
|---|:----:|---|
| 版本条目 | 12 | v1.0.0 至 v1.0.11，取自提交主题 `(1.0.N)` 后缀 |
| 仓库总提交 | 40 | `git rev-list --count HEAD` |
| 未版本化提交（v1.0.11 之后） | 6 | `7d1f96f` … `66f4799` |
| 未版本化提交（v1.0.0 之前） | 22 | 首提交 `a419d16` 至 `20f3865` |
| 时间跨度 | 2026-04-05 至 2026-10-09 | 首个提交至 `HEAD` |
| 跨度天数 | 187 | `(2026-10-09) − (2026-04-05)`，实测 |
| 密集迭代窗口 | 2026-04-17 至 2026-04-18 | 12 个版本条目全部落在 2026-04-18；另有 5 条 CI / 依赖提交落在 2026-04-17 |
| Git 标签 | 4 | `v1.0.0` 至 `v1.0.3`，落后 `VERSION` 8 个 patch 位 |
| 孤立标签对象 | 1 | `v1.0.0` → `a671019` 不在 `master` |
| 未提交改动 | 293 | `git status --porcelain \| wc -l`；已暂存 289 项（`A ` 37 / `D ` 74 / `M ` 175 / `MM` 3），另 ` M` 1、`??` 3 |
| 参与作者 | 2 | 若溪 21 次、chanhanzhan 19 次（`git log --pretty=%an`） |
| Go 测试包 | 14 ok / 45 无测试 | `go test ./... -count=1`，退出码 0 |
| 前端类型错误 | 0 | `npx tsc --noEmit`，退出码 0 |

**[问题]** 四个必须如实登记的统计侧写：

1. 12 个版本条目全部落在 **2026-04-18 一天之内**，提交时间从 `d14559a`（00:49）到 `72e15a9`（08:37）不足 8 小时；回填条目的 `7d1f96f` 为 08:44。因此「版本号递增」不等于「分次发布」，回滚粒度不能按版本号设计
2. 40 个提交中只有 11 个（27.5%）带 `(1.0.N)` 标签；基线 `d14559a` 自身未带标签，22 个（55%）在基线之前，6 个（15%）在 v1.0.11 之后。其中 2 个百万行级 vendor 提交（`a56ce1e` / `0aa078e`）使任何按行数的产出统计失真
3. 全部 11 条带 `(1.0.N)` 标签的提交作者均为若溪（21 次提交），chanhanzhan（19 次提交）名下无一条版本标签——版本号递增与安全 / 类型修复节奏绑定，而功能与工程维护提交（含 `0aa078e`、`820d426`、`74189be`、`66f4799` 四个 v1.0.11 之后的提交）不参与版本递增，这正是 `VERSION` 停在 `1.0.11` 的直接原因
4. `74189be` 一个提交就贡献了 252 个文件的行尾转换噪音，任何按 `git show --shortstat` 直接求和的工作量统计在该提交上会虚高约 8.9 倍（+70521 对 +7907，`git show -w --shortstat` 实测）
5. 规模统计必须区分口径：`--shortstat` 计二进制文件而 `--numstat` 不计，`a419d16`（300 对 250）、`a56ce1e`（5806 对 5790）、`66f4799 -w`（245 对 243）三处可见差异；本文所有文件数取 `--shortstat`

---

## 取证与复验命令

以下命令均为本次文档重建实际执行过的最小复验集，在 `${ROOT}` = `/home/clfchen/Desktop/file/archive/dnsplane/dns` 下运行。

| 目的 | 命令 |
|---|---|
| 版本条目与主题标签 | `git log --pretty=format:'%h\|%ad\|%s' --date=format:'%Y-%m-%d'` |
| 每提交规模 | `git show --shortstat --pretty=format:'' <sha>`；需剔除空白噪音时加 `-w` |
| 规模口径差异 | `git show --numstat --pretty=format:'' <sha>` 不计二进制文件，与 `--shortstat` 的文件数在 `a419d16`（250 对 300）、`a56ce1e`（5790 对 5806）、`66f4799 -w`（243 对 245）三处不同；本文表格一律取 `--shortstat` |
| merge 提交规模 | `git show --shortstat --pretty=format:'' 7e6e2c3`（`git log --shortstat` 对 merge 提交不产出统计，两者口径不同） |
| 标签与指向 | `git for-each-ref refs/tags --format='%(refname:short)\|%(objectname:short)\|%(creatordate:short)'` |
| 标签可达性 | `git branch -a --contains a671019`、`git merge-base --is-ancestor a671019 HEAD` |
| 孤立提交树一致性 | `git diff --stat 8917f68 a671019`（输出为空） |
| 行尾转换取证 | `git show 74189be^:main/internal/api/handler/auth.go \| od -c \| head -2` 与 `git show 74189be:...` 对比 |
| vendor 规模归因 | `git show --name-only --pretty=format:'' a56ce1e \| grep -c '^main/vendor/'` |
| 工作区状态 | `git status --porcelain`，按前两列分组计数 |
| 后端构建 | `cd main && go build ./...` |
| 后端静态检查 | `cd main && go vet ./internal/api/handler/` |
| 后端测试 | `cd main && go test ./... -count=1` |
| 部署族鉴权测试 | `cd main && go test ./internal/api/handler/ -run 'TestCreateCertDeploy\|TestGetCertDeploys\|TestUpdateCertDeploy\|TestDeleteCertDeploy\|TestProcessCertDeploy' -count=1` |
| 前端类型检查 | `cd web && npx tsc --noEmit` |
| 行号核对 | 对 `cert.go`、`cert_authz.go`、`safe_url.go`、`path_guard.go`、`totp.go`、`account.go`、`dashboard.go` 逐一 `grep -n '^func '` |

**[文档]** 本文的重建依据为：`git log` / `git show` 实测输出、工作区源码实读行号、既有 `docs/Chanlog.md` 历史条目，以及 `.wiki-context/` 下 `scan-infra.md` 与 7 份 `analysis-*.md` 分析产物。上一版本文档中的「v1.0.12」条目与「贡献者」表因在仓库中无可验证依据（前者无版本号来源且 SHA 实为已提交对象，后者在 `README.md` 与 `git log` 作者列表中均无匹配）已移除，相关事实改写至《未提交工作区增量》与《统计》两节。
