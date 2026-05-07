package service

import (
	"apigo.cc/go/id"
)

// MakeId 生成指定长度的 ID
func MakeId(size int) string {
	return id.MakeID(size)
}

// MakeIdForMysql 生成适用于 MySQL 的有序 ID
func MakeIdForMysql(size int) string {
	return id.DefaultIDMaker.GetForMysql(size)
}

// MakeIdForPostgreSQL 生成适用于 PostgreSQL 的有序 ID
func MakeIdForPostgreSQL(size int) string {
	return id.DefaultIDMaker.GetForPostgreSQL(size)
}
