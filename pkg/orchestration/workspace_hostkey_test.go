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
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
)

// testHostKeyLine generates a fresh ed25519 public key and renders it as the
// OpenSSH public-key line a binding pins. It is GENERATED rather than a
// constant so no key material of any provenance is committed and two tests
// never accidentally share a key.
func testHostKeyLine(t *testing.T) string {
	t.Helper()
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generating a host key: %v", err)
	}
	key, err := ssh.NewPublicKey(pub)
	if err != nil {
		t.Fatalf("wrapping the host key: %v", err)
	}
	return strings.TrimSpace(string(ssh.MarshalAuthorizedKey(key)))
}

// =============================================================================
// Parsing the pinned line
// =============================================================================

// TestParseSSHHostKey_AcceptsOneOpenSSHPublicKeyLine: the field holds what
// `ssh-keyscan` prints after the host, with or without a trailing comment and
// with or without surrounding whitespace.
func TestParseSSHHostKey_AcceptsOneOpenSSHPublicKeyLine(t *testing.T) {
	t.Parallel()
	line := testHostKeyLine(t)
	for name, input := range map[string]string{
		"bare":          line,
		"comment":       line + " forge.invalid",
		"padded":        "  " + line + "  ",
		"tabs around":   "\t" + line + "\t",
		"trailing crlf": line + "\r\n",
	} {
		t.Run(name, func(t *testing.T) {
			key, err := ParseSSHHostKey(input)
			if err != nil {
				t.Fatalf("a valid host key line was refused: %v", err)
			}
			if key.Type() != ssh.KeyAlgoED25519 {
				t.Fatalf("key type = %q", key.Type())
			}
			if err := ValidateSSHHostKey(input); err != nil {
				t.Fatalf("ValidateSSHHostKey disagrees with ParseSSHHostKey: %v", err)
			}
		})
	}
}

// TestParseSSHHostKey_RefusesAnythingElse: every plausible wrong paste — an
// empty field, a full known_hosts line with the host still on the front, a
// whole known_hosts file, and a typo'd blob — is refused by name and unwraps
// to the sentinel.
func TestParseSSHHostKey_RefusesAnythingElse(t *testing.T) {
	t.Parallel()
	line := testHostKeyLine(t)
	for name, input := range map[string]string{
		"empty":            "",
		"whitespace":       "   \t ",
		"known_hosts line": "forge.invalid " + line,
		"two lines":        line + "\n" + line,
		"not base64":       "ssh-ed25519 this-is-not-base64",
		"no key data":      "ssh-ed25519",
		"unknown type":     "ssh-nonesuch AAAAB3NzaC1yc2E=",
	} {
		t.Run(name, func(t *testing.T) {
			key, err := ParseSSHHostKey(input)
			if err == nil {
				t.Fatalf("%q was accepted as a host key: %v", input, key)
			}
			var typed *SSHHostKeyError
			if !errors.As(err, &typed) || !errors.Is(err, ErrSSHHostKeyInvalid) {
				t.Fatalf("got %T %v, want *SSHHostKeyError", err, err)
			}
			if typed.Field() != "forge.ssh_host_key" {
				t.Fatalf("the error must name the binding field: %q", typed.Field())
			}
			if !strings.Contains(err.Error(), "forge.ssh_host_key") {
				t.Fatalf("error message = %q", err.Error())
			}
		})
	}
}

// =============================================================================
// The host pattern
// =============================================================================

// TestSSHRepoHost_DerivesThePatternSSHLooksUp covers every spelling git
// accepts for an ssh remote, including the bracketed non-default port.
func TestSSHRepoHost_DerivesThePatternSSHLooksUp(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct{ url, want string }{
		"url with port":     {"ssh://git@forge.invalid:2022/o/r.git", "[forge.invalid]:2022"},
		"url default port":  {"ssh://git@forge.invalid/o/r.git", "forge.invalid"},
		"url explicit 22":   {"ssh://git@forge.invalid:22/o/r.git", "forge.invalid"},
		"url no user":       {"ssh://forge.invalid/o/r.git", "forge.invalid"},
		"url mixed case":    {"ssh://git@Forge.INVALID/o/r.git", "forge.invalid"},
		"scp form":          {"git@forge.invalid:o/r.git", "forge.invalid"},
		"scp mixed case":    {"git@Forge.Invalid:o/r.git", "forge.invalid"},
		"ipv6 with port":    {"ssh://git@[fd00::1]:2022/o/r.git", "[fd00::1]:2022"},
		"https":             {"https://forge.invalid/o/r.git", ""},
		"file":              {"file:///srv/r.git", ""},
		"bare path":         {"/srv/r.git", ""},
		"empty":             {"", ""},
		"scp without colon": {"git@forge.invalid", ""},
		"scp empty path":    {"git@forge.invalid:", ""},
		"ssh without host":  {"ssh:///o/r.git", ""},
	} {
		t.Run(name, func(t *testing.T) {
			if got := SSHRepoHost(tc.url); got != tc.want {
				t.Fatalf("SSHRepoHost(%q) = %q, want %q", tc.url, got, tc.want)
			}
		})
	}
}

