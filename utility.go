package service

import (
	"apigo.cc/go/id"
	"apigo.cc/go/log"
	"apigo.cc/go/redis"
	"sync"
)

var (
	idMaker     IDMakerInterface
	idMakerLock sync.Mutex
)

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
