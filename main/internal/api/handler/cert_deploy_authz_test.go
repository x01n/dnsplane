package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"main/internal/cache"
	"main/internal/config"
	"main/internal/database"
	"main/internal/models"
)

/*
 * cert/deploys 全族的越权测试
 *
 * 攻击链：B 用自己的部署账户 + A 的证书订单创建部署，再触发 ProcessCertDeploy，
 * handler 会把 A 的私钥送到 B 指定的服务器。三处入口（创建/更新/删除/执行）此前
 * 均无所有权校验，只有 checkLocalDeployAllowed 拦 local 类型——那是防「借进程权限
 * 执行命令」，不是防「私钥外送」，改用 ssh 类型即可绕开
 *
 * 这些用例全部走真实 handler，不依赖网络：CreateCertDeploy 在校验阶段即被拒，
 * 不会进入 deploy.GetProvider
 */

/* setupCertTestDB 初始化临时库与缓存 */
func setupCertTestDB(t *testing.T) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	if err := database.Init(&config.DatabaseConfig{
		Driver:   "sqlite",
		FilePath: filepath.Join(t.TempDir(), "certdeploy.db"),
	}); err != nil {
		t.Fatalf("初始化测试数据库失败: %v", err)
	}
	cache.Init(&cache.Config{})
	t.Cleanup(func() {
		_ = cache.Close()
		_ = database.Close()
	})
}

/* certTestUser 造一个普通用户 */
func certTestUser(t *testing.T, name string) models.User {
	t.Helper()
	u := models.User{Username: name, Email: name + "@example.com", Level: 1, Status: 1}
	if err := database.DB.Create(&u).Error; err != nil {
		t.Fatalf("创建用户失败: %v", err)
	}
	return u
}

/* certTestAccount 造一个证书账户，返回其 ID */
func certTestAccount(t *testing.T, owner uint, accType string) models.CertAccount {
	t.Helper()
	acc := models.CertAccount{UserID: owner, Type: accType, Name: "acc-" + accType}
	if err := database.DB.Create(&acc).Error; err != nil {
		t.Fatalf("创建证书账户失败: %v", err)
	}
	return acc
}

/* certTestOrder 造一个已签发的证书订单（私钥非空，模拟真实可泄露的目标） */
func certTestOrder(t *testing.T, accID uint) models.CertOrder {
	t.Helper()
	order := models.CertOrder{
		AccountID:  accID,
		OrderKind:  "dns",
		FullChain:  "-----BEGIN CERTIFICATE-----\nFAKE\n-----END CERTIFICATE-----",
		PrivateKey: "-----BEGIN PRIVATE KEY-----\nSECRET\n-----END PRIVATE KEY-----",
	}
	if err := database.DB.Create(&order).Error; err != nil {
		t.Fatalf("创建证书订单失败: %v", err)
	}
	return order
}

/*
 * certTestDeploy 造一条属于指定用户的部署记录
 *
 * 种子里的 host 刻意与攻击者 payload 用不同的值：两处都用同一个串会让
 * 「配置未被改写」这条断言恒为假阴性——即便修改真的落库了，
 * Contains 仍会命中种子自带的那个串，用例照样通过
 */
func certTestDeploy(t *testing.T, owner, accID, orderID uint) models.CertDeploy {
	t.Helper()
	deploy := models.CertDeploy{
		UserID:    owner,
		AccountID: accID,
		OrderID:   orderID,
		Config:    `{"host":"origin.example.com"}`,
		Active:    true,
	}
	if err := database.DB.Create(&deploy).Error; err != nil {
		t.Fatalf("创建部署任务失败: %v", err)
	}
	return deploy
}

/*
 * certHandlerContext 构造带身份与 JSON body 的 gin 上下文
 *
 * uid 写字符串是控制台 Auth 中间件的既有约定（GetString("user_id")），
 * isAdmin 由 level 决定
 */
func certHandlerContext(t *testing.T, uid uint, level int, path, body string) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set("user_id", itoaUint(uid))
	c.Set("level", level)
	return c, w
}

/* itoaUint 数字转字符串（测试内联，避免依赖其它包的助手） */
func itoaUint(v uint) string {
	return strings.TrimSpace(json.Number(jsonUint(v)).String())
}

/* jsonUint 把 uint 转成可被 json.Number 解析的文本 */
func jsonUint(v uint) string {
	b, _ := json.Marshal(v)
	return string(b)
}

/* decodeCertResp 解析控制台格式的响应 */
func decodeCertResp(t *testing.T, w *httptest.ResponseRecorder) (int, string) {
	t.Helper()
	var resp struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("解析响应失败: %v（原文 %s）", err, w.Body.String())
	}
	return resp.Code, resp.Msg
}

/*
 * TestCreateCertDeployRejectsForeignOrder 断言 B 不能用 A 的订单创建部署
 *
 * 修复前：checkLocalDeployAllowed 只拦 local 类型，ssh 类型放行 → 部署记录写入成功
 * 修复后：requireCertOrderOwner 在写库前拒绝
 */
