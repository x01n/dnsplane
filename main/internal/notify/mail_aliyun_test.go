package notify

import (
	"net/url"
	"testing"
)

func aliyunTestParams() map[string]string {
	return map[string]string{
		"Action":           "SingleSendMail",
		"AccountName":      "noreply@example.com",
		"ReplyToAddress":   "false",
		"AddressType":      "1",
		"ToAddress":        "a@example.com,b@example.com",
		"FromAlias":        "DNS Plane 通知",
		"Subject":          "测试 * 主题 ~ tilde + plus & amp / slash",
		"HtmlBody":         `<b>hello</b> world & "quote"`,
		"Format":           "JSON",
		"Version":          "2015-11-23",
		"AccessKeyId":      "TESTAK",
		"SignatureMethod":  "HMAC-SHA1",
		"Timestamp":        "2026-10-05T00:00:00Z",
		"SignatureVersion": "1.0",
		"SignatureNonce":   "abcd1234",
	}
}

func TestAliyunRPCSignature(t *testing.T) {
	got := aliyunRPCSignature(aliyunTestParams(), "TESTSECRET", "POST")
	want := "tLsHoIIO1nqsswMI7yhhCzeR5UE="
	if got != want {
		t.Fatalf("签名不一致\n got=%s\nwant=%s", got, want)
	}
}

func TestAliyunPercentEncode(t *testing.T) {
	got := aliyunPercentEncode("测试 * ~ + & /")
	want := "%E6%B5%8B%E8%AF%95%20%2A%20~%20%2B%20%26%20%2F"
	if got != want {
		t.Fatalf("转义不一致\n got=%s\nwant=%s", got, want)
	}
}

func TestAliyunCanonicalQuery(t *testing.T) {
	got := encodeAliyunParams(aliyunTestParams(), true)
	want := "AccessKeyId=TESTAK&AccountName=noreply%40example.com&Action=SingleSendMail&AddressType=1" +
		"&Format=JSON&FromAlias=DNS%20Plane%20%E9%80%9A%E7%9F%A5&HtmlBody=%3Cb%3Ehello%3C%2Fb%3E%20world%20%26%20%22quote%22" +
		"&ReplyToAddress=false&SignatureMethod=HMAC-SHA1&SignatureNonce=abcd1234&SignatureVersion=1.0" +
		"&Subject=%E6%B5%8B%E8%AF%95%20%2A%20%E4%B8%BB%E9%A2%98%20~%20tilde%20%2B%20plus%20%26%20amp%20%2F%20slash" +
		"&Timestamp=2026-10-05T00%3A00%3A00Z&ToAddress=a%40example.com%2Cb%40example.com&Version=2015-11-23"
	if got != want {
		t.Fatalf("规范查询串不一致\n got=%s\nwant=%s", got, want)
	}
}

func TestEncodeAliyunBodyAppendsSignature(t *testing.T) {
	params := aliyunTestParams()
	params["Signature"] = "tLsHoIIO1nqsswMI7yhhCzeR5UE="
	body := encodeAliyunBody(params)
	want := "&Signature=" + url.QueryEscape("tLsHoIIO1nqsswMI7yhhCzeR5UE=")
	if len(body) <= len(want) || body[len(body)-len(want):] != want {
		t.Fatalf("请求体未按预期追加 Signature: %s", body)
	}
	if contains := "Signature=tLsHoIIO1nqsswMI7yhhCzeR5UE%3D"; body[len(body)-len(contains):] != contains {
		t.Fatalf("请求体 Signature 编码异常: %s", body)
	}
}

func TestSplitMailRecipients(t *testing.T) {
	got := splitMailRecipients(" a@example.com , b@example.com ,, c@example.com ")
	want := []string{"a@example.com", "b@example.com", "c@example.com"}
	if len(got) != len(want) {
		t.Fatalf("收件人数量不一致: %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("收件人解析不一致: %v", got)
		}
	}
	if n := len(splitMailRecipients("   ")); n != 0 {
		t.Fatalf("空白收件人应为空列表，实际 %d", n)
	}
}
