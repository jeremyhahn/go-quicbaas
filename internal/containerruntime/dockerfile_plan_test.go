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
	"context"
	"errors"
	"fmt"
	"maps"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	softwarefactoryworker "github.com/jeremyhahn/go-quicbaas/deployments/agents/software-factory-worker"
)

var errFakeBase = errors.New("fake base: no such image")

// fakeBases serves base image configurations from a map and counts the
// lookups of each reference.
type fakeBases struct {
	configs map[string]imageConfig
	calls   map[string]int
}

func newFakeBases() *fakeBases {
	return &fakeBases{
		configs: map[string]imageConfig{
			"img:1": {Env: []string{"PATH=/usr/bin"}, Cmd: []string{"base"}, Labels: map[string]string{"base": "yes"}},
			"img:2": {Env: []string{"PATH=/bin"}},
		},
		calls: map[string]int{},
	}
}

func (f *fakeBases) lookup(_ context.Context, ref string) (imageConfig, error) {
	f.calls[ref]++
	if cfg, ok := f.configs[ref]; ok {
		return cfg, nil
	}
	return imageConfig{}, fmt.Errorf("%w: %s", errFakeBase, ref)
}

type planCase struct {
	name  string
	src   string
	opts  buildPlanOptions
	check func(t *testing.T, plan *buildPlan, bases *fakeBases)
}

func planSource(t *testing.T, src string, opts buildPlanOptions, bases *fakeBases) (*buildPlan, error) {
	t.Helper()
	instructions, err := parseDockerfile([]byte(src))
	require.NoError(t, err)
	return planDockerfile(context.Background(), instructions, opts, bases.lookup)
}

func runPlanCases(t *testing.T, cases []planCase) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			bases := newFakeBases()
			plan, err := planSource(t, tc.src, tc.opts, bases)
			require.NoError(t, err)
			tc.check(t, plan, bases)
		})
	}
}

func TestPlanDockerfile_Stages(t *testing.T) {
	runPlanCases(t, []planCase{
		{
			name: "global ARG default in FROM",
			src:  "ARG TAG=1\nFROM img:${TAG}\n",
			check: func(t *testing.T, plan *buildPlan, _ *fakeBases) {
				require.Len(t, plan.Stages, 1)
				s := plan.Stages[0]
				assert.Equal(t, "img:1", s.Base)
				assert.Equal(t, -1, s.BaseStage)
				assert.Equal(t, []string{"PATH=/usr/bin"}, s.Config.Env)
				assert.Equal(t, []string{"base"}, s.Config.Cmd)
			},
		},
		{
			name: "build argument overrides the global default",
			src:  "ARG TAG=1\nFROM img:${TAG}\n",
			opts: buildPlanOptions{Args: map[string]string{"TAG": "2"}},
			check: func(t *testing.T, plan *buildPlan, _ *fakeBases) {
				assert.Equal(t, "img:2", plan.Stages[0].Base)
			},
		},
		{
			name: "scratch, an earlier stage by any case, and the base cache",
			src: "FROM img:1 AS Build\nENV A=1\nFROM BUILD AS second\n" +
				"FROM scratch\nFROM img:1\n",
			check: func(t *testing.T, plan *buildPlan, bases *fakeBases) {
				require.Len(t, plan.Stages, 4)
				assert.Equal(t, "build", plan.Stages[0].Name)
				assert.Equal(t, "img:1", plan.Stages[0].Base)

				assert.Equal(t, "second", plan.Stages[1].Name)
				assert.Empty(t, plan.Stages[1].Base)
				assert.Equal(t, 0, plan.Stages[1].BaseStage)
				assert.Equal(t, []string{"PATH=/usr/bin", "A=1"}, plan.Stages[1].Config.Env)

				assert.Empty(t, plan.Stages[2].Base)
				assert.Equal(t, -1, plan.Stages[2].BaseStage)
				assert.Equal(t, imageConfig{}, plan.Stages[2].Config)

				assert.Equal(t, "img:1", plan.Stages[3].Base)
				assert.Equal(t, []string{"PATH=/usr/bin"}, plan.Stages[3].Config.Env)
				assert.Equal(t, 3, plan.Stages[3].Index)
				assert.Equal(t, map[string]int{"img:1": 1}, bases.calls)
			},
		},
		{
			name: "a global ARG reaches a stage only when redeclared",
			src:  "ARG V=g\nFROM img:1\nRUN echo\nARG V\nRUN echo\n",
			check: func(t *testing.T, plan *buildPlan, _ *fakeBases) {
				steps := plan.Stages[0].Steps
				require.Len(t, steps, 2)
				assert.Equal(t, []string{"PATH=/usr/bin"}, steps[0].Env)
				assert.Equal(t, []string{"PATH=/usr/bin", "V=g"}, steps[1].Env)
				assert.Equal(t, []string{"PATH=/usr/bin"}, plan.Stages[0].Config.Env)
			},
		},
		{
			name: "stage ARG: build argument, then its default, then the global",
			src:  "ARG G=global\nFROM img:1\nARG A=own\nARG B=${A}-x\nARG G\nRUN true\n",
			opts: buildPlanOptions{Args: map[string]string{"A": "arg"}},
			check: func(t *testing.T, plan *buildPlan, _ *fakeBases) {
				assert.Equal(t, []string{"PATH=/usr/bin", "A=arg", "B=arg-x", "G=global"},
					plan.Stages[0].Steps[0].Env)
			},
		},
	})
}

