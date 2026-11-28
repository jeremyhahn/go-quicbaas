//go:build rest

package rest

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// composeHealthGlobs match every compose file in the repository; each one's
// healthchecks probe the daemon's REST listener on localhost:8080
// (go-quicbaas#2228). Patterns are relative to this package directory.
var composeHealthGlobs = []string{
	"../../../docker-compose*.yml",
	"../../../deployments/*/docker-compose*.yml",
	"../../../test/integration/docker-compose*.yml",
	"../../../test/integration/*/docker-compose*.yml",
}

// composeHealthFiles expands composeHealthGlobs. A malformed pattern, or a
// pattern that matches no file, fails the test.
func composeHealthFiles(t *testing.T) []string {
	t.Helper()
	var files []string
	for _, pattern := range composeHealthGlobs {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			t.Fatalf("glob %s: %v", pattern, err)
		}
		if len(matches) == 0 {
			t.Fatalf("glob %s matched no compose file", pattern)
		}
		files = append(files, matches...)
	}
	return files
}

// healthCheckURL matches the path of a URL that targets the daemon's REST
// port, e.g. "http://localhost:8080/api/v1/health/live".
var healthCheckURL = regexp.MustCompile(`https?://localhost:8080(/[^"'\s\]]*)`)

// servedGETPaths returns every GET pattern the REST route table serves.
func servedGETPaths() map[string]struct{} {
	served := map[string]struct{}{}
	for _, spec := range Routes() {
		if spec.Method == "GET" {
			served[spec.Pattern] = struct{}{}
		}
	}
	return served
}

// unservedHealthPaths returns the localhost:8080 paths in compose that the
// REST router does not serve.
func unservedHealthPaths(compose []byte, served map[string]struct{}) []string {
	var missing []string
	for _, match := range healthCheckURL.FindAllSubmatch(compose, -1) {
		path := string(match[1])
		if _, ok := served[path]; !ok {
			missing = append(missing, path)
		}
	}
	return missing
}

func TestHealthPath_ComposeHealthchecksHitServedRoutes(t *testing.T) {
	served := servedGETPaths()
	probes := 0
	for _, file := range composeHealthFiles(t) {
		compose, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read %s: %v", file, err)
		}
		probes += len(healthCheckURL.FindAll(compose, -1))
		t.Run(strings.TrimPrefix(file, "../../../"), func(t *testing.T) {
			for _, path := range unservedHealthPaths(compose, served) {
				t.Errorf("%s: healthcheck path %q is not a REST route", file, path)
			}
		})
	}
	if probes == 0 {
		t.Fatal("no compose file has a localhost:8080 healthcheck")
	}
}

func TestHealthPath_UnservedPathIsReported(t *testing.T) {
	compose := []byte(`services:
  quicbaas:
    healthcheck:
      test: ["CMD", "wget", "--quiet", "--spider", "http://localhost:8080/v1/health/live"]
`)
	missing := unservedHealthPaths(compose, servedGETPaths())
	if len(missing) != 1 || missing[0] != "/v1/health/live" {
		t.Fatalf("unservedHealthPaths = %q, want [\"/v1/health/live\"]", missing)
	}
}

// kubernetesProbePath matches the httpGet path of a Kubernetes probe.
var kubernetesProbePath = regexp.MustCompile(`(?m)^\s+path:\s*(/\S*)\s*$`)

// The StatefulSet's startup, liveness and readiness probes hit REST routes the
// node serves (go-quicbaas#1696): a probe on an unserved path fails every
// pod, and the cluster never becomes ready.
func TestHealthPath_KubernetesProbesHitServedRoutes(t *testing.T) {
	const file = "../../../deployments/kubernetes/statefulset.yaml"
	manifest, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("read %s: %v", file, err)
	}
	served := servedGETPaths()
	probes := kubernetesProbePath.FindAllSubmatch(manifest, -1)
	if len(probes) < 3 {
		t.Fatalf("%s has %d probe paths, want the startup, liveness and readiness probes", file, len(probes))
	}
	for _, m := range probes {
		if _, ok := served[string(m[1])]; !ok {
			t.Errorf("%s: probe path %q is not a REST route", file, m[1])
		}
	}
}

