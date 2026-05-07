package service

import (
	"apigo.cc/go/log"
	"testing"
)

func TestServiceRegister(t *testing.T) {
	handler := func(req *Request, logger *log.Logger) string {
		return "ok"
	}

	Register(0, "/test", handler, "test service")

	webServicesLock.RLock()
	s := webServices["/test"]
	webServicesLock.RUnlock()

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

	Register(0, "/user/{id}", handler, "get user")

	webServicesLock.RLock()
	found := false
	for _, s := range regexWebServices {
		if s.path == "/user/{id}" {
			found = true
			if len(s.pathArgs) != 1 || s.pathArgs[0] != "id" {
				t.Errorf("pathArgs mismatch: %v", s.pathArgs)
			}
			break
		}
	}
	webServicesLock.RUnlock()

	if !found {
		t.Fatal("Regex service not registered")
	}
}
