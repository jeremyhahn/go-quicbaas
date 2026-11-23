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
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	// hostedRunner is the standard GitHub-hosted runner (ci-template v1.43
	// and v1.46). Since v1.54 (ruling 1137) the jobs on overflowRunnerDocker
	// and overflowRunnerGate take it from the preflight output whenever the
	// balanced selector picks the hosted pool.
	hostedRunner = "ubuntu-latest"
	// overflowRunnerDocker is ci-template v1.48's runs-on for a job that
	// builds or runs images: the preflight `runner_docker` output on
	// github.com ("ubuntu-latest", or the Dells' bigdisk-docker labels when
	// v1.54's balanced selector picks them), and the forge label bigdisk
	// elsewhere.
	overflowRunnerDocker = `${{ github.server_url == 'https://github.com' && fromJSON(needs.preflight.outputs.runner_docker) || 'bigdisk' }}`
	// overflowRunnerGate is ci-template v1.48's runs-on for CI Gate: the
	// preflight `runner` output on github.com, and ci-gate elsewhere.
	overflowRunnerGate = `${{ github.server_url == 'https://github.com' && fromJSON(needs.preflight.outputs.runner || '"ubuntu-latest"') || 'ci-gate' }}`
	// preflightRunner is preflight's runs-on (ci-template v1.52): on
	// github.com the Dells' bigdisk pool unless the repository variable
	// CI_PREFLIGHT_RUNS_ON (runs-on JSON) overrides it, and off github.com
	// ubuntu-latest. A pull request (pull_request or pull_request_target)
	// whose head repository is not this one, a deleted fork included, takes
	// ubuntu-latest whatever CI_PREFLIGHT_RUNS_ON holds: this preflight runs
	// the pull request's scripts/docs-only.sh.
	preflightRunner = `${{ github.server_url == 'https://github.com' && (!startsWith(github.event_name, 'pull_request') || github.event.pull_request.head.repo.full_name == github.repository) && fromJSON(vars.CI_PREFLIGHT_RUNS_ON || '["self-hosted","linux","x64","bigdisk"]') || 'ubuntu-latest' }}`
	// selfHostedWaiver opens the waiver a job that leaves hostedRunner
	// carries in its runs-on line comment, followed by a category and a
	// measured reason.
	selfHostedWaiver = "# self-hosted: "
)

// waiverCategories are the categories a ci.yml waiver may name: the job
// needs specific hardware, or it needs more than a standard hosted runner's
// 4 vCPU, 16 GB memory, about 14 GB free disk or 6 hours. `credential:` is
// only for the Codex review job in code-review.yml, never in ci.yml.
var waiverCategories = []string{"hardware", "oversize"}

// ciJob is one ci.yml job's runs-on node.
type ciJob struct {
	name   string
	runsOn *yaml.Node
}

// runnerJobs parses ci.yml and returns its jobs in order, or a problem.
func runnerJobs(body []byte) ([]ciJob, string) {
	var doc yaml.Node
	if err := yaml.Unmarshal(body, &doc); err != nil || len(doc.Content) == 0 {
		return nil, "ci.yml does not parse: " + errString(err)
	}
	jobs := mapValue(doc.Content[0], "jobs")
	if jobs == nil || len(jobs.Content) == 0 {
		return nil, "ci.yml has no jobs"
	}
	var out []ciJob
	for i := 0; i+1 < len(jobs.Content); i += 2 {
		out = append(out, ciJob{jobs.Content[i].Value, mapValue(jobs.Content[i+1], "runs-on")})
	}
	return out, ""
}

// waiverProblem checks the self-hosted waiver in a runs-on line comment and
// returns "" when it names an allowed category and a non-empty reason.
func waiverProblem(comment string) string {
	i := strings.Index(comment+" ", strings.TrimSpace(selfHostedWaiver)+" ")
	if i < 0 {
		return "carries no `" + selfHostedWaiver + "hardware: <reason>` or `" + selfHostedWaiver + "oversize: <reason>` waiver"
	}
	rest := strings.TrimSpace(comment[i+len(strings.TrimSpace(selfHostedWaiver)):])
	category, reason, ok := strings.Cut(rest, ":")
	category = strings.TrimSpace(category)
	switch {
	case ok && category == "credential":
		return "has a credential: waiver, which only the Codex review job in code-review.yml may carry"
	case !ok || !slices.Contains(waiverCategories, category):
		return "has a waiver that names no category: want `" + selfHostedWaiver + "hardware: <reason>` or `" + selfHostedWaiver + "oversize: <reason>`, got " + strconv.Quote(comment)
	case strings.TrimSpace(reason) == "":
		return "has a " + category + ": waiver with an empty reason"
	}
	return ""
}

// runnerProblems lists every ci.yml job whose runs-on breaks the runner
// rule, given the workflow's text: a job runs on hostedRunner, on one of
// ci-template v1.48's two runs-on expressions exactly (overflowRunnerDocker,
// overflowRunnerGate: the pool v1.54's balanced selector picks), or on a
// self-hosted label with a categorised waiver, and never on
// any other expression, which can pick a self-hosted label.
func runnerProblems(body []byte) []string {
	jobs, problem := runnerJobs(body)
	if problem != "" {
		return []string{problem}
	}
	var problems []string
	for _, j := range jobs {
		where := "job " + j.name
		switch {
		case j.runsOn == nil:
			problems = append(problems, where+" has no runs-on")
		case j.runsOn.Kind != yaml.ScalarNode:
			problems = append(problems, where+" runs on a label list; every job runs on "+hostedRunner+" or one self-hosted label with a waiver")
			if p := waiverProblem(j.runsOn.LineComment); p != "" {
				problems = append(problems, where+" "+p)
			}
		case isOverflowRunner(j.runsOn.Value):
		case j.name == "preflight" && j.runsOn.Value == preflightRunner:
		case strings.Contains(j.runsOn.Value, "${{"):
			problems = append(problems, where+" runs on an expression, which can pick a self-hosted label: "+strconv.Quote(j.runsOn.Value))
		case j.runsOn.Value != hostedRunner:
			if p := waiverProblem(j.runsOn.LineComment); p != "" {
				problems = append(problems, where+" runs on "+strconv.Quote(j.runsOn.Value)+" and "+p)
			}
		}
	}
	return problems
}

// isOverflowRunner reports whether a runs-on value is exactly one of
// ci-template v1.48's two hosted-first expressions.
func isOverflowRunner(v string) bool {
	return v == overflowRunnerDocker || v == overflowRunnerGate
}

// mapValue returns the value of key in the mapping n, or nil.
func mapValue(n *yaml.Node, key string) *yaml.Node {
	if n == nil || n.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == key {
			return n.Content[i+1]
		}
	}
	return nil
}

func errString(err error) string {
	if err == nil {
		return "empty document"
	}
	return err.Error()
}

func readCI(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(repoRoot, ".github", "workflows", "ci.yml"))
	if err != nil {
		t.Fatalf("read ci.yml: %v", err)
	}
	return b
}

// TestCIRunsHostedFirst holds the runner rule on ci.yml, and that no job
// carries a self-hosted waiver today: every job runs on hostedRunner or on
// one of ci-template v1.48's two hosted-first runs-on expressions.
func TestCIRunsHostedFirst(t *testing.T) {
	body := readCI(t)
	for _, p := range runnerProblems(body) {
		t.Error(p)
	}
	jobs, problem := runnerJobs(body)
	if problem != "" {
		t.Fatal(problem)
	}
	for _, j := range jobs {
		if j.runsOn != nil && j.runsOn.Value != hostedRunner && !isOverflowRunner(j.runsOn.Value) &&
			(j.name != "preflight" || j.runsOn.Value != preflightRunner) {
			t.Errorf("job %s runs on %q; no ci.yml job is waived off %s today", j.name, j.runsOn.Value, hostedRunner)
		}
	}
}

