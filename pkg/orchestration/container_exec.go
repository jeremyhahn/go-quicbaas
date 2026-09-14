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

package orchestration

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"strings"
)

// =============================================================================
// Running a command inside a container that is already running
// =============================================================================
//
// The interface had Run, RunInteractive, Stop, Remove, Logs, IsRunning and
// Pull: everything about a container's LIFECYCLE and nothing about its
// inside. The only channel into a live sandboxed session was the agent's own
// stdio pipe — which means the only way to make something happen in a worker's
// container was to ask the worker to do it, in its own conversation.
//
// That is the wrong instrument for a step the SYSTEM owns. A delivery's commit
// and push are not the worker's judgement to exercise: they must happen once,
// with the identity the project configured, whatever the agent decided to say
// that turn. So the runtime grows one verb for them.
//
// WHAT THIS IS NOT. It is not a general remote-execution surface. No transport
// exposes it — a caller that could name a container and a command would have
// arbitrary code execution inside another tenant's sandbox — and it takes a
// container id, not a session id, so reaching it at all means already holding
// the engine's own handle on a running container.

// ExecSpec is one command run inside a container that is already running.
//
// It carries no image, no mounts and no network: those are the LAUNCH
// configuration, they were measured when the container started
// (ContainerConfigDigest), and a command that could change them would be a
// second, unmeasured launch wearing an exec's clothes.
type ExecSpec struct {
	// Cmd is the argument vector, entry point first. It is executed
	// directly, never through a shell: a caller that wants shell semantics
	// asks for a shell by name.
	Cmd []string `json:"cmd"`

	// Env is added to the environment the container already has. It travels
	// in a private 0600 --env-file exactly as a launch environment does —
	// never as "-e KEY=VALUE" in the argument vector, where every local user
	// can read it out of /proc/<pid>/cmdline. See container_env_file.go.
	Env map[string]string `json:"env,omitempty"`

	// WorkDir is the directory inside the CONTAINER the command runs in.
	// Empty takes the container's own working directory.
	WorkDir string `json:"work_dir,omitempty"`

	// Stdin is fed to the command. Empty leaves stdin closed, which is what
	// a non-interactive command should see.
	Stdin []byte `json:"-"`
}

// Validate checks that the ExecSpec names a command that can be run.
func (s *ExecSpec) Validate() error {
	if s == nil {
		return &InputValidationError{Field: "spec", Message: "cannot be nil"}
	}
	if len(s.Cmd) == 0 {
		return &InputValidationError{Field: "cmd", Message: "cannot be empty"}
	}
	if strings.TrimSpace(s.Cmd[0]) == "" {
		return &InputValidationError{Field: "cmd[0]", Message: "cannot be blank"}
	}
	return validateContainerEnv(s.Env)
}

// ExecResult is what one command did.
//
// A NON-ZERO ExitCode IS NOT AN ERROR. The command ran and answered; whether
// its answer is a failure is the caller's judgement, and callers routinely
// want the non-zero one — "git rev-parse --verify" answers a question by its
// exit status. An error from Exec means the command could not be run at all.
type ExecResult struct {
	// ExitCode is the command's exit status.
	ExitCode int `json:"exit_code"`

	// Stdout is everything the command wrote to standard output.
	Stdout string `json:"stdout,omitempty"`

	// Stderr is everything it wrote to standard error.
	//
	// Like CommandError.Output this is evidence, not a message: a build step
	// that echoes a token before dying puts that token here. Nothing renders
	// it into an error string; a caller with somewhere safe to put it asks
	// for it by name.
	Stderr string `json:"stderr,omitempty"`
}

// Succeeded reports whether the command exited zero.
func (r *ExecResult) Succeeded() bool { return r != nil && r.ExitCode == 0 }

// Output is the command's stdout with surrounding whitespace removed, which
// is what a caller reading a one-line answer (a SHA, a count) wants.
func (r *ExecResult) Output() string {
	if r == nil {
		return ""
	}
	return strings.TrimSpace(r.Stdout)
}

// Exec runs one command inside a container that is already running.
//
// It shells out to "<runtime> exec". The command's exit status is REPORTED,
// not raised: a returned error means the command could not be run — no such
// container, the runtime refused, the context ended — and a returned result
// means it ran, whatever it exited with.
func (r *ExecContainerRuntime) Exec(ctx context.Context, containerID string, spec *ExecSpec) (*ExecResult, error) {
	if containerID == "" {
		return nil, &InputValidationError{Field: "containerID", Message: "cannot be empty"}
	}
	if err := spec.Validate(); err != nil {
		return nil, err
	}

	// The environment travels in a private 0600 file, never in argv. The exec
	// process reads it while it parses its command line, so the file is
	// released as soon as cmd.Run returns.
	envFile, envErr := writeContainerEnvFile(spec.Env)
	if envErr != nil {
		return nil, envErr
	}
	if envFile.Cleanup != nil {
		defer envFile.Cleanup()
	}

	args := buildExecArgs(containerID, spec, envFile.Path)
	r.logger.Debugf("container exec: %s %s", r.command, redactRunArgs(args))

	cmd := exec.CommandContext(ctx, r.command, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if len(spec.Stdin) > 0 {
		cmd.Stdin = bytes.NewReader(spec.Stdin)
	}

	runErr := cmd.Run()
	result := &ExecResult{Stdout: stdout.String(), Stderr: stderr.String()}
	if runErr == nil {
		return result, nil
	}

	// A cancelled context kills the process, and the ExitError that produces
	// says nothing about the command. Report the cancellation, not a exit
	// status the command never chose.
	if ctxErr := ctx.Err(); ctxErr != nil {
		return nil, &ContainerExecError{ContainerID: containerID, Cause: ctxErr}
	}

	var exitErr *exec.ExitError
	if errors.As(runErr, &exitErr) {
		trimmed := strings.TrimSpace(result.Stderr)
		if isNotFoundOutput(trimmed) {
			return nil, &ContainerNotFoundError{ContainerID: containerID}
		}
		// A negative code means the process was signalled rather than
		// exiting; there is no status to report and it is not the command's
		// answer.
		if code := exitErr.ExitCode(); code >= 0 {
			result.ExitCode = code
			return result, nil
		}
		// The runtime ran and died. Its stderr is the evidence, and it stays
		// on the field rather than in the message.
		return nil, &ContainerExecError{
			ContainerID: containerID,
			Cause:       &CommandError{Command: r.command, Op: "exec", Output: trimmed},
		}
	}

	// THE RUNTIME NEVER STARTED — ENOENT, EACCES, ETXTBSY. There is no stderr
	// to carry and the OS error IS the diagnosis, so it is the cause: an
	// operator gets "no such file or directory" instead of a bare "exec
	// failed", and errors.Is against fs.ErrNotExist works.
	return nil, &ContainerExecError{ContainerID: containerID, Cause: runErr}
}

// buildExecArgs constructs the argument list for a docker/podman exec.
// envFile is referenced by path; no environment VALUE is ever placed in the
// argument vector.
func buildExecArgs(containerID string, spec *ExecSpec, envFile string) []string {
	args := make([]string, 0, 8+len(spec.Cmd))
	args = append(args, "exec")
	if len(spec.Stdin) > 0 {
		args = append(args, "-i")
	}
	if spec.WorkDir != "" {
		args = append(args, "--workdir", spec.WorkDir)
	}
	if envFile != "" {
		args = append(args, envFileFlag, envFile)
	}
	args = append(args, containerID)
	return append(args, spec.Cmd...)
}
