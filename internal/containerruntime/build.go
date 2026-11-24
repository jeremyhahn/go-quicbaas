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
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
)

// buildOptions are an image build's inputs besides its Dockerfile.
type buildOptions struct {
	Tags   []string          // at least one; the built image is named by each
	Args   map[string]string // build arguments
	Labels map[string]string // labels on the final image, over its LABELs
	Pull   bool              // pull every base image even when it is stored
}

// buildLogTail is how much of a failed RUN step's output its error carries.
const buildLogTail = 2048

// buildNetworkMode is a build container's network. The runtime has no other
// mode yet, so a RUN step that fetches from the network fails.
const buildNetworkMode = "none"

// buildImage builds dockerfile with an empty context and returns the
// stored image, tagged with every tag.
//
// Every step commits one layer on the image the step before it made, with
// its stage's final configuration; the intermediate images stay stored,
// untagged. Build containers run with no network.
func (r *Runtime) buildImage(ctx context.Context, dockerfile []byte, opts buildOptions) (imageRecord, error) {
	if len(opts.Tags) == 0 {
		return imageRecord{}, errors.New("build: at least one tag is required")
	}
	if r.backends == nil || r.backends.commit == nil {
		return imageRecord{}, fmt.Errorf("build: no layer committer: %w", ErrRuntimeRequiresLinuxCgo)
	}

	instructions, err := parseDockerfile(dockerfile)
	if err != nil {
		return imageRecord{}, err
	}
	bases := map[string]imageRecord{}
	plan, err := planDockerfile(ctx, instructions, buildPlanOptions{Args: opts.Args, Labels: opts.Labels},
		func(ctx context.Context, ref string) (imageConfig, error) {
			image, err := r.buildBase(ctx, ref, opts.Pull)
			if err != nil {
				return imageConfig{}, err
			}
			bases[ref] = image
			return image.Config, nil
		})
	if err != nil {
		return imageRecord{}, err
	}
	if len(plan.UnusedArgs) > 0 {
		r.logger.Warn("build arguments no ARG declares", "args", plan.UnusedArgs)
	}

	// stageImages[i] is the ID of the image stage i ended at.
	stageImages := make([]string, len(plan.Stages))
	for i := range plan.Stages {
		stage := &plan.Stages[i]
		var from string
		switch {
		case stage.Base != "":
			from = bases[stage.Base].ID
		case stage.BaseStage >= 0:
			from = stageImages[stage.BaseStage]
		}
		b := &stageBuild{r: r, plan: plan, stage: stage, stageImages: stageImages}
		if stageImages[i], err = b.run(ctx, from); err != nil {
			return imageRecord{}, err
		}
	}

	final := "sha256:" + stageImages[len(stageImages)-1]
	for _, tag := range opts.Tags {
		if err := r.backends.images.Tag(ctx, final, tag); err != nil {
			return imageRecord{}, fmt.Errorf("build: tag %s: %w", tag, err)
		}
	}
	image, err := r.backends.images.Inspect(ctx, final)
	if err != nil {
		return imageRecord{}, fmt.Errorf("build: inspect the built image: %w", err)
	}
	r.logger.Info("built image", "id", image.ID, "tags", opts.Tags)
	return image, nil
}

// buildBase is the stored base image ref, pulled when pull is set or when
// it is not stored.
func (r *Runtime) buildBase(ctx context.Context, ref string, pull bool) (imageRecord, error) {
	if !pull {
		image, err := r.backends.images.Inspect(ctx, ref)
		if err == nil {
			return image, nil
		}
		if !errors.Is(err, ErrImageNotFound) {
			return imageRecord{}, err
		}
	}
	image, err := r.backends.images.Pull(ctx, ref)
	if err != nil {
		return imageRecord{}, errors.Join(ErrImagePullFailed, err)
	}
	if err := verifyPinnedDigest(ref, image.Digests); err != nil {
		return imageRecord{}, err
	}
	return image, nil
}

// stageBuild runs one stage of a plan.
type stageBuild struct {
	r           *Runtime
	plan        *buildPlan
	stage       *buildStage
	stageImages []string
}

