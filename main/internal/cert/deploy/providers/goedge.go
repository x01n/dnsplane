package providers

import (
	"bytes"
	"context"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"main/internal/cert"
	"main/internal/cert/deploy/base"
)

func init() {
	base.Register("goedge", NewGoEdgeProvider)
}

// GoEdgeProvider GoEdge/FlexCDN部署器
type GoEdgeProvider struct {
	base.BaseProvider
	panelURL    string
	accessKeyID string
	accessKey   string
	userType    string
	sysType     string
	accessToken string
	client      *http.Client
}

// NewGoEdgeProvider 创建GoEdge部署器
func NewGoEdgeProvider(config map[string]interface{}) base.DeployProvider {
	accessKeyID := base.GetConfigString(config, "accessKeyId")
	if accessKeyID == "" {
		accessKeyID = base.GetConfigString(config, "access_key")
	}
	accessKey := base.GetConfigString(config, "accessKey")
	if accessKey == "" {
		accessKey = base.GetConfigString(config, "access_secret")
	}
	return &GoEdgeProvider{
		BaseProvider: base.BaseProvider{Config: config},
		panelURL:     strings.TrimSuffix(base.GetConfigString(config, "url"), "/"),
		accessKeyID:  accessKeyID,
		accessKey:    accessKey,
		userType:     base.GetConfigString(config, "usertype"),
		sysType:      base.GetConfigString(config, "systype"),
		client:       &http.Client{Timeout: 30 * time.Second},
	}
}

func (p *GoEdgeProvider) Check(ctx context.Context) error {
	if p.panelURL == "" || p.accessKeyID == "" || p.accessKey == "" {
		return fmt.Errorf("必填参数不能为空")
	}
	return p.getAccessToken(ctx)
}

func (p *GoEdgeProvider) Deploy(ctx context.Context, fullchain, privateKey string, config map[string]interface{}) error {
	domains := base.GetConfigDomains(config)
	if len(domains) == 0 {
		domainsStr := p.GetString("domains")
		if domainsStr != "" {
			domains = base.SplitDomains(domainsStr)
		}
	}
	if len(domains) == 0 {
		return fmt.Errorf("没有设置要部署的域名")
	}

	if err := p.getAccessToken(ctx); err != nil {
		return err
	}

	// 获取证书列表
	params := map[string]interface{}{
		"domains": domains,
		"offset":  0,
		"size":    10,
	}
	resp, err := p.request(ctx, "/SSLCertService/listSSLCerts", params)
	if err != nil {
		return fmt.Errorf("获取证书列表失败：%v", err)
	}

	certsJSON, _ := resp["sslCertsJSON"].(string)
	var certList []map[string]interface{}
	if certsJSON != "" {
		decoded, _ := base64.StdEncoding.DecodeString(certsJSON)
		json.Unmarshal(decoded, &certList)
	}
	p.Log(fmt.Sprintf("获取证书列表成功(total=%d)", len(certList)))

	certInfo, err := p.parseCertInfo(fullchain)
	if err != nil {
		return err
	}
	certName := strings.ReplaceAll(certInfo.commonName, "*.", "") + "-" + fmt.Sprintf("%d", certInfo.notBefore)

	if len(certList) > 0 {
		for _, row := range certList {
			id := row["id"]
			name, _ := row["name"].(string)
			description, _ := row["description"].(string)
			serverName, _ := row["serverName"].(string)

			params := map[string]interface{}{
				"sslCertId":   id,
				"isOn":        true,
				"name":        name,
				"description": description,
				"serverName":  serverName,
				"isCA":        false,
				"certData":    base64.StdEncoding.EncodeToString([]byte(fullchain)),
				"keyData":     base64.StdEncoding.EncodeToString([]byte(privateKey)),
				"timeBeginAt": certInfo.notBefore,
				"timeEndAt":   certInfo.notAfter,
				"dnsNames":    domains,
				"commonNames": []string{certInfo.issuerCN},
			}
			_, err := p.request(ctx, "/SSLCertService/updateSSLCert", params)
			if err != nil {
				return fmt.Errorf("证书ID:%v更新失败：%v", id, err)
			}
			p.Log(fmt.Sprintf("证书ID:%v更新成功！", id))
		}
	} else {
		params := map[string]interface{}{
			"isOn":        true,
			"name":        certName,
			"description": certName,
			"serverName":  certInfo.commonName,
			"isCA":        false,
			"certData":    base64.StdEncoding.EncodeToString([]byte(fullchain)),
			"keyData":     base64.StdEncoding.EncodeToString([]byte(privateKey)),
			"timeBeginAt": certInfo.notBefore,
			"timeEndAt":   certInfo.notAfter,
			"dnsNames":    domains,
			"commonNames": []string{certInfo.issuerCN},
		}
		resp, err := p.request(ctx, "/SSLCertService/createSSLCert", params)
		if err != nil {
			return fmt.Errorf("添加证书失败：%v", err)
		}
		if certID, ok := resp["sslCertId"].(float64); ok {
			p.Log(fmt.Sprintf("证书ID:%d添加成功！", int(certID)))
		} else {
			p.Log("证书添加成功！")
		}
	}

	return nil
}

func (p *GoEdgeProvider) getAccessToken(ctx context.Context) error {
	params := map[string]interface{}{
		"type":        p.userType,
		"accessKeyId": p.accessKeyID,
		"accessKey":   p.accessKey,
	}

	resp, err := p.request(ctx, "/APIAccessTokenService/getAPIAccessToken", params)
	if err != nil {
		return err
	}

	if token, ok := resp["token"].(string); ok {
		p.accessToken = token
		return nil
	}
	return fmt.Errorf("登录成功，获取AccessToken失败")
}

func (p *GoEdgeProvider) request(ctx context.Context, path string, params map[string]interface{}) (map[string]interface{}, error) {
	reqURL := p.panelURL + path

	var bodyReader io.Reader
	if params != nil {
		bodyBytes, _ := json.Marshal(params)
		bodyReader = bytes.NewReader(bodyBytes)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", reqURL, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %v", err)
	}

	if p.accessToken != "" {
		if p.sysType == "1" {
			req.Header.Set("X-Cloud-Access-Token", p.accessToken)
		} else {
			req.Header.Set("X-Edge-Access-Token", p.accessToken)
		}
	}
	if params != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求失败: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取响应失败: %v", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("解析响应失败: %v", err)
	}

	if code, ok := result["code"].(float64); ok && code == 200 {
		if data, ok := result["data"].(map[string]interface{}); ok {
			return data, nil
		}
		return result, nil
	}

	if msg, ok := result["message"].(string); ok {
		return nil, fmt.Errorf("%s", msg)
	}
	return nil, fmt.Errorf("返回数据解析失败")
}

type goEdgeCertInfo struct {
	commonName string
	issuerCN   string
	notBefore  int64
	notAfter   int64
}

func (p *GoEdgeProvider) parseCertInfo(fullchain string) (*goEdgeCertInfo, error) {
	block, _ := pem.Decode([]byte(fullchain))
	if block == nil {
		return nil, fmt.Errorf("无法解析证书")
	}

	certObj, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("证书解析失败: %v", err)
	}

	return &goEdgeCertInfo{
		commonName: certObj.Subject.CommonName,
		issuerCN:   certObj.Issuer.CommonName,
		notBefore:  certObj.NotBefore.Unix(),
		notAfter:   certObj.NotAfter.Unix(),
	}, nil
}

func (p *GoEdgeProvider) SetLogger(logger cert.Logger) {
	p.BaseProvider.SetLogger(logger)
}
