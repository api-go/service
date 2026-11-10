package service_test

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/api-go/crypto"
	"github.com/api-go/encoding"
	"github.com/api-go/file"
	"github.com/api-go/service"
	"github.com/dop251/goja"
)

// Execute both sides of the HTTP boundary. Size assertions alone cannot detect
// a compressor that produces valid JavaScript with different security semantics.
func TestJavaScriptMinificationSemanticMatrix(t *testing.T) {
	cases := []struct{ name, source, want string }{
		{"branches", `function allowed(a,b,c) { if(a) { if(b) {} else return false; } else if(c) {} else return false; return true; } var out=[]; for(var a of [false,true]) for(var b of [false,true]) for(var c of [false,true]) out.push(allowed(a,b,c)); out.join(',')`, "false,true,false,true,false,false,true,true"},
		{"asi-regexp", "function f() { return\n{unsafe:true}; }\nvar x=1; x\n++x; [String(f()),x,/a\\/b/.test('a/b')].join(',')", "undefined,2,true"},
		{"closures-finally", `var out=[]; for(let i=0;i<3;i++) out.push(()=>i); function f(){try{return 7}finally{out.push(()=>9)}} var n=f(); [n,...out.map(f=>f())].join(',')`, "7,0,1,2,9"},
		{"modern-unicode", `class Stable { value = '中文'; get(){return this?.value ?? 'bad'} } const stable = new Stable(); [Stable.name,stable.get(),Object.is(-0,0),String(12345678901234567890n)].join(',')`, "Stable,中文,false,12345678901234567890"},
		{"quoted-html-marker", "const text = '/*html*/`<div>  keep   whitespace </div>`'; text", "/*html*/`<div>  keep   whitespace </div>`"},
		{"nested-html", "const view = /*html*/`<pre>${`  keep   whitespace  `}</pre>`; view", "<pre>  keep   whitespace  </pre>"},
		{"interpolated-html", "const value = '  keep   whitespace  '; const view = /*html*/`<pre>${value}</pre>`; view", "<pre>  keep   whitespace  </pre>"},
		{"tagged-html", "function raw(parts){return parts.raw[0]} raw /*html*/`<div>  keep   whitespace </div>`", "<div>  keep   whitespace </div>"},
		{"template", "const raw = `  keep   whitespace  `; const nested = `a${`b${2}`}c`; [raw,nested].join(',')", "  keep   whitespace  ,ab2c"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			value, err := goja.New().RunString(tc.source)
			if err != nil || value.String() != tc.want {
				t.Fatalf("source: %v, %v", value, err)
			}
			for _, ext := range []string{".js", ".mjs"} {
				t.Run(ext, func(t *testing.T) {
					ws := service.NewWebServer()
					ws.Config.AutoMinify = true
					ws.Config.AutoReversionCachePath = t.TempDir()
					ws.ReplaceStaticFS("*", map[string]http.FileSystem{"/": http.FS(fstest.MapFS{"app" + ext: {Data: []byte("// removable padding\n" + tc.source)}})})
					server := httptest.NewServer(ws.Handler())
					defer server.Close()
					for i := 0; i < 2; i++ {
						body := readMinifiedResponse(t, server.URL+"/app"+ext)
						if strings.Contains(body, "removable padding") {
							t.Fatal("compression path was bypassed")
						}
						value, err := goja.New().RunString(body)
						if err != nil || value.String() != tc.want {
							t.Fatalf("served pass %d: %v, %v; %s", i, value, err, body)
						}
					}
				})
			}
		})
	}
}

func readMinifiedResponse(t *testing.T, url string) string {
	t.Helper()
	response, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil || response.StatusCode != http.StatusOK {
		t.Fatalf("HTTP %d: %s (%v)", response.StatusCode, body, err)
	}
	return string(body)
}

func TestJavaScriptMinificationCacheLifecycle(t *testing.T) {
	for _, physical := range []bool{false, true} {
		t.Run(fmt.Sprint("physical=", physical), func(t *testing.T) {
			cache := t.TempDir()
			root := t.TempDir()
			makeServer := func() *service.WebServer {
				ws := service.NewWebServer()
				ws.Config.AutoMinify = true
				ws.Config.AutoReversionCachePath = cache
				return ws
			}
			ws := makeServer()
			mount := func(ws *service.WebServer, source string) {
				if physical {
					if err := file.Write(filepath.Join(root, "app.js"), source); err != nil {
						t.Fatal(err)
					}
					ws.Static("/", root)
				} else {
					ws.ReplaceStaticFS("*", map[string]http.FileSystem{"/": http.FS(fstest.MapFS{"app.js": {Data: []byte(source), ModTime: time.Unix(100, 0)}})})
				}
			}
			source := "// compress me\nfunction publicName() { return 42; } publicName();"
			mount(ws, source)
			server := httptest.NewServer(ws.Handler())
			defer server.Close()
			// Cold concurrent requests must all receive a complete executable cache entry.
			var wg sync.WaitGroup
			for i := 0; i < 12; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					body := readMinifiedResponse(t, server.URL+"/app.js")
					value, err := goja.New().RunString(body)
					if err != nil || value.ToInteger() != 42 {
						t.Errorf("concurrent result: %v, %v", value, err)
					}
					if strings.Contains(body, "compress me") {
						t.Error("not compressed")
					}
				}()
			}
			wg.Wait()
			entries, err := filepath.Glob(filepath.Join(cache, "*", "**"))
			if err != nil || len(entries) == 0 {
				t.Fatal("no persistent cache", err)
			}
			// A new server shares disk cache; changed bytes must invalidate it even when
			// the source timestamp remains identical (StaticFS mount replacement).
			next := makeServer()
			mount(next, source)
			restarted := httptest.NewServer(next.Handler())
			defer restarted.Close()
			if body := readMinifiedResponse(t, restarted.URL+"/app.js"); strings.Contains(body, "compress me") {
				t.Fatal("restart bypassed compression")
			}
			mount(next, "// changed\nfunction publicName() { return 99; } publicName();")
			body := readMinifiedResponse(t, restarted.URL+"/app.js")
			value, err := goja.New().RunString(body)
			if err != nil || value.ToInteger() != 99 {
				t.Fatalf("stale cache: %v, %v", value, err)
			}
			// Parse failures preserve the original bytes, including on repeated requests;
			// replacing the bad source must recover instead of reusing fallback bytes.
			invalid := "function broken( { // invalid JavaScript"
			mount(next, invalid)
			for i := 0; i < 2; i++ {
				if body := readMinifiedResponse(t, restarted.URL+"/app.js"); body != invalid {
					t.Fatalf("fallback changed source: %q", body)
				}
			}
			mount(next, source)
			body = readMinifiedResponse(t, restarted.URL+"/app.js")
			value, err = goja.New().RunString(body)
			if err != nil || value.ToInteger() != 42 {
				t.Fatalf("recovery: %v, %v", value, err)
			}
		})
	}
}

