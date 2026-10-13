package apiv1

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"

	"main/internal/database"
	"main/internal/models"
)

/*
 * 优选 IP 四条需要 id 的接口必须真的能拿到 id
 *
 * 该组此前是裸 `v1.Group("/optimizeip")`、未挂桥接，而 update / toggle /
 * delete / run 四条读的是 `c.Param("id")`——在 v1 的固定路径下恒为空串，
 * `ParseUint` 丢弃错误得 0。后果分两种：
 *
 *   update / run  -> 先 First(&task, 0) 查不到，报 4006「任务不存在」
 *   toggle/delete -> 不查存在性，直接按 id=0 写/删，**报 code=0「成功」而库里无变化**
 *
 * 后两条是最坏形态：调用方看到成功，实际什么都没发生。
 * 这批用例锁的是「参数真的送达了」——断言的是**数据库里的行**，不是响应文案
 */

/* seedOptimizeTask 造一个真任务，返回其 ID 与所属域名 */
func seedOptimizeTask(t *testing.T, tag string) (models.User, string, models.OptimizeIP) {
	t.Helper()
	admin, key := seedAPIAdmin(t, tag)

	account := models.Account{UserID: admin.ID, Type: "cloudflare", Name: tag + "-acc", Config: "{}"}
	if err := database.DB.Create(&account).Error; err != nil {
		t.Fatalf("创建账户失败: %v", err)
	}
	domain := models.Domain{AccountID: account.ID, Name: tag + ".example.com", ThirdID: tag + "-zone"}
	if err := database.DB.Create(&domain).Error; err != nil {
		t.Fatalf("创建域名失败: %v", err)
	}
	task := models.OptimizeIP{
		DomainID: domain.ID, RR: "cdn", Type: 0, IPType: "ipv4",
		RecordNum: 3, TTL: 600, Active: true,
	}
	if err := database.DB.Create(&task).Error; err != nil {
		t.Fatalf("创建优选任务失败: %v", err)
	}
	return admin, key, task
}

/* callOpt 向优化 IP 路由发一次请求 */
func callOpt(t *testing.T, r *gin.Engine, uid uint, key, path, body string) v1Err {
	t.Helper()
	resp := doRequest(r, http.MethodPost, path, []byte(body),
		signedHeaders(t, uid, key, http.MethodPost, path, body))
	var got v1Err
	_ = json.NewDecoder(resp.Body).Decode(&got)
	resp.Body.Close()
	return got
}

/*
 * TestOptimizeIPUpdateReachesHandler 带 id 的修改必须真的改到那一行
 */
func TestOptimizeIPUpdateReachesHandler(t *testing.T) {
	setupBodyTestDB(t)
	admin, key, task := seedOptimizeTask(t, "opt-upd")

	r := v1FullEngine(nil)
	body := `{"id":"` + itoa(task.ID) + `","did":` + itoa(task.DomainID) +
		`,"rr":"changed","ip_type":"ipv4","recordnum":5,"ttl":900}`
	got := callOpt(t, r, admin.ID, key, "/api/v1/optimizeip/tasks/update", body)

	if got.Code != 0 {
		t.Fatalf("带 id 的修改应成功，实际 code=%d message=%q——说明 id 没送达", got.Code, got.Message)
	}
	var after models.OptimizeIP
	if err := database.DB.First(&after, task.ID).Error; err != nil {
		t.Fatalf("读回任务失败: %v", err)
	}
	if after.RR != "changed" || after.RecordNum != 5 {
		t.Errorf("库里未变：rr=%q recordnum=%d，期望 changed/5——响应报成功但没做事",
			after.RR, after.RecordNum)
	}
}

/*
 * TestOptimizeIPToggleReachesHandler 带 id 的启停必须真的改到那一行
 *
 * 这条此前是最坏形态：handler 不查存在性，id=0 时照样回「设置成功」
 */
func TestOptimizeIPToggleReachesHandler(t *testing.T) {
	setupBodyTestDB(t)
	admin, key, task := seedOptimizeTask(t, "opt-tog")

	r := v1FullEngine(nil)
	body := `{"id":"` + itoa(task.ID) + `","active":false}`
	got := callOpt(t, r, admin.ID, key, "/api/v1/optimizeip/tasks/toggle", body)

	if got.Code != 0 {
		t.Fatalf("带 id 的启停应成功，实际 code=%d message=%q", got.Code, got.Message)
	}
	var after models.OptimizeIP
	database.DB.First(&after, task.ID)
	if after.Active {
		t.Error("库里 active 仍为 true——响应报「设置成功」但没改到那一行")
	}
}

/*
 * TestOptimizeIPDeleteReachesHandler 带 id 的删除必须真的删掉那一行
 *
 * 同上：此前报「删除成功」而库里一行不少
 */
func TestOptimizeIPDeleteReachesHandler(t *testing.T) {
	setupBodyTestDB(t)
	admin, key, task := seedOptimizeTask(t, "opt-del")

	r := v1FullEngine(nil)
	got := callOpt(t, r, admin.ID, key, "/api/v1/optimizeip/tasks/delete",
		`{"id":"`+itoa(task.ID)+`"}`)

	if got.Code != 0 {
		t.Fatalf("带 id 的删除应成功，实际 code=%d message=%q", got.Code, got.Message)
	}
	var n int64
	database.DB.Model(&models.OptimizeIP{}).Where("id = ?", task.ID).Count(&n)
	if n != 0 {
		t.Errorf("库里仍剩 %d 行——响应报「删除成功」但没删掉", n)
	}
}

/*
 * TestOptimizeIPRunReportsNotFoundForMissingTask run 对不存在的任务是 4006
 *
 * 与上面三条配对：run 是唯一在 v1 下「报错而非静默成功」的一条，
 * 因为它在写之前先 First 查存在性
 */
func TestOptimizeIPRunReportsNotFoundForMissingTask(t *testing.T) {
	setupBodyTestDB(t)
	admin, key, _ := seedOptimizeTask(t, "opt-run")

	r := v1FullEngine(nil)
	got := callOpt(t, r, admin.ID, key, "/api/v1/optimizeip/tasks/run", `{"id":"999999"}`)

	if got.Code != CodeTaskNotFound {
		t.Errorf("不存在的任务应回 CodeTaskNotFound(%d)，实际 code=%d message=%q",
			CodeTaskNotFound, got.Code, got.Message)
	}
}

/*
 * TestOptimizeIPListStillWorks 未受影响的 list 仍正常
 *
 * 桥接是组级的，list 不读 :id；这条确认补挂没有把它弄坏
 */
func TestOptimizeIPListStillWorks(t *testing.T) {
	setupBodyTestDB(t)
	admin, key, task := seedOptimizeTask(t, "opt-list")

	r := v1FullEngine(nil)
	got := callOpt(t, r, admin.ID, key, "/api/v1/optimizeip/tasks/list", `{"limit":10}`)

	if got.Code != 0 {
		t.Fatalf("list 应正常，实际 code=%d message=%q", got.Code, got.Message)
	}
	_ = task
}
