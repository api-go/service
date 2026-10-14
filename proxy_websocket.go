package service

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"apigo.cc/go/discover"
	goencoding "apigo.cc/go/encoding"
	gohttp "apigo.cc/go/http"
	"apigo.cc/go/log"
	"github.com/gorilla/websocket"
)

var proxyWebSocketWritePools sync.Map
var proxyWebSocketCopyPool = sync.Pool{New: func() any { buffer := make([]byte, 32*1024); return &buffer }}

func proxyWebSocketWritePool(size int) *sync.Pool {
	if size <= 0 {
		size = 4096
	}
	pool, _ := proxyWebSocketWritePools.LoadOrStore(size, &sync.Pool{})
	return pool.(*sync.Pool)
}

func (ws *WebServer) proxyWebSocketClient(settings WebSocketProxyConfig) *gohttp.Client {
	client := ws.getHttpClient().Clone()
	timeout := settings.HandshakeTimeout
	if timeout <= 0 {
		timeout = ws.Config.RedirectTimeout
	}
	if timeout <= 0 {
		timeout = 30000
	}
	client.GetRawClient().Timeout = time.Duration(timeout) * time.Millisecond
	ws.proxyClientsMu.RLock()
	custom := ws.proxyWebSocketDialer
	ws.proxyClientsMu.RUnlock()
	if custom != nil {
		client.SetWebSocketDialer(custom)
	}
	dialer := client.WebSocketDialer()
	// Proxy clients must not share upstream cookie jars across ingress users.
	client.GetRawClient().Jar = nil
	dialer.Jar = nil
	if settings.Compression {
		dialer.EnableCompression = true
	}
	if dialer.WriteBufferPool == nil {
		dialer.WriteBufferPool = proxyWebSocketWritePool(dialer.WriteBufferSize)
	}
	client.SetWebSocketDialer(dialer)
	return client
}

