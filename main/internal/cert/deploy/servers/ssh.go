package servers

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"main/internal/cert/deploy/base"
	"main/internal/logger"
	"net"
	"strings"
	"time"

	"main/internal/cert"

	"golang.org/x/crypto/ssh"
)

func init() {
	base.Register("ssh", NewSSHProvider)
}

type SSHProvider struct {
	base.BaseProvider
}

func NewSSHProvider(config map[string]interface{}) base.DeployProvider {
	return &SSHProvider{
		BaseProvider: base.BaseProvider{Config: config},
	}
}

func (p *SSHProvider) Check(ctx context.Context) error {
	client, err := p.connect()
	if err != nil {
		return err
	}
	defer client.Close()
	return nil
}

func (p *SSHProvider) connect() (*ssh.Client, error) {
	host := p.GetString("host")
	port := p.GetString("port")
	if port == "" {
		port = "22"
	}
	username := p.GetString("username")
	authType := p.GetString("auth_type")
	if authType == "" {
		if p.GetString("auth") == "1" {
			authType = "key"
		} else {
			authType = "password"
		}
	}
	credential := ""
	if authType == "key" {
		credential = firstStringInMap(p.Config, "private_key", "privatekey")
	} else {
		credential = p.GetString("password")
	}

	passphrase := p.GetString("passphrase")

	var auth []ssh.AuthMethod
	if authType == "key" {
		var signer ssh.Signer
		var err error
		if passphrase != "" {
			signer, err = ssh.ParsePrivateKeyWithPassphrase([]byte(credential), []byte(passphrase))
		} else {
			signer, err = ssh.ParsePrivateKey([]byte(credential))
		}
		if err != nil {
			return nil, fmt.Errorf("解析私钥失败: %w", err)
		}
		auth = append(auth, ssh.PublicKeys(signer))
	} else {
		auth = append(auth, ssh.Password(credential))
	}

	config := &ssh.ClientConfig{
		User:            username,
		Auth:            auth,
		HostKeyCallback: buildHostKeyCallback(host, p.GetString("host_key")),
		Timeout:         10 * time.Second,
	}

	addr := net.JoinHostPort(host, port)
	client, err := ssh.Dial("tcp", addr, config)
	if err != nil {
		return nil, fmt.Errorf("SSH连接失败: %w", err)
	}

	return client, nil
}

// buildHostKeyCallback 根据 host_key 配置返回 SSH 主机密钥校验回调。
//
// 修复项（安全审计 H-2）：
//   - 原实现固定使用 ssh.InsecureIgnoreHostKey()，对所有 SSH 目标永不校验主机密钥；
//     攻击者可在网络链路上实施 MITM，截获上传的 fullchain + privateKey。
//   - 现在支持在 CertDeploy.Config 中填写 host_key 字段（支持两种格式）：
//       1) known_hosts 整行（OpenSSH 格式，如 "host ssh-ed25519 AAAAC3..."）
//       2) "algo base64" 两段式（如 "ssh-ed25519 AAAAC3..."）
//   - 未配置 host_key 时回退到不校验但打 Warn 日志，保留与历史部署任务的兼容性；
//     建议运维尽快为每个目标录入 host_key。
func buildHostKeyCallback(host, hostKey string) ssh.HostKeyCallback {
	hostKey = strings.TrimSpace(hostKey)
	if hostKey == "" {
		logger.Warn("[ssh] 部署目标 %s 未配置 host_key，当前跳过主机密钥校验（MITM 风险）。请在部署配置中填入 known_hosts 行。", host)
		return ssh.InsecureIgnoreHostKey()
	}
	pubKey, err := parseHostKey(hostKey)
	if err != nil {
		logger.Warn("[ssh] host_key 解析失败，退回跳过校验: %v", err)
		return ssh.InsecureIgnoreHostKey()
	}
	return ssh.FixedHostKey(pubKey)
}

