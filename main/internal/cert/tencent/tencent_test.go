package tencent

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"main/internal/cert"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const (
	testSecretID  = "AKIDTEST"
	testSecretKey = "SecretKeyTest"
	testTimestamp = int64(1759622400)
)

func newTestProvider() *Provider {
	return &Provider{secretID: testSecretID, secretKey: testSecretKey, email: "a@example.com"}
}

func TestSign(t *testing.T) {
	p := newTestProvider()

	payload := []byte(`{"DvAuthMethod":"DNS","DomainName":"example.com","ContactEmail":"a@example.com","CsrEncryptAlgo":"RSA","CsrKeyParameter":"2048"}`)
	want := "TC3-HMAC-SHA256 Credential=AKIDTEST/2025-10-05/ssl/tc3_request, " +
		"SignedHeaders=content-type;host, Signature=e484150be92c9bf009ef01d670fdd57324e015699bf6c79d3299d7e6812e6fb9"
	if got := p.sign(payload, testTimestamp); got != want {
		t.Fatalf("签名不一致\n got=%s\nwant=%s", got, want)
	}

	emptyWant := "TC3-HMAC-SHA256 Credential=AKIDTEST/2025-10-05/ssl/tc3_request, " +
		"SignedHeaders=content-type;host, Signature=b79bb1a6c0f311daef409d0f63f27767a3554288efcc03bb8fd59f3d95270fe5"
	if got := p.sign([]byte("{}"), testTimestamp); got != emptyWant {
		t.Fatalf("空参数签名不一致\n got=%s\nwant=%s", got, emptyWant)
	}
}

func TestMarshalParams(t *testing.T) {
	got, err := marshalParams(nil)
	if err != nil || string(got) != "{}" {
		t.Fatalf("nil 参数应序列化为 {}: %s, err=%v", got, err)
	}

	got, err = marshalParams(map[string]interface{}{"A": nil, "B": nil})
	if err != nil || string(got) != "{}" {
		t.Fatalf("全 nil 参数应序列化为 {}: %s, err=%v", got, err)
	}

	got, err = marshalParams(map[string]interface{}{"CertificateId": "abc", "Skip": nil})
	if err != nil || string(got) != `{"CertificateId":"abc"}` {
		t.Fatalf("应剔除 nil 值: %s, err=%v", got, err)
	}
}

func TestBuildDNSRecords(t *testing.T) {
	detail := map[string]interface{}{
		"DvAuthDetail": map[string]interface{}{
			"DvAuths": []interface{}{
				map[string]interface{}{
					"DvAuthKey":        "_dnsauth.example.com",
					"DvAuthValue":      "token-a",
					"DvAuthVerifyType": "TXT",
				},
				map[string]interface{}{
					"DvAuthKey":   "_dnsauth2.example.com",
					"DvAuthValue": "token-b",
				},
			},
		},
	}

	got := buildDNSRecords(detail)
	records := got["example.com"]
	if len(records) != 2 {
		t.Fatalf("分组结果不符: %#v", got)
	}
	if records[0].Name != "_dnsauth" || records[0].Type != "TXT" || records[0].Value != "token-a" {
		t.Fatalf("首条记录不符: %#v", records[0])
	}
	if records[1].Name != "_dnsauth2" || records[1].Type != "CNAME" || records[1].Value != "token-b" {
		t.Fatalf("第二条记录不符: %#v", records[1])
	}

	if empty := buildDNSRecords(map[string]interface{}{}); len(empty) != 0 {
		t.Fatalf("无 DvAuths 时应返回空映射: %#v", empty)
	}
}

func buildTestZip(t *testing.T, entries map[string]string) []byte {
	t.Helper()
	buf := new(bytes.Buffer)
	zw := zip.NewWriter(buf)
	for name, content := range entries {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("构造 ZIP 失败: %v", err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatalf("写入 ZIP 失败: %v", err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("关闭 ZIP 失败: %v", err)
	}
	return buf.Bytes()
}

func TestExtractCertificate(t *testing.T) {
	zipData := buildTestZip(t, map[string]string{
		"Nginx/example.com.key": "KEY-CONTENT",
		"Nginx/example.com.crt": "CRT-CONTENT",
		"Nginx/README.txt":      "ignored",
	})

	key, chain, err := extractCertificate(zipData)
	if err != nil {
		t.Fatalf("解压失败: %v", err)
	}
	if key != "KEY-CONTENT" || chain != "CRT-CONTENT" {
		t.Fatalf("解压内容不符: key=%q chain=%q", key, chain)
	}
}

func TestExtractCertificateMissingFile(t *testing.T) {
	zipData := buildTestZip(t, map[string]string{"Nginx/example.com.key": "KEY-CONTENT"})
	if _, _, err := extractCertificate(zipData); err == nil {
		t.Fatal("缺少 .crt 时应报错")
	}

	if _, _, err := extractCertificate([]byte("not a zip")); err == nil {
		t.Fatal("非 ZIP 数据应报错")
	}
}

func TestBuildCertResult(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("生成密钥失败: %v", err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "example.com"},
		NotBefore:    time.Unix(1700000000, 0),
		NotAfter:     time.Unix(1800000000, 0),
	}
	// 签发者主体即证书的 Issuer 字段来源，单独构造以便断言解析结果
	parent := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: "Test CA"},
		NotBefore:    time.Unix(1700000000, 0),
		NotAfter:     time.Unix(1800000000, 0),
		IsCA:         true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, parent, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("生成自签证书失败: %v", err)
	}
	fullChain := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))

	result, err := buildCertResult(fullChain, "PRIVATE-KEY")
	if err != nil {
		t.Fatalf("解析证书失败: %v", err)
	}
	if result.Issuer != "Test CA" || result.ValidFrom != 1700000000 || result.ValidTo != 1800000000 {
		t.Fatalf("证书信息不符: %#v", result)
	}
	if result.FullChain != fullChain || result.PrivateKey != "PRIVATE-KEY" {
		t.Fatal("证书内容未按原样返回")
	}

	if _, err := buildCertResult("not a pem", "k"); err == nil {
		t.Fatal("非法 PEM 应报错")
	}
}

