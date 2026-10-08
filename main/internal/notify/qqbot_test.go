package notify

import (
	"crypto/ed25519"
	"encoding/hex"
	"strings"
	"testing"
)

type qqBotSignatureRef struct {
	Secret       string
	Timestamp    string
	PlainToken   string
	SignatureHex string
	PubkeyHex    string
}

var qqBotSignatureRefs = []qqBotSignatureRef{
	{
		Secret:       "abcdef",
		Timestamp:    "1728000000",
		PlainToken:   "Arq0ABCDEFGHIJKLMNOPQRSTUVWXYZ",
		SignatureHex: "2b22ef07f0bb3a7fe55b1ca73fb3ed36ae42845e544ad68a6c02580e38a838771085972902fe276745fcfeab5b44fd6623249bf84eec4b9193e677a8a1b7c806",
		PubkeyHex:    "8af3c616a314fc8a666d14a97e50db587c1e4e5ece6e898856b6e47dfd7a41ac",
	},
	{
		Secret:       "0123456789abcdef0123456789abcdef",
		Timestamp:    "1728000001",
		PlainToken:   "token-2",
		SignatureHex: "03140f9d200aa7da60565a82df62bc9fb1391b48447d70566e2eaae749d6454cf269ea19ffe1df431314be147a10b95c968f8feee62d625bea3824d0d5c85205",
		PubkeyHex:    "23bc54912c1e6e92c4a86825c867e27ffdc555bffbd4244f17a26abfffee965d",
	},
	{
		Secret:       "x",
		Timestamp:    "1",
		PlainToken:   "y",
		SignatureHex: "59bca8ae77a27d1c451581cb29b851903b7836bac2dc16769df12060f26bc7e491bf81e0ee07613548c3e0040ddf39f0aed750fe5210b1d8c6b8900b870f7f0d",
		PubkeyHex:    "ee31f83c88a71219a6fcf9bee0da9bc22620588f5a15a6145553504df9649e5c",
	},
	{
		Secret:       "a-very-long-secret-value-that-exceeds-thirty-two-bytes-in-length",
		Timestamp:    "99",
		PlainToken:   "tok",
		SignatureHex: "d564570abf19e11f880dc0edc7f855f2efca0d786d1d1b5e037f2fa5d656bb114806a85c9f22f2e0d5eab1e14c50733dc6293e1c7fb3d7b5193a4e305deaa806",
		PubkeyHex:    "335cf33155271b968b2122322296d4df4708aec42a233bddef9f35f47a3cbc81",
	},
}

func TestQQBotSecretToSeed(t *testing.T) {
	cases := []struct{ secret, want string }{
		{"x", "xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"},
		{"abcdef", "abcdefabcdefabcdefabcdefabcdefab"},
		{"0123456789abcdef0123456789abcdef", "0123456789abcdef0123456789abcdef"},
		{"a-very-long-secret-value-that-exceeds-thirty-two-bytes-in-length", "a-very-long-secret-value-that-ex"},
	}
	for _, c := range cases {
		if got := string(QQBotSecretToSeed(c.secret)); got != c.want {
			t.Errorf("secret=%q seed=%q 期望 %q", c.secret, got, c.want)
		}
	}
	if got := len(QQBotSecretToSeed("")); got != 0 {
		t.Errorf("空 secret 应返回空 seed，实际长度 %d", got)
	}
	if QQBotPrivateKey("") != nil {
		t.Error("空 secret 应返回 nil 私钥")
	}
}

func TestQQBotKeyAndSignature(t *testing.T) {
	for _, ref := range qqBotSignatureRefs {
		pub, ok := QQBotPrivateKey(ref.Secret).Public().(ed25519.PublicKey)
		if !ok {
			t.Fatalf("secret=%q 公钥派生失败", ref.Secret)
		}
		if got := hex.EncodeToString(pub); got != ref.PubkeyHex {
			t.Errorf("secret=%q 公钥=%s 期望 %s", ref.Secret, got, ref.PubkeyHex)
		}
		sig := ed25519.Sign(QQBotPrivateKey(ref.Secret), []byte(ref.Timestamp+ref.PlainToken))
		if got := hex.EncodeToString(sig); got != ref.SignatureHex {
			t.Errorf("secret=%q 签名=%s 期望 %s", ref.Secret, got, ref.SignatureHex)
		}
	}
}

func TestQQBotSignValidation(t *testing.T) {
	for _, ref := range qqBotSignatureRefs {
		got, err := QQBotSignValidation(ref.Secret, ref.Timestamp, ref.PlainToken)
		if err != nil {
			t.Fatalf("secret=%q 签名失败: %v", ref.Secret, err)
		}
		if got != ref.SignatureHex {
			t.Errorf("secret=%q 签名=%s 期望 %s", ref.Secret, got, ref.SignatureHex)
		}
	}
	if _, err := QQBotSignValidation("s", "", "token"); err == nil {
		t.Error("空 event_ts 应返回错误")
	}
	if _, err := QQBotSignValidation("s", "1", ""); err == nil {
		t.Error("空 plain_token 应返回错误")
	}
	if _, err := QQBotSignValidation("", "1", "tok"); err == nil {
		t.Error("空 AppSecret 应返回错误而非 panic")
	}
}

