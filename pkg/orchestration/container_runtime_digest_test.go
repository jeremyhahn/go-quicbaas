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
	"strings"
	"testing"
)

// fakeInspect builds an inspectFunc that answers from a table keyed on the
// joined argument list, so a test states exactly which runtime queries it
// expects. An unlisted query returns a not-found error, which is what an
// inspect against a missing object does in practice.
func fakeInspect(responses map[string]string) inspectFunc {
	return func(_ context.Context, args ...string) (string, error) {
		key := strings.Join(args, " ")
		out, ok := responses[key]
		if !ok {
			return "", &CommandError{Output: "Error: no such object: " + key}
		}
		return out, nil
	}
}

// newPinTestRuntime builds an ExecContainerRuntime whose inspect queries are
// answered from a table. No container runtime binary is involved.
func newPinTestRuntime(t *testing.T, responses map[string]string) *ExecContainerRuntime {
	t.Helper()
	return &ExecContainerRuntime{
		command: RuntimeDocker,
		logger:  newContainerTestLogger(t),
		inspect: fakeInspect(responses),
	}
}

func TestExecContainerRuntime_ResolveImageDigest(t *testing.T) {
	t.Run("FromRepoDigests", func(t *testing.T) {
		rt := newPinTestRuntime(t, map[string]string{
			`image inspect --format {{range .RepoDigests}}{{println .}}{{end}} alpine:3.20`: "alpine@" + testDigest + "\n",
		})

		got, err := rt.ResolveImageDigest(t.Context(), "alpine:3.20")
		if err != nil {
			t.Fatalf("ResolveImageDigest: %v", err)
		}
		if got != testDigest {
			t.Fatalf("digest = %q, want %q", got, testDigest)
		}
	})

	t.Run("PrefersMatchingRepository", func(t *testing.T) {
		rt := newPinTestRuntime(t, map[string]string{
			`image inspect --format {{range .RepoDigests}}{{println .}}{{end}} alpine:3.20`: "mirror.example.com/alpine@" + otherDigest + "\nalpine@" + testDigest + "\n",
		})

		got, err := rt.ResolveImageDigest(t.Context(), "alpine:3.20")
		if err != nil {
			t.Fatalf("ResolveImageDigest: %v", err)
		}
		if got != testDigest {
			t.Fatalf("digest = %q, want the entry matching the requested repository", got)
		}
	})

	t.Run("AlreadyPinnedNeedsNoLookup", func(t *testing.T) {
		// An empty response table proves no inspect call was made: any
		// query would return the not-found error.
		rt := newPinTestRuntime(t, map[string]string{})

		got, err := rt.ResolveImageDigest(t.Context(), "alpine@"+testDigest)
		if err != nil {
			t.Fatalf("ResolveImageDigest: %v", err)
		}
		if got != testDigest {
			t.Fatalf("digest = %q, want %q", got, testDigest)
		}
	})

	t.Run("EmptyImageRejected", func(t *testing.T) {
		rt := newPinTestRuntime(t, map[string]string{})
		if _, err := rt.ResolveImageDigest(t.Context(), ""); err == nil {
			t.Fatal("expected an error for an empty image")
		}
	})

	t.Run("LocallyBuiltImageHasNoRepoDigest", func(t *testing.T) {
		rt := newPinTestRuntime(t, map[string]string{
			`image inspect --format {{range .RepoDigests}}{{println .}}{{end}} local/agent:dev`: "\n",
		})

		_, err := rt.ResolveImageDigest(t.Context(), "local/agent:dev")
		if err == nil {
			t.Fatal("expected an error for an image with no repository digest")
		}
		if !errors.Is(err, ErrImageDigestUnresolved) {
			t.Fatalf("error %v does not wrap ErrImageDigestUnresolved", err)
		}
	})

	t.Run("InspectFailurePropagates", func(t *testing.T) {
		rt := newPinTestRuntime(t, map[string]string{})
		_, err := rt.ResolveImageDigest(t.Context(), "missing:latest")
		if err == nil {
			t.Fatal("expected an error when inspect fails")
		}
		if !errors.Is(err, ErrImageDigestUnresolved) {
			t.Fatalf("error %v does not wrap ErrImageDigestUnresolved", err)
		}
	})

	t.Run("MalformedInlineDigestRejected", func(t *testing.T) {
		rt := newPinTestRuntime(t, map[string]string{})
		if _, err := rt.ResolveImageDigest(t.Context(), "alpine@sha256:short"); err == nil {
			t.Fatal("expected an error for a malformed inline digest")
		}
	})
}

