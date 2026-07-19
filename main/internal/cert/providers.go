package cert

func init() {
	registerCertProviders()
	registerDeployProviders()
}

func registerCertProviders() {

	// Google SSL
	Register("google", nil, ProviderConfig{
		Type: "google",
		Name: "Google SSL",
		Icon: "google.png",
		Note: "需要配置EAB凭证",
		Config: []ConfigField{
			{Name: "邮箱地址", Key: "email", Type: "input", Placeholder: "EAB申请邮箱", Required: true},
			{
				Name: "EAB获取方式", Key: "eab_mode", Type: "radio",
				Options: []ConfigOption{{Value: "auto", Label: "自动获取"}, {Value: "manual", Label: "手动输入"}},
				Value:   "manual",
			},
			{Name: "keyId", Key: "kid", Type: "input", Required: true},
			{Name: "b64MacKey", Key: "key", Type: "input", Required: true},
			{
				Name: "环境选择", Key: "mode", Type: "radio",
				Options: []ConfigOption{{Value: "live", Label: "正式环境"}, {Value: "staging", Label: "测试环境"}},
				Value:   "live",
			},
			{
				Name: "使用代理服务器", Key: "proxy", Type: "radio",
				Options: []ConfigOption{{Value: "0", Label: "否"}, {Value: "1", Label: "是"}},
				Value:   "0",
			},
		},
		CNAME: true,
	})
	Register("litessl", nil, ProviderConfig{
		Type: "litessl",
		Name: "LiteSSL",
		Icon: "litessl.png",
		Note: "需要从freessl.cn获取EAB凭证",
		Config: []ConfigField{
			{Name: "邮箱地址", Key: "email", Type: "input", Placeholder: "EAB申请邮箱", Required: true},
			{Name: "EAB KID", Key: "kid", Type: "input", Required: true},
			{Name: "EAB HMAC Key", Key: "key", Type: "input", Required: true},
			{
				Name: "使用代理服务器", Key: "proxy", Type: "radio",
				Options: []ConfigOption{{Value: "0", Label: "否"}, {Value: "1", Label: "是"}},
				Value:   "0",
			},
		},
		CNAME: true,
	})
	Register("tencent", nil, ProviderConfig{
		Type: "tencent",
		Name: "腾讯云免费SSL",
		Icon: "tencent.png",
		Note: "一个账号有50张免费证书额度",
		Config: []ConfigField{
			{Name: "SecretId", Key: "secret_id", Type: "input", Required: true},
			{Name: "SecretKey", Key: "secret_key", Type: "input", Required: true},
			{Name: "邮箱地址", Key: "email", Type: "input", Placeholder: "申请证书时填写的邮箱", Required: true},
			{
				Name: "使用代理服务器", Key: "proxy", Type: "radio",
				Options: []ConfigOption{{Value: "0", Label: "否"}, {Value: "1", Label: "是"}},
				Value:   "0",
			},
		},
		CNAME: false,
	})

	// 阿里云免费SSL
	Register("aliyun_cert", nil, ProviderConfig{
		Type: "aliyun_cert",
		Name: "阿里云免费SSL",
		Icon: "aliyun.png",
		Note: "每年有20张免费证书额度",
		Config: []ConfigField{
			{Name: "AccessKeyId", Key: "access_key_id", Type: "input", Required: true},
			{Name: "AccessKeySecret", Key: "access_key_secret", Type: "input", Required: true},
			{Name: "姓名", Key: "username", Type: "input", Placeholder: "申请联系人的姓名", Required: true},
			{Name: "手机号码", Key: "phone", Type: "input", Placeholder: "申请联系人的手机号码", Required: true},
			{Name: "邮箱地址", Key: "email", Type: "input", Placeholder: "申请联系人的邮箱地址", Required: true},
			{
				Name: "使用代理服务器", Key: "proxy", Type: "radio",
				Options: []ConfigOption{{Value: "0", Label: "否"}, {Value: "1", Label: "是"}},
				Value:   "0",
			},
		},
		CNAME: false,
	})

	// 自定义ACME
	Register("customacme", nil, ProviderConfig{
		Type: "customacme",
		Name: "自定义ACME",
		Icon: "ssl.png",
		Config: []ConfigField{
			{Name: "ACME地址", Key: "directory", Type: "input", Placeholder: "ACME Directory 地址", Required: true},
			{Name: "邮箱地址", Key: "email", Type: "input", Placeholder: "证书申请邮箱", Required: true},
			{Name: "EAB KID", Key: "kid", Type: "input", Placeholder: "留空则不使用EAB认证"},
			{Name: "EAB HMAC Key", Key: "key", Type: "input", Placeholder: "留空则不使用EAB认证"},
			{
				Name: "使用代理服务器", Key: "proxy", Type: "radio",
				Options: []ConfigOption{{Value: "0", Label: "否"}, {Value: "1", Label: "是"}},
				Value:   "0",
			},
		},
		CNAME: true,
	})
}

