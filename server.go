package service

import (
	"apigo.cc/go/log"
	"context"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

// AsyncServer 异步服务实例
type AsyncServer struct {
	server    *http.Server
	listener  net.Listener
	Addr      string
	stopChan  chan os.Signal
	startChan chan bool
}

// AsyncStart 异步启动服务
func AsyncStart() *AsyncServer {
	as := &AsyncServer{
		startChan: make(chan bool, 1),
		stopChan:  make(chan os.Signal, 1),
	}

	go as.start()

	<-as.startChan
	return as
}

func (as *AsyncServer) start() {
	if Config.Listen == "" {
		Config.Listen = ":8080" // 默认端口
	}

	listener, err := net.Listen("tcp", Config.Listen)
	if err != nil {
		log.DefaultLogger.Error("failed to listen", "addr", Config.Listen, "error", err.Error())
		as.startChan <- false
		return
	}

	as.listener = listener
	as.Addr = listener.Addr().String()
	serverAddr = as.Addr

	as.server = &http.Server{
		Handler: &RouteHandler{},
	}

	signal.Notify(as.stopChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		log.DefaultLogger.Info("service starting", "addr", as.Addr)
		as.startChan <- true
		if err := as.server.Serve(listener); err != nil && err != http.ErrServerClosed {
			log.DefaultLogger.Error("server error", "error", err.Error())
		}
	}()
}

// Stop 停止服务
func (as *AsyncServer) Stop() {
	log.DefaultLogger.Info("service stopping")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := as.server.Shutdown(ctx); err != nil {
		log.DefaultLogger.Error("server shutdown error", "error", err.Error())
	}
	log.DefaultLogger.Info("service stopped")
}

// Wait 等待服务结束 (信号监听)
func (as *AsyncServer) Wait() {
	<-as.stopChan
	as.Stop()
}

// Start 同步启动服务
func Start() {
	AsyncStart().Wait()
}
