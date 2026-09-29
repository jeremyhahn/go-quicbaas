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

package workerimage

import (
	"cmp"
	"context"
	"errors"
	"log/slog"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"golang.org/x/mod/semver"

	softwarefactoryworker "github.com/jeremyhahn/go-quicbaas/deployments/agents/software-factory-worker"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/sdlc"
)

// Engine is the image lifecycle a refresh drives;
// orchestration.ExecContainerRuntime satisfies it.
type Engine interface {
	ImageRunner
	BuildImage(ctx context.Context, spec *orchestration.ImageBuildSpec) (string, error)
	InspectImage(ctx context.Context, image string) (*orchestration.ImageInfo, error)
	TagImage(ctx context.Context, source, target string) error
	PushImage(ctx context.Context, image string, auth *orchestration.RegistryAuth) (string, error)
}

// Bindings lists SDLC bindings, which is how a pass learns which Agents the
// factory runs and which project a smoke of each runs under;
// service.SDLCService satisfies it.
type Bindings interface {
	ListBindings(ctx context.Context, tenantID string) ([]*sdlc.ProjectBinding, error)
}

// Agents reads the tenant agent catalog and repoints an Agent's image. An
// image is a property of the Agent, so a promotion is one Agent write however
// many bindings use it; service.AgentService satisfies it, and promotion goes
// through it rather than the store so the write is validated like any other.
type Agents interface {
	Catalogue(ctx context.Context, tenantID string) (orchestration.AgentCatalogue, error)
	ReplaceAgentImage(ctx context.Context, tenantID, agentID string, stale []string, image string) (bool, error)
}

// CredentialSource yields the registry push credential, read when a push
// needs it so a rotated secret is picked up without a restart.
type CredentialSource interface {
	Credential(ctx context.Context) (*orchestration.RegistryAuth, error)
}

// FleetNodes names a tenant's fleet nodes that are NOT this one: the nodes
// whose container engines cannot see an image this host built, and so the
// ones a promotion to a host-local tag would strand.
type FleetNodes interface {
	RemoteFleetNodes(ctx context.Context, tenantID string) ([]string, error)
}

// Auditor records the refresh on the audit trail.
type Auditor interface {
	RecordWorkerImageEvent(ctx context.Context, event AuditEvent)
}

// The audited actions.
const (
	// AuditActionRefresh is one pass that attempted a build.
	AuditActionRefresh = "worker_image:refresh"

	// AuditActionPromote is one binding rebound to a new image.
	AuditActionPromote = "worker_image:promote"
)

// AuditEvent is one entry the refresh puts on the audit trail.
type AuditEvent struct {
	// TenantID is the tenant the entry belongs to: the binding's tenant for a
	// promotion, empty (the system tenant) for a pass.
	TenantID string

	// Action is AuditActionRefresh or AuditActionPromote.
	Action string

	// Resource is the project a promotion rebound, or the candidate tag.
	Resource string

	Success bool
	Details map[string]string
}

// HostResolver names the fleet node a smoke must run on: the one whose
// container engine holds the candidate, which is the one registered from this
// node. The answer is matched by the orchestrator's host affinity (an agent's
// name or host).
type HostResolver interface {
	LocalFleetNodeHost(ctx context.Context, tenantID, backend string) (string, error)
}

// Observer records pass outcomes as metrics.
type Observer interface {
	ObserveRefresh(outcome Outcome, finished time.Time)
}

// DefaultRepository is the local repository the worker image is built into —
// the name `make build-worker-image` uses.
const DefaultRepository = "quicbaas-software-factory-worker"

// DefaultSmokeBackend is the backend the default-model smoke runs on.
const DefaultSmokeBackend = "claude"

