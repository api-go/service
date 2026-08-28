package service

import (
	"net/url"
	pathpkg "path"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// AssetVersionResolver returns the last-modified time for a local asset URL.
// The URL is resolved relative to sourcePath, which is the requesting document.
type AssetVersionResolver func(host, sourcePath, assetPath string) (time.Time, bool)

type assetDependencyCacheEntry struct {
	modTime time.Time
	refs    []string
}

type assetDependencyCache struct {
	mu      sync.RWMutex
	entries map[string]assetDependencyCacheEntry
}

var defaultAssetDependencyCache = assetDependencyCache{entries: map[string]assetDependencyCacheEntry{}}

// CachedAssetReferences returns references for a source document, rescanning
// only when its modification time changes.
func CachedAssetReferences(sourcePath string, modTime time.Time, content []byte) []string {
	defaultAssetDependencyCache.mu.RLock()
	entry, ok := defaultAssetDependencyCache.entries[sourcePath]
	defaultAssetDependencyCache.mu.RUnlock()
	if ok && entry.modTime.Equal(modTime) {
		return append([]string(nil), entry.refs...)
	}
	refs := collectAssetReferences(string(content))
	defaultAssetDependencyCache.mu.Lock()
	defaultAssetDependencyCache.entries[sourcePath] = assetDependencyCacheEntry{modTime: modTime, refs: refs}
	defaultAssetDependencyCache.mu.Unlock()
	return append([]string(nil), refs...)
}

func collectAssetReferences(content string) []string {
	refs := make([]string, 0)
	for _, match := range assetAttrPattern.FindAllStringSubmatch(content, -1) {
		value := match[2]
		if value == "" {
			value = match[3]
		}
		if value == "" {
			value = match[4]
		}
		refs = append(refs, value)
	}
	for _, match := range srcsetAttrPattern.FindAllStringSubmatch(content, -1) {
		for _, candidate := range strings.Split(match[2], ",") {
			if fields := strings.Fields(candidate); len(fields) > 0 {
				refs = append(refs, fields[0])
			}
		}
	}
	for _, match := range cssURLPattern.FindAllStringSubmatch(content, -1) {
		refs = append(refs, match[2])
	}
	return refs
}

var assetAttrPattern = regexp.MustCompile(`(?is)(<[a-z][^>]*?\s(?:src|href|poster|file)\s*=\s*)(?:"([^"]+)"|'([^']+)'|([^\s>]+))`)
var srcsetAttrPattern = regexp.MustCompile(`(?is)(<[a-z][^>]*?\ssrcset\s*=\s*["'])([^"']+)(["'])`)
var cssURLPattern = regexp.MustCompile(`(?is)(url\(\s*["']?)([^"')\s]+)(["']?\s*\))`)

// AssetVersion appends the source asset's second-resolution modification time.
func AssetVersion(host, sourcePath, assetPath string, resolve AssetVersionResolver) string {
	if resolve == nil || !isLocalAssetURL(assetPath) {
		return assetPath
	}
	parsed, err := url.Parse(assetPath)
	if err != nil || parsed.Path == "" || hasVersionQuery(parsed) {
		return assetPath
	}
	versionPath := resolveAssetPath(sourcePath, parsed.Path)
	modTime, ok := resolve(host, sourcePath, versionPath)
	if !ok || modTime.IsZero() {
		modTime = time.Now()
	}
	query := parsed.Query()
	query.Set("v", strconv.FormatInt(modTime.Unix(), 36))
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

// RewriteAssetReferences rewrites common HTML and CSS references in content.
func RewriteAssetReferences(host, sourcePath string, content []byte, resolve AssetVersionResolver) []byte {
	return rewriteAssetReferences(host, sourcePath, content, resolve, strings.HasSuffix(strings.ToLower(sourcePath), ".css"))
}

func rewriteAssetReferences(host, sourcePath string, content []byte, resolve AssetVersionResolver, rewriteCSS bool) []byte {
	if resolve == nil || len(content) == 0 {
		return content
	}
	result := assetAttrPattern.ReplaceAllStringFunc(string(content), func(match string) string {
		parts := assetAttrPattern.FindStringSubmatch(match)
		value := parts[2]
		quote := `"`
		if value == "" {
			value = parts[3]
			quote = `'`
		}
		if value == "" {
			value = parts[4]
			quote = ""
		}
		return parts[1] + quote + AssetVersion(host, sourcePath, value, resolve) + quote
	})
	result = srcsetAttrPattern.ReplaceAllStringFunc(result, func(match string) string {
		parts := srcsetAttrPattern.FindStringSubmatch(match)
		candidates := strings.Split(parts[2], ",")
		for i, candidate := range candidates {
			fields := strings.Fields(strings.TrimSpace(candidate))
			if len(fields) == 0 {
				continue
			}
			fields[0] = AssetVersion(host, sourcePath, fields[0], resolve)
			candidates[i] = strings.Join(fields, " ")
		}
		return parts[1] + strings.Join(candidates, ", ") + parts[3]
	})
	if rewriteCSS {
		result = cssURLPattern.ReplaceAllStringFunc(result, func(match string) string {
			parts := cssURLPattern.FindStringSubmatch(match)
			return parts[1] + AssetVersion(host, sourcePath, parts[2], resolve) + parts[3]
		})
	}
	return []byte(result)
}

// RewriteAssetReferencesCached avoids rescanning dependency-free documents.
func RewriteAssetReferencesCached(host, sourcePath string, modTime time.Time, content []byte, resolve AssetVersionResolver) []byte {
	if len(CachedAssetReferences(sourcePath, modTime, content)) == 0 {
		return content
	}
	return RewriteAssetReferences(host, sourcePath, content, resolve)
}

func isLocalAssetURL(value string) bool {
	trimmed := strings.TrimSpace(value)
	lower := strings.ToLower(trimmed)
	return trimmed != "" && !strings.HasPrefix(trimmed, "#") && !strings.HasPrefix(lower, "data:") && !strings.HasPrefix(lower, "blob:") && !strings.Contains(trimmed, "//")
}

func hasVersionQuery(parsed *url.URL) bool {
	return parsed.Query().Get("v") != ""
}

func resolveAssetPath(sourcePath, assetPath string) string {
	if strings.HasPrefix(assetPath, "/") {
		return pathpkg.Clean(assetPath)
	}
	return pathpkg.Clean(pathpkg.Join(pathpkg.Dir("/"+strings.TrimPrefix(sourcePath, "/")), assetPath))
}
