package apiv1

import (
	"net/http"
	"testing"
)

/*
 * TestParamAliasBridgesConsoleHandlers 断言组级桥接真的把 body 里的 ID 送到了 handler
 *
 * 判定对象是 handler 的**响应码**，不是 SQL：
 *   送达 → 该 ID 被解析成主键，查不到 → 4001/4002 类「不存在」
 *   未送达 → c.Param 为空串或零值，handler 走另一条分支 → 2001/2002「缺少 ID」
 *
 * 用响应码而不是 SQL，是因为 SQL 判据区分不了「WHERE id=999999 查不到」与
 * 「WHERE id=0 查不到」——两者都出现等值条件，但只有前者说明入参送达了
 */
func TestParamAliasBridgesConsoleHandlers(t *testing.T) {
	setupBodyTestDB(t)
	admin, key := seedAPIAdmin(t, "bridge-admin")

	r := v1FullEngine(nil)

	cases := []struct {
		path     string
		body     string
		wantCode int
		note     string
	}{
		// domains 组：:id ← body.domain_id
		{"/api/v1/domains/aliases", `{"domain_id":"999999"}`, 4002, "组级 paramAlias"},
		{"/api/v1/domains/aliases/add", `{"domain_id":"999999","name":"a.example.com"}`, 4002, "组级 paramAlias"},
		{"/api/v1/domains/groups", `{"domain_id":"999999"}`, 4002, "组级 paramAlias"},
		{"/api/v1/domains/groups/change", `{"domain_id":"999999","record_ids":["1"],"group_id":"1"}`, 4002, "组级 paramAlias"},
		{"/api/v1/records/weight", `{"domain_id":"999999","name":"www"}`, 4002, "组级 paramAlias"},
		{"/api/v1/records/quickinfo", `{"domain_id":"999999"}`, 4002, "组级 paramAlias"},
		{"/api/v1/records/logs", `{"domain_id":"999999"}`, 4002, "组级 paramAlias"},
		// domains 组外单条：键名各不相同
		{"/api/v1/domains/aliases/delete", `{"alias_id":"999999"}`, 4001, "单条 paramAlias（aliasId）"},
		{"/api/v1/domains/categories/update", `{"id":"999999","name":"x"}`, 4001, "单条 paramAlias（id）"},
		{"/api/v1/domains/categories/delete", `{"id":"999999"}`, 4001, "单条 paramAlias（id）"},
		// schedule 子组：:id ← body.id
		{"/api/v1/schedule/tasks/update", `{"id":"999999","did":1,"rr":"www","recordid":"1"}`, 4001, "子组 paramAlias"},
		{"/api/v1/schedule/tasks/delete", `{"id":"999999"}`, 4001, "子组 paramAlias"},
		{"/api/v1/schedule/tasks/toggle", `{"id":"999999"}`, 4001, "子组 paramAlias"},
	}

	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			resp := doRequest(r, http.MethodPost, tc.path, []byte(tc.body),
				signedHeaders(t, admin.ID, key, http.MethodPost, tc.path, tc.body))
			var got v1Err
			decodeBody(t, resp, &got)

			if got.Code != tc.wantCode {
				t.Errorf("%s（%s）期望 %d，实际 code=%d message=%q",
					tc.path, tc.note, tc.wantCode, got.Code, got.Message)
			}
		})
	}
}
