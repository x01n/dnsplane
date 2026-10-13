package providers

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"main/internal/cert"
	"main/internal/cert/deploy/base"

	"gopkg.in/yaml.v3"
)

func init() {
	base.Register("k8s", NewK8SProvider)
}

// K8SProvider Kubernetes证书部署器
type K8SProvider struct {
	base.BaseProvider
	client *http.Client
	server string
	token  string
}

// NewK8SProvider 创建K8S部署器
func NewK8SProvider(config map[string]interface{}) base.DeployProvider {
	return &K8SProvider{
		BaseProvider: base.BaseProvider{Config: config},
	}
}

// Check 检查K8S API连通性
func (p *K8SProvider) Check(ctx context.Context) error {
	if p.GetString("kubeconfig") == "" {
		return fmt.Errorf("Kubeconfig不能为空")
	}
	if err := p.parseKubeconfig(); err != nil {
		return err
	}
	code, _, err := p.k8sRequest(ctx, "GET", "/version", nil)
	if err != nil {
		return fmt.Errorf("连接API失败: %v", err)
	}
	if code != 200 {
		return fmt.Errorf("连接API失败: HTTP %d", code)
	}
	return nil
}

// Deploy 部署TLS Secret到K8S集群，可选更新Ingress
func (p *K8SProvider) Deploy(ctx context.Context, fullchain, privateKey string, config map[string]interface{}) error {
	namespace := p.GetStringFrom(config, "namespace")
	secretName := p.GetStringFrom(config, "secret_name")
	if namespace == "" {
		return fmt.Errorf("命名空间不能为空")
	}
	if secretName == "" {
		return fmt.Errorf("Secret名称不能为空")
	}

	if err := p.parseKubeconfig(); err != nil {
		return err
	}

	// 构建TLS Secret
	secretPayload := map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "Secret",
		"metadata": map[string]string{
			"name":      secretName,
			"namespace": namespace,
		},
		"type": "kubernetes.io/tls",
		"data": map[string]string{
			"tls.crt": base64.StdEncoding.EncodeToString([]byte(fullchain)),
			"tls.key": base64.StdEncoding.EncodeToString([]byte(privateKey)),
		},
	}

	// 检查Secret是否已存在
	secretURL := fmt.Sprintf("/api/v1/namespaces/%s/secrets/%s", namespace, secretName)
	code, _, err := p.k8sRequest(ctx, "GET", secretURL, nil)

	if code == 404 {
		// 创建新Secret
		createURL := fmt.Sprintf("/api/v1/namespaces/%s/secrets", namespace)
		p.Log(fmt.Sprintf("Secret %s 不存在，正在创建...", secretName))
		code, body, err := p.k8sRequest(ctx, "POST", createURL, secretPayload)
		if err != nil || code < 200 || code >= 300 {
			return fmt.Errorf("创建Secret失败(HTTP %d): %s %v", code, string(body), err)
		}
		p.Log(fmt.Sprintf("Secret %s 创建成功", secretName))
	} else if code >= 200 && code < 300 {
		// 更新已有Secret
		p.Log(fmt.Sprintf("Secret %s 已存在，正在更新...", secretName))
		patch := map[string]interface{}{
			"data": secretPayload["data"],
			"type": "kubernetes.io/tls",
		}
		code, body, err := p.k8sRequest(ctx, "PATCH", secretURL, patch)
		if err != nil || code < 200 || code >= 300 {
			return fmt.Errorf("更新Secret失败(HTTP %d): %s %v", code, string(body), err)
		}
		p.Log(fmt.Sprintf("Secret %s 更新成功", secretName))
	} else {
		return fmt.Errorf("查询Secret失败(HTTP %d): %v", code, err)
	}

	// 可选：更新Ingress TLS配置
	ingresses := p.GetStringFrom(config, "ingresses")
	if ingresses != "" {
		ingressBase := fmt.Sprintf("/apis/networking.k8s.io/v1/namespaces/%s/ingresses", namespace)
		for _, ingName := range strings.Split(ingresses, ",") {
			ingName = strings.TrimSpace(ingName)
			if ingName == "" {
				continue
			}
			if err := p.updateIngress(ctx, ingressBase, ingName, secretName); err != nil {
				return err
			}
		}
	}

	return nil
}

