package service

import (
	"apigo.cc/go/log"
)

type RequestLog struct {
	log.BaseLog
	ServerId           string            `log:"pos:6,hide:true"`
	App                string            `log:"pos:7,color:cyan,keyname:App"`
	Node               string            `log:"pos:8,color:gray,attachBefore:true"`
	FromApp            string            `log:"pos:9,color:cyan,keyname:From"`
	FromNode           string            `log:"pos:10,color:gray,attachBefore:true"`
	ClientIp           string            `log:"pos:11,withoutkey:true"`
	ClientAppName      string            `log:"pos:12,attachBefore:true,keyname:Client"`
	ClientAppVersion   string            `log:"pos:13,attachBefore:true"`
	UserId             string            `log:"pos:14,color:magenta,keyname:User"`
	DeviceId           string            `log:"pos:15,color:gray,keyname:Device"`
	SessionId          string            `log:"pos:16,keyname:Session"`
	Host               string            `log:"pos:17,color:gray,withoutkey:true"`
	Method             string            `log:"pos:18,color:gray,withoutkey:true"`
	Path               string            `log:"pos:19,color:cyan,withoutkey:true"`
	Scheme             string            `log:"pos:20,color:gray,withoutkey:true"`
	Proto              string            `log:"pos:21,color:gray,withoutkey:true"`
	AuthLevel          int               `log:"pos:22,color:green"`
	Priority           int               `log:"pos:23,hide:true"`
	RequestData        any               `log:"pos:24,color:cyan,keyname:Request"`
	RequestHeaders     map[string]string `log:"pos:25,color:cyan,keyname:Headers"`
	UsedTime           float32           `log:"pos:26,color:green,precision:6"`
	ResponseCode       int               `log:"pos:27,color:magenta,keyname:Status"`
	ResponseDataLength uint              `log:"pos:28,color:magenta,keyname:ContentLength"`
	ResponseData       any               `log:"pos:29,color:magenta,keyname:Response"`
	ResponseHeaders    map[string]string `log:"pos:30,color:magenta,keyname:Headers"`
}

func (l *RequestLog) Reset() {
	l.BaseLog.Reset()
	l.ServerId = ""
	l.App = ""
	l.Node = ""
	l.ClientIp = ""
	l.FromApp = ""
	l.FromNode = ""
	l.UserId = ""
	l.DeviceId = ""
	l.ClientAppName = ""
	l.ClientAppVersion = ""
	l.SessionId = ""
	l.Host = ""
	l.Scheme = ""
	l.Proto = ""
	l.AuthLevel = 0
	l.Priority = 0
	l.Method = ""
	l.Path = ""
	if l.RequestHeaders == nil {
		l.RequestHeaders = make(map[string]string, 8)
	} else {
		clear(l.RequestHeaders)
	}
	l.RequestData = nil
	l.UsedTime = 0
	l.ResponseCode = 0
	if l.ResponseHeaders == nil {
		l.ResponseHeaders = make(map[string]string, 8)
	} else {
		clear(l.ResponseHeaders)
	}
	l.ResponseDataLength = 0
	l.ResponseData = nil
}

// LogRequest 闭环式日志调用封装
func LogRequest(logger *log.Logger, fn func(entry *RequestLog)) {
	if !logger.CheckLevel(log.INFO) {
		return
	}

	entry := log.GetEntry[RequestLog]()
	logger.FillBase(entry.GetBaseLog(), log.LogTypeRequest)

	// 执行业务赋值逻辑
	fn(entry)

	// 统一发送
	logger.Log(entry)
}

func init() {
	log.RegisterType(log.LogTypeRequest, &RequestLog{})
}
