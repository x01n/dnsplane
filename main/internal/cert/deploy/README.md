# Deploy 模块

证书自动部署模块，支持将SSL证书部署到多种目标平台。

## 目录结构

```
deploy/
├── base/                  # 基础类型包
│   └── base.go           # BaseProvider, DeployProvider, Register() 等
├── providers/             # 云服务商部署器 (16个)
├── panels/                # 面板类部署器 (15个)
├── servers/               # 服务器部署器 (4个)
├── others/                # 其他部署器 (5个)
├── config.go              # 部署器配置结构定义
├── config_*.go            # 各类别部署器配置
├── registry.go            # 类型别名（向后兼容）
├── interface.go           # cert.Register 配置注册
└── executor.go            # 部署任务执行器
```

## 文件分类

### 核心文件
| 文件 | 说明 |
|------|------|
| `config.go` | `DeployProviderConfig` 结构和类别常量 |
| `config_*.go` | 各类别部署器的配置注册 |
| `interface.go` | `DeployProvider` 接口定义 |
| `registry.go` | 注册中心、`BaseProvider` 基类 |
| `executor.go` | 任务调度、执行、重试逻辑 |

### 云服务商部署器 (providers)
| 文件 | 说明 |
|------|------|
| `aliyun_cdn.go` | 阿里云 CDN/DCDN/ESA/OSS/WAF/CLB/ALB/NLB |
| `tencent_cdn.go` | 腾讯云 CDN/EO/WAF/COS/CLB |
| `huawei_cdn.go` | 华为云 CDN/ELB/WAF/OBS |
| `baidu_cdn.go` | 百度云 CDN/BLB |
| `huoshan_cdn.go` | 火山引擎 CDN/DCDN/CLB/TOS |
| `qiniu.go` | 七牛云 CDN/OSS |
| `upyun.go` | 又拍云 CDN |
| `doge.go` | 多吉云 CDN |
| `ctyun_cdn.go` | 天翼云 CDN/ICDN |
| `ksyun_cdn.go` | 金山云 CDN |
| `ucloud_cdn.go` | UCloud UCDN |
| `aws_cloudfront.go` | AWS CloudFront/ACM |
| `wangsu_cdn.go` | 网宿科技 CDN/CDN Pro |
| `baishan_cdn.go` | 白山云 CDN |
| `gcore.go` | Gcore CDN |
| `cachefly.go` | Cachefly CDN |

### 自建系统部署器 (panels)
| 文件 | 说明 |
|------|------|
| `btpanel.go` | 宝塔面板 (网站/Docker/邮局/面板本身) |
| `btwaf.go` | 堡塔云WAF |
| `opanel.go` | 1Panel |
| `safeline.go` | 雷池WAF |
| `cdnfly.go` | Cdnfly |
| `lecdn.go` | LeCDN |
| `goedge.go` | GoEdge/FlexCDN |
| `kangle.go` | Kangle 用户/管理员面板 |
| `mwpanel.go` | MW面板 |
| `ratpanel.go` | 筷子面板 |
| `xp.go` | 小皮面板 |
| `synology.go` | 群晖 DSM |
| `lucky.go` | Lucky |
| `fnos.go` | 飞牛OS |
| `proxmox.go` | Proxmox VE |
| `k8s.go` | Kubernetes |
| `uusec.go` | 南墙WAF |

### 服务器部署器 (servers)
| 文件 | 说明 |
|------|------|
| `ssh.go` | SSH 远程部署 (PEM/PFX格式) |
| `ftp.go` | FTP 上传部署 |
| `local.go` | 本地文件部署 (PEM/PFX/JKS格式) |

### 其他部署器 (others)
| 文件 | 说明 |
|------|------|
| `west.go` | 西部数码虚拟主机 |
| `rainyun.go` | 雨云 |
| `unicloud.go` | uniCloud 服务空间 |
| `kuocai.go` | 括彩云 |

## 双注册规则（新增部署器前必读）

同一个部署器类型可能同时出现在两处注册表，但 `/api/cert/providers` 只输出一份。
决定哪一份生效的是 `merge_deploy_api.go` 的 `MergeDeployProviderConfigsForAPI`。

### 为什么会有两处注册

- **cert 注册表**：各部署器实现文件 `init()` 里的 `cert.Register(...)`，同时登记工厂函数（供
  `deploy.GetProvider` 解析后执行部署）和一份 `ProviderConfig`（API 视图用的名称/图标/表单字段）。

两处独立演进，因此同名类型会有两份可能不一致的定义。

### 哪一份生效

`MergeDeployProviderConfigsForAPI` 以 cert 注册表为基底，再遍历 `AllDeployConfigs`：

| 情形 | 处理 |
|------|------|
| 类型同时在两处，且**在** `patchServer` 白名单内 | 用 `AllDeployConfigs` 覆盖 Config / DeployConfig / Note / Name / Icon |
| 类型同时在两处，且**不在**白名单内 | 直接 `continue`，保留 cert 注册表的定义 |
| 类型仅在 `AllDeployConfigs` | 经 `deployConfigToAPIProviderConfig` 补入为独立 deploy 视图 |
| 类型仅在 cert 注册表 | 原样保留 |

