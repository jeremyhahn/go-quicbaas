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
	"fmt"
	"os"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// tagStoreEngine models an engine's image store the way docker keeps it: a
// reference names one image, `tag` moves a reference, and `image rm <ref>`
// untags the image, deleting it when that reference was its last.
type tagStoreEngine struct {
	mu     sync.Mutex
	builds int
	tags   map[string]string // reference -> image ID
	images map[string]bool

	// after runs once each command has been applied, outside mu.
	after func(args []string)
}

func newTagStoreEngine() *tagStoreEngine {
	return &tagStoreEngine{tags: map[string]string{}, images: map[string]bool{}}
}

func (e *tagStoreEngine) run(_ context.Context, _ string, cmd imageCommand) (imageCommandResult, error) {
	result, err := e.apply(cmd.Args)
	if e.after != nil {
		e.after(cmd.Args)
	}
	return result, err
}

func (e *tagStoreEngine) resolve(ref string) (string, bool) {
	if id, ok := e.tags[ref]; ok {
		return id, true
	}
	return ref, e.images[ref]
}

func (e *tagStoreEngine) apply(args []string) (imageCommandResult, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	noSuch := func(ref string) imageCommandResult {
		return imageCommandResult{ExitCode: 1, Stderr: "Error response from daemon: No such image: " + ref}
	}
	switch {
	case args[0] == "build":
		e.builds++
		id := fmt.Sprintf("sha256:%064x", e.builds)
		e.images[id] = true
		for i, arg := range args {
			if arg == "--tag" {
				e.tags[args[i+1]] = id
			}
		}
		return imageCommandResult{}, os.WriteFile(argAfter(args, "--iidfile"), []byte(id+"\n"), 0o600)
	case args[0] == "tag":
		id, ok := e.resolve(args[1])
		if !ok {
			return noSuch(args[1]), nil
		}
		e.tags[args[2]] = id
	case args[0] == "image" && args[1] == "inspect":
		id, ok := e.resolve(args[2])
		if !ok {
			return noSuch(args[2]), nil
		}
		return imageCommandResult{Stdout: `[{"Id":"` + id + `","Config":{"Labels":null}}]`}, nil
	case args[0] == "image" && args[1] == "rm":
		id, ok := e.tags[args[2]]
		if !ok {
			return noSuch(args[2]), nil
		}
		delete(e.tags, args[2])
		for _, other := range e.tags {
			if other == id {
				return imageCommandResult{Stdout: "Untagged: " + args[2]}, nil
			}
		}
		delete(e.images, id)
	}
	return imageCommandResult{}, nil
}

func (e *tagStoreEngine) state() (builds int, tags map[string]string, images map[string]bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	tags = map[string]string{}
	for k, v := range e.tags {
		tags[k] = v
	}
	images = map[string]bool{}
	for k, v := range e.images {
		images[k] = v
	}
	return e.builds, tags, images
}

