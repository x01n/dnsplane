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

	for _, rec := range sameType {
		if challengeValuesEqual(rt, rec.Value, wantValue) {
			if rec.ID != "" {
				return rec.ID, true, nil
			}
			return "", true, nil
		}
	}
	if strings.EqualFold(rt, "TXT") {
		rid, err := p.AddDomainRecord(ctx, subHost, rt, wantValue, line, ttl, 0, nil, remark)
		return rid, false, err
	}

	for _, rec := range sameType {
		if rec.ID == "" {
			continue
		}
		if err := p.DeleteDomainRecord(ctx, rec.ID); err != nil {
			return "", false, fmt.Errorf("删除旧 %s 记录 %s: %w", rt, rec.ID, err)
		}
	}

	rid, err := p.AddDomainRecord(ctx, subHost, rt, wantValue, line, ttl, 0, nil, remark)
	return rid, false, err
}
