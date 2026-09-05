package network

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"strconv"
	"testing"
	"time"

	"openagentx/internal/api"
)

func TestProbeEndpointNegotiatesSOCKS5Authentication(t *testing.T) {
	listener := listenProbeServer(t)
	done := make(chan error, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			done <- err
			return
		}
		defer conn.Close()
		greeting := make([]byte, 3)
		if _, err = io.ReadFull(conn, greeting); err == nil {
			if !bytes.Equal(greeting, []byte{5, 1, 2}) {
				err = errors.New("unexpected SOCKS5 greeting")
			}
		}
		if err == nil {
			_, err = conn.Write([]byte{5, 2})
		}
		auth := make([]byte, 2+len("user")+1+len("password"))
		if err == nil {
			_, err = io.ReadFull(conn, auth)
		}
		if err == nil {
			expected := append([]byte{1, 4}, []byte("user")...)
			expected = append(expected, byte(8))
			expected = append(expected, []byte("password")...)
			if !bytes.Equal(auth, expected) {
				err = errors.New("unexpected RFC1929 authentication frame")
			}
		}
		if err == nil {
			_, err = conn.Write([]byte{1, 0})
		}
		done <- err
	}()
	host, port := probeAddress(t, listener.Addr())
	content := testNetworkContent("secret-v1")
	content.Host, content.Port = host, port
	content.ManifestDigest = content.ComputeManifestDigest()
	if err := ProbeEndpoint(context.Background(), content, &api.NetworkSecretPayload{Username: "user", Password: "password"}); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestProbeEndpointRejectsSOCKS5AuthenticationFailure(t *testing.T) {
	listener := listenProbeServer(t)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		greeting := make([]byte, 3)
		_, _ = io.ReadFull(conn, greeting)
		_, _ = conn.Write([]byte{5, 2})
		reader := bufio.NewReader(conn)
		version, _ := reader.ReadByte()
		usernameLength, _ := reader.ReadByte()
		_, _ = io.CopyN(io.Discard, reader, int64(usernameLength))
		passwordLength, _ := reader.ReadByte()
		_, _ = io.CopyN(io.Discard, reader, int64(passwordLength))
		if version == 1 {
			_, _ = conn.Write([]byte{1, 1})
		}
	}()
	host, port := probeAddress(t, listener.Addr())
	content := testNetworkContent("secret-v1")
	content.Host, content.Port = host, port
	content.ManifestDigest = content.ComputeManifestDigest()
	if err := ProbeEndpoint(context.Background(), content, &api.NetworkSecretPayload{Username: "user", Password: "wrong"}); err == nil {
		t.Fatal("rejected SOCKS5 credentials passed endpoint probe")
	}
}

func TestProbeEndpointRejectsHTTPProxyAuthenticationRequirement(t *testing.T) {
	listener := listenProbeServer(t)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		reader := bufio.NewReader(conn)
		_, _ = reader.ReadString('\n')
		_, _ = conn.Write([]byte("HTTP/1.1 407 Proxy Authentication Required\r\nContent-Length: 0\r\n\r\n"))
	}()
	host, port := probeAddress(t, listener.Addr())
	content := testNetworkContent("")
	content.Mode, content.Host, content.Port = "only_http_proxy", host, port
	content.DirectIPs = nil
	content.ManifestDigest = content.ComputeManifestDigest()
	if err := ProbeEndpoint(context.Background(), content, nil); !errors.Is(err, ErrHTTPProxyAuthenticationRequired) {
		t.Fatalf("HTTP 407 error=%v", err)
	}
}

func TestProbeEndpointAcceptsBoundedHTTPProtocolResponse(t *testing.T) {
	listener := listenProbeServer(t)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		reader := bufio.NewReader(conn)
		_, _ = reader.ReadString('\n')
		_, _ = conn.Write([]byte("HTTP/1.1 204 No Content\r\nContent-Length: 0\r\n\r\n"))
	}()
	host, port := probeAddress(t, listener.Addr())
	content := testNetworkContent("")
	content.Mode, content.Host, content.Port = "only_http_proxy", host, port
	content.DirectIPs = nil
	content.ManifestDigest = content.ComputeManifestDigest()
	if err := ProbeEndpoint(context.Background(), content, nil); err != nil {
		t.Fatal(err)
	}
}

func TestProbeEndpointCancellationInterruptsStalledRead(t *testing.T) {
	listener := listenProbeServer(t)
	accepted := make(chan struct{})
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		close(accepted)
		_, _ = io.Copy(io.Discard, conn)
	}()
	host, port := probeAddress(t, listener.Addr())
	content := testNetworkContent("")
	content.Mode, content.Host, content.Port = "only_http_proxy", host, port
	content.DirectIPs = nil
	content.ManifestDigest = content.ComputeManifestDigest()
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() { result <- ProbeEndpoint(ctx, content, nil) }()
	<-accepted
	cancelledAt := time.Now()
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancelled probe error=%v", err)
		}
		if elapsed := time.Since(cancelledAt); elapsed > 500*time.Millisecond {
			t.Fatalf("cancelled probe took %v", elapsed)
		}
	case <-time.After(time.Second):
		t.Fatal("cancelled probe remained blocked")
	}
}

func TestProbeEndpointRejectsUnreachableAddress(t *testing.T) {
	listener := listenProbeServer(t)
	host, port := probeAddress(t, listener.Addr())
	_ = listener.Close()
	content := testNetworkContent("")
	content.Host, content.Port = host, port
	content.ManifestDigest = content.ComputeManifestDigest()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := ProbeEndpoint(ctx, content, nil); err == nil {
		t.Fatal("unreachable endpoint passed probe")
	}
}

func listenProbeServer(t *testing.T) net.Listener {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	return listener
}

func probeAddress(t *testing.T, address net.Addr) (string, int) {
	t.Helper()
	host, portText, err := net.SplitHostPort(address.String())
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatal(err)
	}
	return host, port
}
