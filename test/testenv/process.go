//go:build integration

package testenv

import (
	"bytes"
	"context"
	"fmt"
	"net"
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

// Binary holds the compiled applications, which the tests run as separate
// operating system processes, exactly as they run in production.
type Binary struct {
	// API is the executable that serves HTTP.
	API string
	// Workers is the executable that runs the background workers.
	Workers string
	dir     string
}

// BuildBinary compiles both entrypoints. The caller must call Remove when done.
func BuildBinary(ctx context.Context) (*Binary, error) {
	_, source, _, _ := runtime.Caller(0)
	moduleRoot := filepath.Join(filepath.Dir(source), "..", "..")

	dir, err := os.MkdirTemp("", "betledger-binary-")
	if err != nil {
		return nil, err
	}
	binary := &Binary{API: filepath.Join(dir, "api"), Workers: filepath.Join(dir, "workers"), dir: dir}

	for path, entrypoint := range map[string]string{binary.API: "./cmd/api", binary.Workers: "./cmd/workers"} {
		build := exec.CommandContext(ctx, "go", "build", "-o", path, entrypoint)
		build.Dir = moduleRoot
		if output, err := build.CombinedOutput(); err != nil {
			return binary, fmt.Errorf("go build %s: %w: %s", entrypoint, err, output)
		}
	}
	return binary, nil
}

// Remove deletes the compiled binaries.
func (b *Binary) Remove() {
	if b == nil {
		return
	}
	_ = os.RemoveAll(b.dir)
}

// WithEnv overrides one configuration variable of a started application.
func WithEnv(key, value string) func(env map[string]string) {
	return func(env map[string]string) { env[key] = value }
}

// Application is the API running as its own process, with its own memory and
// connection pool.
type Application struct {
	Client
	process *process
}

// StartAPI runs the API against the database, the identity provider and the
// broker, and waits until it is ready, stopping it on cleanup.
func (b *Binary) StartAPI(
	t *testing.T,
	databaseURL string,
	identityProvider *Keycloak,
	broker *LocalStack,
	options ...func(env map[string]string),
) *Application {
	t.Helper()

	port := freePort(t)
	env := defaultEnv(databaseURL, identityProvider, broker)
	env["HTTP_PORT"] = port
	for _, option := range options {
		option(env)
	}
	baseURL := "http://127.0.0.1:" + port
	application := &Application{
		Client:  Client{BaseURL: baseURL, identityProvider: identityProvider},
		process: start(t, b.API, env),
	}
	require.Eventually(t, application.IsListening, 30*time.Second, 100*time.Millisecond,
		"the API on %s never became ready:\n%s", baseURL, application.process.output)
	return application
}

// Stop sends SIGTERM and waits for the process, as an orchestrator would.
func (a *Application) Stop(t *testing.T) {
	t.Helper()

	a.process.stop()
}

// IsListening reports whether the server answers the readiness probe.
func (a *Application) IsListening() bool {
	response, err := (&http.Client{Timeout: time.Second}).Get(a.BaseURL + "/health/ready")
	if err != nil {
		return false
	}
	response.Body.Close()
	return response.StatusCode == http.StatusOK
}

// Workers is the background application running as its own process.
type Workers struct {
	process *process
}

// StartWorkers runs the workers against the database and the broker, stopping
// them on cleanup. Without options they poll once an hour, which keeps them
// from taking the work of other tests.
func (b *Binary) StartWorkers(
	t *testing.T,
	databaseURL string,
	broker *LocalStack,
	options ...func(env map[string]string),
) *Workers {
	t.Helper()

	env := defaultEnv(databaseURL, nil, broker)
	for _, option := range options {
		option(env)
	}
	return &Workers{process: start(t, b.Workers, env)}
}

// Stop sends SIGTERM and waits for the process, as an orchestrator would.
func (w *Workers) Stop(t *testing.T) {
	t.Helper()

	w.process.stop()
}

// defaultEnv is the configuration of an application started by a test: workers
// that never run on their own, and short retries where a test shortens them.
func defaultEnv(databaseURL string, identityProvider *Keycloak, broker *LocalStack) map[string]string {
	issuerURL := "http://identity.invalid/realms/betledger"
	if identityProvider != nil {
		issuerURL = identityProvider.IssuerURL
	}
	return map[string]string{
		"DATABASE_URL":               databaseURL,
		"OIDC_ISSUER_URL":            issuerURL,
		"OIDC_AUDIENCE":              Audience,
		"REFERENCE_RETRY_BASE_DELAY": "1ms",
		"REFERENCE_RETRY_MAX_DELAY":  "1s",
		"REFERENCE_POLL_INTERVAL":    "1h",
		"AWS_REGION":                 "us-east-1",
		"AWS_ENDPOINT_URL":           broker.EndpointURL,
		"EVENTS_QUEUE_NAME":          EventsQueueName,
		"OUTBOX_RETRY_BASE_DELAY":    "1ms",
		"OUTBOX_RETRY_MAX_DELAY":     "1s",
		"OUTBOX_POLL_INTERVAL":       "1h",
	}
}

// process is a running application, stopped on cleanup and whose output is
// printed when the test fails.
type process struct {
	command *exec.Cmd
	output  *lockedBuffer
	exited  chan error
	once    sync.Once
}

func start(t *testing.T, path string, env map[string]string) *process {
	t.Helper()

	output := &lockedBuffer{}
	command := exec.Command(path)
	command.Env = os.Environ()
	for key, value := range env {
		command.Env = append(command.Env, key+"="+value)
	}
	command.Stdout = output
	command.Stderr = output
	require.NoError(t, command.Start())

	p := &process{command: command, output: output, exited: make(chan error, 1)}
	go func() { p.exited <- command.Wait() }()
	t.Cleanup(func() {
		p.stop()
		if t.Failed() {
			t.Logf("output of %s:\n%s", filepath.Base(path), output.String())
		}
	})
	return p
}

// stop sends SIGTERM and kills the process if it has not exited within the
// grace period. Calling it twice is safe: the cleanup always runs it.
func (p *process) stop() {
	p.once.Do(func() {
		_ = p.command.Process.Signal(syscall.SIGTERM)
		select {
		case <-p.exited:
		case <-time.After(15 * time.Second):
			_ = p.command.Process.Kill()
			<-p.exited
		}
	})
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

func freePort(t *testing.T) string {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()
	return fmt.Sprint(listener.Addr().(*net.TCPAddr).Port)
}
