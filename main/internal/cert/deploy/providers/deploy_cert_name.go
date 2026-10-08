package providers

import (
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"strings"
)

func parseLeafCertificate(fullchain string) (*x509.Certificate, error) {
	block, _ := pem.Decode([]byte(fullchain))
	if block == nil {
		return nil, fmt.Errorf("证书解析失败")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("证书解析失败")
	}
	return cert, nil
}

/* certCommonName 获取证书主域名 */
func certCommonName(fullchain string) (string, error) {
	cert, err := parseLeafCertificate(fullchain)
	if err != nil {
		return "", err
	}
	return cert.Subject.CommonName, nil
}

func buildCertName(fullchain string) (string, error) {
	cert, err := parseLeafCertificate(fullchain)
	if err != nil {
		return "", err
	}
	cn := strings.ReplaceAll(cert.Subject.CommonName, "*.", "")
	return fmt.Sprintf("%s-%d", cn, cert.NotBefore.Unix()), nil
}