func TestJavaScriptMinificationInlineScript(t *testing.T) {
	source := `function valid(permitted,safe,value,custom) {
 if(!permitted) { if(custom) {} else return false; }
 else if(safe) {} else if(value) return false; else {}
 return true;
 }
 [valid(false,false,'alert(1)',false),valid(true,false,'',false)].join(',')`
	for _, scriptType := range []string{"", ` type="text/javascript"`, ` type="module"`} {
		t.Run(scriptType, func(t *testing.T) {
			ws := service.NewWebServer()
			ws.Config.AutoMinify = true
			ws.Config.AutoReversionCachePath = t.TempDir()
			ws.ReplaceStaticFS("*", map[string]http.FileSystem{"/": http.FS(fstest.MapFS{"index.html": {Data: []byte("<script" + scriptType + ">// remove me\n" + source + "</script>")}})})
			server := httptest.NewServer(ws.Handler())
			defer server.Close()
			body := readMinifiedResponse(t, server.URL+"/index.html")
			start := strings.Index(body, ">")
			end := strings.Index(body, "</script>")
			if start < 0 || end < start {
				t.Fatalf("missing script: %s", body)
			}
			script := body[start+1 : end]
			if strings.Contains(script, "remove me") {
				t.Fatal("inline script not compressed")
			}
			value, err := goja.New().RunString(script)
			if err != nil || value.String() != "false,true" {
				t.Fatalf("inline script: %v, %v; %s", value, err, script)
			}
		})
	}
}

// Seed the exact pre-fix cache key with executable but incorrect output. A
// compressor upgrade must recover without deleting a shared cache directory.
func TestJavaScriptMinificationIgnoresPreviousCompressorCache(t *testing.T) {
	cache := t.TempDir()
	source := []byte("// compress\nfunction safe() { return true; } safe();")
	namespace := "fs:minify.localhost:/"
	oldHash := crypto.SHA256([]byte(namespace+"\x00app.js\x00"), source)
	oldPath := filepath.Join(cache, encoding.Hex(oldHash), "app.js.min")
	if err := file.Write(oldPath, "false;"); err != nil {
		t.Fatal(err)
	}
	ws := service.NewWebServer()
	ws.Config.AutoMinify = true
	ws.Config.AutoReversionCachePath = cache
	ws.ReplaceStaticFS("*", map[string]http.FileSystem{"/": http.FS(fstest.MapFS{"app.js": {Data: source, ModTime: time.Unix(100, 0)}})})
	server := httptest.NewServer(ws.Handler())
	defer server.Close()
	req, err := http.NewRequest(http.MethodGet, server.URL+"/app.js", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Host = "minify.localhost"
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("response: %d, %v", resp.StatusCode, err)
	}
	value, err := goja.New().RunString(string(body))
	if err != nil || !value.ToBoolean() {
		t.Fatalf("old compressor cache reused: %v, %v; %s", value, err, body)
	}
	if strings.Contains(string(body), "// compress") {
		t.Fatal("compression bypassed")
	}
}

func TestJavaScriptMinificationCacheWriteFailure(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "not-a-directory")
	if err := file.Write(blocker, "block"); err != nil {
		t.Fatal(err)
	}
	ws := service.NewWebServer()
	ws.Config.AutoMinify = true
	ws.Config.AutoReversionCachePath = blocker
	source := "// valid source must survive unavailable cache\nfunction safe() { return true; } safe();"
	ws.ReplaceStaticFS("*", map[string]http.FileSystem{"/": http.FS(fstest.MapFS{"app.js": {Data: []byte(source)}})})
	server := httptest.NewServer(ws.Handler())
	defer server.Close()
	for i := 0; i < 2; i++ {
		if body := readMinifiedResponse(t, server.URL+"/app.js"); body != source {
			t.Fatalf("cache failure did not preserve source: %q", body)
		}
	}
}
