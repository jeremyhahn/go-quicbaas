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

package softwarefactoryworker

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// These tests run the worker-image workflow's "already published?" step
// (build-publish, id: existing) with a fake docker on PATH. Only a registry
// answer of "not found" may count as unpublished: any other lookup failure
// must fail the step, so the build and push steps never run and a published
// tag is never overwritten. No network, registry or engine is touched.

const workflowPath = "../../../.github/workflows/worker-image.yml"

type workflowFile struct {
	Jobs map[string]struct {
		Steps []struct {
			ID  string `yaml:"id"`
			Run string `yaml:"run"`
		} `yaml:"steps"`
	} `yaml:"jobs"`
}

// existingStepScript returns the run: script of build-publish's step with
// id: existing.
func existingStepScript(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile(workflowPath)
	if err != nil {
		t.Fatalf("read %s: %v", workflowPath, err)
	}
	var wf workflowFile
	if err := yaml.Unmarshal(raw, &wf); err != nil {
		t.Fatalf("parse %s: %v", workflowPath, err)
	}
	job, ok := wf.Jobs["build-publish"]
	if !ok {
		t.Fatalf("%s has no build-publish job", workflowPath)
	}
	for _, step := range job.Steps {
		if step.ID != "existing" {
			continue
		}
		if strings.TrimSpace(step.Run) == "" {
			t.Fatalf("build-publish step id: existing has no run: script")
		}
		return step.Run
	}
	t.Fatalf("build-publish has no step with id: existing")
	return ""
}

const fakeDockerScript = `#!/bin/sh
printf '%s' "$FAKE_DOCKER_STDOUT"
printf '%s' "$FAKE_DOCKER_STDERR" >&2
exit "$FAKE_DOCKER_EXIT"
`

func TestWorkflowBuildPublish_OnlyNotFoundCountsAsUnpublished(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash is not on PATH")
	}
	script := existingStepScript(t)

	const (
		repo   = "ghcr.io/example/img"
		tag    = "1.0.0-cc1.2.3-cx4.5.6"
		digest = "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	)

	cases := []struct {
		name      string
		stdout    string
		stderr    string
		exit      string
		needJQ    bool
		wantOK    bool
		wantLines []string
	}{
		{
			name:      "not found",
			stderr:    "ERROR: " + repo + ":" + tag + ": not found\n",
			exit:      "1",
			wantOK:    true,
			wantLines: []string{"exists=false"},
		},
		{
			name:      "manifest unknown",
			stderr:    "MANIFEST_UNKNOWN: manifest unknown\n",
			exit:      "1",
			wantOK:    true,
			wantLines: []string{"exists=false"},
		},
		{
			name:   "server error",
			stderr: "unexpected status from HEAD request: 503 Service Unavailable\n",
			exit:   "1",
		},
		{
			name:   "timeout",
			stderr: "failed to do request: context deadline exceeded\n",
			exit:   "1",
		},
		{
			name:   "denied",
			stderr: "denied: denied\n",
			exit:   "1",
		},
		{
			name: "empty stderr",
			exit: "1",
		},
		{
			name:      "published",
			stdout:    `{"digest":"` + digest + `"}`,
			exit:      "0",
			needJQ:    true,
			wantOK:    true,
			wantLines: []string{"exists=true", "digest=" + digest},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.needJQ {
				if _, err := exec.LookPath("jq"); err != nil {
					t.Skip("jq is not on PATH")
				}
			}
			dir := t.TempDir()
			binDir := filepath.Join(dir, "bin")
			if err := os.Mkdir(binDir, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(binDir, "docker"), []byte(fakeDockerScript), 0o755); err != nil {
				t.Fatal(err)
			}
			output := filepath.Join(dir, "github_output")
			if err := os.WriteFile(output, nil, 0o644); err != nil {
				t.Fatal(err)
			}

			cmd := exec.Command(bash, "-c", script)
			cmd.Env = append(os.Environ(),
				"PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"),
				"REPO="+repo,
				"TAG="+tag,
				"GITHUB_OUTPUT="+output,
				"FAKE_DOCKER_STDOUT="+tc.stdout,
				"FAKE_DOCKER_STDERR="+tc.stderr,
				"FAKE_DOCKER_EXIT="+tc.exit,
			)
			combined, runErr := cmd.CombinedOutput()

			got, err := os.ReadFile(output)
			if err != nil {
				t.Fatal(err)
			}
			lines := strings.Split(strings.TrimSpace(string(got)), "\n")

			if tc.wantOK {
				if runErr != nil {
					t.Fatalf("step failed: %v\n%s", runErr, combined)
				}
				for _, want := range tc.wantLines {
					found := false
					for _, line := range lines {
						if line == want {
							found = true
						}
					}
					if !found {
						t.Errorf("GITHUB_OUTPUT has no %q line:\n%s", want, got)
					}
				}
				return
			}

			if runErr == nil {
				t.Fatalf("step exited 0 on a lookup error, so the build and push would run:\n%s", combined)
			}
			for _, line := range lines {
				if strings.HasPrefix(line, "exists=") {
					t.Errorf("GITHUB_OUTPUT has %q after a lookup error:\n%s", line, got)
				}
			}
		})
	}
}
