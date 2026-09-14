// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// go-quicbaas is dual-licensed:
//
// 1. GNU Affero General Public License v3.0 (AGPL-3.0)
//    See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
// 2. Commercial License
//    Contact licensing@automatethethings.com for commercial licensing options.

package integration

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	qberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/test/integration/certfixture"
)

const (
	// DefaultGRPCAddress is the default gRPC address for tests.
	DefaultGRPCAddress = "localhost:50151"

	// DefaultRESTAddress is the default REST address for tests. TLS is
	// mandatory stack-wide, so the REST listener always serves HTTPS.
	DefaultRESTAddress = "https://localhost:58080"

	// DefaultHealthEndpoint is the health check endpoint.
	DefaultHealthEndpoint = DefaultRESTAddress + "/api/v1/health/live"

	// tlsServerName is the SAN the suite verifies the server certificate
	// against. The fixture serving leaf (node1.crt) carries DNS:localhost
	// and IP:127.0.0.1 SANs.
	tlsServerName = "localhost"

	// DefaultStartupTimeout is the maximum time to wait for server startup.
	DefaultStartupTimeout = 60 * time.Second

	// DefaultHealthCheckInterval is the interval between health checks.
	DefaultHealthCheckInterval = 1 * time.Second

	// DefaultShutdownTimeout is the maximum time to wait for server shutdown.
	DefaultShutdownTimeout = 30 * time.Second
)

var (
	// testEnv holds the global test environment.
	testEnv *TestEnvironment

	// setupOnce ensures setup runs only once.
	setupOnce sync.Once

	// teardownOnce ensures teardown runs only once.
	teardownOnce sync.Once

	// mockOnlyMode indicates if running only mock tests (no Docker required).
	mockOnlyMode bool

	// suiteTLSMu guards suiteTLSConfig.
	suiteTLSMu sync.Mutex

	// suiteTLSConfig caches the TLS client configuration pinned to the
	// integration CA fixture. Nil until the fixture has been loaded.
	suiteTLSConfig *tls.Config

	// certsFixtureDir is the shared integration cert fixture directory
	// (test/integration/certs) the docker-compose mounts into the container
	// at /etc/quicbaas/certs. Resolved (and generated when absent) by
	// ensureFixtureCerts during setup.
	certsFixtureDir string
)

// suiteTLS returns the TLS client configuration that pins trust to the
// integration CA fixture the server serves its certificates under (RootCAs +
// ServerName; explicitly no InsecureSkipVerify). The result is cached for
// the whole suite.
func suiteTLS() (*tls.Config, error) {
	suiteTLSMu.Lock()
	defer suiteTLSMu.Unlock()
	if suiteTLSConfig != nil {
		return suiteTLSConfig, nil
	}
	pemBytes, err := fetchServerCA()
	if err != nil {
		return nil, err
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(pemBytes) {
		return nil, &qberrors.MessageError{Message: "no certificates parsed from server CA chain"}
	}
	suiteTLSConfig = &tls.Config{
		RootCAs:    pool,
		ServerName: tlsServerName,
		MinVersion: tls.VersionTLS13,
	}
	return suiteTLSConfig, nil
}

// resetSuiteTLS drops the cached TLS configuration. Called before starting
// containers so a regenerated cert fixture is never trusted from a stale pin.
func resetSuiteTLS() {
	suiteTLSMu.Lock()
	defer suiteTLSMu.Unlock()
	suiteTLSConfig = nil
}

// fetchServerCA obtains the PEM CA the server serves its TLS certificates
// under. QUICBAAS_TEST_CA overrides with an explicit path; otherwise the
// shared integration CA fixture (test/integration/certs/ca.crt, mounted into
// the container by docker-compose.yml) is read from the host checkout.
func fetchServerCA() ([]byte, error) {
	if path := os.Getenv("QUICBAAS_TEST_CA"); path != "" {
		return os.ReadFile(path) // #nosec G304 - operator-supplied test CA path
	}
	if certsFixtureDir == "" {
		return nil, &qberrors.MessageError{Message: "cert fixture directory not resolved (setup has not run)"}
	}
	return os.ReadFile(filepath.Join(certsFixtureDir, "ca.crt")) // #nosec G304 - path rooted at repo fixture dir
}

// ensureFixtureCerts resolves the shared integration cert fixture directory
// relative to the compose dir and issues the fixtures through QuicBaaS's own
// certificate authority when any declared file is missing.
//
// It is a Go call, not a subprocess. It used to shell out to
// `go run ./test/integration/gen-certs`, which was a 444-line parallel
// certificate authority built from crypto/ecdsa and crypto/x509
// (go-quicbaas#943): the suite pinned that CA as its RootCAs and asserted
// against a lookalike of the product's issuance path. certfixture.Ensure issues
// the same set through pkg/server.NewCAService — the adapter the daemon serves
// every certificate operation through — so the material this suite trusts is
// the material the product produces.
//
// The declared file set comes from the topology, so a node or client added to
// the declaration is automatically part of the completeness check rather than a
// second list here that can fall behind.
func ensureFixtureCerts(composeDir string) error {
	repoRoot := filepath.Clean(filepath.Join(composeDir, "..", "..", "..", ".."))
	certsFixtureDir = filepath.Join(repoRoot, "test", "integration", "certs")

	topology, err := certfixture.LoadTopology(
		filepath.Join(repoRoot, "test", "integration", "certfixture", "topology.json"))
	if err != nil {
		return &qberrors.WrappedMessageError{
			Message: "load integration cert fixture topology",
			Cause:   err,
		}
	}
	if err := certfixture.Ensure(context.Background(), certsFixtureDir, topology); err != nil {
		return &qberrors.WrappedMessageError{
			Message: "issue integration cert fixtures",
			Cause:   err,
		}
	}
	return nil
}

// newHTTPClient returns an HTTP client that verifies the server's certificate
// against the pinned CA chain. If the chain has not been bootstrapped the
// client trusts an empty pool, so every request fails loudly with a
// certificate verification error instead of silently using system roots.
func newHTTPClient(timeout time.Duration) *http.Client {
	tlsCfg, err := suiteTLS()
	if err != nil {
		tlsCfg = &tls.Config{
			RootCAs:    x509.NewCertPool(),
			ServerName: tlsServerName,
			MinVersion: tls.VersionTLS13,
		}
	}
	return &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			TLSClientConfig:   tlsCfg,
			DisableKeepAlives: true,
		},
	}
}

