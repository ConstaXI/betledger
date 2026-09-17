//go:build integration

package testenv

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Binary is the compiled application, shared by the tests that run it as
// separate operating system processes.
type Binary struct {
	// Path locates the executable.
	Path string
	dir  string
}

// BuildBinary compiles the application entrypoint. The caller must call Remove
// when done.
func BuildBinary(ctx context.Context) (*Binary, error) {
	_, source, _, _ := runtime.Caller(0)
	moduleRoot := filepath.Join(filepath.Dir(source), "..", "..")

	dir, err := os.MkdirTemp("", "betledger-binary-")
	if err != nil {
		return nil, err
	}
	binary := &Binary{Path: filepath.Join(dir, "betledger"), dir: dir}

	build := exec.CommandContext(ctx, "go", "build", "-o", binary.Path, "./cmd/api")
	build.Dir = moduleRoot
	if output, err := build.CombinedOutput(); err != nil {
		return binary, fmt.Errorf("go build: %w: %s", err, output)
	}
	return binary, nil
}

// Remove deletes the compiled binary.
func (b *Binary) Remove() {
	if b == nil {
		return
	}
	_ = os.RemoveAll(b.dir)
}

// Process is the application running as its own operating system process, with
// its own memory and connection pool.
type Process struct {
	Client
	command *exec.Cmd
	output  *lockedBuffer
	exited  chan error
}

// StartProcess runs the binary against the database and the identity provider
// and waits until it is ready, stopping it on cleanup. Its output is printed
// when the test fails.
func (b *Binary) StartProcess(
	t *testing.T,
	databaseURL string,
	identityProvider *Keycloak,
	broker *LocalStack,
) *Process {
	t.Helper()

	port := freePort(t)
	output := &lockedBuffer{}
	command := exec.Command(b.Path)
	command.Env = append(os.Environ(),
		"HTTP_PORT="+port,
		"DATABASE_URL="+databaseURL,
		"OIDC_ISSUER_URL="+identityProvider.IssuerURL,
		"OIDC_DISCOVERY_URL="+identityProvider.IssuerURL,
		"OIDC_AUDIENCE="+Audience,
		"REFERENCE_POLL_INTERVAL=1h",
		"OUTBOX_POLL_INTERVAL=1h",
		"AWS_ENDPOINT_URL="+broker.EndpointURL,
	)
	command.Stdout = output
	command.Stderr = output
	require.NoError(t, command.Start())

	process := &Process{
		Client:  Client{BaseURL: "http://127.0.0.1:" + port, identityProvider: identityProvider},
		command: command,
		output:  output,
		exited:  make(chan error, 1),
	}
	go func() { process.exited <- command.Wait() }()
	t.Cleanup(func() {
		process.stop()
		if t.Failed() {
			t.Logf("output of the process on %s:\n%s", process.BaseURL, output.String())
		}
	})

	require.Eventually(t, func() bool {
		response, err := (&http.Client{Timeout: time.Second}).Get(process.BaseURL + "/health/ready")
		if err != nil {
			return false
		}
		response.Body.Close()
		return response.StatusCode == http.StatusOK
	}, 30*time.Second, 100*time.Millisecond, "the process on %s never became ready:\n%s", process.BaseURL, output)
	return process
}

// stop sends SIGTERM, as an orchestrator would, and kills the process if it has
// not exited within the grace period.
func (p *Process) stop() {
	_ = p.command.Process.Signal(syscall.SIGTERM)
	select {
	case <-p.exited:
	case <-time.After(15 * time.Second):
		_ = p.command.Process.Kill()
		<-p.exited
	}
}

type lockedBuffer struct {
	mu     sync.Mutex
	buffer bytes.Buffer
}

func (b *lockedBuffer) Write(data []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buffer.Write(data)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buffer.String()
}
