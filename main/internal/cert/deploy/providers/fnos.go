package providers

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io"
	"net"
	"strings"
	"time"

	"main/internal/cert"
	"main/internal/cert/deploy/base"

	"golang.org/x/crypto/ssh"
)

func init() {
	base.Register("fnos", NewFNOSProvider)
}

// FNOSProvider 飞牛OS证书部署器
// 通过SSH连接飞牛OS，读取证书目录并按域名匹配更新
type FNOSProvider struct {
	base.BaseProvider
}

// NewFNOSProvider 创建飞牛OS部署器
func NewFNOSProvider(config map[string]interface{}) base.DeployProvider {
	return &FNOSProvider{
		BaseProvider: base.BaseProvider{Config: config},
	}
}

// Check 检查SSH连通性
func (p *FNOSProvider) Check(ctx context.Context) error {
	if p.GetString("host") == "" || p.GetString("username") == "" || p.GetString("password") == "" {
		return fmt.Errorf("主机地址、用户名和密码不能为空")
	}
	client, err := p.sshConnect()
	if err != nil {
		return err
	}
	defer client.Close()
	return nil
}

// Deploy 部署证书到飞牛OS
// 1. SSH连接 2. 读取/usr/trim/var/trim_connect/ssls/下的证书目录
// 3. 按域名匹配 4. 更新证书文件 5. 重启webdav/smbftpd/trim_nginx服务
func (p *FNOSProvider) Deploy(ctx context.Context, fullchain, privateKey string, config map[string]interface{}) error {
	p.Log("开始部署到飞牛OS")

	client, err := p.sshConnect()
	if err != nil {
		return err
	}
	defer client.Close()

	// 解析证书获取域名列表用于匹配
	certDomains := parseCertDomains(fullchain)
	if len(certDomains) == 0 {
		return fmt.Errorf("无法解析证书域名信息")
	}
	p.Log(fmt.Sprintf("证书域名: %s", strings.Join(certDomains, ", ")))

	// 读取飞牛OS证书目录列表
	sslBasePath := "/usr/trim/var/trim_connect/ssls"
	output, err := p.sshExec(client, fmt.Sprintf("ls -1 %s 2>/dev/null || echo ''", sslBasePath))
	if err != nil {
		return fmt.Errorf("读取证书目录失败: %v", err)
	}

	dirs := strings.Split(strings.TrimSpace(output), "\n")
	matched := false

	for _, dir := range dirs {
		dir = strings.TrimSpace(dir)
		if dir == "" {
			continue
		}

		certDir := fmt.Sprintf("%s/%s", sslBasePath, dir)

		// 读取现有证书文件以匹配域名
		existingCert, err := p.sshExec(client, fmt.Sprintf(
			"cat %s/cert.crt 2>/dev/null || cat %s/fullchain.pem 2>/dev/null || echo ''",
			certDir, certDir))
		if err != nil || strings.TrimSpace(existingCert) == "" {
			continue
		}

		// 解析现有证书域名
		existDomains := parseCertDomains(existingCert)
		if len(existDomains) == 0 {
			continue
		}

		// 检查域名是否匹配
		if !domainsMatch(certDomains, existDomains) {
			continue
		}

		matched = true
		p.Log(fmt.Sprintf("匹配到证书目录: %s", dir))

		// 更新证书文件
		if err := p.sshWriteFile(client, certDir+"/cert.crt", fullchain); err != nil {
			return fmt.Errorf("写入证书文件失败: %v", err)
		}
		p.Log("证书文件已更新")

		if err := p.sshWriteFile(client, certDir+"/private.key", privateKey); err != nil {
			return fmt.Errorf("写入私钥文件失败: %v", err)
		}
		p.Log("私钥文件已更新")
	}

	if !matched {
		return fmt.Errorf("未找到匹配的证书目录")
	}

	// 重启飞牛OS相关服务
	p.Log("重启相关服务...")
	services := []string{"webdav", "smbftpd", "trim_nginx"}
	for _, svc := range services {
		_, err := p.sshExec(client, fmt.Sprintf(
			"systemctl restart %s 2>/dev/null || service %s restart 2>/dev/null || true",
			svc, svc))
		if err != nil {
			p.Log(fmt.Sprintf("重启 %s 时出现警告: %v", svc, err))
		} else {
			p.Log(fmt.Sprintf("服务 %s 已重启", svc))
		}
	}

	p.Log("飞牛OS部署完成")
	return nil
}

// sshConnect 建立SSH连接（密码认证）
func (p *FNOSProvider) sshConnect() (*ssh.Client, error) {
	host := p.GetString("host")
	port := p.GetString("port")
	if port == "" {
		port = "22"
	}
	username := p.GetString("username")
	password := p.GetString("password")

	config := &ssh.ClientConfig{
		User:            username,
		Auth:            []ssh.AuthMethod{ssh.Password(password)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         10 * time.Second,
	}

	addr := net.JoinHostPort(host, port)
	client, err := ssh.Dial("tcp", addr, config)
	if err != nil {
		return nil, fmt.Errorf("SSH连接失败: %v", err)
	}

	return client, nil
}

// sshExec 执行SSH命令并返回输出
func (p *FNOSProvider) sshExec(client *ssh.Client, cmd string) (string, error) {
	session, err := client.NewSession()
	if err != nil {
		return "", err
	}
	defer session.Close()

	output, err := session.CombinedOutput(cmd)
	return string(output), err
}

// sshWriteFile 通过SCP协议写入文件到远程主机
func (p *FNOSProvider) sshWriteFile(client *ssh.Client, remotePath, content string) error {
	session, err := client.NewSession()
	if err != nil {
		return err
	}
	defer session.Close()

	// 确保目标目录存在
	dir := remotePath[:strings.LastIndex(remotePath, "/")]
	if dir != "" {
		mkdirSession, err := client.NewSession()
		if err == nil {
			mkdirSession.Run("mkdir -p " + dir)
			mkdirSession.Close()
		}
	}

	go func() {
		w, _ := session.StdinPipe()
		defer w.Close()
		filename := remotePath[strings.LastIndex(remotePath, "/")+1:]
		fmt.Fprintf(w, "C0644 %d %s\n", len(content), filename)
		io.WriteString(w, content)
		fmt.Fprint(w, "\x00")
	}()

	return session.Run("scp -t " + remotePath)
}

// parseCertDomains 从PEM证书中解析所有域名（CN + SANs）
func parseCertDomains(pemData string) []string {
	block, _ := pem.Decode([]byte(pemData))
	if block == nil {
		return nil
	}

	c, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil
	}

	domainSet := make(map[string]bool)
	if c.Subject.CommonName != "" {
		domainSet[c.Subject.CommonName] = true
	}
	for _, dns := range c.DNSNames {
		domainSet[dns] = true
	}

	var domains []string
	for d := range domainSet {
		domains = append(domains, d)
	}
	return domains
}

// domainsMatch 检查两个域名列表是否有交集
func domainsMatch(a, b []string) bool {
	set := make(map[string]bool, len(b))
	for _, d := range b {
		set[d] = true
	}
	for _, d := range a {
		if set[d] {
			return true
		}
	}
	return false
}

// SetLogger 设置日志记录器
func (p *FNOSProvider) SetLogger(logger cert.Logger) {
	p.BaseProvider.SetLogger(logger)
}
