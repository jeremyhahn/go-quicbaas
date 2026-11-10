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
	"regexp"
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
	On struct {
		WorkflowDispatch struct {
			Inputs map[string]any `yaml:"inputs"`
		} `yaml:"workflow_dispatch"`
	} `yaml:"on"`
	Jobs map[string]struct {
		Name        string            `yaml:"name"`
		If          string            `yaml:"if"`
		Permissions map[string]string `yaml:"permissions"`
		Steps       []struct {
			ID   string `yaml:"id"`
			Name string `yaml:"name"`
			If   string `yaml:"if"`
			Run  string `yaml:"run"`
		} `yaml:"steps"`
	} `yaml:"jobs"`
}

// loadWorkflow returns the raw worker-image workflow and its parse.
func loadWorkflow(t *testing.T) (string, workflowFile) {
	t.Helper()
	raw, err := os.ReadFile(workflowPath)
	if err != nil {
		t.Fatalf("read %s: %v", workflowPath, err)
	}
	var wf workflowFile
	if err := yaml.Unmarshal(raw, &wf); err != nil {
		t.Fatalf("parse %s: %v", workflowPath, err)
	}
	return string(raw), wf
}

// The workflow is one job that holds only GITHUB_TOKEN with contents: read and
// packages: write: no registry secret, and no job may write issues.
func TestWorkflowBuildPublish_OnlyJob(t *testing.T) {
	raw, wf := loadWorkflow(t)
	if len(wf.Jobs) != 1 {
		names := make([]string, 0, len(wf.Jobs))
		for name := range wf.Jobs {
			names = append(names, name)
		}
		t.Fatalf("jobs = %v, want exactly [build-publish]", names)
	}
	job, ok := wf.Jobs["build-publish"]
	if !ok {
		t.Fatalf("%s has no build-publish job", workflowPath)
	}
	wantIf := "${{ github.event_name == 'schedule' || github.event_name == 'workflow_dispatch' }}"
	if job.If != wantIf {
		t.Errorf("build-publish if = %q, want %q", job.If, wantIf)
	}
	wantPerms := map[string]string{"contents": "read", "packages": "write"}
	if len(job.Permissions) != len(wantPerms) {
		t.Errorf("build-publish permissions = %v, want %v", job.Permissions, wantPerms)
	}
	for k, v := range wantPerms {
		if job.Permissions[k] != v {
			t.Errorf("build-publish permissions = %v, want %v", job.Permissions, wantPerms)
			break
		}
	}
	inputs := wf.On.WorkflowDispatch.Inputs
	if len(inputs) != 2 || inputs["claude_code_version"] == nil || inputs["codex_version"] == nil {
		t.Errorf("workflow_dispatch inputs = %v, want only claude_code_version and codex_version", inputs)
	}
	for _, s := range []string{"REGISTRY_TOKEN", "REGISTRY_USER", "secrets["} {
		if strings.Contains(raw, s) {
			t.Errorf("%s contains %q", workflowPath, s)
		}
	}
	if regexp.MustCompile(`(?m)^\s*issues:`).MatchString(raw) {
		t.Errorf("%s has an issues: permission key", workflowPath)
	}
	for _, ref := range regexp.MustCompile(`secrets\.[A-Za-z0-9_]*`).FindAllString(raw, -1) {
		if ref != "secrets.GITHUB_TOKEN" {
			t.Errorf("%s references %s; only secrets.GITHUB_TOKEN is allowed", workflowPath, ref)
		}
	}
}

