package workercli

import (
	"context"
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
	"testing"
	"time"

	"openagentx/internal/api"
	"openagentx/internal/api/workerapi"
	"openagentx/internal/controlplane"
	"openagentx/internal/domain"
	"openagentx/internal/network/secretstore"
	openagentsqlite "openagentx/internal/persistence/sqlite"
	"openagentx/internal/transport/remotehttps"
)

type processTestCertificate struct {
	cert *x509.Certificate
	key  *ecdsa.PrivateKey
}

func TestRunWorkerProcessCompletesConsecutiveTasksOverRemoteHTTPSAndStops(t *testing.T) {
	ctx := context.Background()
	repository, err := openagentsqlite.Open(ctx, filepath.Join(t.TempDir(), "remote-worker-process.db"), openagentsqlite.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repository.Close() })
	seedWorkerProcessIdentity(t, repository)
	broker := controlplane.NewMemoryWakeupBroker()
	secrets, err := secretstore.Open(filepath.Join(t.TempDir(), "network-secrets"))
	if err != nil {
		t.Fatal(err)
	}
	workflow, err := controlplane.NewNetworkWorkflowService(repository, secrets, broker, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	service, err := controlplane.NewWorkerService(repository, broker, controlplane.WorkerServiceOptions{NetworkWorkflow: workflow})
	if err != nil {
		t.Fatal(err)
	}
	resolver, err := remotehttps.NewCertificatePrincipalResolver(map[string][]string{"worker-principal": {"quote"}})
	if err != nil {
		t.Fatal(err)
	}
	handler, err := workerapi.NewHandler(service, resolver)
	if err != nil {
		t.Fatal(err)
	}
	ca := makeProcessTestCertificate(t, nil, true, "test-ca")
	serverCert := makeProcessTestCertificate(t, &ca, false, "server")
	clientCert := makeProcessTestCertificate(t, &ca, false, "worker-principal")
	pool := x509.NewCertPool()
	pool.AddCert(ca.cert)
	serverTLS := &tls.Config{
		MinVersion: tls.VersionTLS13, MaxVersion: tls.VersionTLS13,
		ClientAuth: tls.RequireAndVerifyClientCert, ClientCAs: pool,
		Certificates: []tls.Certificate{processTLSCertificate(serverCert)},
	}
	remoteServer, err := remotehttps.NewServer("127.0.0.1:0", handler, serverTLS, nil)
	if err != nil {
		t.Fatal(err)
	}
	serverContext, cancelServer := context.WithCancel(ctx)
	serverResult := make(chan error, 1)
	go func() { serverResult <- remoteServer.Start(serverContext) }()
	if err := remoteServer.WaitReady(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cancelServer()
		select {
		case err := <-serverResult:
			if err != nil {
				t.Errorf("stop remote HTTPS server: %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Error("remote HTTPS server did not stop")
		}
	})

	configDir := t.TempDir()
	caFile := writeProcessCertificateFile(t, configDir, "ca.pem", certPEMBytes(ca))
	clientCertFile := writeProcessCertificateFile(t, configDir, "client.pem", certPEMBytes(clientCert))
	clientKeyFile := writeProcessKeyFile(t, configDir, "client.key", clientCert.key)
	configPath := filepath.Join(configDir, "agent.yaml")
	config := fmt.Sprintf(`version: 1
agent_id: quote
transport: https
endpoint: https://%s
ca_file: %s
client_cert_file: %s
client_key_file: %s
server_name: 127.0.0.1
capabilities: [coding]
heartbeat_interval: 20ms
mailbox_wait: 1s
control_wait: 1s
shutdown_timeout: 1s
runtime_backends:
  - backend_id: local
    adapter_id: fake
    options:
      model: fake-remote-process-model
      result: completed-over-remote-process
`, remoteServer.Addr(), caFile, clientCertFile, clientKeyFile)
	if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}

	workerContext, cancelWorker := context.WithCancel(ctx)
	t.Cleanup(cancelWorker)
	workerResult := make(chan error, 1)
	go func() { workerResult <- RunWorkerProcess(workerContext, configPath) }()
	bootstrapWorkerProcessInherit(t, repository, workflow, workerResult, "remote")
	for _, suffix := range []string{"a", "b"} {
		created := createWorkerProcessTask(t, repository, "remote-"+suffix)
		broker.Publish(controlplane.AgentMailboxTopic("quote"))
		waitForTaskStatus(t, repository, created.Task.ID, domain.TaskStatusSucceeded, workerResult)
		settled, err := repository.GetTask(ctx, created.Task.ID)
		if err != nil || settled.Result == nil || *settled.Result != "completed-over-remote-process" {
			t.Fatalf("settled Task=%+v err=%v", settled, err)
		}
	}

	workers, err := repository.ListWorkers(ctx, 10)
	if err != nil || len(workers) != 1 {
		t.Fatalf("remote Worker list=%+v err=%v", workers, err)
	}
	worker := workers[0]
	admin, err := controlplane.NewWorkerAdminService(repository, broker, time.Now, nil)
	if err != nil {
		t.Fatal(err)
	}
	command, err := admin.Command(ctx, "human-owner", worker.ID, domain.WorkerCommandStop, api.WorkerAdminRequest{
		Meta:        api.CommandMeta{IdempotencyKey: "remote-process-stop", ExpectedVersion: 1},
		RequestedBy: "human-owner", ExpectedGeneration: worker.Generation,
	})
	if err != nil || command == nil {
		t.Fatalf("create remote Worker stop command=%+v err=%v", command, err)
	}
	select {
	case err := <-workerResult:
		if err != nil {
			t.Fatalf("controlled remote Worker stop: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("remote Worker did not stop after control command")
	}
}

func makeProcessTestCertificate(t *testing.T, parent *processTestCertificate, isCA bool, commonName string) processTestCertificate {
	t.Helper()
	now := time.Now()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 120))
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: serial, Subject: pkix.Name{CommonName: commonName},
		NotBefore: now.Add(-time.Minute), NotAfter: now.Add(time.Hour),
		BasicConstraintsValid: true, IsCA: isCA,
		KeyUsage: x509.KeyUsageDigitalSignature,
	}
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
	return processTestCertificate{cert: cert, key: key}
}

func processTLSCertificate(c processTestCertificate) tls.Certificate {
	return tls.Certificate{Certificate: [][]byte{c.cert.Raw}, PrivateKey: c.key, Leaf: c.cert}
}

func certPEMBytes(c processTestCertificate) []byte {
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: c.cert.Raw})
}

func writeProcessCertificateFile(t *testing.T, dir, name string, content []byte) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func writeProcessKeyFile(t *testing.T, dir, name string, key *ecdsa.PrivateKey) string {
	t.Helper()
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return writeProcessCertificateFile(t, dir, name, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))
}
