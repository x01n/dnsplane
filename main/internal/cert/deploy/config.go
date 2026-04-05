package deploy

import "main/internal/cert"

/* DeployClass 部署器分类 */
const (
	ClassSelfHosted   = 1 // 自建系统
	ClassCloudService = 2 // 云服务商
	ClassServer       = 3 // 服务器
)

/* ClassNames 分类名称 */
var ClassNames = map[int]string{
	ClassSelfHosted:   "自建系统",
	ClassCloudService: "云服务商",
	ClassServer:       "服务器",
}

/* DeployProviderConfig 部署提供商配置 */
type DeployProviderConfig struct {
	Type       string             `json:"type"`
	Name       string             `json:"name"`
	Class      int                `json:"class"`
	Icon       string             `json:"icon"`
	Desc       string             `json:"desc"`
	Note       string             `json:"note"`
	Inputs     []cert.ConfigField `json:"inputs"`
	TaskInputs []cert.ConfigField `json:"task_inputs"`
	TaskNote   string             `json:"task_note"`
}

/* AllDeployConfigs 所有部署器配置 */
var AllDeployConfigs = make(map[string]DeployProviderConfig)

/* GetAllDeployConfigs 获取所有部署器配置 */
func GetAllDeployConfigs() map[string]DeployProviderConfig {
	return AllDeployConfigs
}

/* GetDeployConfig 获取指定部署器配置 */
func GetDeployConfig(deployType string) (DeployProviderConfig, bool) {
	cfg, ok := AllDeployConfigs[deployType]
	return cfg, ok
}

/* registerDeployConfig 注册部署器配置 */
func registerDeployConfig(cfg DeployProviderConfig) {
	AllDeployConfigs[cfg.Type] = cfg
}
