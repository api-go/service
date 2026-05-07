package service

// CertSet SSL 证书配置
type CertSet struct {
	CertFile string
	KeyFile  string
}

// ServiceConfig 核心服务配置
type ServiceConfig struct {
	Listen                        string              // 监听端口（|隔开多个监听）（,隔开多个选项），例如 80,http|443|443:h2|127.0.0.1:8080,h2c
	SSL                           map[string]*CertSet // SSL 证书配置，key 为域名
	NoLogGets                     bool                // 不记录 GET 请求的日志
	NoLogHeaders                  string              // 不记录请求头中包含的这些字段，多个字段用逗号分隔
	LogInputArrayNum              int                 // 请求字段中容器类型在日志打印个数限制
	LogInputFieldSize             int                 // 请求字段中单个字段在日志打印长度限制
	NoLogOutputFields             string              // 不记录响应字段中包含的这些字段
	LogOutputArrayNum             int                 // 响应字段中容器类型在日志打印个数限制
	LogOutputFieldSize            int                 // 响应字段中单个字段在日志打印长度限制
	LogWebsocketAction            bool                // 记录 Websocket 中每个 Action 的请求日志
	Compress                      bool                // 是否启用压缩
	CompressMinSize               int                 // 启用压缩的最小长度
	CompressMaxSize               int                 // 启用压缩的最大长度
	CheckDomain                   string              // 心跳检测时使用域名
	AccessTokens                  map[string]*int     // 指定 Access-Token 验证及其对应的 auth-level
	RedirectTimeout               int                 // Proxy 和 Discover 发起请求时的超时时间 (ms)
	AcceptXRealIpWithoutRequestId bool                // 是否允许头部没有携带请求ID的 X-Real-IP 信息
	StatisticTime                 bool                // 是否开启请求时间统计
	StatisticTimeInterval         int                 // 统计时间间隔 (ms)
	Fast                          bool                // 是否启用快速模式
	MaxUploadSize                 int64               // 最大上传文件大小 (Bytes)
	IpPrefix                      string              // Discover 服务发现时指定使用的 IP 网段
	Cpu                           int                 // CPU 占用的核数限制
	Memory                        int                 // 内存限制 (MB)
	CpuMonitor                    bool                // 记录 CPU 使用情况
	MemoryMonitor                 bool                // 记录内存使用情况
	CpuLimitValue                 uint                // CPU 自动重启阈值 (10-100)
	MemoryLimitValue              uint                // 内存自动重启阈值 (10-100)
	CpuLimitTimes                 uint                // CPU 报警阈值连续次数
	MemoryLimitTimes              uint                // 内存报警阈值连续次数
	CookieScope                   string              // Session Cookie 有效范围: host|domain|topDomain
	SessionWithoutCookie          bool                // Session 禁用 Cookie
	DeviceWithoutCookie           bool                // 设备ID禁用 Cookie
	IdServer                      string              // Redis 服务器连接 (用于全局唯一 ID 生成)
	KeepKeyCase                   bool                // 是否保持 Key 的首字母大小写
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
}

var Config = ServiceConfig{}
