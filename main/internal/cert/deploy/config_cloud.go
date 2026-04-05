package deploy

import "main/internal/cert"

func init() {
	registerCloudConfigs()
}

func registerCloudConfigs() {
	// 阿里云
	registerDeployConfig(DeployProviderConfig{
		Type:  "aliyun",
		Name:  "阿里云",
		Class: ClassCloudService,
		Icon:  "aliyun.png",
		Desc:  "支持部署到阿里云CDN、ESA、SLB、OSS、WAF等服务",
		Inputs: []cert.ConfigField{
			{Name: "AccessKeyId", Key: "AccessKeyId", Type: "input", Required: true},
			{Name: "AccessKeySecret", Key: "AccessKeySecret", Type: "input", Required: true},
			{Name: "使用代理", Key: "proxy", Type: "radio", Options: []cert.ConfigOption{{Value: "0", Label: "否"}, {Value: "1", Label: "是"}}, Value: "0"},
		},
		TaskInputs: []cert.ConfigField{
			{Name: "要部署的产品", Key: "product", Type: "select", Required: true, Value: "cdn", Options: []cert.ConfigOption{
				{Value: "cdn", Label: "内容分发CDN"},
				{Value: "dcdn", Label: "全站加速DCDN"},
				{Value: "esa", Label: "边缘安全加速ESA"},
				{Value: "oss", Label: "对象存储OSS"},
				{Value: "waf", Label: "Web应用防火墙3.0"},
				{Value: "clb", Label: "传统型负载均衡CLB"},
				{Value: "alb", Label: "应用型负载均衡ALB"},
				{Value: "nlb", Label: "网络型负载均衡NLB"},
				{Value: "live", Label: "视频直播"},
				{Value: "vod", Label: "视频点播"},
				{Value: "fc", Label: "函数计算"},
				{Value: "upload", Label: "上传到证书管理"},
			}},
			{Name: "ESA站点域名", Key: "esa_sitename", Type: "input", Show: "product=='esa'", Required: true},
			{Name: "Endpoint地址", Key: "oss_endpoint", Type: "input", Placeholder: "如: oss-cn-hangzhou.aliyuncs.com", Show: "product=='oss'", Required: true},
			{Name: "Bucket名称", Key: "oss_bucket", Type: "input", Show: "product=='oss'", Required: true},
			{Name: "所属地域", Key: "region", Type: "select", Options: []cert.ConfigOption{{Value: "cn-hangzhou", Label: "中国内地"}, {Value: "ap-southeast-1", Label: "非中国内地"}}, Value: "cn-hangzhou", Show: "product=='waf'||product=='esa'"},
			{Name: "所属地域ID", Key: "regionid", Type: "input", Placeholder: "如: cn-hangzhou", Show: "product=='clb'||product=='alb'||product=='nlb'", Value: "cn-hangzhou"},
			{Name: "负载均衡实例ID", Key: "clb_id", Type: "input", Show: "product=='clb'", Required: true},
			{Name: "HTTPS监听端口", Key: "clb_port", Type: "input", Value: "443", Show: "product=='clb'", Required: true},
			{Name: "监听ID", Key: "alb_listener_id", Type: "input", Show: "product=='alb'||product=='nlb'", Required: true},
			{Name: "绑定的域名", Key: "domain", Type: "input", Placeholder: "多个域名可使用,分隔", Show: "product!='clb'&&product!='alb'&&product!='nlb'&&product!='upload'", Required: true},
		},
	})

	// 腾讯云
	registerDeployConfig(DeployProviderConfig{
		Type:  "tencent",
		Name:  "腾讯云",
		Class: ClassCloudService,
		Icon:  "tencent.png",
		Desc:  "支持部署到腾讯云CDN、EO、CLB、COS等服务",
		Inputs: []cert.ConfigField{
			{Name: "SecretId", Key: "SecretId", Type: "input", Required: true},
			{Name: "SecretKey", Key: "SecretKey", Type: "input", Required: true},
			{Name: "使用代理", Key: "proxy", Type: "radio", Options: []cert.ConfigOption{{Value: "0", Label: "否"}, {Value: "1", Label: "是"}}, Value: "0"},
		},
		TaskInputs: []cert.ConfigField{
			{Name: "要部署的产品", Key: "product", Type: "select", Required: true, Value: "cdn", Options: []cert.ConfigOption{
				{Value: "cdn", Label: "内容分发网络CDN"},
				{Value: "teo", Label: "边缘安全加速EO"},
				{Value: "waf", Label: "Web应用防火墙WAF"},
				{Value: "cos", Label: "对象存储COS"},
				{Value: "clb", Label: "负载均衡CLB"},
				{Value: "live", Label: "云直播LIVE"},
				{Value: "vod", Label: "云点播VOD"},
				{Value: "upload", Label: "上传到证书管理"},
			}},
			{Name: "所属地域ID", Key: "regionid", Type: "input", Placeholder: "如: ap-guangzhou", Show: "product=='clb'||product=='cos'"},
			{Name: "负载均衡ID", Key: "clb_id", Type: "input", Show: "product=='clb'", Required: true},
			{Name: "监听器ID", Key: "clb_listener_id", Type: "input", Show: "product=='clb'"},
			{Name: "绑定的域名", Key: "clb_domain", Type: "input", Show: "product=='clb'"},
			{Name: "存储桶名称", Key: "cos_bucket", Type: "input", Show: "product=='cos'", Required: true},
			{Name: "站点ID", Key: "site_id", Type: "input", Show: "product=='teo'", Required: true},
			{Name: "绑定的域名", Key: "domain", Type: "input", Placeholder: "多个域名可使用,分隔", Show: "product!='clb'&&product!='upload'", Required: true},
		},
	})

	// 华为云
	registerDeployConfig(DeployProviderConfig{
		Type:  "huawei",
		Name:  "华为云",
		Class: ClassCloudService,
		Icon:  "huawei.ico",
		Desc:  "支持部署到华为云CDN、ELB、WAF等服务",
		Inputs: []cert.ConfigField{
			{Name: "AccessKeyId", Key: "AccessKeyId", Type: "input", Required: true},
			{Name: "SecretAccessKey", Key: "SecretAccessKey", Type: "input", Required: true},
			{Name: "使用代理", Key: "proxy", Type: "radio", Options: []cert.ConfigOption{{Value: "0", Label: "否"}, {Value: "1", Label: "是"}}, Value: "0"},
		},
		TaskInputs: []cert.ConfigField{
			{Name: "要部署的产品", Key: "product", Type: "select", Required: true, Value: "cdn", Options: []cert.ConfigOption{
				{Value: "cdn", Label: "内容分发网络CDN"},
				{Value: "elb", Label: "弹性负载均衡ELB"},
				{Value: "waf", Label: "Web应用防火墙WAF"},
				{Value: "obs", Label: "对象存储服务OBS"},
				{Value: "upload", Label: "上传到证书管理"},
			}},
			{Name: "Endpoint地址", Key: "obs_endpoint", Type: "input", Show: "product=='obs'", Required: true},
			{Name: "桶名称", Key: "obs_bucket", Type: "input", Show: "product=='obs'", Required: true},
			{Name: "绑定的域名", Key: "domain", Type: "input", Placeholder: "多个域名可使用,分隔", Show: "product=='cdn'||product=='obs'", Required: true},
			{Name: "项目ID", Key: "project_id", Type: "input", Show: "product=='elb'||product=='waf'", Required: true},
			{Name: "区域ID", Key: "region_id", Type: "input", Show: "product=='elb'||product=='waf'", Required: true},
			{Name: "证书ID", Key: "cert_id", Type: "input", Show: "product=='elb'||product=='waf'", Required: true},
		},
	})

	// 七牛云
	registerDeployConfig(DeployProviderConfig{
		Type:  "qiniu",
		Name:  "七牛云",
		Class: ClassCloudService,
		Icon:  "qiniu.ico",
		Desc:  "支持部署到七牛云CDN",
		Inputs: []cert.ConfigField{
			{Name: "AccessKey", Key: "AccessKey", Type: "input", Required: true},
			{Name: "SecretKey", Key: "SecretKey", Type: "input", Required: true},
			{Name: "使用代理", Key: "proxy", Type: "radio", Options: []cert.ConfigOption{{Value: "0", Label: "否"}, {Value: "1", Label: "是"}}, Value: "0"},
		},
		TaskInputs: []cert.ConfigField{
			{Name: "要部署的产品", Key: "product", Type: "select", Required: true, Value: "cdn", Options: []cert.ConfigOption{
				{Value: "cdn", Label: "CDN"},
				{Value: "oss", Label: "OSS"},
				{Value: "upload", Label: "上传到证书管理"},
			}},
			{Name: "绑定的域名", Key: "domain", Type: "input", Placeholder: "多个域名可使用,分隔", Show: "product!='upload'", Required: true},
		},
	})

	// 又拍云
	registerDeployConfig(DeployProviderConfig{
		Type:     "upyun",
		Name:     "又拍云",
		Class:    ClassCloudService,
		Icon:     "upyun.ico",
		Desc:     "支持部署到又拍云CDN",
		TaskNote: "系统会根据关联SSL证书的域名自动更新",
		Inputs: []cert.ConfigField{
			{Name: "用户名", Key: "username", Type: "input", Required: true},
			{Name: "密码", Key: "password", Type: "input", Required: true},
			{Name: "使用代理", Key: "proxy", Type: "radio", Options: []cert.ConfigOption{{Value: "0", Label: "否"}, {Value: "1", Label: "是"}}, Value: "0"},
		},
		TaskInputs: []cert.ConfigField{},
	})

	// 多吉云
	registerDeployConfig(DeployProviderConfig{
		Type:  "doge",
		Name:  "多吉云",
		Class: ClassCloudService,
		Icon:  "doge.png",
		Desc:  "支持部署到多吉云融合CDN",
		Inputs: []cert.ConfigField{
			{Name: "AccessKey", Key: "AccessKey", Type: "input", Required: true},
			{Name: "SecretKey", Key: "SecretKey", Type: "input", Required: true},
			{Name: "使用代理", Key: "proxy", Type: "radio", Options: []cert.ConfigOption{{Value: "0", Label: "否"}, {Value: "1", Label: "是"}}, Value: "0"},
		},
		TaskInputs: []cert.ConfigField{
			{Name: "CDN域名", Key: "domain", Type: "input", Placeholder: "多个域名可使用,分隔", Required: true},
		},
	})

	// 百度云
	registerDeployConfig(DeployProviderConfig{
		Type:  "baidu",
		Name:  "百度云",
		Class: ClassCloudService,
		Icon:  "baidu.ico",
		Desc:  "支持部署到百度云CDN、BLB",
		Inputs: []cert.ConfigField{
			{Name: "AccessKeyId", Key: "AccessKeyId", Type: "input", Required: true},
			{Name: "SecretAccessKey", Key: "SecretAccessKey", Type: "input", Required: true},
			{Name: "使用代理", Key: "proxy", Type: "radio", Options: []cert.ConfigOption{{Value: "0", Label: "否"}, {Value: "1", Label: "是"}}, Value: "0"},
		},
		TaskInputs: []cert.ConfigField{
			{Name: "要部署的产品", Key: "product", Type: "select", Required: true, Value: "cdn", Options: []cert.ConfigOption{
				{Value: "cdn", Label: "CDN"},
				{Value: "blb", Label: "负载均衡BLB"},
				{Value: "upload", Label: "上传到证书管理"},
			}},
			{Name: "绑定的域名", Key: "domain", Type: "input", Placeholder: "多个域名可使用,分隔", Show: "product=='cdn'", Required: true},
			{Name: "所属地域", Key: "region", Type: "select", Options: []cert.ConfigOption{
				{Value: "bj", Label: "北京"},
				{Value: "gz", Label: "广州"},
				{Value: "su", Label: "苏州"},
				{Value: "hkg", Label: "香港"},
			}, Value: "bj", Show: "product=='blb'"},
			{Name: "负载均衡实例ID", Key: "blb_id", Type: "input", Show: "product=='blb'", Required: true},
			{Name: "HTTPS监听端口", Key: "blb_port", Type: "input", Value: "443", Show: "product=='blb'", Required: true},
		},
	})

	// UCloud
	registerDeployConfig(DeployProviderConfig{
		Type:  "ucloud",
		Name:  "UCloud",
		Class: ClassCloudService,
		Icon:  "ucloud.ico",
		Desc:  "支持部署到UCDN",
		Inputs: []cert.ConfigField{
			{Name: "公钥", Key: "PublicKey", Type: "input", Required: true},
			{Name: "私钥", Key: "PrivateKey", Type: "input", Required: true},
		},
		TaskInputs: []cert.ConfigField{
			{Name: "云分发资源ID", Key: "domain_id", Type: "input", Required: true},
		},
	})

	// 火山引擎
	registerDeployConfig(DeployProviderConfig{
		Type:  "huoshan",
		Name:  "火山引擎",
		Class: ClassCloudService,
		Icon:  "huoshan.ico",
		Desc:  "支持部署到火山引擎CDN、CLB、TOS等",
		Inputs: []cert.ConfigField{
			{Name: "AccessKeyId", Key: "AccessKeyId", Type: "input", Required: true},
			{Name: "SecretAccessKey", Key: "SecretAccessKey", Type: "input", Required: true},
			{Name: "使用代理", Key: "proxy", Type: "radio", Options: []cert.ConfigOption{{Value: "0", Label: "否"}, {Value: "1", Label: "是"}}, Value: "0"},
		},
		TaskInputs: []cert.ConfigField{
			{Name: "要部署的产品", Key: "product", Type: "select", Required: true, Value: "cdn", Options: []cert.ConfigOption{
				{Value: "cdn", Label: "内容分发网络CDN"},
				{Value: "dcdn", Label: "全站加速DCDN"},
				{Value: "clb", Label: "负载均衡CLB"},
				{Value: "tos", Label: "对象存储TOS"},
				{Value: "live", Label: "视频直播"},
				{Value: "upload", Label: "上传到证书管理"},
			}},
			{Name: "Bucket域名", Key: "bucket_domain", Type: "input", Show: "product=='tos'", Required: true},
			{Name: "绑定的域名", Key: "domain", Type: "input", Placeholder: "多个域名可使用,分隔", Show: "product!='clb'&&product!='upload'", Required: true},
			{Name: "监听器ID", Key: "listener_id", Type: "input", Show: "product=='clb'", Required: true},
		},
	})

	// 天翼云
	registerDeployConfig(DeployProviderConfig{
		Type:  "ctyun",
		Name:  "天翼云",
		Class: ClassCloudService,
		Icon:  "ctyun.ico",
		Desc:  "支持部署到天翼云CDN",
		Inputs: []cert.ConfigField{
			{Name: "AccessKeyId", Key: "AccessKeyId", Type: "input", Required: true},
			{Name: "SecretAccessKey", Key: "SecretAccessKey", Type: "input", Required: true},
			{Name: "使用代理", Key: "proxy", Type: "radio", Options: []cert.ConfigOption{{Value: "0", Label: "否"}, {Value: "1", Label: "是"}}, Value: "0"},
		},
		TaskInputs: []cert.ConfigField{
			{Name: "要部署的产品", Key: "product", Type: "select", Required: true, Value: "cdn", Options: []cert.ConfigOption{
				{Value: "cdn", Label: "CDN加速"},
				{Value: "icdn", Label: "全站加速"},
			}},
			{Name: "绑定的域名", Key: "domain", Type: "input", Required: true},
		},
	})

	// 金山云
	registerDeployConfig(DeployProviderConfig{
		Type:  "ksyun",
		Name:  "金山云",
		Class: ClassCloudService,
		Icon:  "ksyun.ico",
		Desc:  "支持部署到金山云CDN",
		Inputs: []cert.ConfigField{
			{Name: "AccessKeyId", Key: "AccessKeyId", Type: "input", Required: true},
			{Name: "SecretAccessKey", Key: "SecretAccessKey", Type: "input", Required: true},
			{Name: "使用代理", Key: "proxy", Type: "radio", Options: []cert.ConfigOption{{Value: "0", Label: "否"}, {Value: "1", Label: "是"}}, Value: "0"},
		},
		TaskInputs: []cert.ConfigField{
			{Name: "绑定的域名", Key: "domain", Type: "input", Placeholder: "多个域名可使用,分隔", Required: true},
		},
	})

	// 白山云
	registerDeployConfig(DeployProviderConfig{
		Type:  "baishan",
		Name:  "白山云",
		Class: ClassCloudService,
		Icon:  "waf.png",
		Desc:  "替换白山云证书管理内的证书",
		Inputs: []cert.ConfigField{
			{Name: "账户名", Key: "account", Type: "input", Required: true},
			{Name: "token", Key: "token", Type: "input", Required: true},
			{Name: "使用代理", Key: "proxy", Type: "radio", Options: []cert.ConfigOption{{Value: "0", Label: "否"}, {Value: "1", Label: "是"}}, Value: "0"},
		},
		TaskInputs: []cert.ConfigField{
			{Name: "证书ID", Key: "id", Type: "input", Required: true},
		},
	})

	// 网宿科技
	registerDeployConfig(DeployProviderConfig{
		Type:  "wangsu",
		Name:  "网宿科技",
		Class: ClassCloudService,
		Icon:  "wangsu.ico",
		Desc:  "支持部署到网宿CDN",
		Inputs: []cert.ConfigField{
			{Name: "账号", Key: "username", Type: "input", Required: true},
			{Name: "APIKEY", Key: "apiKey", Type: "input", Required: true},
			{Name: "特殊KEY", Key: "spKey", Type: "input"},
			{Name: "使用代理", Key: "proxy", Type: "radio", Options: []cert.ConfigOption{{Value: "0", Label: "否"}, {Value: "1", Label: "是"}}, Value: "0"},
		},
		TaskInputs: []cert.ConfigField{
			{Name: "要部署的产品", Key: "product", Type: "select", Required: true, Value: "cdn", Options: []cert.ConfigOption{
				{Value: "cdn", Label: "CDN"},
				{Value: "cdnpro", Label: "CDN Pro"},
				{Value: "certificate", Label: "证书管理"},
			}},
			{Name: "绑定的域名", Key: "domains", Type: "input", Placeholder: "多个域名可使用,分隔", Show: "product=='cdn'", Required: true},
			{Name: "绑定的域名", Key: "domain", Type: "input", Show: "product=='cdnpro'", Required: true},
			{Name: "证书ID", Key: "cert_id", Type: "input", Show: "product=='certificate'", Required: true},
		},
	})

	// AWS
	registerDeployConfig(DeployProviderConfig{
		Type:  "aws",
		Name:  "AWS",
		Class: ClassCloudService,
		Icon:  "aws.png",
		Desc:  "支持部署到Amazon CloudFront、ACM",
		Inputs: []cert.ConfigField{
			{Name: "AccessKeyId", Key: "AccessKeyId", Type: "input", Required: true},
			{Name: "SecretAccessKey", Key: "SecretAccessKey", Type: "input", Required: true},
			{Name: "使用代理", Key: "proxy", Type: "radio", Options: []cert.ConfigOption{{Value: "0", Label: "否"}, {Value: "1", Label: "是"}}, Value: "0"},
		},
		TaskInputs: []cert.ConfigField{
			{Name: "要部署的产品", Key: "product", Type: "select", Required: true, Value: "acm", Options: []cert.ConfigOption{
				{Value: "cloudfront", Label: "CloudFront"},
				{Value: "acm", Label: "AWS Certificate Manager"},
			}},
			{Name: "分配ID", Key: "distribution_id", Type: "input", Show: "product=='cloudfront'", Required: true},
			{Name: "ACM ARN", Key: "acm_arn", Type: "input", Show: "product=='acm'", Required: true},
		},
	})

	// 西部数码
	registerDeployConfig(DeployProviderConfig{
		Type:  "west",
		Name:  "西部数码",
		Class: ClassCloudService,
		Icon:  "west.ico",
		Desc:  "支持部署到西部数码虚拟主机",
		Inputs: []cert.ConfigField{
			{Name: "用户名", Key: "username", Type: "input", Required: true},
			{Name: "API密码", Key: "api_password", Type: "input", Required: true},
			{Name: "使用代理", Key: "proxy", Type: "radio", Options: []cert.ConfigOption{{Value: "0", Label: "否"}, {Value: "1", Label: "是"}}, Value: "0"},
		},
		TaskInputs: []cert.ConfigField{
			{Name: "FTP账号", Key: "sitename", Type: "input", Required: true},
		},
	})

	// Gcore
	registerDeployConfig(DeployProviderConfig{
		Type:  "gcore",
		Name:  "Gcore",
		Class: ClassCloudService,
		Icon:  "gcore.ico",
		Desc:  "替换Gcore CDN证书",
		Inputs: []cert.ConfigField{
			{Name: "账户名", Key: "account", Type: "input", Required: true},
			{Name: "API令牌", Key: "apikey", Type: "input", Required: true},
			{Name: "使用代理", Key: "proxy", Type: "radio", Options: []cert.ConfigOption{{Value: "0", Label: "否"}, {Value: "1", Label: "是"}}, Value: "0"},
		},
		TaskInputs: []cert.ConfigField{
			{Name: "证书ID", Key: "id", Type: "input", Required: true},
			{Name: "证书名称", Key: "name", Type: "input", Required: true},
		},
	})

	// Cachefly
	registerDeployConfig(DeployProviderConfig{
		Type:  "cachefly",
		Name:  "Cachefly",
		Class: ClassCloudService,
		Icon:  "cloud.png",
		Desc:  "替换Cachefly CDN证书",
		Inputs: []cert.ConfigField{
			{Name: "账户名", Key: "account", Type: "input", Required: true},
			{Name: "API Token", Key: "apikey", Type: "input", Required: true},
			{Name: "使用代理", Key: "proxy", Type: "radio", Options: []cert.ConfigOption{{Value: "0", Label: "否"}, {Value: "1", Label: "是"}}, Value: "0"},
		},
		TaskInputs: []cert.ConfigField{},
	})

	// 雨云
	registerDeployConfig(DeployProviderConfig{
		Type:  "rainyun",
		Name:  "雨云",
		Class: ClassCloudService,
		Icon:  "waf.png",
		Desc:  "替换雨云证书管理内的证书",
		Inputs: []cert.ConfigField{
			{Name: "账号", Key: "account", Type: "input", Required: true},
			{Name: "ApiKey", Key: "apikey", Type: "input", Required: true},
			{Name: "使用代理", Key: "proxy", Type: "radio", Options: []cert.ConfigOption{{Value: "0", Label: "否"}, {Value: "1", Label: "是"}}, Value: "0"},
		},
		TaskInputs: []cert.ConfigField{
			{Name: "证书ID", Key: "id", Type: "input", Placeholder: "留空则为添加证书"},
		},
	})

	// uniCloud
	registerDeployConfig(DeployProviderConfig{
		Type:  "unicloud",
		Name:  "uniCloud",
		Class: ClassCloudService,
		Icon:  "unicloud.png",
		Desc:  "部署到uniCloud服务空间",
		Inputs: []cert.ConfigField{
			{Name: "账号", Key: "username", Type: "input", Required: true},
			{Name: "密码", Key: "password", Type: "input", Required: true},
			{Name: "使用代理", Key: "proxy", Type: "radio", Options: []cert.ConfigOption{{Value: "0", Label: "否"}, {Value: "1", Label: "是"}}, Value: "0"},
		},
		TaskInputs: []cert.ConfigField{
			{Name: "服务空间ID", Key: "spaceId", Type: "input", Required: true},
			{Name: "空间提供商", Key: "provider", Type: "select", Options: []cert.ConfigOption{
				{Value: "aliyun", Label: "阿里云"},
				{Value: "tencent", Label: "腾讯云"},
			}, Value: "aliyun", Required: true},
			{Name: "空间域名", Key: "domains", Type: "input", Placeholder: "多个域名可使用,分隔", Required: true},
		},
	})

	// 括彩云
	registerDeployConfig(DeployProviderConfig{
		Type:  "kuocai",
		Name:  "括彩云",
		Class: ClassCloudService,
		Icon:  "kuocai.jpg",
		Desc:  "替换括彩云证书管理内的证书",
		Inputs: []cert.ConfigField{
			{Name: "账号", Key: "username", Type: "input", Required: true},
			{Name: "密码", Key: "password", Type: "input", Required: true},
			{Name: "使用代理", Key: "proxy", Type: "radio", Options: []cert.ConfigOption{{Value: "0", Label: "否"}, {Value: "1", Label: "是"}}, Value: "0"},
		},
		TaskInputs: []cert.ConfigField{
			{Name: "域名ID", Key: "id", Type: "input", Required: true},
		},
	})

	// 阿里云CDN
	registerDeployConfig(DeployProviderConfig{
		Type:  "aliyun_cdn",
		Name:  "阿里云CDN",
		Class: ClassCloudService,
		Icon:  "aliyun.png",
		Desc:  "部署证书到阿里云CDN/DCDN",
		Inputs: []cert.ConfigField{
			{Name: "AccessKey ID", Key: "access_key_id", Type: "input", Required: true},
			{Name: "AccessKey Secret", Key: "access_key_secret", Type: "input", Required: true},
		},
		TaskInputs: []cert.ConfigField{
			{Name: "域名", Key: "domains", Type: "textarea", Placeholder: "每行一个域名", Required: true},
		},
	})

	// 华为云CDN
	registerDeployConfig(DeployProviderConfig{
		Type:  "huawei_cdn",
		Name:  "华为云CDN",
		Class: ClassCloudService,
		Icon:  "huawei.png",
		Desc:  "部署证书到华为云CDN",
		Inputs: []cert.ConfigField{
			{Name: "Access Key", Key: "access_key", Type: "input", Required: true},
			{Name: "Secret Key", Key: "secret_key", Type: "input", Required: true},
		},
		TaskInputs: []cert.ConfigField{
			{Name: "域名", Key: "domains", Type: "textarea", Placeholder: "每行一个域名", Required: true},
		},
	})

	// AWS CloudFront
	registerDeployConfig(DeployProviderConfig{
		Type:  "aws_cloudfront",
		Name:  "AWS CloudFront",
		Class: ClassCloudService,
		Icon:  "aws.png",
		Desc:  "部署证书到 AWS CloudFront CDN 分发",
		Inputs: []cert.ConfigField{
			{Name: "Access Key ID", Key: "access_key_id", Type: "input", Required: true},
			{Name: "Secret Access Key", Key: "access_key_secret", Type: "input", Required: true},
		},
		TaskInputs: []cert.ConfigField{
			{Name: "分发ID", Key: "distribution_id", Type: "input", Placeholder: "留空则仅上传证书到ACM"},
		},
	})
}
