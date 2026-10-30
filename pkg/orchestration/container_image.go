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
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// =============================================================================
// Image lifecycle — build, inspect, tag, push and one-shot runs
// =============================================================================
//
// The sandbox spawns containers FROM images; these operations MAKE them. They
// live on ExecContainerRuntime rather than in a second docker client because
// the engine a worker image is built on has to be the engine the workers are
// spawned on, and this is the one type that already knows which engine that
// is (docker or podman, detected once at startup).
//
// NONE OF THIS IS ON THE ContainerRuntime INTERFACE. The spawn path needs none
// of it, and the one consumer that does — the Software Factory worker image
// refresh (pkg/sdlc/workerimage) — names the narrow interface it needs. A
// remote fleet node's runtime therefore never grows a build surface.
//
// EVERY INVOCATION GOES THROUGH runImageCommand, a field for the same reason
// ExecContainerRuntime.inspect is one: the argv each operation builds and the
// output each one parses are the parts worth proving, and neither needs an
// engine installed to be proved.

// ImageBuildSpec describes one image build.
type ImageBuildSpec struct {
	// Dockerfile is the build definition, handed to the engine on stdin.
	//
	// THE BUILD CONTEXT IS EMPTY. A Dockerfile that COPYs from its context
	// cannot be built this way, and that is the point: the only inputs are the
	// Dockerfile, the build arguments and what its own RUN steps fetch, so the
	// image is a function of exactly what the caller passed.
	Dockerfile []byte

	// Tags are the references the built image is tagged with. At least one is
	// required: an untagged image is garbage the next prune removes.
	Tags []string

	// BuildArgs are passed as --build-arg KEY=VALUE.
	BuildArgs map[string]string

	// Labels are passed as --label KEY=VALUE, on top of any LABEL the
	// Dockerfile declares.
	Labels map[string]string

	// Pull re-resolves the base images, so a rebuild takes the base image's
	// security updates rather than whatever this host cached. Each base is
	// pulled into the engine's image store before the build, so the built
	// image's base layers can be pushed.
	Pull bool
}

// ImageInfo is what an inspect reports about one local image.
type ImageInfo struct {
	// ID is the engine's local image ID ("sha256:<hex>"): the identity a
	// container records and the value a gate pins to.
	ID string

	// Labels are the image's configuration labels.
	Labels map[string]string

	// RepoDigests are the "repository@sha256:<hex>" manifest digests the image
	// is known by in registries it was pushed to or pulled from.
	RepoDigests []string
}

// ImageRunSpec is one foreground, auto-removed container run whose output is
// captured. It is how an image is PROBED: the command's exit status and output
// are the answer, so a non-zero exit is reported on the result rather than
// raised as an error.
type ImageRunSpec struct {
	// Image is the reference to run — a tag, or an image ID to pin the run to
	// exactly one build.
	Image string

	// User is the --user uid:gid the command runs as. Empty takes the image's.
	User string

	// Env is the command's environment, written to a private 0600 env file
	// rather than to argv.
	Env map[string]string

	// NetworkNone runs the command with --network none.
	NetworkNone bool

	// Mounts are bind mounts, in the spawn path's own shape.
	Mounts []ContainerMount

	// Command is the argv run in place of the image's command. Required.
	Command []string

	// Security, when set, runs the command under that sandbox policy —
	// rendered by SandboxSecurity.Apply and ContainerSpec.PrivilegeRunArgs,
	// the same two calls a spawn makes — so a probe proves the boundary a
	// worker actually runs under rather than a copy of it.
	Security *SandboxSecurity

	// Stdin is fed to the command's standard input (the run gains -i). Nil
	// runs it with none.
	Stdin []byte
}

// RegistryAuth is the credential a push logs in with.
//
// It is used for one push and nothing else: the login is written to a private
// client configuration directory created for that push and removed after it,
// so the host's own engine configuration never holds it.
type RegistryAuth struct {
	// Username is the registry account.
	Username string

	// Secret is the account's password or token.
	Secret []byte `json:"secret,omitempty"`
}

