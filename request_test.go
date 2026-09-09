package service

import (
	"net/http/httptest"
	"testing"
)

func TestClientIpRejectsUntrustedProxyHeaders(t *testing.T) {
	raw := httptest.NewRequest("GET", "http://example.test", nil)
	raw.RemoteAddr = "192.0.2.10:4321"
	raw.Header.Set("X-Real-IP", "203.0.113.9")
	raw.Header.Set("X-Forwarded-For", "203.0.113.8, 198.51.100.2")
	request := NewRequest(raw)
	if got := request.ClientIp(); got != "192.0.2.10" {
		t.Fatalf("ClientIp() = %q, want direct peer", got)
	}
}

func TestClientIpUsesTrustedRealIPAndFirstForwardedAddress(t *testing.T) {
	raw := httptest.NewRequest("GET", "http://example.test", nil)
	raw.RemoteAddr = "192.0.2.10:4321"
	request := NewRequest(raw)
	request.trustProxyClientIP = true
	raw.Header.Set("X-Real-IP", "203.0.113.9")
	if got := request.ClientIp(); got != "203.0.113.9" {
		t.Fatalf("trusted X-Real-IP = %q", got)
	}
	raw.Header.Del("X-Real-IP")
	raw.Header.Set("X-Forwarded-For", "203.0.113.8, 198.51.100.2")
	if got := request.ClientIp(); got != "203.0.113.8" {
		t.Fatalf("first X-Forwarded-For = %q", got)
	}
}

func TestClientIpSkipsMalformedForwardedAddresses(t *testing.T) {
	raw := httptest.NewRequest("GET", "http://example.test", nil)
	raw.RemoteAddr = "[2001:db8::10]:4321"
	request := NewRequest(raw)
	request.trustProxyClientIP = true
	raw.Header.Set("X-Real-IP", "not-an-ip")
	raw.Header.Set("X-Forwarded-For", "unknown, 2001:db8::8")
	if got := request.ClientIp(); got != "2001:db8::8" {
		t.Fatalf("validated forwarded IP = %q", got)
	}
}
