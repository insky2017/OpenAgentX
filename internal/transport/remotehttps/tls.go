package remotehttps

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"os"
)

type Config struct {
	CAFile     string
	CertFile   string
	KeyFile    string
	ServerName string
}

func LoadTLSConfig(config Config) (*tls.Config, error) {
	caPEM, err := os.ReadFile(config.CAFile)
	if err != nil {
		return nil, fmt.Errorf("read mTLS CA: %w", err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(caPEM) {
		return nil, fmt.Errorf("invalid mTLS CA certificate")
	}
	certificate, err := tls.LoadX509KeyPair(config.CertFile, config.KeyFile)
	if err != nil {
		return nil, fmt.Errorf("load mTLS client certificate: %w", err)
	}
	return &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: roots, Certificates: []tls.Certificate{certificate}, ServerName: config.ServerName, Renegotiation: tls.RenegotiateNever}, nil
}

func NewHTTPClient(config Config) (*http.Client, error) {
	tlsConfig, err := LoadTLSConfig(config)
	if err != nil {
		return nil, err
	}
	return &http.Client{Transport: &http.Transport{TLSClientConfig: tlsConfig}}, nil
}
