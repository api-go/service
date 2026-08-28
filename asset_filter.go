package service

import (
	"bytes"
	"mime"
	"net/http"
	"strings"

	"apigo.cc/go/log"
)

func (ws *WebServer) shouldFilterDynamicContent(contentType string, data []byte) bool {
	if !ws.hasOutFilter {
		return false
	}
	if contentType == "" && len(data) > 0 {
		contentType = http.DetectContentType(data)
	}
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		mediaType = strings.TrimSpace(strings.Split(contentType, ";")[0])
	}
	mediaType = strings.ToLower(mediaType)
	if mediaType == "text/html" {
		return ws.webDevEnabled || (ws.Config.AutoReversion && ws.assetResolver != nil)
	}
	return mediaType == "text/css" && ws.Config.AutoReversion && ws.assetResolver != nil
}

func (ws *WebServer) SetAssetVersionResolver(resolve AssetVersionResolver) {
	ws.assetResolver = resolve
}
func SetAssetVersionResolver(resolve AssetVersionResolver) {
	DefaultServer.SetAssetVersionResolver(resolve)
}

// EnableAssetReversion installs production HTML rewriting for dynamic outputs.
func (ws *WebServer) EnableAssetReversion(resolve AssetVersionResolver) {
	ws.assetResolver = resolve
	ws.SetOutFilter(func(_ map[string]any, request *Request, response *Response, out any, _ *log.Logger) (any, bool) {
		if response.filteredWrite {
			return nil, false
		}
		var content []byte
		switch value := out.(type) {
		case string:
			content = []byte(value)
		case []byte:
			content = value
		}
		if len(content) == 0 {
			return nil, false
		}
		contentType := response.Header().Get("Content-Type")
		original := content
		mediaType, _, _ := mime.ParseMediaType(contentType)
		mediaType = strings.ToLower(mediaType)
		if ws.Config.AutoReversion && ws.assetResolver != nil && (mediaType == "text/html" || mediaType == "text/css") {
			content = rewriteAssetReferences(request.Host, request.URL.Path, content, ws.assetResolver, mediaType == "text/css")
		}
		if !bytes.Equal(original, content) {
			response.Header().Del("Content-Length")
		}
		return content, false
	})
}

func EnableAssetReversion(resolve AssetVersionResolver) { DefaultServer.EnableAssetReversion(resolve) }
