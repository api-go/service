package service

import (
	"sync"
	"sync/atomic"
	"testing"
)

type graphTestAsset struct {
	content string
	modTime int64
}

func TestAssetDependencyGraphPropagatesNestedDependencyTime(t *testing.T) {
	assets := map[string]*graphTestAsset{
		"/index.html": {content: `<link href="app.css">`, modTime: 100},
		"/app.css":    {content: `.app{background:url("theme.css")}`, modTime: 100},
		"/theme.css":  {content: `.app{color:red}`, modTime: 100},
	}
	var assetsMu sync.RWMutex
	loader := func(path string, readContent bool) ([]byte, int64, bool) {
		assetsMu.RLock()
		defer assetsMu.RUnlock()
		asset := assets[path]
		if asset == nil {
			return nil, 0, false
		}
		if !readContent {
			return nil, asset.modTime, true
		}
		return []byte(asset.content), asset.modTime, true
	}

	graph := newAssetDependencyGraph()
	graph.observe("test", "/index.html", 100, loader)
	if got := graph.logicalTime("test", "/index.html", 0); got != 100 {
		t.Fatalf("initial logical time: got %d, want 100", got)
	}

	assetsMu.Lock()
	assets["/theme.css"].modTime = 200
	assetsMu.Unlock()
	graph.scan()
	if got := graph.logicalTime("test", "/theme.css", 0); got != 200 {
		t.Fatalf("leaf logical time: got %d, want 200", got)
	}
	if got := graph.logicalTime("test", "/app.css", 0); got != 200 {
		t.Fatalf("middle logical time: got %d, want 200", got)
	}
	if got := graph.logicalTime("test", "/index.html", 0); got != 200 {
		t.Fatalf("root logical time: got %d, want 200", got)
	}

	nodes := graph.nodeMap()
	root := nodes["test\x00/index.html"]
	middle := nodes["test\x00/app.css"]
	leaf := nodes["test\x00/theme.css"]
	if len(root.Dependents) != 0 || len(root.Dependencies) != 1 {
		t.Fatalf("unexpected root edges: dependencies=%v dependents=%v", root.Dependencies, root.Dependents)
	}
	if len(middle.Dependents) != 1 || len(middle.Dependencies) != 1 {
		t.Fatalf("unexpected middle edges: dependencies=%v dependents=%v", middle.Dependencies, middle.Dependents)
	}
	if len(leaf.Dependents) != 1 || len(leaf.Dependencies) != 0 {
		t.Fatalf("unexpected leaf edges: dependencies=%v dependents=%v", leaf.Dependencies, leaf.Dependents)
	}
}

func TestAssetDependencyGraphHandlesCycles(t *testing.T) {
	assets := map[string]graphTestAsset{
		"/a.css": {content: `.a{background:url("b.css")}`, modTime: 100},
		"/b.css": {content: `.b{background:url("a.css")}`, modTime: 200},
	}
	loader := func(path string, readContent bool) ([]byte, int64, bool) {
		asset, ok := assets[path]
		if !ok {
			return nil, 0, false
		}
		if !readContent {
			return nil, asset.modTime, true
		}
		return []byte(asset.content), asset.modTime, true
	}

	graph := newAssetDependencyGraph()
	graph.observe("cycle", "/a.css", 100, loader)
	if got := graph.logicalTime("cycle", "/a.css", 0); got != 200 {
		t.Fatalf("cycle logical time: got %d, want 200", got)
	}
}

func TestAssetDependencyGraphLogicalLookupDoesNotLoadSource(t *testing.T) {
	var loads atomic.Int64
	loader := func(path string, readContent bool) ([]byte, int64, bool) {
		loads.Add(1)
		return []byte(`<script src="app.js"></script>`), 100, true
	}

	graph := newAssetDependencyGraph()
	graph.observe("lookup", "/index.html", 100, loader)
	loads.Store(0)
	for range 10000 {
		if got := graph.logicalTime("lookup", "/index.html", 0); got != 100 {
			t.Fatalf("logical time: got %d, want 100", got)
		}
	}
	if got := loads.Load(); got != 0 {
		t.Fatalf("logical lookup loaded source %d times", got)
	}
}
