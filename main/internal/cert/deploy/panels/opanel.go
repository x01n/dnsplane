package panels

import (
	"main/internal/cert/deploy/base"
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"main/internal/cert"
)

func init() {
	base.Register("opanel", NewOPanelProvider)

	cert.Register("opanel", nil, cert.ProviderConfig{
		Type:     "opanel",
		Name:     "1Panel",
		Icon:     "1panel.png",
		IsDeploy: true,
		Config: []cert.ConfigField{
			{Name: "面板地址", Key: "url", Type: "input", Required: true, Placeholder: "https://xxx.com:8888"},
			{Name: "接口密钥", Key: "key", Type: "input", Required: true},
			{Name: "API版本", Key: "version", Type: "select", Options: []cert.ConfigOption{
				{Value: "v1", Label: "v1"},
				{Value: "v2", Label: "v2"},
			}, Value: "v1"},
			{Name: "部署类型", Key: "type", Type: "select", Options: []cert.ConfigOption{
				{Value: "0", Label: "网站证书"},
				{Value: "3", Label: "面板证书"},
			}, Value: "0"},
			{Name: "证书ID", Key: "id", Type: "input", Note: "指定证书ID时直接更新该证书"},
			{Name: "节点名称", Key: "node_name", Type: "input", Note: "多个节点名称换行分隔"},
		},
	})
}

type OPanelProvider struct {
	base.BaseProvider
	client *http.Client
}

func NewOPanelProvider(config map[string]interface{}) base.DeployProvider {
	return &OPanelProvider{
		BaseProvider: base.BaseProvider{Config: config},
		client:       &http.Client{Timeout: 30 * time.Second},
	}
}

func (p *OPanelProvider) Check(ctx context.Context) error {
	url := p.GetString("url")
	key := p.GetString("key")

	if url == "" {
		return fmt.Errorf("面板地址不能为空")
	}
	if key == "" {
		return fmt.Errorf("接口密钥不能为空")
	}

	_, err := p.request(ctx, "/settings/search", nil, "")
	return err
}

func (p *OPanelProvider) Deploy(ctx context.Context, fullchain, privateKey string, config map[string]interface{}) error {
	deployType := base.GetConfigString(config, "type")
	if deployType == "" {
		deployType = p.GetString("type")
	}
	if deployType == "" {
		deployType = "0"
	}

	// 解析节点名称列表
	nodeNames := p.parseNodeNames(config)

	if deployType == "3" {
		// 面板证书部署
		return p.deployPanel(ctx, fullchain, privateKey, nodeNames)
	}

	// 网站证书部署
	return p.deploySite(ctx, fullchain, privateKey, config, nodeNames)
}

func (p *OPanelProvider) deployPanel(ctx context.Context, fullchain, privateKey string, nodeNames []string) error {
	params := map[string]interface{}{
		"cert":    fullchain,
		"key":     privateKey,
		"ssl":     "Enable",
		"sslID":   nil,
		"sslType": "import-paste",
	}

	if len(nodeNames) == 0 {
		// 没有指定节点，只部署到主控节点
		_, err := p.request(ctx, "/core/settings/ssl/update", params, "")
		if err != nil {
			return fmt.Errorf("面板证书更新失败: %v", err)
		}
		p.Log("面板证书更新成功")
		return nil
	}

	// 同时部署到主节点和所有指定的子节点
	successCount := 0
	failCount := 0

	// 先更新主节点
	_, err := p.request(ctx, "/core/settings/ssl/update", params, "")
	if err != nil {
		p.Log("主节点面板证书更新失败: " + err.Error())
		failCount++
	} else {
		p.Log("主节点面板证书更新成功")
		successCount++
	}

	// 然后更新所有子节点
	for _, nodeName := range nodeNames {
		_, err := p.request(ctx, "/core/settings/ssl/update", params, nodeName)
		if err != nil {
			p.Log(fmt.Sprintf("节点 [%s] 面板证书更新失败: %v", nodeName, err))
			failCount++
		} else {
			p.Log(fmt.Sprintf("节点 [%s] 面板证书更新成功", nodeName))
			successCount++
		}
	}

	if failCount > 0 && successCount == 0 {
		return fmt.Errorf("所有节点证书更新失败")
	}

	return nil
}

