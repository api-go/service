package service

import (
	"apigo.cc/go/log"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSessionLogic(t *testing.T) {
	SetClientKeys("", "", "sessid")
	Config.SessionTimeout = 3600

	// 1. 测试 Session 数据存取
	sess := NewSession("test_id", nil)
	sess.Set("key1", "value1")
	if sess.Get("key1") != "value1" {
		t.Errorf("Expected value1, got %v", sess.Get("key1"))
	}

	if err := sess.Save(); err != nil {
		t.Errorf("Save failed: %v", err)
	}

	sess2 := NewSession("test_id", nil)
	if sess2.Get("key1") != "value1" {
		t.Errorf("Expected value1 in new session instance, got %v", sess2.Get("key1"))
	}

	// 1.1 测试 Save 批量设置
	sess3 := NewSession("test_batch", nil)
	m := map[string]any{"a": 1, "b": "hello", "c": true}
	if err := sess3.Save(m); err != nil {
		t.Errorf("Save with map failed: %v", err)
	}
	sess4 := NewSession("test_batch", nil)
	if sess4.Get("a") != 1 {
		t.Errorf("Expected a=1, got %v", sess4.Get("a"))
	}
	if sess4.Get("b") != "hello" {
		t.Errorf("Expected b=hello, got %v", sess4.Get("b"))
	}
	if sess4.Get("c") != true {
		t.Errorf("Expected c=true, got %v", sess4.Get("c"))
	}

	// 1.2 测试 Save 无参数仍然正常工作
	sess5 := NewSession("test_noarg", nil)
	sess5.Set("x", "y")
	if err := sess5.Save(); err != nil {
		t.Errorf("Save without args failed: %v", err)
	}
	sess6 := NewSession("test_noarg", nil)
	if sess6.Get("x") != "y" {
		t.Errorf("Expected x=y, got %v", sess6.Get("x"))
	}

	// 1.3 测试 Load 批量读取
	sess7 := NewSession("test_load", nil)
	sess7.Set("k1", "v1")
	sess7.Set("k2", "v2")
	sess7.Set("k3", "v3")
	_ = sess7.Save()

	sess8 := NewSession("test_load", nil)
	result := sess8.Load([]string{"k1", "k3"})
	if len(result) != 2 {
		t.Errorf("Expected 2 keys, got %d", len(result))
	}
	if result["k1"] != "v1" {
		t.Errorf("Expected k1=v1, got %v", result["k1"])
	}
	if result["k3"] != "v3" {
		t.Errorf("Expected k3=v3, got %v", result["k3"])
	}
	if _, ok := result["k2"]; ok {
		t.Error("k2 should not be in partial Load result")
	}

	// 1.4 测试 Load 空参数返回全部数据
	allData := sess8.Load(nil)
	if len(allData) < 3 {
		t.Errorf("Expected at least 3 keys in full Load, got %d", len(allData))
	}

	// 1.5 测试 Remove 多 key
	sess9 := NewSession("test_remove", nil)
	sess9.Set("a", 1)
	sess9.Set("b", 2)
	sess9.Set("c", 3)
	sess9.Remove("a", "c")
	_ = sess9.Save()

	sess10 := NewSession("test_remove", nil)
	if sess10.Get("a") != nil {
		t.Errorf("Expected a removed, got %v", sess10.Get("a"))
	}
	if sess10.Get("b") != 2 {
		t.Errorf("Expected b=2, got %v", sess10.Get("b"))
	}
	if sess10.Get("c") != nil {
		t.Errorf("Expected c removed, got %v", sess10.Get("c"))
	}

	// 1.6 测试 Remove 无参数（安全无操作）
	sess9.Remove()
	_ = sess9.Save()
	sess11 := NewSession("test_remove", nil)
	if sess11.Get("b") != 2 {
		t.Errorf("Expected b=2 after no-arg Remove, got %v", sess11.Get("b"))
	}

	// 2. 测试 AuthFuncs 逻辑
	sess.Set("funcs", []string{"user.read", "user.write", "system.admin"})

	if !sess.AuthFuncs("user.read") {
		t.Error("Expected true for user.read")
	}
	if !sess.AuthFuncs("user.read", "user.write") {
		t.Error("Expected true for user.read and user.write")
	}
	if sess.AuthFuncs("user.delete") {
		t.Error("Expected false for user.delete")
	}

	// 测试必需权限 &
	if sess.AuthFuncs("&user.read", "other") {
		t.Error("Expected false for &user.read when other is missing")
	}
	if !sess.AuthFuncs("&user.read") {
		t.Error("Expected true for &user.read")
	}

	if sess.AuthFuncs("&user.delete", "user.read") {
		t.Error("Expected false for &user.delete even if user.read exists")
	}

	// 测试超级管理员
	sess.Set("funcs", []string{"system.superAdmin.all"})
	if !sess.AuthFuncs("any.thing") {
		t.Error("Expected true for superAdmin")
	}
}

func TestSessionInjection(t *testing.T) {
	SetClientKeys("", "", "sessid")

	handler := func(s *Session) string {
		if s == nil {
			return "no session"
		}
		s.Set("name", "star")
		_ = s.Save()
		return "ok"
	}
	Host("*").GET("/test-session", handler)

	rh := &RouteHandler{ws: DefaultServer}
	req := httptest.NewRequest("GET", "/test-session", nil)
	req.Header.Set("sessid", "sess_123")
	w := httptest.NewRecorder()

	rh.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d", w.Code)
	}
	if w.Body.String() != "ok" {
		t.Errorf("Expected ok, got %s", w.Body.String())
	}

	// 验证 Session 是否真的保存了
	sess := NewSession("sess_123", nil)
	if sess.Get("name") != "star" {
		t.Errorf("Expected star, got %v", sess.Get("name"))
	}
}