// composeTLSFiles are the compose files whose healthchecks go-quicbaas#1696
// moved to /api/v1/health/live. The REST listener is TLS-only, so each probe
// must speak HTTPS and verify the daemon's own CA chain.
var composeTLSFiles = []string{
	"../../../docker-compose.yml",
	"../../../deployments/agents/docker-compose.yml",
	"../../../test/integration/containers/docker-compose.agents.yml",
	"../../../test/integration/docker-compose.kms-hardware.yml",
	"../../../test/integration/docker-compose.kms-test.yml",
	"../../../test/integration/docker-compose.perf.yml",
	"../../../test/integration/docker-compose.standalone.yml",
	"../../../test/integration/docker-compose.test.yml",
}

// composeTLSInsecureFlags are the flags that turn certificate verification off.
var composeTLSInsecureFlags = []string{`"-k"`, "-ksf", "--insecure", "--no-check-certificate"}

// composeTLSProblems returns one message per localhost:8080 line that does not
// probe over HTTPS with --cacert /data/certs/ca-chain.crt, or that skips
// verification, and one message when no line probes localhost:8080 at all.
func composeTLSProblems(text string) []string {
	var problems []string
	probes := 0
	for i, line := range strings.Split(text, "\n") {
		if !strings.Contains(line, "localhost:8080") {
			continue
		}
		probes++
		var why []string
		if !strings.Contains(line, "https://localhost:8080") {
			why = append(why, "not https")
		}
		if !strings.Contains(line, "--cacert") || !strings.Contains(line, "/data/certs/ca-chain.crt") {
			why = append(why, "does not trust /data/certs/ca-chain.crt with --cacert")
		}
		for _, flag := range composeTLSInsecureFlags {
			if strings.Contains(line, flag) {
				why = append(why, "skips verification with "+flag)
			}
		}
		if len(why) > 0 {
			problems = append(problems, fmt.Sprintf("line %d: %s: %s", i+1, strings.Join(why, ", "), strings.TrimSpace(line)))
		}
	}
	if probes == 0 {
		problems = append(problems, "no line probes localhost:8080")
	}
	return problems
}

func TestHealthPath_ComposeHealthchecksVerifyTLS(t *testing.T) {
	for _, file := range composeTLSFiles {
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read %s: %v", file, err)
		}
		if problems := composeTLSProblems(string(raw)); len(problems) > 0 {
			t.Errorf("%s:\n  %s", strings.TrimPrefix(file, "../../../"), strings.Join(problems, "\n  "))
		}
	}
}

func TestHealthPath_ComposeTLSCheckRejectsPlainAndInsecure(t *testing.T) {
	bad := map[string]string{
		"old wget":    `      test: ["CMD", "wget", "--quiet", "--tries=1", "--spider", "http://localhost:8080/api/v1/health/live"]`,
		"old curl":    `      test: ["CMD", "curl", "-f", "http://localhost:8080/api/v1/health/live"]`,
		"insecure":    `      test: ["CMD", "curl", "-ksf", "https://localhost:8080/api/v1/health/live"]`,
		"no --cacert": `      test: ["CMD", "curl", "-fsS", "https://localhost:8080/api/v1/health/live"]`,
	}
	for name, line := range bad {
		if got := composeTLSProblems(line); len(got) != 1 {
			t.Errorf("%s: composeTLSProblems = %q, want one message", name, got)
		}
	}
	good := `      test: ["CMD", "curl", "-fsS", "--cacert", "/data/certs/ca-chain.crt", "https://localhost:8080/api/v1/health/live"]`
	if got := composeTLSProblems(good); len(got) != 0 {
		t.Errorf("composeTLSProblems(good) = %q, want none", got)
	}
}