func TestPlanDockerfile_Config(t *testing.T) {
	runPlanCases(t, []planCase{
		{
			name: "ENV in both forms, replacing PATH in place",
			src: "FROM img:1\nENV A=1 B=\"two words\"\nENV C legacy value $A\n" +
				"ENV PATH=/opt/bin:${PATH}\n",
			check: func(t *testing.T, plan *buildPlan, _ *fakeBases) {
				assert.Equal(t, []string{"PATH=/opt/bin:/usr/bin", "A=1", "B=two words", "C=legacy value 1"},
					plan.Stages[0].Config.Env)
			},
		},
		{
			name: "ARG expands in ENV but never reaches the config",
			src:  "FROM img:1\nARG V=x\nENV W=${V}\n",
			check: func(t *testing.T, plan *buildPlan, _ *fakeBases) {
				assert.Equal(t, []string{"PATH=/usr/bin", "W=x"}, plan.Stages[0].Config.Env)
			},
		},
		{
			name: "LABEL merges, and the build's labels go on the last stage only",
			src:  "FROM img:1 AS one\nLABEL a=1\nFROM img:1\nLABEL a=2 b=\"x y\"\n",
			opts: buildPlanOptions{Labels: map[string]string{"b": "over", "c": "3"}},
			check: func(t *testing.T, plan *buildPlan, _ *fakeBases) {
				assert.Equal(t, map[string]string{"base": "yes", "a": "1"}, plan.Stages[0].Config.Labels)
				assert.Equal(t, map[string]string{"base": "yes", "a": "2", "b": "over", "c": "3"},
					plan.Stages[1].Config.Labels)
			},
		},
		{
			name: "USER and relative WORKDIR",
			src:  "FROM img:1\nARG U=app\nUSER ${U}\nWORKDIR src\nWORKDIR ../work/./x\n",
			check: func(t *testing.T, plan *buildPlan, _ *fakeBases) {
				assert.Equal(t, "app", plan.Stages[0].Config.User)
				assert.Equal(t, "/work/x", plan.Stages[0].Config.WorkingDir)
			},
		},
		{
			name: "SHELL feeds shell forms and a stage built on it, not an image base",
			src: "FROM img:1 AS a\nSHELL [\"/bin/bash\", \"-ec\"]\nCMD echo $HOME\n" +
				"FROM a\nRUN true\nFROM img:1\nCMD echo hi\n",
			check: func(t *testing.T, plan *buildPlan, _ *fakeBases) {
				assert.Equal(t, []string{"/bin/bash", "-ec", "echo $HOME"}, plan.Stages[0].Config.Cmd)
				assert.Equal(t, []string{"/bin/bash", "-ec", "true"}, plan.Stages[1].Steps[0].Argv)
				assert.Equal(t, []string{"/bin/sh", "-c", "echo hi"}, plan.Stages[2].Config.Cmd)
			},
		},
		{
			name: "ENTRYPOINT clears an inherited Cmd",
			src:  "FROM img:1\nENTRYPOINT [\"run\", \"$X\"]\n",
			check: func(t *testing.T, plan *buildPlan, _ *fakeBases) {
				assert.Equal(t, []string{"run", "$X"}, plan.Stages[0].Config.Entrypoint)
				assert.Nil(t, plan.Stages[0].Config.Cmd)
			},
		},
		{
			name: "ENTRYPOINT keeps a Cmd the stage set",
			src:  "FROM img:1\nCMD [\"x\"]\nENTRYPOINT run me\n",
			check: func(t *testing.T, plan *buildPlan, _ *fakeBases) {
				assert.Equal(t, []string{"/bin/sh", "-c", "run me"}, plan.Stages[0].Config.Entrypoint)
				assert.Equal(t, []string{"x"}, plan.Stages[0].Config.Cmd)
			},
		},
		{
			name: "an empty JSON ENTRYPOINT is empty, not unset",
			src:  "FROM img:1\nENTRYPOINT []\n",
			check: func(t *testing.T, plan *buildPlan, _ *fakeBases) {
				assert.NotNil(t, plan.Stages[0].Config.Entrypoint)
				assert.Empty(t, plan.Stages[0].Config.Entrypoint)
			},
		},
	})
}

