package service

import gohttp "apigo.cc/go/http"

// CertSet SSL 证书配置
type CertSet struct {
	CertFile string
	KeyFile  string
	Cert     string
	Key      string
}

// CallConfig 下游服务调用配置
type CallConfig struct {
	Timeout int    // 超时时间 (ms)
	Token   string // 访问凭证
	Http2   bool   // 是否强制使用 HTTP/2 (H2C)
	SSL     bool   // 是否使用 HTTPS/WSS
}

// WebSocketProxyConfig controls upgraded proxy connections independently of
// ordinary HTTP server timeouts. All durations are milliseconds.
type WebSocketProxyConfig struct {
	HandshakeTimeout int   // 0 uses RedirectTimeout (default 30 seconds)
	WriteTimeout     int   // 0 defaults to 30 seconds; negative disables the limit
	IdleTimeout      int   // 0 disables idle expiry; data and Ping/Pong refresh it
	MaxMessageSize   int64 // 0 leaves the message size unrestricted
	Compression      bool  // negotiate compression independently on each leg
}

// ServiceConfig 核心服务配置
type ServiceConfig struct {
	ExternalAddress               gohttp.ExternalAddressPolicy // 独立信任直接上游；默认不信任转发地址
	WebSocketProxy                WebSocketProxyConfig
	ClientIP                      ClientIPPolicy        // 空模式保留内部兼容；Gateway 使用 direct/trusted
	App                           string                // 应用名称。优先从环境变量 DISCOVER_APP 获取，若为空则自动通过代码检测。
	Register                      string                // 发现服务注册中心地址。支持 Redis URL 或 Redis 配置名称。
	Weight                        int                   // 当前节点在发现服务中的权重 (默认 100)
	Calls                         map[string]CallConfig // 依赖的下游服务调用配置
	Listen                        string                // 监听端口（|隔开多个监听，,隔开选项），例如 :80,http|:443,https|127.0.0.1:8080,h2c
	SSL                           map[string]*CertSet   // SSL 证书配置，key 为域名
	AutoCertDir                   string                // 未配置证书时自签名证书的持久化目录（默认 data/ssl/_auto）
	NoLogGets                     bool                  // 不记录 GET 请求的日志
	NoLogInput                    bool                  // 不记录请求输入
	NoLogOutput                   bool                  // 不记录响应输出
	NoLogAllHeaders               bool                  // 不记录所有请求/响应头
	NoLogRequestHeaders           string                // 不记录请求头中包含的这些字段（追加到动态默认列表），多个字段用逗号分隔
	NoLogResponseHeaders          string                // 不记录响应头中包含的这些字段（追加到动态默认列表），多个字段用逗号分隔
	LogInputObjectNum             int                   // 请求对象中最多记录的 key 数
	LogInputArrayNum              int                   // 请求数组中最多记录的元素数
	LogInputFieldSize             int                   // 请求单个字段的字符串截断长度
	NoLogResponseFields           string                // 不记录响应字段中包含的这些字段
	LogOutputObjectNum            int                   // 响应对象中最多记录的 key 数
	LogOutputArrayNum             int                   // 响应数组中最多记录的元素数
	LogOutputFieldSize            int                   // 响应单个字段的字符串截断长度
	LogOutputMaxSize              int                   // 非对象响应内容的最大记录长度
	Compress                      bool                  // 是否启用压缩
	CompressMinSize               int                   // 启用压缩的最小长度
	CompressMaxSize               int                   // 启用压缩的最大长度
	AutoReversion                 bool                  // 是否自动为文本资源引用追加版本号并启用文本资源处理
	AutoMinify                    bool                  // 是否为文本资源生成磁盘压缩缓存
	AutoReversionCachePath        string                // 文本资源缓存目录（默认 data/wwwcache）
	CheckDomain                   string                // 心跳检测时使用域名
	AccessTokens                  map[string]*int       // 指定 Access-Token 验证及其对应的 auth-level
	RedirectTimeout               int                   // Proxy 和 Discover 发起请求时的超时时间 (ms)
	AcceptXRealIpWithoutRequestId bool                  // 是否允许头部没有携带请求ID的 X-Real-IP 信息
	StatisticTime                 bool                  // 是否开启请求时间统计
	StatisticTimeInterval         int                   // 统计时间间隔 (ms)
	MaxUploadSize                 int64                 // 最大上传文件大小 (Bytes)
	Cpu                           int                   // CPU 占用的核数限制
	Memory                        int                   // 内存限制 (MB)
	CookieScope                   string                // Session Cookie 有效范围: host|domain|topDomain
	SessionWithoutCookie          bool                  // Session 禁用 Cookie
	SessionRedis                  string                // Session 存储使用的 Redis 配置名称 (不设置则使用内存)
	SessionTimeout                int                   // Session 有效期 (秒，默认 3600)
	DeviceWithoutCookie           bool                  // 设备ID禁用 Cookie
	IdServer                      string                // Redis 服务器连接 (用于全局唯一 ID 生成)
	IndexFiles                    []string              // 静态文件索引文件
	IndexDir                      bool                  // 访问目录时显示文件列表
	ReadTimeout                   int                   // 读取请求的超时时间 (ms)
	ReadHeaderTimeout             int                   // 读取请求头的超时时间 (ms)
	WriteTimeout                  int                   // 响应写入的超时时间 (ms)
	IdleTimeout                   int                   // 连接空闲超时时间 (ms)
	MaxHeaderBytes                int                   // 请求头的最大字节数
	MaxHandlers                   int                   // 每个连接的最大处理程序数量
	MaxConcurrentStreams          uint32                // 每个连接的最大并发流数量
	MaxDecoderHeaderTableSize     uint32                // 解码器头表的最大大小
	MaxEncoderHeaderTableSize     uint32                // 编码器头表的最大大小
	MaxReadFrameSize              uint32                // 单个帧的最大读取大小
	MaxUploadBufferPerConnection  int32                 // 每个连接的最大上传缓冲区大小
	MaxUploadBufferPerStream      int32                 // 每个流的最大上传缓冲区大小
	StopTimeout                   int                   // 停止服务的超时时间 (ms)

	// 从配置文件中加载的静态路由策略 (按 Host 分组，全局配置用 "" 或 "*")
	Proxies  map[string]map[string]any
	Rewrites map[string]map[string]any
	Statics  map[string]map[string]string
}
