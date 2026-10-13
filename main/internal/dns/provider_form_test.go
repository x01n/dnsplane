package dns_test

import (
	"testing"

	"main/internal/cert"
	"main/internal/cert/deploy"
	"main/internal/dns"

	// 下列包以空导入触发 init 注册，使注册表在测试进程中完整，避免断言空转
	_ "main/internal/dns/providers/aliyunesa"
	_ "main/internal/dns/providers/aws"
	_ "main/internal/dns/providers/baidu"
	_ "main/internal/dns/providers/bt"
	_ "main/internal/dns/providers/dnsla"
	_ "main/internal/dns/providers/dynv6"
	_ "main/internal/dns/providers/goedge"
	_ "main/internal/dns/providers/huoshan"
	_ "main/internal/dns/providers/jdcloud"
	_ "main/internal/dns/providers/namesilo"
	_ "main/internal/dns/providers/powerdns"
	_ "main/internal/dns/providers/qingcloud"
	_ "main/internal/dns/providers/spaceship"
	_ "main/internal/dns/providers/technitium"
	_ "main/internal/dns/providers/tencenteo"
	_ "main/internal/dns/providers/west"
)

// proxyFormProviders 曾经声明 proxy 表单字段的 DNS 提供商，用于确认注册表未空转
var proxyFormProviders = []string{
	"aliyunesa", "aws", "baidu", "bt", "dnsla", "dynv6", "goedge", "huoshan",
	"jdcloud", "namesilo", "powerdns", "qingcloud", "spaceship", "technitium",
	"tencenteo", "west",
}

/*
 * TestCertProviderFormsCarryNoProxyField 断言证书与部署提供商表单不含 proxy 键
 *
 * proxy 在全部提供商中只有写入没有读取；且自建面板部署器访问的是内网地址，
 * 接入公网代理反而无法连通，因此整体移除。本测试锁定移除结果不被回滚
 */
func TestCertProviderFormsCarryNoProxyField(t *testing.T) {
	certCfgs := cert.GetCertProviderConfigs()
	if len(certCfgs) == 0 {
		t.Fatal("证书提供商注册表为空，断言将空转")
	}
	for name, cfg := range certCfgs {
		assertNoProxyKey(t, "cert/"+name, certFieldKeys(cfg.Config))
	}

	deployCfgs := deploy.MergeDeployProviderConfigsForAPI(cert.GetDeployProviderConfigs())
	if len(deployCfgs) == 0 {
		t.Fatal("部署提供商注册表为空，断言将空转")
	}
	for name, cfg := range deployCfgs {
		assertNoProxyKey(t, "deploy/"+name, certFieldKeys(cfg.Config))
		assertNoProxyKey(t, "deploy/"+name+"/deploy_config", certFieldKeys(cfg.DeployConfig))
	}
}

/*
 * TestDNSProviderFormsCarryNoProxyField 断言 DNS 提供商表单不含 proxy 键
 *
 * 覆盖全部曾声明该字段的提供商，并要求其在注册表中真实存在
 */
func TestDNSProviderFormsCarryNoProxyField(t *testing.T) {
	all := dns.GetAllProviderConfigs()
	for _, name := range proxyFormProviders {
		cfg, ok := all[name]
		if !ok {
			t.Errorf("DNS 提供商 %s 未注册，断言将空转", name)
			continue
		}
		assertNoProxyKey(t, "dns/"+name, dnsFieldKeys(cfg.Config))
	}
}

/* certFieldKeys 提取证书配置字段的键名 */
func certFieldKeys(fields []cert.ConfigField) []string {
	keys := make([]string, 0, len(fields))
	for _, f := range fields {
		keys = append(keys, f.Key)
	}
	return keys
}

/* dnsFieldKeys 提取 DNS 配置字段的键名 */
func dnsFieldKeys(fields []dns.ConfigField) []string {
	keys := make([]string, 0, len(fields))
	for _, f := range fields {
		keys = append(keys, f.Key)
	}
	return keys
}

/* assertNoProxyKey 断言键名列表中不含 proxy */
func assertNoProxyKey(t *testing.T, scope string, keys []string) {
	t.Helper()
	for _, k := range keys {
		if k == "proxy" {
			t.Errorf("%s 仍暴露 proxy 字段", scope)
		}
	}
}