// Both secret scans run before the push, under the push's own condition, fail
// on any finding, and carry no severity filter, skip or ignore file.
func TestWorkflowBuildPublish_SecretScansGateThePush(t *testing.T) {
	_, wf := loadWorkflow(t)
	job, ok := wf.Jobs["build-publish"]
	if !ok {
		t.Fatalf("%s has no build-publish job", workflowPath)
	}
	idx := map[string]int{}
	push := -1
	for i, step := range job.Steps {
		if step.ID == "secret-scan" || step.ID == "layer-scan" {
			idx[step.ID] = i
		}
		if push < 0 && strings.Contains(step.Run, "make push-worker-image") {
			push = i
		}
	}
	if push < 0 {
		t.Fatalf("build-publish has no step that runs make push-worker-image")
	}
	for _, id := range []string{"secret-scan", "layer-scan"} {
		i, ok := idx[id]
		if !ok {
			t.Errorf("build-publish has no step with id: %s", id)
			continue
		}
		step := job.Steps[i]
		if i >= push {
			t.Errorf("step %s (#%d) does not come before the push step (#%d)", id, i, push)
		}
		if step.If != job.Steps[push].If {
			t.Errorf("step %s if = %q, want the push step's %q", id, step.If, job.Steps[push].If)
		}
		for _, s := range []string{"--scanners secret", "--exit-code 1"} {
			if !strings.Contains(step.Run, s) {
				t.Errorf("step %s run: lacks %q", id, s)
			}
		}
		for _, s := range []string{"--severity", "--skip-dirs", "--skip-files", "--secret-config", "--ignorefile"} {
			if strings.Contains(step.Run, s) {
				t.Errorf("step %s run: contains %q", id, s)
			}
		}
	}
	if i, ok := idx["layer-scan"]; ok {
		for _, s := range []string{"docker save", ".Layers[]"} {
			if !strings.Contains(job.Steps[i].Run, s) {
				t.Errorf("step layer-scan run: lacks %q", s)
			}
		}
	}
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

// stepRun returns the run: script of build-publish's step with the given id.
func stepRun(t *testing.T, id string) string {
	t.Helper()
	_, wf := loadWorkflow(t)
	job, ok := wf.Jobs["build-publish"]
	if !ok {
		t.Fatalf("%s has no build-publish job", workflowPath)
	}
	for _, step := range job.Steps {
		if step.ID != id {
			continue
		}
		if strings.TrimSpace(step.Run) == "" {
			t.Fatalf("build-publish step id: %s has no run: script", id)
		}
		return step.Run
	}
	t.Fatalf("build-publish has no step with id: %s", id)
	return ""
}

// The image secret scan also covers the image config, and the layer scan
// adds the config JSON to the tree it scans.
func TestWorkflowBuildPublish_SecretScanCoversImageConfig(t *testing.T) {
	if run := stepRun(t, "secret-scan"); !strings.Contains(run, "--image-config-scanners secret") {
		t.Errorf("step secret-scan run: lacks %q", "--image-config-scanners secret")
	}
	run := stepRun(t, "layer-scan")
	for _, s := range []string{".[0].Config", "config.json"} {
		if !strings.Contains(run, s) {
			t.Errorf("step layer-scan run: lacks %q", s)
		}
	}
}

const fakeNpmScript = `#!/bin/sh
case "$*" in
  *dist.integrity*) printf '%s\n' "$FAKE_INTEGRITY" ;;
  *) echo 1.2.3 ;;
esac
`

// The resolve step records the Gemini CLI pin and the npm sha512 integrity of
// each CLI package, and fails on a missing or non-sha512 integrity.
func TestWorkflowBuildPublish_ResolveRecordsIntegrity(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash is not on PATH")
	}
	script := stepRun(t, "resolve")
	const valid = "sha512-AbCd0123+/xyz=="

	cases := []struct {
		name      string
		integrity string
		wantOK    bool
	}{
		{name: "sha512", integrity: valid, wantOK: true},
		{name: "sha1", integrity: "sha1-abc"},
		{name: "empty", integrity: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			binDir := filepath.Join(dir, "bin")
			if err := os.Mkdir(binDir, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(binDir, "npm"), []byte(fakeNpmScript), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "VERSION"), []byte("1.0.0\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "Makefile"), []byte("GEMINI_CLI_VERSION ?= 0.64.0\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			output := filepath.Join(dir, "github_output")
			if err := os.WriteFile(output, nil, 0o644); err != nil {
				t.Fatal(err)
			}

			cmd := exec.Command(bash, "-c", script)
			cmd.Dir = dir
			cmd.Env = append(os.Environ(),
				"PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"),
				"IMAGE=quicbaas-software-factory-worker",
				"GITHUB_REPOSITORY_OWNER=Example",
				"GITHUB_OUTPUT="+output,
				"CC_INPUT=",
				"CX_INPUT=",
				"FAKE_INTEGRITY="+tc.integrity,
			)
			combined, runErr := cmd.CombinedOutput()
			got, err := os.ReadFile(output)
			if err != nil {
				t.Fatal(err)
			}
			lines := strings.Split(strings.TrimSpace(string(got)), "\n")

			if !tc.wantOK {
				if runErr == nil {
					t.Fatalf("step exited 0 with integrity %q:\n%s", tc.integrity, combined)
				}
				if !strings.Contains(string(combined), "has no sha512 integrity on npm") {
					t.Errorf("output does not show the integrity error:\n%s", combined)
				}
				for _, line := range lines {
					if strings.HasPrefix(line, "cc_int=") {
						t.Errorf("GITHUB_OUTPUT has %q after a bad integrity:\n%s", line, got)
					}
				}
				return
			}
			if runErr != nil {
				t.Fatalf("step failed: %v\n%s", runErr, combined)
			}
			want := []string{"gm=0.64.0"}
			for _, k := range []string{"cc_int", "cc_x64_int", "cx_int", "cx_x64_int", "gm_int"} {
				want = append(want, k+"="+valid)
			}
			for _, w := range want {
				found := false
				for _, line := range lines {
					if line == w {
						found = true
					}
				}
				if !found {
					t.Errorf("GITHUB_OUTPUT has no %q line:\n%s", w, got)
				}
			}
		})
	}
}

