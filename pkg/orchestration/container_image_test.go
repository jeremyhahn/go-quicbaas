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
	"context"
	"errors"
	"os"
	"slices"
	"strings"
	"testing"
)

// recordedImageCommand is one invocation a fake engine saw.
type recordedImageCommand struct {
	Args  []string
	Stdin string
}

// fakeImageEngine answers image-lifecycle invocations from a function and
// records every one, so a test states the argv it expects and the output the
// engine gives back. No engine binary is involved.
type fakeImageEngine struct {
	calls  []recordedImageCommand
	answer func(args []string) (imageCommandResult, error)
}

func (f *fakeImageEngine) run(_ context.Context, _ string, cmd imageCommand) (imageCommandResult, error) {
	f.calls = append(f.calls, recordedImageCommand{Args: cmd.Args, Stdin: string(cmd.Stdin)})
	if f.answer == nil {
		return imageCommandResult{}, nil
	}
	return f.answer(cmd.Args)
}

func newImageTestRuntime(t *testing.T, command string, engine *fakeImageEngine) *ExecContainerRuntime {
	t.Helper()
	return &ExecContainerRuntime{command: command, logger: newContainerTestLogger(t), runImageCommand: engine.run}
}

// argAfter returns the argument following flag in args.
func argAfter(args []string, flag string) string {
	i := slices.Index(args, flag)
	if i < 0 || i+1 >= len(args) {
		return ""
	}
	return args[i+1]
}

func TestBuildImage_PassesDockerfileOnStdinAndReadsTheIIDFile(t *testing.T) {
	const imageID = "sha256:" + "ab12cd34ef56ab12cd34ef56ab12cd34ef56ab12cd34ef56ab12cd34ef56ab12"
	engine := &fakeImageEngine{answer: func(args []string) (imageCommandResult, error) {
		if err := os.WriteFile(argAfter(args, "--iidfile"), []byte(imageID+"\n"), 0o600); err != nil {
			return imageCommandResult{}, err
		}
		return imageCommandResult{}, nil
	}}
	rt := newImageTestRuntime(t, RuntimeDocker, engine)

	got, err := rt.BuildImage(t.Context(), &ImageBuildSpec{
		Dockerfile: []byte("FROM alpine\n"),
		Tags:       []string{"worker:1-cc2-cx3"},
		BuildArgs:  map[string]string{"B": "2", "A": "1"},
		Labels:     map[string]string{"io.example.v": "1"},
	})
	if err != nil {
		t.Fatalf("BuildImage: %v", err)
	}
	if got != imageID {
		t.Fatalf("image ID = %q, want %q", got, imageID)
	}
	if len(engine.calls) != 1 {
		t.Fatalf("a build without Pull runs only the build, ran %v", engine.calls)
	}
	call := engine.calls[0]
	if call.Stdin != "FROM alpine\n" {
		t.Fatalf("the Dockerfile must travel on stdin, got %q", call.Stdin)
	}
	joined := strings.Join(call.Args, " ")
	if strings.Contains(joined, "--pull") {
		t.Errorf("argv %q re-resolves bases without Pull", joined)
	}
	for _, want := range []string{"build --file -", "--tag worker:1-cc2-cx3",
		"--build-arg A=1 --build-arg B=2", "--label io.example.v=1"} {
		if !strings.Contains(joined, want) {
			t.Errorf("argv %q lacks %q", joined, want)
		}
	}
}

// writeIIDFile answers a build by writing an image ID to its --iidfile, and
// every other command with success.
func writeIIDFile(args []string) (imageCommandResult, error) {
	if args[0] != "build" {
		return imageCommandResult{}, nil
	}
	const imageID = "sha256:" + "ab12cd34ef56ab12cd34ef56ab12cd34ef56ab12cd34ef56ab12cd34ef56ab12"
	if err := os.WriteFile(argAfter(args, "--iidfile"), []byte(imageID), 0o600); err != nil {
		return imageCommandResult{}, err
	}
	return imageCommandResult{}, nil
}

