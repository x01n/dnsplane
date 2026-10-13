package aliyun

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"main/internal/cert"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

const (
	defaultEndpoint = "cas.aliyuncs.com"
	defaultVersion  = "2020-04-07"
	/* 证书下载相关超时沿用同目录提供商的 30 秒 */
	requestTimeout = 30 * time.Second

	/* 阿里云免费证书实例类型：测试证书 */
	instanceTypeTest = "TEST"
	/* 申请轮询上限 */
	maxTaskPoll      = 30
	taskPollInterval = time.Second
)

func init() {
	/* Type 留空：保留 providers.go 中已定义的表单字段与说明，仅补全工厂函数 */
	cert.Register("aliyun_cert", NewProvider, cert.ProviderConfig{})
}

/* Provider 阿里云免费 SSL 证书提供商 */
type Provider struct {
	accessKeyID     string
	accessKeySecret string
	client          *http.Client
	logger          cert.Logger
	// apiURL 独立成字段便于测试注入本地服务端，生产路径始终使用 defaultEndpoint
	apiURL string
}

/*
 * NewProvider 创建阿里云免费 SSL 提供商实例
 * @param config 账户配置（access_key_id、access_key_secret）
 */
func NewProvider(config, ext map[string]interface{}) cert.Provider {
	return &Provider{
		accessKeyID:     getString(config, "access_key_id"),
		accessKeySecret: getString(config, "access_key_secret"),
		client:          &http.Client{Timeout: requestTimeout},
		apiURL:          "https://" + defaultEndpoint + "/",
	}
}

func getString(m map[string]interface{}, key string) string {
	if v, ok := m[key]; ok {
		return fmt.Sprintf("%v", v)
	}
	return ""
}

/* getSlice 取数组字段，阿里云返回的列表均为 []interface{} */
func getSlice(m map[string]interface{}, key string) []interface{} {
	if v, ok := m[key].([]interface{}); ok {
		return v
	}
	return nil
}

/* SetLogger 设置日志记录器 */
func (p *Provider) SetLogger(logger cert.Logger) {
	p.logger = logger
}

func (p *Provider) log(msg string) {
	if p.logger != nil {
		p.logger(msg)
	}
}

func (p *Provider) Register(ctx context.Context) (map[string]interface{}, error) {
	if p.accessKeyID == "" || p.accessKeySecret == "" {
		return nil, fmt.Errorf("必填参数不能为空")
	}
	if _, err := p.request(ctx, map[string]string{"Action": "ListInstances"}, true); err != nil {
		return nil, err
	}
	return nil, nil
}

/*
 * BuyCert 检查可用测试证书实例
 */
func (p *Provider) BuyCert(ctx context.Context, domains []string, order *cert.OrderInfo) error {
	data, err := p.request(ctx, map[string]string{
		"Action":       "GetInstanceSummary",
		"InstanceType": instanceTypeTest,
	}, true)
	if err != nil {
		return err
	}
	inactive := getNumber(data, "InactiveCount")
	if inactive == 0 {
		return fmt.Errorf("没有待使用的测试证书实例，请先购买测试证书")
	}
	p.log(fmt.Sprintf("实例总个数:%d,实例待使用总数:%d", getNumber(data, "TotalCount"), inactive))
	return nil
}

/*
 * CreateOrder 申请免费证书并返回 DNS 验证记录
 * → 轮询 GetTaskAttribute → GetInstanceDetail 组装验证记录
 */
func (p *Provider) CreateOrder(ctx context.Context, domains []string, order *cert.OrderInfo, keyType, keySize string) (map[string][]cert.DNSRecord, error) {
	if len(domains) == 0 {
		return nil, fmt.Errorf("域名列表不能为空")
	}
	domain := domains[0]

	listData, err := p.request(ctx, map[string]string{
		"Action":       "ListInstances",
		"Status":       "inactive",
		"InstanceType": instanceTypeTest,
	}, true)
	if err != nil {
		return nil, err
	}
	instances := getSlice(listData, "InstanceList")
	if len(instances) == 0 {
		return nil, fmt.Errorf("待使用的测试证书实例列表为空")
	}
	first, ok := instances[0].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("证书实例格式异常")
	}
	instanceID := getString(first, "InstanceId")
	if instanceID == "" {
		return nil, fmt.Errorf("待使用的测试证书实例列表为空")
	}

	contactData, err := p.request(ctx, map[string]string{"Action": "ListContact"}, true)
	if err != nil {
		return nil, err
	}
	contacts := getSlice(contactData, "ContactList")
	if len(contacts) == 0 {
		return nil, fmt.Errorf("联系人列表为空，请先添加联系人")
	}
	contact, ok := contacts[0].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("联系人格式异常")
	}
	contactID := getString(contact, "ContactId")
	keyAlgorithm := "RSA_2048"
	switch {
	case strings.EqualFold(keyType, "ECC"):
		keyAlgorithm = "ECC_256"
	case keySize == "3072":
		keyAlgorithm = "RSA_3072"
	}

	if _, err := p.request(ctx, map[string]string{
		"Action":          "UpdateInstance",
		"InstanceId":      instanceID,
		"Domain":          domain,
		"KeyAlgorithm":    keyAlgorithm,
		"AutoReissue":     "disable",
		"ContactIdList.1": contactID,
		"ValidateType":    "DNS",
	}, false); err != nil {
		return nil, fmt.Errorf("更新证书实例失败：%s", err.Error())
	}

	if _, err := p.request(ctx, map[string]string{
		"Action":     "ApplyCertificate",
		"InstanceId": instanceID,
	}, false); err != nil {
		return nil, fmt.Errorf("申请证书失败：%s", err.Error())
	}

	time.Sleep(time.Second)

	if err := p.waitTaskDone(ctx, instanceID); err != nil {
		return nil, err
	}

	detail, err := p.request(ctx, map[string]string{
		"Action":     "GetInstanceDetail",
		"InstanceId": instanceID,
	}, true)
	if err != nil {
		return nil, fmt.Errorf("获取实例详情失败：%s", err.Error())
	}

	order.OrderURL = instanceID
	return buildDNSRecords(detail), nil
}

