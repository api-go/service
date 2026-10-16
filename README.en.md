[English](README.en.md) | [简体中文](README.md)

# go/service

HTTP and WebSocket service framework with routing, dependency injection,
validation, static resources, reverse proxies and Discover integration.

## Conservative static minification

`ServiceConfig.AutoMinify` is independent of `AutoReversion` and defaults to
false. It applies to static resources, not arbitrary dynamic responses. A sibling
`.min.*` asset takes precedence over runtime minification.

JavaScript uses esbuild whitespace minification with syntax and identifier
minification disabled. Public declarations, property names and module boundaries
are retained. License comments stay inline. This is a resource compressor, not a
bundler or a browser compatibility compiler.

Embedded HTML in JS is opt-in: `Util.makeDom(/*html*/\`<div>...</div>\`)`.
Only parser-identified, untagged templates without JavaScript interpolation are
eligible. Escaped framework expressions such as `\${this.state.value}` remain
supported. Strings, comments, regular expressions and other templates are not
processed as HTML. Unsupported template pre-parsing syntax skips only the HTML
optimization; JavaScript compression still runs.

HTML retains explicit end tags, attribute quotes, default attribute values,
document tags and comments. Its whitespace normalization must not be used when
an application requires the exact original HTML string. Embedded HTML output is
checked before reinsertion into JavaScript: if HTML entity normalization creates
interpolation, breaks the template boundary or changes escape sequences, that
template is preserved verbatim while the surrounding JavaScript is compressed.
Ordinary HTML inline scripts and `type="module"` use the same JS compressor.

Cache keys include policy version, resource scope, path and source bytes. Host
changes, replaced mounts and compressor upgrades cannot reuse stale transformed
bytes. Transform or cache write errors preserve the original source. An invalid
source remains invalid; minification does not repair application code.

Other registered text formats use the HTML, CSS, JSON, SVG and XML minifiers.
HTTP content encoding such as gzip is separate from source minification.

## Main APIs

- `NewWebServer`, `WebServer.Handler`, `Start`, `AsyncStart`, `Stop`, `Wait`:
  create, serve and manage a service.
- `Host`, `Register`, `Restful`, `Group`, `RegisterWebsocket`: define routes;
  handler parameters support request, response, logger and input injection.
- `Static`, `StaticByHost`, `HostContext.StaticFS`, `ReplaceStatics`,
  `ReplaceStaticFS`, `ReplaceStaticFSExclusive`: configure physical and virtual
  static sources. Custom files must support seeking. Exclusive host mounts do
  not fall through to other custom sources.
- `SetFallback`, `Rewrite`, `Proxy`: fallback, URL rewriting and upstream
  routing. Proxies preserve upstream status, Location, cookies and public Host.
- `Session`, `VerifyStruct`, `GetInjectT`: session authorization, input
  validation and dependency injection.
- `EnableAssetReversion`, `SetAssetPatternResolver`: dependency-aware resource
  versions and wildcard HTML asset references.
- `SetRequestGate`, `SetClientIPPolicy`, `RelayClientIP`, `PrepareHostPolicy`,
  `ReplaceHostPolicies`, `SetCertificateProvider`: trusted ingress and managed
  immutable host configuration.
- `WebServer.Reload`: atomically reload listeners and certificates; invalid
  configuration or a bind failure preserves the previous service.
- `WebServer.SetProxyHTTPClient`, `SetProxyWebSocketDialer`,
  `ServiceConfig.WebSocketProxy`: HTTP and WebSocket proxy transports, limits
  and lifecycle configuration.

Use `go doc -all apigo.cc/go/service` for exact signatures and the
[Chinese reference](README.md) for configuration and lifecycle examples.

## Validation

Run against released dependencies:

```sh
GOWORK=off go test -v ./...
GOWORK=off go test -race ./...
GOWORK=off go test -bench=. ./...
GOWORK=off go vet ./...
```

[TEST.md](TEST.md) records HTTP execution semantics, embedded template boundaries,
cache lifecycle, existing HTML bindings and prebuilt asset coverage.