// imageCommand is one engine invocation.
type imageCommand struct {
	// Args are the engine's arguments, the binary excluded.
	Args []string

	// Stdin is fed to the process when non-nil.
	Stdin []byte
}

// imageCommandResult is what one invocation produced. A non-zero exit is
// carried here; an error from the runner means the process could not be run
// at all.
type imageCommandResult struct {
	ExitCode int
	Stdout   string
	Stderr   string
}

// imageCommandRunner runs one engine invocation.
type imageCommandRunner func(ctx context.Context, command string, cmd imageCommand) (imageCommandResult, error)

// execImageCommand is the production runner: the engine binary, exec'd.
func execImageCommand(ctx context.Context, command string, cmd imageCommand) (imageCommandResult, error) {
	proc := exec.CommandContext(ctx, command, cmd.Args...)
	var stdout, stderr bytes.Buffer
	proc.Stdout = &stdout
	proc.Stderr = &stderr
	if cmd.Stdin != nil {
		proc.Stdin = bytes.NewReader(cmd.Stdin)
	}
	err := proc.Run()
	result := imageCommandResult{Stdout: stdout.String(), Stderr: stderr.String()}
	var exitErr *exec.ExitError
	switch {
	case err == nil:
		return result, nil
	case errors.As(err, &exitErr) && ctx.Err() == nil:
		result.ExitCode = exitErr.ExitCode()
		return result, nil
	default:
		return result, err
	}
}

// imageRunner returns the runner this runtime drives the engine through.
func (r *ExecContainerRuntime) imageRunner() imageCommandRunner {
	if r.runImageCommand != nil {
		return r.runImageCommand
	}
	return execImageCommand
}

// BuildImage builds spec and returns the built image's local ID.
//
// The ID is read from the engine's --iidfile rather than parsed from its
// progress output, which differs between docker's two builders and podman.
func (r *ExecContainerRuntime) BuildImage(ctx context.Context, spec *ImageBuildSpec) (string, error) {
	if err := validateImageBuildSpec(spec); err != nil {
		return "", err
	}
	workDir, err := os.MkdirTemp("", "quicbaas-image-build-")
	if err != nil {
		return "", &ImageBuildError{Tags: spec.Tags, Cause: err}
	}
	defer func() {
		if removeErr := os.RemoveAll(workDir); removeErr != nil {
			r.logger.Warnf("container build: remove scratch directory %s: %v", workDir, removeErr)
		}
	}()
	contextDir := filepath.Join(workDir, "context")
	if err := os.Mkdir(contextDir, 0o700); err != nil {
		return "", &ImageBuildError{Tags: spec.Tags, Cause: err}
	}
	iidFile := filepath.Join(workDir, "iid")

	// A Pull build pulls its bases into the engine's image store first. A
	// `build --pull` alone fetches them into the builder's cache only, and on
	// the containerd image store the builder may keep just the unpacked
	// snapshot: the build succeeds, but the push then fails with "content
	// digest ... not found" for every base layer.
	buildSpec := spec
	if spec.Pull {
		allPulled, err := r.pullBaseImages(ctx, spec)
		if err != nil {
			return "", err
		}
		if allPulled {
			// Every base is now fresh in the image store; building from it,
			// rather than re-resolving each tag, keeps the built image's base
			// layers exactly the blobs the pull stored.
			withoutPull := *spec
			withoutPull.Pull = false
			buildSpec = &withoutPull
		}
	}

	args := buildImageArgs(buildSpec, iidFile, contextDir)
	r.logger.Infof("container build: %s %s", r.command, strings.Join(spec.Tags, " "))
	result, err := r.imageRunner()(ctx, r.command, imageCommand{Args: args, Stdin: spec.Dockerfile})
	if err != nil {
		return "", &ImageBuildError{Tags: spec.Tags, Cause: err}
	}
	if result.ExitCode != 0 {
		return "", &ImageBuildError{
			Tags:  spec.Tags,
			Cause: &CommandError{Command: r.command, Op: "build", Output: strings.TrimSpace(result.Stderr)},
		}
	}
	id, err := os.ReadFile(iidFile) // #nosec G304 -- a path this function created under its own temp dir.
	if err != nil {
		return "", &ImageBuildError{Tags: spec.Tags, Cause: err}
	}
	imageID := strings.TrimSpace(string(id))
	if imageID == "" {
		return "", &ImageBuildError{Tags: spec.Tags, Cause: errImageBuildNoID}
	}
	return imageID, nil
}