type CustomAuth struct {
	User string
}

func TestCustomAuthInjection(t *testing.T) {
	AddAuthChecker([]int{10}, func(authLevel int, logger *log.Logger, url *string, in map[string]any, request *Request, response *Response, options *WebServiceOptions) (pass bool, object any) {
		return true, &CustomAuth{User: "custom_user"}
	})

	handler := func(auth *CustomAuth) string {
		if auth == nil {
			return "no auth"
		}
		return auth.User
	}
	Host("*").GET("/test-auth", handler).Auth(10)

	rh := &RouteHandler{ws: DefaultServer}
	req := httptest.NewRequest("GET", "/test-auth", nil)
	w := httptest.NewRecorder()

	rh.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d", w.Code)
	}
	if w.Body.String() != "custom_user" {
		t.Errorf("Expected custom_user, got %s", w.Body.String())
	}
}

func TestAutomaticAuthLevelCheck(t *testing.T) {
	SetClientKeys("", "", "sessid")

	handler := func() string {
		return "ok"
	}
	Host("*").GET("/test-auto-auth", handler).Auth(1)

	rh := &RouteHandler{ws: DefaultServer}

	// 1. 无 Session 或 AuthLevel=0 时应失败
	req1 := httptest.NewRequest("GET", "/test-auto-auth", nil)
	req1.Header.Set("sessid", "sess_auto_1")
	w1 := httptest.NewRecorder()
	rh.ServeHTTP(w1, req1)
	if w1.Code != http.StatusForbidden {
		t.Errorf("Expected 403, got %d", w1.Code)
	}

	// 2. 设置 Session AuthLevel=1 后应成功
	sess := NewSession("sess_auto_2", nil)
	sess.SetAuthLevel(1)
	_ = sess.Save()

	req2 := httptest.NewRequest("GET", "/test-auto-auth", nil)
	req2.Header.Set("sessid", "sess_auto_2")
	w2 := httptest.NewRecorder()
	rh.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d", w2.Code)
	}
}