// WebSocket handshakes use independent HTTP/1.1 connections. Ordinary requests
// keep their HTTP/2 transport. Authentication has already run in processProxy.
func (ws *WebServer) proxyWebSocket(request *Request, response *Response, logger *log.Logger, app, path string) {
	if request.Method != http.MethodGet {
		response.Writer.Header().Set("Allow", http.MethodGet)
		response.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if request.Request.Header.Get("Sec-WebSocket-Version") != "13" {
		response.Writer.Header().Set("Sec-WebSocket-Version", "13")
		response.WriteHeader(http.StatusUpgradeRequired)
		return
	}
	challenge := request.Request.Header.Get("Sec-WebSocket-Key")
	key, keyError := goencoding.UnBase64(challenge)
	if keyError != nil || len(key) != 16 || goencoding.Base64(key) != challenge {
		response.WriteHeader(http.StatusBadRequest)
		return
	}
	settings := ws.Config.WebSocketProxy
	client := ws.proxyWebSocketClient(settings)
	headers := make(http.Header)
	pairs := proxyRelayHeaders(request)
	for i := 1; i < len(pairs); i += 2 {
		key := http.CanonicalHeaderKey(pairs[i-1])
		switch key {
		case "Sec-Websocket-Key", "Sec-Websocket-Version", "Sec-Websocket-Extensions":
			continue
		}
		headers.Add(key, pairs[i])
	}
	var upstream *gohttp.WebSocketConn
	var handshake *http.Response
	var node string
	var err error
	started := time.Now()
	if strings.Contains(app, "://") {
		target, parseErr := url.Parse(app + path)
		if parseErr != nil {
			response.WriteHeader(http.StatusBadGateway)
			return
		}
		switch target.Scheme {
		case "https":
			target.Scheme = "wss"
		case "http":
			target.Scheme = "ws"
		}
		node = target.Host
		upstream, handshake, err = client.OpenContext(request.Context(), target.String(), headers)
	} else {
		if !ws.discoverStarted {
			response.WriteHeader(http.StatusBadGateway)
			return
		}
		caller := discover.NewCaller(request.Request, logger)
		caller.WebSocketClient = client
		upstream, handshake, node, err = caller.Open(app, path, headers)
	}
	if err != nil {
		var handshakeError *gohttp.WebSocketHandshakeError
		truncated := errors.As(err, &handshakeError) && handshakeError.BodyTruncated
		status := http.StatusBadGateway
		if handshake != nil {
			status = handshake.StatusCode
		}
		logger.Warning("websocket proxy handshake rejected", "app", app, "path", path, "node", node, "status", status, "duration", time.Since(started), "bodyTruncated", truncated, "error", err.Error())
		if handshake != nil && handshake.StatusCode >= 200 {
			copyResponse(&gohttp.Result{Response: handshake}, response, logger)
		} else {
			if handshake != nil && handshake.Body != nil {
				handshake.Body.Close()
			}
			response.WriteHeader(http.StatusBadGateway)
		}
		return
	}
	defer upstream.Close()
	if handshake != nil && handshake.Body != nil {
		defer handshake.Body.Close()
	}
	selected := upstream.Conn.Subprotocol()
	if selected != "" {
		valid := false
		for _, offered := range websocket.Subprotocols(request.Request) {
			if selected == offered {
				valid = true
				break
			}
		}
		if !valid {
			logger.Warning("websocket proxy selected unoffered client subprotocol", "node", node, "protocol", selected)
			response.WriteHeader(http.StatusBadGateway)
			return
		}
	}
	responseHeaders := make(http.Header)
	if handshake != nil {
		blocked := proxyHopHeaders(handshake.Header)
		for _, key := range []string{"Sec-WebSocket-Accept", "Sec-WebSocket-Protocol", "Sec-WebSocket-Extensions", "Content-Length"} {
			blocked[http.CanonicalHeaderKey(key)] = true
		}
		for key, values := range handshake.Header {
			if blocked[http.CanonicalHeaderKey(key)] {
				continue
			}
			for _, value := range values {
				responseHeaders.Add(key, value)
			}
		}
	}
	localBlocked := proxyHopHeaders(response.Writer.Header())
	for _, key := range []string{"Sec-WebSocket-Accept", "Sec-WebSocket-Protocol", "Sec-WebSocket-Extensions", "Content-Length"} {
		localBlocked[http.CanonicalHeaderKey(key)] = true
	}
	for key, values := range response.Writer.Header() {
		canonical := http.CanonicalHeaderKey(key)
		if canonical == http.CanonicalHeaderKey(ws.usedDeviceIdKey) || canonical == http.CanonicalHeaderKey(ws.usedSessionIdKey) {
			continue
		}
		if localBlocked[canonical] {
			continue
		}
		for _, value := range values {
			responseHeaders.Add(key, value)
		}
	}
	upgrader := *defaultUpgrader
	upgrader.WriteBufferPool = proxyWebSocketWritePool(upgrader.WriteBufferSize)
	upgrader.EnableCompression = settings.Compression
	if protocol := upstream.Conn.Subprotocol(); protocol != "" {
		upgrader.Subprotocols = []string{protocol}
	}
	downstream, err := upgrader.Upgrade(response.Writer, request.Request, responseHeaders)
	if err != nil {
		logger.Error("websocket proxy client upgrade failed", "node", node, "error", err.Error())
		return
	}
	response.Code = http.StatusSwitchingProtocols
	response.changed = true
	response.headerWritten = true
	tracked := &WebSocketConn{Conn: downstream}
	if !ws.registerWebSocket(tracked) {
		_ = downstream.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseGoingAway, "service stopping"), time.Now().Add(time.Second))
		_ = downstream.Close()
		return
	}
	defer tracked.Close()
	results, reason := relayProxyWebSocket(request.Context(), upstream.Conn, downstream, settings)
	_ = tracked.Close()
	logger.Info("websocket proxy closed", "app", app, "path", path, "node", node, "status", http.StatusSwitchingProtocols, "duration", time.Since(started), "reason", reason, "upstreamBytes", results[0].bytes, "downstreamBytes", results[1].bytes, "upstreamMessages", results[0].messages, "downstreamMessages", results[1].messages, "upstreamClose", results[0].closeCode, "downstreamClose", results[1].closeCode, "upstreamError", proxyFrameError(results[0].err), "downstreamError", proxyFrameError(results[1].err))
}

type proxyFrameResult struct {
	direction int
	bytes     int64
	messages  int64
	closeCode int
	err       error
}

func proxyFrameError(err error) string {
	if err == nil || errors.Is(err, net.ErrClosed) {
		return ""
	}
	return err.Error()
}

type proxyActivityReader struct {
	io.Reader
	touch func()
	limit int64
	read  int64
}

func (reader *proxyActivityReader) Read(data []byte) (int, error) {
	if reader.limit > 0 {
		remaining := reader.limit - reader.read
		if remaining == 0 {
			var probe [1]byte
			n, err := reader.Reader.Read(probe[:])
			if n > 0 {
				return 0, websocket.ErrReadLimit
			}
			return 0, err
		}
		if int64(len(data)) > remaining {
			data = data[:remaining]
		}
	}
	n, err := reader.Reader.Read(data)
	reader.read += int64(n)
	if n > 0 {
		reader.touch()
	}
	return n, err
}

