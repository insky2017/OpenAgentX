package remotehttps

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"sync"
	"time"
)

// ServerConfig describes the daemon-side Remote Worker Gateway certificate
// material. TLSConfig is useful for tests and for deployments loading secrets
// from a secret manager; file fields are used by the CLI/service wiring.
type ServerConfig struct {
	ListenAddr string
	CAFile     string
	CertFile   string
	KeyFile    string
	TLSConfig  *tls.Config
}

// LoadServerTLSConfig builds a TLS 1.3 server config that requires and verifies
// a client certificate. ClientCAs is deliberately separate from RootCAs: the
// former authenticates remote Workers, while RootCAs is not used as an
// application authorization decision.
func LoadServerTLSConfig(config ServerConfig) (*tls.Config, error) {
	if config.CAFile == "" || config.CertFile == "" || config.KeyFile == "" {
		return nil, fmt.Errorf("mTLS server CA, certificate and key files are required")
	}
	caPEM, err := os.ReadFile(config.CAFile)
	if err != nil {
		return nil, fmt.Errorf("read mTLS client CA: %w", err)
	}
	clientCAs := x509.NewCertPool()
	if !clientCAs.AppendCertsFromPEM(caPEM) {
		return nil, fmt.Errorf("invalid mTLS client CA certificate")
	}
	certificate, err := tls.LoadX509KeyPair(config.CertFile, config.KeyFile)
	if err != nil {
		return nil, fmt.Errorf("load mTLS server certificate: %w", err)
	}
	return &tls.Config{
		MinVersion: tls.VersionTLS13, MaxVersion: tls.VersionTLS13,
		ClientAuth: tls.RequireAndVerifyClientCert, ClientCAs: clientCAs,
		Certificates:  []tls.Certificate{certificate},
		Renegotiation: tls.RenegotiateNever,
	}, nil
}

// NewConfiguredServer loads certificate files and constructs a server.
func NewConfiguredServer(config ServerConfig, handler http.Handler, logger *slog.Logger) (*Server, error) {
	tlsConfig := config.TLSConfig
	var err error
	if tlsConfig == nil {
		tlsConfig, err = LoadServerTLSConfig(config)
		if err != nil {
			return nil, err
		}
	}
	return NewServer(config.ListenAddr, handler, tlsConfig, logger)
}

type Server struct {
	listenAddr string
	handler    http.Handler
	tlsConfig  *tls.Config
	logger     *slog.Logger
	ready      chan error
	mu         sync.RWMutex
	listener   net.Listener
}

// NewServer creates a daemon-side HTTPS Worker Gateway. The server rejects
// weaker TLS configs rather than silently downgrading its security boundary.
func NewServer(listenAddr string, handler http.Handler, tlsConfig *tls.Config, logger *slog.Logger) (*Server, error) {
	if listenAddr == "" || handler == nil || tlsConfig == nil {
		return nil, fmt.Errorf("HTTPS listen address, handler and TLS config are required")
	}
	if tlsConfig.MinVersion < tls.VersionTLS13 || tlsConfig.MaxVersion != 0 && tlsConfig.MaxVersion < tls.VersionTLS13 {
		return nil, fmt.Errorf("remote Worker HTTPS requires TLS 1.3")
	}
	if tlsConfig.ClientAuth != tls.RequireAndVerifyClientCert || tlsConfig.ClientCAs == nil {
		return nil, fmt.Errorf("remote Worker HTTPS requires verified mTLS client certificates")
	}
	if len(tlsConfig.Certificates) == 0 {
		return nil, fmt.Errorf("remote Worker HTTPS server certificate is required")
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Server{listenAddr: listenAddr, handler: handler, tlsConfig: tlsConfig.Clone(), logger: logger, ready: make(chan error, 1)}, nil
}

// WaitReady waits until the TCP listener has been bound. It allows daemon
// startup to fail closed when a configured remote binding cannot start.
func (s *Server) WaitReady(ctx context.Context) error {
	select {
	case err := <-s.ready:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Addr returns the bound listener address after WaitReady succeeds. It is
// primarily useful for tests using :0; production deployments normally use a
// fixed private address from configuration.
func (s *Server) Addr() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.listener == nil {
		return ""
	}
	return s.listener.Addr().String()
}

func (s *Server) Start(ctx context.Context) error {
	listener, err := net.Listen("tcp", s.listenAddr)
	if err != nil {
		s.ready <- err
		return fmt.Errorf("listen remote Worker HTTPS: %w", err)
	}
	s.mu.Lock()
	s.listener = listener
	s.mu.Unlock()
	s.ready <- nil
	tlsListener := tls.NewListener(listener, s.tlsConfig)
	httpServer := &http.Server{
		Handler:           s.handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       40 * time.Second,
		WriteTimeout:      40 * time.Second,
		IdleTimeout:       90 * time.Second,
	}
	serveErr := make(chan error, 1)
	serveDone := make(chan struct{})
	go func() {
		defer close(serveDone)
		if err := httpServer.Serve(tlsListener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErr <- err
		}
	}()
	select {
	case <-ctx.Done():
		// Give already-running HTTP handlers a short grace period, then force
		// close. http.Server.Shutdown cannot observe a TLS connection that is
		// still waiting for its ClientHello, so an unbounded graceful shutdown
		// would otherwise wait for ReadHeaderTimeout and leave listener
		// lifecycle uncertain.
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
		shutdownErr := httpServer.Shutdown(shutdownCtx)
		cancel()
		if shutdownErr != nil {
			if closeErr := httpServer.Close(); closeErr != nil {
				return fmt.Errorf("shutdown remote Worker HTTPS: %w (force close: %v)", shutdownErr, closeErr)
			}
			s.logger.Warn("forced remote Worker HTTPS shutdown after graceful timeout", "error", shutdownErr)
		}
		<-serveDone
		return nil
	case err := <-serveErr:
		<-serveDone
		return err
	case <-serveDone:
		return nil
	}
}
