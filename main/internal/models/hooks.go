package models

import (
	"main/internal/crypto"

	"gorm.io/gorm"
)

// 本文件为敏感字段添加 GORM BeforeSave / AfterFind 钩子，透明加解密。
// 只针对真正保存凭据或私钥的列；普通业务字段保持原状。
//
// 涉及字段：
//   Account.Config         DNS 厂商 AK/SK（JSON）
//   CertAccount.Config     证书厂商凭据（JSON）
//   CertAccount.Ext        EAB 等扩展凭据
//   CertOrder.PrivateKey   证书私钥 PEM
//   CertOrder.Info         ACME 流程中间态（含签名材料）
//   CertDeploy.Config      部署目标凭据（SSH / 宝塔等）
//   CertDeploy.Info        部署器运行态
//   DMTask.ProxyPassword   容灾探测代理密码
//   User.TOTPSecret        TOTP 共享密钥
//   UserOAuth.AccessToken  三方 OAuth access token
//   UserOAuth.RefreshToken 三方 OAuth refresh token
//
// 读路径：AfterFind 将密文解回明文供业务层使用，明文列直接返回（兼容历史数据）。
// 写路径：BeforeSave 将明文加密为 enc:v1:...；已是密文时幂等返回。

func encFields(fields ...*string) error {
	for _, f := range fields {
		if f == nil {
			continue
		}
		out, err := crypto.Encrypt(*f)
		if err != nil {
			return err
		}
		*f = out
	}
	return nil
}

func decFields(fields ...*string) {
	for _, f := range fields {
		if f == nil {
			continue
		}
		*f = crypto.MustDecrypt(*f)
	}
}

func (a *Account) BeforeSave(*gorm.DB) error { return encFields(&a.Config) }
func (a *Account) AfterFind(*gorm.DB) error  { decFields(&a.Config); return nil }

func (c *CertAccount) BeforeSave(*gorm.DB) error { return encFields(&c.Config, &c.Ext) }
func (c *CertAccount) AfterFind(*gorm.DB) error  { decFields(&c.Config, &c.Ext); return nil }

func (o *CertOrder) BeforeSave(*gorm.DB) error {
	return encFields(&o.PrivateKey, &o.Info)
}
func (o *CertOrder) AfterFind(*gorm.DB) error {
	decFields(&o.PrivateKey, &o.Info)
	return nil
}

func (d *CertDeploy) BeforeSave(*gorm.DB) error {
	return encFields(&d.Config, &d.Info)
}
func (d *CertDeploy) AfterFind(*gorm.DB) error {
	decFields(&d.Config, &d.Info)
	return nil
}

func (t *DMTask) BeforeSave(*gorm.DB) error { return encFields(&t.ProxyPassword) }
func (t *DMTask) AfterFind(*gorm.DB) error  { decFields(&t.ProxyPassword); return nil }

// User.ResetToken 不走加密钩子：使用 sha256(token) 落库（见 handler/auth.go hashResetToken）；
// 原因是 GORM Updates(map) 不触发 BeforeSave 钩子，曾导致明文落库（安全审计 H-6）。
func (u *User) BeforeSave(*gorm.DB) error {
	return encFields(&u.TOTPSecret)
}
func (u *User) AfterFind(*gorm.DB) error {
	decFields(&u.TOTPSecret)
	return nil
}

func (o *UserOAuth) BeforeSave(*gorm.DB) error {
	return encFields(&o.AccessToken, &o.RefreshToken)
}
func (o *UserOAuth) AfterFind(*gorm.DB) error {
	decFields(&o.AccessToken, &o.RefreshToken)
	return nil
}