// The digest record carries each CLI's version and npm integrity.
func TestWorkflowBuildPublish_RecordCarriesIntegrity(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash is not on PATH")
	}
	script := stepRun(t, "record")
	const (
		repo   = "ghcr.io/example/img"
		tag    = "1.0.0-cc1.2.3-cx4.5.6"
		digest = "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	)
	dir := t.TempDir()
	summary := filepath.Join(dir, "summary")
	output := filepath.Join(dir, "github_output")
	cmd := exec.Command(bash, "-c", script)
	cmd.Env = append(os.Environ(),
		"REPO="+repo,
		"TAG="+tag,
		"CC=1.2.3",
		"CX=4.5.6",
		"GM=0.64.0",
		"CC_INT=sha512-cc==",
		"CC_X64_INT=sha512-ccx64==",
		"CX_INT=sha512-cx==",
		"CX_X64_INT=sha512-cxx64==",
		"GM_INT=sha512-gm==",
		"EXISTING_DIGEST=",
		"PUSHED_DIGEST="+digest,
		"RUNNER_TEMP="+dir,
		"GITHUB_STEP_SUMMARY="+summary,
		"GITHUB_OUTPUT="+output,
	)
	if combined, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("step failed: %v\n%s", err, combined)
	}
	got, err := os.ReadFile(filepath.Join(dir, "worker-image-digest.txt"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(got)), "\n")
	want := []string{
		"WORKER-IMAGE-DIGEST " + repo + ":" + tag + "@" + digest + " published=true",
		"claude-code=1.2.3 integrity=sha512-cc==",
		"claude-code-linux-x64=1.2.3 integrity=sha512-ccx64==",
		"codex=4.5.6 integrity=sha512-cx==",
		"codex-linux-x64=4.5.6 integrity=sha512-cxx64==",
		"gemini-cli=0.64.0 integrity=sha512-gm==",
	}
	if len(lines) != len(want) {
		t.Fatalf("worker-image-digest.txt =\n%s\nwant %d lines", got, len(want))
	}
	for i, w := range want {
		if lines[i] != w {
			t.Errorf("worker-image-digest.txt line %d = %q, want %q", i+1, lines[i], w)
		}
	}
}

