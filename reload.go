package service

import (
	"github.com/api-go/log"
	"sync"
)

type reloadHook struct {
	hooks []func() error
	lock  sync.RWMutex
}

var globalReloadHook = &reloadHook{}

// OnReload 注册一个在接收到 SIGHUP 信号时触发的重新加载钩子
func OnReload(handler func() error) {
	DefaultServer.OnReload(handler)
}

func (ws *WebServer) OnReload(handler func() error) {
	globalReloadHook.lock.Lock()
	defer globalReloadHook.lock.Unlock()
	globalReloadHook.hooks = append(globalReloadHook.hooks, handler)
}

// triggerReload 触发所有注册的重新加载钩子
func (ws *WebServer) triggerReload() error {
	globalReloadHook.lock.RLock()
	hooks := make([]func() error, len(globalReloadHook.hooks))
	copy(hooks, globalReloadHook.hooks)
	globalReloadHook.lock.RUnlock()

	for _, hook := range hooks {
		if err := hook(); err != nil {
			log.DefaultLogger.Error("reload hook failed", "error", err.Error())
		}
	}
	return nil
}
