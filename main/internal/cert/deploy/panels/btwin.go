package panels

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"main/internal/cert/deploy/base"
	"net/http"
	"net/url"
	"strings"
	"time"
)

func init() { base.Register("btwin", NewBTWinProvider) }

type BTWinProvider struct { base.BaseProvider; client *http.Client }

func NewBTWinProvider(config map[string]interface{}) base.DeployProvider {
	return &BTWinProvider{BaseProvider: base.BaseProvider{Config: config}, client: &http.Client{Timeout: 60 * time.Second}}
}

func (p *BTWinProvider) Check(ctx context.Context) error {
	_, err := p.request(ctx, "/config/get_config", url.Values{})
	return err
}

func (p *BTWinProvider) Deploy(ctx context.Context, fullchain, privateKey string, config map[string]interface{}) error {
	deployType := base.GetConfigString(config, "type")
	if deployType == "1" {
		_, err := p.request(ctx, "/config/set_panel_ssl", url.Values{"ssl_key": {privateKey}, "ssl_pem": {fullchain}})
		if err != nil { return err }
		p.Log("面板证书部署成功")
		return nil
	}
	isIIS := base.GetConfigBool(config, "is_iis")
	if isIIS { return fmt.Errorf("btwin: IIS 模式暂未实现，请选择 nginx 模式") }
	sites := base.SplitDomains(base.GetConfigString(config, "sites"))
	if len(sites) == 0 { return fmt.Errorf("btwin: 要部署的网站不存在") }
	for _, site := range sites {
		siteID, err := p.getSiteID(ctx, site)
		if err != nil { return err }
		_, err = p.request(ctx, "/site/set_site_ssl", url.Values{"siteid": {fmt.Sprintf("%d", siteID)}, "status": {"true"}, "sslType": {""}, "cert": {fullchain}, "key": {privateKey}})
		if err != nil { return err }
		p.Log("网站 " + site + " 证书部署成功")
	}
	return nil
}

func (p *BTWinProvider) getSiteID(ctx context.Context, siteName string) (int, error) {
	result, err := p.request(ctx, "/datalist/get_data_list", url.Values{"table": {"sites"}, "search_type": {"PHP"}, "search": {siteName}, "p": {"1"}, "limit": {"10"}, "type": {"-1"}})
	if err != nil { return 0, err }
	data, ok := result["data"].([]interface{})
	if !ok { return 0, fmt.Errorf("btwin: 站点不存在: %s", siteName) }
	for _, item := range data {
		row, _ := item.(map[string]interface{})
		if fmt.Sprintf("%v", row["name"]) == siteName { return int(row["id"].(float64)), nil }
	}
	return 0, fmt.Errorf("btwin: 站点不存在: %s", siteName)
}

func (p *BTWinProvider) request(ctx context.Context, path string, values url.Values) (map[string]interface{}, error) {
	baseURL := strings.TrimSuffix(p.GetString("url"), "/")
	key := p.GetString("key")
	if baseURL == "" || key == "" { return nil, fmt.Errorf("btwin: 请填写面板地址和接口密钥") }
	now := fmt.Sprintf("%d", time.Now().Unix())
	values.Set("request_time", now)
	values.Set("request_token", md5Hash(now+md5Hash(key)))
	req, err := http.NewRequestWithContext(ctx, "POST", baseURL+path, strings.NewReader(values.Encode()))
	if err != nil { return nil, err }
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := p.client.Do(req)
	if err != nil { return nil, fmt.Errorf("btwin: %w", err) }
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil { return nil, fmt.Errorf("btwin: %s", string(body)) }
	if status, ok := result["status"].(bool); ok && !status {
		if msg, ok := result["msg"].(string); ok && msg != "" { return nil, fmt.Errorf("btwin: %s", msg) }
		return nil, fmt.Errorf("btwin: 请求失败")
	}
	return result, nil
}