/*
 * waitTaskDone 轮询申请任务直到非 processing
 */
func (p *Provider) waitTaskDone(ctx context.Context, instanceID string) error {
	for i := 0; i < maxTaskPoll; i++ {
		data, err := p.request(ctx, map[string]string{
			"Action": "GetTaskAttribute",
			"TaskId": instanceID,
		}, true)
		if err != nil {
			return fmt.Errorf("申请证书提交结果查询失败：%s", err.Error())
		}
		switch getString(data, "TaskStatus") {
		case "processing":
			time.Sleep(taskPollInterval)
			continue
		case "failed":
			return fmt.Errorf("申请证书失败：%s", getString(data, "TaskMessage"))
		default:
			return nil
		}
	}
	return fmt.Errorf("申请证书超时：任务长时间处于 processing 状态")
}

/*
 * buildDNSRecords 从 GetInstanceDetail 响应组装验证记录
 */
func buildDNSRecords(detail map[string]interface{}) map[string][]cert.DNSRecord {
	dnsRecords := make(map[string][]cert.DNSRecord)
	for _, item := range getSlice(detail, "DomainValidationList") {
		opts, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		mainDomain := getMainDomain(getString(opts, "Domain"))
		fullName := getString(opts, "ValidationKey") + "." + getString(opts, "RootDomain")
		dnsRecords[mainDomain] = append(dnsRecords[mainDomain], cert.DNSRecord{
			Name:  strings.TrimSuffix(fullName, "."+mainDomain),
			Type:  getString(opts, "ValidationType"),
			Value: getString(opts, "ValidationValue"),
		})
	}
	return dnsRecords
}

func (p *Provider) AuthOrder(ctx context.Context, domains []string, order *cert.OrderInfo) error {
	return nil
}

/*
 * GetAuthStatus 查询验证状态
 */
func (p *Provider) GetAuthStatus(ctx context.Context, domains []string, order *cert.OrderInfo) (bool, error) {
	data, err := p.request(ctx, map[string]string{
		"Action":     "GetInstanceDetail",
		"InstanceId": order.OrderURL,
	}, true)
	if err != nil {
		return false, err
	}
	switch getString(data, "Status") {
	case "normal":
		return true, nil
	case "closed":
		return false, fmt.Errorf("证书审核失败")
	default:
		return false, nil
	}
}

/*
 * FinalizeOrder 获取已签发证书
 */
func (p *Provider) FinalizeOrder(ctx context.Context, domains []string, order *cert.OrderInfo, keyType, keySize string) (*cert.CertResult, error) {
	detail, err := p.request(ctx, map[string]string{
		"Action":     "GetInstanceDetail",
		"InstanceId": order.OrderURL,
	}, true)
	if err != nil {
		return nil, err
	}
	certificateID := getString(detail, "CertificateId")
	if certificateID == "" {
		return nil, fmt.Errorf("证书ID不存在")
	}

	certData, err := p.request(ctx, map[string]string{
		"Action": "GetUserCertificateDetail",
		"CertId": certificateID,
	}, true)
	if err != nil {
		return nil, err
	}
	fullChain := getString(certData, "Cert")
	privateKey := getString(certData, "Key")
	if fullChain == "" || privateKey == "" {
		return nil, fmt.Errorf("证书内容获取失败")
	}
	return buildCertResult(fullChain, privateKey)
}

func (p *Provider) Revoke(ctx context.Context, order *cert.OrderInfo, pem string) error {
	_, err := p.request(ctx, map[string]string{
		"Action":     "RevokeCertificate",
		"InstanceId": order.OrderURL,
	}, false)
	return err
}

/*
 * Cancel 取消申请中的订单
 */
func (p *Provider) Cancel(ctx context.Context, order *cert.OrderInfo) error {
	data, err := p.request(ctx, map[string]string{
		"Action":     "GetInstanceDetail",
		"InstanceId": order.OrderURL,
	}, true)
	if err != nil {
		return err
	}
	if getString(data, "Status") != "pending" {
		return nil
	}
	_, err = p.request(ctx, map[string]string{
		"Action":     "CancelPendingCertificate",
		"InstanceId": order.OrderURL,
	}, false)
	return err
}

