package middleware

import (
	"encoding/json"
	"net/url"
	"strings"
)

// 本文件提供请求日志脱敏工具：RequestTrace/AuditLog 记录 body 与 headers 之前调用。
// 命中关键字的字段值替换为固定掩码，保留字段结构便于排障。

const redactedMark = "***REDACTED***"

/*
 * sensitiveKeys 词干规则够不着的**例外键**
 *
 * 判定分两步：先查这张表，再过 credentialStems 的词干匹配。表里只放
 * 「键名里没有 secret / password / token 这类词、但值确实是凭据或个人信息」的键。
 *
 * 曾有一张 64 键的表，其中 46 键的键名本身就含词干（password、token、
 * secret_key、access_key_secret……），词干规则上线后那 46 行成了冗余定义——
 * 同一事实写在两处，新增键时无从判断该往哪加。删掉后本表即「例外清单」，
 * 逐行都能回答「它为什么在这儿」
 *
 * 同理，匹配不区分大小写、不区分 _/- 间隔
 */
var sensitiveKeys = map[string]struct{}{
	// 密码的别名：pwd 与 passwd 不同词干，必须单列
	"pwd": {},
	// JWT 签名密钥：不含任何词干
	"master_key": {},
	/*
	 * 一次性验证码一族：键名里没有约定俗成的词干词
	 * totp 是算法名本身；verification_code 与 captcha 是各自的叫法
	 */
	"totp":              {},
	"totp_code":         {},
	"verification_code": {},
	"captcha":           {},
	/*
	 * code 只对**请求体**生效
	 *
	 * 请求体里的 code 是一次性验证码（TOTP、注册、邮箱绑定、OAuth 授权码）；
	 * 响应体里的 code 是业务状态码（控制台 0/-1、v1 分段码）。
	 * 两者只是碰巧同名，响应侧由 redactJSON 的 responseSide 放行
	 */
	"code": {},
	// ACME 外部账户绑定的 HMAC 密钥与标识
	"eab_hmac_key": {},
	"eab_kid":      {},
	/*
	 * bark_key：键名里只有裸 key，而词干表刻意不收裸 key
	 *（record_key、site_key 这类非凭据键也含它）。Bark 的 key 是推送凭证，
	 * 拿到即可向该设备发消息，必须掩
	 */
	"bark_key": {},
	/*
	 * 各服务商的短凭证键
	 *
	 * 这些键名里都没有词干词，靠词干规则够不着。它们会随账户配置的保存请求
	 * 进入 body，在请求日志里原样落库——漏掉任何一个，对应服务商的密钥
	 * 就以明文长期留存
	 */
	"app_key":    {},
	"appkey":     {},
	"skey":       {},
	"sp_key":     {},
	"spkey":      {},
	"public_key": {},
	"publickey":  {},
	/*
	 * 邮箱地址：用户资料、通知收件人、白名单里都会出现，
	 * 属个人信息，与密码同级别处理
	 */
	"email": {},
}

/* normalizedSensitiveKeys 是 sensitiveKeys 的归一化副本
 * isSensitiveKey 对每个 JSON key 都会调用一次，若在函数内遍历原表并对每项
 * 重新执行 normalizeKey，单次判断将从 O(1) 退化为 O(表大小) 次字符串分配
 */
var normalizedSensitiveKeys = func() map[string]struct{} {
	m := make(map[string]struct{}, len(sensitiveKeys))
	for k := range sensitiveKeys {
		m[normalizeKey(k)] = struct{}{}
	}
	return m
}()

var sensitiveHeaders = map[string]struct{}{
	"authorization":       {},
	"cookie":              {},
	"set-cookie":          {},
	"x-refresh-token":     {},
	"x-secret-token":      {},
	"x-csrf-token":        {},
	"proxy-authorization": {},
}

/*
 * credentialStems 凭据词干：键名里出现任一词干即判为凭据
 *
 * 此前是「整串等值匹配」加一条只覆盖 access_key / secret_key 的正则，
 * 够不着带前缀的键名——wxwork_secret、tgbot_token、mail_api_key、
 * github_client_secret 等 22 个真实凭据键全部漏过，明文落进请求日志
 *
 * 改按词干子串匹配后，新增服务商或通知渠道时不必再人工同步这张表：
 * 只要键名里带 secret / token 这类词，就自动落进遮蔽范围
 *
 * 词干只收「一旦出现就必属凭据」的那些。像 id、key、url 这类
 * 大量出现在非凭据键名里的词不能收，否则会把日志可读性掩没
 */
var credentialStems = []string{
	"secret",
	"password",
	"passwd",
	"token",
	"privatekey",
	"apikey",
	"accesskey",
	"credential",
}

func normalizeKey(k string) string {
	k = strings.ToLower(k)
	k = strings.ReplaceAll(k, "-", "")
	k = strings.ReplaceAll(k, "_", "")
	return k
}

