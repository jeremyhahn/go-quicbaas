// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
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
	"bytes"
	"cmp"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// oneShotNamePrefix names the containers RunImage starts.
const oneShotNamePrefix = "quicbaas-run-"

var _ orchestration.ContainerOwnerSweeper = (*JobRuntime)(nil)

// SweepOwned removes every container labelled orchestration.ContainerOwnerLabel
// = owner, oldest first, and reports what it removed. A container already gone
// counts as removed; any other refusal ends the sweep with a
// ContainerSweepError. An empty owner is refused: it could only be a sweep of
// everything.
func (j *JobRuntime) SweepOwned(ctx context.Context, owner string) (orchestration.ContainerSweep, error) {
	owner = strings.TrimSpace(owner)
	if owner == "" {
		return orchestration.ContainerSweep{}, &orchestration.ContainerSweepRefusedError{
			Reason: "the sweep claims containers by owner label and was given no owner; " +
				"an unowned sweep could only be a sweep of everything",
		}
	}

	states, err := j.runtime.ListContainers(ctx, true)
	if err != nil {
		return orchestration.ContainerSweep{}, &orchestration.ContainerSweepError{Owner: owner, Cause: err}
	}
	owned := slices.DeleteFunc(states, func(state *containerState) bool {
		return state.Config == nil || state.Config.Labels[orchestration.ContainerOwnerLabel] != owner
	})
	slices.SortFunc(owned, func(a, b *containerState) int {
		return cmp.Or(a.CreatedAt.Compare(b.CreatedAt), strings.Compare(a.ID, b.ID))
	})

	sweep := orchestration.ContainerSweep{Owner: owner}
	for _, state := range owned {
		if err := j.Remove(ctx, state.ID); err != nil {
			var notFound *orchestration.ContainerNotFoundError
			if !errors.As(err, &notFound) {
				return sweep, &orchestration.ContainerSweepError{Owner: owner, ContainerID: state.ID, Cause: err}
			}
		}
		sweep.Removed = append(sweep.Removed, state.ID)
		j.runtime.logger.Info("container sweep: removed an orphan left by a previous run", "id", state.ID, "owner", owner)
	}
	return sweep, nil
}

// InspectImage reports the stored image: its ID, its configuration labels
// (never nil) and the repository digests it is known by.
func (j *JobRuntime) InspectImage(ctx context.Context, image string) (*orchestration.ImageInfo, error) {
	if strings.TrimSpace(image) == "" {
		return nil, &orchestration.InputValidationError{Field: "image", Message: "cannot be empty"}
	}
	record, err := j.runtime.backends.images.Inspect(ctx, image)
	if err != nil {
		if errors.Is(err, ErrImageNotFound) {
			return nil, &orchestration.ImageNotFoundError{Image: image}
		}
		return nil, &orchestration.ImageInspectError{Image: image, Cause: err}
	}

	id := record.ID
	if id != "" && !strings.Contains(id, ":") {
		id = "sha256:" + id
	}
	labels := maps.Clone(record.Config.Labels)
	if labels == nil {
		labels = map[string]string{}
	}
	return &orchestration.ImageInfo{ID: id, Labels: labels, RepoDigests: repoDigests(record)}, nil
}

// repoDigests pairs each repository the image is named by with each manifest
// digest it is stored under, as "repository@sha256:<hex>".
func repoDigests(record imageRecord) []string {
	var digests []string
	for _, name := range record.Names {
		repository := imageRepository(name)
		for _, digest := range record.Digests {
			digests = append(digests, repository+"@"+digest)
		}
	}
	slices.Sort(digests)
	return slices.Compact(digests)
}

// imageRepository is name without its tag or digest.
func imageRepository(name string) string {
	name, _, _ = strings.Cut(name, "@")
	if colon := strings.LastIndex(name, ":"); colon > strings.LastIndex(name, "/") {
		name = name[:colon]
	}
	return name
}

// RunImage runs spec.Command in a fresh container of spec.Image to
// completion and reports its exit status and output, as `run --rm` does. The
// container is removed afterwards; when ctx ends first it is killed and
// removed, and the run reports ctx's error.
func (j *JobRuntime) RunImage(ctx context.Context, spec *orchestration.ImageRunSpec) (*orchestration.ExecResult, error) {
	containerSpec, err := imageRunContainerSpec(spec)
	if err != nil {
		return nil, err
	}

	id, err := j.launch(ctx, containerSpec, func(id string) error {
		return j.runtime.startInit(id, func(init *processSpec) {
			if spec.Stdin != nil {
				init.Stdin = bytes.NewReader(spec.Stdin)
			}
		})
	})
	if err != nil {
		return nil, err
	}

	// The caller's context may be the one that ended; the cleanup has its own.
	background := context.WithoutCancel(ctx)
	defer func() {
		if rmErr := j.runtime.RemoveContainer(background, id, true); rmErr != nil {
			j.runtime.logger.Warn("failed to remove one-shot container", "id", id, "error", rmErr)
		}
	}()

	code, err := j.runtime.WaitContainer(ctx, id)
	if err != nil {
		if stopErr := j.runtime.StopContainer(background, id, 0); stopErr != nil {
			err = errors.Join(err, stopErr)
		}
		return nil, &orchestration.ContainerStartError{Image: spec.Image, Cause: err}
	}
	stdout, stderr, err := j.runtime.Logs(id)
	if err != nil {
		return nil, &orchestration.TransportError{Operation: "logs", Cause: err}
	}
	return &orchestration.ExecResult{ExitCode: int(code), Stdout: string(stdout), Stderr: string(stderr)}, nil
}

// imageRunContainerSpec validates spec and maps it onto the container spec a
// launch takes, with the sandbox policy applied exactly as a spawn applies it.
func imageRunContainerSpec(spec *orchestration.ImageRunSpec) (*orchestration.ContainerSpec, error) {
	if spec == nil {
		return nil, &orchestration.InputValidationError{Field: "spec", Message: "cannot be nil"}
	}
	if strings.TrimSpace(spec.Image) == "" {
		return nil, &orchestration.InputValidationError{Field: "image", Message: "cannot be empty"}
	}
	if len(spec.Command) == 0 {
		return nil, &orchestration.InputValidationError{Field: "command", Message: "cannot be empty"}
	}
	for i, m := range spec.Mounts {
		if err := m.Validate(); err != nil {
			return nil, &orchestration.InputValidationError{Field: "mounts[" + strconv.Itoa(i) + "]", Message: err.Error()}
		}
	}

	suffix := make([]byte, 8)
	if _, err := rand.Read(suffix); err != nil {
		return nil, &orchestration.ContainerStartError{Image: spec.Image, Cause: err}
	}
	containerSpec := &orchestration.ContainerSpec{
		Name:    oneShotNamePrefix + hex.EncodeToString(suffix),
		Image:   spec.Image,
		User:    spec.User,
		Env:     maps.Clone(spec.Env),
		Mounts:  slices.Clone(spec.Mounts),
		Command: slices.Clone(spec.Command),
	}
	if spec.NetworkNone {
		containerSpec.Network = "none"
	}
	if spec.Security != nil {
		if err := spec.Security.Apply(containerSpec); err != nil {
			return nil, err
		}
	}
	return containerSpec, nil
}
