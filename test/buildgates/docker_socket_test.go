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
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// IS-12 (#1171, go-xkms#1332): a job that bind-mounts the runner host's
// Docker socket into a container hands that container the host daemon, so
// code the job runs can re-tag a cached base or scanner image a later
// release build then uses. Owner ruling 130 option A: release.yml runs on
// the forge's dedicated `release` runner and pins the images it runs by
// digest. Runner-side isolation of PR jobs (option B) is Infrastructure's.

var (
	releaseWorkflowPath = filepath.Join(workflowsDir, "release.yml")

	// A bind spec naming the socket: `-v /var/run/docker.sock:...`,
	// `--volume=/run/docker.sock:...`, or a compose/services volume entry.
	dockerSocketMountRe = regexp.MustCompile(`docker\.sock:`)

	// The runs-on form ci.yml's publish-images job uses: the forge's
	// release runner, and ubuntu-latest on github.com, which has none.
	releaseRunsOn = "${{ github.server_url == 'https://github.com' && 'ubuntu-latest' || 'release' }}"

	runsOnRe = regexp.MustCompile(`(?m)^    runs-on:\s*(.+?)\s*$`)

	// An image reference release.yml runs: the scanner and the binaries'
	// build image. Each must carry a digest.
	releaseImageRefRe = regexp.MustCompile(`\b(aquasec/trivy|golang)(:[A-Za-z0-9._-]+)?(@sha256:[0-9a-f]{64})?\b`)
)

// nonCommentLines returns body without its YAML/shell comment lines, so a
// comment explaining the old form does not trip the checks.
func nonCommentLines(body string) []string {
	var out []string
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		out = append(out, line)
	}
	return out
}

func TestNoWorkflowMountsTheHostDockerSocket(t *testing.T) {
	for _, path := range workflowFiles(t) {
		for i, line := range strings.Split(readFile(t, path), "\n") {
			if dockerSocketMountRe.MatchString(line) {
				t.Errorf("%s:%d bind-mounts the host Docker socket (IS-12, #1171): %s",
					filepath.Base(path), i+1, strings.TrimSpace(line))
			}
		}
	}
}

func TestReleaseJobsRunOnTheReleaseRunner(t *testing.T) {
	order, bodies := ciJobs(t, readFile(t, releaseWorkflowPath))
	for _, job := range order {
		m := runsOnRe.FindStringSubmatch(bodies[job])
		if m == nil {
			t.Errorf("release.yml job %s has no runs-on", job)
			continue
		}
		if m[1] != releaseRunsOn {
			t.Errorf("release.yml job %s runs-on %q, want %q (ruling 130 A, #1171)", job, m[1], releaseRunsOn)
		}
	}
}

func TestReleaseImagesArePinnedByDigest(t *testing.T) {
	found := 0
	for _, line := range nonCommentLines(readFile(t, releaseWorkflowPath)) {
		for _, m := range releaseImageRefRe.FindAllStringSubmatch(line, -1) {
			if m[2] == "" && m[3] == "" {
				continue // a bare word such as the image name in prose
			}
			found++
			if m[3] == "" {
				t.Errorf("release.yml runs %s%s without a @sha256 digest (#1171): %s",
					m[1], m[2], strings.TrimSpace(line))
			}
		}
	}
	if found < 2 {
		t.Fatalf("found %d image references in release.yml, want the scanner and the build image; discovery has broken", found)
	}
}

func TestTheReleaseImageCheckCatchesATag(t *testing.T) {
	for _, tc := range []struct {
		line   string
		pinned bool
	}{
		{"aquasec/trivy:latest image \\", false},
		{"golang:1.27-bookworm \\", false},
		{"TRIVY_IMAGE: aquasec/trivy:0.76.0@sha256:" + strings.Repeat("a", 64), true},
	} {
		m := releaseImageRefRe.FindStringSubmatch(tc.line)
		if m == nil {
			t.Fatalf("%q: no image reference found", tc.line)
		}
		if got := m[3] != ""; got != tc.pinned {
			t.Errorf("%q: pinned=%v, want %v", tc.line, got, tc.pinned)
		}
	}
	if !dockerSocketMountRe.MatchString("-v /var/run/docker.sock:/var/run/docker.sock \\") {
		t.Error("the socket check misses a -v bind mount")
	}
}
