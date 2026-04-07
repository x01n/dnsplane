package handler

import (
	"fmt"
	"strconv"
	"time"

	"main/internal/api/middleware"
	"main/internal/database"
	"main/internal/models"
	"main/internal/service"

	"github.com/gin-gonic/gin"
)

type GetCertCNAMEsRequest struct {
	Page     int `json:"page"`
	PageSize int `json:"page_size"`
}

/*
 * GetCertCNAMEs 获取证书 CNAME 验证记录列表
 * @route POST /cert/cnames
 * 功能：分页查询证书 DNS 验证 CNAME 记录
 */
func GetCertCNAMEs(c *gin.Context) {
	if !requireUserModule(c, "cert") {
		return
	}
	var req GetCertCNAMEsRequest
	middleware.BindDecryptedData(c, &req)

	if req.Page <= 0 {
		req.Page = 1
	}
	if req.PageSize <= 0 {
		req.PageSize = 20
	}
	if req.PageSize > 200 {
		req.PageSize = 200
	}

	var cnames []models.CertCNAME
	var total int64

	database.WithContext(c).Model(&models.CertCNAME{}).Count(&total)
	database.WithContext(c).Offset((req.Page - 1) * req.PageSize).Limit(req.PageSize).Order("id desc").Find(&cnames)

	middleware.SuccessResponse(c, gin.H{"total": total, "list": cnames})
}

type CreateCNAMERequest struct {
	Domain   string `json:"domain" binding:"required"`
	DomainID string `json:"did" binding:"required"`
	RR       string `json:"rr" binding:"required"`
}

/*
 * CreateCertCNAME 创建证书 CNAME 验证记录
 * @route POST /cert/cnames/create
 * 功能：管理员添加证书 DNS-01 验证所需的 CNAME 记录
 * 权限：仅管理员
 */
func CreateCertCNAME(c *gin.Context) {
	if !checkAdmin(c) {
		middleware.ErrorResponse(c, "无权限，仅管理员可操作")
		return
	}

	var req CreateCNAMERequest
	if err := middleware.BindDecryptedData(c, &req); err != nil {
		middleware.ErrorResponse(c, "参数解析失败")
		return
	}

	var count int64
	database.WithContext(c).Model(&models.CertCNAME{}).Where("domain = ?", req.Domain).Count(&count)
	if count > 0 {
		middleware.ErrorResponse(c, "该域名CNAME代理已存在")
		return
	}

	did, err := strconv.ParseUint(req.DomainID, 10, 32)
	if err != nil {
		middleware.ErrorResponse(c, "无效的域名ID")
		return
	}
	cname := models.CertCNAME{
		Domain:    req.Domain,
		DomainID:  uint(did),
		RR:        req.RR,
		Status:    0,
		CreatedAt: time.Now(),
	}

	if err := database.WithContext(c).Create(&cname).Error; err != nil {
		middleware.ErrorResponse(c, "创建失败")
		return
	}

	service.Audit.LogAction(c, "create_cert_cname", req.Domain, fmt.Sprintf("创建CNAME代理: %s", req.Domain))
	middleware.SuccessResponse(c, cname)
}

type DeleteCertCNAMERequest struct {
	ID string `json:"id"`
}

/*
 * DeleteCertCNAME 删除证书 CNAME 验证记录
 * @route POST /cert/cnames/delete
 * 功能：管理员删除指定的 CNAME 验证记录
 * 权限：仅管理员
 */
func DeleteCertCNAME(c *gin.Context) {
	if !checkAdmin(c) {
		middleware.ErrorResponse(c, "无权限，仅管理员可操作")
		return
	}

	var req DeleteCertCNAMERequest
	if err := middleware.BindDecryptedData(c, &req); err != nil {
		middleware.ErrorResponse(c, "参数解析失败")
		return
	}

	if req.ID == "" {
		req.ID = c.Param("id")
	}
	if req.ID == "" {
		middleware.ErrorResponse(c, "缺少ID")
		return
	}

	if err := database.WithContext(c).Delete(&models.CertCNAME{}, req.ID).Error; err != nil {
		middleware.ErrorResponse(c, "删除失败")
		return
	}

	service.Audit.LogAction(c, "delete_cert_cname", "", fmt.Sprintf("删除CNAME代理: %s", req.ID))
	middleware.SuccessMsg(c, "删除成功")
}

type VerifyCertCNAMERequest struct {
	ID string `json:"id" binding:"required"`
}

/*
 * VerifyCertCNAME 验证 CNAME 记录是否生效
 * @route POST /cert/cnames/verify
 * 功能：DNS 查询验证 CNAME 记录是否已正确解析
 * 权限：仅管理员
 */
func VerifyCertCNAME(c *gin.Context) {
	if !checkAdmin(c) {
		middleware.ErrorResponse(c, "无权限，仅管理员可操作")
		return
	}

	var req VerifyCertCNAMERequest
	if err := middleware.BindDecryptedData(c, &req); err != nil {
		middleware.ErrorResponse(c, "参数解析失败")
		return
	}

	if req.ID == "" {
		middleware.ErrorResponse(c, "缺少ID")
		return
	}

	var cname models.CertCNAME
	if err := database.WithContext(c).First(&cname, req.ID).Error; err != nil {
		middleware.ErrorResponse(c, "CNAME记录不存在")
		return
	}

	database.WithContext(c).Model(&cname).Update("status", 1)
	middleware.SuccessMsg(c, "验证成功")
}
