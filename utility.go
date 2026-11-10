package service

import (
	"github.com/api-go/id"
	"net"
	"os"
	"path"
	"runtime/debug"
	"strings"
)

// IDMaker 全局 ID 生成器，默认指向单机版，启动后若配置了 IdServer 会被替换为 Redis 版
var IDMaker = id.DefaultIDMaker

// GetDefaultName 获取默认应用名称
func GetDefaultName() string {
	name := ""
	if info, ok := debug.ReadBuildInfo(); ok && info.Path != "" && info.Path != "command-line-arguments" {
		name = path.Base(info.Path)
	}
	if name == "" {
		name = path.Base(os.Args[0])
	}
	// 处理 Windows 下的 .exe 后缀
	name = strings.TrimSuffix(name, ".exe")
	return name
}

// GetServerIp 获取真实局域网 IP (UDP 8.8.8.8 伪拨号法)
func GetServerIp() string {
	if ip := os.Getenv("SERVICE_IP"); ip != "" {
		return ip
	}
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err == nil {
		localAddr := conn.LocalAddr().(*net.UDPAddr)
		_ = conn.Close()
		return localAddr.IP.String()
	}

	addrs, err := net.InterfaceAddrs()
	if err == nil {
		for _, a := range addrs {
			if an, ok := a.(*net.IPNet); ok {
				if an.IP.IsGlobalUnicast() {
					return an.IP.To4().String()
				}
			}
		}
	}
	return "127.0.0.1"
}
