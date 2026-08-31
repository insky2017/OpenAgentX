package remotehttps

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	openapi "openagentx/internal/api"
	"openagentx/internal/api/workerapi"
	"openagentx/internal/domain"
	openruntime "openagentx/internal/runtime"
)

type testCertificate struct {
	cert *x509.Certificate
	key  *ecdsa.PrivateKey
}

func makeCertificate(t *testing.T, parent *testCertificate, isCA bool, commonName string, notBefore, notAfter time.Time) testCertificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	serial, _ := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 120))
	template := &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: commonName}, NotBefore: notBefore, NotAfter: notAfter, BasicConstraintsValid: true, IsCA: isCA, KeyUsage: x509.KeyUsageDigitalSignature}
	if commonName == "server" {
		template.IPAddresses = []net.IP{net.ParseIP("127.0.0.1")}
	}
	if isCA {
		template.KeyUsage |= x509.KeyUsageCertSign
	}
	if !isCA {
		template.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth}
	}
	signer := template
	signerKey := key
	if parent != nil {
		signer = parent.cert
		signerKey = parent.key
	}
	der, err := x509.CreateCertificate(rand.Reader, template, signer, &key.PublicKey, signerKey)
	if err != nil {
		t.Fatal(err)
	}
	certificate, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return testCertificate{cert: certificate, key: key}
}

func tlsCertificate(t *testing.T, certificate testCertificate) tls.Certificate {
	t.Helper()
	return tls.Certificate{Certificate: [][]byte{certificate.cert.Raw}, PrivateKey: certificate.key, Leaf: certificate.cert}
}

func certPEM(certificate testCertificate) []byte {
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificate.cert.Raw})
}

func TestServerRequiresTLS13AndVerifiedClientCertificate(t *testing.T) {
	ca := makeCertificate(t, nil, true, "test-ca", time.Now().Add(-time.Hour), time.Now().Add(time.Hour))
	serverCert := makeCertificate(t, caPtr(ca), false, "server", time.Now().Add(-time.Hour), time.Now().Add(time.Hour))
	clientCert := makeCertificate(t, caPtr(ca), false, "worker-principal", time.Now().Add(-time.Hour), time.Now().Add(time.Hour))
	pool := x509.NewCertPool()
	pool.AddCert(ca.cert)
	serverTLS := &tls.Config{MinVersion: tls.VersionTLS13, MaxVersion: tls.VersionTLS13, ClientAuth: tls.RequireAndVerifyClientCert, ClientCAs: pool, Certificates: []tls.Certificate{tlsCertificate(t, serverCert)}}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })
	server := httptest.NewUnstartedServer(handler)
	server.TLS = serverTLS
	server.StartTLS()
	t.Cleanup(server.Close)
	clientTLS := &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: pool, Certificates: []tls.Certificate{tlsCertificate(t, clientCert)}}
	client := server.Client()
	client.Transport.(*http.Transport).TLSClientConfig = clientTLS
	response, err := client.Post(server.URL, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("status=%d", response.StatusCode)
	}
	if server.TLS.MinVersion != tls.VersionTLS13 || server.TLS.ClientAuth != tls.RequireAndVerifyClientCert {
		t.Fatal("server TLS policy was weakened")
	}
}

func TestNewServerRejectsWeakTLSConfiguration(t *testing.T) {
	pool := x509.NewCertPool()
	certificate := tls.Certificate{Certificate: [][]byte{{1}}, PrivateKey: &ecdsa.PrivateKey{}}
	for name, config := range map[string]*tls.Config{
		"tls12":          {MinVersion: tls.VersionTLS12, ClientAuth: tls.RequireAndVerifyClientCert, ClientCAs: pool, Certificates: []tls.Certificate{certificate}},
		"no-client-auth": {MinVersion: tls.VersionTLS13, ClientCAs: pool, Certificates: []tls.Certificate{certificate}},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := NewServer("127.0.0.1:0", http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}), config, nil); err == nil {
				t.Fatal("weak TLS configuration was accepted")
			}
		})
	}
}

