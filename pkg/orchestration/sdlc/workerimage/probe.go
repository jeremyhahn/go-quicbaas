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

package workerimage

import (
	"context"
	"maps"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"

	softwarefactoryworker "github.com/jeremyhahn/go-quicbaas/deployments/agents/software-factory-worker"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// ImageRunner runs a one-shot container; orchestration.ExecContainerRuntime
// satisfies it.
type ImageRunner interface {
	RunImage(ctx context.Context, spec *orchestration.ImageRunSpec) (*orchestration.ExecResult, error)
}

// maxDetail bounds the output a check result keeps: enough for the last lines
// of a failure, not a build log.
const maxDetail = 2048

// RunProbes runs every probe of the image against image — an image ID, so the
// result is about exactly one build — as the uid:gid the build was made for,
// and reports each. It stops at nothing: a report naming every failing probe
// is worth more than one naming the first.
//
// An error means a probe could not be RUN (the engine, or the host-side
// workspace, failed); a probe that ran and failed is a result that did not
// pass.
func RunProbes(ctx context.Context, runner ImageRunner, image string, build softwarefactoryworker.Build) ([]CheckResult, error) {
	if strings.TrimSpace(image) == "" {
		return nil, &InvalidConfigError{Field: "image", Message: "cannot be empty"}
	}
	env := softwarefactoryworker.ProbeEnv(build)
	user := strconv.Itoa(build.UID) + ":" + strconv.Itoa(build.GID)
	probes := softwarefactoryworker.Probes()
	results := make([]CheckResult, 0, len(probes))
	for _, probe := range probes {
		result, err := runProbe(ctx, runner, image, user, build.UID, env, probe)
		if err != nil {
			return results, err
		}
		results = append(results, result)
	}
	return results, nil
}

// runProbe runs one probe, with a fresh bind-mounted workspace when it asks
// for one.
func runProbe(ctx context.Context, runner ImageRunner, image, user string, uid int, env map[string]string,
	probe softwarefactoryworker.Probe) (result CheckResult, err error) {
	spec := &orchestration.ImageRunSpec{
		Image:       image,
		User:        user,
		Env:         env,
		NetworkNone: probe.NetworkNone,
		Command:     []string{"sh", "-lec", probe.Script},
		Stdin:       probe.Stdin,
	}
	if probe.Sandboxed {
		// The default policy — the one a spawn applies — with only the process
		// bound lowered when the probe asks, so a fork probe reaches it quickly.
		policy := orchestration.SandboxSecurity{PidsLimit: probe.PidsLimit}
		spec.Security = &policy
		spec.Env = sandboxedProbeEnv(env, policy)
	}
	var workspace string
	if probe.WorkspaceMount {
		dir, mkErr := os.MkdirTemp("", "quicbaas-worker-probe-")
		if mkErr != nil {
			return CheckResult{}, &ProbeWorkspaceError{Probe: probe.Name, Cause: mkErr}
		}
		// A scratch directory that cannot be removed is reported, not left
		// behind quietly: its files belong to this process's own uid, so a
		// failure here means something else is wrong with the host.
		defer func() {
			if rmErr := os.RemoveAll(dir); rmErr != nil && err == nil {
				err = &ProbeWorkspaceError{Probe: probe.Name, Cause: rmErr}
			}
		}()
		workspace = dir
		spec.Mounts = []orchestration.ContainerMount{{Source: dir, Target: "/workspace"}}
	}
	res, err := runner.RunImage(ctx, spec)
	if err != nil {
		return CheckResult{}, err
	}
	result = CheckResult{Name: probe.Name, Passed: res.ExitCode == 0, Detail: tail(res.Stdout + res.Stderr)}
	if !result.Passed || workspace == "" {
		return result, nil
	}
	return checkWorkspaceOwner(result, workspace, uid)
}

// sandboxedProbeEnv returns a copy of env carrying what a probe run under
// policy must observe: its locked-memory limit, in KiB as `ulimit -l` reports
// it. The expectation is DERIVED from the policy the probe is launched under,
// never restated, so the probe cannot pass against a limit production does not
// set.
func sandboxedProbeEnv(env map[string]string, policy orchestration.SandboxSecurity) map[string]string {
	out := maps.Clone(env)
	for _, u := range policy.EffectiveUlimits() {
		if u.Name == orchestration.UlimitMemlock {
			out[softwarefactoryworker.EnvExpectedMemlockKiB] = strconv.FormatInt(u.Soft/1024, 10)
		}
	}
	return out
}

// checkWorkspaceOwner fails a passing result whose workspace write landed
// owned by anyone but the uid the probe ran as.
func checkWorkspaceOwner(result CheckResult, workspace string, uid int) (CheckResult, error) {
	owner, err := fileOwner(filepath.Join(workspace, softwarefactoryworker.ProbeWorkspaceFile))
	if err != nil {
		return CheckResult{}, &ProbeWorkspaceError{Probe: result.Name, Cause: err}
	}
	if owner != uid {
		result.Passed = false
		result.Detail = "a bind-mounted write landed owned by uid " + strconv.Itoa(owner) + ", not " + strconv.Itoa(uid)
	}
	return result, nil
}

// tail keeps the end of out, where a failing script says why.
func tail(out string) string {
	out = strings.TrimSpace(out)
	if len(out) <= maxDetail {
		return out
	}
	start := len(out) - maxDetail
	for start < len(out) && !utf8.RuneStart(out[start]) {
		start++
	}
	return "…" + out[start:]
}

// failedNames lists the checks that did not pass.
func failedNames(results []CheckResult) []string {
	var failed []string
	for _, r := range results {
		if !r.Passed {
			failed = append(failed, r.Name)
		}
	}
	return failed
}
