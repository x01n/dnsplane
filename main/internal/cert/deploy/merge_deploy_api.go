package deploy

import "main/internal/cert"

func MergeDeployProviderConfigsForAPI(base map[string]cert.ProviderConfig) map[string]cert.ProviderConfig {
	out := make(map[string]cert.ProviderConfig, len(base)+len(AllDeployConfigs))
	for k, v := range base {
		out[k] = v
	}

	// 表单字段以 AllDeployConfigs 为准的类型；不在名单内时保留 cert 注册表的定义
	patchServer := map[string]struct{}{"ssh": {}, "ftp": {}, "local": {}}

	for typ, dcfg := range AllDeployConfigs {
		if exist, ok := out[typ]; ok {
			if _, doPatch := patchServer[typ]; doPatch {
				exist.Config = dcfg.Inputs
				exist.DeployConfig = dcfg.TaskInputs
				if dcfg.TaskNote != "" {
					exist.DeployNote = dcfg.TaskNote
				}
				note := dcfg.Desc
				if dcfg.Note != "" {
					note = dcfg.Note
				}
				if note != "" {
					exist.Note = note
				}
				if dcfg.Name != "" {
					exist.Name = dcfg.Name
				}
				if dcfg.Icon != "" {
					exist.Icon = dcfg.Icon
				}
				out[typ] = exist
			}
			continue
		}

		pc, inCert := cert.GetProviderConfig(typ)
		if inCert && pc.IsDeploy {
			continue
		}
		out[typ] = deployConfigToAPIProviderConfig(dcfg)
	}
	return out
}

func deployConfigToAPIProviderConfig(d DeployProviderConfig) cert.ProviderConfig {
	note := d.Desc
	if d.Note != "" {
		note = d.Note
	}
	return cert.ProviderConfig{
		Type:         d.Type,
		Name:         d.Name,
		Icon:         d.Icon,
		Note:         note,
		IsDeploy:     true,
		Config:       d.Inputs,
		DeployConfig: d.TaskInputs,
		DeployNote:   d.TaskNote,
	}
}
