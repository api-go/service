package service

import (
	"strings"

	"apigo.cc/go/log"
)

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
		if !ws.Config.AutoReversion || ws.assetResolver == nil || response.filteredWrite {
			return nil, false
		}
		if !strings.HasPrefix(response.Header().Get("Content-Type"), "text/html") {
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
		return RewriteAssetReferences(request.Host, request.URL.Path, content, ws.assetResolver), false
	})
}

func EnableAssetReversion(resolve AssetVersionResolver) { DefaultServer.EnableAssetReversion(resolve) }
