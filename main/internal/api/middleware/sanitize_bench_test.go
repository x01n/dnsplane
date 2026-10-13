package middleware

import (
	"encoding/json"
	"testing"
)

/* BenchmarkIsSensitiveKey 覆盖脱敏热路径
 * 该函数对请求/响应 JSON 的每一个 key 调用一次，
 * 因此 O(表大小) 的线性扫描会随字段数放大
 */
func BenchmarkIsSensitiveKey(b *testing.B) {
	keys := []string{"RecordId", "Name", "Type", "Value", "Line", "TTL", "Status", "Remark", "code", "data"}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, k := range keys {
			_ = isSensitiveKey(k)
		}
	}
}

/* BenchmarkSanitizeBodyForLog 覆盖完整的 body 脱敏流程
 * 样本为 50 条记录的标准列表响应，接近真实请求日志的落库内容
 */
func BenchmarkSanitizeBodyForLog(b *testing.B) {
	data := make([]map[string]interface{}, 50)
	for i := range data {
		data[i] = map[string]interface{}{
			"RecordId": float64(i),
			"Name":     "www",
			"Type":     "A",
			"Value":    "1.2.3.4",
			"Line":     "默认",
			"TTL":      float64(600),
			"Status":   "ENABLE",
			"Remark":   "",
		}
	}
	raw, _ := json.Marshal(map[string]interface{}{"code": 0, "data": data})
	body := string(raw)

	b.ReportAllocs()
	b.SetBytes(int64(len(body)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = SanitizeBodyForLog(body, "application/json")
	}
}