func TestCreateCertDeployRejectsForeignOrder(t *testing.T) {
	setupCertTestDB(t)
	victim := certTestUser(t, "victim")
	attacker := certTestUser(t, "attacker")

	victimAcc := certTestAccount(t, victim.ID, "letsencrypt")
	order := certTestOrder(t, victimAcc.ID)
	attackerDeployAcc := certTestAccount(t, attacker.ID, "ssh")

	body := `{"account_id":` + jsonUint(attackerDeployAcc.ID) + `,"order_id":` + jsonUint(order.ID) + `,"config":{"host":"attacker.example.com"}}`
	c, w := certHandlerContext(t, attacker.ID, 1, "/api/cert/deploys", body)
	CreateCertDeploy(c)

	code, msg := decodeCertResp(t, w)
	if code == 0 {
		t.Fatalf("用他人订单创建部署不应成功，实际 code=0 msg=%q", msg)
	}

	// 更关键：确认库里没有留下这条记录（私钥外送的下一步就是执行它）
	var count int64
	database.DB.Model(&models.CertDeploy{}).Where("oid = ? AND uid = ?", order.ID, attacker.ID).Count(&count)
	if count != 0 {
		t.Errorf("被拒后仍写入了 %d 条部署记录", count)
	}
}

/*
 * TestCreateCertDeployRejectsForeignAccount 断言 B 不能用 A 的部署账户
 *
 * 与上一条互补：账户维度也要校验，否则可用他人账户发起部署
 */
func TestCreateCertDeployRejectsForeignAccount(t *testing.T) {
	setupCertTestDB(t)
	victim := certTestUser(t, "victim2")
	attacker := certTestUser(t, "attacker2")

	victimDeployAcc := certTestAccount(t, victim.ID, "ssh")
	attackerCertAcc := certTestAccount(t, attacker.ID, "letsencrypt")
	order := certTestOrder(t, attackerCertAcc.ID)

	body := `{"account_id":` + jsonUint(victimDeployAcc.ID) + `,"order_id":` + jsonUint(order.ID) + `,"config":{}}`
	c, w := certHandlerContext(t, attacker.ID, 1, "/api/cert/deploys", body)
	CreateCertDeploy(c)

	if code, msg := decodeCertResp(t, w); code == 0 {
		t.Fatalf("用他人部署账户不应成功，实际 code=0 msg=%q", msg)
	}
}

/*
 * TestCreateCertDeployAllowsOwnResources 断言自有账户与自有订单仍可正常创建
 *
 * 反向用例：校验不能把合法路径一并挡掉
 */
func TestCreateCertDeployAllowsOwnResources(t *testing.T) {
	setupCertTestDB(t)
	owner := certTestUser(t, "owner")

	certAcc := certTestAccount(t, owner.ID, "letsencrypt")
	order := certTestOrder(t, certAcc.ID)
	deployAcc := certTestAccount(t, owner.ID, "ssh")

	body := `{"account_id":` + jsonUint(deployAcc.ID) + `,"order_id":` + jsonUint(order.ID) + `,"config":{"host":"mine.example.com"}}`
	c, w := certHandlerContext(t, owner.ID, 1, "/api/cert/deploys", body)
	CreateCertDeploy(c)

	code, msg := decodeCertResp(t, w)
	if code != 0 {
		t.Fatalf("自有资源应创建成功，实际 code=%d msg=%q", code, msg)
	}
	var count int64
	database.DB.Model(&models.CertDeploy{}).Where("uid = ?", owner.ID).Count(&count)
	if count != 1 {
		t.Errorf("应写入 1 条部署记录，实际 %d 条", count)
	}
}

/*
 * TestGetCertDeploysScopesToOwner 断言非管理员只看到自己的部署
 *
 * 修复前是无 where 的全表查询，普通用户可列出全站部署任务
 */
