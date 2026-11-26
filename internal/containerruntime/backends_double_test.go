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
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
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

	// outlivesKill keeps Wait blocked after SIGKILL, as a descendant still
	// holding the process's output pipes does.
	outlivesKill bool

	// released is closed by Release, which closes the process's pipes and
	// cleans up its workload: Wait returns and the output stops.
	released    chan struct{}
	releaseOnce sync.Once
	// writing, when set, is closed once the output writer Release stops
	// has returned.
	writing chan struct{}
	// waitReturned is closed when Wait returns.
	waitReturned     chan struct{}
	waitReturnedOnce sync.Once
	// written counts the bytes the output writer has written.
	written atomic.Int64

	mu       sync.Mutex
	signals  []os.Signal
	exitCode int
	exitOnce sync.Once
	exited   chan struct{}
}

func newFakeProcess(pid int) *fakeProcess {
	return &fakeProcess{
		pid:          pid,
		exited:       make(chan struct{}),
		released:     make(chan struct{}),
		waitReturned: make(chan struct{}),
	}
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
	defer p.waitReturnedOnce.Do(func() { close(p.waitReturned) })
	select {
	case <-p.exited:
	case <-p.released:
		return -1, os.ErrClosed
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.exitCode, nil
}

// Release closes the process's pipes and cleans up its workload: the
// output writer stops, and Wait returns.
func (p *fakeProcess) Release() error {
	p.releaseOnce.Do(func() { close(p.released) })
	if p.writing != nil {
		<-p.writing
	}
	return nil
}

// isReleased reports whether Release was called.
func (p *fakeProcess) isReleased() bool {
	select {
	case <-p.released:
		return true
	default:
		return false
	}
}

// keepWriting writes to w until Release, as a descendant holding the
// process's output pipe does.
func (p *fakeProcess) keepWriting(w io.Writer) {
	p.writing = make(chan struct{})
	go func() {
		defer close(p.writing)
		ticker := time.NewTicker(time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-p.released:
				return
			case <-ticker.C:
				n, _ := w.Write([]byte("x"))
				p.written.Add(int64(n))
			}
		}
	}()
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
	if sig == syscall.SIGKILL && p.outlivesKill {
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
	// onStart, when set, runs as the init process would, given the
	// container's root filesystem, and its result is what the process
	// writes and exits with. It takes precedence over initResult.
	onStart func(spec processSpec, rootfs string) fakeResult
	// ignoreSIGTERM makes init processes survive SIGTERM.
	ignoreSIGTERM bool
	// execResult is what every exec process writes and exits with.
	execResult fakeResult
	// execBlocks makes exec processes run, with a silent terminal, until
	// signalled.
	execBlocks bool
	// execOutlivesKill makes exec processes' Wait outlast SIGKILL.
	execOutlivesKill bool
	// execKeepsWriting makes exec processes outlast SIGKILL and keep
	// writing to their stdout until released.
	execKeepsWriting bool

	created   map[string]*ociConfig
	destroyed []string

	// reclaimErr fails Reclaim; reclaimed records the containers reclaimed.
	reclaimErr error
	reclaimed  []string
	starts     []processSpec
	// startConfigs are the created configurations of the started
	// containers, in the order of starts.
	startConfigs []*ociConfig
	execs        []processSpec
	processes    []*fakeProcess
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
	e.startConfigs = append(e.startConfigs, e.created[id])
	if e.onStart != nil {
		rootfs := ""
		if config := e.created[id]; config != nil && config.Root != nil {
			rootfs = config.Root.Path
		}
		e.play(spec, process, e.onStart(spec, rootfs))
	} else if e.initResult != nil {
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
	process.outlivesKill = e.execOutlivesKill
	if e.execKeepsWriting {
		// lastProcess returns it.
		e.processes = append(e.processes, process)
		process.outlivesKill = true
		process.keepWriting(spec.Stdout)
		return process, nil
	}
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
	if _, ok := e.created[id]; !ok {
		// libcontainerEngine refuses an id it does not hold, including
		// one it already destroyed.
		return ErrContainerNotFound
	}
	delete(e.created, id)
	e.destroyed = append(e.destroyed, id)
	return nil
}

func (e *fakeEngine) Reclaim(id string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.reclaimErr != nil {
		return e.reclaimErr
	}
	e.reclaimed = append(e.reclaimed, id)
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

	// pushErr fails every push; pushDigest is the digest a push reports.
	pushErr    error
	pushDigest string
	// pushes records each pushed reference and the auth it carried.
	pushes []fakePush

	// removeErr fails every removal; removes records each removed
	// reference in order, failed or not.
	removeErr error
	removes   []string
	// tagErr fails every tag.
	tagErr error
}

// fakePush is one push the fake image store took.
type fakePush struct {
	ref  string
	auth *orchestration.RegistryAuth
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
		if id, isID := strings.CutPrefix(ref, "sha256:"); isID {
			for _, candidate := range s.images {
				if candidate.ID != "" && candidate.ID == id {
					return candidate, nil
				}
			}
		}
		return imageRecord{}, ErrImageNotFound
	}
	return record, nil
}

func (s *fakeImages) Remove(ctx context.Context, ref string, force bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.removes = append(s.removes, ref)
	if s.removeErr != nil {
		return s.removeErr
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if _, ok := s.images[ref]; !ok {
		if force {
			return nil
		}
		return ErrImageNotFound
	}
	delete(s.images, ref)
	return nil
}

func (s *fakeImages) Tag(ctx context.Context, source, target string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.tagErr != nil {
		return s.tagErr
	}
	record, ok := s.images[source]
	if !ok {
		id := strings.TrimPrefix(source, "sha256:")
		for _, candidate := range s.images {
			if candidate.ID != "" && candidate.ID == id {
				record, ok = candidate, true
				break
			}
		}
	}
	if !ok {
		return ErrImageNotFound
	}
	s.images[target] = record
	return nil
}

func (s *fakeImages) Push(ctx context.Context, ref string, auth *orchestration.RegistryAuth) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.images[ref]; !ok {
		return "", ErrImageNotFound
	}
	s.pushes = append(s.pushes, fakePush{ref: ref, auth: auth})
	if s.pushErr != nil {
		return "", s.pushErr
	}
	return s.pushDigest, nil
}

// fakeRootfs is a rootfsStore that makes each container's rootfs a directory
// under a test temporary directory.
type fakeRootfs struct {
	root string

	mu        sync.Mutex
	createErr error
	removeErr error
	removed   []string
	// seed, when set, fills each new root filesystem as an image would.
	seed func(dir string) error
	// snapshots are the committed images' filesystems, by image ID: a root
	// filesystem created from one starts as a copy of it.
	snapshots map[string]string
	// created records each Create, as containerID -> imageID.
	created map[string]string
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
	if s.seed != nil {
		if err := s.seed(dir); err != nil {
			return "", err
		}
	}
	if snapshot, ok := s.snapshots[imageID]; ok {
		if err := copyTestTree(snapshot, dir); err != nil {
			return "", err
		}
	}
	if s.created == nil {
		s.created = map[string]string{}
	}
	s.created[containerID] = imageID
	return dir, nil
}

// snapshot copies containerID's root filesystem to a new directory, the
// filesystem of the committed image imageID, and returns it.
func (s *fakeRootfs) snapshot(containerID, imageID string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	src := filepath.Join(s.root, containerID)
	if _, err := os.Lstat(src); err != nil {
		return "", err
	}
	dst := filepath.Join(filepath.Dir(s.root), "snapshots", imageID)
	if err := os.MkdirAll(dst, 0o700); err != nil {
		return "", err
	}
	if err := copyTestTree(src, dst); err != nil {
		return "", err
	}
	if s.snapshots == nil {
		s.snapshots = map[string]string{}
	}
	s.snapshots[imageID] = dst
	return dst, nil
}

// copyTestTree copies the tree under src onto dst, keeping modes,
// modification times and symlinks.
func copyTestTree(src, dst string) error {
	return filepath.WalkDir(src, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		info, err := entry.Info()
		if err != nil {
			return err
		}
		switch {
		case entry.Type()&fs.ModeSymlink != 0:
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}
			if err := os.RemoveAll(target); err != nil {
				return err
			}
			return os.Symlink(link, target)
		case entry.IsDir():
			if err := os.MkdirAll(target, 0o700); err != nil {
				return err
			}
		default:
			data, err := fs.ReadFile(os.DirFS(src), filepath.ToSlash(rel))
			if err != nil {
				return err
			}
			if err := os.WriteFile(target, data, 0o600); err != nil {
				return err
			}
		}
		if err := os.Chmod(target, info.Mode().Perm()); err != nil {
			return err
		}
		return os.Chtimes(target, info.ModTime(), info.ModTime())
	})
}

