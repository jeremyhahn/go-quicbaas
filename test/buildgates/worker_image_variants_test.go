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
	"archive/tar"
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// variantsWorkflowPath is the workflow that builds, scans and publishes the
// worker image variants (#2700).
var variantsWorkflowPath = filepath.Join(workflowsDir, "worker-image-variants.yml")

// variantSteps are the variant job's steps the publish-path test runs, in
// job order. A renamed or removed step fails the test, so no gate can drop
// out of the path unnoticed.
var variantSteps = []string{
	"Is this variant tag already published?",
	"Build the variant",
	"Scan the image for secrets",
	"Scan every layer tarball for secrets",
	"Scan the image for vulnerabilities",
	"Push and check the digest resolves",
	"Move current to this variant",
}

// variantNotPublishedIf is the `if:` of every step that builds, scans or
// pushes: they run only when the existence check found no published tag.
const variantNotPublishedIf = "${{ steps.existing.outputs.exists != 'true' }}"

const (
	variantRepo           = "ghcr.io/owner/quicbaas-software-factory-worker-go-xkms"
	variantTag            = "base-0123456789ab"
	variantPkgPath        = "/users/owner/packages/container/quicbaas-software-factory-worker-go-xkms"
	variantPushedDigest   = "sha256:1111111111111111111111111111111111111111111111111111111111111111"
	variantExistingDigest = "sha256:2222222222222222222222222222222222222222222222222222222222222222"
	variantBaseDigest     = "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
)

// fakeVariantDocker stands in for docker: the variant tag's lookup answers
// per FAKE_TAG until a push succeeded, then names the pushed digest (or a
// bad one under FAKE_RESOLVE=bad); `current` names what imagetools create
// last set; save copies the image tarball the test prepared.
const fakeVariantDocker = `#!/usr/bin/env bash
set -eu
echo "docker $*" >> "$FAKE_LOG"
st="$FAKE_STATE"
case "$1" in
  build) exit 0 ;;
  push)
    if [ "${FAKE_PUSH:-}" = fail ]; then echo "push failed" >&2; exit 1; fi
    touch "$st/pushed"; exit 0 ;;
  save)
    out=""
    while [ $# -gt 0 ]; do if [ "$1" = -o ]; then out="$2"; fi; shift; done
    cp "$FAKE_IMAGE_TAR" "$out"; exit 0 ;;
  buildx) ;;
  *) echo "fake docker: unexpected $*" >&2; exit 2 ;;
esac
[ "$2" = imagetools ] || { echo "fake docker: unexpected $*" >&2; exit 2; }
case "$3" in
  create)
    for a in "$@"; do case "$a" in *@sha256:*) printf '%s' "${a#*@}" > "$st/current" ;; esac; done
    exit 0 ;;
  inspect) ref="$4" ;;
  *) echo "fake docker: unexpected $*" >&2; exit 2 ;;
esac
case "$ref" in
  *:current)
    [ -f "$st/current" ] || { echo "manifest unknown" >&2; exit 1; }
    printf '{"digest":"%s"}\n' "$(cat "$st/current")" ;;
  *)
    if [ -f "$st/pushed" ]; then
      if [ "${FAKE_RESOLVE:-}" = bad ]; then echo '{"digest":"null"}'; else printf '{"digest":"%s"}\n' "$FAKE_PUSHED_DIGEST"; fi
    else
      case "$FAKE_TAG" in
        present) printf '{"digest":"%s"}\n' "$FAKE_EXISTING_DIGEST" ;;
        absent) echo "ERROR: manifest unknown" >&2; exit 1 ;;
        denied) echo "denied: denied" >&2; exit 1 ;;
        *) echo "connection reset" >&2; exit 1 ;;
      esac
    fi ;;
esac
`