func TestExecContainerRuntime_VerifyPinnedImage(t *testing.T) {
	const pinnedRef = "alpine@" + testDigest
	const imageID = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	const substitutedID = "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

	t.Run("MatchingImagePasses", func(t *testing.T) {
		rt := newPinTestRuntime(t, map[string]string{
			"image inspect --format {{.Id}} " + pinnedRef: imageID,
			"inspect --format {{.Image}} container-1":     imageID,
		})

		spec := &ContainerSpec{Image: pinnedRef, TrustLevel: TrustLevelPinned}
		if err := rt.VerifyPinnedImage(t.Context(), spec, "container-1"); err != nil {
			t.Fatalf("VerifyPinnedImage: %v", err)
		}
	})

	t.Run("SubstitutedImageIsRejected", func(t *testing.T) {
		rt := newPinTestRuntime(t, map[string]string{
			"image inspect --format {{.Id}} " + pinnedRef: imageID,
			"inspect --format {{.Image}} container-1":     substitutedID,
		})

		spec := &ContainerSpec{Image: pinnedRef, TrustLevel: TrustLevelPinned}
		err := rt.VerifyPinnedImage(t.Context(), spec, "container-1")
		if err == nil {
			t.Fatal("expected a digest mismatch to be rejected")
		}
		if !errors.Is(err, ErrImageDigestMismatch) {
			t.Fatalf("error %v does not wrap ErrImageDigestMismatch", err)
		}
		var mismatch *ContainerImageDigestMismatchError
		if !errors.As(err, &mismatch) {
			t.Fatalf("expected *ContainerImageDigestMismatchError, got %T", err)
		}
		if mismatch.ExpectedImageID != imageID || mismatch.ActualImageID != substitutedID {
			t.Fatalf("mismatch does not carry both image ids: %+v", mismatch)
		}
	})

	t.Run("UnpinnedSpecIsSkipped", func(t *testing.T) {
		// An empty table proves no inspect happened.
		rt := newPinTestRuntime(t, map[string]string{})
		spec := &ContainerSpec{Image: "alpine:3.20"}
		if err := rt.VerifyPinnedImage(t.Context(), spec, "container-1"); err != nil {
			t.Fatalf("unpinned spec must skip verification, got %v", err)
		}
	})

	t.Run("EmptyContainerIDIsSkipped", func(t *testing.T) {
		rt := newPinTestRuntime(t, map[string]string{})
		spec := &ContainerSpec{Image: pinnedRef, TrustLevel: TrustLevelPinned}
		if err := rt.VerifyPinnedImage(t.Context(), spec, ""); err != nil {
			t.Fatalf("empty container id must skip verification, got %v", err)
		}
	})

	t.Run("NilSpecRejected", func(t *testing.T) {
		rt := newPinTestRuntime(t, map[string]string{})
		if err := rt.VerifyPinnedImage(t.Context(), nil, "container-1"); err == nil {
			t.Fatal("expected an error for a nil spec")
		}
	})

	t.Run("UnresolvablePinnedImageIsAnError", func(t *testing.T) {
		// The pinned reference itself cannot be resolved: that is a hard
		// failure, because the pin can then never be checked.
		rt := newPinTestRuntime(t, map[string]string{
			"inspect --format {{.Image}} container-1": imageID,
		})
		spec := &ContainerSpec{Image: pinnedRef, TrustLevel: TrustLevelPinned}
		err := rt.VerifyPinnedImage(t.Context(), spec, "container-1")
		if err == nil {
			t.Fatal("expected an error when the pinned reference cannot be resolved")
		}
		if !errors.Is(err, ErrImageDigestUnresolved) {
			t.Fatalf("error %v does not wrap ErrImageDigestUnresolved", err)
		}
	})

	t.Run("NotYetCreatedContainerIsNotAFailure", func(t *testing.T) {
		// The interactive path verifies against a container the foreground
		// run process may not have created yet. The command-line pin still
		// applies, so this must not fail the launch.
		rt := newPinTestRuntime(t, map[string]string{
			"image inspect --format {{.Id}} " + pinnedRef: imageID,
		})
		spec := &ContainerSpec{Image: pinnedRef, TrustLevel: TrustLevelPinned}
		if err := rt.VerifyPinnedImage(t.Context(), spec, "not-created-yet"); err != nil {
			t.Fatalf("an uninspectable container must not fail the pin check, got %v", err)
		}
	})
}

