package providers

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"main/internal/cert"
	"main/internal/cert/deploy/base"
)

func init() {
	base.Register("s3storage", NewS3Provider)
}

// S3Provider S3 存储证书部署
type S3Provider struct {
	base.BaseProvider
	client        *http.Client
	accessKeyID   string
	secretKey     string
	endpoint      string
	region        string
	proxy         bool
}

// NewS3Provider creates a new S3 provider
func NewS3Provider(config map[string]interface{}) base.DeployProvider {
	region := base.GetConfigString(config, "region")
	if region == "" {
		region = "us-east-1"
	}
	proxy := base.GetConfigBool(config, "proxy")

	return &S3Provider{
		BaseProvider: base.BaseProvider{Config: config},
		client:       &http.Client{Timeout: 30 * time.Second},
		accessKeyID:  base.GetConfigString(config, "AccessKeyId"),
		secretKey:    base.GetConfigString(config, "SecretAccessKey"),
		endpoint:     strings.TrimSuffix(base.GetConfigString(config, "endpoint"), "/"),
		region:       region,
		proxy:        proxy,
	}
}

// Check verifies connection to S3
func (p *S3Provider) Check(ctx context.Context) error {
	if p.accessKeyID == "" || p.secretKey == "" || p.endpoint == "" {
		return fmt.Errorf("必填参数不能为空")
	}

	_, err := p.s3Request(ctx, "GET", "/", "", nil)
	return err
}

// Deploy deploys certificate to S3 storage
func (p *S3Provider) Deploy(ctx context.Context, fullchain, privateKey string, config map[string]interface{}) error {
	bucket := base.GetConfigString(config, "bucket")
	if bucket == "" {
		return fmt.Errorf("存储桶名称不能为空")
	}

	certPath := strings.Trim(base.GetConfigString(config, "cert_path"), "/")
	keyPath := strings.Trim(base.GetConfigString(config, "key_path"), "/")
	if certPath == "" || keyPath == "" {
		return fmt.Errorf("证书和私钥保存路径不能为空")
	}

	if _, err := p.putObject(ctx, bucket, certPath, fullchain); err != nil {
		return err
	}
	p.Log(fmt.Sprintf("证书已上传到：s3://%s/%s", bucket, certPath))

	if _, err := p.putObject(ctx, bucket, keyPath, privateKey); err != nil {
		return err
	}
	p.Log(fmt.Sprintf("私钥已上传到：s3://%s/%s", bucket, keyPath))

	return nil
}

// SetLogger sets the logger
func (p *S3Provider) SetLogger(logger cert.Logger) {
	p.BaseProvider.SetLogger(logger)
}

func (p *S3Provider) putObject(ctx context.Context, bucket, key, content string) (string, error) {
	path := "/" + bucket + "/" + key
	ct := "application/x-pem-file"
	return p.s3Request(ctx, "PUT", path, content, &ct)
}

func (p *S3Provider) s3Request(ctx context.Context, method, path, body string, contentType *string) (string, error) {
	timeNow := time.Now().UTC()
	date := timeNow.Format("20060102T150405Z")
	shortDate := timeNow.Format("20060102")

	host := p.endpoint
	scheme := "https"
	if strings.HasPrefix(p.endpoint, "http://") {
		scheme = "http"
		host = strings.TrimPrefix(p.endpoint, "http://")
	} else if strings.HasPrefix(p.endpoint, "https://") {
		host = strings.TrimPrefix(p.endpoint, "https://")
	}

	payloadHash := sha256HexS3(body)

	headers := map[string]string{
		"Host":                 host,
		"X-Amz-Date":           date,
		"X-Amz-Content-Sha256": payloadHash,
	}
	if contentType != nil && *contentType != "" {
		headers["Content-Type"] = *contentType
	}

	authorization := p.generateSign(method, path, nil, headers, body, date, shortDate)
	headers["Authorization"] = authorization

	reqURL := scheme + "://" + host + path

	req, err := http.NewRequestWithContext(ctx, method, reqURL, strings.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("创建请求失败: %v", err)
	}

	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("请求失败: %v", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("读取响应失败: %v", err)
	}

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return string(respBody), nil
	}

	errMsg := fmt.Sprintf("HTTP Code: %d", resp.StatusCode)
	if len(respBody) > 0 {
		// Try to parse XML error response
		bodyStr := string(respBody)
		if msg := p.parseXMLError(bodyStr); msg != "" {
			errMsg = msg
		}
	}

	return "", fmt.Errorf("%s", errMsg)
}