// TestRunnerProblems_Mutations shows that runnerProblems reports each
// regression of ci.yml's runners, and accepts a categorised waiver.
func TestRunnerProblems_Mutations(t *testing.T) {
	body := string(readCI(t))
	if p := runnerProblems([]byte(body)); len(p) != 0 {
		t.Fatalf("ci.yml itself has problems: %v", p)
	}
	coverage := func(runsOn string) func(string) string {
		return func(b string) string { return replaceJobRunner(t, b, "coverage", runsOn) }
	}
	const split = `${{ contains(fromJSON('[0, 2]'), matrix.shard) && ((github.event_name == 'push' && github.ref == 'refs/heads/main') || (github.event_name == 'pull_request' && github.event.pull_request.head.repo.full_name == github.repository)) && fromJSON('["self-hosted","linux","x64","bigdisk"]') || 'ubuntu-latest' }}`
	for _, c := range []struct {
		name   string
		mutate func(string) string
		want   string // "" when the mutation is allowed
	}{
		{"a job sent to a self-hosted label", coverage("bigdisk"), "job coverage runs on \"bigdisk\" and carries no"},
		{"a job sent to a self-hosted label list", coverage("[self-hosted, linux, x64, bigdisk]"), "job coverage runs on a label list"},
		{"the split expression put back", func(b string) string {
			return replaceJobRunner(t, b, "unit", split+"  "+selfHostedWaiver+"oversize: overflow split")
		}, "job unit runs on an expression"},
		{"the Gitea release arm put back", func(b string) string {
			return replaceJobRunner(t, b, "publish-images", `${{ github.server_url == 'https://github.com' && 'ubuntu-latest' || 'release' }}`)
		}, "job publish-images runs on an expression"},
		{"a v1.48 output read without its forge arm", coverage(`${{ fromJSON(needs.preflight.outputs.runner_docker) }}`), "job coverage runs on an expression"},
		{"preflight's runs-on on another job", coverage(preflightRunner), "job coverage runs on an expression"},
		{"a bare waiver", coverage("bigdisk  " + selfHostedWaiver + "needs a big disk"), "names no category"},
		{"a credential waiver", coverage("bigdisk  " + selfHostedWaiver + "credential: the token is here"), "credential: waiver"},
		{"a waiver with an empty reason", coverage("bigdisk  " + selfHostedWaiver + "hardware: "), "hardware: waiver with an empty reason"},
		{"a waiver with an empty oversize reason", coverage("bigdisk  " + selfHostedWaiver + "oversize:"), "oversize: waiver with an empty reason"},
		{"a waiver with neither category nor reason", coverage("bigdisk  " + selfHostedWaiver), "names no category"},
		{"a hardware waiver is allowed", coverage("gpu  " + selfHostedWaiver + "hardware: needs the GPU host"), ""},
		{"an oversize waiver is allowed", coverage("bigdisk  " + selfHostedWaiver + "oversize: measured 40 GB of disk"), ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			mutated := c.mutate(body)
			if mutated == body {
				t.Fatal("the mutation changed nothing")
			}
			p := runnerProblems([]byte(mutated))
			if c.want == "" {
				if len(p) != 0 {
					t.Fatalf("want no problem, got %v", p)
				}
				return
			}
			if !slices.ContainsFunc(p, func(s string) bool { return strings.Contains(s, c.want) }) {
				t.Fatalf("want a problem containing %q, got %v", c.want, p)
			}
		})
	}
}

// TestRunnerProblems_Unparsable shows a broken or empty workflow is reported.
func TestRunnerProblems_Unparsable(t *testing.T) {
	for in, want := range map[string]string{"jobs: [": "does not parse", "on: push\n": "has no jobs"} {
		if p := runnerProblems([]byte(in)); len(p) != 1 || !strings.Contains(p[0], want) {
			t.Errorf("runnerProblems(%q) = %v; want one problem containing %q", in, p, want)
		}
	}
}

// replaceJobRunner replaces the runs-on line of job in the workflow text.
func replaceJobRunner(t *testing.T, body, job, runsOn string) string {
	t.Helper()
	start := strings.Index(body, "\n  "+job+":\n")
	if start < 0 {
		t.Fatalf("ci.yml has no job %s", job)
	}
	i := strings.Index(body[start:], "    runs-on: ") + start
	end := strings.Index(body[i:], "\n") + i
	return body[:i] + "    runs-on: " + runsOn + body[end:]
}

// jobRunners is every ci.yml job's exact runs-on: the hosted-only jobs (the
// non-gating e2e-ui job among them), the jobs that build or run images (the
// Docker overflow class) and CI Gate.
var jobRunners = map[string]string{
	"preflight":      preflightRunner,
	"publish-images": hostedRunner,
	"unit":           hostedRunner,
	"coverage":       hostedRunner,
	"unit-passes":    hostedRunner,
	"e2e-ui":         hostedRunner,
	"lint":           overflowRunnerDocker,
	"image":          overflowRunnerDocker,
	"lint-gates":     overflowRunnerDocker,
	"integration":    overflowRunnerDocker,
	"ci-gate":        overflowRunnerGate,
}

// jobRunnerProblems lists each ci.yml job whose runs-on is not its jobRunners
// value, each job missing from the table, and each table job missing from
// ci.yml.
func jobRunnerProblems(body []byte) []string {
	jobs, problem := runnerJobs(body)
	if problem != "" {
		return []string{problem}
	}
	var problems []string
	seen := map[string]bool{}
	for _, j := range jobs {
		seen[j.name] = true
		want, ok := jobRunners[j.name]
		switch {
		case !ok:
			problems = append(problems, "job "+j.name+" is not in the runs-on table")
		case j.runsOn == nil:
			problems = append(problems, "job "+j.name+" has no runs-on; want "+strconv.Quote(want))
		case j.runsOn.Kind != yaml.ScalarNode || j.runsOn.Value != want:
			problems = append(problems, "job "+j.name+" runs on "+strconv.Quote(j.runsOn.Value)+"; want "+strconv.Quote(want))
		}
	}
	for _, name := range slices.Sorted(maps.Keys(jobRunners)) {
		if !seen[name] {
			problems = append(problems, "the runs-on table's job "+name+" is not in ci.yml")
		}
	}
	return problems
}

// TestCIJobRunners: each job runs on exactly its class's runs-on.
func TestCIJobRunners(t *testing.T) {
	for _, p := range jobRunnerProblems(readCI(t)) {
		t.Error(p)
	}
}

