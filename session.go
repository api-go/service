package service

import (
	"apigo.cc/go/cast"
	"apigo.cc/go/log"
	"apigo.cc/go/redis"
	"errors"
	"strings"
	"sync"
	"time"
)

// Session 会话对象
type Session struct {
	id            string
	conn          *redis.Redis
	data          map[string]any
	funcAuthCache map[string]bool
	lock          sync.RWMutex
}

var (
	memorySessionData     = map[string]map[string]any{}
	memorySessionDataLock = sync.RWMutex{}
	lastSessionClearTime  int64
)

// NewSession 创建或加载会话
func NewSession(id string, logger *log.Logger) *Session {
	data := map[string]any{}
	var conn *redis.Redis

	timeout := Config.SessionTimeout
	if timeout <= 0 {
		timeout = 3600
	}

	if Config.SessionRedis != "" {
		conn = redis.GetRedis(Config.SessionRedis, logger)
		err := conn.GET("SESS_" + id).To(&data)
		if err == nil {
			_ = conn.EXPIRE("SESS_"+id, timeout)
		}
	} else {
		memorySessionDataLock.RLock()
		if d, ok := memorySessionData[id]; ok && d != nil {
			for k, v := range d {
				data[k] = v
			}
		}
		memorySessionDataLock.RUnlock()
	}

	return &Session{
		id:            id,
		conn:          conn,
		data:          data,
		funcAuthCache: map[string]bool{},
	}
}

// Set 设置会话数据
func (s *Session) Set(key string, value any) {
	s.lock.Lock()
	defer s.lock.Unlock()
	s.data[key] = value
}

// Get 获取会话数据
func (s *Session) Get(key string) any {
	s.lock.RLock()
	defer s.lock.RUnlock()
	return s.data[key]
}

// Remove 移除会话数据
func (s *Session) Remove(key string) {
	s.lock.Lock()
	defer s.lock.Unlock()
	delete(s.data, key)
}

// SetAuthLevel 设置鉴权级别
func (s *Session) SetAuthLevel(level int) {
	s.Set("_authLevel", level)
}

// GetAuthLevel 获取当前鉴权级别
func (s *Session) GetAuthLevel() int {
	return cast.Int(s.Get("_authLevel"))
}

// Save 保存会话数据
func (s *Session) Save() error {
	s.lock.Lock()
	defer s.lock.Unlock()

	timeout := Config.SessionTimeout
	if timeout <= 0 {
		timeout = 3600
	}

	if s.conn == nil {
		now := time.Now().Unix()
		s.data["_time"] = now

		// 复制一份数据存储，防止外部修改
		saveData := make(map[string]any)
		for k, v := range s.data {
			saveData[k] = v
		}

		memorySessionDataLock.Lock()
		memorySessionData[s.id] = saveData

		clearTimeDiff := now - lastSessionClearTime
		if clearTimeDiff > 60 {
			lastSessionClearTime = now
		}
		memorySessionDataLock.Unlock()

		if clearTimeDiff > 60 {
			go clearMemorySession(int64(timeout))
		}
		return nil
	} else {
		if !s.conn.SETEX("SESS_"+s.id, timeout, s.data) {
			return errors.New("redis save failed")
		}
		return nil
	}
}

func clearMemorySession(timeout int64) {
	memorySessionDataLock.Lock()
	defer memorySessionDataLock.Unlock()
	now := time.Now().Unix()
	for id, data := range memorySessionData {
		if t, ok := data["_time"].(int64); ok {
			if now-t > timeout {
				delete(memorySessionData, id)
			}
		}
	}
}

// AuthFuncs 检查权限
func (s *Session) AuthFuncs(needFuncs ...string) bool {
	if len(needFuncs) == 0 {
		return true
	}

	s.lock.RLock()
	cacheKey := strings.Join(needFuncs, "; ")
	if res, ok := s.funcAuthCache[cacheKey]; ok {
		s.lock.RUnlock()
		return res
	}
	s.lock.RUnlock()

	userFuncs, _ := cast.ToSlice[string](s.Get("funcs"))
	isOk := false

	// 超级管理员判断
	for _, uf := range userFuncs {
		if uf == "system.superAdmin." || strings.HasPrefix(uf, "system.superAdmin.") {
			isOk = true
			break
		}
	}

	if !isOk && len(userFuncs) > 0 {
		requiredAuthTotal := 0
		for _, nf := range needFuncs {
			if strings.HasPrefix(nf, "&") {
				requiredAuthTotal++
			}
		}

		normalAuthOk := 0
		requiredAuthOk := 0

		for _, nf := range needFuncs {
			isRequired := false
			matchFunc := nf
			if strings.HasPrefix(nf, "&") {
				isRequired = true
				matchFunc = nf[1:]
			}

			for _, uf := range userFuncs {
				if strings.HasPrefix(uf, matchFunc) {
					if isRequired {
						requiredAuthOk++
					} else {
						normalAuthOk++
					}
					break
				}
			}

			// 如果是非必需权限命中，或者必需权限已全部命中且至少命中了一个非必需权限（如果有）
			if (normalAuthOk > 0 || requiredAuthTotal == len(needFuncs)) && requiredAuthOk == requiredAuthTotal {
				isOk = true
				break
			}
		}
	}

	s.lock.Lock()
	s.funcAuthCache[cacheKey] = isOk
	s.lock.Unlock()
	return isOk
}
