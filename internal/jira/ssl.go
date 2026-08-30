package jira

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/agelxnash/go-repo-orchestrator/internal/config"
)

// buildGroupHTTPClient строит http.Client с TLS-настройками jira-группы.
// Возвращает nil, если ssl-блок не задан: тогда используется общий клиент сервиса.
func buildGroupHTTPClient(timeout time.Duration, ssl config.JiraSSL) (*http.Client, error) {
	if ssl.IsZero() {
		return nil, nil
	}

	tlsConf := &tls.Config{}

	if ssl.Verify != nil && !*ssl.Verify {
		// явный opt-out из конфигурации для тестовых сред
		tlsConf.InsecureSkipVerify = true
	}

	if ssl.CACert != "" {
		caPEM, err := os.ReadFile(ssl.CACert)
		if err != nil {
			return nil, fmt.Errorf("прочитать jira ssl.ca_cert %q: %w", ssl.CACert, err)
		}
		pool, err := certPoolFromPEM(caPEM)
		if err != nil {
			return nil, fmt.Errorf("разобрать jira ssl.ca_cert %q: %w", ssl.CACert, err)
		}
		tlsConf.RootCAs = pool
	}

	if ssl.ClientCert != "" {
		certPEM, err := os.ReadFile(ssl.ClientCert)
		if err != nil {
			return nil, fmt.Errorf("прочитать jira ssl.client_cert %q: %w", ssl.ClientCert, err)
		}

		keyPEM := certPEM
		if ssl.ClientKey != "" {
			keyPEM, err = os.ReadFile(ssl.ClientKey)
			if err != nil {
				return nil, fmt.Errorf("прочитать jira ssl.client_key %q: %w", ssl.ClientKey, err)
			}
		}

		if len(keyPEM) == 0 {
			return nil, fmt.Errorf("jira ssl.client_cert %q: пустой файл ключа", ssl.ClientCert)
		}

		keyPEM, err = decryptPEMKeyIfNeeded(keyPEM, ssl.ClientKeyPassword)
		if err != nil {
			return nil, err
		}

		cert, err := tls.X509KeyPair(certPEM, keyPEM)
		if err != nil {
			return nil, fmt.Errorf("собрать клиентский сертификат jira (client_cert=%q, client_key=%q): %w", ssl.ClientCert, ssl.ClientKey, err)
		}
		tlsConf.Certificates = []tls.Certificate{cert}
	}

	return &http.Client{
		Timeout:   timeout,
		Transport: &http.Transport{TLSClientConfig: tlsConf},
	}, nil
}

// certPoolFromPEM создаёт пул доверенных сертификатов из PEM-данных.
func certPoolFromPEM(caPEM []byte) (*x509.CertPool, error) {
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caPEM) {
		return nil, fmt.Errorf("в файле не найдено ни одного валидного сертификата PEM")
	}
	return pool, nil
}

// decryptPEMKeyIfNeeded расшифровывает традиционный зашифрованный PEM-ключ
// (openssl traditional format, заголовок Proc-Type: 4,ENCRYPTED).
func decryptPEMKeyIfNeeded(keyPEM []byte, password string) ([]byte, error) {
	block, _ := pem.Decode(keyPEM)
	if block == nil {
		return nil, fmt.Errorf("файл ключа jira не является валидным PEM")
	}
	if block.Headers["Proc-Type"] != "4,ENCRYPTED" {
		return keyPEM, nil
	}

	if password == "" {
		return nil, fmt.Errorf("ключ jira зашифрован, но ssl.client_key_password не задан")
	}

	der, err := x509.DecryptPEMBlock(block, []byte(password)) //nolint:staticcheck // традиционные PEM-ключи; PKCS#8-шифрование вне текущего скопа
	if err != nil {
		return nil, fmt.Errorf("расшифровать ключ jira (проверьте ssl.client_key_password): %w", err)
	}

	return pem.EncodeToMemory(&pem.Block{Type: block.Type, Bytes: der}), nil
}
