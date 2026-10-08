package notify

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

const (
	aliyunDirectMailURL = "https://dm.aliyuncs.com/"
	aliyunDirectMailVersion = "2015-11-23"
)

/* AliyunDirectMailConfig 阿里云邮件推送配置 */
type AliyunDirectMailConfig struct {
	AccessKeyID     string `json:"access_key_id"`
	AccessKeySecret string `json:"access_key_secret"`
	From            string `json:"from"`
	FromName        string `json:"from_name"`
	To              string `json:"to"`
}

/* AliyunDirectMailNotifier 阿里云邮件推送（DirectMail）通知发送器 */
type AliyunDirectMailNotifier struct {
	config AliyunDirectMailConfig
	client *http.Client
	// apiURL 独立成字段便于测试注入本地服务端，生产路径始终使用 aliyunDirectMailURL
	apiURL string
}

func NewAliyunDirectMailNotifier(config AliyunDirectMailConfig) *AliyunDirectMailNotifier {
	return &AliyunDirectMailNotifier{
		config: config,
		client: &http.Client{Timeout: 30 * time.Second},
		apiURL: aliyunDirectMailURL,
	}
}

/*
 * Send 通过阿里云邮件推送 SingleSendMail 接口发送 HTML 邮件
 * 参数：title 邮件主题，content HTML 正文
 */
func (n *AliyunDirectMailNotifier) Send(ctx context.Context, title, content string) error {
	if n.config.AccessKeyID == "" || n.config.AccessKeySecret == "" {
		return fmt.Errorf("阿里云邮件推送 AccessKey 未配置")
	}
	// 邮件头 CRLF 注入防御：API 路径同样禁止头部字段携带控制字符
	if err := SanitizeMailHeader("Subject", title); err != nil {
		return err
	}
	if err := SanitizeMailHeader("From", n.config.From); err != nil {
		return err
	}
	if err := SanitizeMailHeader("FromName", n.config.FromName); err != nil {
		return err
	}
	to := splitMailRecipients(n.config.To)
	if len(to) == 0 {
		return fmt.Errorf("未配置收件人")
	}
	for _, addr := range to {
		if err := SanitizeMailHeader("To", addr); err != nil {
			return err
		}
	}

	params := map[string]string{
		"Action":         "SingleSendMail",
		"AccountName":    n.config.From,
		"ReplyToAddress": "false",
		"AddressType":    "1",
		"ToAddress":      strings.Join(to, ","),
		"FromAlias":      n.config.FromName,
		"Subject":        title,
		"HtmlBody":       content,
	}

	body, err := n.signedRequest(ctx, params)
	if err != nil {
		return err
	}
	// 成功响应形如 {"RequestId":"...","EnvId":"..."}，此处仅要求可解析为 JSON 对象
	var resp map[string]interface{}
	if err := json.Unmarshal(body, &resp); err != nil {
		return fmt.Errorf("阿里云邮件推送响应解析失败: %s", string(body))
	}
	return nil
}

/*
 * signedRequest 组装签名并发起 POST 请求，返回响应体
 */
func (n *AliyunDirectMailNotifier) signedRequest(ctx context.Context, params map[string]string) ([]byte, error) {
	params["Format"] = "JSON"
	params["Version"] = aliyunDirectMailVersion
	params["AccessKeyId"] = n.config.AccessKeyID
	params["SignatureMethod"] = "HMAC-SHA1"
	params["Timestamp"] = time.Now().UTC().Format("2006-01-02T15:04:05Z")
	params["SignatureVersion"] = "1.0"
	params["SignatureNonce"] = randomNonce(8)
	params["Signature"] = aliyunRPCSignature(params, n.config.AccessKeySecret, http.MethodPost)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		n.apiURL, strings.NewReader(encodeAliyunBody(params)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := n.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("发送阿里云邮件推送失败: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取阿里云邮件推送响应失败: %w", err)
	}

	if resp.StatusCode == http.StatusOK {
		return body, nil
	}
	var errResp struct {
		Message string `json:"Message"`
	}
	if err := json.Unmarshal(body, &errResp); err == nil && errResp.Message != "" {
		return nil, fmt.Errorf("阿里云邮件推送错误: %s", errResp.Message)
	}
	return nil, fmt.Errorf("阿里云邮件推送返回数据解析失败(http_code=%d): %s", resp.StatusCode, string(body))
}

/*
 * aliyunRPCSignature 计算阿里云 RPC 风格签名
 * percentEncode(k)=percentEncode(v) 拼接，stringToSign = METHOD&%2F&percentEncode(canonicalQuery)，
 * HMAC-SHA1（密钥追加 &）后 Base64
 */
func aliyunRPCSignature(params map[string]string, accessKeySecret, method string) string {
	canonical := encodeAliyunParams(params, true)
	stringToSign := method + "&%2F&" + aliyunPercentEncode(canonical)
	mac := hmac.New(sha1.New, []byte(accessKeySecret+"&"))
	mac.Write([]byte(stringToSign))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

/*
 * encodeAliyunParams 按参数名排序后拼接为 k=v&k=v 形式，跳过 Signature 本身
 */
func encodeAliyunParams(params map[string]string, forSignature bool) string {
	keys := make([]string, 0, len(params))
	for k := range params {
		if k == "Signature" {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)

	if !forSignature {
		form := url.Values{}
		for _, k := range keys {
			form.Set(k, params[k])
		}
		return form.Encode()
	}

	var sb strings.Builder
	for i, k := range keys {
		if i > 0 {
			sb.WriteString("&")
		}
		sb.WriteString(aliyunPercentEncode(k))
		sb.WriteString("=")
		sb.WriteString(aliyunPercentEncode(params[k]))
	}
	return sb.String()
}

/*
 * encodeAliyunBody 构造 POST 请求体：规范查询串 + Signature
 */
func encodeAliyunBody(params map[string]string) string {
	body := encodeAliyunParams(params, false)
	if sig := params["Signature"]; sig != "" {
		body += "&Signature=" + url.QueryEscape(sig)
	}
	return body
}

/*
 * aliyunPercentEncode 阿里云规范转义
 */
func aliyunPercentEncode(s string) string {
	return strings.ReplaceAll(url.QueryEscape(s), "+", "%20")
}

/*
 * randomNonce 生成指定长度的随机字符串作为 SignatureNonce
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
