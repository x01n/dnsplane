package notify

import (
	"context"
	"errors"
	"strings"
	"testing"
)

/* errTestNotify 测试用的固定错误 */
var errTestNotify = errors.New("测试用发送失败")

/*
 * 邮件正文内容适配测试（方案 D）
 * 覆盖缺口：NotifyManager 的 content 契约为纯文本（+ \n），而邮件后端按 HTML 投递
 * （SMTP Content-Type: text/html、Sendcloud 的 html 字段、DirectMail 的 HtmlBody），
 * 纯文本的 \n 会塌成一行，需转义为 <br/> 才能换行。
 * 判定依据为实测：templates.go 的 6 个模板邮件均以 <!DOCTYPE html> 起始，
 * 且走 auth.go / dashboard.go 的独立构造点，不经过本适配器。
 */

/* TestEmailHTMLContentPlainText 纯文本正文应转义特殊字符并把换行转为 <br/> */
func TestEmailHTMLContentPlainText(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"换行转 br", "证书部署成功\n域名: a.com\n时间: 2026-10-05", "证书部署成功<br/>域名: a.com<br/>时间: 2026-10-05"},
		{"无换行原样", "单行通知", "单行通知"},
		{"空串", "", ""},
		{"尖括号转义", "错误: timeout < 5s", "错误: timeout &lt; 5s"},
		{"大于号转义", "count > 3", "count &gt; 3"},
		{"与号转义", "a & b", "a &amp; b"},
		{"CRLF 归一后转义", "行一\r\n行二", "行一<br/>行二"},
		{"孤立 CR 归一后转义", "行一\r行二", "行一<br/>行二"},
		{"混合：转义与换行", "域名 <a.com>\n错误: <nil>", "域名 &lt;a.com&gt;<br/>错误: &lt;nil&gt;"},
		{"转义顺序正确（& 先于其它）", "启动 & 停止 <x>", "启动 &amp; 停止 &lt;x&gt;"},
		{"多连续换行", "a\n\n\nb", "a<br/><br/><br/>b"},
	}
	for _, c := range cases {
		if got := emailHTMLContent(c.in); got != c.want {
			t.Errorf("%s: in=%q\n  got =%q\n  want=%q", c.name, c.in, got, c.want)
		}
	}
}

/* TestEmailHTMLContentKeepsHTML 已是 HTML 的内容必须原样透传，绝不替换换行 */
func TestEmailHTMLContentKeepsHTML(t *testing.T) {
	// 取自 templates.go 的真实模板邮件形态：含 <style> 块与大量排版换行
	const tpl = `<!DOCTYPE html>
<html>
<head>
    <style>
        body { font-family: sans-serif; }
        .container { max-width: 600px; }
    </style>
</head>
<body>
    <div class="container">您好</div>
</body>
</html>`

	got := emailHTMLContent(tpl)
	if got != tpl {
		t.Errorf("HTML 模板被改动\n  got =%q\n  want=%q", got, tpl)
	}
	// 关键断言：换行不得被替换为 <br/>，否则 <style> 块内会被插入标签导致错版
	if strings.Count(got, "<br/>") != 0 {
		t.Errorf("HTML 模板不得被插入 <br/>，实际 %d 处", strings.Count(got, "<br/>"))
	}
	// 实测方案 A 会在 <style> 内插入 12 个 <br/>
	styleStart := strings.Index(got, "<style>")
	styleEnd := strings.Index(got, "</style>")
	if styleStart < 0 || styleEnd <= styleStart {
		t.Fatalf("模板缺少 <style> 块，测试用例失效")
	}
	if strings.Contains(got[styleStart:styleEnd], "<br/>") {
		t.Errorf("<style> 块内被插入了 <br/>")
	}
}

