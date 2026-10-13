package apiv1

import "net/http"

/*
 * V1 对外 API 的错误码
 *
 * 分段规则（与 HTTP 状态码相互独立，客户端应以本码为准）：
 *   1xxx  认证与凭据
 *   2xxx  请求参数
 *   3xxx  权限与范围
 *   4xxx  资源与状态
 *   5xxx  业务与外部依赖
 *   9xxx  服务端内部错误
 *
 * 码值一经发布不得复用或改义：外部调用方据此分支，改义会让存量集成静默走错分支
 */
const (
	// 1xxx 认证与凭据
	CodeUnauthorized     = 1001 // 缺少 API 凭据
	CodeTimestampInvalid = 1002 // 时间戳非法或超出 ±5 分钟窗口
	CodeSignMismatch     = 1003 // 签名验证失败
	CodeAPIKeyMissing    = 1004 // 用户未启用 API 访问
	CodeAccountDisabled  = 1005 // 账户已被禁用
	CodeUserNotFound     = 1006 // 凭据指向的用户不存在

	// 2xxx 请求参数
	CodeParamInvalid    = 2001 // 参数格式或取值非法
	CodeParamMissing    = 2002 // 必填参数缺失
	CodeBodyUnreadable  = 2003 // 请求体无法解析
	CodePageOutOfRange  = 2004 // 分页参数超出允许范围
	CodeUnsupportedType = 2005 // 参数取值不在支持集合内

	// 3xxx 权限与范围
	CodeModuleDenied    = 3001 // 无该功能模块的访问权限
	CodeDomainDenied    = 3002 // 无该域名的操作权限
	CodeSubDomainDenied = 3003 // 无该子域名的操作权限
	CodeReadOnly        = 3004 // 该域名仅授予只读权限
	CodeAdminRequired   = 3005 // 该接口仅管理员可用
	CodeOwnerRequired   = 3006 // 仅资源所有者可操作

	// 4xxx 资源与状态
	CodeResourceNotFound  = 4001 // 目标资源不存在
	CodeDomainNotFound    = 4002 // 域名不存在
	CodeAccountNotFound   = 4003 // DNS 账户不存在
	CodeRecordNotFound    = 4004 // 解析记录不存在
	CodeCertOrderNotFound = 4005 // 证书订单不存在
	CodeTaskNotFound      = 4006 // 任务不存在
	CodeResourceConflict  = 4007 // 资源冲突（如域名已存在）
	CodeResourceLocked    = 4008 // 资源被锁定，暂不可变更

	// 5xxx 业务与外部依赖
	CodeProviderError       = 5001 // 上游 DNS 服务商返回错误
	CodeAccountConfigBad    = 5002 // 账户配置缺失或无法解析
	CodeProviderUnsupported = 5003 // 当前服务商不支持该能力
	CodeDNSWriteFailed      = 5004 // 云端记录写入失败
	CodeCertIssueFailed     = 5005 // 证书签发或部署失败
	CodeUpstreamTimeout     = 5006 // 上游服务超时

	// 9xxx 服务端内部错误
	CodeInternalError  = 9001 // 未归类的内部错误
	CodeNotImplemented = 9002 // 该能力尚未提供
)

/*
 * httpStatusFor 给出错误码对应的 HTTP 状态码
 *
 * 客户端可能只看 HTTP 状态（如网关、重试中间件），故两者都必须是准确的：
 * 4xx 表示请求方需修改，5xx 表示可重试或服务方问题
 */
func httpStatusFor(code int) int {
	switch {
	case code == CodeInternalError || code == CodeNotImplemented:
		return http.StatusInternalServerError
	case code == CodeUpstreamTimeout:
		return http.StatusGatewayTimeout
	case code >= 5000 && code < 6000:
		return http.StatusBadGateway
	case code == CodeResourceNotFound || code == CodeDomainNotFound ||
		code == CodeAccountNotFound || code == CodeRecordNotFound ||
		code == CodeCertOrderNotFound || code == CodeTaskNotFound:
		return http.StatusNotFound
	case code == CodeResourceConflict || code == CodeResourceLocked:
		return http.StatusConflict
	case code == CodeUnauthorized || code == CodeTimestampInvalid ||
		code == CodeSignMismatch || code == CodeAPIKeyMissing ||
		code == CodeAccountDisabled || code == CodeUserNotFound:
		return http.StatusUnauthorized
	default:
		return http.StatusBadRequest
	}
}

/* messageFor 错误码的默认中文说明，调用方未提供定制文案时使用 */
func messageFor(code int) string {
	if m, ok := codeMessages[code]; ok {
		return m
	}
	return "请求处理失败"
}

var codeMessages = map[int]string{
	CodeUnauthorized:        "缺少 API 凭据",
	CodeTimestampInvalid:    "请求时间戳已失效，请校准本机时间后重试",
	CodeSignMismatch:        "签名验证失败",
	CodeAPIKeyMissing:       "该用户未启用 API 访问",
	CodeAccountDisabled:     "账户已被禁用",
	CodeUserNotFound:        "API 凭据对应的用户不存在",
	CodeParamInvalid:        "参数不合法",
	CodeParamMissing:        "缺少必填参数",
	CodeBodyUnreadable:      "请求体解析失败",
	CodePageOutOfRange:      "分页参数超出允许范围",
	CodeUnsupportedType:     "参数取值不在支持范围内",
	CodeModuleDenied:        "无该功能模块的访问权限",
	CodeDomainDenied:        "无该域名的操作权限",
	CodeSubDomainDenied:     "无该子域名的操作权限",
	CodeReadOnly:            "该域名仅有只读权限",
	CodeAdminRequired:       "该接口仅管理员可用",
	CodeOwnerRequired:       "仅资源所有者可执行该操作",
	CodeResourceNotFound:    "目标资源不存在",
	CodeDomainNotFound:      "域名不存在",
	CodeAccountNotFound:     "DNS 账户不存在",
	CodeRecordNotFound:      "解析记录不存在",
	CodeCertOrderNotFound:   "证书订单不存在",
	CodeTaskNotFound:        "任务不存在",
	CodeResourceConflict:    "资源已存在，无法重复创建",
	CodeResourceLocked:      "资源正在处理中，请稍后重试",
	CodeProviderError:       "DNS 服务商返回错误",
	CodeAccountConfigBad:    "账户配置缺失或无法解析",
	CodeProviderUnsupported: "当前 DNS 服务商不支持该能力",
	CodeDNSWriteFailed:      "云端记录写入失败",
	CodeCertIssueFailed:     "证书签发或部署失败",
	CodeUpstreamTimeout:     "上游服务响应超时",
	CodeInternalError:       "服务端内部错误",
	CodeNotImplemented:      "该能力尚未提供",
}