// TestEnvironment holds the test infrastructure state.
type TestEnvironment struct {
	// GRPCAddress is the gRPC server address.
	GRPCAddress string

	// RESTAddress is the REST server address.
	RESTAddress string

	// DockerComposeDir is the directory containing docker-compose.yml.
	DockerComposeDir string

	// ContainersStarted indicates if containers were started by the test.
	ContainersStarted bool

	mu sync.Mutex
}

// TestMain sets up and tears down the test environment.
func TestMain(m *testing.M) {
	// Check if running mock-only tests (no Docker required)
	if os.Getenv("MOCK_ONLY") == "true" {
		fmt.Println("MOCK_ONLY mode: running without Docker")
		mockOnlyMode = true
		os.Exit(m.Run())
	}

	// Check if Docker is available
	if !isDockerAvailable() {
		fmt.Println("Docker is not available, skipping integration tests")
		os.Exit(0)
	}

	// Check if we should skip integration tests
	if os.Getenv("SKIP_INTEGRATION_TESTS") == "true" {
		fmt.Println("SKIP_INTEGRATION_TESTS is set, skipping integration tests")
		os.Exit(0)
	}

	// Setup
	var setupErr error
	setupOnce.Do(func() {
		setupErr = setup()
	})
	if setupErr != nil {
		fmt.Printf("Failed to set up test environment: %v\n", setupErr)
		os.Exit(1)
	}

	// Run tests
	code := m.Run()

	// Teardown
	teardownOnce.Do(func() {
		if err := teardown(); err != nil {
			fmt.Printf("Failed to tear down test environment: %v\n", err)
		}
	})

	os.Exit(code)
}

// setup initializes the test environment.
func setup() error {
	env := &TestEnvironment{
		GRPCAddress: getEnvOrDefault("QUICBAAS_GRPC_ADDRESS", DefaultGRPCAddress),
		RESTAddress: getEnvOrDefault("QUICBAAS_REST_ADDRESS", DefaultRESTAddress),
	}

	// Find docker-compose.yml directory
	dir, err := findDockerComposeDir()
	if err != nil {
		return &qberrors.WrappedMessageError{Message: "failed to find docker-compose.yml", Cause: err}
	}
	env.DockerComposeDir = dir

	// Resolve (and generate when absent) the cert fixtures BEFORE any health
	// probing: the pinned CA and the admin PIV bundle both come from them.
	if err := ensureFixtureCerts(dir); err != nil {
		return &qberrors.WrappedMessageError{Message: "failed to prepare cert fixtures", Cause: err}
	}

	// Check if server is already running
	if isServerHealthy() {
		fmt.Println("QuicBaaS server is already running")
		testEnv = env
		return acquireSuiteToken()
	}

	// Start containers
	if err := startContainers(env.DockerComposeDir); err != nil {
		return &qberrors.WrappedMessageError{Message: "failed to start containers", Cause: err}
	}
	env.ContainersStarted = true

	// Wait for server to be healthy
	if err := waitForHealthy(DefaultStartupTimeout); err != nil {
		// Try to get container logs for debugging
		if logs, logErr := getContainerLogs(env.DockerComposeDir); logErr == nil {
			fmt.Printf("Container logs:\n%s\n", logs)
		}
		return &qberrors.WrappedMessageError{Message: "server did not become healthy", Cause: err}
	}

	fmt.Println("QuicBaaS server is ready for tests")
	testEnv = env
	return acquireSuiteToken()
}

