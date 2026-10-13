package models

import (
	"main/internal/crypto"

	"gorm.io/gorm"
)

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
