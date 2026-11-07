//go:build rest

package rest

import (
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
