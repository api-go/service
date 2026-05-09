package service

import (
	"apigo.cc/go/id"
	"apigo.cc/go/log"
	"apigo.cc/go/redis"
	"net"
	"os"
	"path"
	"runtime/debug"
	"sync"
)

var (
	idMaker     IDMakerInterface
	idMakerLock sync.Mutex
)

// GetDefaultName 获取默认应用名称
func GetDefaultName() string {
	name := os.Getenv("DISCOVER_APP")
	if name == "" {
		name = os.Getenv("discover_app")
	}
	if name == "" {
		if info, ok := debug.ReadBuildInfo(); ok && info.Path != "" && info.Path != "command-line-arguments" {
			name = path.Base(info.Path)
		}
	}
	if name == "" {
		name = path.Base(os.Args[0])
	}
	return name
}

// GetServerIp 获取真实局域网 IP (UDP 8.8.8.8 伪拨号法)
func GetServerIp() string {
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

// IDMakerInterface ID 生成器接口
type IDMakerInterface interface {
	Get(size int) string
	GetForMysql(size int) string
	GetForPostgreSQL(size int) string
}

func getIDMaker() IDMakerInterface {
	if idMaker != nil {
		return idMaker
	}

	idMakerLock.Lock()
	defer idMakerLock.Unlock()

	if idMaker != nil {
		return idMaker
	}

	if Config.IdServer != "" {
		rd := redis.GetRedis(Config.IdServer, log.DefaultLogger)
		if rd.Error == nil {
			idMaker = redis.NewIDMaker(rd)
		}
	}

	if idMaker == nil {
		idMaker = id.DefaultIDMaker
	}

	return idMaker
}

// MakeId 生成指定长度的 ID
func MakeId(size int) string {
	return getIDMaker().Get(size)
}

// MakeIdForMysql 生成适用于 MySQL 的有序 ID
func MakeIdForMysql(size int) string {
	return getIDMaker().GetForMysql(size)
}

// MakeIdForPostgreSQL 生成适用于 PostgreSQL 的有序 ID
func MakeIdForPostgreSQL(size int) string {
	return getIDMaker().GetForPostgreSQL(size)
}
