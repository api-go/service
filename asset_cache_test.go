package service

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBuildAssetCacheFallsBackToSourceAndReusesIt(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "a.js.min")
	mod := time.Unix(100, 0)
	input := []byte("broken source")
	calls := 0
	transform := func([]byte) ([]byte, error) { calls++; return nil, errors.New("minify failed") }
	got, err := BuildAssetCache("a.js", target, mod, input, transform)
	if err == nil || string(got) != string(input) {
		t.Fatalf("unexpected fallback: %q, %v", got, err)
	}
	got, err = BuildAssetCache("a.js", target, mod, input, transform)
	if err != nil || string(got) != string(input) || calls != 1 {
		t.Fatalf("cache was retried: %q, %v, calls=%d", got, err, calls)
	}
	if data, readErr := os.ReadFile(target); readErr != nil || string(data) != string(input) {
		t.Fatalf("bad cache: %q, %v", data, readErr)
	}
}

func TestMinifyAsset(t *testing.T) {
	got, err := minifyAsset("index.html", []byte("<html> <body> hello </body> </html>"))
	if err != nil || len(got) >= len("<html> <body> hello </body> </html>") {
		t.Fatalf("unexpected minify: %q, %v", got, err)
	}
	for _, name := range []string{"app.js", "module.mjs"} {
		input := []byte("const value = 1 + 2;\n")
		got, err = minifyAsset(name, input)
		if err != nil || len(got) >= len(input) || !strings.Contains(string(got), "const value") {
			t.Fatalf("JavaScript was not minified safely for %s: %q, %v", name, got, err)
		}
	}
}

func TestMinifyJavaScriptKeepsDeclaredNames(t *testing.T) {
	got, err := minifyAsset("bundle.js", []byte("class StableExtension {}\nconst stableFacet = new StableExtension();"))
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"StableExtension", "stableFacet"} {
		if !strings.Contains(string(got), name) {
			t.Fatalf("JavaScript minification renamed %s: %s", name, got)
		}
	}
}

func TestMinifyHTMLPreservesDeclarativeUIBindings(t *testing.T) {
	input := []byte(`<!doctype html>
<List $.state.list="users" $onclick="selectUser(item.id)">
  <template #item="item">
    <DataGrid $.state.rows="item.rows" $class="${item.active ? 'table-success' : ''}"></DataGrid>
    <AutoForm $.state.schema="item.schema" $onsubmit="saveForm(form)"></AutoForm>
  </template>
</List>
<script>
  const template = '<div data-value="${user.id}">${user.name}</div>';
</script>`)
	got, err := minifyAsset("components.html", input)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`$.state.list="users"`,
		`$onclick="selectUser(item.id)"`,
		`$.state.rows="item.rows"`,
		`${item.active ? 'table-success' : ''}`,
		`$.state.schema="item.schema"`,
		`$onsubmit="saveForm(form)"`,
		`${user.id}`,
		`${user.name}`,
	} {
		if !strings.Contains(string(got), want) {
			t.Fatalf("minified HTML lost declarative or template content %q: %s", want, got)
		}
	}
}

func TestMinifyEmbeddedHTMLKeepsOptionBoundaryBeforeTemplate(t *testing.T) {
	input := []byte("const view = Util.makeDom(/*html*/`<select><option value=\"\">Default model</option><template $each=\"models\"><option $value=\"model\"></option></template></select>`)")
	got, err := minifyAsset("picker.js", input)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), `</option><template $each="models">`) {
		t.Fatalf("option boundary changed during embedded HTML minification: %s", got)
	}
}

func TestMinifyJSCompressesMakeDomHTMLAndPreservesEscapedExpressions(t *testing.T) {
	input := []byte("const blueprint = globalThis.Util.makeDom(/*html*/`\n  <div control-wrapper $class=\"\\${this.inline ? 'd-flex align-items-center gap-2' : 'mb-3 d-flex align-items-center gap-2'}\">\n    <List></List>\n  </div>\n`); const text = `keep whitespace`;\n")
	got, err := minifyAsset("AutoForm.js", input)
	if err != nil {
		t.Fatal(err)
	}
	result := string(got)
	for _, want := range []string{`control-wrapper`, `\${this.inline ? 'd-flex align-items-center gap-2' : 'mb-3 d-flex align-items-center gap-2'}`, `<list></list>`, "text=`keep whitespace`"} {
		if !strings.Contains(result, want) {
			t.Fatalf("embedded template content was not preserved %q: %s", want, result)
		}
	}
	if strings.Contains(result, "\n  <div") {
		t.Fatalf("embedded HTML whitespace was not minified: %s", result)
	}
}

func TestMinifyJSOnlyProcessesHTMLMarkedTemplates(t *testing.T) {
	input := []byte("const html = /*html*/`\n  <div>\n    literal \\` backtick\n  </div>\n`; const text = `  keep   whitespace  `;\n")
	got, err := minifyAsset("marked.js", input)
	if err != nil {
		t.Fatal(err)
	}
	result := string(got)
	if !strings.Contains(result, "`<div>literal \\` backtick</div>`") {
		t.Fatalf("marked HTML template was not minified safely: %s", result)
	}
	if !strings.Contains(result, "text=`  keep   whitespace  `") {
		t.Fatalf("unmarked JavaScript template was changed: %s", result)
	}
}

func TestCleanupAssetCache(t *testing.T) {
	dir := t.TempDir()
	old := filepath.Join(dir, "old.js.min")
	newFile := filepath.Join(dir, "new.js.min")
	if err := os.WriteFile(old, []byte("old"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(newFile, []byte("new"), 0644); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if err := os.Chtimes(old, now.Add(-31*24*time.Hour), now.Add(-31*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := CleanupAssetCache(dir, 30*24*time.Hour, now); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatalf("old cache was not removed: %v", err)
	}
	if _, err := os.Stat(newFile); err != nil {
		t.Fatalf("new cache was removed: %v", err)
	}
}
