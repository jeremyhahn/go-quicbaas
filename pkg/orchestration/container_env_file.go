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
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// =============================================================================
// Container environment transport.
//
// A container's environment carries the session's provider credentials: the
// pooled account's API key / OAuth access token (see resolveAccount) and the
// agent's MCP connection material. Two host-local disclosure channels have to
// be closed for those values, and both of them are the command line:
//
//   - argv. "docker run -e KEY=VALUE" publishes VALUE in
//     /proc/<pid>/cmdline, which is world readable on a default Linux host
//     (hidepid=0) and is what ps(1) prints. Any local unprivileged user could
//     harvest every tenant's credentials for the lifetime of the run process.
//   - logs. The same vector was formatted into a log line, so the credential
//     also landed in the daemon's log sink.
//
// The environment is therefore handed to the runtime through a private
// --env-file (0600, inside a 0700 per-launch directory) and the argument
// vector is rendered for logs by redactRunArgs, which never prints a value.
//
// Why --env-file and not the by-reference "-e KEY" form (which makes the
// runtime CLI read KEY from its own environment, where /proc/<pid>/environ is
// owner-only): the by-reference form ties the container's variable name to a
// variable of the SAME name in the docker/podman process. The sandbox spec
// sets HOME=/tmp for the agent, and docker resolves ~/.docker/config.json —
// and rootless podman its entire runtime state — through HOME. Passing spec
// environment by reference would silently repoint the runtime CLI's own
// configuration. Classifying which variables are "secret" and inlining the
// rest would fail open the moment a new credential variable appeared, so the
// whole environment travels by file, unconditionally.
// =============================================================================

const (
	// envFileDirPrefix names the private per-launch directory that holds the
	// --env-file. os.MkdirTemp creates it 0700.
	envFileDirPrefix = "quicbaas-container-env-*"

	// envFileName is the file inside that directory.
	envFileName = "env"

	// envFileFlag is the docker/podman flag that reads a container's
	// environment from a file. Both runtimes accept it with identical
	// "KEY=VALUE", one-per-line semantics.
	envFileFlag = "--env-file"

	// envFileMode is the permission the env file is created with: readable
	// only by the daemon's own user.
	envFileMode os.FileMode = 0o600
)

// containerEnvFile is a private file holding one container's environment in
// docker/podman --env-file format.
//
// The zero value is the "no environment" case: a spec with no Env needs no
// file, so Path is empty and Cleanup is nil.
type containerEnvFile struct {
	// Path is the file the --env-file flag points at, or "" when the spec
	// carried no environment.
	Path string

	// Cleanup removes the file and its directory. Nil when Path is empty.
	// It is idempotent, so a caller may fire it on an error path and the
	// owner may fire it again at teardown.
	Cleanup func()
}

// writeContainerEnvFile materializes env as a 0600 --env-file inside a fresh
// 0700 directory and returns its path plus the cleanup that removes it.
//
// Keys are emitted in sorted order so a launch is byte-reproducible, matching
// the ordering ContainerConfigDigest measures. An entry that cannot be
// represented in the line-oriented file format is rejected rather than
// truncated or inlined — see validateContainerEnv.
func writeContainerEnvFile(env map[string]string) (containerEnvFile, error) {
	if len(env) == 0 {
		return containerEnvFile{}, nil
	}
	if err := validateContainerEnv(env); err != nil {
		return containerEnvFile{}, err
	}

	dir, mkErr := os.MkdirTemp("", envFileDirPrefix)
	if mkErr != nil {
		return containerEnvFile{}, &ContainerEnvFileError{Operation: "mkdir", Cause: mkErr}
	}
	cleanup := func() { _ = os.RemoveAll(dir) }

	path := filepath.Join(dir, envFileName)
	if writeErr := os.WriteFile(path, []byte(renderEnvFile(env)), envFileMode); writeErr != nil {
		cleanup()
		return containerEnvFile{}, &ContainerEnvFileError{Operation: "write", Cause: writeErr}
	}

	return containerEnvFile{Path: path, Cleanup: cleanup}, nil
}

// renderEnvFile encodes env in the docker/podman env-file format: one
// "KEY=VALUE" line per entry, in sorted key order.
func renderEnvFile(env map[string]string) string {
	var b strings.Builder
	for _, k := range sortedEnvKeys(env) {
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(env[k])
		b.WriteByte('\n')
	}
	return b.String()
}

