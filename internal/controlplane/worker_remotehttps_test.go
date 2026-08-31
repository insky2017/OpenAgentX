package controlplane

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"net"
	"net/http"
	"testing"
	"time"

	"openagentx/internal/api"
	"openagentx/internal/api/workerapi"
	workerclient "openagentx/internal/client/worker"
	"openagentx/internal/domain"
	openruntime "openagentx/internal/runtime"
	"openagentx/internal/transport/remotehttps"
)

type remoteTestCertificate struct {
	cert *x509.Certificate
	key  *ecdsa.PrivateKey
}

func TestRemoteHTTPSWorkerAPIEndToEnd(t *testing.T) {
	environment := newWorkerTestEnvironment(t, nil)
	ca := makeRemoteTestCertificate(t, nil, true, "test-ca", time.Now().Add(-time.Hour), time.Now().Add(time.Hour))
	serverCert := makeRemoteTestCertificate(t, &ca, false, "server", time.Now().Add(-time.Hour), time.Now().Add(time.Hour))
	clientCert := makeRemoteTestCertificate(t, &ca, false, "worker-principal", time.Now().Add(-time.Hour), time.Now().Add(time.Hour))
	resolver, err := remotehttps.NewCertificatePrincipalResolver(map[string][]string{"worker-principal": {environment.agentID}})
	if err != nil {
		t.Fatal(err)
	}
	handler, err := workerapi.NewHandler(environment.service, resolver)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(ca.cert)
	serverTLS := &tls.Config{
		MinVersion: tls.VersionTLS13, MaxVersion: tls.VersionTLS13,
		ClientAuth: tls.RequireAndVerifyClientCert, ClientCAs: pool,
		Certificates: []tls.Certificate{remoteTLSCertificate(serverCert)},
	}
	server, err := remotehttps.NewServer("127.0.0.1:0", handler, serverTLS, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	serverErr := make(chan error, 1)
	go func() { serverErr <- server.Start(ctx) }()
	if err := server.WaitReady(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-serverErr:
			if err != nil {
				t.Errorf("stop remote HTTPS server: %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Error("remote HTTPS server did not stop")
		}
	})
	clientTLS := &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: pool, Certificates: []tls.Certificate{remoteTLSCertificate(clientCert)}}
	client, err := workerclient.NewHTTPWorkerClient("https://"+server.Addr(), &http.Client{Transport: &http.Transport{TLSClientConfig: clientTLS}, Timeout: 40 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	session, err := client.RegisterWorker(context.Background(), api.RegisterRequest{
		ContractVersion: api.ContractVersion, AgentID: environment.agentID, WorkerInstanceID: "worker-remote",
		Transport: domain.WorkerTransportHTTPS, Capabilities: []string{"coding"}, Backends: []openruntime.BackendRegistration{environment.backend},
	})
	if err != nil {
		t.Fatal(err)
	}
	if session.Worker.AuthenticatedPrincipal != "worker-principal" {
		t.Fatalf("authenticated principal=%q", session.Worker.AuthenticatedPrincipal)
	}
	if err := client.Heartbeat(context.Background(), api.HeartbeatRequest{WorkerInstanceID: session.Worker.ID, Generation: session.Worker.Generation, FencingToken: session.Worker.FencingToken, Status: domain.WorkerStatusOnline, BackendHealth: map[string]openruntime.BackendHealth{"local": openruntime.BackendHealthy}}); err != nil {
		t.Fatal(err)
	}
	created := environment.createTask(t, "remote")
	item, err := client.ClaimMailbox(context.Background(), claimRequest(session, 1))
	if err != nil || item == nil || item.ID != created.MailboxItem.ID {
		t.Fatalf("remote claim item=%+v err=%v", item, err)
	}
	begin, err := client.BeginAttempt(context.Background(), item.ID, api.BeginAttemptRequest{WorkerInstanceID: session.Worker.ID, AgentID: environment.agentID, Generation: session.Worker.Generation, FencingToken: session.Worker.FencingToken, ExpectedItemState: domain.MailboxStateClaimed})
	if err != nil {
		t.Fatal(err)
	}
	if err := client.FinishRun(context.Background(), begin.Turn.RunAttempt.ID, api.FinishRunRequest{WorkerInstanceID: session.Worker.ID, Generation: session.Worker.Generation, FencingToken: session.Worker.FencingToken, ExpectedTaskVersion: begin.Turn.Task.Version, ExpectedRunVersion: begin.Turn.RunAttempt.Version, Result: openruntime.TurnResult{Status: openruntime.TurnResultSucceeded, Result: "remote-done", SideEffectsKnown: true}}); err != nil {
		t.Fatal(err)
	}
}

func makeRemoteTestCertificate(t *testing.T, parent *remoteTestCertificate, isCA bool, commonName string, notBefore, notAfter time.Time) remoteTestCertificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 120))
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: commonName}, NotBefore: notBefore, NotAfter: notAfter, BasicConstraintsValid: true, IsCA: isCA, KeyUsage: x509.KeyUsageDigitalSignature}
	if commonName == "server" {
		template.IPAddresses = []net.IP{net.ParseIP("127.0.0.1")}
	}
	if isCA {
		template.KeyUsage |= x509.KeyUsageCertSign
	} else {
		template.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth}
	}
	signer, signerKey := template, key
	if parent != nil {
		signer, signerKey = parent.cert, parent.key
	}
	der, err := x509.CreateCertificate(rand.Reader, template, signer, &key.PublicKey, signerKey)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return remoteTestCertificate{cert: cert, key: key}
}

func remoteTLSCertificate(c remoteTestCertificate) tls.Certificate {
	return tls.Certificate{Certificate: [][]byte{c.cert.Raw}, PrivateKey: c.key, Leaf: c.cert}
}
