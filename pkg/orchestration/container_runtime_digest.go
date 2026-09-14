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
	"bytes"
	"context"
	"errors"
	"os/exec"
	"strings"
)

// =============================================================================
// Tier 0 — Digest Resolution and Post-Launch Verification (docker / podman)
// =============================================================================
//
// Two distinct operations live here and they are easy to conflate:
//
//   ResolveImageDigest  turns a tag into the registry MANIFEST digest
//                       (RepoDigests) so an operator can capture a pin.
//   VerifyPinnedImage   confirms a running container was created from the
//                       pinned image, by comparing local image IDs
//                       (docker's .Id / .Image), which is the only identity
//                       a container carries after creation.
//
// Manifest digest and local image ID are different values; the verification
// path therefore resolves the pinned reference to its local ID first and
// compares like with like.

// ResolveImageDigest returns the registry manifest digest of a locally
// present image, in canonical "sha256:<hex>" form.
//
// It reads RepoDigests, which the runtime populates from the manifest the
// image was pulled by. An image that was built locally and never pushed has
// no RepoDigest; that is reported as ImageDigestUnresolvedError rather than
// silently substituting the local image ID, because the two are not
// interchangeable across hosts.
func (r *ExecContainerRuntime) ResolveImageDigest(ctx context.Context, image string) (string, error) {
	if image == "" {
		return "", &InputValidationError{Field: "image", Message: "cannot be empty"}
	}

	// A reference that already carries a digest needs no lookup.
	if _, digest, pinned := SplitImageReference(image); pinned {
		if err := ValidateImageDigest(digest); err != nil {
			return "", err
		}
		return digest, nil
	}

	out, err := r.runInspect(ctx, "image", "inspect", "--format",
		"{{range .RepoDigests}}{{println .}}{{end}}", image)
	if err != nil {
		return "", &ImageDigestUnresolvedError{Image: image, Cause: err}
	}

	digest := selectRepoDigest(out, image)
	if digest == "" {
		return "", &ImageDigestUnresolvedError{
			Image: image,
			Cause: errImageNoRepoDigest,
		}
	}
	if err := ValidateImageDigest(digest); err != nil {
		return "", &ImageDigestUnresolvedError{Image: image, Cause: err}
	}
	return digest, nil
}

// selectRepoDigest picks the digest belonging to image's repository from a
// newline-separated list of "repository@sha256:<hex>" entries. When no entry
// matches the repository (a mirrored pull, for instance) the first entry is
// used, since every RepoDigest of an image refers to the same manifest
// content.
func selectRepoDigest(out, image string) string {
	repository := ImageRepository(image)
	if idx := strings.LastIndex(repository, ":"); idx > strings.LastIndex(repository, "/") {
		repository = repository[:idx] // strip the tag
	}

	var first string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		entryRepo, digest, pinned := SplitImageReference(line)
		if !pinned {
			continue
		}
		if first == "" {
			first = digest
		}
		if entryRepo == repository {
			return digest
		}
	}
	return first
}

