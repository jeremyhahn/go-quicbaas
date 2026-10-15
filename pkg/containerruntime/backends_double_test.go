// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-trusted-platform.
//
// go-trusted-platform is dual-licensed:
//
// 1. GNU Affero General Public License v3.0 (AGPL-3.0)
//    See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
// 2. Commercial License
//    Contact licensing@automatethethings.com for commercial licensing options.

package containerruntime

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"testing"

	"github.com/stretchr/testify/require"
)

// testImageDigest is the digest the image double reports for every image.
const testImageDigest = "sha256:" +
	"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

// errTestBackend is the failure the doubles return when armed to fail.
var errTestBackend = errors.New("test backend failure")

// fakeResult scripts what a fake process writes and the code it exits with.
type fakeResult struct {
	stdout   string
	stderr   string
	exitCode int
}

// fakeProcess is an in-memory containerProcess. It exits when its script says
// so or when a signal reaches it, with 128+signal as a real process would.
type fakeProcess struct {
	pid     int
	console io.ReadWriteCloser

	// ignoreSIGTERM keeps the process running through SIGTERM, so only
	// SIGKILL stops it.
	ignoreSIGTERM bool

	mu       sync.Mutex
	signals  []os.Signal
	exitCode int
	exitOnce sync.Once
	exited   chan struct{}
}

func newFakeProcess(pid int) *fakeProcess {
	return &fakeProcess{pid: pid, exited: make(chan struct{})}
}

func (p *fakeProcess) exit(code int) {
	p.exitOnce.Do(func() {
		p.mu.Lock()
		p.exitCode = code
		p.mu.Unlock()
		close(p.exited)
	})
}

func (p *fakeProcess) Pid() int { return p.pid }

func (p *fakeProcess) Wait() (int, error) {
	<-p.exited
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.exitCode, nil
}

func (p *fakeProcess) Signal(sig os.Signal) error {
	select {
	case <-p.exited:
		return os.ErrProcessDone
	default:
	}

	p.mu.Lock()
	p.signals = append(p.signals, sig)
	p.mu.Unlock()

	if sig == syscall.SIGTERM && p.ignoreSIGTERM {
		return nil
	}
	if s, ok := sig.(syscall.Signal); ok {
		p.exit(128 + int(s))
	}
	return nil
}

func (p *fakeProcess) Console() io.ReadWriteCloser { return p.console }

func (p *fakeProcess) receivedSignals() []os.Signal {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]os.Signal(nil), p.signals...)
}

// fakeConsole is a terminal that plays back scripted output, then reports
// end of file.
type fakeConsole struct {
	output *bytes.Reader
	input  bytes.Buffer
}

func (c *fakeConsole) Read(p []byte) (int, error)  { return c.output.Read(p) }
func (c *fakeConsole) Write(p []byte) (int, error) { return c.input.Write(p) }
func (c *fakeConsole) Close() error                { return nil }

// silentConsole is a terminal that blocks reads until it is closed.
type silentConsole struct {
	reader *io.PipeReader
	writer *io.PipeWriter
}

func (c *silentConsole) Read(p []byte) (int, error)  { return c.reader.Read(p) }
func (c *silentConsole) Write(p []byte) (int, error) { return len(p), nil }
func (c *silentConsole) Close() error {
	return errors.Join(c.writer.Close(), c.reader.Close())
}

// fakeEngine is an in-memory containerEngine that records what the runtime
// asks of it.
type fakeEngine struct {
	mu sync.Mutex

	createErr  error
	startErr   error
	execErr    error
	destroyErr error

	// initResult, when set, makes the init process write its output and
	// exit at once; otherwise it runs until signalled.
	initResult *fakeResult
	// ignoreSIGTERM makes init processes survive SIGTERM.
	ignoreSIGTERM bool
	// execResult is what every exec process writes and exits with.
	execResult fakeResult
	// execBlocks makes exec processes run, with a silent terminal, until
	// signalled.
	execBlocks bool

	created   map[string]*ociConfig
	destroyed []string
	starts    []processSpec
	execs     []processSpec
	processes []*fakeProcess
}

func newFakeEngine() *fakeEngine {
	return &fakeEngine{created: make(map[string]*ociConfig)}
}

func (e *fakeEngine) Create(id string, config *ociConfig) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.createErr != nil {
		return e.createErr
	}
	if _, ok := e.created[id]; ok {
		return ErrContainerAlreadyExists
	}
	e.created[id] = config
	return nil
}

func (e *fakeEngine) Start(id string, spec processSpec) (containerProcess, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.startErr != nil {
		return nil, e.startErr
	}
	if _, ok := e.created[id]; !ok {
		return nil, ErrContainerNotFound
	}
	e.starts = append(e.starts, spec)

	process := newFakeProcess(1000 + len(e.processes))
	process.ignoreSIGTERM = e.ignoreSIGTERM
	e.processes = append(e.processes, process)
	if e.initResult != nil {
		e.play(spec, process, *e.initResult)
	}
	return process, nil
}