// TestBuildImage_ConcurrentBuildOfTheSameTagKeepsTheReturnedImage is the
// overlapping build of one tag: build B's engine build completes while build
// A reads back its ID. A's temporary tag must not be its image's last
// reference when A removes it, or `image rm` deletes the image whose ID A
// returns.
func TestBuildImage_ConcurrentBuildOfTheSameTagKeepsTheReturnedImage(t *testing.T) {
	engine := newTagStoreEngine()
	rt := &ExecContainerRuntime{command: RuntimeDocker, logger: newContainerTestLogger(t), runImageCommand: engine.run}
	spec := func() *ImageBuildSpec {
		return &ImageBuildSpec{Dockerfile: []byte("FROM a"), Tags: []string{"w:local"}}
	}

	var (
		once   sync.Once
		bBuilt = make(chan struct{})
		bDone  = make(chan struct{})
		idB    string
		errB   error
	)
	engine.after = func(args []string) {
		if args[0] == "build" {
			if builds, _, _ := engine.state(); builds == 2 {
				close(bBuilt)
			}
			return
		}
		if args[0] == "image" && args[1] == "inspect" {
			once.Do(func() {
				go func() {
					defer close(bDone)
					idB, errB = rt.BuildImage(context.WithoutCancel(t.Context()), spec())
				}()
				<-bBuilt
			})
		}
	}

	idA, errA := rt.BuildImage(t.Context(), spec())
	<-bDone
	if errA != nil || errB != nil {
		t.Fatalf("BuildImage: A %v, B %v", errA, errB)
	}
	if idA == idB {
		t.Fatalf("the two builds must produce two images, both %q", idA)
	}
	_, tags, images := engine.state()
	if !images[idA] {
		t.Fatalf("build A's image %s was deleted with its temporary tag; tags %v", idA, tags)
	}
	// After A leaves once.Do both builds apply w:local in either order: the
	// tag names whichever wrote last, and the other build keeps its image by
	// its ID reference.
	winner, loser := idB, idA
	if tags["w:local"] == idA {
		winner, loser = idA, idB
	}
	if tags["w:local"] != winner || !images[idA] || !images[idB] {
		t.Fatalf("w:local must name one of the two builds and both images survive, tags %v, images %v", tags, images)
	}
	if len(idRefsOf(tags, idA)) != 1 || len(idRefsOf(tags, idB)) != 1 {
		t.Fatalf("each returned image must keep its ID reference while its caller uses it, tags %v", tags)
	}
	for ref := range tags {
		if strings.HasPrefix(ref, buildTempRepository+":") {
			t.Errorf("temporary tag %s was left behind", ref)
		}
	}
	// The callers' use of the returned IDs.
	for _, id := range []string{idA, idB} {
		if _, err := rt.InspectImage(t.Context(), id); err != nil {
			t.Fatalf("InspectImage of returned ID %s: %v", id, err)
		}
	}
	if err := rt.TagImage(t.Context(), loser, "localhost:32786/it/w:a"); err != nil {
		t.Fatalf("TagImage of the overwritten build's ID: %v", err)
	}

	// The callers are done: the push tag goes, and each releases its ID, so
	// neither superseded image is retained.
	if _, err := engine.apply([]string{"image", "rm", "localhost:32786/it/w:a"}); err != nil {
		t.Fatal(err)
	}
	rt.ReleaseImage(t.Context(), idA)
	rt.ReleaseImage(t.Context(), idB)
	engine.after = nil
	idNext, err := rt.BuildImage(t.Context(), spec())
	if err != nil {
		t.Fatalf("next BuildImage: %v", err)
	}
	// Moving w:local leaves the earlier image dangling, as the engine does:
	// no reference retains either, so dangling-image pruning reclaims them.
	_, tags, _ = engine.state()
	for ref, id := range tags {
		if id == idA || id == idB {
			t.Fatalf("%s still retains a superseded image %s after the next build, tags %v", ref, id, tags)
		}
	}
	if tags["w:local"] != idNext {
		t.Fatalf("w:local must name the next build %s, tags %v", idNext, tags)
	}
}

// TestBuildImage_FailedBuildAfterTaggingRemovesTheTemporaryTag: an engine can
// export and tag the image and still fail the command (a cancelled build, a
// failed iidfile write). The temporary tag must not outlive the build.
func TestBuildImage_FailedBuildAfterTaggingRemovesTheTemporaryTag(t *testing.T) {
	cases := map[string]func(args []string) (imageCommandResult, error){
		"runner error": func([]string) (imageCommandResult, error) {
			return imageCommandResult{}, context.Canceled
		},
		"non-zero exit": func([]string) (imageCommandResult, error) {
			return imageCommandResult{ExitCode: 1, Stderr: "ERROR: failed to write iidfile"}, nil
		},
	}
	for name, fail := range cases {
		t.Run(name, func(t *testing.T) {
			engine := newTagStoreEngine()
			run := func(ctx context.Context, command string, cmd imageCommand) (imageCommandResult, error) {
				result, err := engine.run(ctx, command, cmd)
				if err == nil && cmd.Args[0] == "build" {
					return fail(cmd.Args)
				}
				return result, err
			}
			rt := &ExecContainerRuntime{command: RuntimeDocker, logger: newContainerTestLogger(t), runImageCommand: run}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			engine.after = func(args []string) {
				if args[0] == "build" {
					cancel() // the caller's context is gone by cleanup time
				}
			}
			_, err := rt.BuildImage(ctx, &ImageBuildSpec{Dockerfile: []byte("FROM a"), Tags: []string{"w:local"}})
			if !errors.Is(err, ErrImageBuild) {
				t.Fatalf("err = %v, want ErrImageBuild", err)
			}
			_, tags, _ := engine.state()
			for ref := range tags {
				if strings.HasPrefix(ref, buildTempRepository+":") {
					t.Errorf("temporary tag %s outlived the failed build", ref)
				}
			}
		})
	}
}

