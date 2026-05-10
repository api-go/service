package service

import (
	"testing"
)

func TestGetDefaultName(t *testing.T) {
	// Test from build info or args
	name := GetDefaultName()
	if name == "" {
		t.Error("Expected non-empty name")
	}
	t.Logf("Detected name: %s", name)
}

func TestGetServerIp(t *testing.T) {
	ip := GetServerIp()
	if ip == "" || ip == "0.0.0.0" {
		t.Errorf("Invalid IP detected: %s", ip)
	}
	t.Logf("Detected IP: %s", ip)
}

func TestSmartStartup(t *testing.T) {
	// Reset config
	Config.Listen = ""
	Config.App = "smart-test"
	
	as := AsyncStart()
	if as.Addr == "" {
		t.Fatal("Server address should not be empty")
	}
	
	t.Logf("Server started on %s", as.Addr)
	
	if !as.useDiscover {
		t.Error("Should have enabled discover")
	}
	
	as.Stop()
}
