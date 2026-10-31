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

## HTTP/2 WebSocket (v1.5.48)

Ingress supports RFC 8441 when the process starts with GODEBUG=http2xconnect=1 (required by Go/x/net). Upstream automatically prefers H2 for WSS, falling back only when ALPN/SETTINGS lack support. Plain WS stays on H1 unless H2C is explicitly configured. No WebSocketProxy.HTTP2 switch is required. HTTP rejections, certificate failures and timeouts do not trigger fallback. Independent Gorilla proxy dialers preserve their H1 network policy. Default proxy clients share H2 pools and bounded capability caches, without a shared cookie jar; Stop cleans idle default pools.

Depends on published HTTP v1.5.14; standalone checks run with GOWORK=off. TLS measurements and raw results: http/testdata/websocket/20261022-tls-compare.md.

## External addressing / 外部地址（v1.5.50）

`ServiceConfig.ExternalAddress` and `WebServer.SetExternalAddressPolicy(http.ExternalAddressPolicy)` configure explicit direct-peer trust independently of ClientIPPolicy. Default behavior (v1.5.50) preserves the existing Request-ID internal relay convention: first ingress without an ID uses actual TLS/Host, subsequent controlled hops with an incoming ID retain external metadata. No per-hop configuration is needed.

Ingress pins HTTP external addressing before health, gates, redirects, proxy and business handling. Invalid trusted metadata returns 400. `Request.ExternalAddress()` exposes the result; `MakeUrl` uses its Origin and actual TLS for direct requests (returns empty on invalid addressing). HTTPS redirects use the same external scheme, avoiding loops after TLS termination.

Fixed URL, discovery and WebSocket transparent proxies overwrite both supported address-header pairs from the snapshot and retain Origin unchanged. RPC callers carry browser context separately from the internal destination and credentials. Never use X-Host/X-Scheme raw values for security decisions. Optional Mode direct always uses the connection; Mode trusted enforces explicitly configured direct-peer addresses. Forwarded lists are unsupported; each proxy writes a single complete pair.

## 默认可用 / Default internal relay (v1.5.50)

Service prepares external addressing before generating a missing Request-ID. This preserves outer HTTPS and MakeUrl across fixed/discovery/H2C/WebSocket proxy hops without extra configuration. It does not change RPC destinations or credentials. Optional `ExternalAddress.Mode` supports legacy (default), direct or trusted; proxy addresses without Mode select trusted. Public Gateway boundaries explicitly use direct by default. Released dependencies: HTTP v1.5.16 and Discover v1.5.19; no local replace.

## AES-GCM configuration compatibility

Released dependencies select crypto v1.5.9. Shared Symmetric.Decrypt/DecryptBytes authenticate `12-byte IV || ciphertext || 16-byte tag` first, then try the configured legacy IV. Existing APIs remain unchanged. Prefer EncryptBytesAutoIV for new data and upgrade readers before enabling writers. CBC/SM4 formats are unchanged.

## Discover asynchronous recovery (unreleased)

Register enables background Discover recovery without waiting for Redis before HTTP startup. `discover recovery started` means worker ownership, not successful registration. Default discovery health reports 503 until registration recovers, then 200; custom handlers override it. Service Stop joins its recovery workers. Use a separate liveness probe to keep the process running during registry outages.

Development validation uses GOWORK=off and explicit ../discover and ../redis replacements. No release or deployment.
