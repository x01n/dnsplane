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