func TestServerRejectsMissingAndWrongCAClientCertificates(t *testing.T) {
	ca := makeCertificate(t, nil, true, "test-ca", time.Now().Add(-time.Hour), time.Now().Add(time.Hour))
	otherCA := makeCertificate(t, nil, true, "other-ca", time.Now().Add(-time.Hour), time.Now().Add(time.Hour))
	serverCert := makeCertificate(t, caPtr(ca), false, "server", time.Now().Add(-time.Hour), time.Now().Add(time.Hour))
	wrongClient := makeCertificate(t, caPtr(otherCA), false, "worker-principal", time.Now().Add(-time.Hour), time.Now().Add(time.Hour))
	pool := x509.NewCertPool()
	pool.AddCert(ca.cert)
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	server.TLS = &tls.Config{MinVersion: tls.VersionTLS13, MaxVersion: tls.VersionTLS13, ClientAuth: tls.RequireAndVerifyClientCert, ClientCAs: pool, Certificates: []tls.Certificate{tlsCertificate(t, serverCert)}}
	server.StartTLS()
	t.Cleanup(server.Close)
	for _, test := range []struct {
		name string
		cert *testCertificate
	}{
		{name: "missing", cert: nil}, {name: "wrong-ca", cert: &wrongClient},
	} {
		t.Run(test.name, func(t *testing.T) {
			config := server.Client().Transport.(*http.Transport).TLSClientConfig.Clone()
			config.RootCAs = pool
			if test.cert != nil {
				config.Certificates = []tls.Certificate{tlsCertificate(t, *test.cert)}
			}
			client := &http.Client{Transport: &http.Transport{TLSClientConfig: config}}
			if _, err := client.Get(server.URL); err == nil {
				t.Fatal("expected mTLS handshake failure")
			}
		})
	}
}

func TestServerRejectsExpiredClientCertificate(t *testing.T) {
	now := time.Now()
	ca := makeCertificate(t, nil, true, "test-ca", now.Add(-2*time.Hour), now.Add(time.Hour))
	serverCert := makeCertificate(t, caPtr(ca), false, "server", now.Add(-time.Hour), now.Add(time.Hour))
	expired := makeCertificate(t, caPtr(ca), false, "worker-principal", now.Add(-2*time.Hour), now.Add(-time.Hour))
	pool := x509.NewCertPool()
	pool.AddCert(ca.cert)
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	server.TLS = &tls.Config{MinVersion: tls.VersionTLS13, MaxVersion: tls.VersionTLS13, ClientAuth: tls.RequireAndVerifyClientCert, ClientCAs: pool, Certificates: []tls.Certificate{tlsCertificate(t, serverCert)}}
	server.StartTLS()
	t.Cleanup(server.Close)
	config := server.Client().Transport.(*http.Transport).TLSClientConfig.Clone()
	config.RootCAs = pool
	config.Certificates = []tls.Certificate{tlsCertificate(t, expired)}
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: config}}
	if _, err := client.Get(server.URL); err == nil {
		t.Fatal("expected expired client certificate handshake failure")
	}
}

func TestServerStopsPromptlyWithIncompleteTLSHandshake(t *testing.T) {
	ca := makeCertificate(t, nil, true, "test-ca", time.Now().Add(-time.Hour), time.Now().Add(time.Hour))
	serverCert := makeCertificate(t, caPtr(ca), false, "server", time.Now().Add(-time.Hour), time.Now().Add(time.Hour))
	pool := x509.NewCertPool()
	pool.AddCert(ca.cert)
	server, err := NewServer("127.0.0.1:0", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}), &tls.Config{
		MinVersion: tls.VersionTLS13, MaxVersion: tls.VersionTLS13,
		ClientAuth: tls.RequireAndVerifyClientCert, ClientCAs: pool,
		Certificates: []tls.Certificate{tlsCertificate(t, serverCert)},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	serverResult := make(chan error, 1)
	go func() { serverResult <- server.Start(ctx) }()
	if err := server.WaitReady(context.Background()); err != nil {
		t.Fatal(err)
	}
	conn, err := net.Dial("tcp", server.Addr())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	// Leave the connection before the TLS ClientHello so it remains in
	// http.StateNew when shutdown starts.
	cancel()
	select {
	case err := <-serverResult:
		if err != nil {
			t.Fatalf("stop server with incomplete TLS handshake: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("server did not stop promptly with incomplete TLS handshake")
	}
}

func TestCertificatePrincipalBindingFailsClosed(t *testing.T) {
	resolver, err := NewCertificatePrincipalResolver(map[string][]string{"worker-principal": {"quote"}})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("{}"))
	request.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{{Subject: pkix.Name{CommonName: "worker-principal"}}}, VerifiedChains: [][]*x509.Certificate{{{}}}}
	if err := resolver.ValidateAgent(request, "quote"); err != nil {
		t.Fatal(err)
	}
	if err := resolver.ValidateAgent(request, "other-agent"); err == nil {
		t.Fatal("unbound Agent was accepted")
	}
	request.TLS.PeerCertificates[0].Subject.CommonName = "unknown-principal"
	if _, err := resolver.ResolvePrincipal(request); err == nil {
		t.Fatal("unknown certificate principal was accepted")
	}
}

