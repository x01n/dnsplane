package notify

import "testing"

var stripTagsGoldenCases = []struct{ in, out string }{
	{`尊敬的用户，您好：<br/>您的域名 <b>example.com</b> 的记录发生了异常`, `尊敬的用户，您好：您的域名 example.com 的记录发生了异常`},
	{`尊敬的用户，您好：<br/>您的域名 <b>a.com</b><br/>异常信息：<font color="warning">timeout</font>`, `尊敬的用户，您好：您的域名 a.com异常信息：timeout`},
	{`<br/><font color="grey">聚合DNS管理系统</font><br/><font color="grey">2026-10-05 12:00:00</font>`, `聚合DNS管理系统2026-10-05 12:00:00`},
	{`您的SSL证书已签发成功！<br/><b>证书账户：</b> Let's Encrypt(1)<br/><b>证书域名：</b> a.com、b.com`, `您的SSL证书已签发成功！证书账户： Let's Encrypt(1)证书域名： a.com、b.com`},
	{`<br/><b>失败原因：</b> <font color="warning">DNS 验证超时</font>`, `失败原因： DNS 验证超时`},
	{`纯文本无标签`, `纯文本无标签`},
	{``, ``},
	{`a < b 且 c > d`, `a < b 且 c > d`},
	{`5 < 10 > 3`, `5 < 10 > 3`},
	{`<b>粗体</b>与<i>斜体</i>与<u>下划线</u>`, `粗体与斜体与下划线`},
	{`<a href="https://example.com/x?y=1&z=2">链接</a>`, `链接`},
	{`<a href='https://example.com'>单引号属性</a>`, `单引号属性`},
	{`<img src="x.png" alt="图" />`, ``},
	{`<div class="a"><span>嵌套</span></div>`, `嵌套`},
	{`<p>段落一</p><p>段落二</p>`, `段落一段落二`},
	{`&lt;已被转义的尖括号&gt;`, `&lt;已被转义的尖括号&gt;`},
	{`&amp; 符号`, `&amp; 符号`},
	{`<font color="warning">警告</font>与<font color="grey">灰色</font>`, `警告与灰色`},
	{`换行<br/>换行<br/>换行`, `换行换行换行`},
	{`<strong>强</strong><em>强调</em>`, `强强调`},
	{`<table><tr><td>单元</td></tr></table>`, `单元`},
	{`<h1>标题</h1><h2>小标题</h2>`, `标题小标题`},
	{`<ul><li>项一</li><li>项二</li></ul>`, `项一项二`},
	{`<br/>`, ``},
	{`<b></b>`, ``},
	{`<font color="red">红</font>`, `红`},
	{`前缀 <b>中缀</b> 后缀`, `前缀 中缀 后缀`},
	{`<b>多行<br/>内容</b>`, `多行内容`},
	{`数值比较 3<5 与 7>2 混排`, `数值比较 32 混排`},
	{`<code>a&lt;b</code>`, `a&lt;b`},
	{`邮箱 user@example.com 与链接 <https://example.com>`, `邮箱 user@example.com 与链接 `},
}

func TestStripTagsGolden(t *testing.T) {
	for i, c := range stripTagsGoldenCases {
		if got := stripTags(c.in); got != c.out {
			t.Errorf("#%d in=%q\n  got  =%q\n  want =%q", i, c.in, got, c.out)
		}
	}
}

/* TestStripTagsRules 覆盖状态机各分支：非标签起始、深度计数、引号态、未闭合标签 */
func TestStripTagsRules(t *testing.T) {
	cases := []struct{ name, in, out string }{
		{"小于号后空格", `a< b`, `a< b`},
		{"小于号后制表符", "a<\tb", "a<\tb"},
		{"行尾孤立小于号", `a<`, `a`},
		{"裸尖括号比较", `x < 5 and y > 3`, `x < 5 and y > 3`},
		{"嵌套小于号", `<a<b>>c`, `c`},
		{"未闭合嵌套", `<a<b>c`, ``},
		{"多余大于号", `<a>>c`, `>c`},
		{"双引号内含大于号", `<a href="x>y">t</a>`, `t`},
		{"单引号内含大于号", `<a href='x>y'>t</a>`, `t`},
		{"双引号内含小于号", `<a "<b>">t`, `t`},
		{"引号未闭合则整个丢弃", `<a href="unclosed>t`, ``},
		{"引号跨过标签结束", `<a b"c>t`, ``},
		{"末尾未闭合标签", `text<b>bold`, `textbold`},
		{"末尾未闭合短标签", `x<y`, `x`},
		{"闭合标签后跟正文", `x</a>`, `x`},
		{"空尖括号对", `<>t`, `t`},
		{"双尖括号", `<<b>>t`, `t`},
	}
	for _, c := range cases {
		if got := stripTags(c.in); got != c.out {
			t.Errorf("%s: in=%q got=%q 期望 %q", c.name, c.in, got, c.out)
		}
	}
}