// pullBaseImages pulls each base image the Dockerfile names into the engine's
// image store. It reports whether every base was pulled: a base only the build
// can resolve (see dockerfileBaseImages) still needs --pull.
func (r *ExecContainerRuntime) pullBaseImages(ctx context.Context, spec *ImageBuildSpec) (bool, error) {
	bases, allLiteral := dockerfileBaseImages(spec.Dockerfile)
	for _, base := range bases {
		r.logger.Infof("container build: %s pull %s", r.command, base)
		result, err := r.imageRunner()(ctx, r.command, imageCommand{Args: []string{"pull", base}})
		if err != nil {
			return false, &ImageBuildError{Tags: spec.Tags, Cause: err}
		}
		if result.ExitCode != 0 {
			return false, &ImageBuildError{
				Tags:  spec.Tags,
				Cause: &CommandError{Command: r.command, Op: "pull", Output: strings.TrimSpace(result.Stderr)},
			}
		}
	}
	return allLiteral, nil
}

// dockerfileBaseImages returns the distinct registry images the Dockerfile's
// FROM instructions name, in order. It leaves out `scratch` and earlier build
// stages, which are not pulled. It reports false when a FROM carries a flag
// such as --platform or spells its image with a build argument: only the build
// itself can resolve that base.
func dockerfileBaseImages(dockerfile []byte) ([]string, bool) {
	var bases []string
	allLiteral := true
	seen := map[string]bool{"scratch": true}
	for _, line := range strings.Split(string(dockerfile), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || !strings.EqualFold(fields[0], "FROM") {
			continue
		}
		operands := fields[1:]
		flagged := false
		for len(operands) > 0 && strings.HasPrefix(operands[0], "--") {
			flagged = true
			operands = operands[1:]
		}
		if len(operands) == 0 {
			continue
		}
		image := operands[0]
		switch {
		case flagged || strings.Contains(image, "$"):
			// A --platform base or one spelled with a build argument is
			// resolved by the build, not by a plain pull.
			allLiteral = false
		case !seen[strings.ToLower(image)]:
			seen[strings.ToLower(image)] = true
			bases = append(bases, image)
		}
		if len(operands) >= 3 && strings.EqualFold(operands[1], "AS") {
			// A later FROM naming this stage builds on it, not on an image.
			seen[strings.ToLower(operands[2])] = true
		}
	}
	return bases, allLiteral
}

// errImageBuildNoID reports an engine that succeeded without naming the image
// it built.
var errImageBuildNoID = errors.New("the engine reported success but wrote no image ID")

// validateImageBuildSpec refuses a build that could not produce a usable image.
func validateImageBuildSpec(spec *ImageBuildSpec) error {
	switch {
	case spec == nil:
		return &InputValidationError{Field: "spec", Message: "cannot be nil"}
	case len(bytes.TrimSpace(spec.Dockerfile)) == 0:
		return &InputValidationError{Field: "dockerfile", Message: "cannot be empty"}
	case len(spec.Tags) == 0:
		return &InputValidationError{Field: "tags", Message: "at least one tag is required; an untagged image is pruned"}
	}
	for _, tag := range spec.Tags {
		if strings.TrimSpace(tag) == "" {
			return &InputValidationError{Field: "tags", Message: "a tag cannot be empty"}
		}
	}
	return nil
}

