package providers

import (
	"context"
	"crypto/x509"
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

/* axisnowAPIBase AxisNow 接口地址 */
const axisnowAPIBase = "https://api.axisnow.io"

func init() {
	base.Register("axisnow", NewAxisNowProvider)
}

/* AxisNowProvider AxisNow 证书部署 */
type AxisNowProvider struct {
	base.BaseProvider
	token  string
	client *http.Client
}

/* NewAxisNowProvider 创建 AxisNow 部署器 */
func NewAxisNowProvider(config map[string]interface{}) base.DeployProvider {
	return &AxisNowProvider{
		BaseProvider: base.BaseProvider{Config: config},
		token:        base.GetConfigString(config, "token"),
		client:       &http.Client{Timeout: 30 * time.Second},
	}
}

func (p *AxisNowProvider) Check(ctx context.Context) error {
	if p.token == "" {
		return fmt.Errorf("Token不能为空")
	}
	_, err := p.request(ctx, "GET", "/client/v1/certificates", nil)
	if err != nil {
		return fmt.Errorf("Token无效或请求失败:%v", err)
	}
	return nil
}

/* Deploy 部署证书到 AxisNow */
func (p *AxisNowProvider) Deploy(ctx context.Context, fullchain, privateKey string, config map[string]interface{}) error {
	_, err := p.deploy(ctx, fullchain, privateKey, config, nil)
	return err
}

/*
 * DeployWithInfo 部署证书并回传证书 UUID
 */
func (p *AxisNowProvider) DeployWithInfo(ctx context.Context, fullchain, privateKey string, config map[string]interface{}, info *base.DeployInfo) error {
	_, err := p.deploy(ctx, fullchain, privateKey, config, info)
	return err
}

/* deploy 执行部署，info 非空时回传本次使用的证书 UUID */
func (p *AxisNowProvider) deploy(ctx context.Context, fullchain, privateKey string, config map[string]interface{}, info *base.DeployInfo) (string, error) {
	certName, err := axisnowCertName(fullchain)
	if err != nil {
		return "", err
	}

	uuid := base.GetConfigString(config, "uuid")
	if uuid != "" {
		updateErr := p.updateCertificate(ctx, uuid, certName, fullchain, privateKey)
		if updateErr != nil {
			p.Log("原证书获取或更新失败(" + updateErr.Error() + ")，将重新上传证书")
		} else {
			return p.saveUUID(info, uuid), nil
		}
	}
	params := map[string]interface{}{
		"type":        "upload",
		"name":        certName,
		"certificate": fullchain,
		"private_key": privateKey,
	}
	result, err := p.request(ctx, "POST", "/client/v1/certificates", params)
	if err != nil {
		return "", fmt.Errorf("上传证书失败:%v", err)
	}
	newUUID, _ := result["uuid"].(string)
	if newUUID == "" {
		return "", fmt.Errorf("上传证书失败:响应缺少 uuid")
	}
	p.Log("上传证书成功，UUID:" + newUUID)
	return p.saveUUID(info, newUUID), nil
}

func (p *AxisNowProvider) updateCertificate(ctx context.Context, uuid, certName, fullchain, privateKey string) error {
	if _, err := p.request(ctx, "GET", "/client/v1/certificates/"+uuid, nil); err != nil {
		return err
	}
	p.Log("证书" + certName + "已存在，UUID:" + uuid)
	params := map[string]interface{}{
		"name":        certName,
		"certificate": fullchain,
		"private_key": privateKey,
	}
	if _, err := p.request(ctx, "PUT", "/client/v1/certificates/"+uuid, params); err != nil {
		return err
	}
	p.Log("证书更新成功，UUID:" + uuid)
	return nil
}

/* saveUUID 把证书 UUID 写入回传信息并返回 */
func (p *AxisNowProvider) saveUUID(info *base.DeployInfo, uuid string) string {
	if info != nil {
		if info.Data == nil {
			info.Data = map[string]interface{}{}
		}
		info.Data["uuid"] = uuid
	}
	return uuid
}

func axisnowCertName(fullchain string) (string, error) {
	block, _ := pem.Decode([]byte(fullchain))
	if block == nil {
		return "", fmt.Errorf("证书解析失败")
	}
	certObj, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return "", fmt.Errorf("证书解析失败")
	}
	return fmt.Sprintf("%s-%d", strings.ReplaceAll(certObj.Subject.CommonName, "*.", ""), certObj.NotBefore.Unix()), nil
}

func (p *AxisNowProvider) request(ctx context.Context, method, path string, data map[string]interface{}) (map[string]interface{}, error) {
	var bodyReader io.Reader
	if data != nil {
		payload, err := json.Marshal(data)
		if err != nil {
			return nil, fmt.Errorf("序列化请求体失败: %v", err)
		}
		bodyReader = strings.NewReader(string(payload))
	}

	req, err := http.NewRequestWithContext(ctx, method, axisnowAPIBase+path, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %v", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.token)
	if data != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取响应失败: %v", err)
	}

	var result map[string]interface{}
	_ = json.Unmarshal(body, &result)
	if success, ok := result["success"]; ok && axisnowTruthy(success) {
		if inner, ok := result["result"].(map[string]interface{}); ok {
			return inner, nil
		}
		return map[string]interface{}{}, nil
	}
	return nil, fmt.Errorf("%s", axisnowErrorMessage(result, string(body)))
}

func axisnowErrorMessage(result map[string]interface{}, body string) string {
	var builder strings.Builder
	if errors, ok := result["errors"].([]interface{}); ok {
		for _, item := range errors {
			if obj, ok := item.(map[string]interface{}); ok {
				if msg, ok := obj["message"].(string); ok {
					builder.WriteString(msg + "; ")
				}
				continue
			}
			if msg, ok := item.(string); ok {
				builder.WriteString(msg + "; ")
			}
		}
	}
	message := strings.Trim(builder.String(), "; ")
	if message == "" && body != "" {
		message = axisnowTruncateRunes(body, 300)
	}
	if message == "" {
		message = "请求失败"
	}
	return message
}

func axisnowTruncateRunes(s string, limit int) string {
	runes := []rune(s)
	if len(runes) <= limit {
		return s
	}
	return string(runes[:limit])
}

func axisnowTruthy(v interface{}) bool {
	switch t := v.(type) {
	case bool:
		return t
	case float64:
		return t != 0
	case string:
		return t != "" && t != "0"
	case []interface{}:
		return len(t) > 0
	case map[string]interface{}:
		return len(t) > 0
	}
	return false
}

/* SetLogger 设置日志记录器 */
func (p *AxisNowProvider) SetLogger(logger cert.Logger) {
	p.BaseProvider.SetLogger(logger)
}
