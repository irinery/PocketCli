package collect

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"pocketcli/internal/investigation/storage"
)

func TestPrincipalQueueAcrossProcesses(t *testing.T) {
	if os.Getenv("POCKETCLI_TEST_PRINCIPAL_CHILD") == "1" {
		principalQueueChild(t)
		return
	}
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	active, maxActive, calls := 0, 0, 0
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/user" {
			_, _ = io.WriteString(w, `{"id":42}`)
			return
		}
		if r.URL.Path != "/work" {
			http.NotFound(w, r)
			return
		}
		mu.Lock()
		active++
		calls++
		if active > maxActive {
			maxActive = active
		}
		mu.Unlock()
		time.Sleep(250 * time.Millisecond)
		mu.Lock()
		active--
		mu.Unlock()
		_, _ = io.WriteString(w, `[]`)
	}))
	defer server.Close()
	first := principalClient(t, store, "token-a", server.Client().Transport)
	second := principalClient(t, store, "token-b", server.Client().Transport)
	first.GitHubURL, second.GitHubURL = server.URL, server.URL
	for _, client := range []*Client{first, second} {
		if err := client.ensurePrincipal(context.Background(), "github"); err != nil {
			t.Fatal(err)
		}
		if _, err := client.PrincipalID(context.Background(), "github"); err != nil {
			t.Fatal(err)
		}
	}
	certPath := filepath.Join(t.TempDir(), "server.pem")
	cert := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
	if err := os.WriteFile(certPath, cert, 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestPrincipalQueueAcrossProcesses$")
	cmd.Env = append(os.Environ(),
		"POCKETCLI_TEST_PRINCIPAL_CHILD=1",
		"POCKETCLI_TEST_PRINCIPAL_ROOT="+store.Root,
		"POCKETCLI_TEST_PRINCIPAL_URL="+server.URL,
		"POCKETCLI_TEST_PRINCIPAL_CERT="+certPath,
	)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	ready, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil || strings.TrimSpace(ready) != "READY" {
		_ = cmd.Wait()
		t.Fatalf("child did not become ready: %q %v %s", ready, err, stderr.String())
	}
	parentDone := make(chan error, 1)
	go func() {
		_, _, _, err := first.GetGitHub(ctx, server.URL+"/work")
		parentDone <- err
	}()
	if _, err := io.WriteString(stdin, "G"); err != nil {
		t.Fatal(err)
	}
	_ = stdin.Close()
	if err := <-parentDone; err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatalf("child request: %v %s", err, stderr.String())
	}
	mu.Lock()
	defer mu.Unlock()
	if calls != 2 || maxActive != 1 {
		t.Fatalf("two processes sharing one actor: calls=%d max_active=%d", calls, maxActive)
	}
}

func principalQueueChild(t *testing.T) {
	root := os.Getenv("POCKETCLI_TEST_PRINCIPAL_ROOT")
	endpoint := os.Getenv("POCKETCLI_TEST_PRINCIPAL_URL")
	cert, err := os.ReadFile(os.Getenv("POCKETCLI_TEST_PRINCIPAL_CERT"))
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(cert) {
		t.Fatal("local TLS certificate invalid")
	}
	store, err := storage.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	transport := &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool}}
	defer transport.CloseIdleConnections()
	client := principalClient(t, store, "token-b", transport)
	client.GitHubURL = endpoint
	fmt.Fprintln(os.Stdout, "READY")
	var signal [1]byte
	if _, err := io.ReadFull(os.Stdin, signal[:]); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := client.GetGitHub(context.Background(), endpoint+"/work"); err != nil {
		t.Fatal(err)
	}
}
