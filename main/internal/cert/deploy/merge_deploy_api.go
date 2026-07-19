package deploy

import "main/internal/cert"

/*
 * MergeDeployProviderConfigsForAPI 合并 cert 注册表中的部署类型与 AllDeployConfigs（dnsmgr 同源），
 * 供 /api/cert/providers 使用：不修改进程内 cert 注册表，避免覆盖 DNS 验证等同名类型（如 huoshan、ucloud）的工厂函数。
 *
 * - 已在 cert 中带 IsDeploy 的条目保留，并对 ssh/ftp/local 用 AllDeployConfigs 覆盖任务表单字段。
 * - 仅存在于 AllDeployConfigs、或 cert 中仅有非部署定义（IsDeploy=false）的类型，补充为独立 deploy 视图。
 */
func MergeDeployProviderConfigsForAPI(base map[string]cert.ProviderConfig) map[string]cert.ProviderConfig {
	out := make(map[string]cert.ProviderConfig, len(base)+len(AllDeployConfigs))
	for k, v := range base {
		out[k] = v
	}

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
