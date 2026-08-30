package jira

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/agelxnash/go-repo-orchestrator/internal/config"
)

type testCertPair struct {
	certPEM []byte
	keyPEM  []byte
	cert    *x509.Certificate
}

// genTestCA создаёт тестовый корневой сертификат.
func genTestCA(t *testing.T) (*x509.Certificate, *ecdsa.PrivateKey) {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "gbc-test-ca"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}

	return cert, key
}

// genTestCert создаёт сертификат, подписанный указанным CA.
func genTestCert(t *testing.T, caCert *x509.Certificate, caKey *ecdsa.PrivateKey, cn string) testCertPair {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: cn},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth},
		// httptest слушает на loopback, поэтому серверный сертификат требует IP SAN.
		IPAddresses: []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, caCert, &key.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}

	return testCertPair{
		certPEM: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		keyPEM:  pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: marshalECKey(t, key)}),
		cert:    cert,
	}
}

func marshalECKey(t *testing.T, key *ecdsa.PrivateKey) []byte {
	t.Helper()

	der, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}

	return der
}

func writeTemp(t *testing.T, name string, data []byte) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}

	return path
}

func TestBuildGroupHTTPClientRequiresClientCert(t *testing.T) {
	t.Parallel()

	caCert, caKey := genTestCA(t)
	client := genTestCert(t, caCert, caKey, "gbc-client")
	serverCert := genTestCert(t, caCert, caKey, "localhost")

	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"issues":[]}`))
	}))
	caPool := x509.NewCertPool()
	caPool.AddCert(caCert)
	server.TLS = &tls.Config{
		Certificates: []tls.Certificate{certFromPair(t, serverCert)},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    caPool,
	}
	server.StartTLS()
	defer server.Close()

	// Без клиентского сертификата сервер обязан отклонить соединение.
	plainClient := &http.Client{Timeout: 5 * time.Second}
	if resp, err := plainClient.Get(server.URL); err == nil {
		_ = resp.Body.Close()
		t.Fatal("expected TLS handshake failure without client certificate")
	}

	httpClient, err := buildGroupHTTPClient(5*time.Second, config.JiraSSL{
		ClientCert: writeTemp(t, "client.pem", concatPEM(client)),
		ClientKey:  writeTemp(t, "client.key", client.keyPEM),
		CACert:     writeTemp(t, "ca.pem", pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caCert.Raw})),
	})
	if err != nil {
		t.Fatalf("buildGroupHTTPClient: %v", err)
	}

	resp, err := httpClient.Get(server.URL)
	if err != nil {
		t.Fatalf("mTLS request failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

func TestBuildGroupHTTPClientCombinedPEM(t *testing.T) {
	t.Parallel()

	caCert, caKey := genTestCA(t)
	client := genTestCert(t, caCert, caKey, "gbc-client")
	combined := writeTemp(t, "combined.pem", append(append([]byte{}, client.certPEM...), client.keyPEM...))

	httpClient, err := buildGroupHTTPClient(time.Second, config.JiraSSL{ClientCert: combined})
	if err != nil {
		t.Fatalf("combined PEM: %v", err)
	}

	transport, ok := httpClient.Transport.(*http.Transport)
	if !ok {
		t.Fatal("expected http.Transport")
	}
	if len(transport.TLSClientConfig.Certificates) != 1 {
		t.Fatalf("expected 1 client certificate, got %d", len(transport.TLSClientConfig.Certificates))
	}
}

func TestBuildGroupHTTPClientVerifyFalseSkipsCA(t *testing.T) {
	t.Parallel()

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()

	verifyFalse := false
	httpClient, err := buildGroupHTTPClient(time.Second, config.JiraSSL{Verify: &verifyFalse})
	if err != nil {
		t.Fatalf("verify=false: %v", err)
	}

	resp, err := httpClient.Get(server.URL)
	if err != nil {
		t.Fatalf("expected insecure request to self-signed server, got %v", err)
	}
	_ = resp.Body.Close()
}

func TestBuildGroupHTTPClientCACertTrustsServer(t *testing.T) {
	t.Parallel()

	caCert, caKey := genTestCA(t)
	serverCert := genTestCert(t, caCert, caKey, "localhost")

	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	server.TLS = &tls.Config{Certificates: []tls.Certificate{certFromPair(t, serverCert)}}
	server.StartTLS()
	defer server.Close()

	httpClient, err := buildGroupHTTPClient(time.Second, config.JiraSSL{
		CACert: writeTemp(t, "ca.pem", pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caCert.Raw})),
	})
	if err != nil {
		t.Fatalf("ca_cert: %v", err)
	}

	transport := httpClient.Transport.(*http.Transport)
	if transport.TLSClientConfig.InsecureSkipVerify {
		t.Fatal("InsecureSkipVerify must stay false when ca_cert is used")
	}

	resp, err := httpClient.Get(server.URL)
	if err != nil {
		t.Fatalf("expected trusted CA request to pass: %v", err)
	}
	_ = resp.Body.Close()
}

func TestBuildGroupHTTPClientDecryptsEncryptedKey(t *testing.T) {
	t.Parallel()

	caCert, caKey := genTestCA(t)
	client := genTestCert(t, caCert, caKey, "gbc-client")

	der, err := x509.MarshalECPrivateKey(clientKeyFromPEM(t, client.keyPEM))
	if err != nil {
		t.Fatal(err)
	}
	encryptedPEM, err := x509.EncryptPEMBlock( //nolint:staticcheck // генерация traditional encrypted PEM для теста расшифровки
		rand.Reader, "EC PRIVATE KEY", der, []byte("s3cr3t"), x509.PEMCipherAES256,
	)
	if err != nil {
		t.Fatal(err)
	}

	httpClient, err := buildGroupHTTPClient(time.Second, config.JiraSSL{
		ClientCert:        writeTemp(t, "client.pem", client.certPEM),
		ClientKey:         writeTemp(t, "client.key", pem.EncodeToMemory(encryptedPEM)),
		ClientKeyPassword: "s3cr3t",
	})
	if err != nil {
		t.Fatalf("encrypted key: %v", err)
	}

	transport := httpClient.Transport.(*http.Transport)
	if len(transport.TLSClientConfig.Certificates) != 1 {
		t.Fatalf("expected decrypted certificate, got %d", len(transport.TLSClientConfig.Certificates))
	}
}

func TestBuildGroupHTTPClientFailsOnWrongPassword(t *testing.T) {
	t.Parallel()

	caCert, caKey := genTestCA(t)
	client := genTestCert(t, caCert, caKey, "gbc-client")

	der, err := x509.MarshalECPrivateKey(clientKeyFromPEM(t, client.keyPEM))
	if err != nil {
		t.Fatal(err)
	}
	encryptedPEM, err := x509.EncryptPEMBlock( //nolint:staticcheck // генерация traditional encrypted PEM для теста ошибки пароля
		rand.Reader, "EC PRIVATE KEY", der, []byte("s3cr3t"), x509.PEMCipherAES256,
	)
	if err != nil {
		t.Fatal(err)
	}

	_, err = buildGroupHTTPClient(time.Second, config.JiraSSL{
		ClientCert:        writeTemp(t, "client.pem", client.certPEM),
		ClientKey:         writeTemp(t, "client.key", pem.EncodeToMemory(encryptedPEM)),
		ClientKeyPassword: "wrong-password",
	})
	if err == nil {
		t.Fatal("expected wrong password error")
	}
}

func TestWithGroupConfigsFailsFastOnBadSSL(t *testing.T) {
	t.Parallel()

	_, err := WithGroupConfigs(time.Second, []config.JiraConfig{{
		Group: "MTLS",
		URL:   "https://jira.corp.local",
		SSL:   config.JiraSSL{ClientCert: "/nonexistent/client.pem"},
	}})
	if err == nil {
		t.Fatal("expected fail-fast error for missing certificate file")
	}
}

func TestResolveStatusUsesGroupMTLSClient(t *testing.T) {
	t.Parallel()

	caCert, caKey := genTestCA(t)
	client := genTestCert(t, caCert, caKey, "gbc-client")
	serverCert := genTestCert(t, caCert, caKey, "localhost")

	var gotAuthHeader string
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuthHeader = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"issues":[{"key":"MTLS-1","fields":{"status":{"name":"Done"}}}]}`))
	}))
	caPool := x509.NewCertPool()
	caPool.AddCert(caCert)
	server.TLS = &tls.Config{
		Certificates: []tls.Certificate{certFromPair(t, serverCert)},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    caPool,
	}
	server.StartTLS()
	defer server.Close()

	opts, err := WithGroupConfigs(time.Second, []config.JiraConfig{{
		Group: "MTLS",
		URL:   server.URL,
		Token: "test-token",
		SSL: config.JiraSSL{
			ClientCert: writeTemp(t, "client.pem", concatPEM(client)),
			ClientKey:  writeTemp(t, "client.key", client.keyPEM),
			CACert:     writeTemp(t, "ca.pem", pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caCert.Raw})),
		},
	}})
	if err != nil {
		t.Fatalf("WithGroupConfigs: %v", err)
	}

	svc := NewStatusService(time.Second, opts)
	result := svc.ResolveStatus("MTLS", "", server.URL, "MTLS-1")
	if result.State == StatusStateError {
		t.Fatalf("expected successful mTLS resolve, got %+v", result)
	}
	if gotAuthHeader != "Bearer test-token" {
		t.Fatalf("expected bearer auth over mTLS, got %q", gotAuthHeader)
	}
}

func concatPEM(pair testCertPair) []byte {
	return append(append([]byte{}, pair.certPEM...), pair.keyPEM...)
}

func certFromPair(t *testing.T, pair testCertPair) tls.Certificate {
	t.Helper()

	cert, err := tls.X509KeyPair(pair.certPEM, pair.keyPEM)
	if err != nil {
		t.Fatal(err)
	}

	return cert
}

func clientKeyFromPEM(t *testing.T, keyPEM []byte) *ecdsa.PrivateKey {
	t.Helper()

	block, _ := pem.Decode(keyPEM)
	if block == nil {
		t.Fatal("invalid key PEM")
	}
	key, err := x509.ParseECPrivateKey(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}

	return key
}