// VerifyPinnedImage confirms that a container was created from the image the
// spec pinned.
//
// It resolves the pinned reference to a local image ID and compares it to the
// image ID recorded on the container. Both values come from the runtime's own
// inspect output, so a substituted image — the exact attack digest pinning
// exists to stop — produces a ContainerImageDigestMismatchError.
//
// The check is skipped entirely for unpinned specs, preserving today's
// behavior. It is also skipped when the container cannot be inspected yet:
// an interactive container is created asynchronously by the foreground run
// process, and failing a launch because the runtime had not finished creating
// the container would be a false positive. The command-line pin still applies
// in that window — the runtime cannot resolve repository@digest to anything
// other than that digest.
func (r *ExecContainerRuntime) VerifyPinnedImage(ctx context.Context, spec *ContainerSpec, containerID string) error {
	if spec == nil {
		return &InputValidationError{Field: "spec", Message: "cannot be nil"}
	}
	if !spec.IsPinned() || containerID == "" {
		return nil
	}

	pinnedRef := spec.PinnedImageReference()

	expectedID, err := r.runInspect(ctx, "image", "inspect", "--format", "{{.Id}}", pinnedRef)
	if err != nil {
		return &ImageDigestUnresolvedError{Image: pinnedRef, Cause: err}
	}
	expectedID = firstLine(expectedID)
	if expectedID == "" {
		return &ImageDigestUnresolvedError{
			Image: pinnedRef,
			Cause: errImagePinnedIDMissing,
		}
	}

	actualID, err := r.runInspect(ctx, "inspect", "--format", "{{.Image}}", containerID)
	if err != nil {
		// The container is not inspectable yet (or at all). See the doc
		// comment: this is not a pin failure.
		r.logger.Debugf("container trust: skipping post-launch digest check for %s: %v",
			truncateID(containerID), err)
		return nil
	}
	actualID = firstLine(actualID)
	if actualID == "" {
		return nil
	}

	if actualID != expectedID {
		return &ContainerImageDigestMismatchError{
			ContainerID:     containerID,
			Image:           pinnedRef,
			ExpectedImageID: expectedID,
			ActualImageID:   actualID,
		}
	}

	r.logger.Debugf("container trust: %s verified against pinned image %s",
		truncateID(containerID), pinnedRef)
	return nil
}

// runInspect executes an inspect query through the injected seam when one is
// set, otherwise through the real runtime binary.
func (r *ExecContainerRuntime) runInspect(ctx context.Context, args ...string) (string, error) {
	if r.inspect != nil {
		return r.inspect(ctx, args...)
	}
	return r.execInspect(ctx, args...)
}

// errImageNoRepoDigest and errImagePinnedIDMissing are AUTHORED diagnoses, and
// that is why they are not CommandError.
//
// CommandError means "a process printed these bytes while failing", and its
// Error() withholds them because a process prints whatever it prints. Neither
// sentence below is that: both are this package's own conclusion about a
// runtime response that was well-formed and simply lacked a field. No process
// wrote them, so no process can have leaked a credential into them, and
// withholding them would destroy real diagnostic value while protecting
// nothing.
//
// Moved OFF CommandError rather than given a second mode on it (review 1330).
// A type with a rendered mode and a withheld mode needs a branch in Error(),
// and the safety property is strictly stronger without one: CommandError never
// renders its payload, so no future caller can set the wrong field and get
// plaintext out. These two were never stderr in the first place — the type's
// own doc said it wraps the output of a failed command.
var (
	errImageNoRepoDigest    = errors.New("image has no repository digest; pull it from a registry to obtain one")
	errImagePinnedIDMissing = errors.New("runtime returned no image id for the pinned reference")
)

// execInspect shells out to the container runtime and returns trimmed stdout.
func (r *ExecContainerRuntime) execInspect(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, r.command, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		op := ""
		if len(args) > 0 {
			op = args[0]
		}
		return "", &CommandError{Command: r.command, Op: op, Output: strings.TrimSpace(stderr.String())}
	}
	return strings.TrimSpace(stdout.String()), nil
}

// firstLine returns the first non-empty trimmed line of s.
func firstLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

// ContainerImageDigestMismatchError indicates a container was created from an
// image other than the one its spec pinned.
type ContainerImageDigestMismatchError struct {
	ContainerID     string
	Image           string
	ExpectedImageID string
	ActualImageID   string
}

// Error implements the error interface.
func (e *ContainerImageDigestMismatchError) Error() string {
	return ErrImageDigestMismatch.Error() + ": container " + truncateID(e.ContainerID) +
		" was created from image " + truncateID(e.ActualImageID) +
		" but its spec pins " + e.Image + " (" + truncateID(e.ExpectedImageID) + ")"
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *ContainerImageDigestMismatchError) Unwrap() error {
	return ErrImageDigestMismatch
}