// fakeCommit is one commit the fake committer took.
type fakeCommit struct {
	containerID string
	config      imageConfig
	createdBy   string
	image       imageRecord
	// snapshot is the committed root filesystem.
	snapshot string
}

// fakeCommitter is a layerCommitter that snapshots the container's root
// filesystem directory and stores the result in the fake image store as
// "commit-<n>".
type fakeCommitter struct {
	images *fakeImages
	rootfs *fakeRootfs

	mu        sync.Mutex
	commitErr error
	commits   []fakeCommit
}

func (c *fakeCommitter) Commit(ctx context.Context, containerID string, config imageConfig, createdBy string) (imageRecord, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.commitErr != nil {
		return imageRecord{}, c.commitErr
	}
	id := fmt.Sprintf("commit-%d", len(c.commits)+1)
	snapshot, err := c.rootfs.snapshot(containerID, id)
	if err != nil {
		return imageRecord{}, err
	}
	record := imageRecord{ID: id, Config: config}
	c.images.mu.Lock()
	c.images.images["sha256:"+id] = record
	c.images.mu.Unlock()
	c.commits = append(c.commits, fakeCommit{
		containerID: containerID, config: config, createdBy: createdBy, image: record, snapshot: snapshot,
	})
	return record, nil
}

// recorded returns the commits taken so far.
func (c *fakeCommitter) recorded() []fakeCommit {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.Clone(c.commits)
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
	commit *fakeCommitter
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
	doubles.commit = &fakeCommitter{images: doubles.images, rootfs: doubles.rootfs}
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
		commit: doubles.commit,
	}))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, rt.Close()) })
	return rt, doubles
}
