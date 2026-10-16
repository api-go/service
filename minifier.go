package service

import (
	"bytes"
	"fmt"
	"io"
	"mime"
	"path/filepath"
	"strings"

	"github.com/evanw/esbuild/pkg/api"
	"github.com/tdewolff/minify/v2"
	"github.com/tdewolff/minify/v2/css"
	"github.com/tdewolff/minify/v2/html"
	"github.com/tdewolff/minify/v2/json"
	"github.com/tdewolff/minify/v2/svg"
	"github.com/tdewolff/minify/v2/xml"
	"github.com/tdewolff/parse/v2"
	jsparse "github.com/tdewolff/parse/v2/js"
)

var assetMinifier = func() *minify.M {
	m := minify.New()
	// Declarative UI templates may place framework <template> elements after
	// elements whose end tags are optional in ordinary HTML. Keep the explicit
	// structure and attribute quoting so minification cannot change the DOM that
	// the binding runtime receives.
	m.Add("text/html", &html.Minifier{
		KeepEndTags: true, KeepQuotes: true, KeepDefaultAttrVals: true,
		KeepDocumentTags: true, KeepComments: true,
	})
	m.Add("text/css", &css.Minifier{})
	m.AddFunc("application/javascript", minifyJavaScript)
	m.AddFunc("text/javascript", minifyJavaScript)
	// The HTML minifier passes type="module" as the embedded media type.
	m.AddFunc("module", minifyJavaScript)
	m.Add("application/json", &json.Minifier{})
	m.Add("image/svg+xml", &svg.Minifier{})
	m.Add("text/xml", &xml.Minifier{})
	return m
}()

// Whitespace compression preserves control flow and public declaration names.
// Syntax optimizations in the former compressor reassociated nested else branches.
func minifyJavaScript(_ *minify.M, w io.Writer, r io.Reader, _ map[string]string) error {
	input, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	result := api.Transform(string(input), api.TransformOptions{
		Loader: api.LoaderJS, Target: api.ESNext, MinifyWhitespace: true,
		MinifySyntax: false, MinifyIdentifiers: false,
		Charset: api.CharsetUTF8, LegalComments: api.LegalCommentsInline,
	})
	if len(result.Errors) != 0 {
		return fmt.Errorf("minify JavaScript: %s", result.Errors[0].Text)
	}
	_, err = w.Write(result.Code)
	return err
}

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
	// Tokenizers may normalize their input buffer in place. Preserve the source
	// bytes used by the cache's error fallback.
	return assetMinifier.Bytes(mimeType, bytes.Clone(input))
}

func safeMinifiedHTMLTemplate(original, minified []byte) bool {
	literal := make([]byte, 0, len(minified)+2)
	literal = append(literal, '`')
	literal = append(literal, minified...)
	literal = append(literal, '`')
	input := parse.NewInputBytes(literal)
	defer input.Restore()
	lexer := jsparse.NewLexer(input)
	token, data := lexer.Next()
	return token == jsparse.TemplateToken && len(data) == len(literal) &&
		bytes.Equal(templateEscapes(original), templateEscapes(minified))
}

func templateEscapes(content []byte) []byte {
	var escapes []byte
	for i := 0; i < len(content); i++ {
		if content[i] == '\\' {
			escapes = append(escapes, content[i])
			if i+1 < len(content) {
				i++
				escapes = append(escapes, content[i])
			}
		}
	}
	return escapes
}

// Only rewrite parser-identified, untagged templates without JS interpolation.
// Searching raw backticks also matches strings, comments and regular expressions;
// interpolations and tagged templates have JavaScript semantics, not plain HTML.
func minifyEmbeddedHTMLTemplates(input []byte) ([]byte, error) {
	if !bytes.Contains(input, []byte("/*html*/")) {
		return input, nil
	}
	parsed := parse.NewInputBytes(input)
	defer parsed.Restore()
	ast, err := jsparse.Parse(parsed, jsparse.Options{})
	if err != nil {
		// This optional HTML optimization must not restrict syntax accepted by the
		// JS compressor. On unsupported syntax, let esbuild handle the source.
		return input, nil
	}
	templates := htmlTemplateVisitor{starts: make(map[*byte][]byte)}
	jsparse.Walk(templates, ast)
	// Parser token slices refer to its input buffer. Match their addresses,
	// rather than their text, so identical strings/comments cannot be rewritten.
	source := parsed.Bytes()
	var out bytes.Buffer
	for i := 0; i < len(source); {
		literal, ok := templates.starts[&source[i]]
		if !ok || !bytes.HasSuffix(bytes.TrimSpace(source[:i]), []byte("/*html*/")) {
			out.WriteByte(source[i])
			i++
			continue
		}
		minified, err := assetMinifier.Bytes("text/html", bytes.Clone(literal[1:len(literal)-1]))
		if err != nil {
			return nil, err
		}
		// HTML entity shortening can introduce JS syntax (&#36;{...}, &#96;,
		// &#92;n). Accept only a single non-interpolated template with the same
		// JS escape sequences. Otherwise retain this template and still minify JS.
		if !safeMinifiedHTMLTemplate(literal[1:len(literal)-1], minified) {
			out.Write(literal)
			i += len(literal)
			continue
		}
		out.WriteByte('`')
		out.Write(minified)
		out.WriteByte('`')
		i += len(literal)
	}
	return out.Bytes(), nil
}

type htmlTemplateVisitor struct{ starts map[*byte][]byte }

func (v htmlTemplateVisitor) Enter(node jsparse.INode) jsparse.IVisitor {
	if template, ok := node.(*jsparse.TemplateExpr); ok && template.Tag == nil && len(template.List) == 0 && len(template.Tail) >= 2 {
		v.starts[&template.Tail[0]] = template.Tail
	}
	return v
}
func (htmlTemplateVisitor) Exit(jsparse.INode) {}
