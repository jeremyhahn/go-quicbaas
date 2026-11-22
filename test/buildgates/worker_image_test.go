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

var workerImageWorkflowPath = filepath.Join(workflowsDir, "worker-image.yml")

// TestWorkerImageMoveCurrentKeepsTheSourceManifest pins the move of
// `:current` to the exact manifest build-publish pushed (#2635). With
// buildx's default --prefer-index=true, imagetools create wraps a
// single-platform manifest in a new image index, so `:current` would name a
// new digest and the job would fail after the tag had already moved.
// --prefer-index=false retags a single-platform manifest as itself and an
// index as the same index. The move runs only on a scheduled run after
// build-publish and post-digest succeeded.
func TestWorkerImageMoveCurrentKeepsTheSourceManifest(t *testing.T) {
	body := readFile(t, workerImageWorkflowPath)

	var creates []string
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") || !strings.Contains(trimmed, "docker buildx imagetools create") {
			continue
		}
		creates = append(creates, trimmed)
	}
	if len(creates) == 0 {
		t.Fatalf("%s has no `docker buildx imagetools create` line; the move-current retag is gone", workerImageWorkflowPath)
	}
	preferIndexFalse := regexp.MustCompile(`(^|\s)--prefer-index=false(\s|$)`)
	for _, line := range creates {
		if !preferIndexFalse.MatchString(line) {
			t.Errorf("%s: imagetools create without --prefer-index=false would wrap a "+
				"single-platform manifest in a new index and change its digest:\n%s", workerImageWorkflowPath, line)
		}
	}

	_, jobs := ciJobs(t, body)
	job, ok := jobs["move-current"]
	if !ok {
		t.Fatalf("%s has no move-current job", workerImageWorkflowPath)
	}
	m := regexp.MustCompile(`(?m)^    if: (.*)$`).FindStringSubmatch(job)
	if m == nil {
		t.Fatalf("move-current has no job-level `if:`; it would move current on every run")
	}
	for _, term := range []string{
		"github.event_name == 'schedule'",
		"needs.build-publish.result == 'success'",
		"needs.post-digest.result == 'success'",
	} {
		if !strings.Contains(m[1], term) {
			t.Errorf("move-current's `if:` (%s) lacks %s; current must move only on a green scheduled nightly", m[1], term)
		}
	}
}
