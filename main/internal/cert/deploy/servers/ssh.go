package servers

import (
	"main/internal/cert/deploy/base"
	"context"
	"fmt"
	"io"
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
	credential := ""
	if authType == "key" {
		credential = p.GetString("private_key")
	} else {
		credential = p.GetString("password")
	}

	var auth []ssh.AuthMethod
	if authType == "key" {
		signer, err := ssh.ParsePrivateKey([]byte(credential))
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
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         10 * time.Second,
	}

	addr := net.JoinHostPort(host, port)
	client, err := ssh.Dial("tcp", addr, config)
	if err != nil {
		return nil, fmt.Errorf("SSH连接失败: %w", err)
	}

	return client, nil
}

func (p *SSHProvider) Deploy(ctx context.Context, fullchain, privateKey string, config map[string]interface{}) error {
	certPath := p.GetStringFrom(config, "cert_path")
	keyPath := p.GetStringFrom(config, "key_path")
	cmdPre := p.GetStringFrom(config, "cmd_pre")
	restartCmd := p.GetStringFrom(config, "cmd")

	client, err := p.connect()
	if err != nil {
		return err
	}
	defer client.Close()

	if cmdPre != "" {
		p.Log("正在执行上传前命令")
		if err := p.runCommand(client, cmdPre); err != nil {
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

		p.Log("正在上传证书文件: " + targetCertPath)
		if err := p.uploadFile(client, targetCertPath, fullchain, 0644); err != nil {
			return fmt.Errorf("上传证书失败: %w", err)
		}

		p.Log("正在上传私钥文件: " + targetKeyPath)
		if err := p.uploadFile(client, targetKeyPath, privateKey, 0600); err != nil {
			return fmt.Errorf("上传私钥失败: %w", err)
		}
	}

	if restartCmd != "" {
		p.Log("正在执行上传后命令")
		if err := p.runCommand(client, restartCmd); err != nil {
			return fmt.Errorf("执行重启命令失败: %w", err)
		}
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

	dir := remotePath[:strings.LastIndex(remotePath, "/")]
	if dir != "" {
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
