package service

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	goCrypto "apigo.cc/go/crypto"
	goEncoding "apigo.cc/go/encoding"
	"apigo.cc/go/file"
	"apigo.cc/go/safe"
)

var (
	serviceConfigAES *goCrypto.Symmetric
	serviceAESMu     sync.RWMutex
)

func init() {
	goCrypto.OnSetDefaultAES(func(aes *goCrypto.Symmetric) {
		serviceAESMu.Lock()
		serviceConfigAES = aes
		serviceAESMu.Unlock()
	})
}

func decodeConfigSecret(value []byte) []byte {
	serviceAESMu.RLock()
	aes := serviceConfigAES
	serviceAESMu.RUnlock()
	if aes != nil {
		decoded, decodeErr := goEncoding.UnURLBase64(strings.TrimSpace(string(value)))
		if decodeErr == nil && len(decoded) > 0 {
			if plain, err := aes.DecryptBytes(decoded); err == nil {
				return plain
			}
		}
	}
	return append([]byte(nil), value...)
}

func encodeConfigSecret(value []byte) []byte {
	serviceAESMu.RLock()
	aes := serviceConfigAES
	serviceAESMu.RUnlock()
	if aes == nil {
		return append([]byte(nil), value...)
	}
	encrypted, err := aes.EncryptBytes(value)
	if err != nil {
		return append([]byte(nil), value...)
	}
	return []byte(goEncoding.URLBase64(encrypted))
}

func readCertificatePart(inline, fileName string) ([]byte, error) {
	value := []byte(inline)
	if fileName != "" {
		var err error
		value, err = file.ReadBytes(fileName)
		if err != nil {
			return nil, err
		}
	}
	return decodeConfigSecret(value), nil
}

func loadCertificate(set *CertSet) (tls.Certificate, error) {
	if set == nil {
		return tls.Certificate{}, fmt.Errorf("certificate config is empty")
	}
	certPEM, err := readCertificatePart(set.Cert, set.CertFile)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("read certificate: %w", err)
	}
	keyPEM, err := readCertificatePart(set.Key, set.KeyFile)
	if err != nil {
		safe.ZeroMemory(certPEM)
		return tls.Certificate{}, fmt.Errorf("read private key: %w", err)
	}
	defer safe.ZeroMemory(certPEM)
	defer safe.ZeroMemory(keyPEM)
	certificate, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("parse certificate: %w", err)
	}
	return certificate, nil
}

func (ws *WebServer) tlsConfig() (*tls.Config, error) {
	certificates := map[string]*tls.Certificate{}
	var fallback *tls.Certificate
	for host, set := range ws.Config.SSL {
		certificate, err := loadCertificate(set)
		if err != nil {
			return nil, fmt.Errorf("load TLS certificate for %q: %w", host, err)
		}
		certificates[strings.ToLower(strings.TrimSpace(host))] = &certificate
		if host == "*" {
			fallback = &certificate
		}
	}
	if fallback == nil {
		certificate, err := ws.loadOrCreateAutoCertificate()
		if err != nil {
			return nil, err
		}
		fallback = &certificate
	}
	return &tls.Config{
		MinVersion: tls.VersionTLS12,
		NextProtos: []string{"h2", "http/1.1"},
		GetCertificate: func(info *tls.ClientHelloInfo) (*tls.Certificate, error) {
			host := strings.ToLower(strings.TrimSuffix(info.ServerName, "."))
			if certificate := certificates[host]; certificate != nil {
				return certificate, nil
			}
			for pattern, certificate := range certificates {
				if strings.HasPrefix(pattern, "*.") && strings.HasSuffix(host, pattern[1:]) {
					return certificate, nil
				}
			}
			return fallback, nil
		},
	}, nil
}

func (ws *WebServer) loadOrCreateAutoCertificate() (tls.Certificate, error) {
	dir := strings.TrimSpace(ws.Config.AutoCertDir)
	if dir == "" {
		dir = filepath.Join("data", "ssl", "_auto")
	}
	certFile := filepath.Join(dir, "cert.pem")
	keyFile := filepath.Join(dir, "key.pem")
	if file.Exists(certFile) && file.Exists(keyFile) {
		return loadCertificate(&CertSet{CertFile: certFile, KeyFile: keyFile})
	}
	certPEM, keyPEM, err := generateAutoCertificate()
	if err != nil {
		return tls.Certificate{}, err
	}
	defer safe.ZeroMemory(certPEM)
	defer safe.ZeroMemory(keyPEM)
	if err = file.Mkdir(dir); err != nil {
		return tls.Certificate{}, fmt.Errorf("create auto certificate directory: %w", err)
	}
	_ = os.Chmod(dir, 0700)
	storedCert := encodeConfigSecret(certPEM)
	storedKey := encodeConfigSecret(keyPEM)
	defer safe.ZeroMemory(storedCert)
	defer safe.ZeroMemory(storedKey)
	if err = file.WriteBytes(certFile, storedCert); err != nil {
		return tls.Certificate{}, fmt.Errorf("write auto certificate: %w", err)
	}
	_ = os.Chmod(certFile, 0600)
	if err = file.WriteBytes(keyFile, storedKey); err != nil {
		return tls.Certificate{}, fmt.Errorf("write auto private key: %w", err)
	}
	_ = os.Chmod(keyFile, 0600)
	return tls.X509KeyPair(certPEM, keyPEM)
}

func generateAutoCertificate() ([]byte, []byte, error) {
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("generate private key: %w", err)
	}
	serialLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serial, err := rand.Int(rand.Reader, serialLimit)
	if err != nil {
		return nil, nil, fmt.Errorf("generate certificate serial: %w", err)
	}
	hostname, _ := os.Hostname()
	dnsNames := []string{"localhost"}
	if hostname = strings.TrimSpace(hostname); hostname != "" && !strings.EqualFold(hostname, "localhost") {
		dnsNames = append(dnsNames, hostname)
	}
	ipAddresses := []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")}
	if interfaces, listErr := net.InterfaceAddrs(); listErr == nil {
		for _, address := range interfaces {
			value, _, parseErr := net.ParseCIDR(address.String())
			if parseErr == nil && !value.IsUnspecified() {
				ipAddresses = append(ipAddresses, value)
			}
		}
	}
	now := time.Now()
	template := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "apigo local service"},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.AddDate(99, 0, 0),
		DNSNames:              dnsNames,
		IPAddresses:           ipAddresses,
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &privateKey.PublicKey, privateKey)
	if err != nil {
		return nil, nil, fmt.Errorf("create auto certificate: %w", err)
	}
	privateDER, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal private key: %w", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privateDER}), nil
}
