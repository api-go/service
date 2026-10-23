package service

import (
	gohttp "apigo.cc/go/http"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strings"
)

// ClientIPPolicy selects legacy internal relay handling, direct peers, or explicitly trusted proxies.
// An empty Mode retains the existing Request-ID based behavior for internal services.
type ClientIPPolicy struct {
	Mode           string
	TrustedProxies []string
	Header         string
}
type compiledClientIPPolicy struct {
	mode     string
	networks []netip.Prefix
	header   string
}

func compileClientIPPolicy(policy ClientIPPolicy) (*compiledClientIPPolicy, error) {
	mode := policy.Mode
	if mode == "" {
		mode = "legacy"
	}
	if mode != "legacy" && mode != "direct" && mode != "trusted" {
		return nil, fmt.Errorf("invalid client IP mode %q", mode)
	}
	header := http.CanonicalHeaderKey(policy.Header)
	if header == "" {
		header = "X-Forwarded-For"
	}
	if header != "X-Forwarded-For" && header != "X-Real-Ip" && header != "X-Client-Ip" {
		return nil, fmt.Errorf("unsupported client IP header")
	}
	compiled := &compiledClientIPPolicy{mode: mode, header: header}
	for _, value := range policy.TrustedProxies {
		prefix, err := netip.ParsePrefix(value)
		if err != nil {
			addr, addrErr := netip.ParseAddr(value)
			if addrErr != nil {
				return nil, fmt.Errorf("invalid trusted proxy %q", value)
			}
			prefix = netip.PrefixFrom(addr, addr.BitLen())
		}
		compiled.networks = append(compiled.networks, prefix.Masked())
	}
	if mode == "trusted" && len(compiled.networks) == 0 {
		return nil, fmt.Errorf("trusted mode requires proxy addresses or CIDRs")
	}
	return compiled, nil
}
func (p *compiledClientIPPolicy) trusts(addr netip.Addr) bool {
	for _, prefix := range p.networks {
		if prefix.Contains(addr.Unmap()) {
			return true
		}
	}
	return false
}

// SetClientIPPolicy validates and atomically replaces the policy without changing existing requests.
func (ws *WebServer) SetClientIPPolicy(policy ClientIPPolicy) error {
	compiled, err := compileClientIPPolicy(policy)
	if err != nil {
		return err
	}
	ws.clientIPPolicy.Store(compiled)
	return nil
}
func (ws *WebServer) resolveClientIP(request *Request, hadRequestID bool) {
	policy := ws.clientIPPolicy.Load()
	if policy == nil || policy.mode == "legacy" {
		request.trustProxyClientIP = hadRequestID || ws.Config.AcceptXRealIpWithoutRequestId
		request.clientIP = request.ClientIp()
		return
	}
	peer := normalizedClientIP(request.RemoteAddr)
	request.clientIP = peer
	address, err := netip.ParseAddr(peer)
	if err != nil || policy.mode == "direct" || !policy.trusts(address) {
		return
	}
	if policy.header != "X-Forwarded-For" {
		if value := normalizedClientIP(request.Header().Get(policy.header)); value != "" {
			request.clientIP = value
		}
		return
	}
	// Walk right to left; an invalid hop terminates trust rather than being skipped.
	hops := strings.Split(request.Header().Get(policy.header), ",")
	for index := len(hops) - 1; index >= 0; index-- {
		if !policy.trusts(address) {
			break
		}
		value := normalizedClientIP(hops[index])
		next, parseErr := netip.ParseAddr(value)
		if parseErr != nil {
			break
		}
		request.clientIP, address = value, next
	}
}

// Handler exposes this server's HTTP pipeline without opening a listener.
func (ws *WebServer) Handler() http.Handler { return &RouteHandler{ws: ws} }

// RequestGate runs before HTTPS redirection, rewriting, proxies and static responses.
// Returning true means the gate has completed the response. The body is not read.
type RequestGate func(*Request, *Response) bool

func (ws *WebServer) SetRequestGate(gate RequestGate) {
	ws.requestGate.Store(&requestGateEntry{gate: gate})
}

type requestGateEntry struct{ gate RequestGate }

// RelayClientIP replaces untrusted ingress IP headers with the resolved address.
// Call at the external gateway boundary before forwarding to internal services.
func RelayClientIP(request *Request) {
	ip := request.ClientIp()
	request.Request.Header.Del("X-Forwarded-For")
	request.Request.Header.Del("X-Client-Ip")
	request.Request.Header.Del("X-Real-IP")
	if net.ParseIP(ip) != nil {
		request.Request.Header.Set("X-Real-IP", ip)
		request.Request.Header.Set("X-Client-Ip", ip)
	}
}

// SetExternalAddressPolicy atomically replaces direct-peer address trust without
// changing ClientIPPolicy. Default uses the existing Request-ID relay convention;
// direct/trusted modes are optional enhancements. Existing snapshots stay fixed.
func (ws *WebServer) SetExternalAddressPolicy(policy gohttp.ExternalAddressPolicy) error {
	resolver, err := gohttp.NewExternalAddressResolver(policy)
	if err != nil {
		return err
	}
	ws.externalAddressPolicy.Store(resolver)
	return nil
}
