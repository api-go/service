package service_test

import (
	"github.com/api-go/service"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestStaticCannotEscapeRegisteredDirectory(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "www")
	if err := os.MkdirAll(root, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "asset.txt"), []byte("public"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(parent, "private.txt"), []byte("private-marker"), 0600); err != nil {
		t.Fatal(err)
	}
	old := service.DefaultServer
	service.DefaultServer = service.NewWebServer()
	t.Cleanup(func() { service.DefaultServer = old })
	service.DefaultServer.Static("/", root)
	handler := &service.RouteHandler{}
	for _, path := range []string{"/asset.txt", "/../private.txt", "/vendor/../../private.txt", "/%2e%2e/private.txt", "/%252e%252e/private.txt"} {
		r := httptest.NewRequest("GET", path, nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if path == "/asset.txt" {
			if w.Code != 200 || w.Body.String() != "public" {
				t.Fatalf("public asset failed: %d", w.Code)
			}
		} else if w.Code == 200 || w.Body.String() == "private-marker" {
			t.Fatalf("escaped static root: %s", path)
		}
	}
}