// TestBuildImage_PullPullsEachBaseIntoTheStoreBeforeBuilding pins the fix for
// "content digest ... not found" on push (#2017): a `build --pull` alone left
// the base layers' blobs out of the engine's image store, so a Pull build must
// `pull` every base first and then build from the store.
func TestBuildImage_PullPullsEachBaseIntoTheStoreBeforeBuilding(t *testing.T) {
	engine := &fakeImageEngine{answer: writeIIDFile}
	rt := newImageTestRuntime(t, RuntimeDocker, engine)
	dockerfile := "# syntax comment\n" +
		"FROM golang:1.27 AS build\n" +
		"RUN true\n" +
		"from build as test\n" +
		"FROM alpine:3.21\n" +
		"COPY --from=build /x /x\n" +
		"FROM scratch\n" +
		"FROM alpine:3.21\n"

	if _, err := rt.BuildImage(t.Context(), &ImageBuildSpec{
		Dockerfile: []byte(dockerfile), Tags: []string{"worker:1"}, Pull: true,
	}); err != nil {
		t.Fatalf("BuildImage: %v", err)
	}
	want := []string{"pull golang:1.27", "pull alpine:3.21", "build"}
	if len(engine.calls) != len(want) {
		t.Fatalf("commands = %v, want %v", engine.calls, want)
	}
	for i, prefix := range want {
		if got := strings.Join(engine.calls[i].Args, " "); !strings.HasPrefix(got, prefix) {
			t.Errorf("command %d = %q, want %q", i, got, prefix)
		}
	}
	if build := strings.Join(engine.calls[2].Args, " "); strings.Contains(build, "--pull") {
		t.Errorf("with every base pulled the build must use the store, got %q", build)
	}
}

// TestBuildImage_PullLeavesUnresolvableBasesToBuildPull checks that a base the
// pull cannot resolve on its own — one spelled with a build argument, or one
// for another platform — keeps --pull on the build.
func TestBuildImage_PullLeavesUnresolvableBasesToBuildPull(t *testing.T) {
	engine := &fakeImageEngine{answer: writeIIDFile}
	rt := newImageTestRuntime(t, RuntimeDocker, engine)
	dockerfile := "ARG BASE=alpine\nFROM $BASE\nFROM --platform=linux/arm64 debian:12\nFROM alpine:3.21\n"

	if _, err := rt.BuildImage(t.Context(), &ImageBuildSpec{
		Dockerfile: []byte(dockerfile), Tags: []string{"worker:1"}, Pull: true,
	}); err != nil {
		t.Fatalf("BuildImage: %v", err)
	}
	if len(engine.calls) != 2 {
		t.Fatalf("commands = %v, want one pull and the build", engine.calls)
	}
	if got := strings.Join(engine.calls[0].Args, " "); got != "pull alpine:3.21" {
		t.Errorf("first command = %q, want the literal base's pull", got)
	}
	if build := strings.Join(engine.calls[1].Args, " "); !strings.Contains(build, "--pull") {
		t.Errorf("a build with unresolved bases must keep --pull, got %q", build)
	}
}

func TestBuildImage_PullFailureStopsBeforeTheBuild(t *testing.T) {
	cases := map[string]func([]string) (imageCommandResult, error){
		"pull exits non-zero": func([]string) (imageCommandResult, error) {
			return imageCommandResult{ExitCode: 1, Stderr: "manifest unknown"}, nil
		},
		"pull cannot run": func([]string) (imageCommandResult, error) {
			return imageCommandResult{}, errors.New("exec failed")
		},
	}
	for name, answer := range cases {
		t.Run(name, func(t *testing.T) {
			engine := &fakeImageEngine{answer: answer}
			rt := newImageTestRuntime(t, RuntimeDocker, engine)
			_, err := rt.BuildImage(t.Context(), &ImageBuildSpec{
				Dockerfile: []byte("FROM alpine:3.21\n"), Tags: []string{"worker:1"}, Pull: true,
			})
			if !errors.Is(err, ErrImageBuild) {
				t.Fatalf("err = %v, want %v", err, ErrImageBuild)
			}
			if len(engine.calls) != 1 || engine.calls[0].Args[0] != "pull" {
				t.Fatalf("a failed pull must not build, ran %v", engine.calls)
			}
		})
	}
}

