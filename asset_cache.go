package service

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// CleanupAssetCache removes generated .min files older than maxAge.
func CleanupAssetCache(root string, maxAge time.Duration, now time.Time) error {
	if maxAge <= 0 {
		return nil
	}
	return filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !strings.Contains(info.Name(), ".min.") && !strings.HasSuffix(info.Name(), ".min") {
			return nil
		}
		if now.Sub(info.ModTime()) > maxAge {
			return os.Remove(path)
		}
		return nil
	})
}

var assetCacheLocks sync.Map

// BuildAssetCache creates or refreshes a derived text-resource cache. The
// source modification time is the only validity key. If transform fails, the
// original bytes are cached so the same broken source is not retried on every
// request.
func BuildAssetCache(source, target string, sourceModTime time.Time, input []byte, transform func([]byte) ([]byte, error)) ([]byte, error) {
	lockValue, _ := assetCacheLocks.LoadOrStore(target, &sync.Mutex{})
	lock := lockValue.(*sync.Mutex)
	lock.Lock()
	defer lock.Unlock()

	if info, err := os.Stat(target); err == nil && !sourceModTime.After(info.ModTime()) {
		return os.ReadFile(target)
	}

	output := input
	var transformErr error
	if transform != nil {
		if transformed, err := transform(input); err == nil {
			output = transformed
		} else {
			transformErr = err
		}
	}
	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		return input, err
	}
	tmp := target + ".tmp"
	if err := os.WriteFile(tmp, output, 0644); err != nil {
		_ = os.Remove(tmp)
		return input, err
	}
	if err := os.Rename(tmp, target); err != nil {
		_ = os.Remove(tmp)
		return input, err
	}
	return output, transformErr
}
