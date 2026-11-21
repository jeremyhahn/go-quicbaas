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
	"fmt"
	"os"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// workerImageWorkflowPath is the publish workflow, relative to this package.
const workerImageWorkflowPath = "../../../.github/workflows/worker-image.yml"

// secretCheckLine is the fail-closed check the layer scan must run on
// trivy's JSON report.
const secretCheckLine = `bash deployments/agents/software-factory-worker/check-secret-findings.sh "$work/secrets.json" "$work/layers"`

type secretScanWorkflow struct {
	Jobs map[string]secretScanJob `yaml:"jobs"`
}

type secretScanJob struct {
	Steps []secretScanStep `yaml:"steps"`
}

type secretScanStep struct {
	ID              string `yaml:"id"`
	Name            string `yaml:"name"`
	If              string `yaml:"if"`
	Run             string `yaml:"run"`
	ContinueOnError any    `yaml:"continue-on-error"`
}

// secretScanProblems checks that job build-publish of the worker-image
// workflow scans the image for secrets in steps that fail on a finding, and
// that both scans run before the push. It returns one line per problem.
func secretScanProblems(raw []byte) []string {
	var wf secretScanWorkflow
	if err := yaml.Unmarshal(raw, &wf); err != nil {
		return []string{fmt.Sprintf("workflow does not parse: %v", err)}
	}
	job, ok := wf.Jobs["build-publish"]
	if !ok {
		return []string{"no job build-publish"}
	}

	var problems []string
	add := func(format string, args ...any) {
		problems = append(problems, fmt.Sprintf(format, args...))
	}

	pushIdx, imageIdx, layerIdx := -1, -1, -1
	for i, s := range job.Steps {
		if s.ContinueOnError != nil && fmt.Sprint(s.ContinueOnError) != "false" {
			add("step %q has continue-on-error: %v", stepLabel(s), s.ContinueOnError)
		}
		switch {
		case s.ID == "secret-scan":
			imageIdx = i
		case s.ID == "layer-scan":
			layerIdx = i
		}
		if pushIdx < 0 && strings.Contains(s.Run, "make push-worker-image") {
			pushIdx = i
		}
	}
	if pushIdx < 0 {
		add("no step runs make push-worker-image")
	}
	if imageIdx < 0 {
		add("no step with id secret-scan")
	}
	if layerIdx < 0 {
		add("no step with id layer-scan")
	}
	if pushIdx < 0 || imageIdx < 0 || layerIdx < 0 {
		return problems
	}
	push, image, layer := job.Steps[pushIdx], job.Steps[imageIdx], job.Steps[layerIdx]

	for _, sc := range []struct {
		id  string
		idx int
		s   secretScanStep
	}{{"secret-scan", imageIdx, image}, {"layer-scan", layerIdx, layer}} {
		if sc.idx > pushIdx {
			add("step %s runs after the push step", sc.id)
		}
		if sc.s.If != push.If {
			add("step %s has if %q, the push step has %q", sc.id, sc.s.If, push.If)
		}
		for _, flag := range []string{"--severity", "--skip-dirs", "--skip-files", "--secret-config", "--ignorefile"} {
			if strings.Contains(sc.s.Run, flag) {
				add("step %s narrows the scan with %s", sc.id, flag)
			}
		}
	}

	for _, want := range []string{"--scanners secret", "--image-config-scanners secret", "--exit-code 1"} {
		if !strings.Contains(image.Run, want) {
			add("step secret-scan does not run trivy image with %s", want)
		}
	}

	run := layer.Run
	if !strings.HasPrefix(strings.TrimSpace(run), "set -euo pipefail") {
		add("step layer-scan does not start with set -euo pipefail")
	}
	for _, want := range []string{"docker save", ".Layers[]", "config.json"} {
		if !strings.Contains(run, want) {
			add("step layer-scan has no %s", want)
		}
	}
	if strings.Contains(run, "set +e") {
		add("step layer-scan turns off errexit with set +e")
	}

	lines := strings.Split(run, "\n")
	trivyIdx, checkIdx := -1, -1
	for i, l := range lines {
		l = strings.TrimSpace(l)
		if trivyIdx < 0 && strings.Contains(l, "trivy fs --scanners secret") {
			trivyIdx = i
			for _, want := range []string{"--format json", `--output "$work/secrets.json"`} {
				if !strings.Contains(l, want) {
					add("step layer-scan's trivy fs line has no %s", want)
				}
			}
		}
		if strings.Contains(l, secretCheckLine) {
			if trivyIdx >= 0 && i > trivyIdx && checkIdx < 0 {
				checkIdx = i
			}
			if strings.HasSuffix(l, "|| true") {
				add("step layer-scan ignores the check's exit with || true")
			}
		}
	}
	if trivyIdx < 0 {
		add("step layer-scan does not run trivy fs --scanners secret")
	} else if checkIdx < 0 {
		add("step layer-scan does not run check-secret-findings.sh on the report after trivy fs")
	}
	return problems
}

