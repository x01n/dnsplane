package handler

import (
	"context"
	"fmt"
	"strings"
	"testing"

	maindns "main/internal/dns"
)

/*
 * conflictFakeProvider 冲突清理用例的 DNS provider 替身
 *
 * 只实现 removeConflictingRecords 真正会调用的两个方法，其余按接口要求给出空实现，
 * 避免用例依赖真实云端
 */
type conflictFakeProvider struct {
	records   []maindns.Record
	deleted   []string
	deleteErr error
	listErr   error
}

func (f *conflictFakeProvider) GetError() string            { return "" }
func (f *conflictFakeProvider) Check(context.Context) error { return nil }

func (f *conflictFakeProvider) GetDomainList(context.Context, string, int, int) (*maindns.PageResult, error) {
	return nil, nil
}

func (f *conflictFakeProvider) GetDomainRecords(context.Context, int, int, string, string, string, string, string, string) (*maindns.PageResult, error) {
	return nil, nil
}

func (f *conflictFakeProvider) GetSubDomainRecords(ctx context.Context, subDomain string, page, pageSize int, recordType, line string) (*maindns.PageResult, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return &maindns.PageResult{Total: len(f.records), Records: f.records}, nil
}

func (f *conflictFakeProvider) GetDomainRecordInfo(context.Context, string) (*maindns.Record, error) {
	return nil, nil
}

func (f *conflictFakeProvider) AddDomainRecord(context.Context, string, string, string, string, int, int, *int, string) (string, error) {
	return "", nil
}

func (f *conflictFakeProvider) UpdateDomainRecord(context.Context, string, string, string, string, string, int, int, *int, string) error {
	return nil
}

func (f *conflictFakeProvider) UpdateDomainRecordRemark(context.Context, string, string) error {
	return nil
}

func (f *conflictFakeProvider) DeleteDomainRecord(ctx context.Context, recordID string) error {
	if f.deleteErr != nil {
		return f.deleteErr
	}
	f.deleted = append(f.deleted, recordID)
	return nil
}

func (f *conflictFakeProvider) SetDomainRecordStatus(context.Context, string, bool) error {
	return nil
}

func (f *conflictFakeProvider) GetDomainRecordLog(context.Context, int, int, string, string, string) (*maindns.PageResult, error) {
	return nil, nil
}

func (f *conflictFakeProvider) GetRecordLine(context.Context) ([]maindns.RecordLine, error) {
	return nil, nil
}

func (f *conflictFakeProvider) GetMinTTL() int { return 600 }

func (f *conflictFakeProvider) AddDomain(context.Context, string) error { return nil }

/* conflictRecords 构造三条分线路的 A 记录 */
func conflictRecords() []maindns.Record {
	return []maindns.Record{
		{ID: "1", Name: "www", Type: "A", Value: "192.0.2.1", Line: "Dianxin"},
		{ID: "2", Name: "www", Type: "A", Value: "192.0.2.2", Line: "Liantong"},
		{ID: "3", Name: "www", Type: "A", Value: "192.0.2.3", Line: "Yidong"},
	}
}

/*
 * TestLegacyRemoveConflictingRecordsDeletedAllLines 用旧实现复现「线路未知时删光全部」
 *
 * legacyRemoveConflictingRecords 逐字保留改动前的逻辑：line == "" 命中 `line == ""` 分支，
 * 该子域全部线路的 A/AAAA 被一次删光
 */
func TestLegacyRemoveConflictingRecordsDeletedAllLines(t *testing.T) {
	provider := &conflictFakeProvider{records: conflictRecords()}

	legacyRemoveConflictingRecords(context.Background(), provider, "www", "")

	if len(provider.deleted) != 3 {
		t.Fatalf("旧实现应删光三条记录以证明缺陷，实际删除 %v", provider.deleted)
	}
}

/* legacyRemoveConflictingRecords 改动前的实现，仅用于复现缺陷，不得在生产代码中引用 */
func legacyRemoveConflictingRecords(ctx context.Context, provider maindns.Provider, subHost, line string) {
	pr, err := provider.GetSubDomainRecords(ctx, subHost, 1, 100, "", "")
	if err != nil {
		return
	}
	list, ok := pr.Records.([]maindns.Record)
	if !ok {
		return
	}
	for _, rec := range list {
		rt := strings.ToUpper(rec.Type)
		if rt != "A" && rt != "AAAA" {
			continue
		}
		if rec.Line == line || line == "" || rec.Line == "" {
			_ = provider.DeleteDomainRecord(ctx, rec.ID)
		}
	}
}