func relayProxyWebSocket(ctx context.Context, upstream, downstream *websocket.Conn, settings WebSocketProxyConfig) ([2]proxyFrameResult, string) {
	writeTimeout := time.Duration(settings.WriteTimeout) * time.Millisecond
	if settings.WriteTimeout == 0 {
		writeTimeout = 30 * time.Second
	}
	idleTimeout := time.Duration(settings.IdleTimeout) * time.Millisecond
	controlDeadline := func() time.Time {
		timeout := time.Second
		if writeTimeout > 0 && writeTimeout < timeout {
			timeout = writeTimeout
		}
		return time.Now().Add(timeout)
	}
	finished := make(chan proxyFrameResult, 2)
	for index, direction := range [][2]*websocket.Conn{{downstream, upstream}, {upstream, downstream}} {
		to, from := direction[0], direction[1]
		from.SetReadLimit(settings.MaxMessageSize)
		touch := func() {
			if idleTimeout > 0 {
				_ = from.SetReadDeadline(time.Now().Add(idleTimeout))
			}
		}
		touch()
		from.SetPingHandler(func(data string) error {
			touch()
			return to.WriteControl(websocket.PingMessage, []byte(data), controlDeadline())
		})
		from.SetPongHandler(func(data string) error {
			touch()
			return to.WriteControl(websocket.PongMessage, []byte(data), controlDeadline())
		})
		from.SetCloseHandler(func(code int, text string) error {
			message := websocket.FormatCloseMessage(code, text)
			_ = to.WriteControl(websocket.CloseMessage, message, controlDeadline())
			return from.WriteControl(websocket.CloseMessage, message, controlDeadline())
		})
		go func() {
			result := proxyFrameResult{direction: index}
			defer func() {
				var closeError *websocket.CloseError
				if errors.As(result.err, &closeError) {
					result.closeCode = closeError.Code
				}
				finished <- result
			}()
			for {
				kind, reader, err := from.NextReader()
				if err != nil {
					result.err = err
					return
				}
				touch()
				deadline := time.Time{}
				if writeTimeout > 0 {
					deadline = time.Now().Add(writeTimeout)
				}
				if err = to.SetWriteDeadline(deadline); err != nil {
					result.err = err
					return
				}
				writer, err := to.NextWriter(kind)
				if err != nil {
					result.err = err
					return
				}
				buffer := proxyWebSocketCopyPool.Get().(*[]byte)
				n, copyErr := io.CopyBuffer(writer, &proxyActivityReader{Reader: reader, touch: touch, limit: settings.MaxMessageSize}, *buffer)
				proxyWebSocketCopyPool.Put(buffer)
				result.bytes += n
				if copyErr != nil {
					// Do not finalize a truncated or oversized message with FIN.
					// The peer must observe an incomplete message and a close.
					result.err = copyErr
					return
				}
				closeErr := writer.Close()
				if closeErr != nil {
					result.err = closeErr
					return
				}
				result.messages++
			}
		}()
	}
	var results [2]proxyFrameResult
	remaining := 2
	reason := "peer closed"
	select {
	case first := <-finished:
		remaining--
		results[first.direction] = first
		code := 0
		message := ""
		var networkError net.Error
		if ctx.Err() != nil {
			code = websocket.CloseGoingAway
			message = "service stopping"
			reason = "context canceled"
		} else if errors.Is(first.err, websocket.ErrReadLimit) {
			code = websocket.CloseMessageTooBig
			message = "message limit exceeded"
			reason = message
		} else if errors.As(first.err, &networkError) && networkError.Timeout() {
			code = websocket.CloseGoingAway
			message = "proxy timeout"
			reason = message
		} else if first.closeCode == 0 || first.closeCode == websocket.CloseAbnormalClosure {
			code = websocket.CloseInternalServerErr
			message = "peer disconnected"
			reason = message
		}
		if code != 0 {
			closeMessage := websocket.FormatCloseMessage(code, message)
			_ = downstream.WriteControl(websocket.CloseMessage, closeMessage, controlDeadline())
			_ = upstream.WriteControl(websocket.CloseMessage, closeMessage, controlDeadline())
		}
	case <-ctx.Done():
		reason = "context canceled"
		message := websocket.FormatCloseMessage(websocket.CloseGoingAway, "service stopping")
		_ = downstream.WriteControl(websocket.CloseMessage, message, controlDeadline())
		_ = upstream.WriteControl(websocket.CloseMessage, message, controlDeadline())
	}
	_ = upstream.Close()
	_ = downstream.Close()
	for range remaining {
		result := <-finished
		results[result.direction] = result
	}
	return results, reason
}
