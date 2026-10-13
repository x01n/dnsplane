package cert

import "context"

/* OrderInfo 证书订单信息 */
type OrderInfo struct {
	OrderURL       string               `json:"order_url"`
	FinalizeURL    string               `json:"finalize_url"`
	CertURL        string               `json:"cert_url"`
	Authorizations []string             `json:"authorizations"`
	Status         string               `json:"status"`
	Identifiers    []Identifier         `json:"identifiers"`
	Challenges     map[string]Challenge `json:"challenges"`
	// PreferredChallenge ACME：空或 dns-01 为默认 DNS TXT；http-01 时对非通配符域名走 HTTP 校验（需公网 80）。IP 标识符始终 http-01。
	PreferredChallenge string `json:"preferred_challenge,omitempty"`
}

/* Identifier 域名标识 */
type Identifier struct {
	Type  string `json:"type"`
	Value string `json:"value"`
}

/* Challenge 验证挑战 */
type Challenge struct {
	Type   string `json:"type"`
	URL    string `json:"url"`
	Token  string `json:"token"`
	Status string `json:"status"`
}

/* DNSRecord DNS验证记录 */
type DNSRecord struct {
	Name  string `json:"name"`
	Type  string `json:"type"`
	Value string `json:"value"`
}

/* CertResult 证书签发结果 */
type CertResult struct {
	FullChain  string `json:"fullchain"`
	PrivateKey string `json:"private_key"`
	Issuer     string `json:"issuer"`
	ValidFrom  int64  `json:"valid_from"`
	ValidTo    int64  `json:"valid_to"`
}

/* Logger 日志记录器 */
type Logger func(msg string)

/* Provider 证书提供商接口 */
type Provider interface {
	// Register 注册账户
	Register(ctx context.Context) (map[string]interface{}, error)

	// BuyCert 购买证书（用于商业CA）
	BuyCert(ctx context.Context, domains []string, order *OrderInfo) error

	// CreateOrder 创建证书订单
	CreateOrder(ctx context.Context, domains []string, order *OrderInfo, keyType, keySize string) (map[string][]DNSRecord, error)

	// AuthOrder 验证订单
	AuthOrder(ctx context.Context, domains []string, order *OrderInfo) error

	// GetAuthStatus 获取验证状态
	GetAuthStatus(ctx context.Context, domains []string, order *OrderInfo) (bool, error)

	// FinalizeOrder 签发证书
	FinalizeOrder(ctx context.Context, domains []string, order *OrderInfo, keyType, keySize string) (*CertResult, error)

	// Revoke 吊销证书
	Revoke(ctx context.Context, order *OrderInfo, pem string) error

	// Cancel 取消订单
	Cancel(ctx context.Context, order *OrderInfo) error

	// SetLogger 设置日志记录器
	SetLogger(logger Logger)
}

/* ProviderConfig 证书提供商配置 */
type ProviderConfig struct {
	Type         string        `json:"type"`
	Name         string        `json:"name"`
	Icon         string        `json:"icon"`
	Note         string        `json:"note"`
	Config       []ConfigField `json:"config"`
	DeployConfig []ConfigField `json:"deploy_config,omitempty"`
	DeployNote   string        `json:"deploy_note,omitempty"`
	CNAME        bool          `json:"cname"`     // 是否支持CNAME代理
	IsDeploy     bool          `json:"is_deploy"` // 是否用于部署
}

/* ConfigField 配置字段 */
type ConfigField struct {
	Name        string         `json:"name"`
	Key         string         `json:"key"`
	Type        string         `json:"type"`
	Placeholder string         `json:"placeholder"`
	Required    bool           `json:"required"`
	Options     []ConfigOption `json:"options,omitempty"`
	Value       string         `json:"value,omitempty"`
	Note        string         `json:"note,omitempty"`
	Show        string         `json:"show,omitempty"`
	Disabled    bool           `json:"disabled,omitempty"`
	Validator   string         `json:"validator,omitempty"`
	Min         int            `json:"min,omitempty"`
	Max         int            `json:"max,omitempty"`
}

/* ConfigOption 配置选项 */
type ConfigOption struct {
	Value string `json:"value"`
	Label string `json:"label"`
}