// TestCIJobRunners_Mutations: the per-job check catches a job moved to
// another class.
func TestCIJobRunners_Mutations(t *testing.T) {
	body := string(readCI(t))
	for _, c := range []struct{ job, runsOn, want string }{
		{"lint", hostedRunner, "job lint runs on \"ubuntu-latest\""},
		{"image", overflowRunnerGate, "job image runs on"},
		{"ci-gate", overflowRunnerDocker, "job ci-gate runs on"},
		{"coverage", overflowRunnerDocker, "job coverage runs on"},
		{"preflight", hostedRunner, "job preflight runs on \"ubuntu-latest\""},
	} {
		t.Run(c.job, func(t *testing.T) {
			mutated := replaceJobRunner(t, body, c.job, c.runsOn)
			if mutated == body {
				t.Fatal("the mutation changed nothing")
			}
			p := jobRunnerProblems([]byte(mutated))
			if !slices.ContainsFunc(p, func(s string) bool { return strings.Contains(s, c.want) }) {
				t.Fatalf("want a problem containing %q, got %v", c.want, p)
			}
		})
	}
	if p := jobRunnerProblems([]byte(strings.Replace(body, "\n  unit-passes:\n", "\n  unit-passes-renamed:\n", 1))); !slices.ContainsFunc(p, func(s string) bool {
		return strings.Contains(s, "unit-passes-renamed is not in the runs-on table")
	}) || !slices.ContainsFunc(p, func(s string) bool { return strings.Contains(s, "job unit-passes is not in ci.yml") }) {
		t.Errorf("a renamed job: want both the unknown and the missing job reported, got %v", p)
	}
}

// ghToken is one token of a GitHub Actions expression: a single-quoted
// string literal (kind 's', text unquoted), an identifier or context path
// (kind 'i') or an operator or bracket (kind 'o').
type ghToken struct {
	kind byte
	text string
}

func ghTokenize(s string) ([]ghToken, error) {
	var toks []ghToken
	for i := 0; i < len(s); {
		c := s[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n':
			i++
		case c == '\'':
			var b strings.Builder
			i++
			for {
				if i >= len(s) {
					return nil, fmt.Errorf("unterminated string in %q", s)
				}
				if s[i] == '\'' {
					if i+1 < len(s) && s[i+1] == '\'' {
						b.WriteByte('\'')
						i += 2
						continue
					}
					i++
					break
				}
				b.WriteByte(s[i])
				i++
			}
			toks = append(toks, ghToken{'s', b.String()})
		case strings.HasPrefix(s[i:], "==") || strings.HasPrefix(s[i:], "!=") ||
			strings.HasPrefix(s[i:], "&&") || strings.HasPrefix(s[i:], "||"):
			toks = append(toks, ghToken{'o', s[i : i+2]})
			i += 2
		case strings.ContainsRune("!(),", rune(c)):
			toks = append(toks, ghToken{'o', string(c)})
			i++
		case c == '_' || c == '-' || c == '.' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z':
			j := i
			for j < len(s) && (s[j] == '_' || s[j] == '-' || s[j] == '.' || s[j] >= '0' && s[j] <= '9' ||
				s[j] >= 'a' && s[j] <= 'z' || s[j] >= 'A' && s[j] <= 'Z') {
				j++
			}
			toks = append(toks, ghToken{'i', s[i:j]})
			i = j
		default:
			return nil, fmt.Errorf("unexpected %q in %q", c, s)
		}
	}
	return toks, nil
}

// ghParser evaluates a token list against a context while it parses. With
// eval false it only parses: the operand a short circuit skips. It calls
// four functions: fromJSON, startsWith (case-insensitive, its arguments cast
// to strings), always() and cancelled(). The context key `__cancelled`
// stands for the run's cancelled status; a context without it is a run that
// is not cancelled.
type ghParser struct {
	toks []ghToken
	pos  int
	ctx  map[string]any
}

func (p *ghParser) peek(op string) bool {
	return p.pos < len(p.toks) && p.toks[p.pos].kind == 'o' && p.toks[p.pos].text == op
}

func (p *ghParser) or(eval bool) (any, error) {
	v, err := p.and(eval)
	for err == nil && p.peek("||") {
		p.pos++
		var r any
		r, err = p.and(eval && !ghTruthy(v))
		if eval && !ghTruthy(v) {
			v = r
		}
	}
	return v, err
}

func (p *ghParser) and(eval bool) (any, error) {
	v, err := p.eq(eval)
	for err == nil && p.peek("&&") {
		p.pos++
		var r any
		r, err = p.eq(eval && ghTruthy(v))
		if eval && ghTruthy(v) {
			v = r
		}
	}
	return v, err
}

func (p *ghParser) eq(eval bool) (any, error) {
	v, err := p.unary(eval)
	for err == nil && (p.peek("==") || p.peek("!=")) {
		neg := p.toks[p.pos].text == "!="
		p.pos++
		var r any
		r, err = p.unary(eval)
		v = ghEqual(v, r) != neg
	}
	return v, err
}

func (p *ghParser) unary(eval bool) (any, error) {
	if p.peek("!") {
		p.pos++
		v, err := p.unary(eval)
		return !ghTruthy(v), err
	}
	return p.primary(eval)
}

func (p *ghParser) primary(eval bool) (any, error) {
	if p.pos >= len(p.toks) {
		return nil, fmt.Errorf("expression ends early")
	}
	t := p.toks[p.pos]
	p.pos++
	switch {
	case t.kind == 'o' && t.text == "(":
		v, err := p.or(eval)
		if err == nil && !p.peek(")") {
			err = fmt.Errorf("missing )")
		}
		p.pos++
		return v, err
	case t.kind == 's':
		return t.text, nil
	case t.kind == 'i' && p.peek("("):
		p.pos++
		var args []any
		if p.peek(")") {
			p.pos++
		} else {
			for {
				arg, err := p.or(eval)
				if err != nil {
					return nil, err
				}
				args = append(args, arg)
				if p.peek(",") {
					p.pos++
					continue
				}
				if !p.peek(")") {
					return nil, fmt.Errorf("%s: missing )", t.text)
				}
				p.pos++
				break
			}
		}
		want := map[string]int{"fromjson": 1, "startswith": 2, "always": 0, "cancelled": 0}
		name := strings.ToLower(t.text)
		n, ok := want[name]
		if !ok {
			return nil, fmt.Errorf("unsupported function %s", t.text)
		}
		if len(args) != n {
			return nil, fmt.Errorf("%s: want %d arguments, got %d", t.text, n, len(args))
		}
		if !eval {
			return nil, nil
		}
		switch name {
		case "always":
			return true, nil
		case "cancelled":
			return ghTruthy(p.ctx["__cancelled"]), nil
		case "startswith":
			return strings.HasPrefix(strings.ToLower(ghString(args[0])), strings.ToLower(ghString(args[1]))), nil
		}
		s, ok := args[0].(string)
		if !ok {
			return nil, fmt.Errorf("fromJSON of a non-string %v", args[0])
		}
		var v any
		if err := json.Unmarshal([]byte(s), &v); err != nil {
			return nil, fmt.Errorf("fromJSON(%q): %w", s, err)
		}
		return v, nil
	case t.kind == 'i':
		switch t.text {
		case "true":
			return true, nil
		case "false":
			return false, nil
		case "null":
			return nil, nil
		}
		var v any = p.ctx
		for _, k := range strings.Split(t.text, ".") {
			m, ok := v.(map[string]any)
			if !ok {
				return nil, nil
			}
			v = m[k]
		}
		return v, nil
	}
	return nil, fmt.Errorf("unexpected %q", t.text)
}

// ghTruthy is GitHub's truthiness: null, false, 0 and ” are falsy.
// ghString casts a value to a string as GitHub's string functions do.
func ghString(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case bool:
		return strconv.FormatBool(x)
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	}
	return fmt.Sprint(v)
}

func ghTruthy(v any) bool {
	switch x := v.(type) {
	case nil:
		return false
	case bool:
		return x
	case float64:
		return x != 0
	case string:
		return x != ""
	}
	return true
}