`patchServer` 是 `merge_deploy_api.go` 中的一个硬编码白名单，当前为 `ssh` / `ftp` / `local`
—— 这三个服务器类部署器的表单字段以 `AllDeployConfigs` 为准。

注意 `cert.GetProviderConfig` **只查 cert 注册表**，不会回退到 `AllDeployConfigs`。
需要「cert 优先、deploy 兜底」时由调用方自行处理，参见
`internal/api/handler/cert.go` 中 `GetCertAccounts` 的 `type_name` 取值。

### 新增部署器时

1. 在 `config_*.go` 里用 `registerDeployConfig` 登记表单字段；
2. 若该类型还要兼作证书申请用途（`cert.Register` 带非空 `ProviderConfig`），检查类型名是否
   落在 `patchServer` 白名单 —— 落入则 cert 侧的字段会被 `AllDeployConfigs` 覆盖；
3. 若 `cert.Register` 的 `IsDeploy=false`（如 `huoshan`），部署视图由 `AllDeployConfigs` 提供；
4. 若类型只在 `AllDeployConfigs` 登记（如 `nginxproxymanager`、`s3storage`），无需 `cert.Register`。

> 两份定义往往并不一致。实测统计（2026-10-05，随注册表变动需重跑）：

| 集合 | 数量 | 明细 |
|------|------|------|
| cert 部署注册表 | 36 | `cert.GetDeployProviderConfigs()` |
| `AllDeployConfigs` | 53 | `deploy.GetAllDeployConfigs()` |
| 合并后 `/api/cert/providers` | 63 | `MergeDeployProviderConfigsForAPI` 输出 |
| **交集** | **26** | `AllDeployConfigs` 赢 3：`ssh` `ftp` `local`（`patchServer` 白名单）；cert 赢 23：`aliyun_cdn` `aws_cloudfront` `btpanel` `cachefly` `cdnfly` `doge` `fnos` `gcore` `goedge` `huawei_cdn` `k8s` `kangle` `kangleadmin` `lecdn` `lucky` `mwpanel` `opanel` `proxmox` `qiniu` `safeline` `synology` `upyun` `xp` |
| 仅 `AllDeployConfigs` | 27 | 经 `deployConfigToAPIProviderConfig` 补入，含 `nginxproxymanager`、`s3storage` |
| 仅 cert 注册表 | 10 | `aliyun_dcdn` `aws_acm` `baidu_cdn` `baishan_cdn` `ctyun_cdn` `huoshan_cdn` `ksyun_cdn` `tencent_cdn` `ucloud_cdn` `wangsu_cdn`（均为纯部署类型，无 `AllDeployConfigs` 条目） |

交集 26 个 key 的差异分级 —— **这正是维护者最容易踩的坑**：

| 差异类型 | 数量 | 明细 |
|----------|------|------|
| **表单结构不同**（Inputs / TaskInputs 的字段数量或 Key 不同） | **17** | `aliyun_cdn` `btpanel` `cachefly` `cdnfly` `doge` `fnos` `ftp` `gcore` `huawei_cdn` `k8s` `kangle` `lecdn` `local` `qiniu` `ssh` `synology` `upyun` |
| 仅名称 / 图标不同 | 2 | `goedge`（`goedge.png` vs `waf.png`）、`proxmox`（`proxmox.png` vs `proxmox.ico`） |
| 仅 Note 文字不同 | 7 | `aws_cloudfront` `kangleadmin` `lucky` `mwpanel` `opanel` `safeline` `xp` |
| 完全一致 | 0 | — |

典型的结构性差异举例：`btpanel` 的认证字段 cert 版是 `api_key`、`AllDeployConfigs` 版是 `key`；
`gcore` 是 `apikey` vs `account` 且 cert 版无 TaskInputs；`qiniu` 是 `access_key` vs `AccessKey`；
`ssh` 是 7 vs 8 个账户字段、5 vs 9 个任务字段。**改表单字段前必须确认自己改的是生效的那一份。**

## 添加新部署器

1. 在对应子目录创建新文件，如 `providers/my_cdn.go`
2. 实现 `DeployProvider` 接口：
   ```go
   package providers
   
   import "main/internal/cert/deploy/base"
   
   type MyCDNProvider struct {
       base.BaseProvider
   }
   
   func NewMyCDNProvider(config map[string]interface{}) base.DeployProvider {
       return &MyCDNProvider{BaseProvider: base.BaseProvider{Config: config}}
   }
   
   func (p *MyCDNProvider) Check(ctx context.Context) error { ... }
   func (p *MyCDNProvider) Deploy(ctx context.Context, fullchain, privateKey string, config map[string]interface{}) error { ... }
   ```
3. 在 `init()` 中注册：`base.Register("my_cdn", NewMyCDNProvider)`
4. 在对应的 `config_*.go` 中添加配置：`registerDeployConfig(DeployProviderConfig{...})`

## BaseProvider 常用方法

```go
p.GetString(key)                    // 获取账户配置字符串
p.GetStringFrom(config, key)        // 优先从任务配置获取，否则从账户配置
p.GetInt(key, defaultVal)           // 获取整数配置
p.Log(msg)                          // 记录日志
base.GetConfigDomains(config)       // 解析域名列表
base.GetConfigString(config, key)   // 从配置获取字符串
base.GetConfigBool(config, key)     // 从配置获取布尔值
```