func (p *OPanelProvider) deploySite(ctx context.Context, fullchain, privateKey string, config map[string]interface{}, nodeNames []string) error {
	// 如果指定了证书ID，直接更新
	certID := base.GetConfigString(config, "id")
	if certID == "" {
		certID = p.GetString("id")
	}

	if certID != "" {
		return p.updateCertByID(ctx, fullchain, privateKey, certID, nodeNames)
	}

	// 根据域名自动匹配证书
	return p.deployByDomain(ctx, fullchain, privateKey, config, nodeNames)
}

func (p *OPanelProvider) updateCertByID(ctx context.Context, fullchain, privateKey, certID string, nodeNames []string) error {
	params := map[string]interface{}{
		"sslID":       certID,
		"type":        "paste",
		"certificate": fullchain,
		"privateKey":  privateKey,
		"description": "",
	}

	if len(nodeNames) == 0 {
		_, err := p.request(ctx, "/websites/ssl/upload", params, "")
		if err != nil {
			return fmt.Errorf("证书ID:%s更新失败: %v", certID, err)
		}
		p.Log(fmt.Sprintf("证书ID:%s更新成功", certID))
		return nil
	}

	successCount := 0
	failCount := 0

	// 先更新主节点
	_, err := p.request(ctx, "/websites/ssl/upload", params, "")
	if err != nil {
		p.Log(fmt.Sprintf("主节点证书ID:%s更新失败: %v", certID, err))
		failCount++
	} else {
		p.Log(fmt.Sprintf("主节点证书ID:%s更新成功", certID))
		successCount++
	}

	// 然后更新所有子节点
	for _, nodeName := range nodeNames {
		_, err := p.request(ctx, "/websites/ssl/upload", params, nodeName)
		if err != nil {
			p.Log(fmt.Sprintf("节点 [%s] 证书ID:%s更新失败: %v", nodeName, certID, err))
			failCount++
		} else {
			p.Log(fmt.Sprintf("节点 [%s] 证书ID:%s更新成功", nodeName, certID))
			successCount++
		}
	}

	if failCount > 0 && successCount == 0 {
		return fmt.Errorf("所有节点证书更新失败")
	}

	return nil
}

func (p *OPanelProvider) deployByDomain(ctx context.Context, fullchain, privateKey string, config map[string]interface{}, nodeNames []string) error {
	domains := base.GetConfigDomains(config)
	if len(domains) == 0 {
		return fmt.Errorf("没有设置要部署的域名")
	}

	if len(nodeNames) == 0 {
		return p.deployToNode(ctx, fullchain, privateKey, domains, "")
	}

	successCount := 0
	failCount := 0

	// 先更新主节点
	err := p.deployToNode(ctx, fullchain, privateKey, domains, "")
	if err != nil {
		p.Log("主节点部署失败: " + err.Error())
		failCount++
	} else {
		successCount++
	}

	// 然后更新所有子节点
	for _, nodeName := range nodeNames {
		err := p.deployToNode(ctx, fullchain, privateKey, domains, nodeName)
		if err != nil {
			p.Log(fmt.Sprintf("节点 [%s] 部署失败: %v", nodeName, err))
			failCount++
		} else {
			successCount++
		}
	}

	if failCount > 0 && successCount == 0 {
		return fmt.Errorf("所有节点部署失败")
	}

	return nil
}