// TestBuildImage_RefusedRequestedTagReleasesTheIDReference: a build that
// fails applying a requested tag returns no ID, so nothing it applied keeps
// the image.
func TestBuildImage_RefusedRequestedTagReleasesTheIDReference(t *testing.T) {
	engine := newTagStoreEngine()
	run := func(ctx context.Context, command string, cmd imageCommand) (imageCommandResult, error) {
		if cmd.Args[0] == "tag" && cmd.Args[2] == "w:refused" {
			return imageCommandResult{ExitCode: 1, Stderr: "Error response from daemon: refusing to create a tag"}, nil
		}
		return engine.run(ctx, command, cmd)
	}
	rt := &ExecContainerRuntime{command: RuntimeDocker, logger: newContainerTestLogger(t), runImageCommand: run}
	_, err := rt.BuildImage(t.Context(), &ImageBuildSpec{Dockerfile: []byte("FROM a"), Tags: []string{"w:local", "w:refused"}})
	if !errors.Is(err, ErrImageBuild) {
		t.Fatalf("err = %v, want ErrImageBuild", err)
	}
	_, tags, _ := engine.state()
	for ref := range tags {
		if strings.HasPrefix(ref, buildIDRepository+"/") || strings.HasPrefix(ref, buildTempRepository+":") {
			t.Errorf("reference %s outlived the failed build, tags %v", ref, tags)
		}
	}
}

// idRefsOf returns the build-ID references (quicbaas-build-id/<nonce>:<hex>)
// that name id, one per build that still keeps it, sorted.
func idRefsOf(tags map[string]string, id string) []string {
	var refs []string
	for ref, target := range tags {
		if target == id && strings.HasPrefix(ref, buildIDRepository+"/") {
			refs = append(refs, ref)
		}
	}
	sort.Strings(refs)
	return refs
}

// oneIDRef returns the single build-ID reference naming id.
func oneIDRef(t *testing.T, tags map[string]string, id string) string {
	t.Helper()
	refs := idRefsOf(tags, id)
	if len(refs) != 1 {
		t.Fatalf("want one build-ID reference to %s, got %v (tags %v)", id, refs, tags)
	}
	return refs[0]
}