func TestBuildImage_RefusesAndReportsFailures(t *testing.T) {
	cases := map[string]struct {
		spec   *ImageBuildSpec
		answer func([]string) (imageCommandResult, error)
		want   error
	}{
		"nil spec":         {spec: nil, want: ErrInvalidInput},
		"empty dockerfile": {spec: &ImageBuildSpec{Tags: []string{"t"}}, want: ErrInvalidInput},
		"no tag":           {spec: &ImageBuildSpec{Dockerfile: []byte("FROM a")}, want: ErrInvalidInput},
		"blank tag":        {spec: &ImageBuildSpec{Dockerfile: []byte("FROM a"), Tags: []string{" "}}, want: ErrInvalidInput},
		"engine exits non-zero": {
			spec: &ImageBuildSpec{Dockerfile: []byte("FROM a"), Tags: []string{"t"}},
			answer: func([]string) (imageCommandResult, error) {
				return imageCommandResult{ExitCode: 1, Stderr: "boom"}, nil
			},
			want: ErrImageBuild,
		},
		"engine cannot run": {
			spec:   &ImageBuildSpec{Dockerfile: []byte("FROM a"), Tags: []string{"t"}},
			answer: func([]string) (imageCommandResult, error) { return imageCommandResult{}, errors.New("exec failed") },
			want:   ErrImageBuild,
		},
		"engine writes no id": {
			spec:   &ImageBuildSpec{Dockerfile: []byte("FROM a"), Tags: []string{"t"}},
			answer: func([]string) (imageCommandResult, error) { return imageCommandResult{}, nil },
			want:   ErrImageBuild,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			rt := newImageTestRuntime(t, RuntimeDocker, &fakeImageEngine{answer: tc.answer})
			if _, err := rt.BuildImage(t.Context(), tc.spec); !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestBuildImage_WithholdsEngineOutputFromTheError(t *testing.T) {
	engine := &fakeImageEngine{answer: func([]string) (imageCommandResult, error) {
		return imageCommandResult{ExitCode: 1, Stderr: "token=hunter2"}, nil
	}}
	rt := newImageTestRuntime(t, RuntimeDocker, engine)
	_, err := rt.BuildImage(t.Context(), &ImageBuildSpec{Dockerfile: []byte("FROM a"), Tags: []string{"t"}})
	if err == nil || strings.Contains(err.Error(), "hunter2") {
		t.Fatalf("a build's stderr must never be rendered, got %v", err)
	}
	var cmdErr *CommandError
	if !errors.As(err, &cmdErr) || cmdErr.Output != "token=hunter2" {
		t.Fatalf("the output must stay on CommandError.Output for a caller that asks, got %v", err)
	}
}

func TestInspectImage_ParsesIDLabelsAndDigests(t *testing.T) {
	doc := `[{"Id":"sha256:1111","RepoDigests":["reg:3001/o/w@sha256:2222"],"Config":{"Labels":{"a":"b"}}}]`
	engine := &fakeImageEngine{answer: func([]string) (imageCommandResult, error) {
		return imageCommandResult{Stdout: doc}, nil
	}}
	info, err := newImageTestRuntime(t, RuntimeDocker, engine).InspectImage(t.Context(), "w:1")
	if err != nil {
		t.Fatalf("InspectImage: %v", err)
	}
	if info.ID != "sha256:1111" || info.Labels["a"] != "b" || len(info.RepoDigests) != 1 {
		t.Fatalf("info = %+v", info)
	}
	if got := strings.Join(engine.calls[0].Args, " "); got != "image inspect w:1" {
		t.Fatalf("argv = %q", got)
	}
}

func TestInspectImage_UnlabelledImageHasAnEmptyLabelMap(t *testing.T) {
	engine := &fakeImageEngine{answer: func([]string) (imageCommandResult, error) {
		return imageCommandResult{Stdout: `[{"Id":"sha256:1","Config":{"Labels":null}}]`}, nil
	}}
	info, err := newImageTestRuntime(t, RuntimeDocker, engine).InspectImage(t.Context(), "w:1")
	if err != nil {
		t.Fatalf("InspectImage: %v", err)
	}
	if info.Labels == nil {
		t.Fatal("labels must be a usable empty map, not nil")
	}
}

func TestInspectImage_Errors(t *testing.T) {
	cases := map[string]struct {
		image  string
		answer func([]string) (imageCommandResult, error)
		want   error
	}{
		"empty reference": {image: "", want: ErrInvalidInput},
		"docker missing image": {image: "w:1", answer: func([]string) (imageCommandResult, error) {
			return imageCommandResult{ExitCode: 1, Stderr: "Error: No such image: w:1"}, nil
		}, want: ErrImageNotFound},
		"podman missing image": {image: "w:1", answer: func([]string) (imageCommandResult, error) {
			return imageCommandResult{ExitCode: 125, Stderr: "Error: w:1: image not known"}, nil
		}, want: ErrImageNotFound},
		"other failure": {image: "w:1", answer: func([]string) (imageCommandResult, error) {
			return imageCommandResult{ExitCode: 1, Stderr: "permission denied"}, nil
		}, want: ErrImageInspect},
		"garbage document": {image: "w:1", answer: func([]string) (imageCommandResult, error) {
			return imageCommandResult{Stdout: "not json"}, nil
		}, want: ErrImageInspect},
		"empty document": {image: "w:1", answer: func([]string) (imageCommandResult, error) {
			return imageCommandResult{Stdout: "[]"}, nil
		}, want: ErrImageNotFound},
		"engine cannot run": {image: "w:1", answer: func([]string) (imageCommandResult, error) {
			return imageCommandResult{}, errors.New("exec")
		}, want: ErrImageInspect},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			rt := newImageTestRuntime(t, RuntimeDocker, &fakeImageEngine{answer: tc.answer})
			if _, err := rt.InspectImage(t.Context(), tc.image); !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestTagImage(t *testing.T) {
	engine := &fakeImageEngine{}
	rt := newImageTestRuntime(t, RuntimeDocker, engine)
	if err := rt.TagImage(t.Context(), "sha256:1", "reg/o/w:1"); err != nil {
		t.Fatalf("TagImage: %v", err)
	}
	if got := strings.Join(engine.calls[0].Args, " "); got != "tag sha256:1 reg/o/w:1" {
		t.Fatalf("argv = %q", got)
	}

	for name, args := range map[string][2]string{"no source": {"", "t"}, "no target": {"s", ""}} {
		t.Run(name, func(t *testing.T) {
			if err := rt.TagImage(t.Context(), args[0], args[1]); !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("err = %v", err)
			}
		})
	}
	failing := newImageTestRuntime(t, RuntimeDocker, &fakeImageEngine{answer: func([]string) (imageCommandResult, error) {
		return imageCommandResult{ExitCode: 1}, nil
	}})
	if err := failing.TagImage(t.Context(), "s", "t"); !errors.Is(err, ErrImageTag) {
		t.Fatalf("err = %v, want ErrImageTag", err)
	}
}

const pushTestDigest = "sha256:" + "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func TestPushImage_DockerLogsInPrivatelyAndParsesTheDigest(t *testing.T) {
	engine := &fakeImageEngine{answer: func(args []string) (imageCommandResult, error) {
		if slices.Contains(args, "push") {
			return imageCommandResult{Stdout: "1-cc2-cx3: digest: " + pushTestDigest + " size: 1234\n"}, nil
		}
		return imageCommandResult{}, nil
	}}
	rt := newImageTestRuntime(t, RuntimeDocker, engine)

	digest, err := rt.PushImage(t.Context(), "reg.example:3001/o/w:1-cc2-cx3",
		&RegistryAuth{Username: "agent", Secret: []byte("s3cret")})
	if err != nil {
		t.Fatalf("PushImage: %v", err)
	}
	if digest != pushTestDigest {
		t.Fatalf("digest = %q", digest)
	}
	if len(engine.calls) != 2 {
		t.Fatalf("want login then push, got %d calls", len(engine.calls))
	}
	login, push := engine.calls[0], engine.calls[1]
	if login.Stdin != "s3cret" || slices.Contains(login.Args, "s3cret") {
		t.Fatalf("the secret must travel on stdin and never in argv: %+v", login)
	}
	config := argAfter(login.Args, "--config")
	if config == "" || argAfter(push.Args, "--config") != config {
		t.Fatalf("login and push must share one private --config: login=%v push=%v", login.Args, push.Args)
	}
	if _, statErr := os.Stat(config); !os.IsNotExist(statErr) {
		t.Fatalf("the private configuration %s must be removed after the push", config)
	}
	if login.Args[len(login.Args)-1] != "reg.example:3001" {
		t.Fatalf("login must name the image's registry, argv %v", login.Args)
	}
}

func TestPushImage_PodmanUsesAnAuthfileAndADigestFile(t *testing.T) {
	engine := &fakeImageEngine{answer: func(args []string) (imageCommandResult, error) {
		if args[0] == "push" {
			if err := os.WriteFile(argAfter(args, "--digestfile"), []byte(pushTestDigest), 0o600); err != nil {
				return imageCommandResult{}, err
			}
		}
		return imageCommandResult{}, nil
	}}
	rt := newImageTestRuntime(t, RuntimePodman, engine)

	digest, err := rt.PushImage(t.Context(), "reg.example:3001/o/w:1", &RegistryAuth{Username: "u", Secret: []byte("p")})
	if err != nil {
		t.Fatalf("PushImage: %v", err)
	}
	if digest != pushTestDigest {
		t.Fatalf("digest = %q", digest)
	}
	if engine.calls[0].Args[0] != "login" || argAfter(engine.calls[0].Args, "--authfile") == "" {
		t.Fatalf("podman login argv = %v", engine.calls[0].Args)
	}
}

func TestPushImage_WithoutAuthUsesTheEngineConfiguration(t *testing.T) {
	engine := &fakeImageEngine{answer: func([]string) (imageCommandResult, error) {
		return imageCommandResult{Stdout: "digest: " + pushTestDigest}, nil
	}}
	rt := newImageTestRuntime(t, RuntimeDocker, engine)
	if _, err := rt.PushImage(t.Context(), "reg.example/o/w:1", nil); err != nil {
		t.Fatalf("PushImage: %v", err)
	}
	if len(engine.calls) != 1 || slices.Contains(engine.calls[0].Args, "--config") {
		t.Fatalf("an unauthenticated push is one plain push, got %+v", engine.calls)
	}
}

func TestPushImage_Errors(t *testing.T) {
	loginFails := func(args []string) (imageCommandResult, error) {
		if slices.Contains(args, "login") {
			return imageCommandResult{ExitCode: 1, Stderr: "unauthorized"}, nil
		}
		return imageCommandResult{}, nil
	}
	cases := map[string]struct {
		command string
		image   string
		auth    *RegistryAuth
		answer  func([]string) (imageCommandResult, error)
		want    error
	}{
		"empty image":        {command: RuntimeDocker, image: "", want: ErrInvalidInput},
		"unknown engine":     {command: "nerdctl", image: "r/o/w:1", want: ErrImagePush},
		"half a credential":  {command: RuntimeDocker, image: "r.x/o/w:1", auth: &RegistryAuth{Username: "u"}, want: ErrInvalidInput},
		"login refused":      {command: RuntimeDocker, image: "r.x/o/w:1", auth: &RegistryAuth{Username: "u", Secret: []byte("p")}, answer: loginFails, want: ErrRegistryLogin},
		"push exits nonzero": {command: RuntimeDocker, image: "r.x/o/w:1", answer: func([]string) (imageCommandResult, error) { return imageCommandResult{ExitCode: 1}, nil }, want: ErrImagePush},
		"no digest printed":  {command: RuntimeDocker, image: "r.x/o/w:1", answer: func([]string) (imageCommandResult, error) { return imageCommandResult{Stdout: "pushed"}, nil }, want: ErrImagePush},
		"push cannot run":    {command: RuntimeDocker, image: "r.x/o/w:1", answer: func([]string) (imageCommandResult, error) { return imageCommandResult{}, errors.New("exec") }, want: ErrImagePush},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			rt := newImageTestRuntime(t, tc.command, &fakeImageEngine{answer: tc.answer})
			if _, err := rt.PushImage(t.Context(), tc.image, tc.auth); !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestParsePushDigest_TakesTheLastDigestPrinted(t *testing.T) {
	other := "sha256:" + strings.Repeat("f", 64)
	out := "a: digest: " + other + " size: 1\nb: digest: " + pushTestDigest + " size: 2\n"
	got, err := parsePushDigest(out)
	if err != nil || got != pushTestDigest {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestRegistryHost(t *testing.T) {
	cases := map[string]string{
		"198.51.100.50:3001/jeremyhahn/w:1":            "198.51.100.50:3001",
		"registry.example.com/o/w:1":                   "registry.example.com",
		"localhost/w:1":                                "localhost",
		"quicbaas-software-factory-worker:0.11.0":      "docker.io",
		"library/alpine:3":                             "docker.io",
		"reg.example:3001/o/w:1@" + pushTestDigest:     "reg.example:3001",
		"quicbaas-software-factory-worker@" + testHash: "docker.io",
	}
	for image, want := range cases {
		if got := RegistryHost(image); got != want {
			t.Errorf("RegistryHost(%q) = %q, want %q", image, got, want)
		}
	}
}

const testHash = "sha256:" + "9999999999999999999999999999999999999999999999999999999999999999"

func TestRunImage_BuildsAOneShotRunAndReportsTheExit(t *testing.T) {
	engine := &fakeImageEngine{answer: func([]string) (imageCommandResult, error) {
		return imageCommandResult{ExitCode: 3, Stdout: "out", Stderr: "err"}, nil
	}}
	rt := newImageTestRuntime(t, RuntimeDocker, engine)
	res, err := rt.RunImage(t.Context(), &ImageRunSpec{
		Image:       "sha256:1",
		User:        "1000:1000",
		Env:         map[string]string{"HOME": "/tmp"},
		NetworkNone: true,
		Mounts:      []ContainerMount{{Source: t.TempDir(), Target: "/workspace"}},
		Command:     []string{"sh", "-lec", "true"},
	})
	if err != nil {
		t.Fatalf("RunImage: %v", err)
	}
	if res.ExitCode != 3 || res.Stdout != "out" || res.Stderr != "err" {
		t.Fatalf("result = %+v", res)
	}
	args := engine.calls[0].Args
	joined := strings.Join(args, " ")
	for _, want := range []string{"run --rm", "--user 1000:1000", "--network none", "--env-file", ":/workspace", "sha256:1 sh -lec true"} {
		if !strings.Contains(joined, want) {
			t.Errorf("argv %q lacks %q", joined, want)
		}
	}
	if strings.Contains(joined, "HOME=/tmp") {
		t.Fatalf("the environment must travel in the env file, not argv: %q", joined)
	}
}

// TestRunImage_SandboxedRunUsesTheSpawnsBoundary pins that a sandboxed one-shot
// run carries exactly the flags the spawn renders from the same policy — the
// worker image probes certify that boundary, so a second rendering would let
// them certify one no worker runs with — and that stdin reaches the process.
func TestRunImage_SandboxedRunUsesTheSpawnsBoundary(t *testing.T) {
	engine := &fakeImageEngine{}
	rt := newImageTestRuntime(t, RuntimeDocker, engine)
	policy := SandboxSecurity{PidsLimit: 64}
	_, err := rt.RunImage(t.Context(), &ImageRunSpec{
		Image:    "sha256:1",
		Security: &policy,
		Stdin:    []byte("print('probe')"),
		Command:  []string{"sh", "-lec", "python3 -"},
	})
	if err != nil {
		t.Fatalf("RunImage: %v", err)
	}
	spawn := &ContainerSpec{}
	if err := policy.Apply(spawn); err != nil {
		t.Fatal(err)
	}
	want := strings.Join(spawn.PrivilegeRunArgs(), " ")
	call := engine.calls[0]
	joined := strings.Join(call.Args, " ")
	if !strings.Contains(joined, want+" sha256:1 sh -lec") {
		t.Fatalf("argv %q does not carry the spawn's boundary %q before the image", joined, want)
	}
	if !strings.Contains(joined, "--pids-limit 64") || !strings.Contains(joined, "--cap-drop ALL") {
		t.Fatalf("argv %q lacks the pids bound or the capability drop", joined)
	}
	if !strings.HasPrefix(joined, "run --rm -i ") || call.Stdin != "print('probe')" {
		t.Fatalf("stdin must reach the process through -i: argv %q, stdin %q", joined, call.Stdin)
	}
}

func TestRunImage_UnsandboxedRunCarriesNoBoundaryOrStdin(t *testing.T) {
	engine := &fakeImageEngine{}
	rt := newImageTestRuntime(t, RuntimeDocker, engine)
	if _, err := rt.RunImage(t.Context(), &ImageRunSpec{Image: "i", Command: []string{"true"}}); err != nil {
		t.Fatalf("RunImage: %v", err)
	}
	joined := strings.Join(engine.calls[0].Args, " ")
	if strings.Contains(joined, "--cap-drop") || strings.Contains(joined, "--pids-limit") || strings.Contains(joined, " -i ") {
		t.Fatalf("an unsandboxed run must not carry sandbox flags or -i: %q", joined)
	}
	if engine.calls[0].Stdin != "" {
		t.Fatal("an unsandboxed run with no stdin must feed none")
	}
}

func TestRunImage_Errors(t *testing.T) {
	cases := map[string]struct {
		spec   *ImageRunSpec
		answer func([]string) (imageCommandResult, error)
		want   error
	}{
		"nil spec":    {spec: nil, want: ErrInvalidInput},
		"no image":    {spec: &ImageRunSpec{Command: []string{"true"}}, want: ErrInvalidInput},
		"no command":  {spec: &ImageRunSpec{Image: "i"}, want: ErrInvalidInput},
		"bad mount":   {spec: &ImageRunSpec{Image: "i", Command: []string{"true"}, Mounts: []ContainerMount{{}}}, want: ErrInvalidInput},
		"bad policy":  {spec: &ImageRunSpec{Image: "i", Command: []string{"true"}, Security: &SandboxSecurity{PidsLimit: -1}}, want: ErrInvalidInput},
		"cannot exec": {spec: &ImageRunSpec{Image: "i", Command: []string{"true"}}, answer: func([]string) (imageCommandResult, error) { return imageCommandResult{}, errors.New("exec") }, want: ErrContainerStart},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			rt := newImageTestRuntime(t, RuntimeDocker, &fakeImageEngine{answer: tc.answer})
			if _, err := rt.RunImage(t.Context(), tc.spec); !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
		})
	}
}

// TestRunImage_CancelledRunForceRemovesItsContainer pins that a one-shot run
// cut short after its container was created removes that container by name:
// cancelling kills the engine client, and --rm only fires when the container
// itself exits, which a hung probe never does. The removal runs on a context
// of its own — live, and bounded — because the caller's has already ended.
func TestRunImage_CancelledRunForceRemovesItsContainer(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	type call struct {
		args        []string
		ctxErr      error
		hasDeadline bool
	}
	var calls []call
	rt := &ExecContainerRuntime{command: RuntimeDocker, logger: newContainerTestLogger(t),
		runImageCommand: func(c context.Context, _ string, cmd imageCommand) (imageCommandResult, error) {
			_, hasDeadline := c.Deadline()
			calls = append(calls, call{args: cmd.Args, ctxErr: c.Err(), hasDeadline: hasDeadline})
			if cmd.Args[0] == "run" {
				// The container exists; the probe hangs until the deadline.
				cancel()
				return imageCommandResult{}, context.Canceled
			}
			return imageCommandResult{}, nil
		}}

	_, err := rt.RunImage(ctx, &ImageRunSpec{Image: "sha256:1", Command: []string{"sleep", "infinity"}})
	if !errors.Is(err, ErrContainerStart) || !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want ErrContainerStart wrapping context.Canceled", err)
	}
	if len(calls) != 2 {
		t.Fatalf("engine saw %d calls, want the run then its removal: %+v", len(calls), calls)
	}
	name := argAfter(calls[0].args, "--name")
	if !strings.HasPrefix(name, oneShotContainerPrefix) {
		t.Fatalf("the run was not named: argv %q", calls[0].args)
	}
	removal := calls[1]
	if strings.Join(removal.args, " ") != "rm --force "+name {
		t.Fatalf("removal argv = %q, want rm --force %s", removal.args, name)
	}
	if removal.ctxErr != nil || !removal.hasDeadline {
		t.Fatalf("the removal must run on a live, bounded context: err=%v deadline=%v", removal.ctxErr, removal.hasDeadline)
	}
}

// TestRunImage_CompletedRunIsNotRemovedAgain pins that a run that finished —
// whatever its exit — leaves removal to --rm, and that each run is named
// uniquely.
func TestRunImage_CompletedRunIsNotRemovedAgain(t *testing.T) {
	engine := &fakeImageEngine{answer: func([]string) (imageCommandResult, error) {
		return imageCommandResult{ExitCode: 1}, nil
	}}
	rt := newImageTestRuntime(t, RuntimeDocker, engine)
	for range 2 {
		if _, err := rt.RunImage(t.Context(), &ImageRunSpec{Image: "i", Command: []string{"false"}}); err != nil {
			t.Fatalf("RunImage: %v", err)
		}
	}
	if len(engine.calls) != 2 || engine.calls[0].Args[0] != "run" || engine.calls[1].Args[0] != "run" {
		t.Fatalf("a completed run must not be followed by a removal: %+v", engine.calls)
	}
	if argAfter(engine.calls[0].Args, "--name") == argAfter(engine.calls[1].Args, "--name") {
		t.Fatal("two runs shared a container name")
	}
}

// TestRunImage_FailedRemovalKeepsTheRunsError pins that a removal the engine
// refuses does not replace the run's own failure.
func TestRunImage_FailedRemovalKeepsTheRunsError(t *testing.T) {
	runErr := errors.New("engine client killed")
	engine := &fakeImageEngine{answer: func(args []string) (imageCommandResult, error) {
		if args[0] == "rm" {
			return imageCommandResult{ExitCode: 1, Stderr: "daemon unavailable"}, nil
		}
		return imageCommandResult{}, runErr
	}}
	rt := newImageTestRuntime(t, RuntimeDocker, engine)
	_, err := rt.RunImage(t.Context(), &ImageRunSpec{Image: "i", Command: []string{"true"}})
	if !errors.Is(err, runErr) {
		t.Fatalf("err = %v, want the run's error", err)
	}
	if len(engine.calls) != 2 || engine.calls[1].Args[0] != "rm" {
		t.Fatalf("a failed run must be followed by its removal: %+v", engine.calls)
	}
}
