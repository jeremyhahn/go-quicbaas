package main

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// exampleInvocations returns the command words of every `quicbaas ...`
// invocation in an Example string: the tokens after "quicbaas" up to the
// first flag, quoted value, placeholder or shell syntax. An invocation may
// start a line or follow a pipe, as in `printf ... | quicbaas ...`. One that
// opens with a global flag (`quicbaas --address ... rebalance start`) is
// skipped: its command words cannot be told apart from flag values.
func exampleInvocations(example string) [][]string {
	var invocations [][]string
	for _, line := range strings.Split(example, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "#") {
			continue
		}
		start := -1
		switch {
		case strings.HasPrefix(line, "quicbaas "):
			start = 0
		case strings.Contains(line, "| quicbaas "):
			start = strings.Index(line, "| quicbaas ") + len("| ")
		}
		if start < 0 {
			continue
		}
		fields := strings.Fields(line[start:])[1:]
		var words []string
		for _, field := range fields {
			if strings.ContainsAny(field[:1], `-"'<$\[{|>`) {
				break
			}
			words = append(words, field)
		}
		if len(words) > 0 {
			invocations = append(invocations, words)
		}
	}
	return invocations
}

// TestExampleInvocations covers the Example line parser itself, so a
// passing tree walk below means the examples resolve, not that the parser
// silently found nothing.
func TestExampleInvocations(t *testing.T) {
	got := exampleInvocations(`  # a comment: quicbaas not a command
  quicbaas iam tenant barrier status --tenant acme
  printf '%s' "$PP" | quicbaas iam tenant barrier init --tenant acme \
    --strategy passphrase
  quicbaas tenant usage tenant-acme <id>`)
	assert.Equal(t, [][]string{
		{"iam", "tenant", "barrier", "status"},
		{"iam", "tenant", "barrier", "init"},
		{"tenant", "usage", "tenant-acme"},
	}, got)

	assert.Empty(t, exampleInvocations(
		"no invocation here\n  quicbaasx foo\n  quicbaas --address localhost:50051 rebalance start"))
}

// TestHelpExamples_ResolveToACommand walks the real command tree and checks
// that every `quicbaas ...` line in every command's help resolves to a
// command, not to a group left holding the unmatched words as arguments. An
// example with a wrong prefix — `quicbaas tenant barrier` for a group
// mounted under `iam tenant` (#228), or `quicbaas scheduler list` for a verb
// under `scheduler job` — fails one of the two. Examples may name another
// command on purpose (a group's help shows its verbs, a login hint points
// at `auth login`), so the resolved command need not be the one showing it.
// A group that runs on its own (`sdlc reviewer <project> <pr>`) may take
// positional arguments, so only a group without a Run of its own is wrong.
func TestHelpExamples_ResolveToACommand(t *testing.T) {
	walkCommands(rootCmd, func(owner *cobra.Command) {
		for _, words := range exampleInvocations(helpText(owner)) {
			example := strings.Join(words, " ")
			resolved, leftover, err := rootCmd.Find(words)
			switch {
			case err != nil:
				t.Errorf("%s: example `quicbaas %s` does not resolve: %v",
					owner.CommandPath(), example, err)
			case resolved.HasSubCommands() && !resolved.Runnable() && len(leftover) > 0:
				t.Errorf("%s: example `quicbaas %s` stops at group %q with %q left over",
					owner.CommandPath(), example, resolved.CommandPath(), leftover)
			}
		}
	})
}

// TestHelpExamples_TenantBarrierResolves pins the per-tenant barrier
// examples to `quicbaas iam tenant barrier ...`: each verb's Example must
// carry at least one invocation, and each must resolve to that verb.
func TestHelpExamples_TenantBarrierResolves(t *testing.T) {
	barrier, _, err := rootCmd.Find([]string{"iam", "tenant", "barrier"})
	require.NoError(t, err)
	require.Equal(t, "quicbaas iam tenant barrier", barrier.CommandPath())

	for _, verb := range []string{"status", "init", "unseal", "unseal-share", "seal"} {
		cmd := findSubcommand(barrier, verb)
		require.NotNil(t, cmd, "`iam tenant barrier %s` is missing", verb)

		invocations := exampleInvocations(helpText(cmd))
		require.NotEmpty(t, invocations, "%s has no example invocation", cmd.CommandPath())
		for _, words := range invocations {
			resolved, _, err := rootCmd.Find(words)
			require.NoError(t, err)
			assert.Equal(t, cmd, resolved, "example `quicbaas %s`", strings.Join(words, " "))
		}
	}
}

// helpText returns the help a command prints that can carry examples: many
// commands write theirs into Long under an "Example:" heading rather than
// into the Example field.
func helpText(cmd *cobra.Command) string {
	return cmd.Long + "\n" + cmd.Example
}

// isSameOrDescendant reports whether cmd is ancestor or one of its
// subcommands.
func isSameOrDescendant(cmd, ancestor *cobra.Command) bool {
	for c := cmd; c != nil; c = c.Parent() {
		if c == ancestor {
			return true
		}
	}
	return false
}