// TestExecContainerRuntime_RunArgsUsePinnedReference proves Tier 0 reaches the
// command line: a pinned spec must be launched as repository@digest, never as
// the mutable tag.
func TestExecContainerRuntime_RunArgsUsePinnedReference(t *testing.T) {
	rt := &ExecContainerRuntime{command: RuntimeDocker, logger: newContainerTestLogger(t)}

	t.Run("PinnedSpec", func(t *testing.T) {
		spec := &ContainerSpec{
			Name:        "agent",
			Image:       "alpine:3.20",
			ImageDigest: testDigest,
			TrustLevel:  TrustLevelPinned,
		}
		args := strings.Join(rt.buildRunArgs(spec, ""), " ")
		want := "alpine:3.20@" + testDigest
		if !strings.Contains(args, want) {
			t.Fatalf("run args %q do not carry the pinned reference %q", args, want)
		}
	})

	t.Run("InteractivePinnedSpec", func(t *testing.T) {
		spec := &ContainerSpec{
			Image:       "alpine:3.20",
			ImageDigest: testDigest,
			TrustLevel:  TrustLevelPinned,
		}
		args := strings.Join(rt.buildInteractiveRunArgs(spec, "agent", ""), " ")
		want := "alpine:3.20@" + testDigest
		if !strings.Contains(args, want) {
			t.Fatalf("interactive run args %q do not carry the pinned reference %q", args, want)
		}
	})

	t.Run("UnpinnedSpecUnchanged", func(t *testing.T) {
		spec := &ContainerSpec{Name: "agent", Image: "alpine:3.20"}
		args := strings.Join(rt.buildRunArgs(spec, ""), " ")
		if strings.Contains(args, "@sha256:") {
			t.Fatalf("unpinned run args must not gain a digest: %q", args)
		}
		if !strings.HasSuffix(args, "alpine:3.20") {
			t.Fatalf("unpinned run args %q do not end with the image reference", args)
		}
	})
}

// TestExecContainerRuntime_RunRejectsUnpinnedAtPinnedLevel proves the tier
// gate fires before any process is spawned: a spec demanding Tier 0 with only
// a tag is rejected by validation, so no container is ever created.
func TestExecContainerRuntime_RunRejectsUnpinnedAtPinnedLevel(t *testing.T) {
	rt := &ExecContainerRuntime{command: "/nonexistent-runtime", logger: newContainerTestLogger(t)}

	spec := &ContainerSpec{Image: "alpine:3.20", TrustLevel: TrustLevelPinned}

	if _, err := rt.Run(t.Context(), spec); !errors.Is(err, ErrImageNotPinned) {
		t.Fatalf("Run error %v does not wrap ErrImageNotPinned", err)
	}
	if _, err := rt.RunInteractive(t.Context(), spec); !errors.Is(err, ErrImageNotPinned) {
		t.Fatalf("RunInteractive error %v does not wrap ErrImageNotPinned", err)
	}
}

func TestFirstLine(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "Single", input: "value", want: "value"},
		{name: "LeadingBlank", input: "\n\n  value  \nsecond", want: "value"},
		{name: "Empty", input: "", want: ""},
		{name: "OnlyWhitespace", input: "  \n\t\n", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := firstLine(tt.input); got != tt.want {
				t.Fatalf("firstLine(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
