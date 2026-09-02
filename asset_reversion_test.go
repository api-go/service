package service_test

import (
	"strings"
	"testing"
	"time"

	"apigo.cc/go/service"
)

func TestAssetVersionAndRewrite(t *testing.T) {
	mod := time.Unix(1724412345, 0)
	resolve := func(_ string, source, asset string) (time.Time, bool) {
		if asset == "/ui/button.js" || asset == "/ui/theme.css" {
			return mod, true
		}
		return time.Time{}, false
	}
	got := string(service.RewriteAssetReferences("example.com", "/views/index.html", []byte(`<script src="../ui/button.js"></script><link href="/ui/theme.css#x"><img src="https://cdn/a.png"><View file="../ui/button.js">`), resolve))
	want := `<script src="../ui/button.js?v=sio52x"></script><link href="/ui/theme.css?v=sio52x#x"><img src="https://cdn/a.png"><View file="../ui/button.js?v=sio52x">`
	if got != want {
		t.Fatalf("rewrite mismatch:\n got %s\nwant %s", got, want)
	}
}

func TestAssetVersionPreservesExistingVersionAndExternal(t *testing.T) {
	resolve := func(string, string, string) (time.Time, bool) { return time.Unix(1, 0), true }
	for _, value := range []string{"/a.js?v=old", "data:text/css,x", "blob:x", "//cdn/a.js", "#top"} {
		if got := service.AssetVersion("example.com", "/index.html", value, resolve); got != value {
			t.Fatalf("unexpected rewrite for %q: %q", value, got)
		}
	}
}

func TestRewriteCSSAndMissingReferences(t *testing.T) {
	resolve := func(_, _, asset string) (time.Time, bool) {
		if asset == "/img/a.png" {
			return time.Unix(10, 0), true
		}
		return time.Time{}, false
	}
	input := []byte(`.a{background:url("/img/a.png")} .b{background:url('/missing.png')}`)
	got := string(service.RewriteAssetReferences("example.com", "/css/main.css", input, resolve))
	if !strings.Contains(got, `.a{background:url("/img/a.png?v=a")}`) || !strings.Contains(got, `/missing.png?v=`) {
		t.Fatalf("unexpected CSS rewrite: %s", got)
	}
}

func TestRewriteDoesNotTouchJavaScriptProperties(t *testing.T) {
	resolve := func(_, _, _ string) (time.Time, bool) { return time.Unix(10, 0), true }
	input := []byte(`const value = new URL(script.src); const href = element.href;`)
	if got := string(service.RewriteAssetReferences("example.com", "/ui/app.js", input, resolve)); got != string(input) {
		t.Fatalf("JavaScript was modified: %s", got)
	}
}

func TestRewriteSrcset(t *testing.T) {
	resolve := func(_, _, asset string) (time.Time, bool) {
		if strings.HasPrefix(asset, "/img/") {
			return time.Unix(10, 0), true
		}
		return time.Time{}, false
	}
	got := string(service.RewriteAssetReferences("example.com", "/index.html", []byte(`<img srcset="/img/a.png 1x, /img/b.png 2x">`), resolve))
	if !strings.Contains(got, "a.png?v=a 1x") || !strings.Contains(got, "b.png?v=a 2x") {
		t.Fatalf("unexpected srcset: %s", got)
	}
}

func TestExpandAssetReferencesPreservesPatternOrderAndDeduplicates(t *testing.T) {
	resolve := func(_, _, pattern string) []string {
		switch pattern {
		case "/frameworks/*.js":
			return []string{"/frameworks/Bootstrap.js", "/frameworks/State.js"}
		case "/components/**/*.js":
			return []string{"/components/List.js", "/components/Nav.js"}
		case "/components/Nav.js":
			return []string{"/components/Nav.js"}
		case "/styles/*.css":
			return []string{"/styles/app.css", "/styles/theme.css"}
		}
		return nil
	}
	input := []byte(`<script defer srcs="/frameworks/*.js, /components/**/*.js, /components/Nav.js"></script><link rel="stylesheet" hrefs="/styles/*.css">`)
	got := string(service.ExpandAssetReferences("example.com", "/admin/index.html", input, resolve))
	want := `<script defer src="/frameworks/Bootstrap.js"></script>
<script defer src="/frameworks/State.js"></script>
<script defer src="/components/List.js"></script>
<script defer src="/components/Nav.js"></script><link rel="stylesheet" href="/styles/app.css">
<link rel="stylesheet" href="/styles/theme.css">`
	if got != want {
		t.Fatalf("expand mismatch:\n got %s\nwant %s", got, want)
	}
}

func TestCachedAssetReferencesInvalidatesByModTime(t *testing.T) {
	_ = service.CachedAssetReferences("/cached.html", time.Unix(1, 0), []byte(`<script src="/a.js"></script>`))
	second := service.CachedAssetReferences("/cached.html", time.Unix(1, 0), []byte(`<script src="/b.js"></script>`))
	if len(second) != 1 || second[0] != "/a.js" {
		t.Fatalf("cache was rescanned unexpectedly: %#v", second)
	}
	third := service.CachedAssetReferences("/cached.html", time.Unix(2, 0), []byte(`<script src="/b.js"></script>`))
	if len(third) != 1 || third[0] != "/b.js" {
		t.Fatalf("cache was not invalidated: %#v", third)
	}
}

func TestAutoMinifyIsIndependentAndDisabledByDefault(t *testing.T) {
	server := service.NewWebServer()
	if server.Config.AutoMinify || server.Config.AutoReversion {
		t.Fatal("automatic text processing must be disabled by default")
	}
	server.Config.AutoMinify = true
	if !server.Config.AutoMinify || server.Config.AutoReversion {
		t.Fatal("AutoMinify must be independent from AutoReversion")
	}
}