type registrationService struct {
	workerapi.Service
	called bool
}

func (s *registrationService) Register(_ context.Context, principal string, request openapi.RegisterRequest) (*openapi.WorkerSession, error) {
	s.called = true
	return &openapi.WorkerSession{Worker: domain.WorkerInstance{ID: request.WorkerInstanceID, AgentID: request.AgentID, AuthenticatedPrincipal: principal}}, nil
}

func TestCertificateBindingGuardsWorkerAPIRegistration(t *testing.T) {
	resolver, err := NewCertificatePrincipalResolver(map[string][]string{"worker-principal": {"quote"}})
	if err != nil {
		t.Fatal(err)
	}
	service := &registrationService{}
	handler, err := workerapi.NewHandler(service, resolver)
	if err != nil {
		t.Fatal(err)
	}
	backend := openruntime.BackendRegistration{BackendID: "local", Health: openruntime.BackendHealthy, Descriptor: openruntime.AdapterDescriptor{AdapterID: "fake", BackendType: "fake", Version: "1", LaunchProtocol: "inproc", Models: []string{"model"}, ReasoningModes: []domain.ReasoningMode{domain.ReasoningBackendDefault}, SessionModes: []domain.SessionMode{domain.SessionModeNew}, Steer: openruntime.SteerQueued, Approval: openruntime.ApprovalPreflight, Cancel: openruntime.CancelProcessSignal, BackendOptionsJSON: []byte(`{}`), MaxConcurrency: 1}}
	requestBytes, err := json.Marshal(openapi.RegisterRequest{ContractVersion: openapi.ContractVersion, AgentID: "quote", WorkerInstanceID: "worker-1", Transport: domain.WorkerTransportHTTPS, Backends: []openruntime.BackendRegistration{backend}})
	if err != nil {
		t.Fatal(err)
	}
	requestBody := string(requestBytes)
	request := httptest.NewRequest(http.MethodPost, openapi.WorkerRegisterPath, strings.NewReader(requestBody))
	request.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{{Subject: pkix.Name{CommonName: "worker-principal"}}}, VerifiedChains: [][]*x509.Certificate{{{}}}}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusCreated || !service.called {
		t.Fatalf("bound registration status=%d called=%v body=%s", response.Code, service.called, response.Body.String())
	}
	service.called = false
	requestBody = strings.Replace(requestBody, `"agent_id":"quote"`, `"agent_id":"other"`, 1)
	request = httptest.NewRequest(http.MethodPost, openapi.WorkerRegisterPath, strings.NewReader(requestBody))
	request.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{{Subject: pkix.Name{CommonName: "worker-principal"}}}, VerifiedChains: [][]*x509.Certificate{{{}}}}
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden || service.called {
		t.Fatalf("unbound registration status=%d called=%v body=%s", response.Code, service.called, response.Body.String())
	}
	requestBody = strings.Replace(requestBody, `"agent_id":"other"`, `"agent_id":"quote"`, 1)
	requestBody = strings.Replace(requestBody, `"transport":"https"`, `"transport":"unix"`, 1)
	request = httptest.NewRequest(http.MethodPost, openapi.WorkerRegisterPath, strings.NewReader(requestBody))
	request.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{{Subject: pkix.Name{CommonName: "worker-principal"}}}, VerifiedChains: [][]*x509.Certificate{{{}}}}
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden || service.called {
		t.Fatalf("wrong transport status=%d called=%v body=%s", response.Code, service.called, response.Body.String())
	}
}

func caPtr(c testCertificate) *testCertificate { return &c }
