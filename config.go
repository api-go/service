package service

import (
	"apigo.cc/go/cast"
	"path/filepath"
)

// CertSet SSL 证书配置
type CertSet struct {
	CertFile string
	KeyFile  string
}

// CallConfig 下游服务调用配置
type CallConfig struct {
	Timeout int    // 超时时间 (ms)
	Token   string // 访问凭证
	Http2   bool   // 是否强制使用 HTTP/2 (H2C)
	SSL     bool   // 是否使用 HTTPS/WSS
}

// ServiceConfig 核心服务配置
type ServiceConfig struct {
	App                           string              // 应用名称。优先从环境变量 DISCOVER_APP 获取，若为空则自动通过代码检测。
	Register                      string              // 发现服务注册中心地址。支持 Redis URL 或 Redis 配置名称。
	Weight                        int                 // 当前节点在发现服务中的权重 (默认 100)
	Calls                         map[string]CallConfig // 依赖的下游服务调用配置
	Listen                        string              // 监听端口（|隔开多个监听）（,隔开多个选项），例如 80,http|443|443:h2|127.0.0.1:8080,h2c
	SSL                           map[string]*CertSet // SSL 证书配置，key 为域名
	NoLogGets                     bool                // 不记录 GET 请求的日志
	NoLogHeaders                  string              // 不记录请求头中包含的这些字段，多个字段用逗号分隔
	LogInputArrayNum              int                 // 请求字段中容器类型在日志打印个数限制
	LogInputFieldSize             int                 // 请求字段中单个字段在日志打印长度限制
	NoLogOutputFields             string              // 不记录响应字段中包含的这些字段
	LogOutputArrayNum             int                 // 响应字段中容器类型在日志打印个数限制
	LogOutputFieldSize            int                 // 响应字段中单个字段在日志打印长度限制
	Compress                      bool                // 是否启用压缩
	CompressMinSize               int                 // 启用压缩的最小长度
	CompressMaxSize               int                 // 启用压缩的最大长度
	CheckDomain                   string              // 心跳检测时使用域名
	AccessTokens                  map[string]*int     // 指定 Access-Token 验证及其对应的 auth-level
	RedirectTimeout               int                 // Proxy 和 Discover 发起请求时的超时时间 (ms)
	AcceptXRealIpWithoutRequestId bool                // 是否允许头部没有携带请求ID的 X-Real-IP 信息
	StatisticTime                 bool                // 是否开启请求时间统计
	StatisticTimeInterval         int                 // 统计时间间隔 (ms)
	MaxUploadSize                 int64               // 最大上传文件大小 (Bytes)
	Cpu                           int                 // CPU 占用的核数限制
	Memory                        int                 // 内存限制 (MB)
	CookieScope                   string              // Session Cookie 有效范围: host|domain|topDomain
	SessionWithoutCookie          bool                // Session 禁用 Cookie
	SessionRedis                  string              // Session 存储使用的 Redis 配置名称 (不设置则使用内存)
	SessionTimeout                int                 // Session 有效期 (秒，默认 3600)
	DeviceWithoutCookie           bool                // 设备ID禁用 Cookie
	IdServer                      string              // Redis 服务器连接 (用于全局唯一 ID 生成)
	IndexFiles                    []string            // 静态文件索引文件
	IndexDir                      bool                // 访问目录时显示文件列表
	ReadTimeout                   int                 // 读取请求的超时时间 (ms)
	ReadHeaderTimeout             int                 // 读取请求头的超时时间 (ms)
	WriteTimeout                  int                 // 响应写入的超时时间 (ms)
	IdleTimeout                   int                 // 连接空闲超时时间 (ms)
	MaxHeaderBytes                int                 // 请求头的最大字节数
	MaxHandlers                   int                 // 每个连接的最大处理程序数量
	MaxConcurrentStreams          uint32              // 每个连接的最大并发流数量
	MaxDecoderHeaderTableSize     uint32              // 解码器头表的最大大小
	MaxEncoderHeaderTableSize     uint32              // 编码器头表的最大大小
	MaxReadFrameSize              uint32              // 单个帧的最大读取大小
	MaxUploadBufferPerConnection  int32               // 每个连接的最大上传缓冲区大小
	MaxUploadBufferPerStream      int32               // 每个流的最大上传缓冲区大小
	StopTimeout                   int                 // 停止服务的超时时间 (ms)
	
	// 从配置文件中加载的静态路由策略 (按 Host 分组，全局配置用 "" 或 "*")
	Proxies  map[string]map[string]any
	Rewrites map[string]map[string]any
	Statics  map[string]map[string]string
}

var Config = ServiceConfig{}

// ApplyConfig 将 ServiceConfig 中的路由策略应用到内部的文件级策略中
func ApplyConfig() {
	hostPoliciesLock.Lock()
	defer hostPoliciesLock.Unlock()

	// 1. Proxies KV 解析
	fileProxies = make(map[string][]*proxyType)
	for host, kv := range Config.Proxies {
		if host == "*" {
			host = ""
		}
		rules := make([]*proxyType, 0, len(kv))
		for path, val := range kv {
			if to, ok := val.(string); ok {
				// 极简 KV 模式: "/api/*": "user-svc/v1/*"
				rules = append(rules, parseProxyRule(0, path, "", "", to))
			} else {
				// 对象模式: "/api/*": {"To": "...", "Auth": 1}
				m, _ := cast.ToMap[string, any](val)
				rules = append(rules, parseProxyRule(
					cast.Int(m["Auth"]),
					path,
					cast.String(m["ToApp"]),
					cast.String(m["ToPath"]),
					cast.String(m["To"]),
				))
			}
		}
		fileProxies[host] = rules
		rebuildProxiesUnderLock(host)
	}

	// 2. Rewrites KV 解析
	fileRewrites = make(map[string][]*rewriteType)
	for host, kv := range Config.Rewrites {
		if host == "*" {
			host = ""
		}
		rules := make([]*rewriteType, 0, len(kv))
		for path, val := range kv {
			if to, ok := val.(string); ok {
				rules = append(rules, parseRewriteRule(path, "", to))
			} else {
				m, _ := cast.ToMap[string, any](val)
				rules = append(rules, parseRewriteRule(
					path,
					cast.String(m["ToPath"]),
					cast.String(m["To"]),
				))
			}
		}
		fileRewrites[host] = rules
		rebuildRewritesUnderLock(host)
	}

	staticsByHostLock.Lock()
	defer staticsByHostLock.Unlock()
	fileStatics = make(map[string]map[string]*string)
	
	for host, config := range Config.Statics {
		if host == "*" {
			host = ""
		}
		newStatics := make(map[string]*string, len(config))
		for path, rootPath := range config {
			rp := rootPath
			if !filepath.IsAbs(rp) {
				if absPath, err := filepath.Abs(rp); err == nil {
					rp = absPath
				}
			}
			newStatics[path] = &rp
		}
		fileStatics[host] = newStatics
		rebuildStaticsUnderLock(host)
	}
}