/* TestQQBotVerifySignatureEmptySecret 空 AppSecret 必须安全返回 false，不得 panic */
func TestQQBotVerifySignatureEmptySecret(t *testing.T) {
	if QQBotVerifySignature("", "1", qqBotSignatureRefs[0].SignatureHex, "tok") {
		t.Error("空 AppSecret 时不应通过验签")
	}
}

func TestQQBotVerifySignature(t *testing.T) {
	for _, ref := range qqBotSignatureRefs {
		if !QQBotVerifySignature(ref.Secret, ref.Timestamp, ref.SignatureHex, ref.PlainToken) {
			t.Errorf("secret=%q 正确签名未通过验签", ref.Secret)
		}
		if QQBotVerifySignature(ref.Secret, ref.Timestamp, ref.SignatureHex, ref.PlainToken+"x") {
			t.Errorf("secret=%q 篡改 body 后仍通过验签", ref.Secret)
		}
		if QQBotVerifySignature(ref.Secret, ref.Timestamp+"1", ref.SignatureHex, ref.PlainToken) {
			t.Errorf("secret=%q 篡改 timestamp 后仍通过验签", ref.Secret)
		}
		if QQBotVerifySignature("other-secret", ref.Timestamp, ref.SignatureHex, ref.PlainToken) {
			t.Errorf("secret=%q 错误密钥仍通过验签", ref.Secret)
		}
		if !QQBotVerifySignature(ref.Secret, " "+ref.Timestamp+" ", " "+strings.ToUpper(ref.SignatureHex)+" ", ref.PlainToken) {
			t.Errorf("secret=%q 大写签名或空白未兼容", ref.Secret)
		}
	}
}

func TestQQBotVerifySignatureRejectsMalformed(t *testing.T) {
	ref := qqBotSignatureRefs[0]

	sig, err := hex.DecodeString(ref.SignatureHex)
	if err != nil {
		t.Fatalf("基准签名解码失败: %v", err)
	}
	malleable := make([]byte, len(sig))
	copy(malleable, sig)
	malleable[63] |= 224
	if QQBotVerifySignature(ref.Secret, ref.Timestamp, hex.EncodeToString(malleable), ref.PlainToken) {
		t.Error("末字节高 3 位非 0 的签名被接受")
	}

	rejects := []struct{ name, ts, sig, body string }{
		{"长度不足", ref.Timestamp, "00", ref.PlainToken},
		{"长度固定但非 hex", ref.Timestamp, strings.Repeat("z", 128), ref.PlainToken},
		{"长度超标", ref.Timestamp, ref.SignatureHex + "00", ref.PlainToken},
		{"空时间戳", "", ref.SignatureHex, ref.PlainToken},
		{"空签名", ref.Timestamp, "", ref.PlainToken},
		{"奇数长度 hex", ref.Timestamp, ref.SignatureHex[:127], ref.PlainToken},
	}
	for _, r := range rejects {
		if QQBotVerifySignature(ref.Secret, r.ts, r.sig, r.body) {
			t.Errorf("%s 的签名被接受", r.name)
		}
	}
}

func TestQQBotToMarkdown(t *testing.T) {
	title := "DNS容灾切换-发生告警通知"
	content := `尊敬的用户，您好：<br/>您的域名 <b>example.com</b> 的 <b>www</b> 记录发生了异常，请及时处理。<br/>异常信息：<font color="warning">timeout</font><br/><font color="grey">聚合DNS管理系统</font>`
	want := "# " + title + "\n\n" + `尊敬的用户，您好：
您的域名 **example.com** 的 **www** 记录发生了异常，请及时处理。
异常信息：timeout
聚合DNS管理系统`
	if got := QQBotToMarkdown(title, content); got != want {
		t.Errorf("toMarkdown 不匹配\ngot:  %q\nwant: %q", got, want)
	}
	if got := QQBotToMarkdown("t", "&lt;a&gt; &amp; &quot;q&quot;"); got != "# t\n\n<a> & \"q\"" {
		t.Errorf("HTML 实体解码不匹配: %q", got)
	}
	if got := QQBotToMarkdown("t", "  <br/>  x  "); got != "# t\n\nx" {
		t.Errorf("首尾空白未裁剪: %q", got)
	}
	if got := QQBotToMarkdown("t", "a < b > c"); got != "# t\n\na < b > c" {
		t.Errorf("裸尖括号被误删: %q", got)
	}
}