// =============================================================================
// The known_hosts content
// =============================================================================

// TestKnownHostsFor_PinsOneHostToOneKey: the rendered file is exactly one
// line, "<pattern> <type> <base64>", built from the parsed key rather than
// the operator's text.
func TestKnownHostsFor_PinsOneHostToOneKey(t *testing.T) {
	t.Parallel()
	line := testHostKeyLine(t)
	got, err := KnownHostsFor("ssh://git@forge.invalid:2022/o/r.git", line+" pasted-comment")
	if err != nil {
		t.Fatalf("KnownHostsFor: %v", err)
	}
	if !strings.HasSuffix(got, "\n") {
		t.Fatalf("known_hosts must end in a newline: %q", got)
	}
	lines := strings.Split(strings.TrimRight(got, "\n"), "\n")
	if len(lines) != 1 {
		t.Fatalf("known_hosts must hold exactly one entry: %q", got)
	}
	if want := "[forge.invalid]:2022 " + line; lines[0] != want {
		t.Fatalf("known_hosts = %q, want %q", lines[0], want)
	}
	if strings.Contains(got, "pasted-comment") {
		t.Fatal("the comment the operator pasted must not survive into the file ssh obeys")
	}
}

// TestKnownHostsFor_HTTPSNeedsNoneAndSSHNeedsOne is the pair of refusals the
// checkout depends on: an https remote pins nothing and is not made wrong by
// a stray key, and an ssh remote with no pinned key is an error naming the
// binding field rather than a clone that trusts whatever answers.
func TestKnownHostsFor_HTTPSNeedsNoneAndSSHNeedsOne(t *testing.T) {
	t.Parallel()
	for name, repoURL := range map[string]string{
		"https":     "https://forge.invalid/o/r.git",
		"bare path": "/srv/r.git",
	} {
		t.Run(name+" pins nothing", func(t *testing.T) {
			got, err := KnownHostsFor(repoURL, "")
			if err != nil || got != "" {
				t.Fatalf("KnownHostsFor(%q) = %q, %v", repoURL, got, err)
			}
			got, err = KnownHostsFor(repoURL, testHostKeyLine(t))
			if err != nil || got != "" {
				t.Fatalf("a stray key on a non-ssh remote is harmless: %q, %v", got, err)
			}
		})
	}
	for name, hostKey := range map[string]string{
		"missing":     "",
		"unparseable": "ssh-ed25519 not-base64",
	} {
		t.Run("ssh "+name+" is refused", func(t *testing.T) {
			got, err := KnownHostsFor("ssh://git@forge.invalid/o/r.git", hostKey)
			if err == nil {
				t.Fatalf("an ssh remote with a %s host key produced %q", name, got)
			}
			var typed *SSHHostKeyError
			if !errors.As(err, &typed) || !errors.Is(err, ErrSSHHostKeyInvalid) {
				t.Fatalf("got %T %v, want *SSHHostKeyError", err, err)
			}
		})
	}
}

// =============================================================================
// The ssh command
// =============================================================================

// TestWorkspaceSSHCommand_IsStrictAgainstTheMountedFile: the value git runs
// names the mounted known_hosts and refuses an unknown host. accept-new would
// restore trust-on-first-use for a worker holding a live signing agent, so it
// must never appear.
func TestWorkspaceSSHCommand_IsStrictAgainstTheMountedFile(t *testing.T) {
	t.Parallel()
	got := WorkspaceSSHCommand("/run/quicbaas/workspace/known_hosts")
	want := "ssh -o UserKnownHostsFile=/run/quicbaas/workspace/known_hosts -o StrictHostKeyChecking=yes"
	if got != want {
		t.Fatalf("GIT_SSH_COMMAND = %q, want %q", got, want)
	}
	if strings.Contains(got, "accept-new") || strings.Contains(got, "StrictHostKeyChecking=no") {
		t.Fatalf("the ssh command must be strict: %q", got)
	}
}