// Two daemons share one engine and build the same candidate tag. Daemon B is
// a separate runtime that knows nothing of A, and it runs to completion in
// the window the review names: after A has applied the candidate tag and
// before A removes its private tag. B moves the candidate tag to its own
// image, so A's private tag would be its image's last reference; the image A
// returns must survive A's cleanup all the same. B runs inside A's tag
// command, so any lock A held across that window would deadlock here: the
// image is kept alive by a reference in the engine, not by coordination in
// one process.
func TestBuildImage_IndependentProcessMovingTheTagKeepsTheReturnedImage(t *testing.T) {
	engine := newTagStoreEngine()
	daemonA := &ExecContainerRuntime{command: RuntimeDocker, logger: newContainerTestLogger(t), runImageCommand: engine.run}
	daemonB := &ExecContainerRuntime{command: RuntimeDocker, logger: newContainerTestLogger(t), runImageCommand: func(_ context.Context, _ string, cmd imageCommand) (imageCommandResult, error) {
		return engine.apply(cmd.Args) // B's commands do not reach A's hook
	}}
	spec := func() *ImageBuildSpec {
		return &ImageBuildSpec{Dockerfile: []byte("FROM a"), Tags: []string{"w:local"}}
	}

	var (
		once sync.Once
		idB  string
		errB error
	)
	engine.after = func(args []string) {
		if args[0] == "tag" && args[2] == "w:local" {
			once.Do(func() {
				idB, errB = daemonB.BuildImage(context.WithoutCancel(t.Context()), spec())
			})
		}
	}

	idA, errA := daemonA.BuildImage(t.Context(), spec())
	if errA != nil || errB != nil {
		t.Fatalf("BuildImage: A %v, B %v", errA, errB)
	}
	if idA == idB {
		t.Fatalf("the two builds must produce two images, both %q", idA)
	}
	_, tags, images := engine.state()
	if tags["w:local"] != idB {
		t.Fatalf("w:local must name daemon B's later build %s, tags %v", idB, tags)
	}
	if !images[idA] {
		t.Fatalf("daemon A's image %s was deleted with its temporary tag; tags %v", idA, tags)
	}
	if len(idRefsOf(tags, idA)) != 1 {
		t.Fatalf("daemon A's image, whose tag moved, must keep the reference naming it by ID, tags %v", tags)
	}
	for ref := range tags {
		if strings.HasPrefix(ref, buildTempRepository+":") {
			t.Errorf("temporary tag %s was left behind", ref)
		}
	}
	if err := daemonA.TagImage(t.Context(), idA, "localhost:32786/it/w:a"); err != nil {
		t.Fatalf("TagImage of daemon A's ID: %v", err)
	}

	// The push tag goes, and daemon A builds again while idA's caller may
	// still use it: the next build leaves idA's ID reference in place.
	if _, err := engine.apply([]string{"image", "rm", "localhost:32786/it/w:a"}); err != nil {
		t.Fatal(err)
	}
	engine.after = nil
	idNext, err := daemonA.BuildImage(t.Context(), spec())
	if err != nil {
		t.Fatalf("daemon A's next BuildImage: %v", err)
	}
	_, tags, images = engine.state()
	if !images[idA] || len(idRefsOf(tags, idA)) != 1 {
		t.Fatalf("daemon A's next build must not reclaim %s before its caller releases it, tags %v", idA, tags)
	}
	if tags["w:local"] != idNext {
		t.Fatalf("w:local must name daemon A's next build %s, tags %v", idNext, tags)
	}

	// The caller releases idA: its ID reference goes, and the superseded
	// image with it, rather than being retained for ever.
	daemonA.ReleaseImage(t.Context(), idA)
	_, tags, images = engine.state()
	if images[idA] {
		t.Fatalf("daemon A's superseded image %s must be deleted once released, tags %v", idA, tags)
	}
	if refs := idRefsOf(tags, idA); len(refs) != 0 {
		t.Fatalf("daemon A's kept ID reference must be removed by ReleaseImage, tags %v", tags)
	}
}

// imageRmRecorder wraps a tagStoreEngine and records each `image rm` target.
type imageRmRecorder struct {
	engine  *tagStoreEngine
	mu      sync.Mutex
	removed []string
}

func (r *imageRmRecorder) run(ctx context.Context, command string, cmd imageCommand) (imageCommandResult, error) {
	if len(cmd.Args) > 2 && cmd.Args[0] == "image" && cmd.Args[1] == "rm" {
		r.mu.Lock()
		r.removed = append(r.removed, cmd.Args[2])
		r.mu.Unlock()
	}
	return r.engine.run(ctx, command, cmd)
}

func (r *imageRmRecorder) removedCount(ref string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, got := range r.removed {
		if got == ref {
			n++
		}
	}
	return n
}