// ghEqual is GitHub's ==: strings compare ignoring case, and null equals
// only null.
func ghEqual(a, b any) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	if as, ok := a.(string); ok {
		bs, ok := b.(string)
		return ok && strings.EqualFold(as, bs)
	}
	switch a.(type) {
	case bool, float64:
		return a == b
	}
	return false
}

// ghEval evaluates a workflow value: a `${{ }}` expression against ctx, or
// a plain string as itself.
func ghEval(value string, ctx map[string]any) (any, error) {
	s := strings.TrimSpace(value)
	if !strings.HasPrefix(s, "${{") {
		return value, nil
	}
	if !strings.HasSuffix(s, "}}") {
		return nil, fmt.Errorf("unterminated expression %q", value)
	}
	toks, err := ghTokenize(s[3 : len(s)-2])
	if err != nil {
		return nil, err
	}
	p := &ghParser{toks: toks, ctx: ctx}
	v, err := p.or(true)
	if err == nil && p.pos != len(toks) {
		err = fmt.Errorf("trailing tokens in %q", value)
	}
	return v, err
}

func TestGHEval(t *testing.T) {
	ctx := map[string]any{"a": map[string]any{"b": "X"}, "e": ""}
	for _, c := range []struct {
		expr, want string
		ctx        map[string]any
	}{
		{"${{ a.b == 'x' }}", "true", nil},
		{"${{ a.c == null }}", "true", nil},
		{"${{ a.c == '' }}", "false", nil},
		{"${{ e || 'd' }}", `"d"`, nil},
		{"${{ a.b && 'y' }}", `"y"`, nil},
		{"${{ !a.b }}", "false", nil},
		{"${{ a.c && fromJSON('bad') || 'z' }}", `"z"`, nil},
		{"${{ fromJSON('[1, \"it''s\"]') }}", `[1,"it's"]`, nil},
		{"plain", `"plain"`, nil},
		{"${{ startsWith('pull_request_target', 'pull_request') }}", "true", nil},
		{"${{ startsWith('Pull_Request', 'pull_') }}", "true", nil},
		{"${{ startsWith('push', 'pull_request') }}", "false", nil},
		{"${{ !startsWith(a.b, 'y') }}", "true", nil},
		{"${{ a.c && startsWith(a.c, 'x') || 'z' }}", `"z"`, nil},
		{"${{ always() }}", "true", nil},
		{"${{ !cancelled() }}", "true", nil},
		{"${{ e && always() || !cancelled() }}", "true", nil},
		{"${{ !cancelled() }}", "false", map[string]any{"__cancelled": true}},
	} {
		cx := ctx
		if c.ctx != nil {
			cx = c.ctx
		}
		v, err := ghEval(c.expr, cx)
		if err != nil {
			t.Errorf("%s: %v", c.expr, err)
			continue
		}
		if got, _ := json.Marshal(v); string(got) != c.want {
			t.Errorf("%s = %s, want %s", c.expr, got, c.want)
		}
	}
	for _, c := range []struct{ expr, want string }{
		{"${{ contains('ab', 'a') }}", "unsupported function contains"},
		{"${{ startsWith('a') }}", "startsWith"},
		{"${{ fromJSON('1', '2') }}", "fromJSON"},
		{"${{ always(1) }}", "always"},
	} {
		_, err := ghEval(c.expr, ctx)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: want an error containing %q, got %v", c.expr, c.want, err)
		}
	}
}

// ghContext is the expression context of one ci.yml run. repository is
// o/r; for a pull request (pull_request or pull_request_target), head is the head repository's full name, and
// "" a head repository that is gone (null). preflightVar "" leaves
// CI_PREFLIGHT_RUNS_ON unset.
func ghContext(server, event, ref, head, preflightVar string) map[string]any {
	gh := map[string]any{"server_url": server, "event_name": event, "ref": ref, "repository": "o/r"}
	if strings.HasPrefix(event, "pull_request") {
		var repo any
		if head != "" {
			repo = map[string]any{"full_name": head}
		}
		gh["event"] = map[string]any{"pull_request": map[string]any{"head": map[string]any{"repo": repo}}}
	}
	vars := map[string]any{}
	if preflightVar != "" {
		vars["CI_PREFLIGHT_RUNS_ON"] = preflightVar
	}
	return map[string]any{"github": gh, "vars": vars, "secrets": map[string]any{"AGENT_TOKEN": "tok"}}
}

const (
	githubCom   = "https://github.com"
	forge       = "https://forge.example"
	bigdiskJSON = `["self-hosted","linux","x64","bigdisk"]`
	mainRef     = "refs/heads/main"
	prRef       = "refs/pull/1/merge"
)

func TestCIPreflightRunsOn(t *testing.T) {
	runsOn := mapValue(ciJobNode(readCI(t), "preflight"), "runs-on")
	if runsOn == nil || runsOn.Kind != yaml.ScalarNode {
		t.Fatal("ci.yml's preflight job has no scalar runs-on")
	}
	for _, c := range []struct {
		name                           string
		server, event, ref, head, varv string
		want                           string
	}{
		{"github.com push to main, variable unset", githubCom, "push", mainRef, "", "", bigdiskJSON},
		{"github.com push to main, variable hosted", githubCom, "push", mainRef, "", `"ubuntu-latest"`, `"ubuntu-latest"`},
		{"github.com push to main, variable bigdisk", githubCom, "push", mainRef, "", bigdiskJSON, bigdiskJSON},
		{"github.com dispatch, variable unset", githubCom, "workflow_dispatch", mainRef, "", "", bigdiskJSON},
		{"github.com same-repository pull request", githubCom, "pull_request", prRef, "o/r", "", bigdiskJSON},
		{"github.com fork pull request", githubCom, "pull_request", prRef, "fork/r", "", `"ubuntu-latest"`},
		{"github.com fork pull request, variable bigdisk", githubCom, "pull_request", prRef, "fork/r", bigdiskJSON, `"ubuntu-latest"`},
		{"github.com deleted fork pull request", githubCom, "pull_request", prRef, "", "", `"ubuntu-latest"`},
		{"github.com same-repository pull_request_target", githubCom, "pull_request_target", mainRef, "o/r", "", bigdiskJSON},
		{"github.com fork pull_request_target", githubCom, "pull_request_target", mainRef, "fork/r", "", `"ubuntu-latest"`},
		{"github.com fork pull_request_target, variable bigdisk", githubCom, "pull_request_target", mainRef, "fork/r", bigdiskJSON, `"ubuntu-latest"`},
		{"github.com deleted fork pull_request_target", githubCom, "pull_request_target", mainRef, "", "", `"ubuntu-latest"`},
		{"off github.com, variable unset", forge, "push", mainRef, "", "", `"ubuntu-latest"`},
		{"off github.com, variable bigdisk", forge, "push", mainRef, "", bigdiskJSON, `"ubuntu-latest"`},
	} {
		t.Run(c.name, func(t *testing.T) {
			v, err := ghEval(runsOn.Value, ghContext(c.server, c.event, c.ref, c.head, c.varv))
			if err != nil {
				t.Fatal(err)
			}
			if got, _ := json.Marshal(v); string(got) != c.want {
				t.Errorf("preflight runs on %s, want %s", got, c.want)
			}
		})
	}
}

