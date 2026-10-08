package notify

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

/*
 * 第三方验证码服务端校验（Google reCAPTCHA / hCaptcha）
 *
 * 设置页的验证码类型下拉框提供 image / behavioral / turnstile / recaptcha / hcaptcha，
 * 后两者此前只有前端组件、没有服务端校验：管理员选中后组件照常渲染，
 * 提交的 token 却无人核验，用户以为开启了人机验证而实际零保护。
 *
 * 语义与 Turnstile 完全一致：上游不可达、响应非 JSON、success 不为 true 一律判失败，
 * 绝不降级放行——虚假的安全感比没有开关更危险。
 *
 * 三家厂商的请求参数（secret / response / remoteip）与成功判据（success == true）相同，
 * 因此共用 verifyCaptchaAt 一份实现，各文件只保留端点与厂商名这类真正有差异的配置
 */

/* 两家厂商的站点校验端点，hCaptcha 官方文档指定 api.hcaptcha.com 主机 */
const (
	captchaVerifyURLRecaptcha = "https://www.google.com/recaptcha/api/siteverify"
	captchaVerifyURLHcaptcha  = "https://api.hcaptcha.com/siteverify"
)

/* captchaHTTPTimeout 第三方校验请求超时，超时即判定校验失败 */
const captchaHTTPTimeout = 10 * time.Second

/*
 * captchaResponse 站点校验响应体
 *
 * reCAPTCHA 与 hCaptcha 的成功标志与响应字段一致，无需按厂商分叉结构。
 * 错误码键名：实测两家端点均返回连字符形式 error-codes，
 * 下划线形式 error_codes 出现在 hCaptcha 官方后端示例中，一并接收以防版本差异
 */
type captchaResponse struct {
	Success       bool     `json:"success"`
	ErrorCodes    []string `json:"error-codes"`
	ErrorCodesAlt []string `json:"error_codes"`
}

/*
 * captchaProvider 按 captcha_type 取得厂商校验端点与厂商名
 *
 * turnstile 不在其列：它有独立的 turnstile_secret_key 配置与校验入口，
 * 与登录验证码使用的通用键 captcha_secret_key 是两套并行配置
 *
 * @param captchaType captcha_type 配置值
 * @returns 端点、用于错误文案的厂商名、是否受支持
 */
func captchaProvider(captchaType string) (endpoint, provider string, ok bool) {
	switch strings.ToLower(strings.TrimSpace(captchaType)) {
	case "recaptcha":
		return captchaVerifyURLRecaptcha, "reCAPTCHA", true
	case "hcaptcha":
		return captchaVerifyURLHcaptcha, "hCaptcha", true
	default:
		return "", "", false
	}
}

/*
 * IsThirdPartyCaptchaType 判定 captcha_type 是否属于需要第三方服务端校验的类型
 *
 * @param captchaType captcha_type 配置值
 * @returns 是否为 recaptcha / hcaptcha
 */
func IsThirdPartyCaptchaType(captchaType string) bool {
	_, _, ok := captchaProvider(captchaType)
	return ok
}

/*
 * VerifyCaptcha 按 captcha_type 核验前端组件返回的 token
 *
 * @param ctx         请求上下文，随请求取消可立即中止校验
 * @param captchaType captcha_type 配置值
 * @param secretKey   厂商私有密钥，为空时返回错误（调用方应先判断是否需要校验）
 * @param token       前端组件返回的 token
 * @param remoteIP    用户 IP，可为空，供厂商侧风控关联
 * @returns 校验通过返回 nil，否则返回可直接回显给用户的错误
 */
func VerifyCaptcha(ctx context.Context, captchaType, secretKey, token, remoteIP string) error {
	endpoint, provider, ok := captchaProvider(captchaType)
	if !ok {
		return fmt.Errorf("不支持的第三方验证码类型: %s", strings.TrimSpace(captchaType))
	}
	return verifyCaptchaAt(ctx, endpoint, provider, secretKey, token, remoteIP)
}

/*
 * verifyCaptchaAt 在指定端点核验 token，端点作为参数传入以便测试打桩
 *
 * 三家厂商的协议一致，本函数是唯一的校验实现，Turnstile 亦复用此处
 *
 * @param ctx       请求上下文
 * @param endpoint  站点校验端点
 * @param provider  厂商名，仅用于错误文案
 * @param secretKey 厂商私有密钥
 * @param token     前端组件返回的 token
 * @param remoteIP  用户 IP，可为空
 * @returns 校验通过返回 nil，否则返回可直接回显给用户的错误
 */
func verifyCaptchaAt(ctx context.Context, endpoint, provider, secretKey, token, remoteIP string) error {
	if strings.TrimSpace(secretKey) == "" {
		return fmt.Errorf("未配置 %s 私有密钥", provider)
	}
	if strings.TrimSpace(token) == "" {
		return fmt.Errorf("请先完成人机验证")
	}

	form := url.Values{}
	form.Set("secret", strings.TrimSpace(secretKey))
	form.Set("response", strings.TrimSpace(token))
	if remoteIP != "" {
		form.Set("remoteip", remoteIP)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("人机验证请求构造失败")
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	client := &http.Client{Timeout: captchaHTTPTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("人机验证服务不可达")
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if err != nil {
		return fmt.Errorf("人机验证响应读取失败")
	}

	var result captchaResponse
	if err := json.Unmarshal(raw, &result); err != nil {
		return fmt.Errorf("人机验证响应解析失败")
	}
	if !result.Success {
		codes := result.ErrorCodes
		if len(codes) == 0 {
			codes = result.ErrorCodesAlt
		}
		if len(codes) > 0 {
			return fmt.Errorf("人机验证未通过: %s", strings.Join(codes, ","))
		}
		return fmt.Errorf("人机验证未通过")
	}
	return nil
}
