package providers

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"strings"
	"time"

	"main/internal/cert"
	"main/internal/cert/deploy/base"
)

func init() {
	base.Register("synology", NewSynologyProvider)
}

// SynologyProvider 群晖NAS证书部署器
type SynologyProvider struct {
	base.BaseProvider
	client    *http.Client
	sid       string
	synoToken string
}

// NewSynologyProvider 创建群晖部署器
func NewSynologyProvider(config map[string]interface{}) base.DeployProvider {
	return &SynologyProvider{
		BaseProvider: base.BaseProvider{Config: config},
		client: &http.Client{
			Timeout: 30 * time.Second,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
			},
		},
	}
}

// Check 检查配置
func (p *SynologyProvider) Check(ctx context.Context) error {
	if p.GetString("url") == "" || p.GetString("username") == "" || p.GetString("password") == "" {
		return fmt.Errorf("面板地址、用户名和密码不能为空")
	}
	return p.login(ctx)
}

// Deploy 部署证书到群晖NAS
func (p *SynologyProvider) Deploy(ctx context.Context, fullchain, privateKey string, config map[string]interface{}) error {
	if err := p.login(ctx); err != nil {
		return fmt.Errorf("登录失败: %v", err)
	}

	desc := p.GetStringFrom(config, "desc")

	// 解析证书获取CN用于匹配
	var certCN string
	block, _ := pem.Decode([]byte(fullchain))
	if block != nil {
		if c, err := x509.ParseCertificate(block.Bytes); err == nil {
			certCN = c.Subject.CommonName
		}
	}

	// 列出已有证书
	certs, err := p.listCerts(ctx)
	if err != nil {
		return fmt.Errorf("获取证书列表失败: %v", err)
	}

	// 按描述或CN匹配已有证书
	var existingID string
	for _, c := range certs {
		if desc != "" && c.Desc == desc {
			existingID = c.ID
			p.Log(fmt.Sprintf("匹配到证书(描述匹配): ID=%s, 描述=%s", c.ID, c.Desc))
			break
		}
		if certCN != "" && c.Subject.CommonName == certCN {
			existingID = c.ID
			p.Log(fmt.Sprintf("匹配到证书(CN匹配): ID=%s, CN=%s", c.ID, c.Subject.CommonName))
			break
		}
	}

	// 导入证书
	if existingID != "" {
		p.Log(fmt.Sprintf("更新已有证书: %s", existingID))
		if err := p.importCert(ctx, fullchain, privateKey, desc, existingID); err != nil {
			return err
		}
		p.Log("证书更新成功")
	} else {
		if desc == "" {
			desc = "DNSPlane"
		}
		p.Log("上传新证书...")
		if err := p.importCert(ctx, fullchain, privateKey, desc, ""); err != nil {
			return err
		}
		p.Log("证书上传成功")
	}

	return nil
}

// login 登录群晖DSM获取sid和synotoken
func (p *SynologyProvider) login(ctx context.Context) error {
	baseURL := strings.TrimRight(p.GetString("url"), "/")
	version := p.GetString("version")

	cgi := "entry.cgi"
	if version == "1" {
		cgi = "auth.cgi" // DSM 6.x
	}
	u := fmt.Sprintf("%s/webapi/%s", baseURL, cgi)

	params := url.Values{}
	params.Set("api", "SYNO.API.Auth")
	params.Set("version", "6")
	params.Set("method", "login")
	params.Set("session", "webui")
	params.Set("account", p.GetString("username"))
	params.Set("passwd", p.GetString("password"))
	params.Set("format", "sid")
	params.Set("enable_syno_token", "yes")

	req, err := http.NewRequestWithContext(ctx, "POST", u, strings.NewReader(params.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("请求失败: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var result struct {
		Success bool `json:"success"`
		Data    struct {
			Sid       string `json:"sid"`
			SynoToken string `json:"synotoken"`
		} `json:"data"`
		Error struct {
			Code int `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return fmt.Errorf("解析响应失败: %v", err)
	}

	if !result.Success {
		return fmt.Errorf("登录失败(错误码: %d)", result.Error.Code)
	}

	p.sid = result.Data.Sid
	p.synoToken = result.Data.SynoToken
	return nil
}

// synoCert 群晖证书信息
type synoCert struct {
	ID      string `json:"id"`
	Desc    string `json:"desc"`
	Subject struct {
		CommonName string `json:"common_name"`
	} `json:"subject"`
}

// listCerts 列出群晖证书
func (p *SynologyProvider) listCerts(ctx context.Context) ([]synoCert, error) {
	baseURL := strings.TrimRight(p.GetString("url"), "/")
	u := fmt.Sprintf("%s/webapi/entry.cgi", baseURL)

	params := url.Values{}
	params.Set("api", "SYNO.Core.Certificate.CRT")
	params.Set("version", "1")
	params.Set("method", "list")
	params.Set("_sid", p.sid)
	if p.synoToken != "" {
		params.Set("SynoToken", p.synoToken)
	}

	req, err := http.NewRequestWithContext(ctx, "GET", u+"?"+params.Encode(), nil)
	if err != nil {
		return nil, err
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var result struct {
		Success bool `json:"success"`
		Data    struct {
			Certificates []synoCert `json:"certificates"`
		} `json:"data"`
		Error interface{} `json:"error"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("解析响应失败: %v", err)
	}

	if !result.Success {
		return nil, fmt.Errorf("获取证书列表失败: %v", result.Error)
	}

	return result.Data.Certificates, nil
}

// importCert 导入证书到群晖（multipart form上传）
func (p *SynologyProvider) importCert(ctx context.Context, fullchain, privateKey, desc, id string) error {
	baseURL := strings.TrimRight(p.GetString("url"), "/")
	u := fmt.Sprintf("%s/webapi/entry.cgi", baseURL)

	params := url.Values{}
	params.Set("api", "SYNO.Core.Certificate")
	params.Set("version", "1")
	params.Set("method", "import")
	params.Set("_sid", p.sid)
	if p.synoToken != "" {
		params.Set("SynoToken", p.synoToken)
	}

	// 构建multipart请求
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	// 私钥文件
	keyPart, _ := synoFormFile(writer, "key", "privkey.pem", "application/octet-stream")
	keyPart.Write([]byte(privateKey))

	// 证书文件
	certPart, _ := synoFormFile(writer, "cert", "fullchain.pem", "application/octet-stream")
	certPart.Write([]byte(fullchain))

	if id != "" {
		writer.WriteField("id", id)
	}
	writer.WriteField("desc", desc)
	writer.Close()

	req, err := http.NewRequestWithContext(ctx, "POST", u+"?"+params.Encode(), body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("请求失败: %v", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	var result struct {
		Success bool        `json:"success"`
		Error   interface{} `json:"error"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return fmt.Errorf("解析响应失败: %v", err)
	}

	if !result.Success {
		return fmt.Errorf("导入证书失败: %v", result.Error)
	}

	return nil
}

// synoFormFile 创建multipart表单文件part
func synoFormFile(w *multipart.Writer, fieldname, filename, contentType string) (io.Writer, error) {
	h := make(textproto.MIMEHeader)
	h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="%s"; filename="%s"`, fieldname, filename))
	h.Set("Content-Type", contentType)
	return w.CreatePart(h)
}

// SetLogger 设置日志记录器
func (p *SynologyProvider) SetLogger(logger cert.Logger) {
	p.BaseProvider.SetLogger(logger)
}