// A second build on the same runtime must not reclaim the first build's ID
// reference: the first caller may still be using that ID. Only ReleaseImage
// of the first ID removes it.
func TestBuildImage_NextBuildKeepsEarlierIDUntilReleaseImage(t *testing.T) {
	rec := &imageRmRecorder{engine: newTagStoreEngine()}
	rt := &ExecContainerRuntime{command: RuntimeDocker, logger: newContainerTestLogger(t), runImageCommand: rec.run}
	spec := &ImageBuildSpec{Dockerfile: []byte("FROM a"), Tags: []string{"w:local"}}
	first, err := rt.BuildImage(t.Context(), spec)
	if err != nil {
		t.Fatalf("first BuildImage: %v", err)
	}
	second, err := rt.BuildImage(t.Context(), spec)
	if err != nil {
		t.Fatalf("second BuildImage: %v", err)
	}
	if first == second {
		t.Fatalf("the two builds must produce two images, both %q", first)
	}
	_, tags, images := rec.engine.state()
	firstRef := oneIDRef(t, tags, first)
	secondRef := oneIDRef(t, tags, second)
	if n := rec.removedCount(firstRef); n != 0 || !images[first] || tags[firstRef] != first {
		t.Fatalf("the second build removed the first ID's reference (%d image rm), tags %v", n, tags)
	}

	rt.ReleaseImage(t.Context(), first)
	_, tags, images = rec.engine.state()
	if n := rec.removedCount(firstRef); n != 1 {
		t.Fatalf("ReleaseImage(first) ran %d image rm of %s, want 1", n, firstRef)
	}
	if _, ok := tags[firstRef]; ok || images[first] {
		t.Fatalf("the released first image must be gone, tags %v", tags)
	}
	if tags[secondRef] != second || rec.removedCount(secondRef) != 0 {
		t.Fatalf("releasing the first ID must leave the second's reference, tags %v", tags)
	}
}

// Two builds on one runtime that yield the same ID each apply their own
// reference: the first release removes only the first build's, so the second
// build's survives it, and the second release removes that.
func TestReleaseImage_SameIDFromTwoBuildsReleasesOnlyItsOwnRef(t *testing.T) {
	const id = "sha256:" + "6b6b6b6b6b6b6b6b6b6b6b6b6b6b6b6b6b6b6b6b6b6b6b6b6b6b6b6b6b6b6b6b"
	rec := &imageRmRecorder{engine: newTagStoreEngine()}
	engine := rec.engine
	rt := &ExecContainerRuntime{command: RuntimeDocker, logger: newContainerTestLogger(t), runImageCommand: cachedBuild(engine, id, rec.run)}
	var own []string
	for _, name := range []string{"A", "B"} {
		got, err := rt.BuildImage(t.Context(), &ImageBuildSpec{Dockerfile: []byte("FROM a"), Tags: []string{"w:" + name}})
		if err != nil || got != id {
			t.Fatalf("build %s: id %q, err %v", name, got, err)
		}
		_, tags, _ := engine.state()
		for _, ref := range idRefsOf(tags, id) {
			if !slices.Contains(own, ref) {
				own = append(own, ref)
			}
		}
	}
	if len(own) != 2 {
		t.Fatalf("each build must apply its own build-ID reference, got %v", own)
	}
	firstRef, secondRef := own[0], own[1]

	rt.ReleaseImage(t.Context(), id)
	_, tags, _ := engine.state()
	if rec.removedCount(firstRef) != 1 || rec.removedCount(secondRef) != 0 || tags[secondRef] != id {
		t.Fatalf("the first release must remove only the first build's %s and keep %s, removed %v, tags %v",
			firstRef, secondRef, rec.removed, tags)
	}
	rt.ReleaseImage(t.Context(), id)
	_, tags, _ = engine.state()
	if n := rec.removedCount(secondRef); n != 1 {
		t.Fatalf("the second release ran %d image rm of %s, want 1", n, secondRef)
	}
	if refs := idRefsOf(tags, id); len(refs) != 0 {
		t.Fatalf("both releases must remove both build-ID references, left %v", refs)
	}
	rt.ReleaseImage(t.Context(), id)
	if n := rec.removedCount(firstRef) + rec.removedCount(secondRef); n != 2 {
		t.Fatalf("a release past the builds ran image rm again (%d)", n)
	}
}

