package servers

import (
	"main/internal/cert/deploy/base"
	"context"
	"fmt"
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

	conn, err := ftp.Dial(host+":"+port, ftp.DialWithTimeout(10*time.Second))
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
	certPath := p.GetStringFrom(config, "cert_path")
	keyPath := p.GetStringFrom(config, "key_path")

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
	dir := remotePath[:strings.LastIndex(remotePath, "/")]
	if dir != "" {
		conn.MakeDir(dir)
	}

	return conn.Stor(remotePath, strings.NewReader(content))
}

func (p *FTPProvider) SetLogger(logger cert.Logger) {
	p.BaseProvider.SetLogger(logger)
}