// run runs the stage's steps on the image from ("" for scratch) and
// returns the ID of the image the last step committed.
func (b *stageBuild) run(ctx context.Context, from string) (string, error) {
	if len(b.stage.Steps) == 0 {
		// A stage with no steps still records its configuration, which
		// takes a commit; the commit adds one empty layer.
		image, err := b.commitInRoot(ctx, from, "CONFIG", nil)
		if err != nil {
			return "", fmt.Errorf("build: stage %d: %w", b.stage.Index, err)
		}
		return image.ID, nil
	}

	current := from
	for i := range b.stage.Steps {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		step := &b.stage.Steps[i]
		var (
			image imageRecord
			err   error
		)
		switch step.Command {
		case "RUN":
			image, err = b.runStep(ctx, step, current)
		case "COPY":
			image, err = b.copyStep(ctx, step, current)
		default:
			err = fmt.Errorf("%s is not a build step", step.Command)
		}
		if err != nil {
			return "", fmt.Errorf("build: dockerfile line %d: %w", step.Line, err)
		}
		current = image.ID
	}
	return current, nil
}

// runStep runs a RUN step in a container of the runtime created from the
// image current, and commits what it changed.
//
// The step's working directory need not exist: runc creates a missing
// process cwd inside the container before it runs the process, as docker
// does for a WORKDIR.
func (b *stageBuild) runStep(ctx context.Context, step *buildStep, current string) (imageRecord, error) {
	if current == "" {
		return imageRecord{}, errors.New("RUN needs a base image with a program in it; the stage is still empty scratch")
	}
	env := make(map[string]string, len(step.Env))
	for _, entry := range step.Env {
		if key, value, _ := strings.Cut(entry, "="); key != "" {
			env[key] = value
		}
	}
	user := step.User
	if user == "" {
		// The image's USER is the stage's last one; a step before it runs as
		// root. (The image's environment is the stage's last too: a variable
		// an ENV after this step declares is set for it, one it changes
		// keeps the step's value.)
		user = "0"
	}

	r := b.r
	state, err := r.CreateContainer(ctx, &ContainerConfig{
		Name:        "build-" + r.generateContainerID()[:12],
		Image:       "sha256:" + current,
		Entrypoint:  step.Argv, // set, so the image's ENTRYPOINT and CMD are dropped
		Environment: env,
		WorkDir:     step.WorkDir,
		User:        user,
		NetworkMode: buildNetworkMode,
	})
	if err != nil {
		return imageRecord{}, err
	}
	image, err := b.runAndCommit(ctx, step, state.ID)
	if rmErr := r.RemoveContainer(context.WithoutCancel(ctx), state.ID, true); rmErr != nil {
		err = errors.Join(err, rmErr)
	}
	return image, err
}

// runAndCommit starts the created build container id, waits for it and,
// when it exits 0, commits its root filesystem.
func (b *stageBuild) runAndCommit(ctx context.Context, step *buildStep, id string) (imageRecord, error) {
	r := b.r
	if err := r.StartContainer(ctx, id); err != nil {
		return imageRecord{}, err
	}
	code, err := r.WaitContainer(ctx, id)
	if err != nil {
		stopErr := r.StopContainer(context.WithoutCancel(ctx), id, forcedRemoveStopTimeout)
		return imageRecord{}, errors.Join(err, stopErr)
	}
	if code != 0 {
		stdout, stderr, logErr := r.Logs(id)
		output := slices.Concat(stdout, stderr)
		if len(output) > buildLogTail {
			output = output[len(output)-buildLogTail:]
		}
		return imageRecord{}, errors.Join(
			fmt.Errorf("RUN exited with code %d: %s", code, strings.TrimSpace(string(output))), logErr)
	}
	return r.backends.commit.Commit(ctx, id, b.stage.Config, "RUN "+strings.Join(step.Argv, " "))
}

// commitInRoot creates a root filesystem from the image current, lets
// write change it (when set), commits it as createdBy and removes it.
func (b *stageBuild) commitInRoot(ctx context.Context, current, createdBy string,
	write func(rootfs string) error) (imageRecord, error) {
	r := b.r
	id := "build-" + r.generateContainerID()
	rootfs, err := r.backends.rootfs.Create(ctx, id, current)
	if err != nil {
		return imageRecord{}, fmt.Errorf("create a root filesystem: %w", err)
	}
	var image imageRecord
	if write != nil {
		err = write(rootfs)
	}
	if err == nil {
		image, err = r.backends.commit.Commit(ctx, id, b.stage.Config, createdBy)
	}
	if rmErr := r.backends.rootfs.Remove(id); rmErr != nil {
		err = errors.Join(err, rmErr)
	}
	return image, err
}