// buildImageArgs is the build's argv, in a stable order so two builds of one
// spec are the same invocation.
func buildImageArgs(spec *ImageBuildSpec, iidFile, contextDir string) []string {
	args := []string{"build", "--file", "-", "--iidfile", iidFile}
	if spec.Pull {
		args = append(args, "--pull")
	}
	for _, tag := range spec.Tags {
		args = append(args, "--tag", tag)
	}
	for _, key := range sortedKeys(spec.BuildArgs) {
		args = append(args, "--build-arg", key+"="+spec.BuildArgs[key])
	}
	for _, key := range sortedKeys(spec.Labels) {
		args = append(args, "--label", key+"="+spec.Labels[key])
	}
	return append(args, contextDir)
}

// InspectImage reports a local image's ID, labels and repository digests. An
// image this host does not hold is an ImageNotFoundError.
func (r *ExecContainerRuntime) InspectImage(ctx context.Context, image string) (*ImageInfo, error) {
	if strings.TrimSpace(image) == "" {
		return nil, &InputValidationError{Field: "image", Message: "cannot be empty"}
	}
	result, err := r.imageRunner()(ctx, r.command, imageCommand{Args: []string{"image", "inspect", image}})
	if err != nil {
		return nil, &ImageInspectError{Image: image, Cause: err}
	}
	if result.ExitCode != 0 {
		if isImageNotFoundOutput(result.Stderr) {
			return nil, &ImageNotFoundError{Image: image}
		}
		return nil, &ImageInspectError{
			Image: image,
			Cause: &CommandError{Command: r.command, Op: "image inspect", Output: strings.TrimSpace(result.Stderr)},
		}
	}
	return parseImageInspect(image, result.Stdout)
}

// imageInspectRecord is the part of an engine's image inspect document this
// package reads. docker and podman agree on every field named here.
type imageInspectRecord struct {
	ID          string   `json:"Id"`
	RepoDigests []string `json:"RepoDigests"`
	Config      struct {
		Labels map[string]string `json:"Labels"`
	} `json:"Config"`
}

// parseImageInspect decodes an inspect document: a JSON array holding one
// record per reference asked about.
func parseImageInspect(image, out string) (*ImageInfo, error) {
	var records []imageInspectRecord
	if err := json.Unmarshal([]byte(out), &records); err != nil {
		return nil, &ImageInspectError{Image: image, Cause: err}
	}
	if len(records) == 0 || strings.TrimSpace(records[0].ID) == "" {
		return nil, &ImageNotFoundError{Image: image}
	}
	record := records[0]
	labels := record.Config.Labels
	if labels == nil {
		labels = map[string]string{}
	}
	return &ImageInfo{ID: record.ID, Labels: labels, RepoDigests: record.RepoDigests}, nil
}

// isImageNotFoundOutput recognises the engines' own words for a missing
// image, and nothing looser — see isNotFoundOutput for why.
func isImageNotFoundOutput(stderr string) bool {
	lower := strings.ToLower(stderr)
	return strings.Contains(lower, "no such image") || strings.Contains(lower, "image not known")
}

// TagImage adds target as a reference to the image source names.
func (r *ExecContainerRuntime) TagImage(ctx context.Context, source, target string) error {
	if strings.TrimSpace(source) == "" {
		return &InputValidationError{Field: "source", Message: "cannot be empty"}
	}
	if strings.TrimSpace(target) == "" {
		return &InputValidationError{Field: "target", Message: "cannot be empty"}
	}
	result, err := r.imageRunner()(ctx, r.command, imageCommand{Args: []string{"tag", source, target}})
	if err != nil {
		return &ImageTagError{Source: source, Target: target, Cause: err}
	}
	if result.ExitCode != 0 {
		return &ImageTagError{
			Source: source, Target: target,
			Cause: &CommandError{Command: r.command, Op: "tag", Output: strings.TrimSpace(result.Stderr)},
		}
	}
	return nil
}