func TestPlanDockerfile_Steps(t *testing.T) {
	runPlanCases(t, []planCase{
		{
			name: "RUN in JSON and shell form, unexpanded",
			src:  "FROM img:1\nENV A=1\nRUN [\"echo\", \"$A\"]\nRUN echo $A\n",
			check: func(t *testing.T, plan *buildPlan, _ *fakeBases) {
				steps := plan.Stages[0].Steps
				require.Len(t, steps, 2)
				assert.Equal(t, "RUN", steps[0].Command)
				assert.Equal(t, 3, steps[0].Line)
				assert.Equal(t, []string{"echo", "$A"}, steps[0].Argv)
				assert.Equal(t, []string{"/bin/sh", "-c", "echo $A"}, steps[1].Argv)
				assert.Equal(t, "/", steps[1].WorkDir)
				assert.Empty(t, steps[1].User)
			},
		},
		{
			name: "RUN of a heredoc runs its body",
			src:  "FROM img:1\nRUN <<'EOF'\necho a\necho $B\nEOF\n",
			check: func(t *testing.T, plan *buildPlan, _ *fakeBases) {
				assert.Equal(t, []string{"/bin/sh", "-c", "echo a\necho $B\n"}, plan.Stages[0].Steps[0].Argv)
			},
		},
		{
			name: "RUN Env lists ARGs after ENV and skips those ENV sets",
			src: "ARG G=g\nFROM img:1\nARG A=1\nARG G\nENV A=env\nARG B\n" +
				"USER u\nWORKDIR /w\nRUN x\n",
			check: func(t *testing.T, plan *buildPlan, _ *fakeBases) {
				step := plan.Stages[0].Steps[0]
				assert.Equal(t, []string{"PATH=/usr/bin", "A=env", "G=g", "B="}, step.Env)
				assert.Equal(t, "u", step.User)
				assert.Equal(t, "/w", step.WorkDir)
			},
		},
		{
			name: "COPY --from by name and by index",
			src: "FROM img:1 AS Build\nFROM img:2\nWORKDIR /app\nARG D=out\n" +
				"COPY --from=build /a /b ${D}/\nCOPY --from=0 /c rel\n",
			check: func(t *testing.T, plan *buildPlan, _ *fakeBases) {
				steps := plan.Stages[1].Steps
				require.Len(t, steps, 2)
				assert.Equal(t, buildStep{Line: 5, Command: "COPY", FromStage: 0,
					Sources: []string{"/a", "/b"}, Dest: "/app/out/"}, steps[0])
				assert.Equal(t, buildStep{Line: 6, Command: "COPY", FromStage: 0,
					Sources: []string{"/c"}, Dest: "/app/rel"}, steps[1])
			},
		},
		{
			name: "heredoc COPY with unquoted and quoted delimiters",
			src: "FROM img:1\nENV N=v\nCOPY <<EOF /etc/x\nval=$N\nEOF\n" +
				"COPY --chmod=0755 --chown=${N}:g <<'EOF' /bin/\nval=$N\nEOF\n" +
				"WORKDIR /w\nCOPY --chmod=644 <<EOF rel\nx\nEOF\n",
			check: func(t *testing.T, plan *buildPlan, _ *fakeBases) {
				steps := plan.Stages[0].Steps
				require.Len(t, steps, 3)
				assert.Equal(t, buildStep{Line: 3, Command: "COPY", FromStage: -1,
					Content: "val=v\n", Dest: "/etc/x"}, steps[0])
				assert.Equal(t, buildStep{Line: 6, Command: "COPY", FromStage: -1,
					Content: "val=$N\n", Dest: "/bin/EOF", Chmod: "0755", Chown: "v:g"}, steps[1])
				assert.Equal(t, "/w/rel", steps[2].Dest)
				assert.Equal(t, "644", steps[2].Chmod)
			},
		},
	})
}

