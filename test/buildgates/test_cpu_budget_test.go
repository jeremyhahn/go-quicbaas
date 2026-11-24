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
	"path/filepath"
	"strconv"
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
		"test-awskms-retry":            nil,
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

// makePrint runs `make -s print-<variable>` in the repository with exactly
// env plus GOPROXY=off, and returns the trimmed output.
func makePrint(t *testing.T, env []string, variable string, vars ...string) string {
	t.Helper()
	strip := []string{"GOMAXPROCS=", "TEST_CPU_BUDGET=", "TEST_P=", "TEST_CPU_MAX_FILE=", "MAKEFLAGS=", "MFLAGS=", "MAKELEVEL="}
	var cmdEnv []string
	for _, kv := range os.Environ() {
		keep := true
		for _, prefix := range strip {
			if strings.HasPrefix(kv, prefix) {
				keep = false
				break
			}
		}
		if keep {
			cmdEnv = append(cmdEnv, kv)
		}
	}
	cmdEnv = append(cmdEnv, "GOPROXY=off")
	cmdEnv = append(cmdEnv, env...)
	args := append([]string{"-s", "--no-print-directory", "-C", repoRoot, "print-" + variable}, vars...)
	cmd := exec.Command("make", args...)
	cmd.Env = cmdEnv
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("make -s print-%s %v (env %v): %v\n%s", variable, vars, env, err, out)
	}
	return strings.TrimSpace(string(out))
}

// TestCPUBudgetDerivation pins how the Makefile derives the test CPU budget:
// the budget is the cgroup v2 cpu.max quota rounded up, else GOMAXPROCS, else
// nproc, and each test binary gets budget/TEST_P CPUs, never fewer than 1.
func TestCPUBudgetDerivation(t *testing.T) {
	nprocOut, err := exec.Command("nproc").Output()
	if err != nil {
		t.Fatalf("nproc: %v", err)
	}
	nprocStr := strings.TrimSpace(string(nprocOut))
	nproc, err := strconv.Atoi(nprocStr)
	if err != nil {
		t.Fatalf("nproc printed %q: %v", nprocStr, err)
	}
	nprocBin := nproc / 2
	if nprocBin < 1 {
		nprocBin = 1
	}

	cases := []struct {
		name       string
		cpuMax     string
		env        []string
		testP      string
		wantBudget string
		wantBin    string
	}{
		{"16-CPU quota", "1600000 100000\n", nil, "", "16", "8"},
		{"fractional quota rounds up", "150000 100000\n", nil, "", "2", "1"},
		{"half a CPU", "50000 100000\n", nil, "", "1", "1"},
		{"4 binaries", "1600000 100000\n", nil, "4", "16", "4"},
		{"no quota uses GOMAXPROCS", "max 100000\n", []string{"GOMAXPROCS=6"}, "", "6", "3"},
		{"no file uses GOMAXPROCS", "", []string{"GOMAXPROCS=5"}, "", "5", "2"},
		{"no file and no GOMAXPROCS uses nproc", "", nil, "", nprocStr, strconv.Itoa(nprocBin)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "cpu.max")
			if tc.cpuMax != "" {
				if err := os.WriteFile(path, []byte(tc.cpuMax), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			vars := []string{"TEST_CPU_MAX_FILE=" + path}
			if tc.testP != "" {
				vars = append(vars, "TEST_P="+tc.testP)
			}
			if got := makePrint(t, tc.env, "TEST_CPU_BUDGET", vars...); got != tc.wantBudget {
				t.Errorf("TEST_CPU_BUDGET = %q, want %q", got, tc.wantBudget)
			}
			if got := makePrint(t, tc.env, "TEST_BIN_CPUS", vars...); got != tc.wantBin {
				t.Errorf("TEST_BIN_CPUS = %q, want %q", got, tc.wantBin)
			}
		})
	}
}
