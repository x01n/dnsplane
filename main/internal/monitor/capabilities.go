package monitor

/* PlatformCaps DNS平台能力 */
type PlatformCaps struct {
	Pause  bool // 支持暂停记录
	Delete bool // 支持删除记录
	Mixed  bool // 支持A+CNAME混合
}

var platformCapabilities = map[string]PlatformCaps{
	"dnspod":     {Pause: true, Delete: true, Mixed: true},
	"aliyun":     {Pause: true, Delete: true, Mixed: false},
	"cloudflare": {Pause: false, Delete: true, Mixed: false},
	"huawei":     {Pause: false, Delete: true, Mixed: true},
	"huoshan":    {Pause: true, Delete: true, Mixed: false},
	"dnsla":      {Pause: true, Delete: true, Mixed: false},
	"west":       {Pause: true, Delete: true, Mixed: false},
	"jdcloud":    {Pause: true, Delete: true, Mixed: false},
	"baidu":      {Pause: true, Delete: true, Mixed: false},
	"namesilo":   {Pause: false, Delete: true, Mixed: false},
	"powerdns":   {Pause: false, Delete: true, Mixed: false},
	"spaceship":  {Pause: false, Delete: false, Mixed: false},
	"bt":         {Pause: true, Delete: true, Mixed: false},
	"tencenteo":  {Pause: false, Delete: true, Mixed: false},
	"aliyunesa":  {Pause: false, Delete: true, Mixed: false},
}

func GetPlatformCaps(providerType string) PlatformCaps {
	if caps, ok := platformCapabilities[providerType]; ok {
		return caps
	}
	return PlatformCaps{Pause: false, Delete: true, Mixed: false}
}