/*
 * TestRemoveConflictingRecordsSkipsWhenLineUnknown 断言线路未知时不删除任何记录
 *
 * 旧实现在 line == "" 时命中 `line == ""` 分支，把该子域**全部线路**的 A/AAAA 一次删光。
 * 触发路径：eo_only / esa_only 策略走 default 分支取 lineMapping["DEF"]，
 * 而 dnspod / west / dnsla 的 DEF 就是空串
 */
func TestRemoveConflictingRecordsSkipsWhenLineUnknown(t *testing.T) {
	provider := &conflictFakeProvider{records: conflictRecords()}

	deleted, skipped, err := removeConflictingRecords(context.Background(), provider, "www", "")
	if err != nil {
		t.Fatalf("线路未知时不应报错，实际 %v", err)
	}
	if !skipped {
		t.Error("线路未知时应标记为已跳过，便于调用方提示用户")
	}
	if deleted != 0 {
		t.Errorf("线路未知时不应删除任何记录，实际删除 %d 条", deleted)
	}
	if len(provider.deleted) != 0 {
		t.Errorf("不应调用删除接口，实际删除 %v", provider.deleted)
	}
}

/*
 * TestRemoveConflictingRecordsDeletesSameLineOnly 断言只删除指定线路的记录
 */
func TestRemoveConflictingRecordsDeletesSameLineOnly(t *testing.T) {
	provider := &conflictFakeProvider{records: conflictRecords()}

	deleted, skipped, err := removeConflictingRecords(context.Background(), provider, "www", "Dianxin")
	if err != nil {
		t.Fatalf("不应报错，实际 %v", err)
	}
	if skipped {
		t.Error("线路已知时不应标记为跳过")
	}
	if deleted != 1 {
		t.Errorf("应只删除 1 条，实际 %d 条", deleted)
	}
	if len(provider.deleted) != 1 || provider.deleted[0] != "1" {
		t.Errorf("应只删除线路为 Dianxin 的记录 1，实际 %v", provider.deleted)
	}
}

/*
 * TestRemoveConflictingRecordsReportsDeleteFailure 断言删除失败会向上返回错误
 *
 * 旧实现用 `_ =` 丢弃删除错误，调用方随后照样写入 CNAME，
 * 结果是同一子域同一线路同时存在 A 与 CNAME（DNS 上非法）
 */
func TestRemoveConflictingRecordsReportsDeleteFailure(t *testing.T) {
	provider := &conflictFakeProvider{records: conflictRecords(), deleteErr: fmt.Errorf("远端拒绝删除")}

	deleted, _, err := removeConflictingRecords(context.Background(), provider, "www", "Dianxin")
	if err == nil {
		t.Fatal("删除失败时应返回错误，否则后续写入会造成 A 与 CNAME 并存")
	}
	if deleted != 0 {
		t.Errorf("删除失败时成功计数应为 0，实际 %d", deleted)
	}
}

/*
 * TestRemoveConflictingRecordsReportsListFailure 断言列举记录失败也会向上返回错误
 *
 * 旧实现遇到列举失败直接 return，调用方无法区分「没有冲突记录」与「没查到」，
 * 会在状态未知的情况下继续写入
 */
func TestRemoveConflictingRecordsReportsListFailure(t *testing.T) {
	provider := &conflictFakeProvider{listErr: fmt.Errorf("列举失败")}

	if _, _, err := removeConflictingRecords(context.Background(), provider, "www", "Dianxin"); err == nil {
		t.Fatal("列举记录失败时应返回错误")
	}
}

/*
 * TestRemoveConflictingRecordsTreatsEmptyRecordLineAsUnmatched 断言记录侧线路为空时不删除
 *
 * 记录自身线路未知（如 huawei 的 provider 不返回线路字段）时无法判断是否同线路，
 * 保守放过，交给调用方提示，而不是连同其他线路一起删
 */
func TestRemoveConflictingRecordsTreatsEmptyRecordLineAsUnmatched(t *testing.T) {
	provider := &conflictFakeProvider{records: []maindns.Record{
		{ID: "9", Name: "www", Type: "A", Value: "192.0.2.9", Line: ""},
	}}

	deleted, _, err := removeConflictingRecords(context.Background(), provider, "www", "Dianxin")
	if err != nil {
		t.Fatalf("不应报错，实际 %v", err)
	}
	if deleted != 0 {
		t.Errorf("记录线路未知时不应删除，实际删除 %d 条", deleted)
	}
}