func stepLabel(s secretScanStep) string {
	if s.ID != "" {
		return s.ID
	}
	return s.Name
}

func readWorkerImageWorkflow(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile(workerImageWorkflowPath)
	if err != nil {
		t.Fatalf("read %s: %v", workerImageWorkflowPath, err)
	}
	return string(raw)
}

func TestWorkerImageWorkflow_SecretScanFailsClosed(t *testing.T) {
	if problems := secretScanProblems([]byte(readWorkerImageWorkflow(t))); len(problems) > 0 {
		t.Fatalf("worker-image.yml secret scan problems:\n%s", strings.Join(problems, "\n"))
	}
}

func TestSecretScanProblems_Mutations(t *testing.T) {
	orig := readWorkerImageWorkflow(t)
	const indent = "          "
	const layerHead = "        id: layer-scan\n        if: ${{ steps.existing.outputs.exists != 'true' }}\n"

	cases := []struct {
		name, old, new, want string
	}{
		{
			name: "check line dropped",
			old:  indent + secretCheckLine + "\n",
			new:  "",
			want: "does not run check-secret-findings.sh",
		},
		{
			name: "check exit ignored",
			old:  secretCheckLine,
			new:  secretCheckLine + " || true",
			want: "|| true",
		},
		{
			name: "image scan exits zero",
			old:  "--image-config-scanners secret --exit-code 1",
			new:  "--image-config-scanners secret --exit-code 0",
			want: "secret-scan does not run trivy image with --exit-code 1",
		},
		{
			name: "layer scan skips a directory",
			old:  `--no-progress "$work/layers"`,
			new:  `--no-progress --skip-dirs /x "$work/layers"`,
			want: "layer-scan narrows the scan with --skip-dirs",
		},
		{
			name: "layer scan renamed away",
			old:  "id: layer-scan\n",
			new:  "id: layer-scan-after-push\n",
			want: "no step with id layer-scan",
		},
		{
			name: "layer scan may fail",
			old:  layerHead,
			new:  layerHead + "        continue-on-error: true\n",
			want: `step "layer-scan" has continue-on-error`,
		},
		{
			name: "layer scan if changed",
			old:  layerHead,
			new:  "        id: layer-scan\n        if: ${{ always() }}\n",
			want: "step layer-scan has if",
		},
		{
			name: "set +e in layer scan",
			old:  indent + secretCheckLine + "\n",
			new:  indent + "set +e\n" + indent + secretCheckLine + "\n",
			want: "set +e",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mutated := strings.Replace(orig, tc.old, tc.new, 1)
			if mutated == orig {
				t.Fatalf("replacement %q found nothing in worker-image.yml", tc.old)
			}
			problems := secretScanProblems([]byte(mutated))
			for _, p := range problems {
				if strings.Contains(p, tc.want) {
					return
				}
			}
			t.Fatalf("want a problem containing %q, got %q", tc.want, problems)
		})
	}
}
