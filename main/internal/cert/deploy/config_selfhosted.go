package deploy

import "main/internal/cert"

func init() {
	registerSelfHostedConfigs()
}

func registerSelfHostedConfigs() {
	// 宝塔面板
	registerDeployConfig(DeployProviderConfig{
		Type:  "btpanel",
		Name:  "宝塔面板",
		Class: ClassSelfHosted,
		Icon:  "bt.png",
		Desc:  "支持部署到宝塔面板搭建的站点",
		Inputs: []cert.ConfigField{
			{Name: "面板地址", Key: "url", Type: "input", Placeholder: "如: http://192.168.1.100:8888", Required: true},
			{Name: "接口密钥", Key: "key", Type: "input", Placeholder: "宝塔面板API接口密钥", Required: true},
			{Name: "面板版本", Key: "version", Type: "radio", Options: []cert.ConfigOption{{Value: "0", Label: "Linux面板"}, {Value: "1", Label: "Win极速版"}}, Value: "0"},
		},
		TaskInputs: []cert.ConfigField{
			{Name: "部署类型", Key: "type", Type: "radio", Required: true, Options: []cert.ConfigOption{
				{Value: "0", Label: "网站证书"},
				{Value: "3", Label: "Docker网站"},
				{Value: "2", Label: "邮局域名"},
				{Value: "1", Label: "面板本身"},
			}, Value: "0"},
			{Name: "网站名称列表", Key: "sites", Type: "textarea", Placeholder: "每行一个网站名称", Show: "type==0||type==2||type==3", Required: true},
			{Name: "是否IIS站点", Key: "is_iis", Type: "radio", Options: []cert.ConfigOption{{Value: "0", Label: "否"}, {Value: "1", Label: "是"}}, Show: "type==0", Value: "0"},
		},
	})

	registerDeployConfig(DeployProviderConfig{
		Type:  "acepanel",
		Name:  "ACE Panel",
		Class: ClassSelfHosted,
		Icon:  "acepanel.png",
		Desc:  "部署证书到 ACE Panel",
		Inputs: []cert.ConfigField{
			{Name: "面板地址", Key: "url", Type: "input", Required: true},
			{Name: "令牌ID", Key: "id", Type: "input", Required: true},
			{Name: "访问令牌", Key: "token", Type: "input", Required: true},
		},
		TaskInputs: []cert.ConfigField{
			{Name: "部署类型", Key: "type", Type: "radio", Required: true, Options: []cert.ConfigOption{{Value: "0", Label: "网站证书"}, {Value: "1", Label: "面板本身"}}, Value: "0"},
			{Name: "网站名称列表", Key: "sites", Type: "textarea", Placeholder: "每行一个网站名称", Show: "type==0", Required: true},
		},
	})

	registerDeployConfig(DeployProviderConfig{
		Type:  "amh",
		Name:  "AMH",
		Class: ClassSelfHosted,
		Icon:  "amh.png",
		Desc:  "部署证书到 AMH 面板",
		Inputs: []cert.ConfigField{
			{Name: "面板地址", Key: "url", Type: "input", Required: true},
			{Name: "接口密钥", Key: "apikey", Type: "input", Required: true},
		},
		TaskInputs: []cert.ConfigField{
			{Name: "环境名称", Key: "env_name", Type: "input", Required: true},
			{Name: "网站标识域名", Key: "vhost_name", Type: "textarea", Placeholder: "每行一个虚拟主机标识", Required: true},
		},
	})

	registerDeployConfig(DeployProviderConfig{
		Type:  "btwin",
		Name:  "宝塔 Windows 极速版",
		Class: ClassSelfHosted,
		Icon:  "bt.png",
		Desc:  "部署证书到宝塔 Windows 极速版",
		Inputs: []cert.ConfigField{
			{Name: "面板地址", Key: "url", Type: "input", Required: true},
			{Name: "接口密钥", Key: "key", Type: "input", Required: true},
		},
		TaskInputs: []cert.ConfigField{
			{Name: "部署类型", Key: "type", Type: "radio", Required: true, Options: []cert.ConfigOption{{Value: "0", Label: "网站证书"}, {Value: "1", Label: "面板本身"}}, Value: "0"},
			{Name: "网站名称列表", Key: "sites", Type: "textarea", Placeholder: "每行一个网站名称", Show: "type==0", Required: true},
			{Name: "是否IIS站点", Key: "is_iis", Type: "radio", Options: []cert.ConfigOption{{Value: "0", Label: "否"}, {Value: "1", Label: "是"}}, Show: "type==0", Value: "0"},
		},
	})

	// 1Panel
	registerDeployConfig(DeployProviderConfig{
		Type:  "opanel",
		Name:  "1Panel",
		Class: ClassSelfHosted,
		Icon:  "opanel.png",
		Desc:  "更新1Panel证书管理内的SSL证书",
		Inputs: []cert.ConfigField{
			{Name: "面板地址", Key: "url", Type: "input", Placeholder: "如: http://192.168.1.100:8888", Required: true},
			{Name: "接口密钥", Key: "key", Type: "input", Placeholder: "1Panel API接口密钥", Required: true},
			{Name: "版本", Key: "version", Type: "radio", Options: []cert.ConfigOption{{Value: "v1", Label: "1.x"}, {Value: "v2", Label: "2.x"}}, Value: "v2"},
		},
		TaskInputs: []cert.ConfigField{
			{Name: "部署类型", Key: "type", Type: "radio", Required: true, Options: []cert.ConfigOption{
				{Value: "0", Label: "更新已有证书"},
				{Value: "3", Label: "面板本身"},
			}, Value: "0"},
			{Name: "证书ID", Key: "id", Type: "input", Placeholder: "在证书列表查看ID", Show: "type==0"},
		},
	})

	// 雷池WAF
	registerDeployConfig(DeployProviderConfig{
		Type:     "safeline",
		Name:     "雷池WAF",
		Class:    ClassSelfHosted,
		Icon:     "safeline.png",
		Desc:     "部署证书到雷池WAF",
		TaskNote: "系统会根据关联SSL证书的域名自动更新",
		Inputs: []cert.ConfigField{
			{Name: "控制台地址", Key: "url", Type: "input", Placeholder: "如: https://192.168.1.100:9443", Required: true},
			{Name: "API Token", Key: "token", Type: "input", Required: true},
		},
		TaskInputs: []cert.ConfigField{},
	})

	// Nginx Proxy Manager
	registerDeployConfig(DeployProviderConfig{
		Type:     "nginxproxymanager",
		Name:     "Nginx Proxy Manager",
		Class:    ClassSelfHosted,
		Icon:     "cloud.png",
		Desc:     "更新 Nginx Proxy Manager 的自定义证书并自动绑定 Proxy Host",
		Note:     "填写 Nginx Proxy Manager 面板地址与登录账号密码，系统将通过官方 API 登录并执行证书更新。",
		TaskNote: "如填写证书ID则优先更新该自定义证书；留空时系统会根据当前证书订单的域名在 NPM 中匹配 Proxy Host，并在首次成功后自动保存证书ID，后续续期优先走该ID，不再依赖域名匹配。",
		Inputs: []cert.ConfigField{
			{Name: "面板地址", Key: "url", Type: "input", Placeholder: "Nginx Proxy Manager 面板地址", Note: "填写规则如：http://192.168.1.100:81 ，不要带 /api 等后缀", Required: true},
			{Name: "登录邮箱", Key: "email", Type: "input", Placeholder: "NPM 登录邮箱", Validator: "email", Required: true},
			{Name: "登录密码", Key: "password", Type: "input", Placeholder: "NPM 登录密码", Required: true},
		},
		TaskInputs: []cert.ConfigField{
			{Name: "证书ID", Key: "id", Type: "input", Placeholder: "留空则按域名匹配 Proxy Host 并自动回填", Note: "优先级最高。填写后将直接更新该自定义证书ID；仅支持 NPM 中 provider 为 other 的自定义证书。"},
			{Name: "Proxy Host ID", Key: "host_id", Type: "input", Placeholder: "可留空，留空则按域名自动匹配", Note: "可选。未填写证书ID时，若填写此项则仅处理指定 Proxy Host；若留空则按当前证书订单域名自动查找匹配的 Proxy Host。"},
		},
	})

	// 堡塔云WAF
	registerDeployConfig(DeployProviderConfig{
		Type:  "btwaf",
		Name:  "堡塔云WAF",
		Class: ClassSelfHosted,
		Icon:  "bt.png",
		Desc:  "部署证书到堡塔云WAF",
		Inputs: []cert.ConfigField{
			{Name: "面板地址", Key: "url", Type: "input", Placeholder: "如: http://192.168.1.100:8379", Required: true},
			{Name: "接口密钥", Key: "key", Type: "input", Required: true},
		},
		TaskInputs: []cert.ConfigField{
			{Name: "部署类型", Key: "type", Type: "radio", Required: true, Options: []cert.ConfigOption{
				{Value: "0", Label: "网站证书"},
				{Value: "1", Label: "面板本身"},
			}, Value: "0"},
			{Name: "网站名称列表", Key: "sites", Type: "textarea", Placeholder: "每行一个网站名称", Show: "type==0", Required: true},
		},
	})

	// Cdnfly
	registerDeployConfig(DeployProviderConfig{
		Type: "cdnfly",
		Name: "Cdnfly",
		Class: ClassSelfHosted,
		Icon: "waf.png",
		Desc: "部署证书到Cdnfly",
		Inputs: []cert.ConfigField{
			{Name: "控制台地址", Key: "url", Type: "input", Required: true},
			{Name: "认证方式", Key: "auth", Type: "radio", Options: []cert.ConfigOption{{Value: "0", Label: "接口密钥"}, {Value: "1", Label: "模拟登录"}}, Value: "0"},
			{Name: "api_key", Key: "api_key", Type: "input", Show: "auth==0", Required: true},
			{Name: "api_secret", Key: "api_secret", Type: "input", Show: "auth==0", Required: true},
			{Name: "登录账号", Key: "username", Type: "input", Show: "auth==1", Required: true},
			{Name: "登录密码", Key: "password", Type: "input", Show: "auth==1", Required: true},
		},
		TaskInputs: []cert.ConfigField{
			{Name: "证书ID", Key: "id", Type: "input", Placeholder: "留空则为添加证书"},
		},
	})

	// LeCDN
	registerDeployConfig(DeployProviderConfig{
		Type:  "lecdn",
		Name:  "LeCDN",
		Class: ClassSelfHosted,
		Icon:  "waf.png",
		Desc:  "部署证书到LeCDN",
		Inputs: []cert.ConfigField{
			{Name: "控制台地址", Key: "url", Type: "input", Required: true},
			{Name: "认证方式", Key: "auth", Type: "radio", Options: []cert.ConfigOption{{Value: "0", Label: "账号密码"}, {Value: "1", Label: "API令牌"}}, Value: "0"},
			{Name: "API访问令牌", Key: "api_key", Type: "input", Show: "auth==1", Required: true},
			{Name: "邮箱地址", Key: "email", Type: "input", Show: "auth==0", Required: true},
			{Name: "密码", Key: "password", Type: "input", Show: "auth==0", Required: true},
		},
		TaskInputs: []cert.ConfigField{
			{Name: "证书ID", Key: "id", Type: "input", Placeholder: "留空则为添加证书"},
		},
	})

	// GoEdge
	registerDeployConfig(DeployProviderConfig{
		Type:     "goedge",
		Name:     "GoEdge",
		Class:    ClassSelfHosted,
		Icon:     "waf.png",
		Desc:     "支持GoEdge与FlexCDN",
		TaskNote: "系统会根据关联SSL证书的域名自动更新",
		Inputs: []cert.ConfigField{
			{Name: "HTTP API地址", Key: "url", Type: "input", Required: true},
			{Name: "AccessKey ID", Key: "accessKeyId", Type: "input", Required: true},
			{Name: "AccessKey密钥", Key: "accessKey", Type: "input", Required: true},
			{Name: "用户类型", Key: "usertype", Type: "radio", Options: []cert.ConfigOption{{Value: "user", Label: "平台用户"}, {Value: "admin", Label: "系统用户"}}, Value: "user"},
			{Name: "系统类型", Key: "systype", Type: "radio", Options: []cert.ConfigOption{{Value: "0", Label: "GoEdge"}, {Value: "1", Label: "FlexCDN"}}, Value: "0"},
		},
		TaskInputs: []cert.ConfigField{},
	})

	// Kangle用户
	registerDeployConfig(DeployProviderConfig{
		Type: "kangle",
		Name: "Kangle用户",
		Class: ClassSelfHosted,
		Icon: "host.png",
		Desc: "支持虚拟主机与CDN站点",
		Inputs: []cert.ConfigField{
			{Name: "面板地址", Key: "url", Type: "input", Required: true},
			{Name: "认证方式", Key: "auth", Type: "radio", Options: []cert.ConfigOption{{Value: "0", Label: "网站密码"}, {Value: "1", Label: "面板安全码"}}, Value: "0"},
			{Name: "网站用户名", Key: "username", Type: "input", Required: true},
			{Name: "网站密码", Key: "password", Type: "input", Show: "auth==0", Required: true},
			{Name: "面板安全码", Key: "skey", Type: "input", Show: "auth==1", Required: true},
		},
		TaskInputs: []cert.ConfigField{
			{Name: "部署类型", Key: "type", Type: "radio", Required: true, Options: []cert.ConfigOption{
				{Value: "0", Label: "网站SSL证书"},
				{Value: "1", Label: "单域名SSL证书"},
			}, Value: "0"},
			{Name: "CDN域名列表", Key: "domains", Type: "textarea", Placeholder: "每行一个域名", Show: "type==1", Required: true},
		},
	})

	// Kangle管理员
	registerDeployConfig(DeployProviderConfig{
		Type: "kangleadmin",
		Name: "Kangle管理员",
		Class: ClassSelfHosted,
		Icon: "host.png",
		Desc: "支持虚拟主机与CDN站点",
		Inputs: []cert.ConfigField{
			{Name: "面板地址", Key: "url", Type: "input", Required: true},
			{Name: "管理员面板路径", Key: "path", Type: "input", Placeholder: "留空默认为/admin"},
			{Name: "管理员用户名", Key: "username", Type: "input", Required: true},
			{Name: "面板安全码", Key: "skey", Type: "input", Required: true},
		},
		TaskInputs: []cert.ConfigField{
			{Name: "网站用户名", Key: "name", Type: "input", Required: true},
			{Name: "部署类型", Key: "type", Type: "radio", Required: true, Options: []cert.ConfigOption{
				{Value: "0", Label: "网站SSL证书"},
				{Value: "1", Label: "单域名SSL证书"},
			}, Value: "0"},
			{Name: "CDN域名列表", Key: "domains", Type: "textarea", Placeholder: "每行一个域名", Show: "type==1", Required: true},
		},
	})

	// MW面板
	registerDeployConfig(DeployProviderConfig{
		Type:  "mwpanel",
		Name:  "MW面板",
		Class: ClassSelfHosted,
		Icon:  "mwpanel.ico",
		Desc:  "部署证书到MW面板",
		Inputs: []cert.ConfigField{
			{Name: "面板地址", Key: "url", Type: "input", Required: true},
			{Name: "应用ID", Key: "appid", Type: "input", Required: true},
			{Name: "应用密钥", Key: "appsecret", Type: "input", Required: true},
		},
		TaskInputs: []cert.ConfigField{
			{Name: "部署类型", Key: "type", Type: "radio", Required: true, Options: []cert.ConfigOption{
				{Value: "0", Label: "网站证书"},
				{Value: "1", Label: "面板本身"},
			}, Value: "0"},
			{Name: "网站名称列表", Key: "sites", Type: "textarea", Placeholder: "每行一个网站名称", Show: "type==0", Required: true},
		},
	})

	// 耗子面板
	registerDeployConfig(DeployProviderConfig{
		Type: "ratpanel",
		Name: "耗子面板",
		Class: ClassSelfHosted,
		Icon: "ratpanel.ico",
		Desc: "支持耗子面板 v2.5+ 版本",
		Inputs: []cert.ConfigField{
			{Name: "面板地址", Key: "url", Type: "input", Required: true},
			{Name: "访问令牌ID", Key: "id", Type: "input", Required: true},
			{Name: "访问令牌", Key: "token", Type: "input", Required: true},
		},
		TaskInputs: []cert.ConfigField{
			{Name: "部署类型", Key: "type", Type: "radio", Required: true, Options: []cert.ConfigOption{
				{Value: "0", Label: "网站证书"},
				{Value: "1", Label: "面板本身"},
			}, Value: "0"},
			{Name: "网站名称列表", Key: "sites", Type: "textarea", Placeholder: "每行一个网站名称", Show: "type==0", Required: true},
		},
	})

	// 小皮面板
	registerDeployConfig(DeployProviderConfig{
		Type:  "xp",
		Name:  "小皮面板",
		Class: ClassSelfHosted,
		Icon:  "xp.png",
		Desc:  "部署证书到小皮面板",
		Inputs: []cert.ConfigField{
			{Name: "面板地址", Key: "url", Type: "input", Required: true},
			{Name: "接口密钥", Key: "apikey", Type: "input", Required: true},
		},
		TaskInputs: []cert.ConfigField{
			{Name: "网站名称列表", Key: "sites", Type: "textarea", Placeholder: "每行一个网站名称", Required: true},
		},
	})

	// 群晖面板
	registerDeployConfig(DeployProviderConfig{
		Type: "synology",
		Name: "群晖面板",
		Class: ClassSelfHosted,
		Icon: "synology.png",
		Desc: "支持群晖DSM 6.x/7.x版本",
		Inputs: []cert.ConfigField{
			{Name: "面板地址", Key: "url", Type: "input", Required: true},
			{Name: "登录账号", Key: "username", Type: "input", Required: true},
			{Name: "登录密码", Key: "password", Type: "input", Required: true},
			{Name: "群晖版本", Key: "version", Type: "radio", Options: []cert.ConfigOption{{Value: "0", Label: "7.x"}, {Value: "1", Label: "6.x"}}, Value: "0"},
		},
		TaskInputs: []cert.ConfigField{
			{Name: "群晖证书描述", Key: "desc", Type: "input", Placeholder: "留空则根据证书通用名匹配"},
		},
	})

	// Lucky
	registerDeployConfig(DeployProviderConfig{
		Type:     "lucky",
		Name:     "Lucky",
		Class:    ClassSelfHosted,
		Icon:     "lucky.png",
		Desc:     "更新Lucky证书",
		TaskNote: "系统会根据关联SSL证书的域名自动更新",
		Inputs: []cert.ConfigField{
			{Name: "面板地址", Key: "url", Type: "input", Required: true},
			{Name: "安全入口", Key: "path", Type: "input"},
			{Name: "OpenToken", Key: "opentoken", Type: "input", Required: true},
		},
		TaskInputs: []cert.ConfigField{},
	})

	// 飞牛OS
	registerDeployConfig(DeployProviderConfig{
		Type:     "fnos",
		Name:     "飞牛OS",
		Class:    ClassSelfHosted,
		Icon:     "fnos.png",
		Desc:     "更新飞牛OS的证书",
		TaskNote: "系统会根据关联SSL证书的域名自动更新",
		Inputs: []cert.ConfigField{
			{Name: "主机地址", Key: "host", Type: "input", Required: true},
			{Name: "SSH端口", Key: "port", Type: "input", Value: "22", Required: true},
			{Name: "用户名", Key: "username", Type: "input", Required: true},
			{Name: "密码", Key: "password", Type: "input", Required: true},
		},
		TaskInputs: []cert.ConfigField{},
	})

	// Proxmox VE
	registerDeployConfig(DeployProviderConfig{
		Type: "proxmox",
		Name: "Proxmox VE",
		Class: ClassSelfHosted,
		Icon: "proxmox.ico",
		Desc: "部署到PVE节点",
		Inputs: []cert.ConfigField{
			{Name: "面板地址", Key: "url", Type: "input", Required: true},
			{Name: "API令牌ID", Key: "api_user", Type: "input", Required: true},
			{Name: "API令牌密钥", Key: "api_key", Type: "input", Required: true},
		},
		TaskInputs: []cert.ConfigField{
			{Name: "节点名称", Key: "node", Type: "input", Required: true},
		},
	})

	// K8S
	registerDeployConfig(DeployProviderConfig{
		Type: "k8s",
		Name: "K8S",
		Class: ClassSelfHosted,
		Icon: "server.png",
		Desc: "部署到K8S集群的Secret和Ingress",
		Inputs: []cert.ConfigField{
			{Name: "名称", Key: "name", Type: "input", Required: true},
			{Name: "kubeconfig", Key: "kubeconfig", Type: "textarea", Required: true},
		},
		TaskInputs: []cert.ConfigField{
			{Name: "命名空间", Key: "namespace", Type: "input", Value: "default", Required: true},
			{Name: "Secret名称", Key: "secret_name", Type: "input", Required: true},
			{Name: "Ingress名称", Key: "ingresses", Type: "input", Placeholder: "多个用逗号分隔，可留空"},
		},
	})

	// 南墙WAF
	registerDeployConfig(DeployProviderConfig{
		Type:  "uusec",
		Name:  "南墙WAF",
		Class: ClassSelfHosted,
		Icon:  "waf.png",
		Desc:  "部署证书到南墙WAF",
		Inputs: []cert.ConfigField{
			{Name: "控制台地址", Key: "url", Type: "input", Required: true},
			{Name: "用户名", Key: "username", Type: "input", Required: true},
			{Name: "密码", Key: "password", Type: "input", Required: true},
		},
		TaskInputs: []cert.ConfigField{
			{Name: "证书ID", Key: "id", Type: "input", Required: true},
			{Name: "证书名称", Key: "name", Type: "input", Required: true},
		},
	})

	// CloudPanel
	registerDeployConfig(DeployProviderConfig{
		Type:     "cloudpanel",
		Name:     "CloudPanel",
		Class:    ClassSelfHosted,
		Icon:     "cloudpanel.svg",
		Note:     "使用管理员账号密码登录面板。暂不支持已开启双因素认证的账户。",
		Desc:     "",
		TaskNote: "",
		Inputs: []cert.ConfigField{
			{Name: "面板地址", Key: "url", Type: "input", Placeholder: "CloudPanel 面板地址", Note: "填写规则如：https://192.168.1.100:8443 ，不要带其他后缀", Required: true},
			{Name: "登录用户名", Key: "username", Type: "input", Placeholder: "管理员用户名", Required: true},
			{Name: "登录密码", Key: "password", Type: "input", Placeholder: "管理员密码", Required: true},
		},
		TaskInputs: []cert.ConfigField{
			{Name: "网站域名列表", Key: "sites", Type: "textarea", Placeholder: "填写要部署证书的网站主域名，每行一个", Note: "必须与 CloudPanel 站点列表中的主域名一致", Required: true},
		},
	})

	// DirectAdmin
	registerDeployConfig(DeployProviderConfig{
		Type:     "directadmin",
		Name:     "DirectAdmin",
		Class:    ClassSelfHosted,
		Icon:     "directadmin.png",
		Desc:     "通过 DirectAdmin 官方 API 自动部署域名 SSL 证书",
		Note:     "使用 HTTPS 和 HTTP Basic Authentication。认证密码可填写 DirectAdmin 账户密码；如服务商开放 Login Key，也可填写 Login Key。",
		TaskNote: "填写 DirectAdmin 中已存在的目标域名。多个域名可用换行或逗号分隔；系统会分别调用 CMD_API_SSL 更新证书。",
		Inputs: []cert.ConfigField{
			{Name: "面板地址", Key: "url", Type: "input", Placeholder: "https://server.example.com:2222", Note: "必须使用 HTTPS，不要带 CMD_API_SSL 等路径", Required: true},
			{Name: "用户名", Key: "username", Type: "input", Placeholder: "DirectAdmin 登录用户名", Required: true},
			{Name: "认证密码", Key: "password", Type: "input", Placeholder: "DirectAdmin 账户密码或 Login Key", Required: true},
		},
		TaskInputs: []cert.ConfigField{
			{Name: "DirectAdmin 域名", Key: "domain", Type: "textarea", Placeholder: "example.com\nsecond.example", Note: "必须是该 DirectAdmin 账户中已存在的域名；多个域名可换行或用逗号分隔。", Required: true},
		},
	})

	// AxisNow
	registerDeployConfig(DeployProviderConfig{
		Type:  "axisnow",
		Name:  "AxisNow",
		Class: ClassCloudService,
		Icon:  "axisnow.png",
		Desc:  "支持上传证书到AxisNow平台",
		Note:  "支持上传证书到AxisNow平台",
		Inputs: []cert.ConfigField{
			{Name: "租户名", Key: "name", Type: "input", Required: true},
			{Name: "API 令牌", Key: "token", Type: "input", Required: true},
		},
		TaskInputs: []cert.ConfigField{},
	})

	// 全栈云
	registerDeployConfig(DeployProviderConfig{
		Type:  "qzyun",
		Name:  "全栈云",
		Class: ClassCloudService,
		Icon:  "qzyun.svg",
		Desc:  "更新全栈云证书管理中的证书",
		Note:  "填写 panel.qzyun.cn 的登录邮箱和密码",
		Inputs: []cert.ConfigField{
			{Name: "登录邮箱", Key: "email", Type: "input", Placeholder: "全栈云账号邮箱", Validator: "email", Required: true},
			{Name: "密码", Key: "password", Type: "input", Placeholder: "全栈云登录密码", Required: true},
		},
		TaskInputs: []cert.ConfigField{
			{Name: "证书名称或文档ID", Key: "certificate", Type: "input", Placeholder: "例如 qzyun.cn", Note: "填写全栈云“证书管理”中显示的证书名称；首次部署后会自动保存内部文档ID", Required: true},
			{Name: "绑定域名", Key: "domain", Type: "input", Placeholder: "例如 qzyun.cn，可留空", Note: "填写后会将证书自动绑定到全栈云对应域名"},
		},
	})
}