/*
 * request 发起一次阿里云 RPC 调用
 * @param params 业务参数（Action 等），公共参数在内部补齐
 * @param returnData true 返回响应对象，false 仅返回是否成功
 */
func (p *Provider) request(ctx context.Context, params map[string]string, returnData bool) (map[string]interface{}, error) {
	body := p.signedBody(params)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.apiURL, strings.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求阿里云失败: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var result map[string]interface{}
	parsed := json.Unmarshal(respBody, &result) == nil && result != nil
	if resp.StatusCode == http.StatusOK {
		if !returnData {
			return nil, nil
		}
		return result, nil
	}
	if parsed {
		if msg := getString(result, "Message"); msg != "" {
			return nil, fmt.Errorf("%s", msg)
		}
	}
	return nil, fmt.Errorf("返回数据解析失败")
}

/*
 * signedBody 组装公共参数并计算签名，返回 POST 表单体
 */
func (p *Provider) signedBody(params map[string]string) string {
	all := make(map[string]string, len(params)+8)
	for k, v := range params {
		all[k] = v
	}
	all["Format"] = "JSON"
	all["Version"] = defaultVersion
	all["AccessKeyId"] = p.accessKeyID
	all["SignatureMethod"] = "HMAC-SHA1"
	all["Timestamp"] = time.Now().UTC().Format("2006-01-02T15:04:05Z")
	all["SignatureVersion"] = "1.0"
	all["SignatureNonce"] = randomNonce(8)

	all["Signature"] = signForm(all, p.accessKeySecret)
	return url.Values(toValues(all)).Encode()
}

func signForm(params map[string]string, accessKeySecret string) string {
	stringToSign := http.MethodPost + "&%2F&" + percentEncode(canonicalQuery(params))
	mac := hmac.New(sha1.New, []byte(accessKeySecret+"&"))
	mac.Write([]byte(stringToSign))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

/*
 * signedFormSignature 对已提交的表单参数复算签名，供测试验证服务端收到的参数与 Signature 自洽
 */
func signedFormSignature(params map[string]string, accessKeySecret string) string {
	copied := make(map[string]string, len(params))
	for k, v := range params {
		copied[k] = v
	}
	delete(copied, "Signature")
	return signForm(copied, accessKeySecret)
}

/*
 * canonicalQuery 按参数名排序拼接 k=v&k=v（键值均做阿里云规范转义），跳过 Signature
 */
func canonicalQuery(params map[string]string) string {
	keys := sortedKeys(params)
	var sb strings.Builder
	for i, k := range keys {
		if i > 0 {
			sb.WriteString("&")
		}
		sb.WriteString(percentEncode(k))
		sb.WriteString("=")
		sb.WriteString(percentEncode(params[k]))
	}
	return sb.String()
}

/*
 * percentEncode 阿里云规范转义
 */
func percentEncode(s string) string {
	return strings.ReplaceAll(url.QueryEscape(s), "+", "%20")
}

func sortedKeys(params map[string]string) []string {
	keys := make([]string, 0, len(params))
	for k := range params {
		if k == "Signature" {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func toValues(params map[string]string) url.Values {
	values := make(url.Values, len(params))
	for k, v := range params {
		values.Set(k, v)
	}
	return values
}

/*
 * randomNonce 生成指定长度随机串作为 SignatureNonce
 */
func randomNonce(length int) string {
	const alphabet = "0123456789abcdefghijklmnopqrstuvwxyz"
	buf := make([]byte, length)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	for i := range buf {
		buf[i] = alphabet[int(buf[i])%len(alphabet)]
	}
	return string(buf)
}

/* getNumber 取数值字段，阿里云返回的计数为 JSON number */
func getNumber(m map[string]interface{}, key string) int64 {
	if v, ok := m[key].(float64); ok {
		return int64(v)
	}
	return 0
}

/* buildCertResult 解析证书链并组装签发结果 */
func buildCertResult(fullChain, privateKey string) (*cert.CertResult, error) {
	block, _ := pem.Decode([]byte(fullChain))
	if block == nil {
		return nil, fmt.Errorf("证书解析失败")
	}
	parsed, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("证书解析失败")
	}
	return &cert.CertResult{
		FullChain:  fullChain,
		PrivateKey: privateKey,
		Issuer:     parsed.Issuer.CommonName,
		ValidFrom:  parsed.NotBefore.Unix(),
		ValidTo:    parsed.NotAfter.Unix(),
	}, nil
}

/* getMainDomain 取域名的主域名（最后两段），与 internal/cert/acme 的同类实现保持一致 */
func getMainDomain(domain string) string {
	domain = strings.TrimPrefix(domain, "*.")
	parts := strings.Split(domain, ".")
	if len(parts) >= 2 {
		return parts[len(parts)-2] + "." + parts[len(parts)-1]
	}
	return domain
}
