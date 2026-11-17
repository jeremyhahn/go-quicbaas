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
	"strings"
	"sync"
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
	if tags[mustBuildIDRef(t, idA)] != idA || tags[mustBuildIDRef(t, idB)] != idB {
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

	// The callers are done: the push tag goes, and the runtime's next build
	// reclaims both ID references, so neither superseded image is retained.
	if _, err := engine.apply([]string{"image", "rm", "localhost:32786/it/w:a"}); err != nil {
		t.Fatal(err)
	}
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
		if strings.HasPrefix(ref, buildIDRepository+":") || strings.HasPrefix(ref, buildTempRepository+":") {
			t.Errorf("reference %s outlived the failed build, tags %v", ref, tags)
		}
	}
}

// mustBuildIDRef is the reference BuildImage names an image by its ID.
func mustBuildIDRef(t *testing.T, id string) string {
	t.Helper()
	ref, err := buildIDRef(id)
	if err != nil {
		t.Fatalf("buildIDRef(%q): %v", id, err)
	}
	return ref
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
	if tags[mustBuildIDRef(t, idA)] != idA {
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

	// The caller is done with idA: its push tag goes, and daemon A's next
	// build reclaims the ID reference it kept, so the superseded image is
	// deleted rather than retained for ever.
	if _, err := engine.apply([]string{"image", "rm", "localhost:32786/it/w:a"}); err != nil {
		t.Fatal(err)
	}
	engine.after = nil
	idNext, err := daemonA.BuildImage(t.Context(), spec())
	if err != nil {
		t.Fatalf("daemon A's next BuildImage: %v", err)
	}
	_, tags, images = engine.state()
	if images[idA] {
		t.Fatalf("daemon A's superseded image %s must be reclaimed by its next build, tags %v", idA, tags)
	}
	if _, ok := tags[mustBuildIDRef(t, idA)]; ok {
		t.Fatalf("daemon A's kept ID reference must be removed by its next build, tags %v", tags)
	}
	if tags["w:local"] != idNext {
		t.Fatalf("w:local must name daemon A's next build %s, tags %v", idNext, tags)
	}
}

// A cached build gives two builds one image ID. Both pin the same reference,
// and either one's cleanup leaves the image the other returned.
func TestBuildImage_TwoBuildsOfOneImageShareItsIDReference(t *testing.T) {
	const id = "sha256:" + "5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a"
	engine := newTagStoreEngine()
	cached := func(ctx context.Context, command string, cmd imageCommand) (imageCommandResult, error) {
		if cmd.Args[0] == "build" {
			engine.mu.Lock()
			engine.images[id] = true
			engine.tags[argAfter(cmd.Args, "--tag")] = id
			engine.mu.Unlock()
			return imageCommandResult{}, os.WriteFile(argAfter(cmd.Args, "--iidfile"), []byte(id+"\n"), 0o600)
		}
		return engine.run(ctx, command, cmd)
	}
	for _, name := range []string{"A", "B"} {
		rt := &ExecContainerRuntime{command: RuntimeDocker, logger: newContainerTestLogger(t), runImageCommand: cached}
		got, err := rt.BuildImage(t.Context(), &ImageBuildSpec{Dockerfile: []byte("FROM a"), Tags: []string{"w:" + name}})
		if err != nil || got != id {
			t.Fatalf("build %s: id %q, err %v", name, got, err)
		}
	}
	_, tags, images := engine.state()
	if !images[id] || tags[mustBuildIDRef(t, id)] != id {
		t.Fatalf("the shared image must keep its ID reference, tags %v, images %v", tags, images)
	}
}

func TestBuildIDRef(t *testing.T) {
	const hex = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	for _, id := range []string{"sha256:" + hex, hex} {
		if got := mustBuildIDRef(t, id); got != "quicbaas-build-id:"+hex {
			t.Errorf("buildIDRef(%q) = %q", id, got)
		}
	}
	for _, id := range []string{"", "sha256:", "sha256:abc", "sha512:" + hex, "sha256:" + strings.ToUpper(hex), "w:local"} {
		if _, err := buildIDRef(id); !errors.Is(err, errImageIDNotDigest) {
			t.Errorf("buildIDRef(%q) err = %v, want errImageIDNotDigest", id, err)
		}
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
