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