func registerDeployProviders() {
	// 宝塔面板
	Register("btpanel", nil, ProviderConfig{
		Type:     "btpanel",
		Name:     "宝塔面板",
		Icon:     "bt.png",
		Note:     "支持部署到宝塔面板搭建的站点",
		IsDeploy: true,
		Config: []ConfigField{
			{Name: "面板地址", Key: "url", Type: "input", Placeholder: "如: http://192.168.1.100:8888", Required: true},
			{Name: "接口密钥", Key: "api_key", Type: "input", Placeholder: "宝塔面板API接口密钥", Required: true},
			{
				Name: "面板版本", Key: "version", Type: "radio",
				Options: []ConfigOption{{Value: "0", Label: "Linux面板+Win经典版"}, {Value: "1", Label: "Win极速版"}},
				Value:   "0",
			},
			{
				Name: "使用代理服务器", Key: "proxy", Type: "radio",
				Options: []ConfigOption{{Value: "0", Label: "否"}, {Value: "1", Label: "是"}},
				Value:   "0",
			},
		},
		DeployConfig: []ConfigField{
			{Name: "部署类型", Key: "type", Type: "radio", Required: true, Options: []ConfigOption{
				{Value: "0", Label: "网站的证书"},
				{Value: "3", Label: "Docker网站的证书"},
				{Value: "2", Label: "邮局域名的证书"},
				{Value: "1", Label: "面板本身的证书"},
			}, Value: "0"},
			{Name: "网站名称列表", Key: "sites", Type: "textarea", Placeholder: "每行一个网站名称", Show: "type==0||type==2||type==3", Required: true,
				Note: "PHP/反代填写绑定的第一个域名；Java/Node/Go填写项目名称；邮局/IIS填写绑定域名"},
			{Name: "是否IIS站点", Key: "is_iis", Type: "radio", Options: []ConfigOption{{Value: "0", Label: "否"}, {Value: "1", Label: "是"}}, Show: "type==0", Value: "0"},
		},
		DeployNote: "系统会根据关联SSL证书的域名，自动更新对应证书",
	})

	// SSH部署
	Register("ssh", nil, ProviderConfig{
		Type:     "ssh",
		Name:     "SSH部署",
		Icon:     "ssh.png",
		Note:     "通过SSH部署证书到服务器",
		IsDeploy: true,
		Config: []ConfigField{
			{Name: "服务器地址", Key: "host", Type: "input", Placeholder: "服务器IP或域名", Required: true},
			{Name: "SSH端口", Key: "port", Type: "input", Placeholder: "默认22", Value: "22", Required: true},
			{Name: "用户名", Key: "username", Type: "input", Required: true},
			{
				Name: "认证方式", Key: "auth_type", Type: "radio",
				Options: []ConfigOption{{Value: "password", Label: "密码"}, {Value: "key", Label: "私钥"}},
				Value:   "password",
			},
			{Name: "密码", Key: "password", Type: "input"},
			{Name: "私钥内容", Key: "private_key", Type: "textarea"},
			{Name: "是否Windows", Key: "windows", Type: "radio", Options: []ConfigOption{{Value: "0", Label: "否"}, {Value: "1", Label: "是"}}, Value: "0",
				Note: "Windows系统需要先安装OpenSSH"},
		},
		DeployConfig: []ConfigField{
			{Name: "证书保存路径", Key: "cert_path", Type: "input", Placeholder: "/path/to/cert.pem", Required: true},
			{Name: "私钥保存路径", Key: "key_path", Type: "input", Placeholder: "/path/to/key.pem", Required: true},
			{Name: "上传前执行命令", Key: "cmd_pre", Type: "textarea", Placeholder: "可留空，上传前执行脚本命令"},
			{Name: "上传完执行命令", Key: "cmd", Type: "textarea", Placeholder: "可留空，每行一条命令，如：service nginx reload"},
			{Name: "部署域名", Key: "domain", Type: "input", Placeholder: "可选，用于替换路径中的{domain}占位符"},
		},
		DeployNote: "请确保路径存在且有写入权限，Windows路径使用/代替\\，且路径以/开头",
	})

	// 阿里云CDN（DeployConfig 与 deploy/config_cloud.go 中 aliyun_cdn 的 TaskInputs 一致）
	Register("aliyun_cdn", nil, ProviderConfig{
		Type:     "aliyun_cdn",
		Name:     "阿里云",
		Icon:     "aliyun.png",
		Note:     "部署证书到阿里云 CDN/DCDN（专用通道，与控制台「阿里云」多产品账户区分）",
		IsDeploy: true,
		Config: []ConfigField{
			{Name: "AccessKeyId", Key: "access_key_id", Type: "input", Required: true},
			{Name: "AccessKeySecret", Key: "access_key_secret", Type: "input", Required: true},
			{
				Name: "使用代理服务器", Key: "proxy", Type: "radio",
				Options: []ConfigOption{{Value: "0", Label: "否"}, {Value: "1", Label: "是"}},
				Value:   "0",
			},
		},
		DeployConfig: []ConfigField{
			{Name: "域名", Key: "domains", Type: "textarea", Placeholder: "每行一个域名", Required: true},
		},
	})

	// 阿里云 DCDN（与 CDN 通道共用实现，账户类型为 aliyun + product=dcdn 时解析为此部署器）
	Register("aliyun_dcdn", nil, ProviderConfig{
		Type:     "aliyun_dcdn",
		Name:     "阿里云 DCDN",
		Icon:     "aliyun.png",
		Note:     "部署证书到阿里云全站加速 DCDN（与 CDN 接口相同）",
		IsDeploy: true,
		Config: []ConfigField{
			{Name: "AccessKeyId", Key: "access_key_id", Type: "input", Required: true},
			{Name: "AccessKeySecret", Key: "access_key_secret", Type: "input", Required: true},
			{
				Name: "使用代理服务器", Key: "proxy", Type: "radio",
				Options: []ConfigOption{{Value: "0", Label: "否"}, {Value: "1", Label: "是"}},
				Value:   "0",
			},
		},
		DeployConfig: []ConfigField{
			{Name: "域名", Key: "domains", Type: "textarea", Placeholder: "每行一个域名", Required: true},
		},
	})

	// 腾讯云（与 deploy/config_cloud 及 dnsmgr tencent.php 能力对齐：按产品部署到 CDN / EdgeOne / CLB 等）
	Register("tencent_cdn", nil, ProviderConfig{
		Type:     "tencent_cdn",
		Name:     "腾讯云",
		Icon:     "tencent.png",
		Note:     "支持部署到腾讯云 CDN、EdgeOne(EO)、CLB、COS、WAF、TKE、SCF、轻量、DDoS 等；请在任务中选择产品与实例/域名",
		IsDeploy: true,
		Config: []ConfigField{
			{Name: "SecretId", Key: "secret_id", Type: "input", Required: true},
			{Name: "SecretKey", Key: "secret_key", Type: "input", Required: true},
			{
				Name: "使用代理服务器", Key: "proxy", Type: "radio",
				Options: []ConfigOption{{Value: "0", Label: "否"}, {Value: "1", Label: "是"}},
				Value:   "0",
			},
		},
		DeployConfig: []ConfigField{
			{Name: "要部署的产品", Key: "product", Type: "select", Required: true, Value: "cdn", Options: []ConfigOption{
				{Value: "cdn", Label: "内容分发网络 CDN"},
				{Value: "teo", Label: "边缘安全加速 EdgeOne(EO)"},
				{Value: "waf", Label: "Web应用防火墙 WAF"},
				{Value: "cos", Label: "对象存储 COS"},
				{Value: "clb", Label: "负载均衡 CLB"},
				{Value: "live", Label: "云直播 LIVE"},
				{Value: "vod", Label: "云点播 VOD"},
				{Value: "tke", Label: "容器服务 TKE"},
				{Value: "scf", Label: "云函数 SCF"},
				{Value: "lighthouse", Label: "轻量应用服务器"},
				{Value: "ddos", Label: "DDoS 高防包"},
				{Value: "upload", Label: "仅上传到证书管理"},
			}},
			{Name: "所属地域ID", Key: "regionid", Type: "input", Placeholder: "如: ap-guangzhou", Show: "product=='clb'||product=='cos'||product=='tke'||product=='scf'||product=='lighthouse'", Required: true},
			{Name: "WAF 地域", Key: "region", Type: "input", Placeholder: "如: ap-guangzhou（与控制台地域一致）", Show: "product=='waf'", Required: true},
			{Name: "负载均衡 ID", Key: "clb_id", Type: "input", Show: "product=='clb'", Required: true},
			{Name: "监听器 ID", Key: "clb_listener_id", Type: "input", Show: "product=='clb'"},
			{Name: "SNI 域名", Key: "clb_domain", Type: "input", Placeholder: "开启 SNI 时填写规则域名", Show: "product=='clb'"},
			{Name: "存储桶名称", Key: "cos_bucket", Type: "input", Show: "product=='cos'", Required: true},
			{Name: "EO 站点类型", Key: "site_type", Type: "select", Value: "cn", Options: []ConfigOption{
				{Value: "cn", Label: "中国站"},
				{Value: "intl", Label: "国际站(teo.intl)"},
			}, Show: "product=='teo'"},
			{Name: "站点 ID (ZoneId)", Key: "site_id", Type: "input", Show: "product=='teo'", Required: true},
			{Name: "TKE 集群 ID", Key: "tke_cluster_id", Type: "input", Show: "product=='tke'", Required: true},
			{Name: "TKE 命名空间", Key: "tke_namespace", Type: "input", Show: "product=='tke'", Required: true},
			{Name: "TKE Secret 名称", Key: "tke_secret", Type: "input", Show: "product=='tke'", Required: true},
			{Name: "实例 ID", Key: "lighthouse_id", Type: "input", Placeholder: "轻量实例 ID 或 DDoS 实例 ID", Show: "product=='lighthouse'||product=='ddos'", Required: true},
			{Name: "绑定的域名", Key: "domain", Type: "textarea", Placeholder: "多个域名用逗号或换行分隔；CDN/EO/WAF 等填加速域名", Show: "product!='clb'&&product!='upload'&&product!='tke'", Required: true},
		},
	})

	// AWS CloudFront（与 deploy/config_cloud.go 中 aws_cloudfront TaskInputs 一致；未填分发 ID 时可用订单域名自动查找）
	Register("aws_cloudfront", nil, ProviderConfig{
		Type:     "aws_cloudfront",
		Name:     "AWS CloudFront",
		Icon:     "aws.png",
		Note:     "部署证书到 AWS CloudFront CDN 分发",
		IsDeploy: true,
		Config: []ConfigField{
			{Name: "AccessKeyId", Key: "access_key_id", Type: "input", Required: true},
			{Name: "AccessKeySecret", Key: "access_key_secret", Type: "input", Required: true},
		},
		DeployConfig: []ConfigField{
			{Name: "分发ID", Key: "distribution_id", Type: "input", Placeholder: "留空则仅上传证书到ACM；也可留空由订单域名查找分发"},
		},
	})

	// AWS ACM（与 CloudFront 通道共用实现；统一账户 aws + product=acm 时解析为此部署器）
	Register("aws_acm", nil, ProviderConfig{
		Type:     "aws_acm",
		Name:     "AWS Certificate Manager",
		Icon:     "aws.png",
		Note:     "导入证书到 us-east-1 的 ACM（与 CloudFront 使用同一实现；可不关联 CloudFront 分发）",
		IsDeploy: true,
		Config: []ConfigField{
			{Name: "AccessKeyId", Key: "access_key_id", Type: "input", Required: true},
			{Name: "AccessKeySecret", Key: "access_key_secret", Type: "input", Required: true},
		},
		DeployConfig: []ConfigField{
			{Name: "分发ID", Key: "distribution_id", Type: "input", Placeholder: "留空则仅导入 ACM，不更新 CloudFront"},
		},
	})

	// 七牛云（DeployConfig 与 deploy/config_cloud.go 中 qiniu TaskInputs 一致）
	Register("qiniu", nil, ProviderConfig{
		Type:     "qiniu",
		Name:     "七牛云",
		Icon:     "qiniu.png",
		Note:     "支持部署到七牛云 CDN / OSS 等",
		IsDeploy: true,
		Config: []ConfigField{
			{Name: "AccessKey", Key: "access_key", Type: "input", Required: true},
			{Name: "SecretKey", Key: "secret_key", Type: "input", Required: true},
			{
				Name: "使用代理服务器", Key: "proxy", Type: "radio",
				Options: []ConfigOption{{Value: "0", Label: "否"}, {Value: "1", Label: "是"}},
				Value:   "0",
			},
		},
		DeployConfig: []ConfigField{
			{Name: "要部署的产品", Key: "product", Type: "select", Required: true, Value: "cdn", Options: []ConfigOption{
				{Value: "cdn", Label: "CDN"},
				{Value: "oss", Label: "OSS"},
				{Value: "upload", Label: "上传到证书管理"},
			}},
			{Name: "绑定的域名", Key: "domain", Type: "input", Placeholder: "多个域名可使用,分隔", Show: "product!='upload'", Required: true},
		},
	})

	// 又拍云（与 deploy/config_cloud.go：无 TaskInputs，由订单域名注入后绑定）
	Register("upyun", nil, ProviderConfig{
		Type:     "upyun",
		Name:     "又拍云",
		Icon:     "upyun.png",
		Note:     "支持部署到又拍云 CDN",
		IsDeploy: true,
		Config: []ConfigField{
			{Name: "Token", Key: "token", Type: "input", Required: true},
			{
				Name: "使用代理服务器", Key: "proxy", Type: "radio",
				Options: []ConfigOption{{Value: "0", Label: "否"}, {Value: "1", Label: "是"}},
				Value:   "0",
			},
		},
		DeployConfig: []ConfigField{},
		DeployNote:   "系统会根据关联SSL证书的域名自动更新",
	})

	// 本地部署
	Register("local", nil, ProviderConfig{
		Type:     "local",
		Name:     "本地部署",
		Icon:     "local.png",
		Note:     "部署证书到本地服务器",
		IsDeploy: true,
		Config: []ConfigField{
			{Name: "证书路径", Key: "cert_path", Type: "input", Placeholder: "证书文件保存路径", Required: true},
			{Name: "私钥路径", Key: "key_path", Type: "input", Placeholder: "私钥文件保存路径", Required: true},
			{Name: "重启命令", Key: "reload_cmd", Type: "input", Placeholder: "证书更新后执行的命令"},
		},
		DeployConfig: []ConfigField{
			{Name: "部署域名", Key: "domain", Type: "input", Placeholder: "可选，用于替换路径中的{domain}占位符"},
		},
		DeployNote: "支持路径变量 {domain}，更新后可执行重启命令",
	})

	// FTP部署
	Register("ftp", nil, ProviderConfig{
		Type:     "ftp",
		Name:     "FTP服务器",
		Icon:     "server.png",
		Note:     "部署证书到FTP服务器",
		IsDeploy: true,
		Config: []ConfigField{
			{Name: "FTP地址", Key: "host", Type: "input", Required: true},
			{Name: "FTP端口", Key: "port", Type: "input", Value: "21", Required: true},
			{Name: "用户名", Key: "username", Type: "input", Required: true},
			{Name: "密码", Key: "password", Type: "input", Required: true},
		},
		DeployConfig: []ConfigField{
			{Name: "证书路径", Key: "cert_path", Type: "input", Placeholder: "/path/to/cert.pem", Required: true},
			{Name: "私钥路径", Key: "key_path", Type: "input", Placeholder: "/path/to/key.pem", Required: true},
			{Name: "部署域名", Key: "domain", Type: "input", Placeholder: "可选，用于替换路径中的{domain}占位符"},
		},
		DeployNote: "请确保路径存在且有写入权限，支持路径变量 {domain}",
	})

	// 雷池WAF（与 deploy/config_selfhosted.go：TaskInputs 为空，由任务注入订单域名）
	Register("safeline", nil, ProviderConfig{
		Type:     "safeline",
		Name:     "雷池WAF",
		Icon:     "safeline.png",
		Note:     "部署证书到雷池WAF",
		IsDeploy: true,
		Config: []ConfigField{
			{Name: "控制台地址", Key: "url", Type: "input", Placeholder: "如: https://192.168.1.100:9443", Required: true},
			{Name: "API Token", Key: "token", Type: "input", Required: true},
			{
				Name: "使用代理服务器", Key: "proxy", Type: "radio",
				Options: []ConfigOption{{Value: "0", Label: "否"}, {Value: "1", Label: "是"}},
				Value:   "0",
			},
		},
		DeployConfig: []ConfigField{},
		DeployNote:   "系统会根据关联SSL证书的域名自动更新",
	})

	// 1Panel（账户字段与 deploy/config_selfhosted opanel 对齐：key；DeployConfig 与 TaskInputs 对齐）
	Register("1panel", nil, ProviderConfig{
		Type:     "1panel",
		Name:     "1Panel",
		Icon:     "1panel.png",
		Note:     "更新 1Panel 证书管理内的 SSL 证书",
		IsDeploy: true,
		Config: []ConfigField{
			{Name: "面板地址", Key: "url", Type: "input", Placeholder: "如: http://192.168.1.100:8090", Required: true},
			{Name: "接口密钥", Key: "key", Type: "input", Placeholder: "1Panel API 接口密钥", Required: true},
			{Name: "API版本", Key: "version", Type: "select", Options: []ConfigOption{
				{Value: "v1", Label: "1.x (v1)"},
				{Value: "v2", Label: "2.x (v2)"},
			}, Value: "v2"},
			{
				Name: "使用代理服务器", Key: "proxy", Type: "radio",
				Options: []ConfigOption{{Value: "0", Label: "否"}, {Value: "1", Label: "是"}},
				Value:   "0",
			},
		},
		DeployConfig: []ConfigField{
			{Name: "部署类型", Key: "type", Type: "radio", Required: true, Options: []ConfigOption{
				{Value: "0", Label: "更新已有证书"},
				{Value: "3", Label: "面板本身"},
			}, Value: "0"},
			{Name: "证书ID", Key: "id", Type: "input", Placeholder: "在证书列表查看ID", Show: "type==0"},
		},
	})

	// Cdnfly（DeployConfig 与 deploy/config_selfhosted.go TaskInputs 一致）
	Register("cdnfly", nil, ProviderConfig{
		Type:     "cdnfly",
		Name:     "Cdnfly",
		Icon:     "cdnfly.png",
		Note:     "部署证书到Cdnfly CDN",
		IsDeploy: true,
		Config: []ConfigField{
			{Name: "API地址", Key: "url", Type: "input", Placeholder: "如: https://cdn.example.com", Required: true},
			{Name: "用户ID", Key: "user_id", Type: "input", Required: true},
			{Name: "API密钥", Key: "api_key", Type: "input", Required: true},
			{
				Name: "使用代理服务器", Key: "proxy", Type: "radio",
				Options: []ConfigOption{{Value: "0", Label: "否"}, {Value: "1", Label: "是"}},
				Value:   "0",
			},
		},
		DeployConfig: []ConfigField{
			{Name: "证书ID", Key: "id", Type: "input", Placeholder: "留空则为添加证书"},
		},
	})

	// LeCDN（DeployConfig 与 deploy/config_selfhosted.go TaskInputs 一致）
	Register("lecdn", nil, ProviderConfig{
		Type:     "lecdn",
		Name:     "LeCDN",
		Icon:     "lecdn.png",
		Note:     "部署证书到LeCDN",
		IsDeploy: true,
		Config: []ConfigField{
			{Name: "API地址", Key: "url", Type: "input", Placeholder: "如: https://lecdn.example.com", Required: true},
			{Name: "API密钥", Key: "api_key", Type: "input", Required: true},
			{
				Name: "使用代理服务器", Key: "proxy", Type: "radio",
				Options: []ConfigOption{{Value: "0", Label: "否"}, {Value: "1", Label: "是"}},
				Value:   "0",
			},
		},
		DeployConfig: []ConfigField{
			{Name: "证书ID", Key: "id", Type: "input", Placeholder: "留空则为添加证书"},
		},
	})

	// GoEdge（与 deploy/config_selfhosted.go：账户字段 accessKeyId/accessKey + TaskNote）
	Register("goedge", nil, ProviderConfig{
		Type:     "goedge",
		Name:     "GoEdge",
		Icon:     "goedge.png",
		Note:     "支持 GoEdge 与 FlexCDN",
		IsDeploy: true,
		Config: []ConfigField{
			{Name: "HTTP API地址", Key: "url", Type: "input", Placeholder: "如: https://goedge.example.com", Required: true},
			{Name: "AccessKey ID", Key: "accessKeyId", Type: "input", Required: true},
			{Name: "AccessKey密钥", Key: "accessKey", Type: "input", Required: true},
			{Name: "用户类型", Key: "usertype", Type: "radio", Options: []ConfigOption{
				{Value: "user", Label: "平台用户"},
				{Value: "admin", Label: "系统用户"},
			}, Value: "user"},
			{Name: "系统类型", Key: "systype", Type: "radio", Options: []ConfigOption{
				{Value: "0", Label: "GoEdge"},
				{Value: "1", Label: "FlexCDN"},
			}, Value: "0"},
			{
				Name: "使用代理服务器", Key: "proxy", Type: "radio",
				Options: []ConfigOption{{Value: "0", Label: "否"}, {Value: "1", Label: "是"}},
				Value:   "0",
			},
		},
		DeployConfig: []ConfigField{},
		DeployNote:   "系统会根据关联SSL证书的域名自动更新",
	})

	// Kangle用户
	Register("kangle", nil, ProviderConfig{
		Type:     "kangle",
		Name:     "Kangle用户",
		Icon:     "kangle.png",
		Note:     "部署证书到Kangle站点",
		IsDeploy: true,
		Config: []ConfigField{
			{Name: "面板地址", Key: "url", Type: "input", Placeholder: "如: http://192.168.1.100:3312", Required: true},
			{Name: "用户名", Key: "username", Type: "input", Required: true},
			{Name: "密码", Key: "password", Type: "input", Required: true},
			{
				Name: "使用代理服务器", Key: "proxy", Type: "radio",
				Options: []ConfigOption{{Value: "0", Label: "否"}, {Value: "1", Label: "是"}},
				Value:   "0",
			},
		},
	})

	// Kangle管理员
	Register("kangle_admin", nil, ProviderConfig{
		Type:     "kangle_admin",
		Name:     "Kangle管理员",
		Icon:     "kangle.png",
		Note:     "部署证书到Kangle站点(管理员)",
		IsDeploy: true,
		Config: []ConfigField{
			{Name: "面板地址", Key: "url", Type: "input", Placeholder: "如: http://192.168.1.100:3311", Required: true},
			{Name: "用户名", Key: "username", Type: "input", Required: true},
			{Name: "密码", Key: "password", Type: "input", Required: true},
			{
				Name: "使用代理服务器", Key: "proxy", Type: "radio",
				Options: []ConfigOption{{Value: "0", Label: "否"}, {Value: "1", Label: "是"}},
				Value:   "0",
			},
		},
	})

	// MW面板
	Register("mwpanel", nil, ProviderConfig{
		Type:     "mwpanel",
		Name:     "MW面板",
		Icon:     "mw.png",
		Note:     "部署证书到MW面板",
		IsDeploy: true,
		Config: []ConfigField{
			{Name: "面板地址", Key: "url", Type: "input", Placeholder: "如: http://192.168.1.100:7200", Required: true},
			{Name: "API密钥", Key: "api_key", Type: "input", Required: true},
			{
				Name: "使用代理服务器", Key: "proxy", Type: "radio",
				Options: []ConfigOption{{Value: "0", Label: "否"}, {Value: "1", Label: "是"}},
				Value:   "0",
			},
		},
	})

	// 堡垒云WAF
	Register("baolei", nil, ProviderConfig{
		Type:     "baolei",
		Name:     "堡垒云WAF",
		Icon:     "baolei.png",
		Note:     "部署证书到堡垒云WAF",
		IsDeploy: true,
		Config: []ConfigField{
			{Name: "API地址", Key: "url", Type: "input", Placeholder: "如: https://api.baolei.com", Required: true},
			{Name: "API密钥", Key: "api_key", Type: "input", Required: true},
			{
				Name: "使用代理服务器", Key: "proxy", Type: "radio",
				Options: []ConfigOption{{Value: "0", Label: "否"}, {Value: "1", Label: "是"}},
				Value:   "0",
			},
		},
	})

	// 群晖面板
	Register("synology", nil, ProviderConfig{
		Type:     "synology",
		Name:     "群晖面板",
		Icon:     "synology.png",
		Note:     "部署证书到群晖NAS,支持DSM 6.x/7.x版本",
		IsDeploy: true,
		Config: []ConfigField{
			{Name: "DSM地址", Key: "url", Type: "input", Placeholder: "如: https://192.168.1.100:5001", Required: true},
			{Name: "用户名", Key: "username", Type: "input", Required: true},
			{Name: "密码", Key: "password", Type: "input", Required: true},
			{Name: "OTP码", Key: "otp", Type: "input", Placeholder: "如启用二步验证则填写"},
			{
				Name: "使用代理服务器", Key: "proxy", Type: "radio",
				Options: []ConfigOption{{Value: "0", Label: "否"}, {Value: "1", Label: "是"}},
				Value:   "0",
			},
		},
		DeployConfig: []ConfigField{
			{Name: "群晖证书描述", Key: "desc", Type: "input", Placeholder: "留空则根据证书通用名匹配"},
		},
	})

	// Lucky（与 deploy/config_selfhosted.go Inputs / TaskNote）
	Register("lucky", nil, ProviderConfig{
		Type:     "lucky",
		Name:     "Lucky",
		Icon:     "lucky.png",
		Note:     "更新 Lucky 证书",
		IsDeploy: true,
		Config: []ConfigField{
			{Name: "面板地址", Key: "url", Type: "input", Placeholder: "如: http://192.168.1.100:16601", Required: true},
			{Name: "安全入口", Key: "path", Type: "input", Placeholder: "如有路径前缀则填写"},
			{Name: "OpenToken", Key: "opentoken", Type: "input", Required: true},
			{
				Name: "使用代理服务器", Key: "proxy", Type: "radio",
				Options: []ConfigOption{{Value: "0", Label: "否"}, {Value: "1", Label: "是"}},
				Value:   "0",
			},
		},
		DeployConfig: []ConfigField{},
		DeployNote:   "系统会根据关联SSL证书的域名自动更新",
	})

	// 飞牛OS（与 deploy/config_selfhosted：无 TaskInputs，依赖订单域名）
	Register("fnos", nil, ProviderConfig{
		Type:     "fnos",
		Name:     "飞牛OS",
		Icon:     "fnos.png",
		Note:     "更新飞牛OS的证书",
		IsDeploy: true,
		Config: []ConfigField{
			{Name: "主机地址", Key: "host", Type: "input", Placeholder: "如: 192.168.1.100", Required: true},
			{Name: "SSH端口", Key: "port", Type: "input", Value: "22", Required: true},
			{Name: "用户名", Key: "username", Type: "input", Required: true},
			{Name: "密码", Key: "password", Type: "input", Required: true},
			{
				Name: "使用代理服务器", Key: "proxy", Type: "radio",
				Options: []ConfigOption{{Value: "0", Label: "否"}, {Value: "1", Label: "是"}},
				Value:   "0",
			},
		},
		DeployConfig: []ConfigField{},
		DeployNote:   "系统会根据关联SSL证书的域名自动更新",
	})

	// Proxmox VE（与 deploy/config_selfhosted.go Inputs / TaskInputs）
	Register("proxmox", nil, ProviderConfig{
		Type:     "proxmox",
		Name:     "Proxmox VE",
		Icon:     "proxmox.png",
		Note:     "部署到 PVE 节点",
		IsDeploy: true,
		Config: []ConfigField{
			{Name: "面板地址", Key: "url", Type: "input", Placeholder: "如: https://192.168.1.100:8006", Required: true},
			{Name: "API令牌ID", Key: "api_user", Type: "input", Required: true},
			{Name: "API令牌密钥", Key: "api_key", Type: "input", Required: true},
			{
				Name: "使用代理服务器", Key: "proxy", Type: "radio",
				Options: []ConfigOption{{Value: "0", Label: "否"}, {Value: "1", Label: "是"}},
				Value:   "0",
			},
		},
		DeployConfig: []ConfigField{
			{Name: "节点名称", Key: "node", Type: "input", Placeholder: "如: pve", Required: true},
		},
	})

	// K8S
	Register("k8s", nil, ProviderConfig{
		Type:     "k8s",
		Name:     "K8S",
		Icon:     "k8s.png",
		Note:     "部署到K8S集群的Secret/Ingress",
		IsDeploy: true,
		Config: []ConfigField{
			{Name: "Kubeconfig", Key: "kubeconfig", Type: "textarea", Placeholder: "Kubeconfig内容", Required: true},
		},
		DeployConfig: []ConfigField{
			{Name: "命名空间", Key: "namespace", Type: "input", Placeholder: "如: default", Required: true, Value: "default"},
			{Name: "Secret名称", Key: "secret_name", Type: "input", Placeholder: "如: tls-secret", Required: true},
			{Name: "Ingress名称列表", Key: "ingresses", Type: "textarea", Placeholder: "可选，多个用逗号或换行分隔"},
		},
	})

	// 筷子面板
	Register("chopsticks", nil, ProviderConfig{
		Type:     "chopsticks",
		Name:     "筷子面板",
		Icon:     "chopsticks.png",
		Note:     "部署筷子面板 v2.5+ 版本使用",
		IsDeploy: true,
		Config: []ConfigField{
			{Name: "面板地址", Key: "url", Type: "input", Placeholder: "如: http://192.168.1.100:8888", Required: true},
			{Name: "API密钥", Key: "api_key", Type: "input", Required: true},
			{
				Name: "使用代理服务器", Key: "proxy", Type: "radio",
				Options: []ConfigOption{{Value: "0", Label: "否"}, {Value: "1", Label: "是"}},
				Value:   "0",
			},
		},
	})

	// 小皮面板（与 deploy/config_selfhosted 中 xp：url/apikey + TaskInputs sites；部署注册名 xp / xpanel）
	Register("xpanel", nil, ProviderConfig{
		Type:     "xpanel",
		Name:     "小皮面板",
		Icon:     "xpanel.png",
		Note:     "部署证书到小皮面板",
		IsDeploy: true,
		Config: []ConfigField{
			{Name: "面板地址", Key: "url", Type: "input", Placeholder: "如: http://192.168.1.100:9080", Required: true},
			{Name: "接口密钥", Key: "apikey", Type: "input", Required: true},
			{
				Name: "使用代理服务器", Key: "proxy", Type: "radio",
				Options: []ConfigOption{{Value: "0", Label: "否"}, {Value: "1", Label: "是"}},
				Value:   "0",
			},
		},
		DeployConfig: []ConfigField{
			{Name: "网站名称列表", Key: "sites", Type: "textarea", Placeholder: "每行一个网站名称", Required: true},
		},
	})

	// 华为云CDN（DeployConfig 与 deploy/config_cloud.go 中 huawei_cdn TaskInputs 一致）
	Register("huawei_cdn", nil, ProviderConfig{
		Type:     "huawei_cdn",
		Name:     "华为云",
		Icon:     "huawei.png",
		Note:     "部署证书到华为云 CDN",
		IsDeploy: true,
		Config: []ConfigField{
			{Name: "Access Key", Key: "access_key", Type: "input", Required: true},
			{Name: "Secret Key", Key: "secret_key", Type: "input", Required: true},
			{
				Name: "使用代理服务器", Key: "proxy", Type: "radio",
				Options: []ConfigOption{{Value: "0", Label: "否"}, {Value: "1", Label: "是"}},
				Value:   "0",
			},
		},
		DeployConfig: []ConfigField{
			{Name: "域名", Key: "domains", Type: "textarea", Placeholder: "每行一个域名", Required: true},
		},
	})

	// UCloud（DeployConfig 与 deploy/config_cloud.go 中 ucloud TaskInputs 一致；部署实现注册为 ucloud_cdn）
	Register("ucloud", nil, ProviderConfig{
		Type:     "ucloud",
		Name:     "UCloud",
		Icon:     "ucloud.png",
		Note:     "部署证书到 UCloud UCDN",
		IsDeploy: true,
		Config: []ConfigField{
			{Name: "公钥", Key: "public_key", Type: "input", Required: true},
			{Name: "私钥", Key: "private_key", Type: "input", Required: true},
			{
				Name: "使用代理服务器", Key: "proxy", Type: "radio",
				Options: []ConfigOption{{Value: "0", Label: "否"}, {Value: "1", Label: "是"}},
				Value:   "0",
			},
		},
		DeployConfig: []ConfigField{
			{Name: "云分发资源ID", Key: "domain_id", Type: "input", Required: true},
		},
	})
}
