package service

import (
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
	return assetMinifier.Bytes(mimeType, input)
}
