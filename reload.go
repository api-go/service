package service

import (
	"apigo.cc/go/log"
	"sync"
)

var (
	reloadHooks []func() error
	reloadLock  sync.RWMutex
)

// OnReload 注册一个在接收到 SIGHUP 信号时触发的重新加载钩子
func OnReload(handler func() error) {
	reloadLock.Lock()
	defer reloadLock.Unlock()
	reloadHooks = append(reloadHooks, handler)
}

// triggerReload 触发所有注册的重新加载钩子
func triggerReload() error {
	reloadLock.RLock()
	hooks := make([]func() error, len(reloadHooks))
	copy(hooks, reloadHooks)
	reloadLock.RUnlock()

	for _, hook := range hooks {
		if err := hook(); err != nil {
			log.DefaultLogger.Error("reload hook failed", "error", err.Error())
		}
	}
	return nil
}
