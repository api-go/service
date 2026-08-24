package service

import (
	"errors"
	"os"
	"path/filepath"
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
