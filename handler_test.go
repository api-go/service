package service

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestServeHTTP(t *testing.T) {
	// 注册服务
	handler := func(in struct{ Name string }) string {
		return "Hello " + in.Name
	}
	Host("*").POST("/hello", handler).Auth(0).Memo("say hello")

	rh := &RouteHandler{}
	
	// 模拟请求
	req := httptest.NewRequest("POST", "/hello", strings.NewReader(`{"name":"Star"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	rh.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	body := w.Body.String()
	if body != "Hello Star" {
		t.Errorf("Expected 'Hello Star', got '%s'", body)
	}
}

func TestServeHTTP_404(t *testing.T) {
	rh := &RouteHandler{}
	req := httptest.NewRequest("GET", "/notfound", nil)
	w := httptest.NewRecorder()

	rh.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("Expected status 404, got %d", w.Code)
	}
}

func TestServeHTTP_VerifyFailed(t *testing.T) {
	type ValidIn struct {
		Age int `verify:"between:18-100"`
	}
	handler := func(in ValidIn) string {
		return "ok"
	}
	Host("*").POST("/verify", handler).Auth(0).Memo("test verify")

	rh := &RouteHandler{}
	req := httptest.NewRequest("POST", "/verify", strings.NewReader(`{"age":10}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	rh.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d", w.Code)
	}
}

func TestServeHTTP_Panic(t *testing.T) {
	Host("*").GET("/panic", func() string {
		panic("intentional panic")
	})

	rh := &RouteHandler{}
	req := httptest.NewRequest("GET", "/panic", nil)
	w := httptest.NewRecorder()

	rh.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("Expected status 500, got %d", w.Code)
	}
}