// acquireSuiteToken obtains the platform access token the suite attaches to
// every protected data-plane request under the default-deny gate. It runs once
// against the public OAuth2 bootstrap (DCR + client_credentials). A failure is
// fatal for the suite: without a token the gate rejects every data-plane call,
// so silently continuing would mask a broken bootstrap.
func acquireSuiteToken() error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := acquirePlatformToken(ctx); err != nil {
		return &qberrors.WrappedMessageError{Message: "failed to acquire platform token", Cause: err}
	}
	fmt.Println("Acquired platform token for default-deny data-plane access")
	return nil
}

// teardown cleans up the test environment.
func teardown() error {
	if testEnv == nil {
		return nil
	}

	// Only stop containers if we started them
	if testEnv.ContainersStarted {
		fmt.Println("Stopping test containers...")
		if err := stopContainers(testEnv.DockerComposeDir); err != nil {
			return &qberrors.WrappedMessageError{Message: "failed to stop containers", Cause: err}
		}
		fmt.Println("Test containers stopped")
	}

	return nil
}

// isDockerAvailable checks if Docker is available.
func isDockerAvailable() bool {
	cmd := exec.Command("docker", "info")
	return cmd.Run() == nil
}

// isDockerComposeAvailable checks if Docker Compose is available.
func isDockerComposeAvailable() bool {
	// Try docker compose (v2)
	cmd := exec.Command("docker", "compose", "version")
	if cmd.Run() == nil {
		return true
	}

	// Try docker-compose (v1)
	cmd = exec.Command("docker-compose", "version")
	return cmd.Run() == nil
}

// findDockerComposeDir finds the directory containing docker-compose.yml.
func findDockerComposeDir() (string, error) {
	// First try the current directory
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}

	// Check if docker-compose.yml exists in current directory
	composePath := filepath.Join(wd, "docker-compose.yml")
	if _, err := os.Stat(composePath); err == nil {
		return wd, nil
	}

	// Try to find it relative to this file
	// The test file is in sdk/go/test/integration/
	candidates := []string{
		wd,
		filepath.Join(wd, ".."),
		filepath.Join(wd, "..", ".."),
		filepath.Join(wd, "..", "..", "test", "integration"),
	}

	for _, dir := range candidates {
		composePath := filepath.Join(dir, "docker-compose.yml")
		if _, err := os.Stat(composePath); err == nil {
			return dir, nil
		}
	}

	return "", &qberrors.MessageError{Message: "docker-compose.yml not found in any expected location"}
}

// startContainers starts the Docker containers.
func startContainers(dir string) error {
	fmt.Println("Starting test containers...")

	// A rebuilt container may serve a regenerated cert fixture; never trust
	// a stale pin.
	resetSuiteTLS()

	// Remove any leftover stack state first. A previous run whose setup
	// failed skips teardown, and a stale data volume poisons this boot: the
	// first-boot admin bootstrap (cert-bound PIV identity) only runs once
	// per data dir, so reusing state from a differently-configured boot
	// leaves the suite without its bootstrap credential.
	downArgs := []string{"compose", "-f", filepath.Join(dir, "docker-compose.yml"), "down", "-v", "--remove-orphans"}
	downCmd := exec.Command("docker", downArgs...)
	downCmd.Dir = dir
	if out, err := downCmd.CombinedOutput(); err != nil {
		return &qberrors.WrappedMessageError{
			Message: "docker compose down (pre-start cleanup) failed: " + string(out),
			Cause:   err,
		}
	}

	args := []string{"compose", "-f", filepath.Join(dir, "docker-compose.yml"), "up", "-d", "--build", "--wait"}

	cmd := exec.Command("docker", args...)
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return &qberrors.WrappedMessageError{Message: "docker compose up failed", Cause: err}
	}

	return nil
}