func TestCIPreflightAgentToken(t *testing.T) {
	const guard = "Is a main push run of this workflow in flight?"
	steps := mapValue(ciJobNode(readCI(t), "preflight"), "steps")
	if steps == nil {
		t.Fatal("ci.yml's preflight job has no steps")
	}
	var bot *yaml.Node
	for _, s := range steps.Content {
		if n := mapValue(s, "name"); n != nil && n.Value == guard {
			bot = mapValue(mapValue(s, "env"), "BOT")
		}
	}
	if bot == nil {
		t.Fatalf("ci.yml's preflight step %q has no BOT env", guard)
	}
	for _, c := range []struct{ name, server, event, ref, want string }{
		{"github.com push to main", githubCom, "push", mainRef, `""`},
		{"github.com dispatch", githubCom, "workflow_dispatch", mainRef, `""`},
		{"off github.com push to main", forge, "push", mainRef, `"tok"`},
		{"off github.com pull request", forge, "pull_request", prRef, `""`},
	} {
		t.Run(c.name, func(t *testing.T) {
			v, err := ghEval(bot.Value, ghContext(c.server, c.event, c.ref, "o/r", ""))
			if err != nil {
				t.Fatal(err)
			}
			if got, _ := json.Marshal(v); string(got) != c.want {
				t.Errorf("BOT is %s, want %s", got, c.want)
			}
		})
	}
}

// inImageMemoryExpr8g and inImageMemoryExpr16g are IN_IMAGE_MEMORY on the jobs
// that run a stage in the toolchain image: a cap everywhere but a GitHub-hosted
// runner, keyed on the preflight runner_docker output (the runner context is
// not available in jobs.<id>.env). Lint and image take 16g, since
// golangci-lint is killed at 8g on a self-hosted runner (#2657); lint-gates
// and integration keep 8g.
const (
	inImageMemoryExpr8g  = `${{ (github.server_url != 'https://github.com' || needs.preflight.outputs.runner_docker != '"ubuntu-latest"') && '8g' || 'none' }}`
	inImageMemoryExpr16g = `${{ (github.server_url != 'https://github.com' || needs.preflight.outputs.runner_docker != '"ubuntu-latest"') && '16g' || 'none' }}`
)

// inImageMemoryJobs maps each job to its wanted IN_IMAGE_MEMORY expression:
// 16g on lint and image, 8g on lint-gates and integration.
var inImageMemoryJobs = map[string]string{
	"lint":        inImageMemoryExpr16g,
	"image":       inImageMemoryExpr16g,
	"lint-gates":  inImageMemoryExpr8g,
	"integration": inImageMemoryExpr8g,
}

// inImageMemoryCap is the cap a wanted expression sets on a self-hosted runner.
func inImageMemoryCap(expr string) string {
	if expr == inImageMemoryExpr16g {
		return "16g"
	}
	return "8g"
}

// ciJobNode is ci.yml's job <name>, or nil.
func ciJobNode(body []byte, name string) *yaml.Node {
	var doc yaml.Node
	if yaml.Unmarshal(body, &doc) != nil || len(doc.Content) == 0 {
		return nil
	}
	return mapValue(mapValue(doc.Content[0], "jobs"), name)
}

func inImageMemoryProblems(body []byte) []string {
	var problems []string
	for _, name := range slices.Sorted(maps.Keys(inImageMemoryJobs)) {
		want := inImageMemoryJobs[name]
		j := ciJobNode(body, name)
		if j == nil {
			problems = append(problems, "ci.yml has no job "+name)
			continue
		}
		switch v := mapValue(mapValue(j, "env"), "IN_IMAGE_MEMORY"); {
		case v == nil:
			problems = append(problems, "job "+name+" sets no env.IN_IMAGE_MEMORY")
		case v.Value != want:
			problems = append(problems, "job "+name+" sets IN_IMAGE_MEMORY to "+strconv.Quote(v.Value)+"; want "+strconv.Quote(want)+" ("+inImageMemoryCap(want)+" on every runner that is not GitHub-hosted)")
		}
	}
	return problems
}

// TestCIInImageMemory: an overflow stage on a Dell keeps its cap, 16g on lint
// and image, 8g on lint-gates and integration.
func TestCIInImageMemory(t *testing.T) {
	for _, p := range inImageMemoryProblems(readCI(t)) {
		t.Error(p)
	}
}

// replaceInImageMemory is ci.yml body with job's IN_IMAGE_MEMORY value set to
// expr.
func replaceInImageMemory(t *testing.T, body, job, expr string) string {
	t.Helper()
	start := strings.Index(body, "\n  "+job+":\n")
	if start < 0 {
		t.Fatalf("ci.yml has no job %s", job)
	}
	i := strings.Index(body[start:], "      IN_IMAGE_MEMORY: ")
	if i < 0 {
		t.Fatalf("job %s has no IN_IMAGE_MEMORY line", job)
	}
	i += start
	end := strings.Index(body[i:], "\n") + i
	mutated := body[:i] + "      IN_IMAGE_MEMORY: " + expr + body[end:]
	if mutated == body {
		t.Fatal("the mutation changed nothing")
	}
	return mutated
}

// TestCIInImageMemory_Mutations: the server-only expression, which leaves an
// overflow stage on a Dell uncapped, is reported on every job, and the 8g
// expression is reported on a 16g job.
func TestCIInImageMemory_Mutations(t *testing.T) {
	body := string(readCI(t))
	const old = `${{ github.server_url != 'https://github.com' && '8g' || 'none' }}`
	for _, job := range slices.Sorted(maps.Keys(inImageMemoryJobs)) {
		t.Run(job, func(t *testing.T) {
			p := inImageMemoryProblems([]byte(replaceInImageMemory(t, body, job, old)))
			if len(p) != 1 || !strings.Contains(p[0], "job "+job+" sets IN_IMAGE_MEMORY") {
				t.Fatalf("want one problem for job %s, got %v", job, p)
			}
		})
		if inImageMemoryJobs[job] != inImageMemoryExpr16g {
			continue
		}
		t.Run(job+"-at-8g", func(t *testing.T) {
			p := inImageMemoryProblems([]byte(replaceInImageMemory(t, body, job, inImageMemoryExpr8g)))
			if len(p) != 1 || !strings.Contains(p[0], "job "+job+" sets IN_IMAGE_MEMORY") || !strings.Contains(p[0], "(16g on") {
				t.Fatalf("want one 16g problem for job %s, got %v", job, p)
			}
		})
	}
}

// selectorStepName is the preflight step that picks the hosted or the
// self-hosted pool (ci-template v1.54, ruling 1137).
const selectorStepName = "Balanced runner pool (ruling 1137)"

// selectorStep is the selector step's env and run body.
func selectorStep(t *testing.T) (map[string]string, string) {
	t.Helper()
	steps := mapValue(ciJobNode(readCI(t), "preflight"), "steps")
	if steps == nil {
		t.Fatal("ci.yml's preflight job has no steps")
	}
	for _, s := range steps.Content {
		if n := mapValue(s, "name"); n == nil || n.Value != selectorStepName {
			continue
		}
		env := map[string]string{}
		if e := mapValue(s, "env"); e != nil {
			for i := 0; i+1 < len(e.Content); i += 2 {
				env[e.Content[i].Value] = e.Content[i+1].Value
			}
		}
		run := mapValue(s, "run")
		if run == nil || run.Value == "" {
			t.Fatalf("step %q has no run body", selectorStepName)
		}
		return env, run.Value
	}
	t.Fatalf("ci.yml's preflight job has no step %q", selectorStepName)
	return nil, ""
}