// fakeVariantTrivy stands in for trivy: FAKE_TRIVY=secret or vuln fails that
// image scan, and FAKE_TRIVY=layer makes the layer scan report a finding the
// real check-secret-findings.sh does not allow.
const fakeVariantTrivy = `#!/usr/bin/env bash
set -eu
echo "trivy $*" >> "$FAKE_LOG"
case "$1" in
  image)
    case " $* " in
      *" --scanners secret "*) if [ "${FAKE_TRIVY:-}" = secret ]; then echo "secret found" >&2; exit 1; fi ;;
      *" --scanners vuln "*) if [ "${FAKE_TRIVY:-}" = vuln ]; then echo "HIGH vulnerability" >&2; exit 1; fi ;;
      *) echo "fake trivy: unexpected $*" >&2; exit 2 ;;
    esac
    exit 0 ;;
  fs)
    out=""
    while [ $# -gt 0 ]; do if [ "$1" = --output ]; then out="$2"; fi; shift; done
    if [ "${FAKE_TRIVY:-}" = layer ]; then
      printf '%s\n' '{"SchemaVersion":2,"Results":[{"Target":"1/leak.txt","Secrets":[{"RuleID":"aws-access-key-id"}]}]}' > "$out"
    else
      printf '%s\n' '{"SchemaVersion":2,"Results":[]}' > "$out"
    fi
    exit 0 ;;
esac
echo "fake trivy: unexpected $*" >&2; exit 2
`

// fakeVariantGh stands in for gh api: FAKE_GH is the packages API's answer.
const fakeVariantGh = `#!/usr/bin/env bash
echo "gh $*" >> "$FAKE_LOG"
case "${FAKE_GH:-}" in
  404) echo "gh: Not Found (HTTP 404)" >&2; exit 1 ;;
  200) exit 0 ;;
  *) echo "gh: Forbidden (HTTP 403)" >&2; exit 1 ;;
esac
`

type variantStepDef struct {
	name, ifExpr, run string
	env               map[string]string
}

// variantStep returns the variant job's step named name: its env, its `if:`
// and its run body.
func variantStep(t *testing.T, name string) (env map[string]string, ifExpr, run string) {
	t.Helper()
	steps := mapValue(ciJobNode([]byte(readFile(t, variantsWorkflowPath)), "variant"), "steps")
	if steps == nil {
		t.Fatalf("%s has no variant job with steps", variantsWorkflowPath)
	}
	for _, s := range steps.Content {
		if n := mapValue(s, "name"); n == nil || n.Value != name {
			continue
		}
		env = map[string]string{}
		if e := mapValue(s, "env"); e != nil {
			for i := 0; i+1 < len(e.Content); i += 2 {
				env[e.Content[i].Value] = e.Content[i+1].Value
			}
		}
		if i := mapValue(s, "if"); i != nil {
			ifExpr = i.Value
		}
		r := mapValue(s, "run")
		if r == nil || r.Value == "" {
			t.Fatalf("step %q has no run body", name)
		}
		return env, ifExpr, r.Value
	}
	t.Fatalf("%s's variant job has no step %q", variantsWorkflowPath, name)
	return nil, "", ""
}

// variantImageTar builds the tarball the fake `docker save` writes: a
// manifest, a config and one layer holding a harmless file.
func variantImageTar(t *testing.T) []byte {
	t.Helper()
	pack := func(files map[string][]byte, order []string) []byte {
		var buf bytes.Buffer
		w := tar.NewWriter(&buf)
		for _, name := range order {
			body := files[name]
			if err := w.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
				t.Fatal(err)
			}
			if _, err := w.Write(body); err != nil {
				t.Fatal(err)
			}
		}
		if err := w.Close(); err != nil {
			t.Fatal(err)
		}
		return buf.Bytes()
	}
	layer := pack(map[string][]byte{"hello.txt": []byte("hello\n")}, []string{"hello.txt"})
	return pack(map[string][]byte{
		"manifest.json": []byte(`[{"Config":"c.json","Layers":["l1.tar"]}]`),
		"c.json":        []byte(`{}`),
		"l1.tar":        layer,
	}, []string{"manifest.json", "c.json", "l1.tar"})
}

type variantCase struct {
	name                                  string
	tag, gh, trivy, push, resolve         string
	wantPass, wantBuild, wantPush         bool
	wantFailStep, wantGhPath, wantCurrent string
}

type variantResult struct {
	failedStep string
	log        []string
	output     string
}

