package unixhttp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

type Server struct {
	socketPath string
	logger     *slog.Logger
	httpServer *http.Server
	listener   net.Listener
	ownedInfo  os.FileInfo
}

func NewServer(socketPath string, handler http.Handler, logger *slog.Logger) (*Server, error) {
	if strings.TrimSpace(socketPath) == "" || handler == nil {
		return nil, fmt.Errorf("Unix HTTP socket path and handler are required")
	}
	absPath, err := filepath.Abs(socketPath)
	if err != nil {
		return nil, fmt.Errorf("resolve Unix socket path: %w", err)
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Server{
		socketPath: absPath, logger: logger,
		httpServer: &http.Server{
			Handler: handler, ReadHeaderTimeout: 5 * time.Second,
			ReadTimeout: 40 * time.Second, WriteTimeout: 40 * time.Second,
		},
	}, nil
}

func (s *Server) SocketPath() string { return s.socketPath }

func (s *Server) Start(ctx context.Context) error {
	if err := os.MkdirAll(filepath.Dir(s.socketPath), 0o700); err != nil {
		return fmt.Errorf("create Unix socket directory: %w", err)
	}
	if err := s.removeStaleSocket(); err != nil {
		return err
	}
	listener, err := net.Listen("unix", s.socketPath)
	if err != nil {
		return fmt.Errorf("listen on Unix socket: %w", err)
	}
	s.listener = listener
	if unixListener, ok := listener.(*net.UnixListener); ok {
		unixListener.SetUnlinkOnClose(false)
	}
	if err := os.Chmod(s.socketPath, 0o600); err != nil {
		_ = listener.Close()
		return fmt.Errorf("set Unix socket permission 0600: %w", err)
	}
	ownedInfo, err := os.Lstat(s.socketPath)
	if err != nil {
		_ = listener.Close()
		return fmt.Errorf("stat owned Unix socket: %w", err)
	}
	s.ownedInfo = ownedInfo

	serveErrors := make(chan error, 1)
	go func() {
		if err := s.httpServer.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErrors <- err
		}
	}()
	select {
	case <-ctx.Done():
		shutdownContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		shutdownErr := s.httpServer.Shutdown(shutdownContext)
		s.cleanupOwnedSocket()
		if shutdownErr != nil {
			return fmt.Errorf("shutdown Unix HTTP server: %w", shutdownErr)
		}
		return nil
	case err := <-serveErrors:
		s.cleanupOwnedSocket()
		return err
	}
}

func (s *Server) removeStaleSocket() error {
	info, err := os.Lstat(s.socketPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect Unix socket path: %w", err)
	}
	if info.Mode()&os.ModeSocket == 0 {
		return fmt.Errorf("Unix socket path exists and is not a socket: %s", s.socketPath)
	}
	connection, dialErr := net.DialTimeout("unix", s.socketPath, 300*time.Millisecond)
	if dialErr == nil {
		_ = connection.Close()
		return fmt.Errorf("Unix socket already has an active listener: %s", s.socketPath)
	}
	if !errors.Is(dialErr, syscall.ECONNREFUSED) && !strings.Contains(strings.ToLower(dialErr.Error()), "connection refused") {
		return fmt.Errorf("cannot prove existing Unix socket is stale: %w", dialErr)
	}
	if err := os.Remove(s.socketPath); err != nil {
		return fmt.Errorf("remove stale Unix socket: %w", err)
	}
	return nil
}

func (s *Server) cleanupOwnedSocket() {
	if s.ownedInfo == nil {
		return
	}
	current, err := os.Lstat(s.socketPath)
	if err != nil || current.Mode()&os.ModeSocket == 0 || !sameSocket(s.ownedInfo, current) {
		return
	}
	if err := os.Remove(s.socketPath); err != nil {
		s.logger.Warn("failed to remove owned Unix socket", slog.String("error", err.Error()))
	}
}

func sameSocket(left os.FileInfo, right os.FileInfo) bool {
	if left == nil || right == nil || !os.SameFile(left, right) {
		return false
	}
	leftStat, leftOK := left.Sys().(*syscall.Stat_t)
	rightStat, rightOK := right.Sys().(*syscall.Stat_t)
	if !leftOK || !rightOK {
		return true
	}
	return leftStat.Dev == rightStat.Dev && leftStat.Ino == rightStat.Ino &&
		leftStat.Ctim == rightStat.Ctim && leftStat.Mtim == rightStat.Mtim
}
