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

	// 阿里云CDN
	Register("aliyun_cdn", nil, ProviderConfig{
		Type:     "aliyun_cdn",
		Name:     "阿里云",
		Icon:     "aliyun.png",
		Note:     "支持部署到阿里云CDN、OSS、WAF等服务",
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
			{Name: "绑定的域名", Key: "domain", Type: "textarea", Placeholder: "填写要部署证书的域名，多个可用逗号或换行分隔", Required: true},
		},
	})

	// 腾讯云CDN
	Register("tencent_cdn", nil, ProviderConfig{
		Type:     "tencent_cdn",
		Name:     "腾讯云",
		Icon:     "tencent.png",
		Note:     "支持部署到腾讯云CDN、EO、CLB、COS、TKE、SCF等服务",
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
			{Name: "绑定的域名", Key: "domain", Type: "textarea", Placeholder: "填写要部署证书的域名，多个可用逗号或换行分隔", Required: true},
		},
	})

	// AWS CloudFront
	Register("aws_cloudfront", nil, ProviderConfig{
		Type:     "aws_cloudfront",
		Name:     "AWS CloudFront",
		Icon:     "aws.png",
		Note:     "部署证书到CloudFront",
		IsDeploy: true,
		Config: []ConfigField{
			{Name: "AccessKeyId", Key: "access_key_id", Type: "input", Required: true},
			{Name: "AccessKeySecret", Key: "access_key_secret", Type: "input", Required: true},
		},
		DeployConfig: []ConfigField{
			{Name: "分发ID", Key: "distribution_id", Type: "input", Placeholder: "CloudFront Distribution ID"},
			{Name: "域名", Key: "domain", Type: "input", Placeholder: "用于自动查找分发ID的域名"},
		},
	})

	// 七牛云CDN
	Register("qiniu", nil, ProviderConfig{
		Type:     "qiniu",
		Name:     "七牛云",
		Icon:     "qiniu.png",
		Note:     "部署证书到七牛云CDN",
		IsDeploy: true,
		Config: []ConfigField{
			{Name: "AccessKey", Key: "access_key", Type: "input", Required: true},
			{Name: "SecretKey", Key: "secret_key", Type: "input", Required: true},
		},
		DeployConfig: []ConfigField{
			{Name: "域名", Key: "domain", Type: "input", Placeholder: "要部署证书的CDN域名"},
		},
	})

	// 又拍云CDN
	Register("upyun", nil, ProviderConfig{
		Type:     "upyun",
		Name:     "又拍云",
		Icon:     "upyun.png",
		Note:     "部署证书到又拍云CDN",
		IsDeploy: true,
		Config: []ConfigField{
			{Name: "Token", Key: "token", Type: "input", Required: true},
		},
		DeployConfig: []ConfigField{
			{Name: "域名", Key: "domain", Type: "input", Placeholder: "要部署证书的CDN域名"},
		},
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

	// 雷池WAF
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
		DeployConfig: []ConfigField{
			{Name: "域名列表", Key: "domainList", Type: "textarea", Placeholder: "填写要更新证书的域名，多个用逗号分隔"},
		},
		DeployNote: "系统会根据关联SSL证书的域名，自动更新对应证书",
	})

	// 1Panel
	Register("1panel", nil, ProviderConfig{
		Type:     "1panel",
		Name:     "1Panel",
		Icon:     "1panel.png",
		Note:     "部署证书到1Panel面板",
		IsDeploy: true,
		Config: []ConfigField{
			{Name: "面板地址", Key: "url", Type: "input", Placeholder: "如: http://192.168.1.100:8090", Required: true},
			{Name: "API密钥", Key: "api_key", Type: "input", Required: true},
			{
				Name: "使用代理服务器", Key: "proxy", Type: "radio",
				Options: []ConfigOption{{Value: "0", Label: "否"}, {Value: "1", Label: "是"}},
				Value:   "0",
			},
		},
	})

	// Cdnfly
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
	})

	// LeCDN
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
	})

	// GoEdge
	Register("goedge", nil, ProviderConfig{
		Type:     "goedge",
		Name:     "GoEdge",
		Icon:     "goedge.png",
		Note:     "部署证书到GoEdge/FlexCDN",
		IsDeploy: true,
		Config: []ConfigField{
			{Name: "API地址", Key: "url", Type: "input", Placeholder: "如: https://goedge.example.com", Required: true},
			{Name: "Access Key", Key: "access_key", Type: "input", Required: true},
			{Name: "Access Secret", Key: "access_secret", Type: "input", Required: true},
			{
				Name: "使用代理服务器", Key: "proxy", Type: "radio",
				Options: []ConfigOption{{Value: "0", Label: "否"}, {Value: "1", Label: "是"}},
				Value:   "0",
			},
		},
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
			{Name: "证书描述", Key: "desc", Type: "input", Placeholder: "用于匹配或新建证书描述"},
		},
	})

	// Lucky
	Register("lucky", nil, ProviderConfig{
		Type:     "lucky",
		Name:     "Lucky",
		Icon:     "lucky.png",
		Note:     "部署Lucky证书",
		IsDeploy: true,
		Config: []ConfigField{
			{Name: "Lucky地址", Key: "url", Type: "input", Placeholder: "如: http://192.168.1.100:16601", Required: true},
			{Name: "API Token", Key: "token", Type: "input", Required: true},
			{
				Name: "使用代理服务器", Key: "proxy", Type: "radio",
				Options: []ConfigOption{{Value: "0", Label: "否"}, {Value: "1", Label: "是"}},
				Value:   "0",
			},
		},
	})

	// 飞牛OS
	Register("fnos", nil, ProviderConfig{
		Type:     "fnos",
		Name:     "飞牛OS",
		Icon:     "fnos.png",
		Note:     "部署飞牛OS的证书",
		IsDeploy: true,
		Config: []ConfigField{
			{Name: "API地址", Key: "url", Type: "input", Placeholder: "如: http://192.168.1.100:5000", Required: true},
			{Name: "用户名", Key: "username", Type: "input", Required: true},
			{Name: "密码", Key: "password", Type: "input", Required: true},
			{
				Name: "使用代理服务器", Key: "proxy", Type: "radio",
				Options: []ConfigOption{{Value: "0", Label: "否"}, {Value: "1", Label: "是"}},
				Value:   "0",
			},
		},
	})

	// Proxmox VE
	Register("proxmox", nil, ProviderConfig{
		Type:     "proxmox",
		Name:     "Proxmox VE",
		Icon:     "proxmox.png",
		Note:     "部署到PVET证书",
		IsDeploy: true,
		Config: []ConfigField{
			{Name: "PVE地址", Key: "url", Type: "input", Placeholder: "如: https://192.168.1.100:8006", Required: true},
			{Name: "用户名", Key: "username", Type: "input", Placeholder: "如: root@pam", Required: true},
			{Name: "密码", Key: "password", Type: "input", Required: true},
			{Name: "节点名称", Key: "node", Type: "input", Placeholder: "默认: pve", Value: "pve"},
			{
				Name: "使用代理服务器", Key: "proxy", Type: "radio",
				Options: []ConfigOption{{Value: "0", Label: "否"}, {Value: "1", Label: "是"}},
				Value:   "0",
			},
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

	// 小皮面板
	Register("xpanel", nil, ProviderConfig{
		Type:     "xpanel",
		Name:     "小皮面板",
		Icon:     "xpanel.png",
		Note:     "部署证书到小皮面板",
		IsDeploy: true,
		Config: []ConfigField{
			{Name: "面板地址", Key: "url", Type: "input", Placeholder: "如: http://192.168.1.100:9080", Required: true},
			{Name: "用户名", Key: "username", Type: "input", Required: true},
			{Name: "密码", Key: "password", Type: "input", Required: true},
			{
				Name: "使用代理服务器", Key: "proxy", Type: "radio",
				Options: []ConfigOption{{Value: "0", Label: "否"}, {Value: "1", Label: "是"}},
				Value:   "0",
			},
		},
	})

	// 华为云CDN
	Register("huawei_cdn", nil, ProviderConfig{
		Type:     "huawei_cdn",
		Name:     "华为云",
		Icon:     "huawei.png",
		Note:     "支持部署到华为云CDN、WAF、ELB等服务",
		IsDeploy: true,
		Config: []ConfigField{
			{Name: "Access Key ID", Key: "access_key_id", Type: "input", Required: true},
			{Name: "Secret Access Key", Key: "access_key_secret", Type: "input", Required: true},
			{Name: "Endpoint", Key: "endpoint", Type: "input", Value: "cdn.myhuaweicloud.com"},
			{
				Name: "使用代理服务器", Key: "proxy", Type: "radio",
				Options: []ConfigOption{{Value: "0", Label: "否"}, {Value: "1", Label: "是"}},
				Value:   "0",
			},
		},
		DeployConfig: []ConfigField{
			{Name: "绑定的域名", Key: "domain", Type: "textarea", Placeholder: "填写要部署证书的域名，多个可用逗号或换行分隔", Required: true},
			{
				Name: "强制HTTPS", Key: "force_https", Type: "radio",
				Options: []ConfigOption{{Value: "0", Label: "否"}, {Value: "1", Label: "是"}},
				Value:   "0",
			},
			{
				Name: "启用HTTP/2", Key: "http2", Type: "radio",
				Options: []ConfigOption{{Value: "0", Label: "否"}, {Value: "1", Label: "是"}},
				Value:   "1",
			},
			{Name: "证书名称", Key: "cert_name", Type: "input", Placeholder: "留空自动生成"},
		},
	})

	// UCloud
	Register("ucloud", nil, ProviderConfig{
		Type:     "ucloud",
		Name:     "UCloud",
		Icon:     "ucloud.png",
		Note:     "部署证书到UCloud CDN",
		IsDeploy: true,
		Config: []ConfigField{
			{Name: "公钥", Key: "public_key", Type: "input", Required: true},
			{Name: "私钥", Key: "private_key", Type: "input", Required: true},
			{Name: "项目ID", Key: "project_id", Type: "input", Required: true},
			{
				Name: "使用代理服务器", Key: "proxy", Type: "radio",
				Options: []ConfigOption{{Value: "0", Label: "否"}, {Value: "1", Label: "是"}},
				Value:   "0",
			},
		},
	})
}