// fakeCurl answers each URL from $FAKE_CURL_DIR: <key>.body goes to the -o
// file and <key>.code is printed for -w, where <key> is the URL with every
// character that is not a letter or digit made `_`. An unknown URL is a 404.
// Every URL is appended to $FAKE_CURL_CALLS. -H and its value are ignored.
const fakeCurl = `#!/usr/bin/env bash
out= url=
while [ $# -gt 0 ]; do
  case "$1" in
    -o) out=$2; shift 2 ;;
    -w|-H|--max-time) shift 2 ;;
    *) url=$1; shift ;;
  esac
done
printf '%s\n' "$url" >> "$FAKE_CURL_CALLS"
f="$FAKE_CURL_DIR/${url//[^A-Za-z0-9]/_}"
if [ -f "$f.code" ]; then
  cp "$f.body" "$out"
  printf '%s' "$(< "$f.code")"
else
  : > "$out"
  printf 404
fi
`

func fixtureKey(url string) string {
	return strings.Map(func(r rune) rune {
		if r < 128 && (r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9') {
			return r
		}
		return '_'
	}, url)
}

type apiReply struct {
	code int
	body string
}

// apiFixture is the Actions API the selector sees: URL → reply.
type apiFixture map[string]apiReply

const selectAPI = "https://api.github.com/repos/o/r/actions"

func runsURL(status string, page int) string {
	return selectAPI + "/runs?status=" + status + "&per_page=100&page=" + strconv.Itoa(page)
}

func jobsURL(run, page int) string {
	return selectAPI + "/runs/" + strconv.Itoa(run) + "/jobs?filter=latest&per_page=100&page=" + strconv.Itoa(page)
}

// list sets url's reply to a 200 page of <key> items of a listing of total.
func (f apiFixture) list(t *testing.T, url, key string, total int, items []any) {
	t.Helper()
	if items == nil {
		items = []any{}
	}
	b, err := json.Marshal(map[string]any{"total_count": total, key: items})
	if err != nil {
		t.Fatal(err)
	}
	f[url] = apiReply{200, string(b)}
}

// selJob is one job of the listing, created age ago.
func selJob(run int, status string, age time.Duration, labels ...string) map[string]any {
	return map[string]any{
		"run_id":     run,
		"status":     status,
		"labels":     labels,
		"created_at": time.Now().UTC().Add(-age).Format(time.RFC3339),
	}
}

func repeatJob(n int, j map[string]any) []map[string]any {
	out := make([]map[string]any, n)
	for i := range out {
		out[i] = j
	}
	return out
}

// loadFixture: no queued run, and one in-progress run per run_id of jobs.
func loadFixture(t *testing.T, jobs ...map[string]any) apiFixture {
	t.Helper()
	f := apiFixture{}
	f.list(t, runsURL("queued", 1), "workflow_runs", 0, nil)
	byRun := map[int][]any{}
	for _, j := range jobs {
		byRun[j["run_id"].(int)] = append(byRun[j["run_id"].(int)], j)
	}
	if len(byRun) == 0 {
		byRun[1] = nil
	}
	var runs []any
	for _, id := range slices.Sorted(maps.Keys(byRun)) {
		runs = append(runs, map[string]any{"id": id})
		f.list(t, jobsURL(id, 1), "jobs", len(byRun[id]), byRun[id])
	}
	f.list(t, runsURL("in_progress", 1), "workflow_runs", len(runs), runs)
	return f
}

type selectRun struct {
	output, stdout string
	calls          []string
}

// runSelector runs the selector step's script with env against the fixture.
func runSelector(t *testing.T, env map[string]string, fix apiFixture) selectRun {
	t.Helper()
	for _, tool := range []string{"bash", "jq"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Fatalf("the selector script needs %s: %v", tool, err)
		}
	}
	stepEnv, run := selectorStep(t)
	dir := t.TempDir()
	binDir, fixDir := filepath.Join(dir, "bin"), filepath.Join(dir, "api")
	for _, d := range []string{binDir, fixDir, filepath.Join(dir, "tmp")} {
		if err := os.Mkdir(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	write := func(path, body string, mode os.FileMode) {
		if err := os.WriteFile(path, []byte(body), mode); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(binDir, "curl"), fakeCurl, 0o755)
	for url, r := range fix {
		k := filepath.Join(fixDir, fixtureKey(url))
		write(k+".body", r.body, 0o644)
		write(k+".code", strconv.Itoa(r.code), 0o644)
	}
	script, output, calls := filepath.Join(dir, "select.sh"), filepath.Join(dir, "output"), filepath.Join(dir, "calls")
	write(script, run, 0o644)
	write(output, "", 0o644)
	cmdEnv := []string{
		"PATH=" + binDir + string(os.PathListSeparator) + os.Getenv("PATH"),
		"GITHUB_OUTPUT=" + output,
		"RUNNER_TEMP=" + filepath.Join(dir, "tmp"),
		"FAKE_CURL_DIR=" + fixDir,
		"FAKE_CURL_CALLS=" + calls,
	}
	for _, k := range slices.Sorted(maps.Keys(stepEnv)) {
		v, ok := env[k]
		if !ok {
			t.Fatalf("step %q reads env %s, which the test does not set", selectorStepName, k)
		}
		cmdEnv = append(cmdEnv, k+"="+v)
	}
	cmd := exec.Command("bash", script)
	cmd.Dir = dir
	cmd.Env = cmdEnv
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("the selector script failed: %v\n%s", err, out)
	}
	o, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	r := selectRun{output: string(o), stdout: string(out)}
	if c, err := os.ReadFile(calls); err == nil {
		r.calls = strings.Fields(string(c))
	}
	return r
}

