package cache

import (
	"strconv"
	"testing"
)

// TestMemoryLTrimKeepsRange 内存缓存的 LTrim 必须与 Redis 同名命令一致：
// 保留闭区间 [start, stop]，负下标从尾部计数，区间取空则清空该列表。
// 每个用例用独立键，避免上一次的列表残留干扰（Delete 只作用于键值区，清不掉列表区）
func TestMemoryLTrimKeepsRange(t *testing.T) {
	Init(&Config{})
	defer func() { _ = Close() }()

	// LPush 头插：依次压入 a b c d 后列表为 [d c b a]
	for i, tc := range []struct {
		name        string
		start, stop int64
		want        []string
	}{
		{"保留前两个", 0, 1, []string{"d", "c"}},
		{"保留末尾一个", -1, -1, []string{"a"}},
		{"起点越界则清空", 4, 5, nil},
		{"区间取空则清空", 1, 0, nil},
		{"起止颠倒则清空", 3, 1, nil},
	} {
		key := "probe-" + strconv.Itoa(i)
		for _, v := range []string{"a", "b", "c", "d"} {
			_ = C.LPush(key, v)
		}

		if err := C.LTrim(key, tc.start, tc.stop); err != nil {
			t.Fatalf("%s：LTrim 失败: %v", tc.name, err)
		}
		got, _ := C.LRange(key, 0, -1)
		if len(got) != len(tc.want) {
			t.Errorf("%s：剩余 %v，期望 %v", tc.name, got, tc.want)
			continue
		}
		for j := range got {
			if got[j] != tc.want[j] {
				t.Errorf("%s：剩余 %v，期望 %v", tc.name, got, tc.want)
				break
			}
		}
	}
}
