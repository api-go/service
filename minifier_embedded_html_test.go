package service_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"apigo.cc/go/service"
	"github.com/dop251/goja"
)

func TestJavaScriptMinificationEmbeddedHTMLSafety(t *testing.T) {
	cases := []struct{ name, html, check string }{
		{"compress-indentation", "\n  <div>\n    <span>ok</span>\n  </div>\n", "!view.includes('\\n')"},
		{"html-comment", "<div><!-- component anchor --><span>ok</span></div>", "view.includes('<!-- component anchor -->')"},
		{"entity-interpolation", "<div>&#36;{hits++}</div>", "hits === 0"},
		{"entity-backslash", "<div>&#92;n</div>", "!view.includes('\\n')"},
		{"entity-backtick", "<div>&#96;;hits++;//&#96;</div>", "hits === 0"},
		{"escaped-expression", "<div $class=\"\\${this.active ? 'on' : 'off'}\">ok</div>", "view.includes('${this.active')"},
		{"escaped-backticks", "<div>literal \\` backtick and \\\\ slash</div>", "view.includes('`') && view.includes('\\\\ slash')"},
		{"pre", "<pre>  first\n    second  </pre>", "view.includes('  first\\n    second  ')"},
		{"textarea", "<textarea>  first\n    second  </textarea>", "view.includes('  first\\n    second  ')"},
		{"inline-gap", "<span>Hello</span> <span>world</span>", "view.includes('</span> <span>')"},
		{"quoted-attribute", "<div title=\"  keep   spaces  \" $.state.value=\"model\" $onclick=\"save()\"></div>", "view.includes('  keep   spaces  ') && view.includes('$.state.value=\"model\"') && view.includes('$onclick=\"save()\"')"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			source := "// compression must still run\nvar hits=0; const view=/*html*/`" + tc.html + "`; (" + tc.check + ");"
			sourceCheck := source
			if tc.name == "compress-indentation" {
				sourceCheck = "const view = `" + tc.html + "`; view.includes('\\n')"
			}
			original, err := goja.New().RunString(sourceCheck)
			if err != nil || !original.ToBoolean() {
				t.Fatalf("invalid source fixture: %v, %v", original, err)
			}
			ws := service.NewWebServer()
			ws.Config.AutoMinify = true
			ws.Config.AutoReversionCachePath = t.TempDir()
			ws.ReplaceStaticFS("*", map[string]http.FileSystem{"/": http.FS(fstest.MapFS{"app.js": {Data: []byte(source)}})})
			server := httptest.NewServer(ws.Handler())
			defer server.Close()
			for i := 0; i < 2; i++ {
				body := readMinifiedResponse(t, server.URL+"/app.js")
				if strings.Contains(body, "compression must still run") {
					t.Fatal("JavaScript compression was bypassed")
				}
				value, err := goja.New().RunString(body)
				if err != nil || !value.ToBoolean() {
					t.Fatalf("template semantics changed: %v, %v; %s", value, err, body)
				}
			}
		})
	}
}