// TestRunnerSelect runs the selector step's script against a fake Actions API
// (ci-template v1.54, ruling 1137: the pool this repository uses less wins).
// The base limits are HS=12 hosted, C1=4 bigdisk and C2=4 bigdisk-docker.
func TestRunnerSelect(t *testing.T) {
	const (
		hs     = 12
		units  = 4
		runID  = 1000
		hosted = "ubuntu-latest"
	)
	hostedOut := "runner=\"ubuntu-latest\"\nrunner_docker=\"ubuntu-latest\"\n"
	overflowOut := "runner=[\"self-hosted\",\"linux\",\"x64\",\"bigdisk\"]\nrunner_docker=[\"self-hosted\",\"linux\",\"x64\",\"bigdisk-docker\"]\n"
	bigdisk := []string{"self-hosted", "linux", "x64", "bigdisk"}
	bigdiskDocker := []string{"self-hosted", "linux", "x64", "bigdisk-docker"}
	// h, s1, s2: n jobs with status st on hosted (run 1), bigdisk (run 2)
	// and bigdisk-docker (run 3).
	h := func(n int, st string) []map[string]any { return repeatJob(n, selJob(1, st, time.Minute, hosted)) }
	s1 := func(n int, st string) []map[string]any { return repeatJob(n, selJob(2, st, time.Minute, bigdisk...)) }
	s2 := func(n int, st string) []map[string]any {
		return repeatJob(n, selJob(3, st, time.Minute, bigdiskDocker...))
	}
	load := func(parts ...[]map[string]any) apiFixture { return loadFixture(t, slices.Concat(parts...)...) }
	baseEnv := map[string]string{
		"SERVER": "https://github.com", "API": selectAPI, "RUN_ID": strconv.Itoa(runID), "JOB_TOKEN": "job-token",
		"HS": strconv.Itoa(hs), "C1": strconv.Itoa(units), "C2": strconv.Itoa(units),
		"EVENT": "push", "REPO": "o/r", "HEAD_REPO": "",
	}
	const (
		toDells  = "this run's jobs run on the Dells"
		toHosted = "this run's jobs take the GitHub-hosted default"
	)

	// 150 in-progress runs over two pages; only run 150, on page 2, has jobs:
	// HS hosted ones, a full hosted pool.
	paged := apiFixture{}
	paged.list(t, runsURL("queued", 1), "workflow_runs", 0, nil)
	var page1, page2 []any
	for id := 1; id <= 150; id++ {
		var jobs []any
		if id == 150 {
			for range hs {
				jobs = append(jobs, selJob(150, "in_progress", time.Minute, hosted))
			}
		}
		paged.list(t, jobsURL(id, 1), "jobs", len(jobs), jobs)
		if id <= 100 {
			page1 = append(page1, map[string]any{"id": id})
		} else {
			page2 = append(page2, map[string]any{"id": id})
		}
	}
	paged.list(t, runsURL("in_progress", 1), "workflow_runs", 150, page1)
	paged.list(t, runsURL("in_progress", 2), "workflow_runs", 150, page2)

	short := apiFixture{}
	short.list(t, runsURL("queued", 1), "workflow_runs", 0, nil)
	short.list(t, runsURL("in_progress", 1), "workflow_runs", 150, page1)
	short.list(t, runsURL("in_progress", 2), "workflow_runs", 150, nil)
	for id := 1; id <= 100; id++ {
		short.list(t, jobsURL(id, 1), "jobs", 0, nil)
	}

	// v1.50 and v1.59: a short listing is a lower bound. Each fixture below
	// lists fewer entries than its total_count.
	shortRuns := func(base apiFixture, id int) apiFixture {
		f := maps.Clone(base)
		f.list(t, runsURL("in_progress", 1), "workflow_runs", 2, []any{map[string]any{"id": id}})
		f.list(t, runsURL("in_progress", 2), "workflow_runs", 2, nil)
		return f
	}
	shortRunsHostedFull := shortRuns(load(h(hs, "in_progress")), 1)
	shortRunsBelow := shortRuns(load(h(3, "in_progress")), 1)
	shortJobsHostedFull := load(h(hs, "in_progress"))
	var hsJobs []any
	for _, j := range h(hs, "in_progress") {
		hsJobs = append(hsJobs, j)
	}
	shortJobsHostedFull.list(t, jobsURL(1, 1), "jobs", hs+5, hsJobs)
	shortJobsHostedFull.list(t, jobsURL(1, 2), "jobs", hs+5, nil)

	hostedFull := load(h(hs, "in_progress"))
	notOK := load(h(hs, "in_progress"))
	notOK[runsURL("queued", 1)] = apiReply{502, ""}
	noTotal := load(h(hs, "in_progress"))
	noTotal[runsURL("queued", 1)] = apiReply{200, `{"workflow_runs":[]}`}

	for _, c := range []struct {
		name     string
		env      map[string]string
		fix      apiFixture
		overflow bool   // the Dells' labels; false for the hosted default
		warning  string // a ::warning:: line contains it; "" for no warning
		warning2 string // a ::warning:: line contains it too
		notice   string // a ::notice:: line contains it
		notice2  string // a ::notice:: line contains it too
		call     string // a called URL contains it
		notCall  string // no called URL contains it
		noCall   bool
	}{
		// Both pools below their limit: the lower use wins, a tie goes hosted.
		{name: "an idle repository: a tie goes hosted", fix: load(), notice: "use hosted 0/12, bigdisk 0/4, bigdisk-docker 0/4; " + toHosted},
		{name: "hosted busier than the Dells", fix: load(h(3, "in_progress")), overflow: true, notice: "use hosted 3/12, bigdisk 0/4, bigdisk-docker 0/4; " + toDells},
		{name: "equal use: a tie goes hosted", fix: load(h(3, "in_progress"), s1(1, "in_progress")), notice: toHosted},
		{name: "the Dells busier than hosted", fix: load(h(1, "in_progress"), s1(1, "in_progress")), notice: toHosted},
		{name: "both kinds below hosted use", fix: load(h(6, "in_progress"), s1(1, "in_progress"), s2(1, "in_progress")), overflow: true, notice: toDells},
		{name: "bigdisk-docker alone at hosted use", fix: load(h(6, "in_progress"), s2(2, "in_progress")), notice: toHosted},
		{name: "queued jobs count as use", fix: load(h(3, "queued")), overflow: true, notice: "use hosted 3/12"},
		{name: "completed jobs are not counted", fix: load(h(hs, "completed"), s1(units, "completed")), notice: "use hosted 0/12, bigdisk 0/4"},
		{name: "a self-hosted job off bigdisk is not counted", fix: load(h(1, "in_progress"), repeatJob(units, selJob(4, "in_progress", time.Minute, "self-hosted", "ci-gate"))), overflow: true, notice: "bigdisk 0/4, bigdisk-docker 0/4"},
		{name: "this run's own jobs are not counted", fix: load(repeatJob(hs, selJob(runID, "queued", time.Hour, hosted))), notice: "use hosted 0/12", notCall: "/runs/1000/jobs"},
		// One pool full: the other pool.
		{name: "the hosted pool full", fix: hostedFull, overflow: true, notice: "the hosted pool is full (12 of 12); " + toDells},
		{name: "bigdisk full", fix: load(h(11, "in_progress"), s1(units, "in_progress")), notice: "the self-hosted pool is full (bigdisk 4 of 4, bigdisk-docker 0 of 4); " + toHosted},
		{name: "bigdisk-docker full", fix: load(h(11, "in_progress"), s2(units, "in_progress")), notice: "the self-hosted pool is full (bigdisk 0 of 4, bigdisk-docker 4 of 4)"},
		// Both full: fewer queued wins, a tie goes hosted.
		{name: "both full, hosted more queued", fix: load(h(hs, "in_progress"), h(2, "queued"), s1(units, "in_progress"), s2(1, "queued")), overflow: true, notice: "both pools full: hosted 2 queued, self-hosted 1 queued; " + toDells},
		{name: "both full, the Dells more queued", fix: load(h(hs, "in_progress"), h(1, "queued"), s1(units, "in_progress"), s1(1, "queued"), s2(2, "queued")), notice: "both pools full: hosted 1 queued, self-hosted 3 queued; " + toHosted},
		{name: "both full, a tie goes hosted", fix: load(h(hs, "in_progress"), s1(units, "in_progress")), notice: "both pools full: hosted 0 queued, self-hosted 0 queued"},
		// The limits.
		{name: "CI_HOSTED_SHARE unset is 12", env: map[string]string{"HS": ""}, fix: hostedFull, overflow: true, warning: "CI_HOSTED_SHARE='' is unset, 0 or not a whole number; HS=12", notice: "the hosted pool is full (12 of 12)"},
		{name: "CI_HOSTED_SHARE with a leading zero is decimal", env: map[string]string{"HS": "08"}, fix: load(h(8, "in_progress")), overflow: true, notice: "the hosted pool is full (8 of 8)"},
		{name: "CI_HOSTED_SHARE over 6 digits", env: map[string]string{"HS": "1234567"}, fix: hostedFull, overflow: true, warning: "CI_HOSTED_SHARE='1234567'"},
		{name: "CI_SELF_HOSTED_BIGDISK unset", env: map[string]string{"C1": ""}, fix: hostedFull, warning: "CI_SELF_HOSTED_BIGDISK='' is unset", noCall: true},
		{name: "CI_SELF_HOSTED_BIGDISK not a number", env: map[string]string{"C1": "four"}, fix: hostedFull, warning: "CI_SELF_HOSTED_BIGDISK='four'", noCall: true},
		{name: "CI_SELF_HOSTED_BIGDISK_DOCKER 0", env: map[string]string{"C2": "0"}, fix: hostedFull, warning: "CI_SELF_HOSTED_BIGDISK_DOCKER='0'", noCall: true},
		// The listing.
		{name: "a run list over two pages", fix: paged, overflow: true, call: "page=2", notice: "the hosted pool is full"},
		// v1.59: a listing short of its total_count is a lower bound, named in a
		// ::notice, and the choice still uses it; only a failed call goes hosted.
		{name: "a run list short of its total_count is a lower bound", fix: short, notice: "runs?status=in_progress: 100 of 150 entries); the counts are a lower bound", notice2: "use hosted 0/12, bigdisk 0/4, bigdisk-docker 0/4; " + toHosted},
		{name: "a short run list at the hosted share goes to the Dells", fix: shortRunsHostedFull, overflow: true, notice: "runs?status=in_progress: 1 of 2 entries", notice2: "the hosted pool is full (12 of 12); " + toDells},
		{name: "a short job list at the hosted share goes to the Dells", fix: shortJobsHostedFull, overflow: true, notice: "runs/1/jobs?filter=latest: 12 of 17 entries", notice2: "the hosted pool is full (12 of 12); " + toDells},
		{name: "a short run list below every limit still chooses", fix: shortRunsBelow, overflow: true, notice: "1 of 2 entries", notice2: "use hosted 3/12, bigdisk 0/4, bigdisk-docker 0/4; " + toDells},
		{name: "a reply that is not 200", fix: notOK, warning: "/runs?status=queued: 502"},
		{name: "a 200 with no total_count", fix: noTotal, warning: "200 with no total_count"},
		// No selection.
		{name: "not github.com", env: map[string]string{"SERVER": "https://gitea.example"}, fix: hostedFull, noCall: true},
		{name: "a fork pull request with the hosted pool full", env: map[string]string{"EVENT": "pull_request", "HEAD_REPO": "someone/r"}, fix: hostedFull, notice: "head repository is not this repository (a fork)", noCall: true},
		{name: "a deleted fork's pull request with the hosted pool full", env: map[string]string{"EVENT": "pull_request", "HEAD_REPO": ""}, fix: hostedFull, notice: "head repository is not this repository (a fork)", noCall: true},
		{name: "a same-repository pull request with the hosted pool full", env: map[string]string{"EVENT": "pull_request", "HEAD_REPO": "o/r"}, fix: hostedFull, overflow: true},
		{name: "a fork's pull_request_target with the hosted pool full", env: map[string]string{"EVENT": "pull_request_target", "HEAD_REPO": "someone/r"}, fix: hostedFull, notice: "head repository is not this repository (a fork)", noCall: true},
		{name: "a deleted fork's pull_request_target with the hosted pool full", env: map[string]string{"EVENT": "pull_request_target", "HEAD_REPO": ""}, fix: hostedFull, notice: "head repository is not this repository (a fork)", noCall: true},
		{name: "a same-repository pull_request_target with the hosted pool full", env: map[string]string{"EVENT": "pull_request_target", "HEAD_REPO": "o/r"}, fix: hostedFull, overflow: true},
	} {
		t.Run(c.name, func(t *testing.T) {
			env := maps.Clone(baseEnv)
			maps.Copy(env, c.env)
			r := runSelector(t, env, c.fix)
			want := hostedOut
			if c.overflow {
				want = overflowOut
			}
			if r.output != want {
				t.Errorf("GITHUB_OUTPUT = %q; want %q\n%s", r.output, want, r.stdout)
			}
			lines := strings.Split(r.stdout, "\n")
			hasLine := func(prefix, text string) bool {
				return slices.ContainsFunc(lines, func(l string) bool { return strings.HasPrefix(l, prefix) && strings.Contains(l, text) })
			}
			if c.warning == "" && hasLine("::warning::", "") {
				t.Errorf("want no ::warning, got\n%s", r.stdout)
			}
			if c.warning != "" && !hasLine("::warning::", c.warning) {
				t.Errorf("want a ::warning containing %q, got\n%s", c.warning, r.stdout)
			}
			if c.warning2 != "" && !hasLine("::warning::", c.warning2) {
				t.Errorf("want a ::warning containing %q, got\n%s", c.warning2, r.stdout)
			}
			for _, n := range []string{c.notice, c.notice2} {
				if n != "" && !hasLine("::notice::", n) {
					t.Errorf("want a ::notice containing %q, got\n%s", n, r.stdout)
				}
			}
			if c.noCall && len(r.calls) != 0 {
				t.Errorf("want no API call, got %v", r.calls)
			}
			if !c.noCall && len(r.calls) == 0 {
				t.Errorf("want API calls, got none")
			}
			if c.call != "" && !slices.ContainsFunc(r.calls, func(u string) bool { return strings.Contains(u, c.call) }) {
				t.Errorf("want a call containing %q, got %v", c.call, r.calls)
			}
			if c.notCall != "" && slices.ContainsFunc(r.calls, func(u string) bool { return strings.Contains(u, c.notCall) }) {
				t.Errorf("want no call containing %q, got %v", c.notCall, r.calls)
			}
		})
	}
}