// cachedBuild answers every build with the same image id, as a fully cached
// build does, and passes every other command to next.
func cachedBuild(engine *tagStoreEngine, id string, next imageCommandRunner) imageCommandRunner {
	return func(ctx context.Context, command string, cmd imageCommand) (imageCommandResult, error) {
		if cmd.Args[0] == "build" {
			engine.mu.Lock()
			engine.images[id] = true
			engine.tags[argAfter(cmd.Args, "--tag")] = id
			engine.mu.Unlock()
			return imageCommandResult{}, os.WriteFile(argAfter(cmd.Args, "--iidfile"), []byte(id+"\n"), 0o600)
		}
		return next(ctx, command, cmd)
	}
}

// TestReleaseImage_FailedRemovalIsRetriedOnTheNextRelease: an `image rm` of
// a build-ID reference the engine refuses is not forgotten: the next
// ReleaseImage removes it before its own.
func TestReleaseImage_FailedRemovalIsRetriedOnTheNextRelease(t *testing.T) {
	rec := &imageRmRecorder{engine: newTagStoreEngine()}
	var refuse atomic.Bool
	run := func(ctx context.Context, command string, cmd imageCommand) (imageCommandResult, error) {
		if cmd.Args[0] == "image" && cmd.Args[1] == "rm" && strings.HasPrefix(cmd.Args[2], buildIDRepository+"/") && refuse.Swap(false) {
			rec.mu.Lock()
			rec.removed = append(rec.removed, cmd.Args[2])
			rec.mu.Unlock()
			return imageCommandResult{ExitCode: 1, Stderr: "Error response from daemon: busy"}, nil
		}
		return rec.run(ctx, command, cmd)
	}
	rt := &ExecContainerRuntime{command: RuntimeDocker, logger: newContainerTestLogger(t), runImageCommand: run}
	spec := &ImageBuildSpec{Dockerfile: []byte("FROM a"), Tags: []string{"w:local"}}
	first, err := rt.BuildImage(t.Context(), spec)
	if err != nil {
		t.Fatalf("first BuildImage: %v", err)
	}
	second, err := rt.BuildImage(t.Context(), spec)
	if err != nil {
		t.Fatalf("second BuildImage: %v", err)
	}
	_, tags, _ := rec.engine.state()
	firstRef, secondRef := oneIDRef(t, tags, first), oneIDRef(t, tags, second)

	refuse.Store(true)
	rt.ReleaseImage(t.Context(), first)
	_, tags, images := rec.engine.state()
	if tags[firstRef] != first || !images[first] {
		t.Fatalf("the refused removal must leave %s in place, tags %v", firstRef, tags)
	}
	rt.ReleaseImage(t.Context(), second)
	_, tags, images = rec.engine.state()
	if n := rec.removedCount(firstRef); n != 2 {
		t.Fatalf("the next release ran %d image rm of the refused %s, want 2", n, firstRef)
	}
	if _, ok := tags[firstRef]; ok || images[first] {
		t.Fatalf("the retried %s must be gone with its image, tags %v", firstRef, tags)
	}
	if _, ok := tags[secondRef]; ok {
		t.Fatalf("the second release must remove its own %s, tags %v", secondRef, tags)
	}
	rt.ReleaseImage(t.Context(), second)
	if n := rec.removedCount(firstRef); n != 2 {
		t.Fatalf("a removed pending reference was retried again (%d image rm)", n)
	}
}

