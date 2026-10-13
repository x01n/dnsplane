package apiv1

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"
)

/*
 * apiv1 测试助手
 *
 * 这些函数此前混在 util.go（**非测试**文件）里，导致 util.go 在生产构建中
 * import 了 "testing" 与 "net/http/httptest"——两个只应出现在测试里的包。
 * 后果有两层：
 *   一、生产二进制里静态链入整个 testing 包
 *   二、`go vet` 的 testinggoroutine 等检查覆盖到了不该覆盖的位置，
 *       且 util.go 一旦被非测试代码误用会连带把 testing 拖进依赖图
 *
 * 集中到本文件后，util.go 只剩三个纯工具函数（itoa / domainPermissionOf /
 * jsonUnmarshal），它们的调用点里非测试的占多数（scope.go 三处、
 * record_sync.go 一处），因此留在 util.go
 */

/* newRecorder httptest.ResponseRecorder 的别名，避免测试文件重复导包 */
func newRecorder() *httptest.ResponseRecorder {
	return httptest.NewRecorder()
}

/* timestampNow 当前 Unix 秒级时间戳的字符串形式 */
func timestampNow() string {
	return strconv.FormatInt(time.Now().Unix(), 10)
}

/* decodeBody 解析响应体到 out，失败即 Fatal 以免用零值静默通过断言 */
func decodeBody(t *testing.T, resp *http.Response, out interface{}) {
	t.Helper()
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		t.Fatalf("解析响应失败: %v", err)
	}
}
