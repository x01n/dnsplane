package servers

import (
	"main/internal/cert/deploy/base"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"main/internal/cert"
)

func init() {
	base.Register("local", NewLocalProvider)
}

type LocalProvider struct {
	base.BaseProvider
}

func NewLocalProvider(config map[string]interface{}) base.DeployProvider {
	return &LocalProvider{
		BaseProvider: base.BaseProvider{Config: config},
	}
}

func (p *LocalProvider) Check(ctx context.Context) error {
	certPath := p.GetString("cert_path")
	keyPath := p.GetString("key_path")

	if certPath == "" {
		return fmt.Errorf("证书路径不能为空")
	}
	if keyPath == "" {
		return fmt.Errorf("私钥路径不能为空")
	}

	certDir := filepath.Dir(certPath)
	if _, err := os.Stat(certDir); os.IsNotExist(err) {
		return fmt.Errorf("证书目录不存在: %s", certDir)
	}

	keyDir := filepath.Dir(keyPath)
	if _, err := os.Stat(keyDir); os.IsNotExist(err) {
		return fmt.Errorf("私钥目录不存在: %s", keyDir)
	}

	return nil
}

func (p *LocalProvider) Deploy(ctx context.Context, fullchain, privateKey string, config map[string]interface{}) error {
	certPath := p.GetStringFrom(config, "cert_path")
	keyPath := p.GetStringFrom(config, "key_path")
	restartCmd := p.GetStringFrom(config, "restart_cmd")
	if restartCmd == "" {
		restartCmd = p.GetStringFrom(config, "reload_cmd")
	}

	domains := base.GetConfigDomains(config)
	if len(domains) == 0 {
		domains = []string{""}
	}

	for _, domain := range domains {
		targetCertPath := certPath
		targetKeyPath := keyPath
		if domain != "" {
			targetCertPath = strings.ReplaceAll(targetCertPath, "{domain}", domain)
			targetKeyPath = strings.ReplaceAll(targetKeyPath, "{domain}", domain)
		}

		p.Log("正在写入证书文件: " + targetCertPath)
		certDir := filepath.Dir(targetCertPath)
		if err := os.MkdirAll(certDir, 0755); err != nil {
			return fmt.Errorf("创建证书目录失败: %w", err)
		}
		if err := os.WriteFile(targetCertPath, []byte(fullchain), 0644); err != nil {
			return fmt.Errorf("写入证书失败: %w", err)
		}

		p.Log("正在写入私钥文件: " + targetKeyPath)
		keyDir := filepath.Dir(targetKeyPath)
		if err := os.MkdirAll(keyDir, 0755); err != nil {
			return fmt.Errorf("创建私钥目录失败: %w", err)
		}
		if err := os.WriteFile(targetKeyPath, []byte(privateKey), 0600); err != nil {
			return fmt.Errorf("写入私钥失败: %w", err)
		}
	}

	if restartCmd != "" {
		p.Log("正在执行重启命令: " + restartCmd)
		var cmd *exec.Cmd
		if isWindows() {
			cmd = exec.CommandContext(ctx, "cmd", "/c", restartCmd)
		} else {
			cmd = exec.CommandContext(ctx, "sh", "-c", restartCmd)
		}
		output, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("执行重启命令失败: %s, %w", string(output), err)
		}
		p.Log("重启命令执行成功")
	}

	p.Log("本地部署完成")
	return nil
}

func isWindows() bool {
	return os.PathSeparator == '\\' && os.PathListSeparator == ';'
}

func (p *LocalProvider) SetLogger(logger cert.Logger) {
	p.BaseProvider.SetLogger(logger)
}
