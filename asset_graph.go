package service

import (
	"net/url"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/api-go/timer"
)

const assetGraphScanInterval = 5 * time.Second

type assetGraphLoader func(path string, readContent bool) ([]byte, int64, bool)

type assetGraphNode struct {
	Key          string
	Path         string
	Dependencies []string
	Dependents   []string

	physicalTime atomic.Int64
	logicalTime  atomic.Int64
	loader       atomic.Value
	scanned      atomic.Bool
}

type assetDependencyGraph struct {
	nodes   sync.Map
	scanMu  sync.Mutex
	wheelMu sync.Mutex
	wheel   *timer.TimeWheel
	running atomic.Bool
}

func newAssetDependencyGraph() *assetDependencyGraph {
	return &assetDependencyGraph{}
}

func (g *assetDependencyGraph) start() {
	if g == nil || !g.running.CompareAndSwap(false, true) {
		return
	}
	g.wheelMu.Lock()
	g.wheel = timer.NewTimeWheel(time.Second, 8)
	wheel := g.wheel
	g.wheelMu.Unlock()
	wheel.Start()
	var schedule func()
	schedule = func() {
		if !g.running.Load() {
			return
		}
		wheel.AfterFunc(assetGraphScanInterval, func() {
			g.scan()
			schedule()
		})
	}
	schedule()
}

func (g *assetDependencyGraph) stop() {
	if g == nil || !g.running.CompareAndSwap(true, false) {
		return
	}
	g.wheelMu.Lock()
	wheel := g.wheel
	g.wheel = nil
	g.wheelMu.Unlock()
	if wheel != nil {
		wheel.Stop()
	}
}

func (g *assetDependencyGraph) observe(namespace, path string, physicalTime int64, loader assetGraphLoader) *assetGraphNode {
	if g == nil || loader == nil {
		return nil
	}
	path = resolveAssetPath("/", path)
	key := namespace + "\x00" + path
	newNode := &assetGraphNode{Key: key, Path: path}
	newNode.loader.Store(loader)
	value, loaded := g.nodes.LoadOrStore(key, newNode)
	node := value.(*assetGraphNode)
	if !loaded {
		node.physicalTime.Store(physicalTime)
		node.logicalTime.Store(physicalTime)
		g.scan()
	} else {
		node.loader.Store(loader)
	}
	return node
}

func (g *assetDependencyGraph) logicalTime(namespace, path string, fallback int64) int64 {
	if g == nil {
		return fallback
	}
	key := namespace + "\x00" + resolveAssetPath("/", path)
	if value, ok := g.nodes.Load(key); ok {
		if logicalTime := value.(*assetGraphNode).logicalTime.Load(); logicalTime > 0 {
			return logicalTime
		}
	}
	return fallback
}

func (g *assetDependencyGraph) scan() {
	if g == nil || !g.scanMu.TryLock() {
		return
	}
	defer g.scanMu.Unlock()

	nodes := g.nodeMap()
	queue := make([]*assetGraphNode, 0, len(nodes))
	for _, node := range nodes {
		queue = append(queue, node)
	}
	sort.Slice(queue, func(i, j int) bool { return queue[i].Key < queue[j].Key })
	for index := 0; index < len(queue); index++ {
		node := queue[index]
		loader, _ := node.loader.Load().(assetGraphLoader)
		if loader == nil {
			continue
		}
		readContent := isAssetGraphSource(node.Path)
		content, physicalTime, ok := loader(node.Path, readContent)
		if !ok {
			continue
		}
		previousPhysicalTime := node.physicalTime.Load()
		if physicalTime <= previousPhysicalTime && node.scanned.Load() {
			continue
		}
		node.physicalTime.Store(physicalTime)
		if physicalTime > node.logicalTime.Load() {
			node.logicalTime.Store(physicalTime)
		}
		dependencies := make([]string, 0)
		seen := map[string]bool{}
		for _, reference := range CachedAssetReferences(node.Key, time.Unix(physicalTime, 0), content) {
			dependencyPath, ok := graphDependencyPath(node.Path, reference)
			if !ok {
				continue
			}
			dependencyKey := node.Key[:strings.IndexByte(node.Key, 0)] + "\x00" + dependencyPath
			if seen[dependencyKey] {
				continue
			}
			seen[dependencyKey] = true
			dependencies = append(dependencies, dependencyKey)
			if _, exists := nodes[dependencyKey]; !exists {
				dependency := &assetGraphNode{Key: dependencyKey, Path: dependencyPath}
				dependency.loader.Store(loader)
				g.nodes.Store(dependencyKey, dependency)
				nodes[dependencyKey] = dependency
				queue = append(queue, dependency)
			}
		}
		sort.Strings(dependencies)
		node.Dependencies = dependencies
		node.scanned.Store(true)
	}

	for _, node := range nodes {
		node.Dependents = node.Dependents[:0]
	}
	for _, node := range nodes {
		for _, dependencyKey := range node.Dependencies {
			if dependency := nodes[dependencyKey]; dependency != nil {
				dependency.Dependents = append(dependency.Dependents, node.Key)
			}
		}
	}
	for _, node := range nodes {
		sort.Strings(node.Dependents)
	}

	states := make(map[string]uint8, len(nodes))
	for _, node := range nodes {
		if len(node.Dependents) == 0 {
			g.updateLogicalTime(node, nodes, states)
		}
	}
	for _, node := range nodes {
		g.updateLogicalTime(node, nodes, states)
	}
}

func isAssetGraphSource(path string) bool {
	lower := strings.ToLower(path)
	return strings.HasSuffix(lower, ".html") || strings.HasSuffix(lower, ".htm") || strings.HasSuffix(lower, ".css") || strings.HasSuffix(lower, ".js") || strings.HasSuffix(lower, ".mjs")
}

func (g *assetDependencyGraph) nodeMap() map[string]*assetGraphNode {
	nodes := map[string]*assetGraphNode{}
	g.nodes.Range(func(key, value any) bool {
		nodes[key.(string)] = value.(*assetGraphNode)
		return true
	})
	return nodes
}

func (g *assetDependencyGraph) updateLogicalTime(node *assetGraphNode, nodes map[string]*assetGraphNode, states map[string]uint8) int64 {
	if node == nil {
		return 0
	}
	if states[node.Key] == 2 {
		return node.logicalTime.Load()
	}
	if states[node.Key] == 1 {
		return node.physicalTime.Load()
	}
	states[node.Key] = 1
	logicalTime := node.physicalTime.Load()
	for _, dependencyKey := range node.Dependencies {
		if dependencyTime := g.updateLogicalTime(nodes[dependencyKey], nodes, states); dependencyTime > logicalTime {
			logicalTime = dependencyTime
		}
	}
	if logicalTime > node.logicalTime.Load() {
		node.logicalTime.Store(logicalTime)
	}
	states[node.Key] = 2
	return node.logicalTime.Load()
}

func graphDependencyPath(sourcePath, reference string) (string, bool) {
	if !isLocalAssetURL(reference) {
		return "", false
	}
	parsed, err := url.Parse(reference)
	if err != nil || parsed.Path == "" || hasVersionQuery(parsed) {
		return "", false
	}
	return resolveAssetPath(sourcePath, parsed.Path), true
}