// An ID the runtime holds no reference for, or one that is not a digest, is
// a no-op: ReleaseImage runs no engine command.
func TestReleaseImage_UnknownOrNonDigestIDIsANoOp(t *testing.T) {
	rec := &imageRmRecorder{engine: newTagStoreEngine()}
	rt := &ExecContainerRuntime{command: RuntimeDocker, logger: newContainerTestLogger(t), runImageCommand: rec.run}
	for _, id := range []string{"", "w:local", "sha256:abc", "sha256:" + strings.Repeat("7c", 32)} {
		rt.ReleaseImage(t.Context(), id)
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if len(rec.removed) != 0 {
		t.Fatalf("ReleaseImage of unknown IDs ran image rm %v", rec.removed)
	}
}

// A cached build gives two processes' builds one image ID. Each pins its own
// reference, so one process's release leaves the image the other returned.
func TestBuildImage_TwoBuildsOfOneImageEachKeepTheirOwnIDReference(t *testing.T) {
	const id = "sha256:" + "5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a"
	engine := newTagStoreEngine()
	cached := cachedBuild(engine, id, engine.run)
	var runtimes []*ExecContainerRuntime
	for _, name := range []string{"A", "B"} {
		rt := &ExecContainerRuntime{command: RuntimeDocker, logger: newContainerTestLogger(t), runImageCommand: cached}
		got, err := rt.BuildImage(t.Context(), &ImageBuildSpec{Dockerfile: []byte("FROM a"), Tags: []string{"w:" + name}})
		if err != nil || got != id {
			t.Fatalf("build %s: id %q, err %v", name, got, err)
		}
		runtimes = append(runtimes, rt)
	}
	_, tags, images := engine.state()
	if !images[id] || len(idRefsOf(tags, id)) != 2 {
		t.Fatalf("each process's build must keep its own ID reference, tags %v, images %v", tags, images)
	}
	// Process A is done, and the requested tags are gone: process B's own
	// reference still keeps the image it uses.
	for _, tag := range []string{"w:A", "w:B"} {
		if _, err := engine.apply([]string{"image", "rm", tag}); err != nil {
			t.Fatal(err)
		}
	}
	runtimes[0].ReleaseImage(t.Context(), id)
	_, tags, images = engine.state()
	if !images[id] || len(idRefsOf(tags, id)) != 1 {
		t.Fatalf("process A's release must leave process B's reference and image, tags %v", tags)
	}
	runtimes[1].ReleaseImage(t.Context(), id)
	_, tags, images = engine.state()
	if images[id] || len(idRefsOf(tags, id)) != 0 {
		t.Fatalf("both releases must free the image, tags %v", tags)
	}
}

func TestBuildIDRef(t *testing.T) {
	const hex = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	const nonce = "00112233aabbccdd"
	for _, id := range []string{"sha256:" + hex, hex} {
		got, err := buildIDRef(nonce, id)
		if err != nil || got != "quicbaas-build-id/"+nonce+":"+hex {
			t.Errorf("buildIDRef(%q) = %q, %v", id, got, err)
		}
	}
	for _, id := range []string{"", "sha256:", "sha256:abc", "sha512:" + hex, "sha256:" + strings.ToUpper(hex), "w:local"} {
		if _, err := buildIDRef(nonce, id); !errors.Is(err, errImageIDNotDigest) {
			t.Errorf("buildIDRef(%q) err = %v, want errImageIDNotDigest", id, err)
		}
	}
	a, errA := newBuildNonce()
	b, errB := newBuildNonce()
	if errA != nil || errB != nil || a == b || !regexp.MustCompile(`^[0-9a-f]{16}$`).MatchString(a) {
		t.Fatalf("newBuildNonce = %q, %q (%v, %v), want two distinct 16-hex-character nonces", a, b, errA, errB)
	}
}

// An engine whose inspect answers an ID that names no digest fails the build
// rather than leave the image with only its temporary tag.
func TestBuildImage_IDThatIsNoDigestIsABuildError(t *testing.T) {
	engine := &fakeImageEngine{answer: builtImageEngine("sha256:1", "sha256:1")}
	rt := newImageTestRuntime(t, RuntimeDocker, engine)
	_, err := rt.BuildImage(t.Context(), &ImageBuildSpec{Dockerfile: []byte("FROM a"), Tags: []string{"w:local"}})
	if !errors.Is(err, ErrImageBuild) || !errors.Is(err, errImageIDNotDigest) {
		t.Fatalf("err = %v, want ErrImageBuild wrapping errImageIDNotDigest", err)
	}
	ref := buildRefOf(t, engine.calls)
	if got := strings.Join(engine.calls[len(engine.calls)-1].Args, " "); got != "image rm "+ref {
		t.Fatalf("the temporary tag must still be removed, last argv %q", got)
	}
}
