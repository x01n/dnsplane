package providers

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"main/internal/cert/deploy/base"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	"main/internal/cert"
)

func init() {
	base.Register("aws_cloudfront", NewAWSCloudFrontProvider)
}

/* AWS CloudFront XML 解析用预编译正则（避免循环内反复编译） */
var (
	cfViewerCertRe  = regexp.MustCompile(`(?s)<ViewerCertificate>.*?</ViewerCertificate>`)
	cfDistSummaryRe = regexp.MustCompile(`(?s)<DistributionSummary>.*?</DistributionSummary>`)
	cfIdRe          = regexp.MustCompile(`<Id>(.*?)</Id>`)
	cfNextMarkerRe  = regexp.MustCompile(`<NextMarker>(.*?)</NextMarker>`)
)

type AWSCloudFrontProvider struct {
	base.BaseProvider
	client *http.Client
}

func NewAWSCloudFrontProvider(config map[string]interface{}) base.DeployProvider {
	return &AWSCloudFrontProvider{
		BaseProvider: base.BaseProvider{Config: config},
		client:       &http.Client{Timeout: 30 * time.Second},
	}
}

func (p *AWSCloudFrontProvider) Check(ctx context.Context) error {
	accessKeyID := p.GetString("access_key_id")
	accessKeySecret := p.GetString("access_key_secret")

	if accessKeyID == "" || accessKeySecret == "" {
		return fmt.Errorf("AccessKey不能为空")
	}

	// Check ACM permission
	_, err := p.request(ctx, "acm", "us-east-1", "ListCertificates", map[string]interface{}{})
	return err
}

func (p *AWSCloudFrontProvider) Deploy(ctx context.Context, fullchain, privateKey string, config map[string]interface{}) error {
	// 1. Import Certificate to ACM (us-east-1 is required for CloudFront)
	p.Log("正在导入证书到 AWS ACM (us-east-1)...")

	certArn, err := p.importCertToACM(ctx, fullchain, privateKey)
	if err != nil {
		return fmt.Errorf("导入证书到ACM失败: %v", err)
	}
	p.Log("证书导入成功, ARN: " + certArn)

	distributionID := p.GetStringFrom(config, "distribution_id")

	// Support lookup by domain
	if distributionID == "" {
		domains := base.GetConfigDomains(config)
		if len(domains) > 0 {
			p.Log("未指定分发ID，尝试通过域名查找CloudFront分发: " + domains[0])
			var err error
			distributionID, err = p.findDistributionByDomain(ctx, domains[0])
			if err != nil {
				p.Log("查找分发失败: " + err.Error())
			} else {
				p.Log("找到分发ID: " + distributionID)
			}
		}
	}

	if distributionID == "" {
		p.Log("未指定CloudFront分发ID，仅上传证书")
		return nil
	}

	// 2. Update CloudFront Distribution
	p.Log("正在更新 CloudFront 分发配置: " + distributionID)
	if err := p.updateCloudFront(ctx, distributionID, certArn); err != nil {
		return fmt.Errorf("更新CloudFront失败: %v", err)
	}

	p.Log("CloudFront 分发配置更新成功")
	return nil
}

func (p *AWSCloudFrontProvider) importCertToACM(ctx context.Context, cert, key string) (string, error) {
	// ACM ImportCertificate
	params := map[string]interface{}{
		"Certificate": cert,
		"PrivateKey":  key,
	}
	// Attempt to find existing cert? AWS allows re-importing if same serial?
	// For simplicity, we just import. If it fails, we might need to handle it.
	// But usually for renewal we want a new import or re-import.

	// We might need CertificateChain if it's separate.
	// In ACME fullchain usually contains cert + intermediate.
	// AWS expects Certificate and CertificateChain separately if chain is involved.
	// We'll try splitting.

	parts := strings.SplitAfter(cert, "-----END CERTIFICATE-----")
	var mainCert, chain string
	if len(parts) > 0 {
		mainCert = strings.TrimSpace(parts[0])
	}
	if len(parts) > 1 {
		chain = strings.TrimSpace(strings.Join(parts[1:], ""))
	}

	params["Certificate"] = mainCert
	if chain != "" {
		params["CertificateChain"] = chain
	}

	// Retry loop for import? No.

	resp, err := p.request(ctx, "acm", "us-east-1", "ImportCertificate", params)
	if err != nil {
		return "", err
	}

	if arn, ok := resp["CertificateArn"].(string); ok {
		return arn, nil
	}
	return "", fmt.Errorf("未返回CertificateArn")
}

