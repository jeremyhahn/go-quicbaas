package softwarefactoryworker

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeKotlinLspAnswers is a kotlin-lsp that speaks LSP over stdio: it logs a
// notification first, as the real server does, then answers initialize and
// shutdown and exits on exit.
const fakeKotlinLspAnswers = `#!/usr/bin/env python3
import json, sys

def read():
    length = None
    while True:
        line = sys.stdin.buffer.readline()
        if not line:
            sys.exit(0)
        if line == b"\r\n":
            break
        name, _, value = line.partition(b":")
        if name.lower() == b"content-length":
            length = int(value)
    return json.loads(sys.stdin.buffer.read(length))

def send(message):
    body = json.dumps(message).encode()
    sys.stdout.buffer.write(b"Content-Length: %d\r\n\r\n" % len(body) + body)
    sys.stdout.buffer.flush()

assert sys.argv[1:] == ["--stdio"], sys.argv
send({"jsonrpc": "2.0", "method": "window/logMessage", "params": {"type": 3, "message": "starting"}})
while True:
    message = read()
    if message.get("method") == "initialize":
        send({"jsonrpc": "2.0", "id": message["id"], "result": {"capabilities": {"hoverProvider": True}}})
    elif message.get("method") == "shutdown":
        send({"jsonrpc": "2.0", "id": message["id"], "result": None})
    elif message.get("method") == "exit":
        sys.exit(0)
`

// runKotlinLspProbe feeds kotlin-lsp-probe.py to python3 with launcher
// installed as kotlin-lsp first on the PATH, under deadlineSeconds.
func runKotlinLspProbe(t *testing.T, launcher, deadlineSeconds string) (string, error) {
	t.Helper()
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "kotlin-lsp"), []byte(launcher), 0o755); err != nil {
		t.Fatalf("write the fake kotlin-lsp: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "python3", "-", deadlineSeconds)
	cmd.Stdin = bytes.NewReader(kotlinLspProbeScript)
	cmd.Env = append(os.Environ(), "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	out, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("the probe outlived its own deadline: %s", out)
	}
	return string(out), err
}

func TestKotlinLspProbe_IsRegisteredOfflineAndSandboxed(t *testing.T) {
	for _, p := range Probes() {
		if p.Script == kotlinLspProbe {
			if !p.Sandboxed || !p.NetworkNone || !bytes.Equal(p.Stdin, kotlinLspProbeScript) {
				t.Fatalf("the Kotlin probe must run kotlin-lsp-probe.py sandboxed and offline, got %+v", p)
			}
			return
		}
	}
	t.Fatal("the Kotlin language server probe is not among Probes()")
}

func TestKotlinLspProbe_PassesAServerThatAnswersInitialize(t *testing.T) {
	out, err := runKotlinLspProbe(t, fakeKotlinLspAnswers, "20")
	if err != nil {
		t.Fatalf("the probe failed a server that answers: %v\n%s", err, out)
	}
	if !strings.Contains(out, "answered initialize with 1 capabilities") {
		t.Fatalf("the probe did not report the server's capabilities: %s", out)
	}
}

func TestKotlinLspProbe_FailsAServerThatExitsOrNeverAnswers(t *testing.T) {
	for _, tc := range []struct {
		name     string
		launcher string
		want     string
	}{
		{"exits", "#!/bin/sh\necho no runtime >&2\nexit 3\n", "exited before it answered initialize"},
		{"never answers", "#!/bin/sh\nwhile read -r line; do :; done\n", "did not answer initialize before the deadline"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, err := runKotlinLspProbe(t, tc.launcher, "1")
			var exitErr *exec.ExitError
			if !errors.As(err, &exitErr) || exitErr.ExitCode() != 1 {
				t.Fatalf("want the probe to exit 1, got %v\n%s", err, out)
			}
			if !strings.Contains(out, tc.want) {
				t.Fatalf("want %q in the probe's output, got:\n%s", tc.want, out)
			}
		})
	}
}
