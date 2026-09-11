package service

import (
	"net/http"
	"strings"
	"testing"
)

func TestSensitiveRequestHeadersAreExcludedByDefault(t *testing.T) {
	server := &WebServer{}
	headers := http.Header{
		"Authorization":       []string{"Bearer secret"},
		"Proxy-Authorization": []string{"Basic secret"},
		"Cookie":              []string{"session=secret"},
		"X-Business":          []string{"visible"},
	}
	logged := sanitizeLogHeaders(headers, server.effectiveNoLogRequestHeaders())
	for _, name := range []string{"Authorization", "Proxy-Authorization", "Cookie"} {
		if _, ok := logged[name]; ok {
			t.Fatalf("%s leaked into request log", name)
		}
	}
	if logged["X-Business"] != "visible" {
		t.Fatalf("ordinary business header was removed: %#v", logged)
	}
	for _, name := range []string{"Authorization", "Proxy-Authorization", "Cookie"} {
		if !strings.Contains(server.effectiveNoLogRequestHeaders(), name) {
			t.Fatalf("default exclusion is missing %s", name)
		}
	}
}
