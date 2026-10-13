package cloudflare

import (
	"context"
	"testing"
)

/*
 * TestLinesCatalogMatchesProxyMapping 锁定 Cloudflare 线路编目的内容与条数
 *
 * 编目里的 id 会被 AddDomainRecord/UpdateDomainRecord 当作线路值判定：
 * 只有 "1" 代表开启代理，其余一律仅 DNS。因此 id 集合一旦变动，
 * 代理开关的语义就会跟着漂移，这里用固定期望值锁住
 */
func TestLinesCatalogMatchesProxyMapping(t *testing.T) {
	if len(Lines) != 2 {
		t.Fatalf("Cloudflare 线路应为 2 条，实际 %d 条：%v", len(Lines), Lines)
	}

	want := []struct {
		id   string
		name string
	}{
		{id: "0", name: "仅DNS"},
		{id: "1", name: "已代理"},
	}
	for i, w := range want {
		if Lines[i].ID != w.id || Lines[i].Name != w.name {
			t.Errorf("第 %d 条线路应为 %s/%s，实际 %s/%s", i, w.id, w.name, Lines[i].ID, Lines[i].Name)
		}
	}
}

/*
 * TestGetRecordLineReturnsCatalogCopy 断言 GetRecordLine 返回编目内容且不暴露内部切片
 *
 * provider 的每次调用都应拿到独立副本，调用方改动返回值不应污染包级编目
 */
func TestGetRecordLineReturnsCatalogCopy(t *testing.T) {
	provider := NewProvider(map[string]string{"apikey": "0123456789abcdef"}, "example.com", "zone1")

	lines, err := provider.GetRecordLine(context.Background())
	if err != nil {
		t.Fatalf("获取线路失败: %v", err)
	}
	if len(lines) != len(Lines) {
		t.Fatalf("返回线路数应为 %d，实际 %d", len(Lines), len(lines))
	}
	for i := range Lines {
		if lines[i] != Lines[i] {
			t.Errorf("第 %d 条线路应为 %+v，实际 %+v", i, Lines[i], lines[i])
		}
	}

	lines[0].ID = "tampered"
	if Lines[0].ID != "0" {
		t.Errorf("调用方改动返回值污染了包级编目，实际 %q", Lines[0].ID)
	}
}