// stopContainers stops the Docker containers.
func stopContainers(dir string) error {
	args := []string{"compose", "-f", filepath.Join(dir, "docker-compose.yml"), "down", "-v", "--remove-orphans"}

	cmd := exec.Command("docker", args...)
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return &qberrors.WrappedMessageError{Message: "docker compose down failed", Cause: err}
	}

	return nil
}

// getContainerLogs retrieves logs from containers.
func getContainerLogs(dir string) (string, error) {
	args := []string{"compose", "-f", filepath.Join(dir, "docker-compose.yml"), "logs", "--tail=100"}

	cmd := exec.Command("docker", args...)
	cmd.Dir = dir

	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", err
	}

	return string(output), nil
}

// isServerHealthy checks if the server is healthy. The gRPC port gets a TCP
// reachability probe (the suite's live gRPC coverage rides mock transports;
// only the listener presence matters here). REST must answer a real HTTPS 200
// on the liveness path with a certificate that verifies against the pinned
// integration CA — a wrong-scheme or stale server fails here instead of
// poisoning setup.
func isServerHealthy() bool {
	// Check gRPC port (50151 external -> 50051 internal)
	grpcConn, err := net.DialTimeout("tcp", "localhost:50151", 2*time.Second)
	if err != nil {
		return false
	}
	grpcConn.Close()

	// Verified HTTPS liveness check on the REST listener.
	tlsCfg, err := suiteTLS()
	if err != nil {
		return false
	}
	client := &http.Client{
		Timeout: 2 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig:   tlsCfg,
			DisableKeepAlives: true,
		},
	}
	endpoint := getEnvOrDefault("QUICBAAS_REST_ADDRESS", DefaultRESTAddress) + "/api/v1/health/live"
	resp, err := client.Get(endpoint)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// waitForHealthy waits for the server to become healthy.
func waitForHealthy(timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	ticker := time.NewTicker(DefaultHealthCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return &qberrors.MessageError{Message: "timeout waiting for server to become healthy"}
		case <-ticker.C:
			if isServerHealthy() {
				return nil
			}
			fmt.Print(".")
		}
	}
}

// getEnvOrDefault returns the environment variable value or a default.
func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// GetTestEnvironment returns the test environment.
// It returns nil if setup hasn't been called.
func GetTestEnvironment() *TestEnvironment {
	return testEnv
}

// GetGRPCAddress returns the gRPC server address.
func GetGRPCAddress() string {
	if testEnv == nil {
		return DefaultGRPCAddress
	}
	return testEnv.GRPCAddress
}

// GetRESTAddress returns the REST server address.
func GetRESTAddress() string {
	if testEnv == nil {
		return DefaultRESTAddress
	}
	return testEnv.RESTAddress
}

// SkipIfNoDocker fails the test if Docker is not available.
// Integration tests require Docker to run the test infrastructure.
func SkipIfNoDocker(t *testing.T) {
	t.Helper()
	require.False(t, mockOnlyMode, "Test requires Docker but running in MOCK_ONLY mode")
	require.True(t, isDockerAvailable(), "Docker must be available for integration tests")
}

// SkipIfNoServer fails the test if the server is not running.
// Integration tests require the QuicBaaS server to be healthy.
func SkipIfNoServer(t *testing.T) {
	t.Helper()
	require.False(t, mockOnlyMode, "Test requires server but running in MOCK_ONLY mode")
	require.True(t, isServerHealthy(), "QuicBaaS server must be running for integration tests")
}

// IsMockOnlyMode returns true if running in mock-only mode.
func IsMockOnlyMode() bool {
	return mockOnlyMode
}

// CleanupKey deletes a key after a test.
func CleanupKey(t *testing.T, entityType, key string) {
	t.Helper()
	// This will be implemented once we have a working client
	// For now, it's a placeholder for proper test cleanup
}

// GenerateUniqueKey generates a unique key for testing.
func GenerateUniqueKey(prefix string) string {
	return fmt.Sprintf("%s_%d_%d", prefix, time.Now().UnixNano(), os.Getpid())
}

// GenerateUniquePrefix generates a unique prefix for testing.
func GenerateUniquePrefix() string {
	return fmt.Sprintf("test_%d_%d", time.Now().UnixNano(), os.Getpid())
}

// WithTimeout creates a context with the specified timeout.
func WithTimeout(t *testing.T, timeout time.Duration) (context.Context, context.CancelFunc) {
	t.Helper()
	return context.WithTimeout(context.Background(), timeout)
}

// DefaultContext returns a context with default timeout for tests.
func DefaultContext(t *testing.T) (context.Context, context.CancelFunc) {
	t.Helper()
	return context.WithTimeout(context.Background(), 30*time.Second)
}
