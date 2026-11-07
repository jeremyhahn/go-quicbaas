//go:build rest

package rest

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// composeHealthFiles are the compose files whose healthchecks probe the
// daemon's REST listener on localhost:8080 (go-quicbaas#2228). Paths are
// relative to this package directory.
var composeHealthFiles = []string{
	"../../../docker-compose.yml",
	"../../../deployments/agents/docker-compose.yml",
	"../../../test/integration/docker-compose.kms-test.yml",
	"../../../test/integration/docker-compose.perf.yml",
	"../../../test/integration/docker-compose.cluster.yml",
	"../../../test/integration/docker-compose.integration.yml",
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
	for _, file := range composeHealthFiles {
		t.Run(strings.TrimPrefix(file, "../../../"), func(t *testing.T) {
			compose, err := os.ReadFile(file)
			if err != nil {
				t.Fatalf("read %s: %v", file, err)
			}
			if len(healthCheckURL.FindAll(compose, -1)) == 0 {
				t.Fatalf("%s has no localhost:8080 healthcheck", file)
			}
			for _, path := range unservedHealthPaths(compose, served) {
				t.Errorf("%s: healthcheck path %q is not a REST route", file, path)
			}
		})
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
