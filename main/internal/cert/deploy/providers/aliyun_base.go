package providers

import (
	"context"
	"crypto/hmac"
	"crypto/md5"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"main/internal/cert/deploy/base"

	"github.com/aliyun/alibaba-cloud-sdk-go/sdk"
	"github.com/aliyun/alibaba-cloud-sdk-go/sdk/auth/credentials"
	"github.com/aliyun/alibaba-cloud-sdk-go/sdk/requests"
	"github.com/aliyun/alibaba-cloud-sdk-go/sdk/responses"
)

/*
 * aliyunClient 阿里云通用请求客户端抽象
 * *sdk.Client 满足该接口，便于在不重建客户端的前提下复用请求逻辑
 */
type aliyunClient interface {
	ProcessCommonRequest(request *requests.CommonRequest) (response *responses.CommonResponse, err error)
}

/*
 * AliyunProviderBase 阿里云部署器公共基础
 * 只承载凭据、地域与通用请求构造，不实现 DeployWithInfo，避免方法提升让子类被误判为支持信息回传
 */
type AliyunProviderBase struct {
	base.BaseProvider
}

/* client 创建通用客户端，配合 CommonRequest 调用各云产品接口 */
func (p *AliyunProviderBase) client(region string) (*sdk.Client, error) {
	accessKeyID := p.GetString("access_key_id")
	accessKeySecret := p.GetString("access_key_secret")
	if accessKeyID == "" || accessKeySecret == "" {
		return nil, errors.New("AccessKey不能为空")
	}
	cred := credentials.NewAccessKeyCredential(accessKeyID, accessKeySecret)
	// 必须显式指定 https：SDK 的 NewConfig 默认 Scheme 为 "HTTP"，
	// 而本路径的 UploadUserCertificate 把私钥全文放进查询参数，
	// 明文传输会泄露证书私钥；手写实现（aliyun_media / aliyun_oss / cert/aliyun）均硬编码 https
	return sdk.NewClientWithOptions(region, sdk.NewConfig().WithScheme("https"), cred)
}

/* regionOf 读取任务配置中的产品地域，缺省为中国内地 */
func (p *AliyunProviderBase) regionOf(config map[string]interface{}) string {
	region := p.GetStringFrom(config, "region")
	if region == "" {
		region = "cn-hangzhou"
	}
	return region
}

/* regionIDOf 读取任务配置中的地域ID，缺省为中国内地 */
func (p *AliyunProviderBase) regionIDOf(config map[string]interface{}) string {
	regionID := p.GetStringFrom(config, "regionid")
	if regionID == "" {
		regionID = "cn-hangzhou"
	}
	return regionID
}

func (p *AliyunProviderBase) deployTypeOf(config map[string]interface{}) int {
	if config != nil {
		if value, ok := config["deploy_type"]; ok {
			return int(intFromAny(value))
		}
	}
	return int(intFromAny(p.GetString("deploy_type")))
}

/* Check 校验 AccessKey 有效性 */
func (p *AliyunProviderBase) Check(ctx context.Context) error {
	client, err := p.client("cn-hangzhou")
	if err != nil {
		return err
	}
	req := newAliyunRequest("POST", aliyunCASDefaultEndpoint, aliyunCASVersion, "ListUserCertificateOrder")
	_, err = callAliyun(client, req)
	return err
}

/*
 * newAliyunRequest 构造阿里云 RPC 风格通用请求
 * @param method HTTP 方法，查询类接口用 GET，变更类接口用 POST
 * @param endpoint 云产品接入点域名
 * @param version OpenAPI 版本号
 * @param action 接口名称
 */
func newAliyunRequest(method, endpoint, version, action string) *requests.CommonRequest {
	req := requests.NewCommonRequest()
	req.Method = method
	req.Domain = endpoint
	req.Version = version
	req.ApiName = action
	req.QueryParams["Action"] = action
	return req
}

/* callAliyun 发送通用请求并解析 JSON 响应，响应中带业务错误码时转为错误返回 */
func callAliyun(client aliyunClient, req *requests.CommonRequest) (map[string]interface{}, error) {
	resp, err := client.ProcessCommonRequest(req)
	if err != nil {
		return nil, err
	}
	result := map[string]interface{}{}
	if jsonErr := json.Unmarshal(resp.GetHttpContentBytes(), &result); jsonErr != nil {
		return map[string]interface{}{}, nil
	}
	if code, ok := result["Code"].(string); ok && code != "" {
		if message, ok := result["Message"].(string); ok && message != "" {
			return nil, errors.New(message)
		}
		return nil, errors.New(code)
	}
	return result, nil
}

