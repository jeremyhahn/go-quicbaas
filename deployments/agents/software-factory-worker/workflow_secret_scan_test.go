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
// trivy's JSON report, as its own unconditional line at the top level of
// the run script: exactly this text at column 0, not continued from the
// previous line.
const secretCheckLine = `bash deployments/agents/software-factory-worker/check-secret-findings.sh "$work/secrets.json" "$work/layers"`

// layerListLine lists the layer tarballs of the saved image from its
// manifest.json, for layerExtractLoop to extract.
const layerListLine = `layers="$(jq -r '.[0].Layers[]' "$work/image/manifest.json")"`

// layerExtractLine extracts one layer tarball into its own directory under
// the scanned root "$work/layers".
const layerExtractLine = `tar -xf "$work/image/$layer" -C "$work/layers/$i" --no-same-owner --no-same-permissions`

// layerExtractLoop is the top-level loop, as trimmed lines in order, that
// extracts every layer listed in manifest.json (layerListLine) into the
// scanned root "$work/layers", with no other command in the loop body.
var layerExtractLoop = []string{
	`while IFS= read -r layer; do`,
	`i=$((i + 1))`,
	`mkdir -p "$work/layers/$i"`,
	layerExtractLine,
	`done <<< "$layers"`,
}

// imageScanLine is the fail-closed image and image-config secret scan the
// secret-scan step must run, held to the same rule as secretCheckLine:
// exactly this text at column 0, at the top level of the run script, not
// continued from the previous line, with errexit on.
const imageScanLine = `trivy image --scanners secret --image-config-scanners secret --exit-code 1 --no-progress "${IMAGE}:${TAG}"`

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
// that both scans run before the push. The push and both scans must have no
// status function (success(), failure(), always(), cancelled()) in their if,
// so GitHub's implicit success() gate stays and a failed scan stops the push.
// It returns one line per problem.
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

	for _, s := range []secretScanStep{push, image, layer} {
		if fn := statusFunction(s.If); fn != "" {
			add("step %s has if %q, which uses the status function %s(): only an if without one keeps the implicit success() gate, so a failed scan would not block the push", stepLabel(s), s.If, fn)
		}
	}

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
	imageProblems, _ := commandLineProblems("secret-scan", image.Run, imageScanLine, "trivy image")
	problems = append(problems, imageProblems...)

	run := layer.Run
	for _, want := range []string{"docker save", ".Layers[]", "config.json"} {
		if !strings.Contains(run, want) {
			add("step layer-scan has no %s", want)
		}
	}
	layerProblems, checkIdx := commandLineProblems("layer-scan", run, secretCheckLine, "check-secret-findings.sh")
	problems = append(problems, layerProblems...)

	lines := strings.Split(run, "\n")
	depths := blockDepths(lines)
	w := -1
	for i, l := range lines {
		if strings.TrimSpace(l) == layerExtractLoop[0] {
			w = i
			break
		}
	}
	extracted := w >= 0 && depths[w] == 0 && w+len(layerExtractLoop) <= len(lines)
	if extracted {
		for k, want := range layerExtractLoop {
			if strings.TrimSpace(lines[w+k]) != want {
				extracted = false
				break
			}
		}
	}
	if extracted {
		listed := false
		for l := 0; l < w; l++ {
			if strings.TrimSpace(lines[l]) == layerListLine && depths[l] == 0 {
				listed = true
				break
			}
		}
		extracted = listed
	}
	if !extracted {
		add("step layer-scan does not extract every layer listed in manifest.json into \"$work/layers\" with %q", layerExtractLine)
	}

	trivyIdx := -1
	for i, l := range lines {
		l = strings.TrimSpace(l)
		if strings.Contains(l, "trivy fs --scanners secret") {
			trivyIdx = i
			for _, want := range []string{"--format json", `--output "$work/secrets.json"`} {
				if !strings.Contains(l, want) {
					add("step layer-scan's trivy fs line has no %s", want)
				}
			}
			if fields := strings.Fields(l); fields[len(fields)-1] != `"$work/layers"` {
				add("step layer-scan's trivy fs does not scan the whole layer root \"$work/layers\": %q", l)
			}
			break
		}
	}
	if extracted && trivyIdx >= 0 && trivyIdx < w+len(layerExtractLoop) {
		add("step layer-scan runs trivy fs before the layers are extracted")
	}
	if trivyIdx < 0 {
		add("step layer-scan does not run trivy fs --scanners secret")
	} else if checkIdx <= trivyIdx {
		add("step layer-scan does not run check-secret-findings.sh on the report after trivy fs")
	}
	return problems
}

