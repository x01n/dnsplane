package servers

import (
	"context"
	"crypto/tls"
	"fmt"
	"main/internal/cert/deploy/base"
	"net"
	"strings"
	"time"

	"main/internal/cert"

	"github.com/jlaffaye/ftp"
)

func init() {
	base.Register("ftp", NewFTPProvider)
}

type FTPProvider struct {
	base.BaseProvider
}

func NewFTPProvider(config map[string]interface{}) base.DeployProvider {
	return &FTPProvider{
		BaseProvider: base.BaseProvider{Config: config},
	}
}

func (p *FTPProvider) Check(ctx context.Context) error {
	conn, err := p.connect()
	if err != nil {
		return err
	}
	defer conn.Quit()
	return nil
}

func (p *FTPProvider) connect() (*ftp.ServerConn, error) {
	host := p.GetString("host")
	port := p.GetString("port")
	if port == "" {
		port = "21"
	}
	username := p.GetString("username")
	password := p.GetString("password")

	addr := net.JoinHostPort(host, port)
	opts := []ftp.DialOption{ftp.DialWithTimeout(10 * time.Second)}
	if p.GetString("secure") == "1" {
		opts = append(opts, ftp.DialWithExplicitTLS(&tls.Config{
			MinVersion: tls.VersionTLS12,
			ServerName: host,
		}))
	}
	if p.GetString("passive") == "0" {
		opts = append(opts, ftp.DialWithDisabledEPSV(true))
	}

	conn, err := ftp.Dial(addr, opts...)
	if err != nil {
		return nil, fmt.Errorf("FTP连接失败: %w", err)
	}

	if err := conn.Login(username, password); err != nil {
		conn.Quit()
		return nil, fmt.Errorf("FTP登录失败: %w", err)
	}

	return conn, nil
}

func (p *FTPProvider) Deploy(ctx context.Context, fullchain, privateKey string, config map[string]interface{}) error {
	format := strings.TrimSpace(firstStringInMap(config, "format"))
	if format == "pfx" || format == "jks" {
		return fmt.Errorf("FTP 部署当前仅支持 PEM 格式")
	}
	certPath := firstStringInMap(config, "cert_path", "pem_cert_file")
	if certPath == "" {
		certPath = p.GetStringFrom(config, "cert_path")
	}
	keyPath := firstStringInMap(config, "key_path", "pem_key_file")
	if keyPath == "" {
		keyPath = p.GetStringFrom(config, "key_path")
	}

	conn, err := p.connect()
	if err != nil {
		return err
	}
	defer conn.Quit()

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

		// FTP 远端路径遍历防御（安全审计 H-1）
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
		if err := p.uploadFile(conn, targetCertPath, fullchain); err != nil {
			return fmt.Errorf("上传证书失败: %w", err)
		}

		p.Log("正在上传私钥文件: " + targetKeyPath)
		if err := p.uploadFile(conn, targetKeyPath, privateKey); err != nil {
			return fmt.Errorf("上传私钥失败: %w", err)
		}
	}

	p.Log("FTP部署完成")
	return nil
}

func (p *FTPProvider) uploadFile(conn *ftp.ServerConn, remotePath, content string) error {
	if i := strings.LastIndex(remotePath, "/"); i > 0 {
		conn.MakeDir(remotePath[:i])
	}

	return conn.Stor(remotePath, strings.NewReader(content))
}

func (p *FTPProvider) SetLogger(logger cert.Logger) {
	p.BaseProvider.SetLogger(logger)
}
