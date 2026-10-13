package servers

import (
	"context"
	"fmt"
	"main/internal/cert/deploy/base"
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
	certPath := firstStringInMap(p.Config, "cert_path", "pem_cert_file")
	keyPath := firstStringInMap(p.Config, "key_path", "pem_key_file")

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
	format := strings.TrimSpace(firstStringInMap(config, "format"))
	if format == "pfx" || format == "jks" {
		return fmt.Errorf("本地部署当前仅支持 PEM 格式")
	}
	certPath := firstStringInMap(config, "cert_path", "pem_cert_file")
	if certPath == "" {
		certPath = p.GetStringFrom(config, "cert_path")
	}
	keyPath := firstStringInMap(config, "key_path", "pem_key_file")
	if keyPath == "" {
		keyPath = p.GetStringFrom(config, "key_path")
	}
	restartCmd := firstStringInMap(config, "restart_cmd", "reload_cmd", "cmd")
	if restartCmd == "" {
		restartCmd = p.GetStringFrom(config, "restart_cmd")
	}
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

		// 路径遍历防御（安全审计 H-1）：拒绝 `..` 上溯、控制字符、非绝对路径
		cleanCert, err := sanitizeLocalPath("cert_path", targetCertPath)
		if err != nil {
			return err
		}
		cleanKey, err := sanitizeLocalPath("key_path", targetKeyPath)
		if err != nil {
			return err
		}
		targetCertPath, targetKeyPath = cleanCert, cleanKey

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

	restartLines := splitExecLines(restartCmd)
	for _, line := range restartLines {
		p.Log("正在执行重启命令: " + line)
		var cmd *exec.Cmd
		if isWindows() {
			cmd = exec.CommandContext(ctx, "cmd", "/c", line)
		} else {
			cmd = exec.CommandContext(ctx, "sh", "-c", line)
		}
		output, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("执行重启命令失败: %s, %w", string(output), err)
		}
	}
	if len(restartLines) > 0 {
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