// PushImage pushes image to its registry and returns the manifest digest the
// registry stored it under ("sha256:<hex>").
//
// auth, when non-nil, is logged in to a PRIVATE client configuration created
// for this push and deleted after it; nil pushes with whatever credentials the
// engine's own configuration already holds for the registry.
func (r *ExecContainerRuntime) PushImage(ctx context.Context, image string, auth *RegistryAuth) (string, error) {
	if strings.TrimSpace(image) == "" {
		return "", &InputValidationError{Field: "image", Message: "cannot be empty"}
	}
	dialect, ok := registryDialects[r.command]
	if !ok {
		return "", &ImagePushError{Image: image, Cause: &ContainerRuntimeNotAvailableError{
			Message: r.command + " has no registry push support in this build",
		}}
	}
	workDir, err := os.MkdirTemp("", "quicbaas-image-push-")
	if err != nil {
		return "", &ImagePushError{Image: image, Cause: err}
	}
	defer func() {
		if removeErr := os.RemoveAll(workDir); removeErr != nil {
			r.logger.Warnf("container push: remove scratch directory %s: %v", workDir, removeErr)
		}
	}()

	var configArgs []string
	if auth != nil {
		if configArgs, err = r.registryLogin(ctx, dialect, workDir, RegistryHost(image), auth); err != nil {
			return "", &ImagePushError{Image: image, Cause: err}
		}
	}

	digestFile := filepath.Join(workDir, "digest")
	args := dialect.pushArgs(configArgs, image, digestFile)
	r.logger.Infof("container push: %s %s", r.command, image)
	result, err := r.imageRunner()(ctx, r.command, imageCommand{Args: args})
	if err != nil {
		return "", &ImagePushError{Image: image, Cause: err}
	}
	if result.ExitCode != 0 {
		return "", &ImagePushError{
			Image: image,
			Cause: &CommandError{Command: r.command, Op: "push", Output: strings.TrimSpace(result.Stderr)},
		}
	}
	digest, err := dialect.readDigest(result.Stdout, digestFile)
	if err != nil {
		return "", &ImagePushError{Image: image, Cause: err}
	}
	if err := ValidateImageDigest(digest); err != nil {
		return "", &ImagePushError{Image: image, Cause: err}
	}
	return digest, nil
}

// registryLogin logs auth in to host under a private configuration in dir and
// returns the arguments that point a later invocation at that configuration.
func (r *ExecContainerRuntime) registryLogin(ctx context.Context, dialect registryDialect, dir, host string, auth *RegistryAuth) ([]string, error) {
	if strings.TrimSpace(auth.Username) == "" || len(auth.Secret) == 0 {
		return nil, &InputValidationError{Field: "auth", Message: "a registry login needs a username and a secret"}
	}
	configArgs := dialect.configArgs(dir)
	args := dialect.loginArgs(configArgs, host, auth.Username)
	result, err := r.imageRunner()(ctx, r.command, imageCommand{Args: args, Stdin: auth.Secret})
	if err != nil {
		return nil, &RegistryLoginError{Registry: host, Cause: err}
	}
	if result.ExitCode != 0 {
		return nil, &RegistryLoginError{
			Registry: host,
			Cause:    &CommandError{Command: r.command, Op: "login", Output: strings.TrimSpace(result.Stderr)},
		}
	}
	return configArgs, nil
}

// registryDialect is how one engine spells a private login and a push that
// reports its digest. docker and podman differ in both.
type registryDialect struct {
	// configArgs points an invocation at a private client configuration
	// rooted in dir.
	configArgs func(dir string) []string

	// loginArgs is a --password-stdin login under that configuration.
	loginArgs func(configArgs []string, host, username string) []string

	// pushArgs is the push under that configuration.
	pushArgs func(configArgs []string, image, digestFile string) []string

	// readDigest extracts the pushed manifest digest.
	readDigest func(stdout, digestFile string) (string, error)
}