func (p *AWSCloudFrontProvider) updateCloudFront(ctx context.Context, distID, certArn string) error {
	// 1. Get Distribution Config
	configURL := fmt.Sprintf("https://cloudfront.amazonaws.com/2020-05-31/distribution/%s/config", distID)

	req, err := http.NewRequestWithContext(ctx, "GET", configURL, nil)
	if err != nil {
		return err
	}

	p.signCloudFront(req, nil)

	resp, err := p.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	if resp.StatusCode != 200 {
		return fmt.Errorf("GetConfig Error %d: %s", resp.StatusCode, string(bodyBytes))
	}

	etag := resp.Header.Get("ETag")
	if etag == "" {
		return fmt.Errorf("未获取到ETag")
	}

	xmlContent := string(bodyBytes)

	// 2. Replace ViewerCertificate
	// We use regex to replace the <ViewerCertificate>...</ViewerCertificate> block
	// New block:
	// <ViewerCertificate>
	//   <ACMCertificateArn>arn:aws:acm:...</ACMCertificateArn>
	//   <SSLSupportMethod>sni-only</SSLSupportMethod>
	//   <MinimumProtocolVersion>TLSv1.2_2021</MinimumProtocolVersion>
	//   <CertificateSource>acm</CertificateSource>
	// </ViewerCertificate>
	// Note: We need to respect existing fields if possible, but usually for ACM we overwrite.
	// dnsmgr logic: sets ACMCertificateArn, CloudFrontDefaultCertificate=false, removes Certificate, CertificateSource.

	newViewerCert := fmt.Sprintf(`<ViewerCertificate><ACMCertificateArn>%s</ACMCertificateArn><SSLSupportMethod>sni-only</SSLSupportMethod><MinimumProtocolVersion>TLSv1.2_2021</MinimumProtocolVersion><CertificateSource>acm</CertificateSource><CloudFrontDefaultCertificate>false</CloudFrontDefaultCertificate></ViewerCertificate>`, certArn)

	re := cfViewerCertRe
	if !re.MatchString(xmlContent) {
		return fmt.Errorf("未在配置中找到ViewerCertificate节点")
	}

	newXmlContent := re.ReplaceAllString(xmlContent, newViewerCert)

	// 3. Put Config
	newXmlBytes := []byte(newXmlContent)
	putReq, err := http.NewRequestWithContext(ctx, "PUT", configURL, bytes.NewReader(newXmlBytes))
	if err != nil {
		return err
	}

	putReq.Header.Set("If-Match", etag)
	putReq.Header.Set("Content-Type", "application/xml")

	p.signCloudFront(putReq, newXmlBytes)

	putResp, err := p.client.Do(putReq)
	if err != nil {
		return err
	}
	defer putResp.Body.Close()

	putBodyBytes, err := io.ReadAll(putResp.Body)
	if err != nil {
		return err
	}

	if putResp.StatusCode != 200 {
		return fmt.Errorf("PutConfig Error %d: %s", putResp.StatusCode, string(putBodyBytes))
	}

	return nil
}

func (p *AWSCloudFrontProvider) signCloudFront(req *http.Request, body []byte) {
	accessKeyID := p.GetString("access_key_id")
	accessKeySecret := p.GetString("access_key_secret")

	p.sign(req, accessKeyID, accessKeySecret, "us-east-1", "cloudfront", body)
}

// Generic AWS JSON-RPC Request (for ACM)
func (p *AWSCloudFrontProvider) request(ctx context.Context, service, region, target string, params map[string]interface{}) (map[string]interface{}, error) {
	accessKeyID := p.GetString("access_key_id")
	accessKeySecret := p.GetString("access_key_secret")

	host := fmt.Sprintf("%s.%s.amazonaws.com", service, region)
	endpoint := "https://" + host + "/"

	bodyBytes, _ := json.Marshal(params)

	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/x-amz-json-1.1")
	req.Header.Set("X-Amz-Target", "CertificateManager."+target)
	req.Header.Set("Host", host)

	p.sign(req, accessKeyID, accessKeySecret, region, service, bodyBytes)

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("AWS Error %d: %s", resp.StatusCode, string(respBody))
	}

	var result map[string]interface{}
	json.Unmarshal(respBody, &result)
	return result, nil
}