// updateIngress 更新Ingress的TLS配置
func (p *K8SProvider) updateIngress(ctx context.Context, basePath, ingName, secretName string) error {
	ingURL := basePath + "/" + ingName
	code, body, err := p.k8sRequest(ctx, "GET", ingURL, nil)
	if err != nil || code < 200 || code >= 300 {
		return fmt.Errorf("获取Ingress '%s' 失败(HTTP %d): %s %v", ingName, code, string(body), err)
	}

	var ing map[string]interface{}
	if err := json.Unmarshal(body, &ing); err != nil {
		return fmt.Errorf("解析Ingress JSON失败: %v", err)
	}

	spec, _ := ing["spec"].(map[string]interface{})
	if spec == nil {
		return fmt.Errorf("Ingress '%s' spec为空", ingName)
	}

	// 从rules中收集hosts
	hostsMap := make(map[string]bool)
	if rules, ok := spec["rules"].([]interface{}); ok {
		for _, r := range rules {
			if rule, ok := r.(map[string]interface{}); ok {
				if h, ok := rule["host"].(string); ok && h != "" {
					hostsMap[h] = true
				}
			}
		}
	}

	// 更新TLS配置
	tlsList, _ := spec["tls"].([]interface{})
	if tlsList == nil {
		tlsList = []interface{}{}
	}

	found := false
	for i, t := range tlsList {
		entry, _ := t.(map[string]interface{})
		if entry == nil {
			continue
		}
		if name, ok := entry["secretName"].(string); ok && name == secretName {
			found = true
			// 合并hosts
			if existingHosts, ok := entry["hosts"].([]interface{}); ok {
				for _, eh := range existingHosts {
					if h, ok := eh.(string); ok {
						hostsMap[h] = true
					}
				}
			}
			var allHosts []string
			for h := range hostsMap {
				allHosts = append(allHosts, h)
			}
			entry["hosts"] = allHosts
			tlsList[i] = entry
			break
		}
	}

	if !found {
		var hosts []string
		for h := range hostsMap {
			hosts = append(hosts, h)
		}
		tlsList = append(tlsList, map[string]interface{}{
			"secretName": secretName,
			"hosts":      hosts,
		})
	}

	patch := map[string]interface{}{
		"spec": map[string]interface{}{
			"tls": tlsList,
		},
	}

	code, body, err = p.k8sRequest(ctx, "PATCH", ingURL, patch)
	if err != nil || code < 200 || code >= 300 {
		return fmt.Errorf("更新Ingress '%s' 失败(HTTP %d): %s %v", ingName, code, string(body), err)
	}
	p.Log(fmt.Sprintf("Ingress '%s' TLS更新成功", ingName))
	return nil
}

// kubeConfig kubeconfig文件结构
type kubeConfig struct {
	CurrentContext string `yaml:"current-context"`
	Contexts       []struct {
		Name    string `yaml:"name"`
		Context struct {
			Cluster string `yaml:"cluster"`
			User    string `yaml:"user"`
		} `yaml:"context"`
	} `yaml:"contexts"`
	Clusters []struct {
		Name    string `yaml:"name"`
		Cluster struct {
			Server string `yaml:"server"`
			CAData string `yaml:"certificate-authority-data"`
		} `yaml:"cluster"`
	} `yaml:"clusters"`
	Users []struct {
		Name string `yaml:"name"`
		User struct {
			Token          string `yaml:"token"`
			ClientCertData string `yaml:"client-certificate-data"`
			ClientKeyData  string `yaml:"client-key-data"`
		} `yaml:"user"`
	} `yaml:"users"`
}

// parseKubeconfig 解析kubeconfig配置
func (p *K8SProvider) parseKubeconfig() error {
	kubeconfig := p.GetString("kubeconfig")

	var kcfg kubeConfig
	if err := yaml.Unmarshal([]byte(kubeconfig), &kcfg); err != nil {
		return fmt.Errorf("Kubeconfig格式错误: %v", err)
	}

	if kcfg.CurrentContext == "" {
		return fmt.Errorf("Kubeconfig缺少current-context")
	}

	// 查找当前context
	var clusterName, userName string
	for _, ctx := range kcfg.Contexts {
		if ctx.Name == kcfg.CurrentContext {
			clusterName = ctx.Context.Cluster
			userName = ctx.Context.User
			break
		}
	}
	if clusterName == "" || userName == "" {
		return fmt.Errorf("Context '%s' 未找到或不完整", kcfg.CurrentContext)
	}

	// 查找cluster server地址
	var server string
	for _, c := range kcfg.Clusters {
		if c.Name == clusterName {
			server = c.Cluster.Server
			break
		}
	}
	if server == "" {
		return fmt.Errorf("Cluster '%s' server未找到", clusterName)
	}
	p.server = strings.TrimRight(server, "/")

	// 查找用户认证信息
	var userToken, clientCertData, clientKeyData string
	for _, u := range kcfg.Users {
		if u.Name == userName {
			userToken = u.User.Token
			clientCertData = u.User.ClientCertData
			clientKeyData = u.User.ClientKeyData
			break
		}
	}
	p.token = userToken

	// 配置TLS客户端
	tlsConfig := &tls.Config{InsecureSkipVerify: true}
	if clientCertData != "" && clientKeyData != "" {
		certBytes, _ := base64.StdEncoding.DecodeString(clientCertData)
		keyBytes, _ := base64.StdEncoding.DecodeString(clientKeyData)
		pair, err := tls.X509KeyPair(certBytes, keyBytes)
		if err == nil {
			tlsConfig.Certificates = []tls.Certificate{pair}
		}
	}

	p.client = &http.Client{
		Timeout:   30 * time.Second,
		Transport: &http.Transport{TLSClientConfig: tlsConfig},
	}

	return nil
}

// k8sRequest 发送K8S API请求
func (p *K8SProvider) k8sRequest(ctx context.Context, method, path string, body interface{}) (int, []byte, error) {
	u := p.server + path
	var bodyReader io.Reader
	if body != nil {
		jsonBytes, _ := json.Marshal(body)
		bodyReader = bytes.NewReader(jsonBytes)
	}

	req, err := http.NewRequestWithContext(ctx, method, u, bodyReader)
	if err != nil {
		return 0, nil, err
	}

	if p.token != "" {
		req.Header.Set("Authorization", "Bearer "+p.token)
	}
	if body != nil {
		if method == "PATCH" {
			req.Header.Set("Content-Type", "application/strategic-merge-patch+json")
		} else {
			req.Header.Set("Content-Type", "application/json")
		}
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	return resp.StatusCode, respBody, err
}

// SetLogger 设置日志记录器
func (p *K8SProvider) SetLogger(logger cert.Logger) {
	p.BaseProvider.SetLogger(logger)
}