func TestGetMainDomain(t *testing.T) {
	cases := map[string]string{
		"_dnsauth.example.com":     "example.com",
		"example.com":              "example.com",
		"*.example.com":            "example.com",
		"_dnsauth.sub.example.com": "example.com",
	}
	for in, want := range cases {
		if got := getMainDomain(in); got != want {
			t.Fatalf("getMainDomain(%q)=%q, want %q", in, got, want)
		}
	}
}

/*
 * TestRegistrationKeepsFormConfig 验证子包 init 注册工厂时不会覆盖 providers.go 中的表单配置
 * Register 的语义是「config.Type 为空只补全工厂」，本测试锁定该行为，防止表单字段被空配置清空
 */
func TestRegistrationKeepsFormConfig(t *testing.T) {
	provider, err := cert.GetProvider("tencent", map[string]interface{}{
		"secret_id": "id", "secret_key": "key", "email": "a@example.com",
	}, nil)
	if err != nil {
		t.Fatalf("tencent 应已注册工厂: %v", err)
	}
	if provider == nil {
		t.Fatal("tencent 工厂返回了 nil 实现")
	}

	cfg, ok := cert.GetProviderConfig("tencent")
	if !ok {
		t.Fatal("tencent 表单配置应存在")
	}
	if cfg.Name != "腾讯云免费SSL" || len(cfg.Config) == 0 {
		t.Fatalf("表单配置被覆盖: name=%q fields=%d", cfg.Name, len(cfg.Config))
	}
}

/*
 * TestFinalizeOrderEndToEnd 用本地假服务端跑通「关闭到期通知 → 取下载地址 → 下载 ZIP → 解压 → 解析」
 * 覆盖 tencent.go FinalizeOrder 的完整链路，校验请求头、下载与结果字段
 */
func TestFinalizeOrderEndToEnd(t *testing.T) {
	certPEM, keyPEM := selfSignedCertPEM(t)
	// 腾讯云按 nginx 模板打包：目录内同时存在 .key 与 .crt
	zipData := buildTestZip(t, map[string]string{
		"Nginx/example.com_bundle.key": keyPEM,
		"Nginx/example.com_bundle.crt": certPEM,
	})

	var zipSrv *httptest.Server
	zipSrv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(zipData)
	}))
	defer zipSrv.Close()

	var actions []string
	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-TC-Version") != defaultVersion {
			t.Errorf("X-TC-Version 不符: %s", r.Header.Get("X-TC-Version"))
		}
		if !strings.HasPrefix(r.Header.Get("Authorization"), "TC3-HMAC-SHA256 Credential="+testSecretID+"/") {
			t.Errorf("Authorization 头不符: %s", r.Header.Get("Authorization"))
		}
		action := r.Header.Get("X-TC-Action")
		actions = append(actions, action)

		switch action {
		case "ModifyCertificatesExpiringNotificationSwitch":
			_, _ = w.Write([]byte(`{"Response":{"RequestId":"r1"}}`))
		case "DescribeDownloadCertificateUrl":
			_, _ = w.Write([]byte(`{"Response":{"DownloadCertificateUrl":"` + zipSrv.URL + `/c.zip","DownloadFilename":"example.com.zip"}}`))
		default:
			t.Errorf("未预期的 Action: %s", action)
		}
	}))
	defer apiSrv.Close()

	p := newTestProvider()
	p.apiURL = apiSrv.URL
	p.client = apiSrv.Client()

	result, err := p.FinalizeOrder(context.Background(), []string{"example.com"}, &cert.OrderInfo{OrderURL: "cert-1"}, "RSA", "2048")
	if err != nil {
		t.Fatalf("签发失败: %v", err)
	}
	if result.Issuer != "Test CA" || result.FullChain != certPEM || result.PrivateKey != keyPEM {
		t.Fatalf("签发结果不符: issuer=%q chainLen=%d keyLen=%d", result.Issuer, len(result.FullChain), len(result.PrivateKey))
	}
	if result.ValidFrom != 1700000000 || result.ValidTo != 1800000000 {
		t.Fatalf("有效期不符: %d - %d", result.ValidFrom, result.ValidTo)
	}
	want := "ModifyCertificatesExpiringNotificationSwitch,DescribeDownloadCertificateUrl"
	if strings.Join(actions, ",") != want {
		t.Fatalf("调用顺序不符: %v", actions)
	}
}

/* selfSignedCertPEM 生成自签证书 PEM 与对应的私钥 PEM，供下载链路测试使用 */
func selfSignedCertPEM(t *testing.T) (certPEM, keyPEM string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("生成密钥失败: %v", err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "example.com"},
		NotBefore:    time.Unix(1700000000, 0),
		NotAfter:     time.Unix(1800000000, 0),
	}
	parent := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: "Test CA"},
		NotBefore:    time.Unix(1700000000, 0),
		NotAfter:     time.Unix(1800000000, 0),
		IsCA:         true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, parent, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("生成自签证书失败: %v", err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatalf("序列化私钥失败: %v", err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})),
		string(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}))
}