func TestPlanDockerfile_Errors(t *testing.T) {
	cases := []struct {
		name string
		src  string
		line int
		msg  string
	}{
		{"instruction before FROM", "ENV A=1\nFROM img:1\n", 1, "before the first FROM"},
		{"FROM with a flag", "FROM --platform=linux/amd64 img:1\n", 1, "--platform"},
		{"FROM that expands to nothing", "ARG E\nFROM ${E}\n", 2, "no image"},
		{"FROM with extra words", "FROM img:1 img:2\n", 1, "optional AS"},
		{"duplicate stage name", "FROM img:1 AS a\nFROM img:1 AS A\n", 2, "used twice"},
		{"numeric stage name", "FROM img:1 AS 1\n", 1, "number"},
		{"ENV with an empty key", "FROM img:1\nENV =x\n", 2, "no name"},
		{"ENV word with no =", "FROM img:1\nENV A=1 B\n", 2, "no ="},
		{"LABEL with no =", "FROM img:1\nLABEL a\n", 2, "no ="},
		{"SHELL in shell form", "FROM img:1\nSHELL /bin/bash\n", 2, "JSON"},
		{"empty SHELL", "FROM img:1\nSHELL []\n", 2, "JSON"},
		{"RUN --mount", "FROM img:1\nRUN --mount=type=cache,target=/x true\n", 2, "--mount"},
		{"RUN --network", "FROM img:1\nRUN --network=none true\n", 2, "--network"},
		{"RUN heredoc inside a command", "FROM img:1\nRUN cat <<EOF\nx\nEOF\n", 2, "whole script"},
		{"COPY --from an image", "FROM img:1\nCOPY --from=alpine /a /b\n", 2, "earlier stage"},
		{"COPY --from a later stage", "FROM img:1 AS a\nCOPY --from=b /a /b\nFROM img:1 AS b\n", 2, "earlier stage"},
		{"COPY --from the current stage", "FROM img:1 AS a\nCOPY --from=a /a /b\n", 2, "earlier stage"},
		{"COPY --from the current index", "FROM img:1\nCOPY --from=0 /a /b\n", 2, "earlier stage"},
		{"COPY --from with no source", "FROM img:1 AS a\nFROM img:1\nCOPY --from=a /b\n", 3, "source"},
		{"COPY --from with a heredoc", "FROM img:1 AS a\nFROM img:1\nCOPY --from=a <<EOF /x\ny\nEOF\n", 3, "heredoc"},
		{"COPY of two heredocs", "FROM img:1\nCOPY <<A <<B /x\na\nA\nb\nB\n", 2, "exactly one heredoc"},
		{"COPY from the build context", "FROM img:1\nCOPY a /b\n", 2, "the build context is empty"},
		{"COPY --link", "FROM img:1\nCOPY --link=true <<EOF /x\ny\nEOF\n", 2, "--link"},
		{"COPY --chmod not octal", "FROM img:1\nCOPY --chmod=0758 <<EOF /x\ny\nEOF\n", 2, "octal"},
		{"COPY --chmod too short", "FROM img:1\nCOPY --chmod=75 <<EOF /x\ny\nEOF\n", 2, "octal"},
		{"COPY --chmod too long", "FROM img:1\nCOPY --chmod=17777 <<EOF /x\ny\nEOF\n", 2, "octal"},
		{"no FROM", "ARG A=1\n", 0, "no FROM"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			plan, err := planSource(t, tc.src, buildPlanOptions{}, newFakeBases())
			require.Error(t, err)
			assert.Nil(t, plan)
			var dfErr *DockerfileError
			require.ErrorAs(t, err, &dfErr)
			assert.Equal(t, tc.line, dfErr.Line)
			assert.Contains(t, dfErr.Message, tc.msg)
		})
	}

	t.Run("base lookup error", func(t *testing.T) {
		_, err := planSource(t, "FROM img:1\nFROM missing:1\n", buildPlanOptions{}, newFakeBases())
		require.ErrorIs(t, err, errFakeBase)
		assert.Contains(t, err.Error(), "line 2")
	})
}