// Config is what a refresher builds, where it publishes and how it smokes.
type Config struct {
	// Repository is the local repository candidates are built into. Bindings
	// naming an image of this repository — or of Registry — are MANAGED: the
	// refresh keeps them current. Any other image is the operator's own and is
	// never touched.
	Repository string

	// Registry is the repository ("host[:port]/owner/name") promoted images
	// are pushed to. Empty is local mode: Agents are repointed to the local
	// tag, which only this host's engine holds — and so a local-mode pass
	// refuses to promote in a tenant whose fleet has any other node.
	//
	// With a registry, an Agent that still names a LOCAL tag of Repository is
	// unpublished: the pass pushes that same image, unrebuilt, and repoints
	// the Agent to its digest-pinned registry reference.
	Registry string

	// Credential logs the push in; nil pushes with the engine's own
	// configuration.
	Credential CredentialSource

	// SmokeModel is the model the default Claude smoke asks, whatever the
	// bindings name.
	SmokeModel string

	// SmokeHost pins every smoke to one named fleet node. Empty resolves the
	// agent registered from this node per smoke (Deps.Hosts), which is right
	// unless that agent registers under a name this node cannot recognise.
	SmokeHost string

	// Base is the QuicBaaS release the embedded Dockerfile came from.
	Base string

	// Pins are the toolchain versions this binary was linked with.
	Pins softwarefactoryworker.Pins

	// UID and GID are the ids the image's agent user is created with and the
	// probes run as: the ids sandboxed spawns run as.
	UID int
	GID int
}

// validate checks the configuration can drive a pass.
func (c Config) validate() error {
	switch {
	case !validRepository(c.Repository):
		return &InvalidConfigError{Field: "repository", Message: "must be a repository name with no tag or digest"}
	case c.Registry != "" && (!validRepository(c.Registry) || !strings.Contains(c.Registry, "/")):
		return &InvalidConfigError{Field: "registry", Message: "must be host[:port]/owner/name with no tag or digest"}
	case strings.TrimSpace(c.SmokeModel) == "":
		return &InvalidConfigError{Field: "smoke_model", Message: "the default Claude model is what every refresh must prove works"}
	case c.UID < 0 || c.GID < 0:
		return &InvalidConfigError{Field: "uid/gid", Message: "must not be negative"}
	}
	if err := softwarefactoryworker.ValidateVersion("base", c.Base); err != nil {
		return err
	}
	return c.Pins.Validate()
}

// validRepository reports whether ref names a repository and nothing more.
func validRepository(ref string) bool {
	ref = strings.TrimSpace(ref)
	if ref == "" || strings.Contains(ref, "@") || strings.ContainsAny(ref, " \t") {
		return false
	}
	return repositoryOf(ref) == ref
}

// repositoryOf strips a reference's digest and tag.
func repositoryOf(ref string) string {
	repository := orchestration.ImageRepository(ref)
	if i := strings.LastIndex(repository, ":"); i > strings.LastIndex(repository, "/") {
		return repository[:i]
	}
	return repository
}

// Deps are the refresher's collaborators; all are required.
type Deps struct {
	Engine   Engine
	Versions VersionSource
	Tenants  orchestration.TenantLister
	Bindings Bindings
	Agents   Agents
	Smoker   Smoker
	Auditor  Auditor
	Observer Observer
	Logger   *slog.Logger

	// Fleet answers whether a tenant's fleet reaches past this host, which
	// is what a local-mode promotion must never strand.
	Fleet FleetNodes

	// Hosts resolves the local agent for a smoke. Required unless
	// Config.SmokeHost names one.
	Hosts HostResolver
}

// validate checks every collaborator is present.
func (d Deps) validate() error {
	required := []struct {
		field   string
		missing bool
	}{
		{"engine", d.Engine == nil}, {"versions", d.Versions == nil}, {"tenants", d.Tenants == nil},
		{"bindings", d.Bindings == nil}, {"agents", d.Agents == nil}, {"smoker", d.Smoker == nil}, {"auditor", d.Auditor == nil},
		{"observer", d.Observer == nil}, {"logger", d.Logger == nil}, {"fleet", d.Fleet == nil},
	}
	for _, r := range required {
		if r.missing {
			return &InvalidConfigError{Field: r.field, Message: "required"}
		}
	}
	return nil
}

// Refresher keeps the worker image current. See the package documentation.
type Refresher struct {
	cfg  Config
	deps Deps

	// kick carries a requested pass to the loop; capacity one, so a second
	// request while one is queued is refused rather than stacked.
	kick chan Trigger

	serving      atomic.Bool
	running      atomic.Bool
	runningSince atomic.Int64

	last atomic.Pointer[Report]
	now  func() time.Time
}