// The job fails before the build if any DEVCACHE_* variable is set, and both
// make calls pass the dev-cache args empty.
func TestWorkflowBuildPublish_DevcacheArgsMustBeEmpty(t *testing.T) {
	_, wf := loadWorkflow(t)
	job := wf.Jobs["build-publish"]
	check, build, push := -1, -1, -1
	for i, step := range job.Steps {
		if step.ID == "devcache-empty" {
			check = i
		}
		if build < 0 && strings.Contains(step.Run, "make build-worker-image") {
			build = i
		}
		if push < 0 && strings.Contains(step.Run, "make push-worker-image") {
			push = i
		}
	}
	if check < 0 || build < 0 || push < 0 {
		t.Fatalf("build-publish steps: devcache-empty #%d, build #%d, push #%d; want all three", check, build, push)
	}
	if check >= build {
		t.Errorf("step devcache-empty (#%d) does not come before the build step (#%d)", check, build)
	}
	if job.Steps[check].If != job.Steps[build].If {
		t.Errorf("step devcache-empty if = %q, want the build step's %q", job.Steps[check].If, job.Steps[build].If)
	}
	for _, i := range []int{build, push} {
		if !strings.Contains(job.Steps[i].Run, "DEVCACHE_GOPROXY= DEVCACHE_NPM_REGISTRY=") {
			t.Errorf("step %q run: does not pass DEVCACHE_GOPROXY= DEVCACHE_NPM_REGISTRY=", job.Steps[i].Name)
		}
	}

	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash is not on PATH")
	}
	script := job.Steps[check].Run
	var base []string
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "DEVCACHE_") {
			base = append(base, kv)
		}
	}
	cases := []struct {
		name   string
		extra  []string
		wantOK bool
	}{
		{name: "none set", wantOK: true},
		{name: "goproxy set", extra: []string{"DEVCACHE_GOPROXY=http://cache.invalid:3000"}},
		{name: "npm registry set", extra: []string{"DEVCACHE_NPM_REGISTRY=http://cache.invalid:4873"}},
		{name: "goproxy empty", extra: []string{"DEVCACHE_GOPROXY="}, wantOK: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cmd := exec.Command(bash, "-c", script)
			cmd.Env = append(append([]string{}, base...), tc.extra...)
			combined, runErr := cmd.CombinedOutput()
			if tc.wantOK && runErr != nil {
				t.Fatalf("step failed: %v\n%s", runErr, combined)
			}
			if !tc.wantOK && runErr == nil {
				t.Fatalf("step exited 0 with %v set:\n%s", tc.extra, combined)
			}
		})
	}
}

const fakeAnonDockerScript = `#!/bin/sh
if [ -e "$DOCKER_CONFIG/config.json" ]; then exit 9; fi
printf '%s' "$FAKE_DOCKER_STDOUT"
printf '%s' "$FAKE_DOCKER_STDERR" >&2
exit "$FAKE_DOCKER_EXIT"
`

