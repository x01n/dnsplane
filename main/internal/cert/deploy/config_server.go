package deploy

import "main/internal/cert"

func init() {
	registerServerConfigs()
}

func registerServerConfigs() {
	// SSH服务器
	registerDeployConfig(DeployProviderConfig{
		Type:     "ssh",
		Name:     "SSH服务器",
		Class:    ClassServer,
		Icon:     "server.png",
		Desc:     "通过SSH连接到Linux/Windows服务器并部署证书",
		TaskNote: "请确保路径存在且有写入权限",
		Inputs: []cert.ConfigField{
			{Name: "主机地址", Key: "host", Type: "input", Placeholder: "填写IP地址或域名", Required: true},
			{Name: "端口", Key: "port", Type: "input", Value: "22", Required: true},
			{Name: "认证方式", Key: "auth", Type: "radio", Options: []cert.ConfigOption{{Value: "0", Label: "密码认证"}, {Value: "1", Label: "密钥认证"}}, Value: "0"},
			{Name: "用户名", Key: "username", Type: "input", Value: "root", Required: true},
			{Name: "密码", Key: "password", Type: "input", Show: "auth==0", Required: true},
			{Name: "私钥", Key: "privatekey", Type: "textarea", Placeholder: "填写PEM格式私钥内容", Show: "auth==1", Required: true},
			{Name: "私钥密码", Key: "passphrase", Type: "input", Show: "auth==1"},
			{Name: "是否Windows", Key: "windows", Type: "radio", Options: []cert.ConfigOption{{Value: "0", Label: "否"}, {Value: "1", Label: "是"}}, Value: "0"},
		},
		TaskInputs: []cert.ConfigField{
			{Name: "证书类型", Key: "format", Type: "select", Required: true, Value: "pem", Options: []cert.ConfigOption{
				{Value: "pem", Label: "PEM格式（Nginx/Apache等）"},
				{Value: "pfx", Label: "PFX格式（IIS/Tomcat）"},
			}},
			{Name: "证书保存路径", Key: "pem_cert_file", Type: "input", Placeholder: "/path/to/cert.pem", Show: "format=='pem'", Required: true},
			{Name: "私钥保存路径", Key: "pem_key_file", Type: "input", Placeholder: "/path/to/key.pem", Show: "format=='pem'", Required: true},
			{Name: "PFX证书保存路径", Key: "pfx_file", Type: "input", Placeholder: "/path/to/cert.pfx", Show: "format=='pfx'", Required: true},
			{Name: "PFX证书密码", Key: "pfx_pass", Type: "input", Show: "format=='pfx'"},
			{Name: "上传完操作", Key: "uptype", Type: "radio", Options: []cert.ConfigOption{{Value: "0", Label: "执行指定命令"}, {Value: "1", Label: "部署到IIS"}}, Value: "0", Show: "format=='pfx'"},
			{Name: "上传前执行命令", Key: "cmd_pre", Type: "textarea", Show: "format=='pem'||uptype==0"},
			{Name: "上传完执行命令", Key: "cmd", Type: "textarea", Placeholder: "每行一条命令，如：service nginx reload", Show: "format=='pem'||uptype==0"},
			{Name: "IIS绑定域名", Key: "iis_domain", Type: "input", Show: "format=='pfx'&&uptype==1"},
		},
	})

	// FTP服务器
	registerDeployConfig(DeployProviderConfig{
		Type:     "ftp",
		Name:     "FTP服务器",
		Class:    ClassServer,
		Icon:     "server.png",
		Desc:     "将证书上传到FTP服务器",
		TaskNote: "请确保路径存在且有写入权限",
		Inputs: []cert.ConfigField{
			{Name: "FTP地址", Key: "host", Type: "input", Placeholder: "填写IP地址或域名", Required: true},
			{Name: "FTP端口", Key: "port", Type: "input", Value: "21", Required: true},
			{Name: "用户名", Key: "username", Type: "input", Required: true},
			{Name: "密码", Key: "password", Type: "input", Required: true},
			{Name: "是否使用SSL", Key: "secure", Type: "radio", Options: []cert.ConfigOption{{Value: "0", Label: "否"}, {Value: "1", Label: "是"}}, Value: "0"},
			{Name: "被动模式", Key: "passive", Type: "radio", Options: []cert.ConfigOption{{Value: "0", Label: "否"}, {Value: "1", Label: "是"}}, Value: "1"},
		},
		TaskInputs: []cert.ConfigField{
			{Name: "证书类型", Key: "format", Type: "select", Required: true, Value: "pem", Options: []cert.ConfigOption{
				{Value: "pem", Label: "PEM格式（Nginx/Apache等）"},
				{Value: "pfx", Label: "PFX格式（IIS/Tomcat）"},
			}},
			{Name: "证书保存路径", Key: "pem_cert_file", Type: "input", Placeholder: "/path/to/cert.pem", Show: "format=='pem'", Required: true},
			{Name: "私钥保存路径", Key: "pem_key_file", Type: "input", Placeholder: "/path/to/key.pem", Show: "format=='pem'", Required: true},
			{Name: "PFX证书保存路径", Key: "pfx_file", Type: "input", Placeholder: "/path/to/cert.pfx", Show: "format=='pfx'", Required: true},
			{Name: "PFX证书密码", Key: "pfx_pass", Type: "input", Show: "format=='pfx'"},
		},
	})

	// 本地部署
	registerDeployConfig(DeployProviderConfig{
		Type:     "local",
		Name:     "本地部署",
		Class:    ClassServer,
		Icon:     "server.png",
		Desc:     "将证书保存到本地服务器",
		TaskNote: "请确保路径存在且有写入权限",
		Inputs: []cert.ConfigField{
			{Name: "名称", Key: "name", Type: "input", Placeholder: "仅用于区分", Required: true},
		},
		TaskInputs: []cert.ConfigField{
			{Name: "证书类型", Key: "format", Type: "select", Required: true, Value: "pem", Options: []cert.ConfigOption{
				{Value: "pem", Label: "PEM格式（Nginx/Apache等）"},
				{Value: "pfx", Label: "PFX格式（IIS/Tomcat）"},
				{Value: "jks", Label: "JKS格式（Java）"},
			}},
			{Name: "证书保存路径", Key: "pem_cert_file", Type: "input", Placeholder: "/path/to/cert.pem", Show: "format=='pem'", Required: true},
			{Name: "私钥保存路径", Key: "pem_key_file", Type: "input", Placeholder: "/path/to/key.pem", Show: "format=='pem'", Required: true},
			{Name: "PFX证书保存路径", Key: "pfx_file", Type: "input", Placeholder: "/path/to/cert.pfx", Show: "format=='pfx'", Required: true},
			{Name: "PFX证书密码", Key: "pfx_pass", Type: "input", Show: "format=='pfx'"},
			{Name: "JKS证书保存路径", Key: "jks_file", Type: "input", Placeholder: "/path/to/cert.jks", Show: "format=='jks'", Required: true},
			{Name: "JKS证书密码", Key: "jks_pass", Type: "input", Show: "format=='jks'", Required: true},
			{Name: "JKS别名", Key: "jks_alias", Type: "input", Value: "ssl", Show: "format=='jks'", Required: true},
			{Name: "部署完执行命令", Key: "cmd", Type: "textarea", Placeholder: "每行一条命令，如：service nginx reload"},
		},
	})
}
