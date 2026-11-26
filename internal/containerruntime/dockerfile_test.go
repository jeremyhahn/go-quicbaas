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

package containerruntime

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	softwarefactoryworker "github.com/jeremyhahn/go-quicbaas/deployments/agents/software-factory-worker"
)

func TestParseDockerfile(t *testing.T) {
	src := "# syntax=docker/dockerfile:1\n" +
		"ARG BASE=debian:trixie-slim\n" +
		"FROM --platform=linux/amd64 ${BASE} AS build\n" +
		"# a comment\n" +
		"RUN apt-get update \\\n" +
		"    # dropped inside a continuation\n" +
		"\n" +
		"    && apt-get install -y jq\n" +
		"COPY --chmod=0755 <<'SCRIPT' /usr/local/bin/hello\n" +
		"#!/bin/sh\n" +
		"echo \"${NAME:-world}\"\n" +
		"SCRIPT\n" +
		"COPY --from=build /out/ /usr/local/bin/\n" +
		"CMD [\"claude\", \"--help\"]\n" +
		"ENTRYPOINT not json\n"

	got, err := parseDockerfile([]byte(src))
	require.NoError(t, err)
	require.Len(t, got, 7)

	assert.Equal(t, dockerfileInstruction{Command: "ARG", Args: []string{"BASE=debian:trixie-slim"}, Line: 2}, got[0])

	assert.Equal(t, "FROM", got[1].Command)
	platform, ok := got[1].flag("platform")
	assert.True(t, ok)
	assert.Equal(t, "linux/amd64", platform)
	assert.Equal(t, []string{"${BASE} AS build"}, got[1].Args)

	assert.Equal(t, []string{"apt-get update     && apt-get install -y jq"}, got[2].Args)
	assert.Equal(t, 5, got[2].Line)

	assert.Equal(t, "COPY", got[3].Command)
	assert.Equal(t, []dockerfileHeredoc{{
		Name: "SCRIPT", Body: "#!/bin/sh\necho \"${NAME:-world}\"\n", Expand: false,
	}}, got[3].Heredocs)
	assert.Equal(t, 9, got[3].Line)

	from, _ := got[4].flag("from")
	assert.Equal(t, "build", from)
	assert.Equal(t, []string{"/out/ /usr/local/bin/"}, got[4].Args)

	assert.True(t, got[5].JSON)
	assert.Equal(t, []string{"claude", "--help"}, got[5].Args)
	assert.False(t, got[6].JSON)
}

func TestParseDockerfile_Heredocs(t *testing.T) {
	src := "FROM scratch\n" +
		"COPY <<EOF /a\n" +
		"$HOME\n" +
		"EOF\n" +
		"RUN <<-\"END\" sh\n" +
		"\t\techo hi\n" +
		"\tEND\n"
	got, err := parseDockerfile([]byte(src))
	require.NoError(t, err)
	require.Len(t, got, 3)
	assert.Equal(t, []dockerfileHeredoc{{Name: "EOF", Body: "$HOME\n", Expand: true}}, got[1].Heredocs)
	assert.Equal(t, []dockerfileHeredoc{{Name: "END", Body: "echo hi\n", Expand: false}}, got[2].Heredocs)
}

func TestParseDockerfile_Errors(t *testing.T) {
	for name, src := range map[string]string{
		"Empty":          "# only a comment\n",
		"Unsupported":    "FROM scratch\nADD https://example.com/x /x\n",
		"Unknown":        "FROM scratch\nFROBNICATE\n",
		"NoArgs":         "FROM scratch\nUSER\n",
		"FlagNeedsValue": "FROM scratch\nCOPY --link /a /b\n",
		"OpenHeredoc":    "FROM scratch\nCOPY <<EOF /a\nbody\n",
		"BadQuotes":      "FROM scratch\nCOPY <<'EOF /a\nbody\nEOF\n",
		"Escape":         "# escape=`\nFROM scratch\n",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := parseDockerfile([]byte(src))
			var dfErr *DockerfileError
			require.ErrorAs(t, err, &dfErr)
		})
	}
}

// The worker image's Dockerfile is what the in-process build exists for:
// every instruction in it parses.
func TestParseDockerfile_WorkerImage(t *testing.T) {
	got, err := parseDockerfile(softwarefactoryworker.Dockerfile)
	require.NoError(t, err)

	var froms, heredocCopies, stageCopies int
	for _, instruction := range got {
		switch instruction.Command {
		case "FROM":
			froms++
		case "COPY":
			if _, ok := instruction.flag("from"); ok {
				stageCopies++
			}
			if len(instruction.Heredocs) > 0 {
				heredocCopies++
			}
		}
	}
	assert.Equal(t, 2, froms)
	assert.Equal(t, 2, stageCopies)
	assert.Equal(t, 2, heredocCopies)
	last := got[len(got)-1]
	assert.Equal(t, "LABEL", last.Command)
}

func TestExpandWords(t *testing.T) {
	env := map[string]string{"A": "one", "B": "two words", "EMPTY": ""}
	lookup := func(name string) (string, bool) { v, ok := env[name]; return v, ok }

	for in, want := range map[string][]string{
		`$A ${A}`:                       {"one", "one"},
		`"$B" k=$B`:                     {"two words", "k=two words"},
		`'$A' \$A`:                      {"$A", "$A"},
		`${MISSING:-def} ${EMPTY:-def}`: {"def", "def"},
		`${EMPTY-def}x ${A:+set}`:       {"x", "set"},
		`${MISSING:-${A}}`:              {"one"},
		`k="v 1" k2=v\ 2`:               {"k=v 1", "k2=v 2"},
		`"a\"b\n"`:                      {`a"b\n`},
		`$ 1$`:                          {"$", "1$"},
		`${MISSING+set}${A+set}`:        {"set"},
		`/usr/local/go/bin:${PATH:-}/x`: {"/usr/local/go/bin:/x"},
	} {
		got, err := expandWords(in, lookup)
		require.NoError(t, err, in)
		assert.Equal(t, want, got, in)
	}

	word, err := expandWord(`"$B" and $A`, lookup)
	require.NoError(t, err)
	assert.Equal(t, "two words and one", word)

	body, err := expandHeredoc("echo \"$A\" '\\$B'\n", lookup)
	require.NoError(t, err)
	assert.Equal(t, "echo \"one\" '$B'\n", body)

	for _, bad := range []string{`"open`, `${`, `${A`, `${A:-x`, `${A/x/y}`, `${MISSING:?is required}`} {
		_, err := expandWords(bad, lookup)
		assert.Error(t, err, bad)
	}
}