// After the digest is recorded, an anonymous lookup of it must be refused:
// a public package fails the job, and so does an answer that is not a refusal.
func TestWorkflowBuildPublish_PackageMustBePrivate(t *testing.T) {
	_, wf := loadWorkflow(t)
	job := wf.Jobs["build-publish"]
	priv, record, upload := -1, -1, -1
	for i, step := range job.Steps {
		switch {
		case step.ID == "private":
			priv = i
		case step.ID == "record":
			record = i
		case step.Name == "Upload the digest":
			upload = i
		}
	}
	if priv < 0 || record < 0 || upload < 0 {
		t.Fatalf("build-publish steps: private #%d, record #%d, upload #%d; want all three", priv, record, upload)
	}
	if priv <= record || priv >= upload {
		t.Errorf("step private (#%d) is not between record (#%d) and the upload (#%d)", priv, record, upload)
	}

	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash is not on PATH")
	}
	script := stepRun(t, "private")
	cases := []struct {
		name   string
		stderr string
		exit   string
		wantOK bool
	}{
		{name: "public", exit: "0"},
		{name: "denied", stderr: "denied: denied\n", exit: "1", wantOK: true},
		{name: "unauthorized", stderr: "unauthorized: authentication required\n", exit: "1", wantOK: true},
		{name: "server error", stderr: "503 Service Unavailable\n", exit: "1"},
		{name: "server error naming a digest", stderr: "failed to fetch ghcr.io/example/img@sha256:ab401cd403ef0123456789abcdef0123456789abcdef0123456789abcdef0123: 503 Service Unavailable\n", exit: "1"},
		{name: "403", stderr: "unexpected status: 403 Forbidden\n", exit: "1", wantOK: true},
		{name: "401", stderr: "unexpected status: 401 Unauthorized\n", exit: "1", wantOK: true},
		{name: "empty stderr", exit: "1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			binDir := filepath.Join(dir, "bin")
			if err := os.Mkdir(binDir, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(binDir, "docker"), []byte(fakeAnonDockerScript), 0o755); err != nil {
				t.Fatal(err)
			}
			cfg := filepath.Join(dir, "docker-config")
			if err := os.Mkdir(cfg, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(cfg, "config.json"), []byte(`{"auths":{}}`), 0o600); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command(bash, "-c", script)
			cmd.Env = append(os.Environ(),
				"PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"),
				"DOCKER_CONFIG="+cfg,
				"REPO=ghcr.io/example/img",
				"DIGEST=sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
				"FAKE_DOCKER_STDERR="+tc.stderr,
				"FAKE_DOCKER_EXIT="+tc.exit,
			)
			combined, runErr := cmd.CombinedOutput()
			if tc.wantOK && runErr != nil {
				t.Fatalf("step failed: %v\n%s", runErr, combined)
			}
			if !tc.wantOK && runErr == nil {
				t.Fatalf("step exited 0:\n%s", combined)
			}
		})
	}
}

// fakeAnonCurlScript stands in for curl in the private-before step. It
// refuses credentials and a bearer token on argv, logs each URL to
// $FAKE_CURL_LOG, writes the case's body to the -o file and prints the
// case's -w code. The tags lookup must send "Authorization: Bearer anon"
// from a header file.
const fakeAnonCurlScript = `#!/bin/sh
for a in "$@"; do
  case "$a" in -u|--user|*Bearer*|*anon*) exit 9 ;; esac
done
out=""; url=""; hdr=""
while [ $# -gt 0 ]; do
  case "$1" in
    -o|-w|-H) [ "$1" = -o ] && out="$2"; [ "$1" = -H ] && hdr="$2"; shift 2 ;;
    -*) shift ;;
    *) url="$1"; shift ;;
  esac
done
printf '%s\n' "$url" >> "$FAKE_CURL_LOG"
case "$url" in
  "https://ghcr.io/token?service=ghcr.io&scope=repository:example/img:pull")
    printf '%s' "$FAKE_TOKEN_BODY" > "$out"
    printf '%s' "$FAKE_TOKEN_CODE" ;;
  "https://ghcr.io/v2/example/img/tags/list")
    case "$hdr" in @*) ;; *) exit 8 ;; esac
    [ "$(cat "${hdr#@}")" = "Authorization: Bearer anon" ] || exit 8
    : > "$out"
    printf '%s' "$FAKE_TAGS_CODE" ;;
  *) exit 7 ;;
esac
`

