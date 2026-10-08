package providers

import (
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	/* aliyunCASDefaultEndpoint 证书管理服务中国内地接入点 */
	aliyunCASDefaultEndpoint = "cas.aliyuncs.com"
	/* aliyunCASSingaporeEndpoint 证书管理服务新加坡接入点 */
	aliyunCASSingaporeEndpoint = "cas.ap-southeast-1.aliyuncs.com"
	/* aliyunCASVersion 证书管理 OpenAPI 版本号 */
	aliyunCASVersion = "2020-04-07"
)

/*
 * aliyunCertInfo 证书解析结果
 */
type aliyunCertInfo struct {
	certName string
	serialNo string
	commonCN string
}

/* parseAliyunCert 解析证书链首张证书，提取名称、序列号与通用名 */
func parseAliyunCert(fullchain string) (*aliyunCertInfo, error) {
	block, _ := pem.Decode([]byte(fullchain))
	if block == nil {
		return nil, errors.New("证书解析失败")
	}
	certObj, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, errors.New("证书解析失败: " + err.Error())
	}

	cn := strings.ReplaceAll(certObj.Subject.CommonName, "*.", "")
	return &aliyunCertInfo{
		certName: fmt.Sprintf("%s-%d", cn, certObj.NotBefore.Unix()),
		serialNo: strings.ToLower(certObj.SerialNumber.Text(16)),
		commonCN: certObj.Subject.CommonName,
	}, nil
}

/*
 * getAliyunCertID 查询或上传证书，返回证书管理服务中的 CertId 与证书名称
 */
func (p *AliyunProviderBase) getAliyunCertID(config map[string]interface{}, fullchain, privateKey string) (string, string, error) {
	certInfo, err := parseAliyunCert(fullchain)
	if err != nil {
		return "", "", err
	}

	endpoint := aliyunCASDefaultEndpoint
	if p.regionOf(config) == "ap-southeast-1" {
		endpoint = aliyunCASSingaporeEndpoint
	}
	client, err := p.client("cn-hangzhou")
	if err != nil {
		return "", "", err
	}

	listReq := newAliyunRequest("POST", endpoint, aliyunCASVersion, "ListUserCertificateOrder")
	listReq.QueryParams["Keyword"] = certInfo.commonCN
	listReq.QueryParams["OrderType"] = "CERT"
	listResult, err := callAliyun(client, listReq)
	if err != nil {
		return "", "", errors.New("查询证书列表失败：" + err.Error())
	}

	if intFromAny(listResult["TotalCount"]) > 0 {
		orders, _ := listResult["CertificateOrderList"].([]interface{})
		for _, item := range orders {
			order, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			serialNo := strings.ToLower(stringFromAny(order["SerialNo"]))
			if serialNo != certInfo.serialNo && !strings.Contains(serialNo, certInfo.serialNo) {
				continue
			}
			certID := stringFromAny(order["CertificateId"])
			if certID == "" {
				continue
			}
			p.Log("找到已上传的证书 CertId=" + certID)
			return certID, stringFromAny(order["Name"]), nil
		}
	}

	uploadReq := newAliyunRequest("POST", endpoint, aliyunCASVersion, "UploadUserCertificate")
	uploadReq.QueryParams["Name"] = certInfo.certName
	uploadReq.QueryParams["Cert"] = fullchain
	uploadReq.QueryParams["Key"] = privateKey
	uploadResult, err := callAliyun(client, uploadReq)
	if err != nil {
		return "", "", errors.New("上传证书失败：" + err.Error())
	}

	certID := stringFromAny(uploadResult["CertId"])
	if certID == "" {
		return "", "", errors.New("证书上传失败：未返回 CertId")
	}
	p.Log("证书上传成功！CertId=" + certID)
	time.Sleep(500 * time.Millisecond)
	return certID, certInfo.certName, nil
}

/* aliyunCertIDSuffix 证书ID地域后缀 */
func aliyunCertIDSuffix(region string) string {
	if region == "ap-southeast-1" {
		return "-ap-southeast-1"
	}
	return "-cn-hangzhou"
}

/* requireCertID 读取任务配置中的证书ID，缺失时返回错误 */
func (p *AliyunProviderBase) requireCertID(config map[string]interface{}) (string, error) {
	certID := p.GetStringFrom(config, "cert_id")
	if certID == "" {
		return "", errors.New("证书ID不能为空，需先上传证书到CAS")
	}
	return certID, nil
}