// NewRefresher validates its configuration and collaborators.
func NewRefresher(cfg Config, deps Deps) (*Refresher, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	if err := deps.validate(); err != nil {
		return nil, err
	}
	if cfg.SmokeHost == "" && deps.Hosts == nil {
		return nil, &InvalidConfigError{Field: "hosts", Message: "required when no smoke host is configured"}
	}
	return &Refresher{cfg: cfg, deps: deps, kick: make(chan Trigger, 1), now: time.Now}, nil
}

// Status reports whether a pass is running or queued, and the last report.
func (r *Refresher) Status() Status {
	status := Status{Serving: r.serving.Load(), Running: r.running.Load(), Queued: len(r.kick) > 0, Last: r.last.Load()}
	if since := r.runningSince.Load(); status.Running && since != 0 {
		status.RunningSince = time.Unix(0, since)
	}
	return status
}

// RequestRefresh queues a full pass for the loop, returning at once. It is
// refused while a pass runs or is already queued, and when no loop is running
// to pick it up.
func (r *Refresher) RequestRefresh() error {
	if !r.serving.Load() {
		return &RefresherNotRunningError{}
	}
	if r.running.Load() {
		return &RefreshInProgressError{Since: time.Unix(0, r.runningSince.Load())}
	}
	select {
	case r.kick <- TriggerManual:
		return nil
	default:
		return &RefreshInProgressError{}
	}
}

// Refresh runs one full pass now and reports it. The npm registry is read
// here and nowhere else: once per pass.
func (r *Refresher) Refresh(ctx context.Context, trigger Trigger) *Report {
	started := r.now()
	r.running.Store(true)
	r.runningSince.Store(started.UnixNano())
	report := &Report{Trigger: trigger, StartedAt: started}
	err := r.run(ctx, report)
	// The pass is over before it is reported, so whoever the report reaches
	// can already request the next one.
	r.runningSince.Store(0)
	r.running.Store(false)
	r.finish(ctx, report, err)
	return report
}

// imageUse is one Agent a binding runs, in a managed image: the Agent that is
// repointed, and the project a smoke of it runs under.
type imageUse struct {
	TenantID  string
	ProjectID string
	AgentID   string
	Backend   string
	Model     string
	Image     string
}

// run is the pass itself; see the package documentation for its steps.
//
// A pass that returns an error has changed no Agent. Stale images are
// refreshed first, because that path can fail as a whole; publishing images
// that are already current comes after, and a failure there is one Agent's
// failed promotion, never the pass's.
func (r *Refresher) run(ctx context.Context, report *Report) error {
	fetched, err := LatestAgents(ctx, r.deps.Versions)
	if err != nil {
		return err
	}
	latest := &fetched
	report.Latest = *latest

	uses, err := r.managedUses(ctx, report)
	if err != nil {
		return err
	}
	stale, bound, err := r.staleImages(ctx, uses, *latest)
	if err != nil {
		return err
	}
	report.Stale, report.Bound = stale, bound
	unpublished := r.unpublishedUses(uses, stale)
	if len(stale) == 0 && len(unpublished) == 0 {
		report.Outcome = OutcomeCurrent
		if len(report.Skipped) > 0 {
			// Current only as far as the pass could see: the tenants it could
			// not read were judged by nobody.
			report.Outcome = OutcomePartial
			report.Error = strconv.Itoa(len(report.Skipped)) + " tenants could not be read; " +
				"the bound images of every tenant that could be read are current"
		}
		return nil
	}
	if len(stale) > 0 {
		if err := r.refreshStale(ctx, report, uses, stale, *latest); err != nil {
			return err
		}
	}
	r.publishHeld(ctx, report, unpublished)
	r.settle(report)
	return nil
}