func parseHostKey(raw string) (ssh.PublicKey, error) {
	// 尝试 known_hosts 整行格式
	if _, _, pub, _, _, err := ssh.ParseKnownHosts([]byte(raw + "\n")); err == nil && pub != nil {
		return pub, nil
	}
	// 尝试 "algo base64" 两段式
	parts := strings.Fields(raw)
	if len(parts) < 2 {
		return nil, fmt.Errorf("host_key 格式无法识别（期望 known_hosts 行或 \"algo base64\"）")
	}
	data, err := base64.StdEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("base64 解码失败: %w", err)
	}
	return ssh.ParsePublicKey(data)
}

func (p *SSHProvider) Deploy(ctx context.Context, fullchain, privateKey string, config map[string]interface{}) error {
	format := strings.TrimSpace(firstStringInMap(config, "format"))
	if format == "pfx" || format == "jks" {
		return fmt.Errorf("SSH 部署当前仅支持 PEM 格式（与 dnsmgr 表单中的 PEM 选项一致）")
	}
	certPath := firstStringInMap(config, "cert_path", "pem_cert_file")
	if certPath == "" {
		certPath = p.GetStringFrom(config, "cert_path")
	}
	keyPath := firstStringInMap(config, "key_path", "pem_key_file")
	if keyPath == "" {
		keyPath = p.GetStringFrom(config, "key_path")
	}
	cmdPre := p.GetStringFrom(config, "cmd_pre")
	restartCmd := firstStringInMap(config, "cmd", "restart_cmd")
	if restartCmd == "" {
		restartCmd = p.GetStringFrom(config, "cmd")
	}

	client, err := p.connect()
	if err != nil {
		return err
	}
	defer client.Close()

	for _, line := range splitExecLines(cmdPre) {
		p.Log("正在执行上传前命令: " + line)
		if err := p.runCommand(client, line); err != nil {
			return fmt.Errorf("执行上传前命令失败: %w", err)
		}
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

		// 远端路径遍历防御（安全审计 H-1）
		cleanCert, err := sanitizeRemotePath("cert_path", targetCertPath)
		if err != nil {
			return err
		}
		cleanKey, err := sanitizeRemotePath("key_path", targetKeyPath)
		if err != nil {
			return err
		}
		targetCertPath, targetKeyPath = cleanCert, cleanKey

		p.Log("正在上传证书文件: " + targetCertPath)
		if err := p.uploadFile(client, targetCertPath, fullchain, 0644); err != nil {
			return fmt.Errorf("上传证书失败: %w", err)
		}

		p.Log("正在上传私钥文件: " + targetKeyPath)
		if err := p.uploadFile(client, targetKeyPath, privateKey, 0600); err != nil {
			return fmt.Errorf("上传私钥失败: %w", err)
		}
	}

	restartLines := splitExecLines(restartCmd)
	for _, line := range restartLines {
		p.Log("正在执行上传后命令: " + line)
		if err := p.runCommand(client, line); err != nil {
			return fmt.Errorf("执行重启命令失败: %w", err)
		}
	}
	if len(restartLines) > 0 {
		p.Log("命令执行成功")
	}

	p.Log("SSH部署完成")
	return nil
}

func (p *SSHProvider) uploadFile(client *ssh.Client, remotePath, content string, perm int) error {
	session, err := client.NewSession()
	if err != nil {
		return err
	}
	defer session.Close()

	if i := strings.LastIndex(remotePath, "/"); i > 0 {
		dir := remotePath[:i]
		mkdirSession, _ := client.NewSession()
		mkdirSession.Run("mkdir -p " + dir)
		mkdirSession.Close()
	}

	go func() {
		w, _ := session.StdinPipe()
		defer w.Close()
		fmt.Fprintf(w, "C%04o %d %s\n", perm, len(content), remotePath[strings.LastIndex(remotePath, "/")+1:])
		io.WriteString(w, content)
		fmt.Fprint(w, "\x00")
	}()

	return session.Run("scp -t " + remotePath)
}

func (p *SSHProvider) runCommand(client *ssh.Client, cmd string) error {
	session, err := client.NewSession()
	if err != nil {
		return err
	}
	defer session.Close()

	output, err := session.CombinedOutput(cmd)
	if err != nil {
		return fmt.Errorf("%s: %s", err, string(output))
	}
	return nil
}

func (p *SSHProvider) SetLogger(logger cert.Logger) {
	p.BaseProvider.SetLogger(logger)
}