// registryDialects maps an engine binary to its dialect.
var registryDialects = map[string]registryDialect{
	RuntimeDocker: {
		// docker's configuration directory is a GLOBAL flag, so it precedes
		// the subcommand.
		configArgs: func(dir string) []string { return []string{"--config", dir} },
		loginArgs: func(configArgs []string, host, username string) []string {
			return append(append([]string{}, configArgs...), "login", "--username", username, "--password-stdin", host)
		},
		pushArgs: func(configArgs []string, image, _ string) []string {
			return append(append([]string{}, configArgs...), "push", image)
		},
		readDigest: func(stdout, _ string) (string, error) { return parsePushDigest(stdout) },
	},
	RuntimePodman: {
		// podman's is an --authfile on each subcommand, and its push writes
		// the digest to a file rather than printing it.
		configArgs: func(dir string) []string { return []string{"--authfile", filepath.Join(dir, "auth.json")} },
		loginArgs: func(configArgs []string, host, username string) []string {
			return append(append([]string{"login"}, configArgs...), "--username", username, "--password-stdin", host)
		},
		pushArgs: func(configArgs []string, image, digestFile string) []string {
			return append(append([]string{"push"}, configArgs...), "--digestfile", digestFile, image)
		},
		readDigest: func(_, digestFile string) (string, error) {
			raw, err := os.ReadFile(digestFile) // #nosec G304 -- a path PushImage created under its own temp dir.
			if err != nil {
				return "", err
			}
			return strings.TrimSpace(string(raw)), nil
		},
	},
}

// pushDigestPattern matches the line docker prints when a push completes:
// "<tag>: digest: sha256:<hex> size: <n>".
var pushDigestPattern = regexp.MustCompile(`digest: (sha256:[a-f0-9]{64})`)

// errPushNoDigest reports a push that succeeded without naming its digest.
var errPushNoDigest = errors.New("the push succeeded but reported no manifest digest")

// parsePushDigest reads the manifest digest from docker push's output. The
// LAST match wins: a push of several tags reports each, and the one asked
// about is the one printed last.
func parsePushDigest(stdout string) (string, error) {
	matches := pushDigestPattern.FindAllStringSubmatch(stdout, -1)
	if len(matches) == 0 {
		return "", errPushNoDigest
	}
	return matches[len(matches)-1][1], nil
}

// RegistryHost is the registry an image reference names: its first path
// component when that looks like a host (it carries a dot or a port, or is
// "localhost"), and Docker Hub otherwise.
func RegistryHost(image string) string {
	repository := ImageRepository(image)
	first, _, found := strings.Cut(repository, "/")
	if found && (strings.ContainsAny(first, ".:") || first == "localhost") {
		return first
	}
	return "docker.io"
}

