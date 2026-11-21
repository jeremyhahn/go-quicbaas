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
	"os"
	"os/exec"
	"strings"
	"testing"
)

// repoMakeDryRunVars is repoMakeDryRun with extra make arguments (variable
// assignments such as TEST_CPU_BUDGET=16, or -o <target>) appended.
func repoMakeDryRunVars(t *testing.T, target string, vars ...string) string {
	t.Helper()
	args := append([]string{"-n", "--no-print-directory", "-C", repoRoot, target}, vars...)
	cmd := exec.Command("make", args...)
	cmd.Env = append(os.Environ(), "GOPROXY=off")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("make -n %s %v: %v\n%s", target, vars, err, out)
	}
	return string(out)
}

// A job's CPU budget is split across the test binaries go test -p runs at
// once: each binary gets budget/p CPUs for GOMAXPROCS and -parallel, so the
// job as a whole stays within its cgroup quota.
func TestCoverageShardSplitsTheCPUBudget(t *testing.T) {
	out := repoMakeDryRunVars(t, "coverage-shard", "TEST_CPU_BUDGET=16")
	for _, want := range []string{"GOMAXPROCS=8", "-p 2", "-parallel 8"} {
		if !strings.Contains(out, want) {
			t.Errorf("make -n coverage-shard TEST_CPU_BUDGET=16 lacks %q:\n%s", want, out)
		}
	}
}

func TestCPUBudgetNeverDropsBelowOne(t *testing.T) {
	out := repoMakeDryRunVars(t, "coverage-shard", "TEST_CPU_BUDGET=1")
	for _, want := range []string{"GOMAXPROCS=1", "-parallel 1"} {
		if !strings.Contains(out, want) {
			t.Errorf("make -n coverage-shard TEST_CPU_BUDGET=1 lacks %q:\n%s", want, out)
		}
	}
}

func TestEveryCITestTargetCarriesTheSplit(t *testing.T) {
	tagChecks := []string{"check-insecure-tls", "check-build-tags", "check-local-keygen"}
	targets := map[string][]string{
		"coverage-shard":               nil,
		"test-sdk-go":                  nil,
		"test-minimal":                 tagChecks,
		"test-standard":                tagChecks,
		"test-lean":                    tagChecks,
		"test-tooling":                 nil,
		"test-software-factory-worker": nil,
	}
	for target, prereqs := range targets {
		t.Run(target, func(t *testing.T) {
			args := []string{"TEST_CPU_BUDGET=16"}
			for _, p := range prereqs {
				args = append(args, "-o", p)
			}
			out := repoMakeDryRunVars(t, target, args...)
			found := 0
			for _, line := range strings.Split(out, "\n") {
				if !strings.Contains(line, "go test ") {
					continue
				}
				found++
				for _, want := range []string{"GOMAXPROCS=8", "-parallel 8"} {
					if !strings.Contains(line, want) {
						t.Errorf("%s: go test line lacks %q: %s", target, want, line)
					}
				}
			}
			if found == 0 {
				t.Errorf("%s: no go test line in make -n output:\n%s", target, out)
			}
		})
	}
}