// TestRunnerSelect_ForkEnv: the selector reads where the pull request comes
// from out of the event, not out of anything the pull request controls.
func TestRunnerSelect_ForkEnv(t *testing.T) {
	env, _ := selectorStep(t)
	for k, want := range map[string]string{
		"EVENT":     "${{ github.event_name }}",
		"REPO":      "${{ github.repository }}",
		"HEAD_REPO": "${{ github.event.pull_request.head.repo.full_name }}",
	} {
		if env[k] != want {
			t.Errorf("step %q env %s = %q; want %q", selectorStepName, k, env[k], want)
		}
	}
}

// TestRunnerSelect_LimitVars: the balanced selector's limits are the
// repository variables the Infra Department sets (ci-template v1.54), and
// ci.yml no longer carries v1.48's HOSTED_* env.
func TestRunnerSelect_LimitVars(t *testing.T) {
	env, _ := selectorStep(t)
	for k, want := range map[string]string{
		"HS": "${{ vars.CI_HOSTED_SHARE }}",
		"C1": "${{ vars.CI_SELF_HOSTED_BIGDISK }}",
		"C2": "${{ vars.CI_SELF_HOSTED_BIGDISK_DOCKER }}",
	} {
		if env[k] != want {
			t.Errorf("step %q env %s = %q; want %q", selectorStepName, k, env[k], want)
		}
	}
	for _, k := range []string{"LIMIT", "GRACE"} {
		if _, ok := env[k]; ok {
			t.Errorf("step %q still reads env %s (removed in ci-template v1.54)", selectorStepName, k)
		}
	}
	var wf struct {
		Env map[string]any `yaml:"env"`
	}
	if err := yaml.Unmarshal(readCI(t), &wf); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"HOSTED_JOBS_PER_REPO", "HOSTED_QUEUE_GRACE_SECONDS"} {
		if _, ok := wf.Env[k]; ok {
			t.Errorf("ci.yml env still sets %s (removed in ci-template v1.54)", k)
		}
	}
}
