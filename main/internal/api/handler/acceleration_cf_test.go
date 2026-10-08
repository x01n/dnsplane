package handler

import (
	"context"
	"strings"
	"testing"

	"main/internal/models"
	"main/internal/service"
)

/*
 * TestAccelerateCFReportsConfigErrors 覆盖 CF 加速在配置不完整或回落域名不可用时必须报错
 *
 * 旧实现在这两种情况下都会跳过创建、直接返回 Status:"success"，
 * 调用方据此把 CNAME 写到优选域名上，解析最终指向一个云端并不存在的接入点。
 * 这里用「CFDomainID 指向不存在的域名」制造真实失败，无需网络即可复现
 */
func TestAccelerateCFReportsConfigErrors(t *testing.T) {
	cases := []struct {
		name        string
		domainID    uint
		zoneID      string
		wantContain string
	}{
		{name: "缺少SaaS回落域名", domainID: 0, zoneID: "zone1", wantContain: "缺少 SaaS 回落域名"},
		{name: "缺少ZoneID", domainID: 1, zoneID: "", wantContain: "缺少 SaaS 回落域名"},
		{name: "回落域名不存在", domainID: 999, zoneID: "zone1", wantContain: "CF 加速域名不可用"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setupHandlerTestDB(t)

			cfg := &service.AccelConfig{
				CFEnabled:      true,
				CFPreferDomain: "cdn.example.com",
				CFZoneID:       tc.zoneID,
				CFDomainID:     tc.domainID,
			}

			got := accelerateCF(context.Background(), cfg, "shop.example.com", models.Domain{Name: "example.com"})

			if got.Status != "error" {
				t.Fatalf("应返回 error，实际 status=%q msg=%q，CNAME=%q", got.Status, got.Msg, got.CNAME)
			}
			if got.CNAME != "" {
				t.Errorf("失败时不应给出 CNAME，实际 %q", got.CNAME)
			}
			if !strings.Contains(got.Msg, tc.wantContain) {
				t.Errorf("错误信息应包含 %q，实际 %q", tc.wantContain, got.Msg)
			}
		})
	}
}

/*
 * TestAccelerateCFSkipsWhenDisabled 断言 CF 未启用或未配置优选域名时仍走合法跳过
 *
 * 跳过是既有语义，本次改动只把「配置了但不可用」从成功改为失败，不能牵连这一分支
 */
func TestAccelerateCFSkipsWhenDisabled(t *testing.T) {
	cases := []struct {
		name string
		cfg  *service.AccelConfig
	}{
		{name: "未启用", cfg: &service.AccelConfig{CFEnabled: false, CFPreferDomain: "cdn.example.com"}},
		{name: "未配置优选域名", cfg: &service.AccelConfig{CFEnabled: true, CFPreferDomain: ""}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := accelerateCF(context.Background(), tc.cfg, "shop.example.com", models.Domain{Name: "example.com"})
			if got.Status != "skipped" {
				t.Fatalf("应返回 skipped，实际 status=%q msg=%q", got.Status, got.Msg)
			}
			if got.CNAME != "" {
				t.Errorf("跳过时不应给出 CNAME，实际 %q", got.CNAME)
			}
		})
	}
}

/*
 * TestAccelerateCFLegacyReportedSuccessOnFailure 用旧实现复现缺陷并对照新实现
 *
 * legacyAccelerateCF 逐字保留改动前的逻辑（HEAD 版本 acceleration.go:207-233）：
 * 回落域名不可用时跳过创建、仍返回 Status:"success"。
 * 本用例同时断言「旧逻辑返回 success」与「新逻辑返回 error」，
 * 既证明缺陷真实存在，也锁定修复后的行为不再回退
 */
func TestAccelerateCFLegacyReportedSuccessOnFailure(t *testing.T) {
	setupHandlerTestDB(t)

	cfg := &service.AccelConfig{
		CFEnabled:      true,
		CFPreferDomain: "cdn.example.com",
		CFZoneID:       "zone1",
		CFDomainID:     999, // 不存在的域名，getCFDomainContext 必然失败
	}
	domain := models.Domain{Name: "example.com"}

	legacy := legacyAccelerateCF(context.Background(), cfg, "shop.example.com", domain)
	if legacy.Status != "success" {
		t.Fatalf("旧实现应在此场景误报 success（缺陷复现失败，说明触发条件有误），实际 %q", legacy.Status)
	}
	if legacy.CNAME != "cdn.example.com" {
		t.Fatalf("旧实现应把优选域名当作 CNAME 返回，实际 %q", legacy.CNAME)
	}

	fixed := accelerateCF(context.Background(), cfg, "shop.example.com", domain)
	if fixed.Status != "error" {
		t.Fatalf("新实现应返回 error，实际 status=%q msg=%q", fixed.Status, fixed.Msg)
	}
	if fixed.CNAME != "" {
		t.Errorf("新实现失败时不应给出 CNAME，实际 %q", fixed.CNAME)
	}
}

/* legacyAccelerateCF 改动前的 accelerateCF 实现，仅用于复现缺陷，不得在生产代码中引用 */
func legacyAccelerateCF(ctx context.Context, cfg *service.AccelConfig, fqdn string, domain models.Domain) service.AccelResult {
	if !cfg.CFEnabled || cfg.CFPreferDomain == "" {
		return service.AccelResult{
			Platform: "cloudflare",
			Line:     "AB",
			LineName: "海外",
			Status:   "skipped",
			Msg:      "CF 加速未启用或未配置优选域名",
		}
	}

	if cfg.CFDomainID > 0 && cfg.CFZoneID != "" {
		svc, _, _, err := getCFDomainContext(cfg.CFDomainID)
		if err == nil {
			_, _ = svc.CreateCustomHostname(cfg.CFZoneID, fqdn, "", "http", "1.0")
		}
	}

	return service.AccelResult{
		Platform: "cloudflare",
		Line:     "AB",
		LineName: "海外",
		CNAME:    cfg.CFPreferDomain,
		Status:   "success",
	}
}