// refreshStale builds, gates and publishes a candidate carrying latest, and
// repoints every Agent naming a stale image to it.
func (r *Refresher) refreshStale(ctx context.Context, report *Report, uses []imageUse, stale []string,
	latest softwarefactoryworker.Agents) error {
	if err := refuseDowngrade(report.Bound, latest); err != nil {
		return err
	}
	staleUses := slices.DeleteFunc(slices.Clone(uses), func(u imageUse) bool { return !slices.Contains(stale, u.Image) })
	if r.cfg.Registry == "" {
		if err := r.refuseStrandingRemoteNodes(ctx, staleUses); err != nil {
			return err
		}
	}

	build := softwarefactoryworker.Build{Base: r.cfg.Base, Agents: latest, Pins: r.cfg.Pins, UID: r.cfg.UID, GID: r.cfg.GID}
	if err := build.Validate(); err != nil {
		return err
	}
	localRef := r.cfg.Repository + ":" + build.Tag()
	report.Candidate = localRef
	imageID, err := r.candidate(ctx, build, localRef)
	if err != nil {
		return err
	}
	report.ImageID = imageID
	if err := r.gate(ctx, report, build, imageID, localRef, staleUses); err != nil {
		return err
	}
	reference, digest, err := r.publish(ctx, imageID, build, localRef)
	if err != nil {
		return err
	}
	report.Reference, report.Digest = reference, digest
	for _, use := range staleUses {
		r.repoint(ctx, report, use, reference, nil)
	}
	return nil
}

// refuseStrandingRemoteNodes refuses a local-mode promotion in any tenant
// whose fleet has a node other than this one. Checked before the build: a
// pass that may not promote has no reason to spend an hour building.
func (r *Refresher) refuseStrandingRemoteNodes(ctx context.Context, staleUses []imageUse) error {
	var tenants []string
	for _, use := range staleUses {
		if !slices.Contains(tenants, use.TenantID) {
			tenants = append(tenants, use.TenantID)
		}
	}
	slices.Sort(tenants)
	for _, tenantID := range tenants {
		nodes, err := r.deps.Fleet.RemoteFleetNodes(orchestration.ContextWithTenant(ctx, tenantID), tenantID)
		if err != nil {
			return err
		}
		if len(nodes) > 0 {
			nodes = slices.Clone(nodes)
			slices.Sort(nodes)
			return &LocalPromotionRefusedError{TenantID: tenantID, Nodes: nodes}
		}
	}
	return nil
}

// unpublishedUses lists, when a registry is configured, the Agents that name
// a CURRENT image by a local tag of Repository: an image no other node can
// pull, which needs publishing rather than rebuilding. Stale images are left
// to the refresh, which replaces them with a published candidate anyway.
func (r *Refresher) unpublishedUses(uses []imageUse, stale []string) []imageUse {
	if r.cfg.Registry == "" {
		return nil
	}
	var unpublished []imageUse
	for _, use := range uses {
		if repositoryOf(use.Image) == r.cfg.Repository && !slices.Contains(stale, use.Image) {
			unpublished = append(unpublished, use)
		}
	}
	return unpublished
}

// publishHeld pushes each held local image the unpublished Agents name, as
// the same build under the same tag in the registry, and repoints them to the
// digest-pinned reference. An image that cannot be published fails the
// promotion of every Agent naming it, and nothing else.
func (r *Refresher) publishHeld(ctx context.Context, report *Report, unpublished []imageUse) {
	var images []string
	for _, use := range unpublished {
		if !slices.Contains(images, use.Image) {
			images = append(images, use.Image)
		}
	}
	for _, image := range images {
		reference, err := r.publishImage(ctx, image)
		for _, use := range unpublished {
			if use.Image == image {
				r.repoint(ctx, report, use, reference, err)
			}
		}
	}
}

// publishImage pushes the image a local reference names, unrebuilt, and
// returns its digest-pinned registry reference.
func (r *Refresher) publishImage(ctx context.Context, image string) (string, error) {
	info, err := r.deps.Engine.InspectImage(ctx, image)
	if err != nil {
		return "", err
	}
	remote := r.cfg.Registry + ":" + tagOf(image)
	digest, err := r.push(ctx, info.ID, remote)
	if err != nil {
		return "", err
	}
	r.deps.Logger.Info("worker image: published a bound local image", "image", image, "reference", remote, "digest", digest)
	return remote + "@" + digest, nil
}

