package service_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/api-go/service"
	"github.com/dop251/goja"
)

func TestJavaScriptMinificationPreservesNestedEmptyBranches(t *testing.T) {
	// Same branch shape as DOMPurify's attribute validator: disallowed names
	// must reach the outer else, even when the permitted branch ends in if/else.
	source := `function valid(permitted, safe, value, custom) {
		if (!permitted) { if (custom) {} else return false; }
		else if (safe) {} else if (value) return false; else {}
		return true;
	}
	[valid(false,false,"alert(1)",false), valid(true,false,"",false),
	valid(true,true,"x",false), valid(false,false,"x",true)].join(",")`
	ws := service.NewWebServer()
	ws.Config.AutoMinify = true
	ws.Config.AutoReversionCachePath = t.TempDir()
	ws.ReplaceStaticFS("*", map[string]http.FileSystem{"/": http.FS(fstest.MapFS{"validator.js": &fstest.MapFile{Data: []byte(source)}})})
	server := httptest.NewServer(ws.Handler())
	defer server.Close()
	response, err := http.Get(server.URL + "/validator.js")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != 200 {
		t.Fatalf("status %d: %s", response.StatusCode, body)
	}
	for _, code := range []string{source, strings.TrimSpace(string(body))} {
		value, err := goja.New().RunString(code)
		if err != nil || value.String() != "false,true,true,true" {
			t.Fatalf("branch semantics: %v, %v; %s", value, err, code)
		}
	}
}
