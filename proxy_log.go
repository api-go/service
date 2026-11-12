package service

import (
	"context"
	"errors"
	"io"
	"net"
	"net/url"
	"strings"
	"sync/atomic"
	"time"

	"github.com/api-go/log"
)

// ProxyLog replaces the redundant request record only when CompactProxyLog is
// enabled. Normal records contain no bodies or header maps.
type ProxyLog struct {
	log.BaseLog
	App             string            `log:"pos:6"`
	Target          string            `log:"pos:7,color:cyan"`
	Node            string            `log:"pos:8,color:gray"`
	Method          string            `log:"pos:9"`
	Host            string            `log:"pos:10"`
	Path            string            `log:"pos:11,color:cyan"`
	Proto           string            `log:"pos:12"`
	ClientIp        string            `log:"pos:13"`
	Status          int               `log:"pos:14"`
	UsedTime        float64           `log:"pos:15,precision:6"`
	ResponseBytes   int64             `log:"pos:16"`
	Phase           string            `log:"pos:17"`
	Error           string            `log:"pos:18,color:red"`
	RequestHeaders  map[string]string `log:"pos:19"`
	ResponseHeaders map[string]string `log:"pos:20"`
	ResponseData    any               `log:"pos:21"`
	RequestBytes    int64             `log:"pos:22"`
}

func (l *ProxyLog) Reset() {
	l.BaseLog.Reset()
	l.App = ""
	l.Target = ""
	l.Node = ""
	l.Method = ""
	l.Host = ""
	l.Path = ""
	l.Proto = ""
	l.ClientIp = ""
	l.Status = 0
	l.UsedTime = 0
	l.ResponseBytes = 0
	l.RequestBytes = 0
	l.Phase = ""
	l.Error = ""
	l.RequestHeaders = nil
	l.ResponseHeaders = nil
	l.ResponseData = nil
}
func init() { log.RegisterType("proxy", &ProxyLog{}) }

type proxyLogState struct {
	requestBytes               atomic.Int64
	target, node, phase, error string
	bytes                      int64
	started                    time.Time
	detail                     []byte
}

func safeProxyTarget(target string) string {
	if parsed, err := url.Parse(target); err == nil && parsed.Host != "" {
		return parsed.Scheme + "://" + parsed.Host
	}
	return strings.SplitN(target, "?", 2)[0]
}

func (s *proxyLogState) fail(phase string, err error) {
	if s == nil {
		return
	}
	s.phase = phase
	if err == nil {
		return
	}
	switch {
	case errors.Is(err, context.Canceled):
		s.error = "request canceled"
	case errors.Is(err, context.DeadlineExceeded):
		s.error = "request timeout"
	default:
		var requestError *url.Error
		if errors.As(err, &requestError) {
			err = requestError.Err
		}
		var network net.Error
		if errors.As(err, &network) && network.Timeout() {
			s.error = "upstream timeout"
		} else {
			s.error = err.Error()
		}
	}
	if len(s.error) > 1024 {
		s.error = s.error[:1024]
	}
}

func (ws *WebServer) logProxy(request *Request, response *Response, logger *log.Logger) {
	state := response.proxyLog
	if state == nil {
		return
	}
	failed := state.error != "" || response.Code >= 400
	level := log.INFO
	if failed {
		level = log.WARNING
	}
	if !logger.CheckLevel(level) {
		return
	}
	entry := log.GetEntry[ProxyLog]()
	logger.FillBase(&entry.BaseLog, "proxy")
	entry.App = ws.Config.App
	entry.Target = state.target
	entry.Node = state.node
	entry.Method = request.Method
	entry.Host = hostOnly(request.Host)
	entry.Path = request.URL.Path
	entry.Proto = request.Proto
	entry.ClientIp = request.ClientIp()
	entry.Status = response.Code
	entry.UsedTime = time.Since(state.started).Seconds()
	entry.ResponseBytes = state.bytes
	entry.RequestBytes = state.requestBytes.Load()
	entry.Phase = state.phase
	entry.Error = state.error
	if failed {
		if !ws.Config.NoLogAllHeaders {
			entry.RequestHeaders = sanitizeLogHeaders(request.Request.Header, ws.effectiveNoLogRequestHeaders())
			entry.ResponseHeaders = sanitizeLogHeaders(response.Writer.Header(), ws.effectiveNoLogResponseHeaders())
		}
		if !ws.Config.NoLogOutput && len(state.detail) > 0 {
			entry.ResponseData = sanitizeRespBody(state.detail, &ws.Config)
		}
	}
	logger.Log(entry)
}

// proxyDiagnosticCapture retains at most 4096 response bytes and never changes
// streaming backpressure or buffers an entire error response.
type proxyDiagnosticCapture struct{ state *proxyLogState }

func (w proxyDiagnosticCapture) Write(p []byte) (int, error) {
	n := len(p)
	remaining := 4096 - len(w.state.detail)
	if remaining > 0 {
		if remaining > len(p) {
			remaining = len(p)
		}
		w.state.detail = append(w.state.detail, p[:remaining]...)
	}
	return n, nil
}

type proxyRequestBody struct {
	io.ReadCloser
	state *proxyLogState
}

func (body *proxyRequestBody) Read(p []byte) (int, error) {
	n, err := body.ReadCloser.Read(p)
	body.state.requestBytes.Add(int64(n))
	return n, err
}