/* TestEmailHTMLContentHTMLLike 以 < 开头的非完整 HTML 同样按 HTML 处理（宽松判定） */
func TestEmailHTMLContentHTMLLike(t *testing.T) {
	cases := []string{
		"<b>粗体</b>\n下一行",
		"  <font color=\"warning\">警告</font>",
		"<br/>",
	}
	for _, in := range cases {
		if got := emailHTMLContent(in); got != in {
			t.Errorf("以 < 开头应原样透传: in=%q got=%q", in, got)
		}
	}
}

/* TestMailContentAdapterWraps 包装后纯文本被转换、HTML 被保留 */
func TestMailContentAdapterWraps(t *testing.T) {
	rec := &recordingNotifier{}
	adapter := &mailContentAdapter{inner: rec}

	if err := adapter.Send(context.Background(), "标题", "第一行\n第二行"); err != nil {
		t.Fatalf("发送失败: %v", err)
	}
	if rec.content != "第一行<br/>第二行" {
		t.Errorf("纯文本未经转换: %q", rec.content)
	}

	const tpl = "<!DOCTYPE html>\n<html>\n<body>x</body>\n</html>"
	if err := adapter.Send(context.Background(), "标题", tpl); err != nil {
		t.Fatalf("发送失败: %v", err)
	}
	if rec.content != tpl {
		t.Errorf("HTML 被改写: %q", rec.content)
	}

	if rec.title != "标题" {
		t.Errorf("标题应原样透传: %q", rec.title)
	}
}

/* TestMailContentAdapterPropagatesError 内层错误必须原样向上传播 */
func TestMailContentAdapterPropagatesError(t *testing.T) {
	adapter := &mailContentAdapter{inner: &failingNotifier{}}
	if err := adapter.Send(context.Background(), "t", "c"); err == nil {
		t.Error("内层错误未传播")
	}
}

/*
 * TestNewMailContentAdapterExported 导出的包装构造函数供域外构造点使用
 * （如 internal/monitor 自建的 NotifyManager，其容灾告警同为纯文本 + \n）
 */
func TestNewMailContentAdapterExported(t *testing.T) {
	rec := &recordingNotifier{}
	// 返回值必须是 Notifier 接口，且包装后行为与内部适配器一致
	var n Notifier = NewMailContentAdapter(rec)
	if err := n.Send(context.Background(), "标题", "第一行\n第二行"); err != nil {
		t.Fatalf("发送失败: %v", err)
	}
	if rec.content != "第一行<br/>第二行" {
		t.Errorf("纯文本未经转换: %q", rec.content)
	}

	const tpl = "<!DOCTYPE html>\n<html>\n<body>x</body>\n</html>"
	if err := n.Send(context.Background(), "标题", tpl); err != nil {
		t.Fatalf("发送失败: %v", err)
	}
	if rec.content != tpl {
		t.Errorf("HTML 被改写: %q", rec.content)
	}
}

/*
 * TestEmailHTMLContentNotIdempotent 记录一个必须知晓的约束：适配器只应包一层
 * 二次应用会把首次生成的 <br/> 再转义成 &lt;br/&gt;，故禁止嵌套包装
 */
func TestEmailHTMLContentNotIdempotent(t *testing.T) {
	once := emailHTMLContent("第一行\n第二行")
	if once != "第一行<br/>第二行" {
		t.Fatalf("首次转换结果不符: %q", once)
	}
	twice := emailHTMLContent(once)
	if twice == once {
		t.Log("转换已幂等（实现若变更需同步移除本注释与测试）")
		return
	}
	if twice != "第一行&lt;br/&gt;第二行" {
		t.Errorf("二次转换的既有行为已变化: %q", twice)
	}
}

/* recordingNotifier 记录收到的 title/content */
type recordingNotifier struct {
	title, content string
}

func (r *recordingNotifier) Send(_ context.Context, title, content string) error {
	r.title, r.content = title, content
	return nil
}

/* failingNotifier 恒定返回错误 */
type failingNotifier struct{}

func (f *failingNotifier) Send(_ context.Context, _, _ string) error {
	return errTestNotify
}
