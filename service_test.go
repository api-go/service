package service

import (
	"apigo.cc/go/log"
	"testing"
)

func TestServiceRegister(t *testing.T) {
	handler := func(req *Request, logger *log.Logger) string {
		return "ok"
	}

	Host("*").Register("*", "/test", handler).Auth(0).Memo("test service")

	DefaultServer.webServicesLock.RLock()
	s := DefaultServer.webServices["*"]["*/test"]
	DefaultServer.webServicesLock.RUnlock()

	if s == nil {
		t.Fatal("Service not registered")
	}

	if s.requestIndex != 0 {
		t.Errorf("requestIndex mismatch: expected 0, got %d", s.requestIndex)
	}
	if s.loggerIndex != 1 {
		t.Errorf("loggerIndex mismatch: expected 1, got %d", s.loggerIndex)
	}
}

func TestRegexServiceRegister(t *testing.T) {
	handler := func(args map[string]any) string {
		return "ok"
	}

	Host("*").Register("*", "/user/{id}", handler).Auth(0).Memo("get user")

	DefaultServer.webServicesLock.RLock()
	found := false
	for _, services := range DefaultServer.regexWebServices {
		for _, s := range services {
			if s.path == "/user/{id}" {
				found = true
				if len(s.pathArgs) != 1 || s.pathArgs[0] != "id" {
					t.Errorf("pathArgs mismatch: %v", s.pathArgs)
				}
				break
			}
		}
		if found {
			break
		}
	}
	DefaultServer.webServicesLock.RUnlock()

	if !found {
		t.Fatal("Regex service not registered")
	}
}