// commandLineProblems checks that the run script of step runs want as one
// exact line at column 0, at shell block depth 0 (see blockDepths), not
// continued from the previous line, with errexit on from the first line and
// never turned off. Comments are dropped before the continuation check, and
// comment-only lines are skipped like blank lines, so a trailing ||, && or |
// behind a comment still counts. Every other line that mentions mention is reported: it
// runs the command in a form whose exit may not fail the step. It returns
// the problems and the index of the accepted line in the run's lines, or -1.
func commandLineProblems(step, run, want, mention string) ([]string, int) {
	var problems []string
	add := func(format string, args ...any) {
		problems = append(problems, fmt.Sprintf(format, args...))
	}
	if !strings.HasPrefix(strings.TrimSpace(run), "set -euo pipefail") {
		add("step %s does not start with set -euo pipefail", step)
	}
	for _, off := range []string{"set +e", "set +o errexit"} {
		if strings.Contains(run, off) {
			add("step %s turns off errexit with %s", step, off)
		}
	}

	lines := strings.Split(run, "\n")
	depths := blockDepths(lines)
	for _, d := range depths {
		if d < 0 {
			add("step %s has unbalanced shell blocks", step)
			break
		}
	}
	accepted, seen := -1, false
	for i, l := range lines {
		raw := strings.TrimRight(l, " \t\r")
		if raw == want && !seen {
			seen = true
			prev := ""
			for j := i - 1; j >= 0; j-- {
				s := strings.TrimRight(stripShellComment(lines[j]), " \t\r")
				if strings.TrimSpace(s) != "" {
					prev = s
					break
				}
			}
			continued := false
			for _, op := range []string{`\`, "&&", "||", "|"} {
				if strings.HasSuffix(prev, op) {
					continued = true
					break
				}
			}
			switch {
			case depths[i] != 0:
				add("step %s runs %s inside a shell block (depth %d)", step, mention, depths[i])
			case continued:
				add("step %s runs %s as a continuation of the previous line: %q", step, mention, prev)
			default:
				accepted = i
			}
			continue
		}
		if strings.Contains(l, mention) {
			add("step %s runs %s in a form whose exit may not fail the step: %q", step, mention, strings.TrimSpace(l))
		}
	}
	if !seen {
		add("step %s does not run %s as a top-level command", step, mention)
	}
	return problems, accepted
}

// stripShellComment returns line without its shell comment: the text from a
// # outside quotes that starts a word (at index 0 or after a space, tab, ;,
// &, |, (, ), < or >). So ${#x} and a#b are not comments.
func stripShellComment(line string) string {
	var quote byte
	for j := 0; j < len(line); j++ {
		c := line[j]
		switch {
		case quote == '\'':
			if c == '\'' {
				quote = 0
			}
		case c == '\\':
			j++
		case quote == '"':
			if c == '"' {
				quote = 0
			}
		case c == '\'' || c == '"':
			quote = c
		case c == '#' && (j == 0 || strings.IndexByte(" \t;&|()<>", line[j-1]) >= 0):
			return line[:j]
		}
	}
	return line
}

// statusFunction returns the first GitHub Actions status function (success,
// failure, always or cancelled) that cond calls, or "". A step if without
// one is implicitly prefixed with success() &&.
func statusFunction(cond string) string {
	c := strings.NewReplacer(" ", "", "\t", "").Replace(strings.ToLower(cond))
	for _, fn := range []string{"success(", "failure(", "always(", "cancelled("} {
		if strings.Contains(c, fn) {
			return strings.TrimSuffix(fn, "(")
		}
	}
	return ""
}

// blockDepths returns the shell compound-command depth before each line:
// how many { }, ( ), if/fi, while/until/for/select ... done and case/esac
// blocks are open when the line starts. Only whole words outside quotes and
// comments count, so $( ), ${ }, $(( )) and case patterns like a) do not. A
// line that starts inside an open quote, command substitution or
// here-document is data or runs without errexit, not a top-level command,
// and gets one extra level. A negative depth means the closers
// outnumber the openers.
func blockDepths(lines []string) []int {
	depths := make([]int, len(lines))
	depth := 0
	var quote byte       // the open quote character carried across lines, or 0
	heredoc := ""        // the delimiter of the open here-document
	var pending []string // here-documents that start on the next line
	cmdsub := 0          // the paren depth of the open $( ), carried across lines
	for i, line := range lines {
		depths[i] = depth
		if heredoc != "" {
			depths[i]++
			if strings.TrimLeft(strings.TrimRight(line, " \t\r"), "\t") == heredoc {
				heredoc = ""
				if len(pending) > 0 {
					heredoc, pending = pending[0], pending[1:]
				}
			}
			continue
		}
		if quote != 0 || cmdsub > 0 {
			depths[i]++
		}
		var words []string
		var word strings.Builder
		inWord := false
		flush := func() {
			if inWord {
				words = append(words, word.String())
			}
			word.Reset()
			inWord = false
		}
	scan:
		for j := 0; j < len(line); j++ {
			c := line[j]
			switch {
			case quote == '\'':
				word.WriteByte(c)
				if c == '\'' {
					quote = 0
				}
			case quote == '"':
				word.WriteByte(c)
				if c == '\\' && j+1 < len(line) {
					j++
					word.WriteByte(line[j])
				} else if c == '"' {
					quote = 0
				}
			case cmdsub > 0:
				word.WriteByte(c)
				switch c {
				case '(':
					cmdsub++
				case ')':
					cmdsub--
				case '\'', '"':
					quote = c
				}
			case c == ' ' || c == '\t' || c == '\r' || c == ';':
				flush()
			case c == '#' && !inWord:
				break scan
			default:
				inWord = true
				word.WriteByte(c)
				switch {
				case c == '\'' || c == '"':
					quote = c
				case c == '\\' && j+1 < len(line):
					j++
					word.WriteByte(line[j])
				case c == '$' && j+1 < len(line) && line[j+1] == '(':
					j++
					word.WriteByte('(')
					cmdsub = 1
				}
			}
		}
		if quote == 0 && cmdsub == 0 {
			flush()
		}
		for k, w := range words {
			switch w {
			case "{", "(", "if", "while", "for", "until", "case", "select":
				depth++
				continue
			case "}", ")", "fi", "done", "esac":
				depth--
				continue
			}
			if k == 0 && strings.HasPrefix(w, "(") && !strings.HasPrefix(w, "((") {
				depth++
			}
			if k == 0 && (strings.HasPrefix(w, ")") || strings.HasPrefix(w, "}")) {
				depth--
			}
			if at := strings.Index(w, "<<"); at >= 0 && !strings.HasPrefix(w[at:], "<<<") && (at == 0 || w[at-1] != '<') {
				delim := strings.TrimPrefix(w[at+2:], "-")
				if delim == "" && k+1 < len(words) {
					delim = words[k+1]
				}
				if delim = strings.Trim(delim, `'"`); delim != "" {
					pending = append(pending, delim)
				}
			}
		}
		if len(pending) > 0 {
			heredoc, pending = pending[0], pending[1:]
		}
	}
	return depths
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
		// wantAlso, when set, is a second problem the case must report.
		wantAlso string
		// all, when set, replaces every occurrence of old instead of the first.
		all bool
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
			want: "whose exit may not fail the step",
		},
		{
			name:     "check commented out",
			old:      indent + secretCheckLine + "\n",
			new:      indent + "# " + secretCheckLine + "\n",
			want:     "does not run check-secret-findings.sh on the report",
			wantAlso: "whose exit may not fail the step",
		},
		{
			name: "check masked with a trailing comment",
			old:  secretCheckLine,
			new:  secretCheckLine + " || true # tolerate findings",
			want: "whose exit may not fail the step",
		},
		{
			name: "check masked with colon",
			old:  secretCheckLine,
			new:  secretCheckLine + " || :",
			want: "whose exit may not fail the step",
		},
		{
			name: "check wrapped in a masked block",
			old:  indent + secretCheckLine + "\n",
			new:  indent + "{\n" + indent + "  " + secretCheckLine + "\n" + indent + "} || true\n",
			want: "whose exit may not fail the step",
		},
		{
			name: "check short-circuited by the previous line",
			old:  indent + secretCheckLine + "\n",
			new:  indent + "true ||\n" + indent + secretCheckLine + "\n",
			want: "as a continuation of the previous line",
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
		{
			name: "check in a masked block at column zero",
			old:  indent + secretCheckLine + "\n",
			new:  indent + "{\n" + indent + secretCheckLine + "\n" + indent + "} || true\n",
			want: "inside a shell block",
		},
		{
			name: "check in a masked subshell at column zero",
			old:  indent + secretCheckLine + "\n",
			new:  indent + "(\n" + indent + secretCheckLine + "\n" + indent + ") || true\n",
			want: "inside a shell block",
		},
		{
			name:     "image scan exit swallowed",
			old:      imageScanLine,
			new:      imageScanLine + " || true",
			want:     "does not run trivy image as a top-level command",
			wantAlso: "whose exit may not fail the step",
		},
		{
			name:     "image scan commented out",
			old:      indent + imageScanLine + "\n",
			new:      indent + "# " + imageScanLine + "\n",
			want:     "does not run trivy image as a top-level command",
			wantAlso: "whose exit may not fail the step",
		},
		{
			name: "image scan in a masked block at column zero",
			old:  indent + imageScanLine + "\n",
			new:  indent + "{\n" + indent + imageScanLine + "\n" + indent + "} || true\n",
			want: "inside a shell block",
		},
		{
			name: "set +o errexit in image scan",
			old:  indent + imageScanLine + "\n",
			new:  indent + "set +o errexit\n" + indent + imageScanLine + "\n",
			want: "turns off errexit",
		},
		{
			name: "check in a multiline command substitution",
			old:  indent + secretCheckLine + "\n",
			new:  indent + "result=$(\n" + indent + secretCheckLine + "\n" + indent + "printf ignored)\n",
			want: "inside a shell block",
		},
		{
			name: "image scan in a multiline command substitution",
			old:  indent + imageScanLine + "\n",
			new:  indent + "result=$(\n" + indent + imageScanLine + "\n" + indent + "printf ignored)\n",
			want: "inside a shell block",
		},
		{
			name: "layer scan narrowed to one directory",
			old:  `--no-progress "$work/layers"`,
			new:  `--no-progress "$work/layers/config"`,
			want: "does not scan the whole layer root",
		},
		{
			name: "check short-circuited behind a trailing comment",
			old:  indent + secretCheckLine + "\n",
			new:  indent + "true || # tolerate findings\n" + indent + secretCheckLine + "\n",
			want: "as a continuation of the previous line",
		},
		{
			name: "check short-circuited across a comment line",
			old:  indent + secretCheckLine + "\n",
			new:  indent + "true ||\n" + indent + "# tolerate findings\n" + indent + secretCheckLine + "\n",
			want: "as a continuation of the previous line",
		},
		{
			name: "image scan short-circuited behind a trailing comment",
			old:  indent + imageScanLine + "\n",
			new:  indent + "true || # tolerate findings\n" + indent + imageScanLine + "\n",
			want: "as a continuation of the previous line",
		},
		{
			name: "image scan short-circuited across a comment line",
			old:  indent + imageScanLine + "\n",
			new:  indent + "true ||\n" + indent + "# tolerate findings\n" + indent + imageScanLine + "\n",
			want: "as a continuation of the previous line",
		},
		{
			name: "layer extraction removed",
			old:  indent + "  " + layerExtractLine + "\n",
			new:  "",
			want: "does not extract every layer listed in manifest.json",
		},
		{
			name: "layer extraction masked",
			old:  layerExtractLine,
			new:  layerExtractLine + " || true",
			want: "does not extract every layer listed in manifest.json",
		},
		{
			name: "layer extraction skipped",
			old:  indent + "  " + layerExtractLine + "\n",
			new:  indent + "  continue\n" + indent + "  " + layerExtractLine + "\n",
			want: "does not extract every layer listed in manifest.json",
		},
		{
			name: "layer extraction outside the scanned root",
			old:  `-C "$work/layers/$i"`,
			new:  `-C "$work/unscanned/$i"`,
			want: "does not extract every layer listed in manifest.json",
		},
		{
			name: "layer extraction commented out",
			old:  indent + "  " + layerExtractLine + "\n",
			new:  indent + "  # " + layerExtractLine + "\n",
			want: "does not extract every layer listed in manifest.json",
		},
		{
			name:     "scan and push conditions with always()",
			all:      true,
			old:      "if: ${{ steps.existing.outputs.exists != 'true' }}",
			new:      "if: ${{ always() && steps.existing.outputs.exists != 'true' }}",
			want:     "uses the status function always()",
			wantAlso: "step push has if",
		},
		{
			name:     "scan and push conditions with !cancelled()",
			all:      true,
			old:      "if: ${{ steps.existing.outputs.exists != 'true' }}",
			new:      "if: ${{ !cancelled() && steps.existing.outputs.exists != 'true' }}",
			want:     "uses the status function cancelled()",
			wantAlso: "step push has if",
		},
		{
			name:     "scan and push conditions with success() ||",
			all:      true,
			old:      "if: ${{ steps.existing.outputs.exists != 'true' }}",
			new:      "if: ${{ success() || steps.existing.outputs.exists != 'true' }}",
			want:     "uses the status function success()",
			wantAlso: "step push has if",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mutated := strings.Replace(orig, tc.old, tc.new, 1)
			if tc.all {
				mutated = strings.ReplaceAll(orig, tc.old, tc.new)
			}
			if mutated == orig {
				t.Fatalf("replacement %q found nothing in worker-image.yml", tc.old)
			}
			problems := secretScanProblems([]byte(mutated))
			for _, want := range []string{tc.want, tc.wantAlso} {
				if want == "" {
					continue
				}
				found := false
				for _, p := range problems {
					if strings.Contains(p, want) {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("want a problem containing %q, got %q", want, problems)
				}
			}
		})
	}
}