func TestPlanDockerfile_UnusedArgs(t *testing.T) {
	src := "ARG USED\nFROM img:1\nARG INSTAGE=d\nLABEL a=1\nRUN echo\n"
	instructions, err := parseDockerfile([]byte(src))
	require.NoError(t, err)
	before, err := parseDockerfile([]byte(src))
	require.NoError(t, err)
	opts := buildPlanOptions{
		Args:   map[string]string{"USED": "1", "b": "2", "a": "3", "INSTAGE": "x"},
		Labels: map[string]string{"l": "v"},
	}
	argsBefore, labelsBefore := maps.Clone(opts.Args), maps.Clone(opts.Labels)
	bases := newFakeBases()
	baseEnvBefore := append([]string(nil), bases.configs["img:1"].Env...)

	plan, err := planDockerfile(context.Background(), instructions, opts, bases.lookup)
	require.NoError(t, err)
	assert.Equal(t, []string{"a", "b"}, plan.UnusedArgs)

	// Planning mutates neither its instructions nor its options, nor the
	// configuration the base lookup returned.
	plan.Stages[0].Config.Env[0] = "changed=1"
	plan.Stages[0].Config.Labels["base"] = "changed"
	assert.Equal(t, before, instructions)
	assert.Equal(t, argsBefore, opts.Args)
	assert.Equal(t, labelsBefore, opts.Labels)
	assert.Equal(t, baseEnvBefore, bases.configs["img:1"].Env)
	assert.Equal(t, "yes", bases.configs["img:1"].Labels["base"])
}

func TestPlanDockerfile_WorkerImage(t *testing.T) {
	pins, err := softwarefactoryworker.ToolchainPins()
	require.NoError(t, err)
	build := softwarefactoryworker.Build{
		Base:   "0.0.0-test",
		Agents: softwarefactoryworker.Agents{ClaudeCode: "2.1.0", Codex: "0.40.0", Gemini: "0.10.0"},
		Pins:   pins,
		UID:    1001,
		GID:    1001,
	}
	instructions, err := parseDockerfile(softwarefactoryworker.Dockerfile)
	require.NoError(t, err)

	const basePath = "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"
	toolchainRef := "golang:" + pins.Go + "-trixie"
	bases := &fakeBases{
		configs: map[string]imageConfig{
			toolchainRef:         {Env: []string{"PATH=" + basePath}},
			"debian:trixie-slim": {Env: []string{"PATH=" + basePath}},
		},
		calls: map[string]int{},
	}
	plan, err := planDockerfile(context.Background(), instructions,
		buildPlanOptions{Args: build.BuildArgs()}, bases.lookup)
	require.NoError(t, err)

	require.Len(t, plan.Stages, 2)
	assert.Equal(t, "toolchain", plan.Stages[0].Name)
	assert.Equal(t, toolchainRef, plan.Stages[0].Base)
	assert.Equal(t, "debian:trixie-slim", plan.Stages[1].Base)
	assert.Equal(t, map[string]int{toolchainRef: 1, "debian:trixie-slim": 1}, bases.calls)

	var stageCopies, heredocCopies []buildStep
	for _, step := range plan.Stages[1].Steps {
		if step.Command != "COPY" {
			continue
		}
		if step.FromStage == 0 {
			stageCopies = append(stageCopies, step)
		} else if step.FromStage == -1 {
			heredocCopies = append(heredocCopies, step)
		}
	}
	require.Len(t, stageCopies, 2)
	require.Len(t, heredocCopies, 2)

	var devcache *buildStep
	for i := range heredocCopies {
		if heredocCopies[i].Chmod == "0755" {
			devcache = &heredocCopies[i]
		}
	}
	require.NotNil(t, devcache)
	assert.Equal(t, "/usr/local/bin/devcache-env", devcache.Dest)
	assert.Contains(t, devcache.Content, "$", "a quoted delimiter keeps the body unexpanded")

	// ${GOPATH}/bin/ expands to an absolute path and keeps its trailing "/".
	assert.Equal(t, "/home/agent/go/bin/", stageCopies[1].Dest)

	cfg := plan.Stages[1].Config
	assert.Equal(t, "1001:1001", cfg.User)
	assert.Equal(t, "/workspace", cfg.WorkingDir)
	assert.Equal(t, []string{"claude"}, cfg.Cmd)
	pathValue, ok := envValue(cfg.Env, "PATH")
	require.True(t, ok)
	assert.True(t, strings.HasPrefix(pathValue, "/usr/local/go/bin:/home/agent/go/bin:"), pathValue)
	assert.True(t, strings.HasSuffix(pathValue, basePath), pathValue)
	assert.NotEmpty(t, cfg.Labels["org.opencontainers.image.title"])
	assert.Equal(t, pins.Go, cfg.Labels["io.quicbaas.worker.go.version"])

	assert.Empty(t, plan.UnusedArgs)
}