/* aliyunJSONText 把响应对象序列化为 JSON 文本，用于日志输出 */
func aliyunJSONText(data map[string]interface{}) string {
	if len(data) == 0 {
		return "{}"
	}
	text, err := json.Marshal(data)
	if err != nil {
		return "{}"
	}
	return string(text)
}

/* stringFromAny 把 JSON 解析出的值转为字符串，数字按整数输出 */
func stringFromAny(value interface{}) string {
	switch typed := value.(type) {
	case string:
		return typed
	case float64:
		return strconv.FormatInt(int64(typed), 10)
	case int64:
		return strconv.FormatInt(typed, 10)
	case int:
		return strconv.Itoa(typed)
	}
	return ""
}

/* intFromAny 把 JSON 解析出的值转为整数，无法转换时返回 0 */
func intFromAny(value interface{}) int64 {
	switch typed := value.(type) {
	case float64:
		return int64(typed)
	case int64:
		return typed
	case int:
		return int64(typed)
	case string:
		if parsed, err := strconv.ParseInt(typed, 10, 64); err == nil {
			return parsed
		}
	}
	return 0
}

func parseAliyunTime(value string) (time.Time, bool) {
	layouts := []string{
		time.RFC3339,
		"2006-01-02T15:04:05Z",
		"2006-01-02 15:04:05",
		"2006-01-02",
	}
	for _, layout := range layouts {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed, true
		}
	}
	return time.Time{}, false
}

/* fillAliyunInfo 回传证书ID与名称 */
func fillAliyunInfo(info *base.DeployInfo, certID, certName string) {
	if info == nil {
		return
	}
	if info.Data == nil {
		info.Data = map[string]interface{}{}
	}
	info.Data["cert_id"] = certID
	info.Data["cert_name"] = certName
}

/* splitAliyunDomains 按逗号切分域名并去除空白项 */
func splitAliyunDomains(input string) []string {
	var domains []string
	for _, item := range strings.Split(input, ",") {
		if item != "" {
			domains = append(domains, item)
		}
	}
	return domains
}

func aliyunNonce() string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 16)
	}
	sum := md5.Sum(buf)
	return hex.EncodeToString(sum[:])
}

/* aliyunSHA256Hex 计算内容的 SHA-256 十六进制摘要 */
func aliyunSHA256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func aliyunCanonicalEscape(value string) string {
	return strings.ReplaceAll(url.QueryEscape(value), "+", "%20")
}

/* aliyunCanonicalURI 逐段转义请求路径 */
func aliyunCanonicalURI(path string) string {
	if path == "" {
		return "/"
	}
	segments := strings.Split(path, "/")
	for i, segment := range segments {
		segments[i] = aliyunCanonicalEscape(segment)
	}
	return strings.Join(segments, "/")
}

/*
 * aliyunACS3Authorization 生成 ACS3-HMAC-SHA256 授权头
 */
func aliyunACS3Authorization(method, path string, headers map[string]string, body []byte, accessKeyID, accessKeySecret string) string {
	keys := make([]string, 0, len(headers))
	normalized := make(map[string]string, len(headers))
	for key, value := range headers {
		lower := strings.ToLower(key)
		normalized[lower] = strings.TrimSpace(value)
		keys = append(keys, lower)
	}
	sort.Strings(keys)

	var canonicalHeaders strings.Builder
	for _, key := range keys {
		canonicalHeaders.WriteString(key + ":" + normalized[key] + "\n")
	}
	signedHeaders := strings.Join(keys, ";")

	canonicalRequest := method + "\n" +
		aliyunCanonicalURI(path) + "\n" +
		"\n" +
		canonicalHeaders.String() + "\n" +
		signedHeaders + "\n" +
		aliyunSHA256Hex(body)

	stringToSign := "ACS3-HMAC-SHA256\n" + aliyunSHA256Hex([]byte(canonicalRequest))
	mac := hmac.New(sha256.New, []byte(accessKeySecret))
	mac.Write([]byte(stringToSign))
	signature := hex.EncodeToString(mac.Sum(nil))

	return "ACS3-HMAC-SHA256 Credential=" + accessKeyID + ",SignedHeaders=" + signedHeaders + ",Signature=" + signature
}