func (p *S3Provider) generateSign(method, path string, query map[string]string, headers map[string]string, body, date, shortDate string) string {
	algorithm := "AWS4-HMAC-SHA256"

	canonicalURI := p.getCanonicalURI(path)
	canonicalQueryString := p.getCanonicalQueryString(query)
	canonicalHeaders, signedHeaders := p.getCanonicalHeaders(headers)
	hashedPayload := sha256Hex(body)

	canonicalRequest := method + "\n" +
		canonicalURI + "\n" +
		canonicalQueryString + "\n" +
		canonicalHeaders + "\n" +
		signedHeaders + "\n" +
		hashedPayload

	credentialScope := shortDate + "/" + p.region + "/s3/aws4_request"
	stringToSign := algorithm + "\n" +
		date + "\n" +
		credentialScope + "\n" +
		sha256HexS3(canonicalRequest)

	kDate := p.hmacSHA256Str(shortDate, "AWS4"+p.secretKey)
	kRegion := p.hmacSHA256Bytes([]byte(p.region), kDate)
	kService := p.hmacSHA256Bytes([]byte("s3"), kRegion)
	kSigning := p.hmacSHA256Bytes([]byte("aws4_request"), kService)
	signature := hex.EncodeToString(p.hmacSHA256Bytes([]byte(stringToSign), kSigning))

	return algorithm + " Credential=" + p.accessKeyID + "/" + credentialScope +
		", SignedHeaders=" + signedHeaders +
		", Signature=" + signature
}

func (p *S3Provider) escape(str string) string {
	encoded := url.QueryEscape(str)
	encoded = strings.ReplaceAll(encoded, "+", "%20")
	encoded = strings.ReplaceAll(encoded, "*", "%2A")
	encoded = strings.ReplaceAll(encoded, "%7E", "~")
	return encoded
}

func (p *S3Provider) getCanonicalURI(path string) string {
	if path == "" {
		return "/"
	}
	parts := strings.Split(path, "/")
	escaped := make([]string, len(parts))
	for i, part := range parts {
		escaped[i] = p.escape(part)
	}
	return strings.Join(escaped, "/")
}

func (p *S3Provider) getCanonicalQueryString(parameters map[string]string) string {
	if len(parameters) == 0 {
		return ""
	}

	keys := make([]string, 0, len(parameters))
	for k := range parameters {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	pairs := make([]string, 0, len(keys))
	for _, k := range keys {
		pairs = append(pairs, p.escape(k)+"="+p.escape(parameters[k]))
	}
	return strings.Join(pairs, "&")
}

func (p *S3Provider) getCanonicalHeaders(oldHeaders map[string]string) (string, string) {
	headers := make(map[string]string)
	for k, v := range oldHeaders {
		headers[strings.ToLower(k)] = strings.TrimSpace(v)
	}

	keys := make([]string, 0, len(headers))
	for k := range headers {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var canonicalHeaders strings.Builder
	var signedHeaders []string
	for _, k := range keys {
		canonicalHeaders.WriteString(k + ":" + headers[k] + "\n")
		signedHeaders = append(signedHeaders, k)
	}

	return canonicalHeaders.String(), strings.Join(signedHeaders, ";")
}

func (p *S3Provider) parseXMLError(xmlStr string) string {
	// Simple XML parsing for S3 error messages
	// Look for <Message>...</Message> or <Error><Message>...</Message></Error>
	if idx := strings.Index(xmlStr, "<Message>"); idx != -1 {
		start := idx + len("<Message>")
		if end := strings.Index(xmlStr[start:], "</Message>"); end != -1 {
			return xmlStr[start : start+end]
		}
	}
	return ""
}

// Helper functions
func sha256HexS3(data string) string {
	h := sha256.New()
	h.Write([]byte(data))
	return hex.EncodeToString(h.Sum(nil))
}

func (p *S3Provider) hmacSHA256Bytes(data, key []byte) []byte {
	h := hmac.New(sha256.New, key)
	h.Write(data)
	return h.Sum(nil)
}

func (p *S3Provider) hmacSHA256Str(data, key string) []byte {
	h := hmac.New(sha256.New, []byte(key))
	h.Write([]byte(data))
	return h.Sum(nil)
}