func (p *OPanelProvider) deployToNode(ctx context.Context, fullchain, privateKey string, domains []string, nodeName string) error {
	// 获取证书列表
	listParams := map[string]interface{}{
		"page":     1,
		"pageSize": 500,
	}

	nodeLabel := ""
	if nodeName != "" {
		nodeLabel = fmt.Sprintf("节点 [%s] ", nodeName)
	}

	data, err := p.request(ctx, "/websites/ssl/search", listParams, nodeName)
	if err != nil {
		return fmt.Errorf("%s获取证书列表失败: %v", nodeLabel, err)
	}

	total := 0
	if t, ok := data["total"].(float64); ok {
		total = int(t)
	}
	p.Log(fmt.Sprintf("%s获取证书列表成功(total=%d)", nodeLabel, total))

	success := 0
	var lastErr error

	if items, ok := data["items"].([]interface{}); ok {
		for _, item := range items {
			row, ok := item.(map[string]interface{})
			if !ok {
				continue
			}

			primaryDomain, _ := row["primaryDomain"].(string)
			if primaryDomain == "" {
				continue
			}

			// 收集证书的所有域名
			certDomains := []string{primaryDomain}
			if domainsStr, ok := row["domains"].(string); ok && domainsStr != "" {
				certDomains = append(certDomains, strings.Split(domainsStr, ",")...)
			}

			// 检查是否匹配
			matched := false
			for _, certDomain := range certDomains {
				for _, targetDomain := range domains {
					if certDomain == targetDomain {
						matched = true
						break
					}
					// 检查通配符匹配
					if strings.HasPrefix(targetDomain, "*.") {
						wildcardSuffix := targetDomain[1:] // 移除 *
						if strings.HasSuffix(certDomain, wildcardSuffix) || certDomain == targetDomain[2:] {
							matched = true
							break
						}
					}
				}
				if matched {
					break
				}
			}

			if matched {
				certID, _ := row["id"].(float64)
				params := map[string]interface{}{
					"sslID":       int(certID),
					"type":        "paste",
					"certificate": fullchain,
					"privateKey":  privateKey,
					"description": "",
				}

				_, err := p.request(ctx, "/websites/ssl/upload", params, nodeName)
				if err != nil {
					lastErr = err
					p.Log(fmt.Sprintf("%s证书ID:%d更新失败: %v", nodeLabel, int(certID), err))
				} else {
					p.Log(fmt.Sprintf("%s证书ID:%d更新成功", nodeLabel, int(certID)))
					success++
				}
			}
		}
	}

	// 如果没有匹配到任何证书，上传新证书
	if success == 0 {
		params := map[string]interface{}{
			"sslID":       0,
			"type":        "paste",
			"certificate": fullchain,
			"privateKey":  privateKey,
			"description": "",
		}

		_, err := p.request(ctx, "/websites/ssl/upload", params, nodeName)
		if err != nil {
			return fmt.Errorf("%s证书上传失败: %v", nodeLabel, err)
		}
		p.Log(fmt.Sprintf("%s证书上传成功", nodeLabel))
	}

	if success == 0 && lastErr != nil {
		return lastErr
	}

	return nil
}

func (p *OPanelProvider) parseNodeNames(config map[string]interface{}) []string {
	nodeNameStr := base.GetConfigString(config, "node_name")
	if nodeNameStr == "" {
		nodeNameStr = p.GetString("node_name")
	}
	if nodeNameStr == "" {
		return nil
	}

	nodeNameStr = strings.TrimSpace(nodeNameStr)
	if nodeNameStr == "" {
		return nil
	}

	// 按行分割，过滤空行
	lines := strings.Split(nodeNameStr, "\n")
	var nodeNames []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			nodeNames = append(nodeNames, line)
		}
	}

	return nodeNames
}

func (p *OPanelProvider) request(ctx context.Context, path string, params map[string]interface{}, nodeName string) (map[string]interface{}, error) {
	panelURL := strings.TrimSuffix(p.GetString("url"), "/")
	apiKey := p.GetString("key")
	version := p.GetString("version")
	if version == "" {
		version = "v1"
	}

	fullURL := fmt.Sprintf("%s/api/%s%s", panelURL, version, path)

	timestamp := fmt.Sprintf("%d", time.Now().Unix())
	token := md5Sum("1panel" + apiKey + timestamp)

	var bodyReader io.Reader
	if params != nil {
		bodyBytes, err := json.Marshal(params)
		if err != nil {
			return nil, fmt.Errorf("序列化请求体失败: %v", err)
		}
		bodyReader = strings.NewReader(string(bodyBytes))
	} else {
		bodyReader = strings.NewReader("{}")
	}

	req, err := http.NewRequestWithContext(ctx, "POST", fullURL, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %v", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("1Panel-Token", token)
	req.Header.Set("1Panel-Timestamp", timestamp)

	// 只有子节点时才设置 CurrentNode 头
	if nodeName != "" {
		req.Header.Set("CurrentNode", nodeName)
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
		return nil, fmt.Errorf("解析响应失败: %s", string(body))
	}

	// 检查响应状态
	if code, ok := result["code"].(float64); ok && code == 200 {
		if data, ok := result["data"].(map[string]interface{}); ok {
			return data, nil
		}
		return nil, nil
	}

	// 返回错误信息
	if msg, ok := result["message"].(string); ok {
		return nil, fmt.Errorf("%s", msg)
	}

	return nil, fmt.Errorf("请求失败(httpCode=%d)", resp.StatusCode)
}

func md5Sum(s string) string {
	h := md5.New()
	h.Write([]byte(s))
	return hex.EncodeToString(h.Sum(nil))
}

func (p *OPanelProvider) SetLogger(logger cert.Logger) {
	p.BaseProvider.SetLogger(logger)
}