// tagOf is a reference's tag, or "latest" when it names none — what the
// engine itself resolves an untagged reference to.
func tagOf(image string) string {
	tagged := orchestration.ImageRepository(image)
	if tag, ok := strings.CutPrefix(tagged, repositoryOf(image)+":"); ok && tag != "" {
		return tag
	}
	return "latest"
}

// managedUses lists every Agent a binding runs, in every tenant, whose image is
// managed, in a stable order. An Agent is listed once, under the first project
// (by ID) that runs it. A tenant whose bindings or catalog cannot be read is
// recorded on the report and skipped: one sealed tenant must not hold the whole
// fleet on an image that rejects its model.
func (r *Refresher) managedUses(ctx context.Context, report *Report) ([]imageUse, error) {
	tenants, err := r.deps.Tenants.ListTenantIDs(ctx)
	if err != nil {
		return nil, err
	}
	var uses []imageUse
	for _, tenantID := range tenants {
		tenantUses, err := r.tenantUses(orchestration.ContextWithTenant(ctx, tenantID), tenantID)
		if err != nil {
			report.Skipped = append(report.Skipped, tenantID+": "+err.Error())
			continue
		}
		uses = append(uses, tenantUses...)
	}
	if len(tenants) > 0 && len(report.Skipped) == len(tenants) {
		return nil, &BindingsUnreadableError{Skipped: slices.Clone(report.Skipped)}
	}
	slices.SortStableFunc(uses, func(a, b imageUse) int {
		return cmp.Or(cmp.Compare(a.TenantID, b.TenantID), cmp.Compare(a.AgentID, b.AgentID))
	})
	return uses, nil
}

