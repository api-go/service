package service

import (
	"crypto/tls"
	"fmt"
	"net/url"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// HostPolicy replaces all managed request policies together. Empty routes do not fall back to local routes.
// Gate and Certificate are native Go capabilities, not configuration or JS exports.
type HostPolicy struct {
	Proxies     []ProxyRule
	Rewrites    []RewriteRule
	Statics     map[string]string
	ForceHTTPS  bool
	Gate        RequestGate
	Certificate *tls.Certificate
}
type PreparedHostPolicy struct {
	proxies     []*proxyType
	rewrites    []*rewriteType
	statics     []*staticType
	forceHTTPS  bool
	gate        RequestGate
	certificate *tls.Certificate
}
type hostPolicyRegistry struct {
	policies  map[string]*PreparedHostPolicy
	wildcards []string
}

func validateManagedPath(path string) error {
	if path == "" || len(path) > 4096 {
		return fmt.Errorf("invalid route path")
	}
	if strings.ContainsRune(path, '(') {
		if _, err := regexp.Compile("^" + path + "$"); err != nil {
			return err
		}
	} else if !strings.HasPrefix(path, "/") {
		return fmt.Errorf("path must begin with /")
	}
	return nil
}

// PrepareHostPolicy validates and compiles off the request path; rejected input has no effects.
func PrepareHostPolicy(policy HostPolicy) (*PreparedHostPolicy, error) {
	prepared := &PreparedHostPolicy{forceHTTPS: policy.ForceHTTPS, gate: policy.Gate}
	if policy.Certificate != nil {
		copied := *policy.Certificate
		copied.Certificate = make([][]byte, len(policy.Certificate.Certificate))
		for index, der := range policy.Certificate.Certificate {
			copied.Certificate[index] = append([]byte(nil), der...)
		}
		prepared.certificate = &copied
	}
	for _, rule := range policy.Proxies {
		if err := validateManagedPath(rule.Path); err != nil {
			return nil, err
		}
		parsed := parseProxyRule(rule.AuthLevel, rule.Path, rule.ToApp, rule.ToPath, rule.To)
		if parsed.toApp == "" || rule.AuthLevel < 0 {
			return nil, fmt.Errorf("invalid proxy target or auth level")
		}
		if strings.Contains(parsed.toApp, "://") {
			target, err := url.Parse(parsed.toApp)
			if err != nil || (target.Scheme != "http" && target.Scheme != "https") || target.Host == "" || target.User != nil {
				return nil, fmt.Errorf("invalid HTTP proxy target")
			}
		}
		prepared.proxies = append(prepared.proxies, parsed)
	}
	for _, rule := range policy.Rewrites {
		if err := validateManagedPath(rule.Path); err != nil {
			return nil, err
		}
		parsed := parseRewriteRule(rule.Path, rule.ToPath, rule.To)
		if parsed.toPath == "" {
			return nil, fmt.Errorf("empty rewrite target")
		}
		target, err := url.Parse(parsed.toPath)
		if err != nil || (target.IsAbs() && target.Scheme != "http" && target.Scheme != "https") || strings.HasPrefix(parsed.toPath, "//") {
			return nil, fmt.Errorf("invalid rewrite target")
		}
		prepared.rewrites = append(prepared.rewrites, parsed)
	}
	for path, root := range policy.Statics {
		if path == "" || !strings.HasPrefix(path, "/") || root == "" {
			return nil, fmt.Errorf("invalid static mount")
		}
		absolute, err := filepath.Abs(root)
		if err != nil {
			return nil, err
		}
		prepared.statics = append(prepared.statics, &staticType{path: path, rootPath: &absolute})
	}
	sort.Slice(prepared.statics, func(i, j int) bool { return len(prepared.statics[i].path) > len(prepared.statics[j].path) })
	return prepared, nil
}

// ReplaceHostPolicies atomically publishes an entire prepared registry; callers retain immutable policies.
func (ws *WebServer) ReplaceHostPolicies(policies map[string]*PreparedHostPolicy) error {
	registry := &hostPolicyRegistry{policies: make(map[string]*PreparedHostPolicy, len(policies))}
	for host, policy := range policies {
		host = strings.ToLower(strings.TrimSpace(host))
		if host == "" || policy == nil {
			return fmt.Errorf("empty host or policy")
		}
		if _, exists := registry.policies[host]; exists {
			return fmt.Errorf("duplicate host %q", host)
		}
		registry.policies[host] = policy
		if strings.HasPrefix(host, "*.") {
			registry.wildcards = append(registry.wildcards, host)
		}
	}
	sort.Slice(registry.wildcards, func(i, j int) bool { return len(registry.wildcards[i]) > len(registry.wildcards[j]) })
	ws.managedPolicies.Store(registry)
	return nil
}
func (r *hostPolicyRegistry) match(host string) *PreparedHostPolicy {
	if r == nil {
		return nil
	}
	candidates := hostCandidates(host)
	for _, candidate := range candidates {
		if candidate == "*" {
			continue
		}
		if policy := r.policies[candidate]; policy != nil {
			return policy
		}
	}
	name := strings.ToLower(strings.TrimSuffix(hostOnly(host), "."))
	for _, pattern := range r.wildcards {
		if strings.HasSuffix(name, pattern[1:]) {
			return r.policies[pattern]
		}
	}
	return r.policies["*"]
}

// CertificateProvider supplies externally managed certificates (for example ACME).
// Return (nil, nil) to use managed/local certificates; an error fails the handshake.
type CertificateProvider func(*tls.ClientHelloInfo) (*tls.Certificate, error)
type certificateProviderEntry struct{ provider CertificateProvider }

func (ws *WebServer) SetCertificateProvider(provider CertificateProvider) {
	ws.certificateProvider.Store(&certificateProviderEntry{provider: provider})
}