// isSensitiveKey 判断 JSON key / form 字段是否属于敏感列表。
func isSensitiveKey(k string) bool {
	n := normalizeKey(k)
	if _, ok := normalizedSensitiveKeys[n]; ok {
		return true
	}
	for _, stem := range credentialStems {
		if strings.Contains(n, stem) {
			return true
		}
	}
	return false
}

/*
 * SanitizeBodyForLog 脱敏**请求体**：一次性验证码（TOTP、注册、邮箱绑定）
 * 在请求体里叫 code，必须掩掉
 *
 * 响应体请用 SanitizeResponseForLog：那里的 code 是业务状态码，掩掉会让日志读不出成败
 */
func SanitizeBodyForLog(body string, contentType string) string {
	return sanitizeForLog(body, contentType, false)
}

/* SanitizeResponseForLog 脱敏**响应体**：与请求侧唯一差别是放行 code 键 */
func SanitizeResponseForLog(body string, contentType string) string {
	return sanitizeForLog(body, contentType, true)
}

// sanitizeForLog 请求与响应共用的脱敏流程
// 识别 JSON（对象/数组递归）与 x-www-form-urlencoded；其他类型不修改原文。
func sanitizeForLog(body string, contentType string, responseSide bool) string {
	if body == "" {
		return body
	}
	trimmed := strings.TrimSpace(body)
	ct := strings.ToLower(contentType)

	// JSON 分支：尝试解析为 any，递归脱敏后序列化回写
	if strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") || strings.Contains(ct, "application/json") {
		var v any
		if err := json.Unmarshal([]byte(trimmed), &v); err == nil {
			redactJSON(&v, responseSide)
			if out, err := json.Marshal(v); err == nil {
				return string(out)
			}
		}
	}

	// Form 分支
	if strings.Contains(ct, "application/x-www-form-urlencoded") {
		if vals, err := url.ParseQuery(trimmed); err == nil {
			for k := range vals {
				if responseSide && strings.EqualFold(k, "code") {
					continue
				}
				if isSensitiveKey(k) {
					vals.Set(k, redactedMark)
				}
			}
			return vals.Encode()
		}
	}

	return body
}

/*
 * redactJSON 递归脱敏 map 与 slice 中的敏感字段值
 *
 * responseSide 为真时额外放行一个键：code
 *
 * 一次性验证码（TOTP、注册、邮箱绑定）在请求体里就叫 code，必须掩；
 * 而响应体的业务状态码也叫 code（控制台 0/-1、v1 分段码），
 * 掩掉它会让整条请求日志读不出成败。两者只是碰巧同名，
 * 语义不同，因此按方向区分而不是从敏感表里删掉 code
 */
func redactJSON(v *any, responseSide bool) {
	switch node := (*v).(type) {
	case map[string]any:
		for k, val := range node {
			if isSensitiveKey(k) {
				if responseSide && strings.EqualFold(k, "code") {
					// 响应体的 code 是业务状态码，不是凭据
				} else {
					node[k] = redactedMark
					continue
				}
			}
			redactJSON(&val, responseSide)
			node[k] = val
		}
	case []any:
		for i := range node {
			redactJSON(&node[i], responseSide)
		}
	case string:
		/*
		 * 字符串里若嵌着 JSON，也要递归脱敏
		 *
		 * 面板把服务商凭证以一个 JSON **字符串**存进 config 列
		 * （如 cert_accounts.config），提交时整个 config 是 body 里的一个字符串值。
		 * 只看键名的话，这一层的外层键是 "config"——不在敏感表里，
		 * 其内部的 SecretKey 就原样落进请求日志。
		 *
		 * 先看首字符再解析：绝大多数普通文本不是 JSON，
		 * 对它们做一次 Unmarshal 是白费
		 */
		s := node
		trimmed := strings.TrimSpace(s)
		if trimmed == "" || (trimmed[0] != '{' && trimmed[0] != '[') {
			return
		}
		var inner any
		if err := json.Unmarshal([]byte(trimmed), &inner); err != nil {
			return
		}
		redactJSON(&inner, responseSide)
		// 只在确实发生了脱敏时才回写，避免把格式（缩进、键序）改掉
		if out, err := json.Marshal(inner); err == nil && strings.Contains(string(out), redactedMark) {
			*v = string(out)
		}
	}
}

// SanitizeHeadersForLog 将敏感 header 值替换为掩码后返回 JSON 字符串。
// 输入为 map[string]string，保留 key 与大小写以便排障。
func SanitizeHeadersForLog(headers map[string]string) string {
	if len(headers) == 0 {
		return "{}"
	}
	out := make(map[string]string, len(headers))
	for k, v := range headers {
		if _, ok := sensitiveHeaders[strings.ToLower(k)]; ok {
			out[k] = redactedMark
			continue
		}
		out[k] = v
	}
	b, _ := json.Marshal(out)
	return string(b)
}
