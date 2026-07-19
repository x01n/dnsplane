package dns

import (
	"context"
	"fmt"
	"strings"
)

func recordsFromPageResult(pr *PageResult) ([]Record, error) {
	if pr == nil || pr.Records == nil {
		return nil, nil
	}
	list, ok := pr.Records.([]Record)
	if !ok {
		return nil, fmt.Errorf("dns: unexpected PageResult.Records type %T", pr.Records)
	}
	return list, nil
}

func normalizeChallengeValue(recordType, v string) string {
	v = strings.TrimSpace(v)
	v = strings.TrimSuffix(v, ".")
	if strings.EqualFold(recordType, "TXT") {
		if len(v) >= 2 && v[0] == '"' && v[len(v)-1] == '"' {
			v = v[1 : len(v)-1]
		}
	}
	return v
}

func challengeValuesEqual(recordType, a, b string) bool {
	ra := normalizeChallengeValue(recordType, a)
	rb := normalizeChallengeValue(recordType, b)
	if ra == rb {
		return true
	}
	if strings.EqualFold(recordType, "TXT") {
		strip := func(s string) string {
			s = strings.ReplaceAll(s, `"`, "")
			s = strings.ReplaceAll(s, " ", "")
			return s
		}
		return strip(ra) == strip(rb)
	}
	return false
}

func EnsureChallengeRecord(ctx context.Context, p Provider, subHost, recordType, wantValue, line string, ttl int, remark string) (recordID string, skipped bool, err error) {
	rt := strings.TrimSpace(recordType)
	if rt == "" {
		rt = "TXT"
	}

	// TTL 尽量小以加快 DNS 生效
	if ttl <= 0 || ttl > 600 {
		ttl = 60
	}

	pr, err := p.GetSubDomainRecords(ctx, subHost, 1, 100, "", "")
	if err != nil {
		if TreatAsEmptySubDomainRecordListError(err) {
			pr = &PageResult{Total: 0, Records: []Record{}}
		} else {
			return "", false, err
		}
	}
	list, err := recordsFromPageResult(pr)
	if err != nil {
		return "", false, err
	}

	var sameType []Record
	for i := range list {
		if strings.EqualFold(list[i].Type, rt) {
			sameType = append(sameType, list[i])
		}
	}

	// 如果已存在完全相同值的记录，直接跳过
	for _, rec := range sameType {
		if challengeValuesEqual(rt, rec.Value, wantValue) {
			if rec.ID != "" {
				return rec.ID, true, nil
			}
			return "", true, nil
		}
	}

	if strings.EqualFold(rt, "TXT") {
		// TXT 记录：直接追加新记录，不更新已有记录。
		// 同一 _acme-challenge subdomain 可能需要同时存在多条不同 TXT 值
		// （如 *.example.com 和 example.com 同时申请证书时）。
		rid, err := p.AddDomainRecord(ctx, subHost, rt, wantValue, line, ttl, 0, nil, remark)
		return rid, false, err
	}

	for _, rec := range sameType {
		if rec.ID == "" {
			continue
		}
		if line != "" && rec.Line != "" && rec.Line != line {
			continue
		}
		if err := p.DeleteDomainRecord(ctx, rec.ID); err != nil {
			return "", false, fmt.Errorf("删除旧 %s 记录 %s: %w", rt, rec.ID, err)
		}
	}

	rid, err := p.AddDomainRecord(ctx, subHost, rt, wantValue, line, ttl, 0, nil, remark)
	return rid, false, err
}

// EnsureChallengeRecordWithTTLProbe 带 TTL 探测的版本，按给定 TTL 列表依次尝试
func EnsureChallengeRecordWithTTLProbe(ctx context.Context, p Provider, subHost, recordType, wantValue, line string, ttlList []int, remark string) (recordID string, skipped bool, usedTTL int, err error) {
	rt := strings.TrimSpace(recordType)
	if rt == "" {
		rt = "TXT"
	}

	pr, err := p.GetSubDomainRecords(ctx, subHost, 1, 100, "", "")
	if err != nil {
		if TreatAsEmptySubDomainRecordListError(err) {
			pr = &PageResult{Total: 0, Records: []Record{}}
		} else {
			return "", false, 0, err
		}
	}
	list, err := recordsFromPageResult(pr)
	if err != nil {
		return "", false, 0, err
	}

	var sameType []Record
	for i := range list {
		if strings.EqualFold(list[i].Type, rt) {
			sameType = append(sameType, list[i])
		}
	}

	for _, rec := range sameType {
		if challengeValuesEqual(rt, rec.Value, wantValue) && (line == "" || rec.Line == "" || rec.Line == line) {
			if rec.ID != "" {
				return rec.ID, true, 0, nil
			}
			return "", true, 0, nil
		}
	}

	if !strings.EqualFold(rt, "TXT") {
		for _, rec := range sameType {
			if rec.ID == "" {
				continue
			}
			// 只删除同线路的旧记录，避免误删其他线路的有效记录
			if line != "" && rec.Line != "" && rec.Line != line {
				continue
			}
			if delErr := p.DeleteDomainRecord(ctx, rec.ID); delErr != nil {
				return "", false, 0, fmt.Errorf("删除旧 %s 记录 %s: %w", rt, rec.ID, delErr)
			}
		}
	}

	if len(ttlList) == 0 {
		ttlList = []int{60}
	}

	var lastErr error
	for _, ttl := range ttlList {
		rid, addErr := p.AddDomainRecord(ctx, subHost, rt, wantValue, line, ttl, 0, nil, remark)
		if addErr == nil {
			return rid, false, ttl, nil
		}
		lastErr = addErr
	}
	return "", false, 0, lastErr
}