// RunImage runs spec.Command in a fresh, auto-removed container and reports
// what it did. A non-zero exit is on the result; an error means the run could
// not be made at all.
func (r *ExecContainerRuntime) RunImage(ctx context.Context, spec *ImageRunSpec) (*ExecResult, error) {
	if spec == nil {
		return nil, &InputValidationError{Field: "spec", Message: "cannot be nil"}
	}
	if strings.TrimSpace(spec.Image) == "" {
		return nil, &InputValidationError{Field: "image", Message: "cannot be empty"}
	}
	if len(spec.Command) == 0 {
		return nil, &InputValidationError{Field: "command", Message: "cannot be empty"}
	}
	for i, m := range spec.Mounts {
		if err := m.Validate(); err != nil {
			return nil, &InputValidationError{Field: "mounts[" + intToStr(i) + "]", Message: err.Error()}
		}
	}
	boundary, err := sandboxRunArgs(spec.Security)
	if err != nil {
		return nil, err
	}
	envFile, err := writeContainerEnvFile(spec.Env)
	if err != nil {
		return nil, err
	}
	if envFile.Cleanup != nil {
		defer envFile.Cleanup()
	}
	suffix, err := generateContainerID()
	if err != nil {
		return nil, &ContainerStartError{Image: spec.Image, Cause: err}
	}
	name := oneShotContainerPrefix + suffix
	args := runImageArgs(spec, name, envFile.Path, boundary)
	result, err := r.imageRunner()(ctx, r.command, imageCommand{Args: args, Stdin: spec.Stdin})
	if err != nil || ctx.Err() != nil {
		// A cancelled run killed the engine CLIENT, not the container: --rm
		// removes a container when IT exits, and a hung probe never does.
		// Remove it by name on a context of its own — the caller's is the
		// one that just ended — bounded so a wedged engine cannot hold the
		// caller forever.
		r.removeOneShot(ctx, name)
	}
	if err != nil {
		return nil, &ContainerStartError{Image: spec.Image, Cause: err}
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return nil, &ContainerStartError{Image: spec.Image, Cause: ctxErr}
	}
	return &ExecResult{ExitCode: result.ExitCode, Stdout: result.Stdout, Stderr: result.Stderr}, nil
}

// oneShotContainerPrefix names every container RunImage starts, so a leftover
// one is recognisable on the host.
const oneShotContainerPrefix = "quicbaas-oneshot-"

// oneShotCleanupTimeout bounds the forced removal of a one-shot container
// whose run was cut short.
const oneShotCleanupTimeout = 30 * time.Second

// removeOneShot force-removes the named one-shot container on a context
// detached from parent's cancellation and bounded by oneShotCleanupTimeout.
// A container already gone (the run finished and --rm removed it) is not a
// failure; anything else is logged, because the run's own error is the one
// the caller must see.
func (r *ExecContainerRuntime) removeOneShot(parent context.Context, name string) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), oneShotCleanupTimeout)
	defer cancel()
	result, err := r.imageRunner()(ctx, r.command, imageCommand{Args: []string{"rm", "--force", name}})
	switch {
	case err != nil:
		r.logger.Warnf("one-shot container %s may still be running: rm --force: %v", name, err)
	case result.ExitCode != 0 && !isNotFoundOutput(result.Stderr):
		r.logger.Warnf("one-shot container %s may still be running: rm --force exited %d: %s",
			name, result.ExitCode, strings.TrimSpace(result.Stderr))
	}
}

// sandboxRunArgs renders policy's run flags exactly as a spawn would; a nil
// policy renders none.
func sandboxRunArgs(policy *SandboxSecurity) ([]string, error) {
	if policy == nil {
		return nil, nil
	}
	spec := &ContainerSpec{}
	if err := policy.Apply(spec); err != nil {
		return nil, err
	}
	return spec.PrivilegeRunArgs(), nil
}

// runImageArgs is a one-shot run's argv; name is the container's name, and
// boundary is the sandbox flags, placed before the image.
func runImageArgs(spec *ImageRunSpec, name, envFile string, boundary []string) []string {
	args := []string{"run", "--rm"}
	if spec.Stdin != nil {
		args = append(args, "-i")
	}
	args = append(args, "--name", name)
	if spec.User != "" {
		args = append(args, "--user", spec.User)
	}
	if spec.NetworkNone {
		args = append(args, "--network", "none")
	}
	if envFile != "" {
		args = append(args, "--env-file", envFile)
	}
	for _, m := range spec.Mounts {
		volume := m.Source + ":" + m.Target
		if m.ReadOnly {
			volume += ":ro"
		}
		args = append(args, "-v", volume)
	}
	args = append(args, boundary...)
	args = append(args, spec.Image)
	return append(args, spec.Command...)
}