// tenantUses lists one tenant's Agents that bindings run in a managed image.
func (r *Refresher) tenantUses(ctx context.Context, tenantID string) ([]imageUse, error) {
	bindings, err := r.deps.Bindings.ListBindings(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	catalogue, err := r.deps.Agents.Catalogue(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	slices.SortFunc(bindings, func(a, b *sdlc.ProjectBinding) int { return cmp.Compare(a.ProjectID, b.ProjectID) })
	seen := map[string]bool{}
	var uses []imageUse
	for _, binding := range bindings {
		for _, reference := range binding.AgentReferences() {
			candidates, err := catalogue.Candidates(reference.Ref)
			if err != nil {
				// A dangling reference runs nothing; the binding's own
				// validation names it, not the image refresh.
				continue
			}
			for _, agent := range candidates {
				if seen[agent.ID] || !r.managed(agent.Image) {
					continue
				}
				seen[agent.ID] = true
				uses = append(uses, imageUse{
					TenantID: tenantID, ProjectID: binding.ProjectID, AgentID: agent.ID,
					Backend: agent.Backend, Model: agent.Model, Image: agent.Image,
				})
			}
		}
	}
	return uses, nil
}

// managed reports whether image belongs to a repository this refresh keeps.
func (r *Refresher) managed(image string) bool {
	repository := repositoryOf(image)
	return repository == r.cfg.Repository || (r.cfg.Registry != "" && repository == r.cfg.Registry)
}

// staleImages returns the distinct managed images behind latest, sorted.
//
// An image this host does not hold, or holds without labels, carries versions
// nobody can read — which is not the same as current, so it is stale.
//
// It also returns what every distinct bound image carries, for the report:
// the status an operator reads shows each bound image with its CLI versions.
func (r *Refresher) staleImages(ctx context.Context, uses []imageUse, latest softwarefactoryworker.Agents) ([]string, []BoundImage, error) {
	byImage := map[string]*BoundImage{}
	for _, use := range uses {
		if held, decided := byImage[use.Image]; decided {
			held.Uses++
			continue
		}
		bound := &BoundImage{Image: use.Image, Uses: 1}
		info, err := r.deps.Engine.InspectImage(ctx, use.Image)
		switch {
		case errors.Is(err, orchestration.ErrImageNotFound):
			bound.Stale = true
		case err != nil:
			return nil, nil, err
		default:
			bound.Agents = softwarefactoryworker.AgentsFromLabels(info.Labels)
			bound.Stale = behind(bound.Agents, latest)
		}
		byImage[use.Image] = bound
	}
	var stale []string
	boundImages := make([]BoundImage, 0, len(byImage))
	for image, bound := range byImage {
		if bound.Stale {
			stale = append(stale, image)
		}
		boundImages = append(boundImages, *bound)
	}
	slices.Sort(stale)
	slices.SortFunc(boundImages, func(a, b BoundImage) int { return cmp.Compare(a.Image, b.Image) })
	return stale, boundImages, nil
}

// behind reports whether have is older than latest in either CLI.
func behind(have, latest softwarefactoryworker.Agents) bool {
	return versionBehind(have.ClaudeCode, latest.ClaudeCode) || versionBehind(have.Codex, latest.Codex)
}

// refuseDowngrade refuses a promotion that would move any stale bound image
// backwards in either CLI. behind marks an image stale when EITHER CLI trails
// the registry, and the build installs BOTH at the registry's latest — so an
// image ahead in one CLI and behind in the other would lose the newer one. An
// image whose versions are unknown has nothing to lose.
func refuseDowngrade(bound []BoundImage, latest softwarefactoryworker.Agents) error {
	for _, image := range bound {
		if !image.Stale {
			continue
		}
		for _, c := range []struct{ component, have, latest string }{
			{softwarefactoryworker.ClaudeCodePackage, image.Agents.ClaudeCode, latest.ClaudeCode},
			{softwarefactoryworker.CodexPackage, image.Agents.Codex, latest.Codex},
		} {
			if versionAhead(c.have, c.latest) {
				return &DowngradeRefusedError{Image: image.Image, Component: c.component, Bound: c.have, Latest: c.latest}
			}
		}
	}
	return nil
}

// versionAhead reports whether have is a known version strictly newer than
// latest. Only semver on both sides can be ordered; anything else is not
// ahead.
func versionAhead(have, latest string) bool {
	if have == "" || !semver.IsValid("v"+have) || !semver.IsValid("v"+latest) {
		return false
	}
	return semver.Compare("v"+have, "v"+latest) > 0
}

// versionBehind compares by semver when both sides are semver, so a registry
// that moves "latest" BACKWARDS never downgrades a bound image; anything else
// is compared for equality. An unknown version is always behind.
func versionBehind(have, latest string) bool {
	if have == "" {
		return true
	}
	if semver.IsValid("v"+have) && semver.IsValid("v"+latest) {
		return semver.Compare("v"+have, "v"+latest) < 0
	}
	return have != latest
}

// candidate returns the ID of the image built for build, building it only
// when this host does not already hold a correctly labelled one under its tag,
// and proves the labels a later pass will decide by.
func (r *Refresher) candidate(ctx context.Context, build softwarefactoryworker.Build, localRef string) (string, error) {
	imageID := ""
	held, err := r.deps.Engine.InspectImage(ctx, localRef)
	switch {
	case err == nil && softwarefactoryworker.AgentsFromLabels(held.Labels) == build.Agents:
		imageID = held.ID
		r.deps.Logger.Info("worker image: reusing the candidate already built", "tag", localRef, "image_id", imageID)
	case err != nil && !errors.Is(err, orchestration.ErrImageNotFound):
		return "", err
	default:
		r.deps.Logger.Info("worker image: building", "tag", localRef)
		imageID, err = r.deps.Engine.BuildImage(ctx, &orchestration.ImageBuildSpec{
			Dockerfile: softwarefactoryworker.Dockerfile,
			Tags:       []string{localRef},
			BuildArgs:  build.BuildArgs(),
			Pull:       true,
		})
		if err != nil {
			return "", err
		}
	}
	built, err := r.deps.Engine.InspectImage(ctx, imageID)
	if err != nil {
		return "", err
	}
	var wrong []string
	for key, want := range build.Labels() {
		if built.Labels[key] != want {
			wrong = append(wrong, key+"="+strconv.Quote(built.Labels[key])+" (want "+strconv.Quote(want)+")")
		}
	}
	if len(wrong) > 0 {
		slices.Sort(wrong)
		return "", &GateError{Image: localRef, Failed: append([]string{"labels"}, wrong...)}
	}
	return imageID, nil
}

// smokeTarget is one smoke the gate runs and the binding it runs on behalf of.
type smokeTarget struct {
	use   imageUse
	model string
}

// smokePlan is one smoke per distinct agent the promotion would run in the
// candidate, plus the default Claude model — always, because it is the model
// a new binding gets and the one this refresh most exists to keep working.
func (r *Refresher) smokePlan(staleUses []imageUse) []smokeTarget {
	var plan []smokeTarget
	seen := map[string]bool{}
	add := func(use imageUse, backend, model string) {
		key := backend + "\x00" + model
		if seen[key] {
			return
		}
		seen[key] = true
		use.Backend = backend
		plan = append(plan, smokeTarget{use: use, model: model})
	}
	if len(staleUses) > 0 {
		add(staleUses[0], DefaultSmokeBackend, r.cfg.SmokeModel)
	}
	for _, use := range staleUses {
		add(use, use.Backend, use.Model)
	}
	return plan
}

// gate runs the probes against exactly imageID and the smokes against its tag,
// then proves the tag still names the gated build.
func (r *Refresher) gate(ctx context.Context, report *Report, build softwarefactoryworker.Build,
	imageID, localRef string, staleUses []imageUse) error {
	probes, err := RunProbes(ctx, r.deps.Engine, imageID, build)
	report.Probes = probes
	if err != nil {
		return err
	}
	if failed := failedNames(probes); len(failed) > 0 {
		return &GateError{Image: localRef, Failed: failed}
	}

	var failed []string
	for _, target := range r.smokePlan(staleUses) {
		name := target.use.Backend + "/" + cmp.Or(target.model, "(backend default)")
		reply, err := r.smoke(ctx, target, localRef)
		result := CheckResult{Name: name, Passed: err == nil, Detail: tail(reply)}
		if err != nil {
			result.Detail = tail(err.Error())
			failed = append(failed, "smoke "+name)
		}
		report.Smokes = append(report.Smokes, result)
	}
	if len(failed) > 0 {
		return &GateError{Image: localRef, Failed: failed}
	}

	// The smokes spawned by TAG; the probes ran by ID. They are the same build
	// only if nothing retagged the candidate while the gate ran.
	after, err := r.deps.Engine.InspectImage(ctx, localRef)
	if err != nil {
		return err
	}
	if after.ID != imageID {
		return &GateError{Image: localRef, Failed: []string{"the candidate tag moved to " + after.ID + " during the gate"}}
	}
	return nil
}

// smoke runs one smoke on the agent that holds the candidate.
func (r *Refresher) smoke(ctx context.Context, target smokeTarget, image string) (string, error) {
	host := r.cfg.SmokeHost
	if host == "" {
		resolved, err := r.deps.Hosts.LocalFleetNodeHost(ctx, target.use.TenantID, target.use.Backend)
		if err != nil {
			return "", &SmokeError{Backend: target.use.Backend, Model: target.model, Stage: "placement", Cause: err}
		}
		host = resolved
	}
	return r.deps.Smoker.Smoke(ctx, SmokeRequest{
		TenantID: target.use.TenantID, ProjectID: target.use.ProjectID,
		Backend: target.use.Backend, Model: target.model, Image: image, Host: host,
	})
}

// publish returns the reference Agents are repointed to. With a registry it
// is the pushed, digest-pinned reference and only exists once the push has
// succeeded; without one it is the local tag.
func (r *Refresher) publish(ctx context.Context, imageID string, build softwarefactoryworker.Build, localRef string) (string, string, error) {
	if r.cfg.Registry == "" {
		return localRef, "", nil
	}
	remote := r.cfg.Registry + ":" + build.Tag()
	digest, err := r.push(ctx, imageID, remote)
	if err != nil {
		return "", "", err
	}
	r.deps.Logger.Info("worker image: published", "reference", remote, "digest", digest)
	return remote + "@" + digest, digest, nil
}

// push tags imageID as remote and pushes it, logged in with the configured
// credential — read now, so the secret is touched only by a pass that
// publishes — and returns the manifest digest.
func (r *Refresher) push(ctx context.Context, imageID, remote string) (string, error) {
	if err := r.deps.Engine.TagImage(ctx, imageID, remote); err != nil {
		return "", err
	}
	var auth *orchestration.RegistryAuth
	if r.cfg.Credential != nil {
		credential, err := r.cfg.Credential.Credential(ctx)
		if err != nil {
			return "", err
		}
		auth = credential
	}
	return r.deps.Engine.PushImage(ctx, remote, auth)
}

// repoint moves one Agent from the image it names to reference, and audits
// it. A non-nil cause is why no reference could be produced: the Agent is
// left alone and the failure is recorded exactly like a failed write.
func (r *Refresher) repoint(ctx context.Context, report *Report, use imageUse, reference string, cause error) {
	promotion := Promotion{TenantID: use.TenantID, AgentID: use.AgentID, From: []string{use.Image}, To: reference}
	tenantCtx := orchestration.ContextWithTenant(ctx, use.TenantID)
	if cause == nil {
		_, cause = r.deps.Agents.ReplaceAgentImage(tenantCtx, use.TenantID, use.AgentID, promotion.From, reference)
	}
	if cause != nil {
		promotion.Error = cause.Error()
	}
	report.Promotions = append(report.Promotions, promotion)
	r.deps.Auditor.RecordWorkerImageEvent(tenantCtx, AuditEvent{
		TenantID: use.TenantID, Action: AuditActionPromote, Resource: use.AgentID, Success: promotion.Error == "",
		Details: map[string]string{"from": use.Image, "to": reference, "error": promotion.Error},
	})
}

// settle sets the outcome of a pass that promoted: partial when any Agent
// could not be repointed or any tenant could not be read.
func (r *Refresher) settle(report *Report) {
	failures := 0
	for _, promotion := range report.Promotions {
		if promotion.Error != "" {
			failures++
		}
	}
	report.Outcome = OutcomePromoted
	if failures > 0 || len(report.Skipped) > 0 {
		report.Outcome = OutcomePartial
		report.Error = strconv.Itoa(failures) + " of " + strconv.Itoa(len(report.Promotions)) + " agents could not be repointed; " +
			strconv.Itoa(len(report.Skipped)) + " tenants could not be read"
	}
}

// finish records the pass: the report becomes Status().Last, the outcome is
// counted, and anything other than a clean pass is logged at error level and
// put on the audit trail. A pass that found nothing to do is logged, not
// audited.
func (r *Refresher) finish(ctx context.Context, report *Report, err error) {
	report.FinishedAt = r.now()
	if err != nil {
		report.Outcome = OutcomeFailed
		report.Error = err.Error()
	}
	r.last.Store(report)
	r.deps.Observer.ObserveRefresh(report.Outcome, report.FinishedAt)

	attrs := []any{"trigger", report.Trigger, "outcome", report.Outcome, "candidate", report.Candidate,
		"reference", report.Reference, "claude_code", report.Latest.ClaudeCode, "codex", report.Latest.Codex}
	switch report.Outcome {
	case OutcomeCurrent:
		r.deps.Logger.Info("worker image: bound images are current", attrs...)
		return
	case OutcomePromoted:
		r.deps.Logger.Info("worker image: promoted", attrs...)
	default:
		r.deps.Logger.Error("worker image: refresh did not complete cleanly; bindings on a failed pass are unchanged",
			append(attrs, "error", report.Error, "skipped", report.Skipped)...)
	}
	r.deps.Auditor.RecordWorkerImageEvent(ctx, AuditEvent{
		Action: AuditActionRefresh, Resource: report.Candidate, Success: report.Outcome == OutcomePromoted,
		Details: map[string]string{
			"trigger": string(report.Trigger), "outcome": string(report.Outcome), "image_id": report.ImageID,
			"reference": report.Reference, "digest": report.Digest, "error": report.Error,
			"claude_code": report.Latest.ClaudeCode, "codex": report.Latest.Codex,
		},
	})
}