func (p *AWSCloudFrontProvider) sign(req *http.Request, ak, sk, region, service string, body []byte) {
	t := time.Now().UTC()
	amzDate := t.Format("20060102T150405Z")
	dateStamp := t.Format("20060102")

	req.Header.Set("X-Amz-Date", amzDate)

	// 1. Canonical Request
	canonicalUri := "/"
	canonicalQuery := ""

	// Headers
	// Host and X-Amz-Date are required
	// Content-Type and X-Amz-Target are also there

	var headerKeys []string
	headers := make(map[string]string)
	for k, v := range req.Header {
		lower := strings.ToLower(k)
		headers[lower] = strings.TrimSpace(v[0])
		headerKeys = append(headerKeys, lower)
	}
	sort.Strings(headerKeys)

	canonicalHeaders := ""
	for _, k := range headerKeys {
		canonicalHeaders += k + ":" + headers[k] + "\n"
	}
	signedHeaders := strings.Join(headerKeys, ";")

	hashBody := sha256.Sum256(body)
	hexBody := hex.EncodeToString(hashBody[:])

	canonicalRequest := fmt.Sprintf("%s\n%s\n%s\n%s\n%s\n%s",
		req.Method,
		canonicalUri,
		canonicalQuery,
		canonicalHeaders,
		signedHeaders,
		hexBody,
	)

	// 2. String to Sign
	algorithm := "AWS4-HMAC-SHA256"
	credentialScope := fmt.Sprintf("%s/%s/%s/aws4_request", dateStamp, region, service)

	hashCanonicalRequest := sha256.Sum256([]byte(canonicalRequest))
	stringToSign := fmt.Sprintf("%s\n%s\n%s\n%s",
		algorithm,
		amzDate,
		credentialScope,
		hex.EncodeToString(hashCanonicalRequest[:]),
	)

	// 3. Signature
	signingKey := getSignatureKey(sk, dateStamp, region, service)
	signature := hmacSHA256Bytes(signingKey, stringToSign)

	// 4. Authorization
	auth := fmt.Sprintf("%s Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		algorithm, ak, credentialScope, signedHeaders, signature)

	req.Header.Set("Authorization", auth)
}

func getSignatureKey(key, dateStamp, regionName, serviceName string) []byte {
	kDate := hmacSHA256AWS([]byte("AWS4"+key), dateStamp)
	kRegion := hmacSHA256AWS(kDate, regionName)
	kService := hmacSHA256AWS(kRegion, serviceName)
	kSigning := hmacSHA256AWS(kService, "aws4_request")
	return kSigning
}

func hmacSHA256AWS(key []byte, data string) []byte {
	h := hmac.New(sha256.New, key)
	h.Write([]byte(data))
	return h.Sum(nil)
}

func hmacSHA256Bytes(key []byte, data string) string {
	return hex.EncodeToString(hmacSHA256AWS(key, data))
}

func (p *AWSCloudFrontProvider) SetLogger(logger cert.Logger) {
	p.BaseProvider.SetLogger(logger)
}

func (p *AWSCloudFrontProvider) findDistributionByDomain(ctx context.Context, domain string) (string, error) {
	// ListDistributions
	// We need to iterate over distributions and check aliases (CNAMEs)
	// CloudFront ListDistributions API returns summary

	// CloudFront API 2020-05-31
	// ListDistributions is GET /2020-05-31/distribution

	reqURL := "https://cloudfront.amazonaws.com/2020-05-31/distribution"

	for {
		req, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
		if err != nil {
			return "", err
		}

		p.signCloudFront(req, nil)

		resp, err := p.client.Do(req)
		if err != nil {
			return "", err
		}
		defer resp.Body.Close()

		bodyBytes, err := io.ReadAll(resp.Body)
		if err != nil {
			return "", err
		}

		if resp.StatusCode != 200 {
			return "", fmt.Errorf("ListDistributions Error %d: %s", resp.StatusCode, string(bodyBytes))
		}

		// Parse XML
		// We use simple string search or regex for simplicity as we don't have XML struct
		// We are looking for <Id>...</Id> and <Alias>domain</Alias> inside <DistributionSummary>

		// Regex is fragile but might work for simple case.
		// Better to just string search?
		// Or try to use encoding/xml?
		// Given we can't easily add new structs, let's try regex for this specific task.

		xmlContent := string(bodyBytes)

		// Extract DistributionSummaries
		// <DistributionSummary>...</DistributionSummary>
		re := cfDistSummaryRe
		matches := re.FindAllString(xmlContent, -1)

		for _, m := range matches {
			// Check if domain matches any Alias
			// <Aliases>
			//   <Quantity>1</Quantity>
			//   <Items>
			//      <CNAME>example.com</CNAME>
			//   </Items>
			// </Aliases>

			// OR just <CNAME>domain</CNAME> ?
			// In 2020-05-31 API:
			// <Aliases>
			//    <Quantity>...</Quantity>
			//    <Items>
			//       <CNAME>...</CNAME>
			//    </Items>
			// </Aliases>

			if strings.Contains(m, "<CNAME>"+domain+"</CNAME>") {
				// Found it. Extract Id.
				idRe := cfIdRe
				idMatch := idRe.FindStringSubmatch(m)
				if len(idMatch) > 1 {
					return idMatch[1], nil
				}
			}
		}

		// Pagination: <NextMarker>...</NextMarker>
		markerRe := cfNextMarkerRe
		markerMatch := markerRe.FindStringSubmatch(xmlContent)
		if len(markerMatch) > 1 {
			marker := markerMatch[1]
			reqURL = "https://cloudfront.amazonaws.com/2020-05-31/distribution?Marker=" + url.QueryEscape(marker)
		} else {
			break
		}
	}

	return "", fmt.Errorf("未找到绑定域名 %s 的CloudFront分发", domain)
}