func TestGetCertDeploysScopesToOwner(t *testing.T) {
	setupCertTestDB(t)
	alice := certTestUser(t, "alice")
	bob := certTestUser(t, "bob")

	aliceAcc := certTestAccount(t, alice.ID, "letsencrypt")
	bobAcc := certTestAccount(t, bob.ID, "letsencrypt")
	aliceOrder := certTestOrder(t, aliceAcc.ID)
	bobOrder := certTestOrder(t, bobAcc.ID)
	certTestDeploy(t, alice.ID, aliceAcc.ID, aliceOrder.ID)
	certTestDeploy(t, bob.ID, bobAcc.ID, bobOrder.ID)

	// 非管理员 bob 只能看到自己的那条
	c, w := certHandlerContext(t, bob.ID, 1, "/api/cert/deploys", "")
	GetCertDeploys(c)

	var resp struct {
		Code int `json:"code"`
		Data []struct {
			ID  uint `json:"id"`
			UID uint `json:"uid"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("解析响应失败: %v（原文 %s）", err, w.Body.String())
	}
	if len(resp.Data) != 1 {
		t.Fatalf("普通用户应只看到 1 条部署，实际 %d 条", len(resp.Data))
	}
	if resp.Data[0].UID != bob.ID {
		t.Errorf("看到的部署属主应为 bob(%d)，实际 %d", bob.ID, resp.Data[0].UID)
	}

	// 管理员看全量
	ac, aw := certHandlerContext(t, 1, 2, "/api/cert/deploys", "")
	GetCertDeploys(ac)
	var adminResp struct {
		Data []struct {
			ID uint `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(aw.Body.Bytes(), &adminResp); err != nil {
		t.Fatalf("解析管理员响应失败: %v", err)
	}
	if len(adminResp.Data) != 2 {
		t.Errorf("管理员应看到 2 条部署，实际 %d 条", len(adminResp.Data))
	}
}

/*
 * TestUpdateCertDeployRejectsStranger 断言他人不能修改别人的部署配置
 *
 * 部署配置里含外送目标主机，改掉它等于把私钥改送到自己机器
 */
func TestUpdateCertDeployRejectsStranger(t *testing.T) {
	setupCertTestDB(t)
	victim := certTestUser(t, "victim3")
	attacker := certTestUser(t, "attacker3")

	victimAcc := certTestAccount(t, victim.ID, "letsencrypt")
	victimDeployAcc := certTestAccount(t, victim.ID, "ssh")
	order := certTestOrder(t, victimAcc.ID)
	deploy := certTestDeploy(t, victim.ID, victimDeployAcc.ID, order.ID)

	c, w := certHandlerContext(t, attacker.ID, 1, "/api/cert/deploys/"+jsonUint(deploy.ID), `{"config":{"host":"attacker.example.com"}}`)
	c.Params = gin.Params{{Key: "id", Value: jsonUint(deploy.ID)}}
	UpdateCertDeploy(c)

	if code, msg := decodeCertResp(t, w); code == 0 {
		t.Fatalf("修改他人部署不应成功，实际 code=0 msg=%q", msg)
	}

	// 配置未被改写
	var after models.CertDeploy
	database.DB.First(&after, deploy.ID)
	if strings.Contains(after.Config, "attacker.example.com") {
		t.Errorf("被拒后配置仍被改写为 %q", after.Config)
	}
}

/*
 * TestDeleteCertDeployRejectsStranger 断言他人不能删除别人的部署任务
 *
 * 修复前该 handler 连存在性都不查，直接 Delete
 */
func TestDeleteCertDeployRejectsStranger(t *testing.T) {
	setupCertTestDB(t)
	victim := certTestUser(t, "victim4")
	attacker := certTestUser(t, "attacker4")

	victimAcc := certTestAccount(t, victim.ID, "letsencrypt")
	victimDeployAcc := certTestAccount(t, victim.ID, "ssh")
	order := certTestOrder(t, victimAcc.ID)
	deploy := certTestDeploy(t, victim.ID, victimDeployAcc.ID, order.ID)

	c, w := certHandlerContext(t, attacker.ID, 1, "/api/cert/deploys/"+jsonUint(deploy.ID)+"/delete", "")
	c.Params = gin.Params{{Key: "id", Value: jsonUint(deploy.ID)}}
	DeleteCertDeploy(c)

	if code, msg := decodeCertResp(t, w); code == 0 {
		t.Fatalf("删除他人部署不应成功，实际 code=0 msg=%q", msg)
	}

	var count int64
	database.DB.Model(&models.CertDeploy{}).Where("id = ?", deploy.ID).Count(&count)
	if count != 1 {
		t.Error("被拒后部署记录仍被删除")
	}
}

/*
 * TestProcessCertDeployRejectsStranger 断言他人不能触发别人的部署执行
 *
 * 这是攻击链的最后一环：即使绕过创建环节，也不能执行他人的部署任务
 */
func TestProcessCertDeployRejectsStranger(t *testing.T) {
	setupCertTestDB(t)
	victim := certTestUser(t, "victim5")
	attacker := certTestUser(t, "attacker5")

	victimAcc := certTestAccount(t, victim.ID, "letsencrypt")
	victimDeployAcc := certTestAccount(t, victim.ID, "ssh")
	order := certTestOrder(t, victimAcc.ID)
	deploy := certTestDeploy(t, victim.ID, victimDeployAcc.ID, order.ID)

	c, w := certHandlerContext(t, attacker.ID, 1, "/api/cert/deploys/"+jsonUint(deploy.ID)+"/process", "")
	c.Params = gin.Params{{Key: "id", Value: jsonUint(deploy.ID)}}
	ProcessCertDeploy(c)

	if code, msg := decodeCertResp(t, w); code == 0 {
		t.Fatalf("执行他人部署不应成功，实际 code=0 msg=%q", msg)
	}
}
