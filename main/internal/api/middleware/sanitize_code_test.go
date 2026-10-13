package middleware

import (
	"strings"
	"testing"
)

/*
 * 请求体与响应体的 code 语义不同，脱敏口径也不同
 *
 * 一次性验证码（TOTP、注册、邮箱绑定）在请求体里就叫 code，必须掩；
 * 而响应体的业务状态码也叫 code——控制台是 0/-1，v1 是分段码。
 * 此前两侧共用一个函数，响应体的 code 被一并掩成 ***REDACTED***，
 * 整条请求日志于是读不出每次调用的成败
 */

/* TestSanitizeRequestRedactsVerificationCode 请求体里的 code 仍被掩 */
func TestSanitizeRequestRedactsVerificationCode(t *testing.T) {
	cases := []string{
		`{"code":"123456"}`,
		`{"email":"a@b.com","code":"654321"}`,
		`{"code":"authz_code_xyz","state":"s"}`,
		`{"totp_code":"111222","code":"333444"}`,
	}
	for _, in := range cases {
		out := SanitizeBodyForLog(in, "application/json")
		if !strings.Contains(out, redactedMark) {
			t.Errorf("请求体的 code 未被脱敏：%s -> %s", in, out)
		}
	}
}

/* TestSanitizeResponseKeepsBusinessCode 响应体的业务状态码必须可读 */
func TestSanitizeResponseKeepsBusinessCode(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{"控制台成功", `{"code":0,"msg":"更新成功"}`, `"code":0`},
		{"控制台失败", `{"code":-1,"msg":"用户不存在"}`, `"code":-1`},
		{"v1 成功", `{"code":0,"request_id":"abc","data":{"total":5}}`, `"code":0`},
		{"v1 分段失败码", `{"code":4001,"request_id":"abc","message":"资源不存在"}`, `"code":4001`},
	}
	for _, c := range cases {
		out := SanitizeResponseForLog(c.in, "application/json")
		if !strings.Contains(out, c.want) {
			t.Errorf("%s：响应体的 code 被掩掉了，日志读不出成败：%s -> %s", c.name, c.in, out)
		}
	}
}

/* TestSanitizeResponseStillRedactsCredentials 响应侧放行 code 不代表整体不脱敏 */
func TestSanitizeResponseStillRedactsCredentials(t *testing.T) {
	in := `{"code":0,"data":{"token":"abc","refresh_token":"def","secret_key":"ghi","password":"p"}}`
	out := SanitizeResponseForLog(in, "application/json")

	if !strings.Contains(out, `"code":0`) {
		t.Errorf("响应体的 code 应放行：%s", out)
	}
	for _, leaked := range []string{"abc", "def", "ghi", `"p"`} {
		if strings.Contains(out, leaked) {
			t.Errorf("响应体里的凭据 %s 未被脱敏：%s", leaked, out)
		}
	}
}

/* TestSanitizeResponseKeepsNestedBusinessCode 嵌套层级的 code 同样是状态码 */
func TestSanitizeResponseKeepsNestedBusinessCode(t *testing.T) {
	in := `{"code":0,"data":{"list":[{"code":1,"name":"a"},{"code":2,"name":"b"}]}}`
	out := SanitizeResponseForLog(in, "application/json")

	if strings.Contains(out, redactedMark) {
		t.Errorf("响应侧的所有 code 都应放行：%s", out)
	}
}

/*
 * TestSanitizeResponseFormKeepsCode 表单形态的响应侧同样放行 code
 *
 * 表单分支不递归，只有一层键名可比，但口径必须与 JSON 分支一致——
 * 否则同一次调用的成败取决于上游返回的是 JSON 还是表单
 */
func TestSanitizeResponseFormKeepsCode(t *testing.T) {
	in := "code=0&token=secret-value"
	out := SanitizeResponseForLog(in, "application/x-www-form-urlencoded")

	if !strings.Contains(out, "code=0") {
		t.Errorf("表单形态响应体的 code 应放行：%s", out)
	}
	if strings.Contains(out, "secret-value") {
		t.Errorf("表单里的 token 仍须脱敏：%s", out)
	}
}

/* TestSanitizeRequestFormRedactsCode 表单形态的请求侧仍掩 code */
func TestSanitizeRequestFormRedactsCode(t *testing.T) {
	out := SanitizeBodyForLog("email=a%40b.com&code=123456", "application/x-www-form-urlencoded")

	if strings.Contains(out, "123456") {
		t.Errorf("表单形态请求体的验证码未被脱敏：%s", out)
	}
}

/* TestSanitizeResponseCodeCaseInsensitive code 的大小写变体同样放行 */
func TestSanitizeResponseCodeCaseInsensitive(t *testing.T) {
	for _, in := range []string{`{"Code":4001}`, `{"CODE":4001}`} {
		out := SanitizeResponseForLog(in, "application/json")
		if strings.Contains(out, redactedMark) {
			t.Errorf("响应侧 code 的大小写变体应放行：%s -> %s", in, out)
		}
	}
}
