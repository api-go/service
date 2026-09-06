package service

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"net"
	"net/http"
	"testing"
	"time"
)

func testCertificate(t *testing.T, serial int64) (string, string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(serial),
		Subject:      pkix.Name{CommonName: "reload.test"},
		DNSNames:     []string{"reload.test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})), string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}))
}

func peerCertificateSerial(t *testing.T, addr string) int64 {
	t.Helper()
	conn, err := tls.Dial("tcp", addr, &tls.Config{ServerName: "reload.test", InsecureSkipVerify: true})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	return conn.ConnectionState().PeerCertificates[0].SerialNumber.Int64()
}

func TestWebServerStopContinuesWaitingAfterTimeout(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	server := &http.Server{Handler: http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		close(started)
		<-release
	})}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = server.Serve(listener) }()

	ws := NewWebServer()
	ws.running = true
	ws.servers = []*http.Server{server}
	ws.listeners = []net.Listener{listener}
	go func() { _, _ = http.Get("http://" + listener.Addr().String()) }()
	<-started

	firstCtx, firstCancel := context.WithCancel(context.Background())
	firstCancel()
	if err = ws.Stop(firstCtx); !errors.Is(err, context.Canceled) {
		t.Fatalf("first Stop error = %v, want context canceled", err)
	}

	secondDone := make(chan error, 1)
	go func() { secondDone <- ws.Stop(context.Background()) }()
	select {
	case err = <-secondDone:
		t.Fatalf("second Stop returned before request finished: %v", err)
	default:
	}

	close(release)
	if err = <-secondDone; err != nil {
		t.Fatalf("second Stop error = %v", err)
	}
}

func TestWebServerReconcileListenersAddsAndRemoves(t *testing.T) {
	ws := NewWebServer()
	ws.Config.Listen = "127.0.0.1:0,http"
	if err := ws.Start(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ws.Stop(context.Background()) }()

	originalAddr := ws.Addrs[0]
	originalConfig := ws.listenConfigs[0]
	if err := ws.reconcileListeners([]listenConfig{originalConfig, {addr: "127.0.0.1:0", protocol: "h2c"}}); err != nil {
		t.Fatal(err)
	}
	if len(ws.Addrs) != 2 || ws.Addrs[0] != originalAddr {
		t.Fatalf("listeners after add = %v, want retained %s plus one", ws.Addrs, originalAddr)
	}
	addedAddr := ws.Addrs[1]
	if response, err := http.Get("http://" + addedAddr + "/__CHECK__"); err != nil || response.StatusCode != http.StatusNotFound {
		t.Fatalf("added listener response = %v, %v", response, err)
	} else {
		_ = response.Body.Close()
	}

	if err := ws.reconcileListeners([]listenConfig{ws.listenConfigs[1]}); err != nil {
		t.Fatal(err)
	}
	if len(ws.Addrs) != 1 || ws.Addrs[0] != addedAddr {
		t.Fatalf("listeners after remove = %v, want %s", ws.Addrs, addedAddr)
	}
	deadline := time.Now().Add(time.Second)
	for {
		conn, err := net.DialTimeout("tcp", originalAddr, 20*time.Millisecond)
		if err != nil {
			break
		}
		_ = conn.Close()
		if time.Now().After(deadline) {
			t.Fatalf("removed listener %s still accepts connections", originalAddr)
		}
	}
}

func TestWebServerReconcileListenersRollsBackFailedAdd(t *testing.T) {
	ws := NewWebServer()
	ws.Config.Listen = "127.0.0.1:0,http"
	if err := ws.Start(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ws.Stop(context.Background()) }()

	blocked, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer blocked.Close()
	originalAddr := ws.Addrs[0]
	err = ws.reconcileListeners([]listenConfig{
		ws.listenConfigs[0],
		{addr: blocked.Addr().String(), protocol: "http"},
	})
	if err == nil {
		t.Fatal("expected occupied listener error")
	}
	if len(ws.Addrs) != 1 || ws.Addrs[0] != originalAddr {
		t.Fatalf("listeners changed after failed add: %v", ws.Addrs)
	}
	if response, getErr := http.Get("http://" + originalAddr + "/__CHECK__"); getErr != nil || response.StatusCode != http.StatusNotFound {
		t.Fatalf("retained listener response = %v, %v", response, getErr)
	} else {
		_ = response.Body.Close()
	}
}

func TestWebServerTLSCertificateReloadAppliesToNewConnections(t *testing.T) {
	firstCert, firstKey := testCertificate(t, 1)
	secondCert, secondKey := testCertificate(t, 2)
	ws := NewWebServer()
	ws.Config.Listen = "127.0.0.1:0,https"
	ws.Config.SSL = map[string]*CertSet{"reload.test": {Cert: firstCert, Key: firstKey}}
	if err := ws.Start(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ws.Stop(context.Background()) }()

	if serial := peerCertificateSerial(t, ws.Addrs[0]); serial != 1 {
		t.Fatalf("initial certificate serial = %d, want 1", serial)
	}
	ws.Config.SSL = map[string]*CertSet{"reload.test": {Cert: secondCert, Key: secondKey}}
	if err := ws.reloadTLSCertificates(); err != nil {
		t.Fatal(err)
	}
	if serial := peerCertificateSerial(t, ws.Addrs[0]); serial != 2 {
		t.Fatalf("reloaded certificate serial = %d, want 2", serial)
	}

	ws.Config.SSL = map[string]*CertSet{"reload.test": {Cert: "invalid", Key: "invalid"}}
	if err := ws.reloadTLSCertificates(); err == nil {
		t.Fatal("expected invalid certificate reload to fail")
	}
	if serial := peerCertificateSerial(t, ws.Addrs[0]); serial != 2 {
		t.Fatalf("certificate changed after failed reload: serial = %d, want 2", serial)
	}
}
