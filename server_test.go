package service

import (
	"net/http"
	"testing"
)

func TestAsyncServer(t *testing.T) {
	Config.Listen = ":0" // 随机端口
	as := AsyncStart()
	if as.Addr == "" {
		t.Fatal("AsyncStart failed to get address")
	}

	// 测试服务是否可用
	resp, err := http.Get("http://" + as.Addr + "/__CHECK__")
	if err == nil {
		// 虽然没有注册 /__CHECK__，但应该返回 404 而非连接拒绝
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("Expected 404, got %d", resp.StatusCode)
		}
	}

	as.Stop()

	// 确认服务已关闭
	_, err = http.Get("http://" + as.Addr + "/__CHECK__")
	if err == nil {
		t.Error("Server should be closed")
	}
}
