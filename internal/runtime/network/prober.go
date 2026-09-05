package network

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	"strconv"
	"strings"
	"time"

	"openagentx/internal/api"
	"openagentx/internal/domain"
)

const endpointProbeTimeout = 5 * time.Second

var ErrHTTPProxyAuthenticationRequired = errors.New("HTTP proxy authentication is required")

// ProbeEndpoint verifies that the configured address speaks the selected proxy
// protocol. SOCKS5 credentials are negotiated without opening an external
// destination. HTTP probing accepts any bounded, syntactically valid response
// to OPTIONS; it does not claim CONNECT or external network reachability.
func ProbeEndpoint(ctx context.Context, content domain.NetworkProfileContent, secret *api.NetworkSecretPayload) error {
	probeCtx, cancel := context.WithTimeout(ctx, endpointProbeTimeout)
	defer cancel()
	conn, err := (&net.Dialer{}).DialContext(probeCtx, "tcp", net.JoinHostPort(content.Host, strconv.Itoa(content.Port)))
	if err != nil {
		return domain.ErrConflict("proxy endpoint is unreachable")
	}
	defer conn.Close()
	stopCancellation := make(chan struct{})
	go func() {
		select {
		case <-probeCtx.Done():
			_ = conn.Close()
		case <-stopCancellation:
		}
	}()
	defer close(stopCancellation)
	if deadline, ok := probeCtx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	var probeErr error
	switch content.Mode {
	case "only_socks5":
		probeErr = probeSOCKS5(conn, secret)
	case "only_http_proxy":
		probeErr = probeHTTP(conn, content.Host)
	default:
		return domain.ErrUnsupportedCapability
	}
	if probeCtx.Err() != nil {
		return probeCtx.Err()
	}
	return probeErr
}

func probeSOCKS5(conn net.Conn, secret *api.NetworkSecretPayload) error {
	method := byte(0)
	if secret != nil && (secret.Username != "" || secret.Password != "") {
		if len(secret.Username) > 255 || len(secret.Password) > 255 {
			return domain.ErrInvalidInput("SOCKS5 credentials exceed protocol limits")
		}
		method = 2
	}
	if _, err := conn.Write([]byte{5, 1, method}); err != nil {
		return domain.ErrConflict("SOCKS5 negotiation failed")
	}
	response := make([]byte, 2)
	if _, err := io.ReadFull(conn, response); err != nil || response[0] != 5 || response[1] != method {
		return domain.ErrConflict("SOCKS5 authentication method was rejected")
	}
	if method == 0 {
		return nil
	}
	request := make([]byte, 0, 3+len(secret.Username)+len(secret.Password))
	request = append(request, 1, byte(len(secret.Username)))
	request = append(request, secret.Username...)
	request = append(request, byte(len(secret.Password)))
	request = append(request, secret.Password...)
	if _, err := conn.Write(request); err != nil {
		return domain.ErrConflict("SOCKS5 authentication exchange failed")
	}
	if _, err := io.ReadFull(conn, response); err != nil || response[0] != 1 || response[1] != 0 {
		return domain.ErrConflict("SOCKS5 credentials were rejected")
	}
	return nil
}

func probeHTTP(conn net.Conn, host string) error {
	request := "OPTIONS * HTTP/1.1\r\nHost: " + host + "\r\nConnection: close\r\n\r\n"
	if _, err := io.WriteString(conn, request); err != nil {
		return domain.ErrConflict("HTTP proxy protocol write failed")
	}
	line, err := bufio.NewReaderSize(conn, 4096).ReadSlice('\n')
	if err != nil || len(line) > 4096 {
		return domain.ErrConflict("HTTP proxy returned no bounded status line")
	}
	parts := strings.Fields(string(line))
	if len(parts) < 2 || (parts[0] != "HTTP/1.0" && parts[0] != "HTTP/1.1") || len(parts[1]) != 3 {
		return domain.ErrConflict("HTTP proxy returned an invalid status line")
	}
	status, err := strconv.Atoi(parts[1])
	if err != nil || status < 100 || status > 599 {
		return domain.ErrConflict("HTTP proxy returned an invalid status code")
	}
	if status == 407 {
		return ErrHTTPProxyAuthenticationRequired
	}
	return nil
}