func TestWorkflowBuildPublish_PrivateBeforePush(t *testing.T) {
	_, wf := loadWorkflow(t)
	job := wf.Jobs["build-publish"]
	before, layer, push := -1, -1, -1
	for i, step := range job.Steps {
		switch {
		case step.ID == "private-before":
			before = i
		case step.ID == "layer-scan":
			layer = i
		case push < 0 && strings.Contains(step.Run, "make push-worker-image"):
			push = i
		}
	}
	if before < 0 || layer < 0 || push < 0 {
		t.Fatalf("build-publish steps: private-before #%d, layer-scan #%d, push #%d; want all three", before, layer, push)
	}
	if before <= layer || before >= push {
		t.Errorf("step private-before (#%d) is not between layer-scan (#%d) and the push (#%d)", before, layer, push)
	}
	if got, want := job.Steps[before].If, job.Steps[push].If; got != want {
		t.Errorf("step private-before if = %q, want the push step's %q", got, want)
	}
	for _, s := range []string{"GITHUB_TOKEN", "secrets.", " -u "} {
		if strings.Contains(job.Steps[before].Run, s) {
			t.Errorf("step private-before run: contains %q", s)
		}
	}

	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash is not on PATH")
	}
	if _, err := exec.LookPath("jq"); err != nil {
		t.Skip("jq is not on PATH")
	}
	script := stepRun(t, "private-before")
	const tagsURL = "https://ghcr.io/v2/example/img/tags/list"
	cases := []struct {
		name      string
		tokenCode string
		tokenBody string
		tagsCode  string
		wantOK    bool
		want      string
		noTags    bool
	}{
		{name: "public", tokenCode: "200", tagsCode: "200", want: "must be private"},
		{name: "tags 401", tokenCode: "200", tagsCode: "401", wantOK: true},
		{name: "tags 403", tokenCode: "200", tagsCode: "403", wantOK: true},
		{name: "tags 404", tokenCode: "200", tagsCode: "404", wantOK: true},
		{name: "tags 500", tokenCode: "200", tagsCode: "500"},
		{name: "token 403", tokenCode: "403", wantOK: true, noTags: true},
		{name: "token 401", tokenCode: "401", wantOK: true, noTags: true},
		{name: "token 502", tokenCode: "502", noTags: true},
		{name: "token body without a token", tokenCode: "200", tokenBody: "{}", noTags: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			binDir := filepath.Join(dir, "bin")
			if err := os.Mkdir(binDir, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(binDir, "curl"), []byte(fakeAnonCurlScript), 0o755); err != nil {
				t.Fatal(err)
			}
			body := tc.tokenBody
			if body == "" {
				body = `{"token":"anon"}`
			}
			curlLog := filepath.Join(dir, "curl.log")
			cmd := exec.Command(bash, "-c", script)
			cmd.Env = append(os.Environ(),
				"PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"),
				"REPO=ghcr.io/example/img",
				"FAKE_CURL_LOG="+curlLog,
				"FAKE_TOKEN_CODE="+tc.tokenCode,
				"FAKE_TOKEN_BODY="+body,
				"FAKE_TAGS_CODE="+tc.tagsCode,
			)
			combined, runErr := cmd.CombinedOutput()
			if tc.wantOK && runErr != nil {
				t.Fatalf("step failed: %v\n%s", runErr, combined)
			}
			if !tc.wantOK && runErr == nil {
				t.Fatalf("step exited 0:\n%s", combined)
			}
			if tc.want != "" && !strings.Contains(string(combined), tc.want) {
				t.Errorf("output lacks %q:\n%s", tc.want, combined)
			}
			if strings.Contains(string(combined), "Bearer anon") {
				t.Errorf("output carries the bearer token:\n%s", combined)
			}
			calls, err := os.ReadFile(curlLog)
			if err != nil {
				t.Fatalf("curl was never called: %v\n%s", err, combined)
			}
			if called := strings.Contains(string(calls), tagsURL); called == tc.noTags {
				t.Errorf("tags URL called = %v, want %v; curl calls:\n%s", called, !tc.noTags, calls)
			}
		})
	}
}
