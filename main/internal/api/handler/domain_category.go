package handler

import (
	"fmt"

	"main/internal/api/middleware"
	"main/internal/database"
	"main/internal/models"

	"github.com/gin-gonic/gin"
)

/**
 * GetDomainCategories 获取域名分类列表
 * @route GET /domains/categories
 */
func GetDomainCategories(c *gin.Context) {
	if !requireUserModule(c, "domain") {
		return
	}
	var categories []models.DomainCategory
	database.WithContext(c).Order("sort ASC, id DESC").Find(&categories)

	type categoryWithCount struct {
		models.DomainCategory
		DomainCount int64 `json:"domain_count"`
	}

	result := make([]categoryWithCount, 0, len(categories))
	for _, cat := range categories {
		var count int64
		database.WithContext(c).Model(&models.Domain{}).Where("cid = ? AND deleted_at IS NULL", cat.ID).Count(&count)
		result = append(result, categoryWithCount{
			DomainCategory: cat,
			DomainCount:    count,
		})
	}
	middleware.SuccessResponse(c, gin.H{"list": result})
}

type createDomainCategoryRequest struct {
	Name   string `json:"name" binding:"required"`
	Remark string `json:"remark"`
	Sort   int    `json:"sort"`
}

/**
 * CreateDomainCategory 创建域名分类
 * @route POST /domains/categories
 */
func CreateDomainCategory(c *gin.Context) {
	if !requireUserModule(c, "domain") {
		return
	}
	var req createDomainCategoryRequest
	if err := middleware.BindDecryptedData(c, &req); err != nil {
		middleware.ErrorResponse(c, "参数解析失败")
		return
	}
	if req.Name == "" {
		middleware.ErrorResponse(c, "分类名称不能为空")
		return
	}
	var existing models.DomainCategory
	if database.WithContext(c).Where("name = ?", req.Name).First(&existing).Error == nil {
		middleware.ErrorResponse(c, "分类名称已存在")
		return
	}
	cat := models.DomainCategory{
		Name:   req.Name,
		Remark: req.Remark,
		Sort:   req.Sort,
	}
	if err := database.WithContext(c).Create(&cat).Error; err != nil {
		middleware.ErrorResponse(c, "创建分类失败")
		return
	}
	middleware.SuccessMsg(c, "添加分类成功")
}

type updateDomainCategoryRequest struct {
	Name   string `json:"name" binding:"required"`
	Remark string `json:"remark"`
	Sort   int    `json:"sort"`
}

/**
 * UpdateDomainCategory 更新域名分类
 * @route POST /domains/categories/:id
 */
func UpdateDomainCategory(c *gin.Context) {
	if !requireUserModule(c, "domain") {
		return
	}
	id := c.Param("id")
	var cat models.DomainCategory
	if err := database.WithContext(c).First(&cat, id).Error; err != nil {
		middleware.ErrorResponse(c, "分类不存在")
		return
	}
	var req updateDomainCategoryRequest
	if err := middleware.BindDecryptedData(c, &req); err != nil {
		middleware.ErrorResponse(c, "参数解析失败")
		return
	}
	if req.Name == "" {
		middleware.ErrorResponse(c, "分类名称不能为空")
		return
	}
	var dup models.DomainCategory
	if database.WithContext(c).Where("name = ? AND id <> ?", req.Name, cat.ID).First(&dup).Error == nil {
		middleware.ErrorResponse(c, "分类名称已存在")
		return
	}
	database.WithContext(c).Model(&cat).Updates(map[string]interface{}{
		"name":   req.Name,
		"remark": req.Remark,
		"sort":   req.Sort,
	})
	middleware.SuccessMsg(c, "修改分类成功")
}

/**
 * DeleteDomainCategory 删除域名分类
 * @route POST /domains/categories/:id/delete
 */
func DeleteDomainCategory(c *gin.Context) {
	if !requireUserModule(c, "domain") {
		return
	}
	id := c.Param("id")
	var cat models.DomainCategory
	if err := database.WithContext(c).First(&cat, id).Error; err != nil {
		middleware.ErrorResponse(c, "分类不存在")
		return
	}
	var count int64
	database.WithContext(c).Model(&models.Domain{}).Where("cid = ? AND deleted_at IS NULL", cat.ID).Count(&count)
	if count > 0 {
		middleware.ErrorResponse(c, "该分类下存在域名，无法删除")
		return
	}
	database.WithContext(c).Delete(&cat)
	middleware.SuccessMsg(c, "删除分类成功")
}

type setDomainCategoryRequest struct {
	IDs []string `json:"ids" binding:"required"`
	CID uint     `json:"cid"`
}

/**
 * SetDomainCategory 批量设置域名分类
 * @route POST /domains/category
 */
func SetDomainCategory(c *gin.Context) {
	if !requireUserModule(c, "domain") {
		return
	}
	var req setDomainCategoryRequest
	if err := middleware.BindDecryptedData(c, &req); err != nil {
		middleware.ErrorResponse(c, "参数解析失败")
		return
	}
	if len(req.IDs) == 0 {
		middleware.ErrorResponse(c, "请选择要操作的域名")
		return
	}
	count := database.WithContext(c).Model(&models.Domain{}).Where("id IN ?", req.IDs).Update("cid", req.CID).RowsAffected
	middleware.SuccessMsg(c, fmt.Sprintf("成功设置%d个域名的分类", count))
}