// sortedEnvKeys returns env's keys in ascending order. Go randomizes map
// iteration, so every consumer that must be reproducible (the env file, the
// argument vector, the configuration digest) goes through a sorted key list.
func sortedEnvKeys(env map[string]string) []string {
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// validateContainerEnv checks that every entry can be represented faithfully
// in the env-file format. It is called from ContainerSpec.Validate, so an
// unrepresentable entry fails the launch before any file is written.
func validateContainerEnv(env map[string]string) error {
	for _, k := range sortedEnvKeys(env) {
		if err := validateEnvEntry(k, env[k]); err != nil {
			return err
		}
	}
	return nil
}

// validateEnvEntry rejects a single environment entry the env-file format
// cannot carry.
//
// These are not cosmetic rules. A value containing a newline would be parsed
// as a second "KEY=VALUE" line, which is an environment-injection primitive:
// a credential store (or a caller-supplied env map) could otherwise smuggle
// variables the spec never declared — and the digest never measured — into the
// container. A key containing "=" or whitespace, or one starting with "#",
// would be silently dropped or mis-split by the runtime's parser. Failing
// closed is the only honest option: there is no argv fallback, by design.
func validateEnvEntry(key, value string) error {
	field := "env[" + key + "]"
	switch {
	case key == "":
		return &InputValidationError{Field: "env", Message: "environment variable name cannot be empty"}
	case strings.ContainsRune(key, '='):
		return &InputValidationError{Field: field, Message: "environment variable name cannot contain '='"}
	case strings.HasPrefix(key, "#"):
		return &InputValidationError{
			Field:   field,
			Message: "environment variable name cannot start with '#': the runtime env-file parser would treat it as a comment",
		}
	case containsEnvFileControlChar(key):
		return &InputValidationError{
			Field:   field,
			Message: "environment variable name cannot contain whitespace or control characters",
		}
	case strings.ContainsAny(value, "\n\r"):
		return &InputValidationError{
			Field:   field,
			Message: "environment variable value cannot contain a newline: the runtime env-file format is line-oriented and a second line would inject an undeclared variable",
		}
	case strings.ContainsRune(value, 0):
		return &InputValidationError{Field: field, Message: "environment variable value cannot contain a NUL byte"}
	}
	return nil
}

// containsEnvFileControlChar reports whether s holds any byte the env-file
// parser treats as a separator or cannot round-trip: space, tab, CR, LF, NUL
// or any other ASCII control character.
func containsEnvFileControlChar(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] <= ' ' || s[i] == 0x7f {
			return true
		}
	}
	return false
}

// redactRunArgs renders a docker/podman argument vector for a log line with
// every environment VALUE replaced by envValueRedacted.
//
// appendCommonRunArgs no longer emits an inline "-e KEY=VALUE" pair at all, so
// in the normal case this is a plain join. It stays value-aware regardless:
// this formatter is the second line of defense, and if a future change
// reintroduces an inline pair the log line still cannot publish the secret.
func redactRunArgs(args []string) string {
	out := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case isEnvFlag(arg) && i+1 < len(args):
			out = append(out, arg, redactEnvAssignment(args[i+1]))
			i++
		case strings.HasPrefix(arg, "--env="):
			out = append(out, "--env="+redactEnvAssignment(strings.TrimPrefix(arg, "--env=")))
		default:
			out = append(out, arg)
		}
	}
	return strings.Join(out, " ")
}

// isEnvFlag reports whether arg is a docker/podman inline environment flag
// whose following argument is a KEY[=VALUE] assignment.
func isEnvFlag(arg string) bool {
	return arg == "-e" || arg == "--env"
}

// redactEnvAssignment replaces the value half of a KEY=VALUE assignment with
// envValueRedacted, keeping the key visible so a log line remains diagnosable.
// An assignment with no "=" carries no value and is returned unchanged.
func redactEnvAssignment(assignment string) string {
	key, _, found := strings.Cut(assignment, "=")
	if !found {
		return assignment
	}
	return key + "=" + envValueRedacted
}

// envValueRedacted is the placeholder printed in place of an environment value.
const envValueRedacted = "[redacted]"
