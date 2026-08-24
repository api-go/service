package service

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"apigo.cc/go/log"
)

func TestParseListenConfigs(t *testing.T) {
	configs, err := parseListenConfigs(":80,http|443,https|127.0.0.1:9000,h2c")
	if err != nil {
		t.Fatal(err)
	}
	if len(configs) != 3 || configs[0].addr != ":80" || configs[1].addr != ":443" || configs[1].protocol != "https" || configs[2].protocol != "h2c" {
		t.Fatalf("unexpected configs: %#v", configs)
	}
}

func TestMultiListenerWithAutoTLS(t *testing.T) {
	server := NewWebServer()
	server.Config.Listen = "127.0.0.1:0,http|127.0.0.1:0,https"
	server.Config.AutoCertDir = t.TempDir()
	if err := server.Start(context.Background(), log.DefaultLogger); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = server.Stop(ctx)
	})
	if len(server.Addrs) != 2 {
		t.Fatalf("expected two listeners, got %v", server.Addrs)
	}
	if status, err := server.Status(); err != nil || len(strings.Split(status, "|")) != 2 {
		t.Fatalf("unexpected status %q: %v", status, err)
	}
	if response, err := http.Get("http://" + server.Addrs[0] + "/__CHECK__"); err != nil || response.StatusCode != http.StatusNotFound {
		t.Fatalf("HTTP listener failed: response=%v error=%v", response, err)
	}
	server.ReplaceForceHTTPS("example.test", true)
	redirectClient := &http.Client{CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	request, _ := http.NewRequest(http.MethodGet, "http://"+server.Addrs[0]+"/secure?q=1", nil)
	request.Host = "example.test"
	redirect, err := redirectClient.Do(request)
	if err != nil || redirect.StatusCode != http.StatusPermanentRedirect || !strings.HasPrefix(redirect.Header.Get("Location"), "https://example.test:") {
		t.Fatalf("unexpected HTTPS redirect: response=%v location=%q error=%v", redirect, redirect.Header.Get("Location"), err)
	}
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}} // test-only self-signed certificate
	if response, err := client.Get("https://" + server.Addrs[1] + "/__CHECK__"); err != nil || response.StatusCode != http.StatusNotFound {
		t.Fatalf("HTTPS listener failed: response=%v error=%v", response, err)
	}
	certPEM, err := os.ReadFile(filepath.Join(server.Config.AutoCertDir, "cert.pem"))
	if err != nil {
		t.Fatal(err)
	}
	certPEM = decodeConfigSecret(certPEM)
	block, _ := pem.Decode(certPEM)
	certificate, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	if certificate.NotAfter.Before(time.Now().AddDate(98, 11, 0)) {
		t.Fatalf("auto certificate validity is too short: %v", certificate.NotAfter)
	}
	if _, err = os.Stat(filepath.Join(server.Config.AutoCertDir, "key.pem")); err != nil {
		t.Fatal(err)
	}
}