func (e *fakeEngine) Exec(id string, spec processSpec) (containerProcess, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.execErr != nil {
		return nil, e.execErr
	}
	if _, ok := e.created[id]; !ok {
		return nil, ErrContainerNotFound
	}
	e.execs = append(e.execs, spec)

	process := newFakeProcess(2000 + len(e.execs))
	if e.execBlocks {
		reader, writer := io.Pipe()
		process.console = &silentConsole{reader: reader, writer: writer}
		return process, nil
	}
	e.play(spec, process, e.execResult)
	return process, nil
}

// play writes result to the process's terminal or output streams and exits
// the process with the scripted code.
func (e *fakeEngine) play(spec processSpec, process *fakeProcess, result fakeResult) {
	if spec.TTY {
		process.console = &fakeConsole{output: bytes.NewReader([]byte(result.stdout))}
	} else {
		writeScripted(spec.Stdout, result.stdout)
		writeScripted(spec.Stderr, result.stderr)
	}
	process.exit(result.exitCode)
}

func writeScripted(w io.Writer, s string) {
	if w == nil || s == "" {
		return
	}
	if _, err := io.WriteString(w, s); err != nil {
		panic(err)
	}
}

func (e *fakeEngine) Destroy(id string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.destroyErr != nil {
		return e.destroyErr
	}
	delete(e.created, id)
	e.destroyed = append(e.destroyed, id)
	return nil
}

func (e *fakeEngine) lastProcess() *fakeProcess {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.processes[len(e.processes)-1]
}

func (e *fakeEngine) lastStart() processSpec {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.starts[len(e.starts)-1]
}

func (e *fakeEngine) lastExec() processSpec {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.execs[len(e.execs)-1]
}

// fakeImages is an in-memory imageStore holding images by reference.
type fakeImages struct {
	mu sync.Mutex

	pullErr error
	// pullDigests, when set, are the digests a pulled image reports.
	pullDigests []string

	images map[string]imageRecord
	pulls  []string
}

func newFakeImages(refs ...string) *fakeImages {
	images := &fakeImages{images: make(map[string]imageRecord)}
	for _, ref := range refs {
		images.images[ref] = testImageRecord(ref, []string{testImageDigest})
	}
	return images
}

func testImageRecord(ref string, digests []string) imageRecord {
	return imageRecord{ID: "image-" + ref, Names: []string{ref}, Digests: digests, Size: 1024}
}

func (s *fakeImages) Pull(ctx context.Context, ref string) (imageRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pulls = append(s.pulls, ref)
	if s.pullErr != nil {
		return imageRecord{}, s.pullErr
	}
	digests := s.pullDigests
	if digests == nil {
		digests = []string{testImageDigest}
	}
	record := testImageRecord(ref, digests)
	s.images[ref] = record
	return record, nil
}

func (s *fakeImages) Inspect(ctx context.Context, ref string) (imageRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.images[ref]
	if !ok {
		return imageRecord{}, ErrImageNotFound
	}
	return record, nil
}

func (s *fakeImages) Remove(ctx context.Context, ref string, force bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.images[ref]; !ok {
		if force {
			return nil
		}
		return ErrImageNotFound
	}
	delete(s.images, ref)
	return nil
}

// fakeRootfs is a rootfsStore that makes each container's rootfs a directory
// under a test temporary directory.
type fakeRootfs struct {
	root string

	mu        sync.Mutex
	createErr error
	removeErr error
	removed   []string
}

func (s *fakeRootfs) Create(ctx context.Context, containerID, imageID string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.createErr != nil {
		return "", s.createErr
	}
	dir := filepath.Join(s.root, containerID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return dir, nil
}

func (s *fakeRootfs) Remove(containerID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.removeErr != nil {
		return s.removeErr
	}
	s.removed = append(s.removed, containerID)
	return os.RemoveAll(filepath.Join(s.root, containerID))
}

// testBackends are the doubles behind a test runtime.
type testBackends struct {
	engine *fakeEngine
	images *fakeImages
	rootfs *fakeRootfs
}

// newTestRuntime builds a Runtime rooted at a temporary directory over
// in-memory backends that hold "alpine:latest".
func newTestRuntime(t *testing.T) *Runtime {
	t.Helper()
	rt, _ := newTestRuntimeWithBackends(t)
	return rt
}

// newTestRuntimeWithBackends is newTestRuntime, also returning the doubles so
// a test can script and inspect them.
func newTestRuntimeWithBackends(t *testing.T) (*Runtime, *testBackends) {
	t.Helper()

	dir := t.TempDir()
	doubles := &testBackends{
		engine: newFakeEngine(),
		images: newFakeImages("alpine:latest"),
		rootfs: &fakeRootfs{root: filepath.Join(dir, "rootfs")},
	}
	config := &RuntimeConfig{
		Root:            filepath.Join(dir, "root"),
		RunRoot:         filepath.Join(dir, "run"),
		TmpDir:          filepath.Join(dir, "tmp"),
		GraphDriverName: "vfs",
		StaticDir:       filepath.Join(dir, "static"),
		VolumePath:      filepath.Join(dir, "volumes"),
		ImageStorePath:  filepath.Join(dir, "images"),
	}

	rt, err := NewRuntime(context.Background(), config, withBackends(&backends{
		engine: doubles.engine,
		images: doubles.images,
		rootfs: doubles.rootfs,
	}))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, rt.Close()) })
	return rt, doubles
}
