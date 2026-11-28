package buildgates

import (
	"bufio"
	"bytes"
	"go/build/constraint"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// publishedDockerfiles are the quicbaasd images release.yml publishes.
var publishedDockerfiles = []string{"Dockerfile", "Dockerfile.fips"}

// probeRouteFiles register the REST health routes the Kubernetes and compose
// probes hit (/api/v1/health/live and /ready). An image whose build tags
// exclude either file serves no probe route.
var probeRouteFiles = []string{"pkg/api/rest/routes.go", "pkg/apimodule/module_http.go"}

var buildTagsArg = regexp.MustCompile(`(?m)^ARG BUILD_TAGS=(?:"([^"]*)"|(\S*))\s*$`)

// dockerfileBuildTags returns the default of a Dockerfile's ARG BUILD_TAGS.
func dockerfileBuildTags(t *testing.T, name string) []string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(repoRoot, name))
	if err != nil {
		t.Fatal(err)
	}
	m := buildTagsArg.FindSubmatch(raw)
	if m == nil {
		t.Fatalf("%s has no ARG BUILD_TAGS default", name)
	}
	return strings.Fields(string(m[1]) + string(m[2]))
}

// buildConstraint returns a Go file's //go:build expression, or nil.
func buildConstraint(t *testing.T, file string) constraint.Expr {
	t.Helper()
	f, err := os.Open(filepath.Join(repoRoot, file))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "package ") {
			return nil
		}
		if constraint.IsGoBuild(line) {
			expr, err := constraint.Parse(line)
			if err != nil {
				t.Fatalf("%s: %v", file, err)
			}
			return expr
		}
	}
	return nil
}

// imageTagsProblems reports why an image built with tags would not serve the
// probe routes, or would not link.
func imageTagsProblems(tags []string, routes map[string]constraint.Expr) []string {
	set := map[string]bool{"linux": true, "unix": true, "amd64": true, "arm64": true, "gc": true}
	for _, tag := range tags {
		set[tag] = true
	}
	var problems []string
	if !set["gms_pure_go"] {
		problems = append(problems, "lacks gms_pure_go: the CGO_ENABLED=0 link fails")
	}
	for file, expr := range routes {
		if expr != nil && !expr.Eval(func(tag string) bool { return set[tag] }) {
			problems = append(problems, "excludes "+file+" (//go:build "+expr.String()+"): no probe route is served")
		}
	}
	return problems
}

func probeRouteConstraints(t *testing.T) map[string]constraint.Expr {
	t.Helper()
	routes := map[string]constraint.Expr{}
	for _, file := range probeRouteFiles {
		routes[file] = buildConstraint(t, file)
	}
	return routes
}

func TestPublishedImagesServeTheProbeRoutes(t *testing.T) {
	routes := probeRouteConstraints(t)
	for _, name := range publishedDockerfiles {
		for _, p := range imageTagsProblems(dockerfileBuildTags(t, name), routes) {
			t.Errorf("%s: ARG BUILD_TAGS default %s", name, p)
		}
	}

	// The default only ships if the release never overrides it.
	release, err := os.ReadFile(filepath.Join(repoRoot, ".github/workflows/release.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(release, []byte("BUILD_TAGS")) {
		t.Error("release.yml sets BUILD_TAGS: the published image no longer carries the Dockerfile default this test checks")
	}
}

func TestImageTagsProblemsRejectsARestlessImage(t *testing.T) {
	routes := probeRouteConstraints(t)
	if p := imageTagsProblems([]string{"gms_pure_go"}, routes); len(p) != len(probeRouteFiles) {
		t.Errorf("gms_pure_go alone: problems = %q, want one per probe route file", p)
	}
	if p := imageTagsProblems([]string{"rest"}, routes); len(p) != 1 {
		t.Errorf("rest without gms_pure_go: problems = %q, want the link failure", p)
	}
	if p := imageTagsProblems([]string{"rest", "gms_pure_go"}, routes); len(p) != 0 {
		t.Errorf("rest gms_pure_go: problems = %q, want none", p)
	}
}