// runVariantJob runs the variant job's steps in order with the fakes first
// on PATH, skipping the build, scan and push steps when the tag exists and
// stopping at the first step that fails, as the runner does.
func runVariantJob(t *testing.T, c variantCase) variantResult {
	t.Helper()
	for _, tool := range []string{"bash", "jq", "tar", "grep"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Fatalf("the variant workflow's scripts need %s: %v", tool, err)
		}
	}
	root, err := filepath.Abs(repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	var defs []variantStepDef
	for _, name := range variantSteps {
		env, ifExpr, run := variantStep(t, name)
		defs = append(defs, variantStepDef{name: name, ifExpr: ifExpr, run: run, env: env})
	}
	dir := t.TempDir()
	bin, state, tmp, runner := filepath.Join(dir, "bin"), filepath.Join(dir, "state"), filepath.Join(dir, "tmp"), filepath.Join(dir, "runner")
	for _, d := range []string{bin, state, tmp, runner} {
		if err := os.Mkdir(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	write := func(path string, body []byte, mode os.FileMode) {
		if err := os.WriteFile(path, body, mode); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(bin, "docker"), []byte(fakeVariantDocker), 0o755)
	write(filepath.Join(bin, "trivy"), []byte(fakeVariantTrivy), 0o755)
	write(filepath.Join(bin, "gh"), []byte(fakeVariantGh), 0o755)
	imageTar, logPath, summary := filepath.Join(dir, "image.tar"), filepath.Join(dir, "calls.log"), filepath.Join(dir, "summary.md")
	write(imageTar, variantImageTar(t), 0o644)
	write(logPath, nil, 0o644)
	write(summary, nil, 0o644)

	outputs := map[string]map[string]string{}
	exprs := map[string]func() string{
		"${{ steps.resolve.outputs.repo }}":                                  func() string { return variantRepo },
		"${{ steps.resolve.outputs.tag }}":                                   func() string { return variantTag },
		"${{ steps.resolve.outputs.df }}":                                    func() string { return "deployments/agents/software-factory-worker/variants/go-xkms.Dockerfile" },
		"${{ steps.resolve.outputs.base }}":                                  func() string { return "ghcr.io/owner/quicbaas-software-factory-worker@" + variantBaseDigest },
		"${{ steps.resolve.outputs.repo }}:${{ steps.resolve.outputs.tag }}": func() string { return variantRepo + ":" + variantTag },
		"${{ secrets.GITHUB_TOKEN }}":                                        func() string { return "fake-token" },
		"${{ github.event.repository.owner.type }}":                          func() string { return "User" },
		"${{ steps.push.outputs.digest || steps.existing.outputs.digest }}": func() string {
			if d := outputs["push"]["digest"]; d != "" {
				return d
			}
			return outputs["existing"]["digest"]
		},
	}
	ids := map[string]string{"Is this variant tag already published?": "existing", "Push and check the digest resolves": "push"}

	res := variantResult{}
	var out strings.Builder
	for i, d := range defs {
		switch d.ifExpr {
		case "":
		case variantNotPublishedIf:
			if outputs["existing"]["exists"] == "true" {
				continue
			}
		default:
			t.Fatalf("step %q has `if: %s`, which the test does not model", d.name, d.ifExpr)
		}
		script, ghOut := filepath.Join(dir, "step.sh"), filepath.Join(dir, "output-"+string(rune('a'+i)))
		write(script, []byte(d.run), 0o644)
		write(ghOut, nil, 0o644)
		env := []string{
			"PATH=" + bin + string(os.PathListSeparator) + os.Getenv("PATH"),
			"HOME=" + dir,
			"TMPDIR=" + tmp,
			"RUNNER_TEMP=" + runner,
			"GITHUB_OUTPUT=" + ghOut,
			"GITHUB_STEP_SUMMARY=" + summary,
			"GITHUB_REPOSITORY_OWNER=owner",
			"FAKE_LOG=" + logPath,
			"FAKE_STATE=" + state,
			"FAKE_IMAGE_TAR=" + imageTar,
			"FAKE_PUSHED_DIGEST=" + variantPushedDigest,
			"FAKE_EXISTING_DIGEST=" + variantExistingDigest,
			"FAKE_TAG=" + c.tag,
			"FAKE_GH=" + c.gh,
			"FAKE_TRIVY=" + c.trivy,
			"FAKE_PUSH=" + c.push,
			"FAKE_RESOLVE=" + c.resolve,
		}
		for k, v := range d.env {
			f, ok := exprs[v]
			if !ok {
				t.Fatalf("step %q sets %s from %q, which the test does not model", d.name, k, v)
			}
			env = append(env, k+"="+f())
		}
		cmd := exec.Command("bash", "-e", script)
		cmd.Dir = root
		cmd.Env = env
		b, err := cmd.CombinedOutput()
		out.WriteString("== " + d.name + "\n" + string(b))
		o := map[string]string{}
		if raw, rerr := os.ReadFile(ghOut); rerr == nil {
			for _, line := range strings.Split(string(raw), "\n") {
				if k, v, ok := strings.Cut(line, "="); ok {
					o[k] = v
				}
			}
		}
		if id := ids[d.name]; id != "" {
			outputs[id] = o
		}
		if err != nil {
			res.failedStep = d.name
			break
		}
	}
	res.output = out.String()
	raw, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		if line != "" {
			res.log = append(res.log, line)
		}
	}
	return res
}

// TestWorkerImageVariantsPublishPath runs the variant job's own scripts with
// fake docker, trivy and gh, and proves that only a confirmed absent tag (a
// not-found answer, or a denial the packages API confirms with a 404) is
// built, that every failed scan, push or digest check stops the job before a
// push or before `current` moves, and that an existing tag is never pushed
// again.
func TestWorkerImageVariantsPublishPath(t *testing.T) {
	cases := []variantCase{
		{name: "new tag", tag: "absent", wantPass: true, wantBuild: true, wantPush: true, wantCurrent: variantPushedDigest},
		{name: "existing tag", tag: "present", wantPass: true, wantCurrent: variantExistingDigest},
		{name: "denied, package 404", tag: "denied", gh: "404", wantPass: true, wantBuild: true, wantPush: true, wantGhPath: variantPkgPath, wantCurrent: variantPushedDigest},
		{name: "denied, package exists", tag: "denied", gh: "200", wantFailStep: variantSteps[0], wantGhPath: variantPkgPath},
		{name: "denied, packages API 403", tag: "denied", gh: "403", wantFailStep: variantSteps[0], wantGhPath: variantPkgPath},
		{name: "other lookup error", tag: "other", wantFailStep: variantSteps[0]},
		{name: "image secret scan fails", tag: "absent", trivy: "secret", wantBuild: true, wantFailStep: variantSteps[2]},
		{name: "layer secret scan fails", tag: "absent", trivy: "layer", wantBuild: true, wantFailStep: variantSteps[3]},
		{name: "vulnerability scan fails", tag: "absent", trivy: "vuln", wantBuild: true, wantFailStep: variantSteps[4]},
		{name: "push fails", tag: "absent", push: "fail", wantBuild: true, wantPush: true, wantFailStep: variantSteps[5]},
		{name: "pushed digest does not resolve", tag: "absent", resolve: "bad", wantBuild: true, wantPush: true, wantFailStep: variantSteps[5]},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := runVariantJob(t, c)
			if c.wantPass != (r.failedStep == "") || (!c.wantPass && r.failedStep != c.wantFailStep) {
				t.Fatalf("job failed at step %q, want failure at %q (pass=%v)\n%s", r.failedStep, c.wantFailStep, c.wantPass, r.output)
			}
			var builds, pushes, creates, ghCalls []string
			for _, line := range r.log {
				switch {
				case strings.HasPrefix(line, "docker build "):
					builds = append(builds, line)
				case strings.HasPrefix(line, "docker push "):
					pushes = append(pushes, line)
				case strings.HasPrefix(line, "docker buildx imagetools create "):
					creates = append(creates, line)
				case strings.HasPrefix(line, "gh "):
					ghCalls = append(ghCalls, line)
				}
			}
			if (len(builds) > 0) != c.wantBuild {
				t.Errorf("builds %q, want build=%v\n%s", builds, c.wantBuild, r.output)
			}
			if c.wantPush {
				if len(pushes) != 1 || pushes[0] != "docker push "+variantRepo+":"+variantTag {
					t.Errorf("pushes %q, want one push of %s:%s", pushes, variantRepo, variantTag)
				}
			} else if len(pushes) != 0 {
				t.Errorf("pushes %q, want none\n%s", pushes, r.output)
			}
			if c.wantCurrent != "" {
				want := "docker buildx imagetools create --prefer-index=false --tag " + variantRepo + ":current " + variantRepo + "@" + c.wantCurrent
				if len(creates) != 1 || creates[0] != want {
					t.Errorf("current moves %q, want one: %s\n%s", creates, want, r.output)
				}
			} else if len(creates) != 0 {
				t.Errorf("current moved %q, want no move\n%s", creates, r.output)
			}
			if c.wantGhPath != "" {
				want := "gh api " + c.wantGhPath + " --silent"
				if len(ghCalls) != 1 || ghCalls[0] != want {
					t.Errorf("gh calls %q, want one: %s", ghCalls, want)
				}
			} else if len(ghCalls) != 0 {
				t.Errorf("gh calls %q, want none", ghCalls)
			}
		})
	}
}
