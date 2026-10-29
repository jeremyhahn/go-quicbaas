// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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

package buildgates

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// buildMarker is what the stand-in DEVCONTAINER_BUILD prints instead of
// building, so a test sees whether docker-build ran the build.
const buildMarker = "STAND-IN BUILD"

// runDockerBuild runs the Makefile's docker-build with stand-ins on the
// command line: docker is the DOCKER command (`true` holds every image,
// `false` holds none) and the build only prints buildMarker. Nothing touches
// Docker, the network or the checkout.
func runDockerBuild(t *testing.T, docker string, vars ...string) string {
	t.Helper()
	args := append([]string{"-s", "--no-print-directory", "-C", repoRoot, "docker-build",
		"DOCKER=" + docker, "DEVCONTAINER_BUILD=echo " + buildMarker}, vars...)
	cmd := exec.Command("make", args...)
	// An IT_IMAGE in the caller's environment would change the case under test.
	cmd.Env = append(os.Environ(), "IT_IMAGE=")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("make %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

// TestDockerBuildSkipsOnlyUnderAContentAddressedTag pins Codex 7525's P1:
// docker-build may skip the integration build only when the integration tag
// is IT_IMAGE, whose hash covers every build input. The default tag omits
// INTEGRATION_BUILD_TAGS and untracked files' contents, so under it the build
// always runs and compose checks the inputs.
func TestDockerBuildSkipsOnlyUnderAContentAddressedTag(t *testing.T) {
	cases := []struct {
		name      string
		docker    string
		vars      []string
		wantBuild bool
	}{
		{"default tag, images present", "true", nil, true},
		{"default tag, images present, other build tags", "true",
			[]string{"INTEGRATION_BUILD_TAGS=integration buildgatesprobe"}, true},
		{"IT_IMAGE set, images present", "true",
			[]string{"IT_IMAGE=go-quicbaas-ci-it:buildgates"}, false},
		{"IT_IMAGE set, image absent", "false",
			[]string{"IT_IMAGE=go-quicbaas-ci-it:buildgates"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := runDockerBuild(t, tc.docker, tc.vars...)
			if built := strings.Contains(out, buildMarker); built != tc.wantBuild {
				t.Errorf("docker-build ran the build = %v, want %v; output:\n%s", built, tc.wantBuild, out)
			}
		})
	}
}

// itImageHashDefinition returns the Makefile's IT_IMAGE_HASH definition with
// its continuation lines.
func itImageHashDefinition(t *testing.T) string {
	t.Helper()
	makefile := readFile(t, makefilePath)
	start := strings.Index(makefile, "\nIT_IMAGE_HASH = ")
	if start < 0 {
		t.Fatal("Makefile defines no IT_IMAGE_HASH")
	}
	var def []string
	for _, line := range strings.Split(makefile[start+1:], "\n") {
		def = append(def, line)
		if !strings.HasSuffix(line, `\`) {
			break
		}
	}
	return strings.Join(def, "\n") + "\n"
}

// TestITImageHashCoversTheInputsTheDefaultTagOmits proves the key the skip
// trusts changes with the two inputs Codex 7525 named: INTEGRATION_BUILD_TAGS
// and an untracked file's contents. It runs the Makefile's own IT_IMAGE_HASH
// definition in a temporary git repository, never the checkout.
func TestITImageHashCoversTheInputsTheDefaultTagOmits(t *testing.T) {
	tmp := t.TempDir()
	hashMakefile := filepath.Join(tmp, "hash.mk")
	recipe := "TOOLCHAIN_IMAGE = toolchain\nGO_VERSION = go\nGO_IMAGE_DIGEST = digest\n" +
		"INTEGRATION_BUILD_TAGS = integration\n" + itImageHashDefinition(t) +
		"print-hash:\n\t@echo $(IT_IMAGE_HASH)\n"
	if err := os.WriteFile(hashMakefile, []byte(recipe), 0o600); err != nil {
		t.Fatal(err)
	}

	repo := filepath.Join(tmp, "repo")
	if err := os.Mkdir(repo, 0o700); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "init", "-q")
	writeRepoFile(t, repo, "tracked.go", "package tracked\n")
	runGit(t, repo, "add", "tracked.go")
	runGit(t, repo, "commit", "-q", "-m", "tracked")
	writeRepoFile(t, repo, "untracked.go", "package untracked\n")

	hash := func(vars ...string) string {
		t.Helper()
		args := append([]string{"-s", "--no-print-directory", "-C", repo, "-f", hashMakefile, "print-hash"}, vars...)
		cmd := exec.Command("make", args...)
		cmd.Env = gitEnv(repo)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("make %s: %v\n%s", strings.Join(args, " "), err, out)
		}
		return strings.TrimSpace(string(out))
	}

	base := hash()
	if len(base) != 16 {
		t.Fatalf("IT_IMAGE_HASH = %q, want 16 hex digits", base)
	}
	if again := hash(); again != base {
		t.Fatalf("IT_IMAGE_HASH is not stable: %q then %q", base, again)
	}
	if tags := hash("INTEGRATION_BUILD_TAGS=integration buildgatesprobe"); tags == base {
		t.Errorf("IT_IMAGE_HASH did not change with INTEGRATION_BUILD_TAGS (%s)", base)
	}
	// COVER=1 builds a coverage-instrumented daemon (Codex 7582), so a
	// coverage run must not reuse the ordinary image.
	cover := hash("COVER=1")
	if cover == base {
		t.Errorf("IT_IMAGE_HASH did not change with COVER=1 (%s)", base)
	}
	if again := hash("COVER=1"); again != cover {
		t.Errorf("IT_IMAGE_HASH with COVER=1 is not stable: %q then %q", cover, again)
	}
	writeRepoFile(t, repo, "untracked.go", "package untracked // edited\n")
	if edited := hash(); edited == base {
		t.Errorf("IT_IMAGE_HASH did not change with an untracked file's contents (%s)", base)
	}
}

// TestToolchainImageTagCoversTheGoBaseImage checks that the toolchain tag
// hashes the effective GO_VERSION and GO_IMAGE_DIGEST, not only the files:
// docker-build-toolchain-image skips the build when the tag is present, so a
// command-line override of either must name a different image (Codex 8189).
func TestToolchainImageTagCoversTheGoBaseImage(t *testing.T) {
	tag := func(vars ...string) string {
		t.Helper()
		args := append([]string{"-s", "--no-print-directory", "-C", repoRoot, "toolchain-image"}, vars...)
		cmd := exec.Command("make", args...)
		// Stdout alone: a $(shell) command's stderr is not part of the value.
		out, err := cmd.Output()
		if err != nil {
			var exitErr *exec.ExitError
			if errors.As(err, &exitErr) {
				t.Fatalf("make %s: %v\n%s", strings.Join(args, " "), err, exitErr.Stderr)
			}
			t.Fatalf("make %s: %v", strings.Join(args, " "), err)
		}
		return strings.TrimSpace(string(out))
	}

	const (
		otherVersion = "GO_VERSION=0.0.0-buildgatesprobe"
		otherDigest  = "GO_IMAGE_DIGEST=sha256:" +
			"0000000000000000000000000000000000000000000000000000000000000000"
	)
	base := tag()
	if again := tag(); again != base {
		t.Fatalf("TOOLCHAIN_IMAGE is not stable: %q then %q", base, again)
	}
	seen := map[string]string{base: "no override"}
	for _, override := range [][]string{{otherVersion}, {otherDigest}, {otherVersion, otherDigest}} {
		got := tag(override...)
		name := strings.Join(override, " ")
		if prev, dup := seen[got]; dup {
			t.Errorf("TOOLCHAIN_IMAGE with %s = %q, the same as with %s", name, got, prev)
		}
		seen[got] = name
	}
}

// runToolchainBuild runs docker-build-toolchain-image with the given DOCKER
// command and a DEVCONTAINER_BUILD that always fails, the way a build whose
// export lost the race for the tag fails.
func runToolchainBuild(t *testing.T, docker string) (string, error) {
	t.Helper()
	cmd := exec.Command("make", "-s", "--no-print-directory", "-C", repoRoot,
		"docker-build-toolchain-image", "DOCKER="+docker, "DEVCONTAINER_BUILD=false")
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// TestToolchainBuildPassesWhenAConcurrentJobBuiltTheTag pins the CI 71256
// race: two jobs on one daemon both find the tag absent and both build it, and
// the second build fails because the first already wrote the tag. The tag is
// content-addressed, so the image present afterwards is the one wanted.
func TestToolchainBuildPassesWhenAConcurrentJobBuiltTheTag(t *testing.T) {
	dir := t.TempDir()
	docker := filepath.Join(dir, "docker")
	// image inspect fails on the first call (tag absent before the build)
	// and succeeds on every later one (tag written by the other job).
	stub := "#!/bin/sh\n" +
		"if [ \"$1 $2\" = \"image inspect\" ]; then\n" +
		"\tif [ -e '" + dir + "/inspected' ]; then exit 0; fi\n" +
		"\t: > '" + dir + "/inspected'; exit 1\n" +
		"fi\n"
	if err := os.WriteFile(docker, []byte(stub), 0o755); err != nil {
		t.Fatal(err)
	}
	out, err := runToolchainBuild(t, docker)
	if err != nil {
		t.Fatalf("docker-build-toolchain-image failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, "built concurrently") {
		t.Errorf("output does not report the concurrent build:\n%s", out)
	}
}

// TestToolchainBuildFailsWhenNoImageExistsAfterAFailedBuild checks the race
// allowance does not hide a real build failure.
func TestToolchainBuildFailsWhenNoImageExistsAfterAFailedBuild(t *testing.T) {
	out, err := runToolchainBuild(t, "false")
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("docker-build-toolchain-image error = %v, want a non-zero exit; output:\n%s", err, out)
	}
	if strings.Contains(out, "built concurrently") {
		t.Errorf("output reports a concurrent build that did not happen:\n%s", out)
	}
}
