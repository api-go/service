package service

import (
	"bytes"
	"mime"
	"path/filepath"
	"strings"

	"github.com/tdewolff/minify/v2"
	"github.com/tdewolff/minify/v2/css"
	"github.com/tdewolff/minify/v2/html"
	"github.com/tdewolff/minify/v2/js"
	"github.com/tdewolff/minify/v2/json"
	"github.com/tdewolff/minify/v2/svg"
	"github.com/tdewolff/minify/v2/xml"
)

var assetMinifier = func() *minify.M {
	m := minify.New()
	m.Add("text/html", &html.Minifier{})
	m.Add("text/css", &css.Minifier{})
	m.Add("application/javascript", &js.Minifier{})
	m.Add("text/javascript", &js.Minifier{})
	m.Add("application/json", &json.Minifier{})
	m.Add("image/svg+xml", &svg.Minifier{})
	m.Add("text/xml", &xml.Minifier{})
	return m
}()

func minifyAsset(name string, input []byte) ([]byte, error) {
	ext := strings.ToLower(filepath.Ext(name))
	if strings.Contains(strings.TrimSuffix(filepath.Base(name), ext), ".min") {
		return input, nil
	}
	mimeType := mime.TypeByExtension(ext)
	if mimeType == "" {
		return input, nil
	}
	if ext == ".js" || ext == ".mjs" {
		input, err := minifyEmbeddedHTMLTemplates(input)
		if err != nil {
			return nil, err
		}
		return assetMinifier.Bytes(mimeType, input)
	}
	return assetMinifier.Bytes(mimeType, input)
}

// minifyEmbeddedHTMLTemplates handles only template literals marked /*html*/; other JS strings
// must remain untouched because their contents are JavaScript, not HTML.
func minifyEmbeddedHTMLTemplates(input []byte) ([]byte, error) {
	var out bytes.Buffer
	for i := 0; i < len(input); {
		if input[i] != '`' || (i > 0 && input[i-1] == '\\') {
			out.WriteByte(input[i])
			i++
			continue
		}
		start := i
		end := i + 1
		for end < len(input) {
			if input[end] == '`' && input[end-1] != '\\' {
				break
			}
			end++
		}
		if end == len(input) {
			out.Write(input[start:])
			break
		}
		prefix := strings.TrimSpace(string(input[max(0, start-16):start]))
		if !strings.HasSuffix(prefix, "/*html*/") {
			out.Write(input[start : end+1])
			i = end + 1
			continue
		}
		content := input[start+1 : end]
		minified, err := assetMinifier.Bytes("text/html", content)
		if err != nil {
			return nil, err
		}
		out.WriteByte('`')
		out.Write(minified)
		out.WriteByte('`')
		i = end + 1
	}
	return out.Bytes(), nil
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
