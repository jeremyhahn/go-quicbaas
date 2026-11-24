package serviceplane

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// Verb is a lifecycle verb.
type Verb string

const (
	VerbDeploy   Verb = "deploy"
	VerbUndeploy Verb = "undeploy"
	VerbRedeploy Verb = "redeploy"
	VerbStart    Verb = "start"
	VerbStop     Verb = "stop"
	VerbResume   Verb = "resume"
	VerbRestart  Verb = "restart"
)

// Verbs is the seven lifecycle verbs.
var Verbs = []Verb{VerbDeploy, VerbUndeploy, VerbRedeploy, VerbStart, VerbStop, VerbResume, VerbRestart}

// allowed is the state machine: the states each verb on a deployed service
// may start from. Deploy has none: it starts from no record.
var allowed = map[Verb][]State{
	VerbUndeploy: {StateRunning, StateStopped, StateHalted},
	VerbRedeploy: {StateRunning, StateStopped, StateHalted},
	VerbStart:    {StateStopped},
	VerbStop:     {StateRunning},
	VerbResume:   {StateHalted},
	VerbRestart:  {StateRunning, StateHalted},
}

// Launcher runs a service's containers on its node. The node agent's
// container runtime implements it. svc.Slot names the rollout slot the
// instance runs in ("" for the instance Deploy launched); a service has
// one instance per slot, so the launcher keys its instances by tenant,
// name and slot.
type Launcher interface {
	Launch(ctx context.Context, svc *Service) error
	Halt(ctx context.Context, svc *Service) error
}

// Grant gives a deployed service one kind of grant and takes it back.
// Grant is an ensure: a redeploy calls it again with the new manifest and
// the reference the earlier call returned.
type Grant interface {
	Kind() GrantKind
	Grant(ctx context.Context, m *Manifest, prior string) (ref string, err error)
	Revoke(ctx context.Context, m *Manifest, ref string) error
}

// Config builds a Plane. Every part is required, and Grants must hold
// exactly one Grant per kind.
type Config struct {
	Store    *Store
	Verifier Verifier
	Launcher Launcher
	Grants   []Grant
	// Now defaults to time.Now.
	Now func() time.Time
	// StaleClaim is how long a service may stay transitioning before
	// MarkHalted treats the claim as abandoned (its holder crashed, or its
	// settlement write failed) and recovers it. Zero is DefaultStaleClaim.
	StaleClaim time.Duration
	// Rollout rolls a served Redeploy out through the rollout controller;
	// a plane without one refuses Redeploy with ErrConfig. SetRoller sets
	// it after construction, for a controller built over the plane.
	Rollout Roller
}

// DefaultStaleClaim is Config.StaleClaim's default.
const DefaultStaleClaim = 15 * time.Minute

// Plane serves the seven lifecycle verbs.
type Plane struct {
	store      *Store
	verifier   Verifier
	launcher   Launcher
	grants     map[GrantKind]Grant
	now        func() time.Time
	staleClaim time.Duration
	roller     Roller
}

// NewPlane checks cfg and returns the Plane.
func NewPlane(cfg Config) (*Plane, error) {
	if cfg.Store == nil || cfg.Verifier == nil || cfg.Launcher == nil {
		return nil, fmt.Errorf("%w: a plane needs a store, a verifier and a launcher", ErrConfig)
	}
	grants := make(map[GrantKind]Grant, len(GrantKinds))
	for _, g := range cfg.Grants {
		if g == nil {
			return nil, fmt.Errorf("%w: nil grant", ErrConfig)
		}
		if _, dup := grants[g.Kind()]; dup {
			return nil, fmt.Errorf("%w: two grants of kind %s", ErrConfig, g.Kind())
		}
		grants[g.Kind()] = g
	}
	for _, k := range GrantKinds {
		if grants[k] == nil {
			return nil, fmt.Errorf("%w: no grant of kind %s", ErrConfig, k)
		}
	}
	if len(grants) != len(GrantKinds) {
		return nil, fmt.Errorf("%w: unknown grant kind", ErrConfig)
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	stale := cfg.StaleClaim
	if stale <= 0 {
		stale = DefaultStaleClaim
	}
	return &Plane{store: cfg.Store, verifier: cfg.Verifier, launcher: cfg.Launcher, grants: grants, now: now, staleClaim: stale, roller: cfg.Rollout}, nil
}

// SetRoller sets the Roller Redeploy rolls out through. Call it before the
// plane serves.
func (p *Plane) SetRoller(r Roller) { p.roller = r }

// Roller answers the Roller set by Config.Rollout or SetRoller, nil when none.
func (p *Plane) Roller() Roller { return p.roller }

// Get returns a deployed service.
func (p *Plane) Get(ctx context.Context, tenant, name string) (*Service, error) {
	return p.store.Get(ctx, tenant, name)
}

// List returns a tenant's services.
func (p *Plane) List(ctx context.Context, tenant string) ([]*Service, error) {
	return p.store.List(ctx, tenant)
}

// Deploy verifies sm, claims its name, grants the service its identity,
// certificate, registered name, RBAC and secrets, and launches it. A failed
// grant revokes the ones made and releases the name; if a revoke fails too,
// the service stays deployed and halted holding the grants still in force,
// so undeploy can retry their revocation. Each grant is stored before the
// next one starts, so a deploy interrupted after granting (a lost write, a
// crash) leaves a record that MarkHalted and Undeploy can act on. A failed
// launch leaves the service deployed and halted.
func (p *Plane) Deploy(ctx context.Context, tenant string, sm *SignedManifest) (*Service, error) {
	m, err := p.verify(VerbDeploy, tenant, sm)
	if err != nil {
		return nil, err
	}
	svc := &Service{
		Tenant: m.Tenant, Name: m.Name, Signed: *sm, Manifest: *m,
		State: StateTransitioning, Verb: VerbDeploy, Revision: 1, UpdatedAt: p.now(),
	}
	cur, err := p.store.create(ctx, svc)
	if err != nil {
		return nil, err
	}
	cur, grants, _, err := p.grant(ctx, VerbDeploy, cur, svc, m, nil)
	if err != nil {
		if len(grants) > 0 {
			// Grants still in force: keep them for undeploy, and the
			// record out of start, resume and restart.
			next := *cur.svc
			next.Grants, next.Cleanup = grants, true
			return nil, p.settleNext(ctx, VerbDeploy, cur, &next, ErrGrant, err)
		}
		sctx, cancel := settleCtx(ctx)
		defer cancel()
		if rmErr := p.store.remove(sctx, string(VerbDeploy), svc); rmErr != nil {
			err = errors.Join(err, rmErr)
		}
		return nil, p.fail(string(VerbDeploy), svc, ErrGrant, err)
	}
	next := *cur.svc
	next.Grants = grants
	return p.launch(ctx, VerbDeploy, cur, &next, false)
}

// Replace replaces a deployed service's manifest with sm in place: it
// re-applies its grants for the new manifest, halts the service and
// launches it, with no appraisal and no second slot. It is the library
// form for an embedder that drives its own launches; the served redeploy
// is Redeploy, which rolls a signed Release out through the rollout
// controller.
func (p *Plane) Replace(ctx context.Context, tenant string, sm *SignedManifest) (*Service, error) {
	m, err := p.verify(VerbRedeploy, tenant, sm)
	if err != nil {
		return nil, err
	}
	cur, err := p.claim(ctx, VerbRedeploy, m.Tenant, m.Name)
	if err != nil {
		return nil, err
	}
	prior := cur.svc.Grants
	cleanup := cur.svc.Cleanup
	cur, grants, incomplete, err := p.grant(ctx, VerbRedeploy, cur, cur.svc, m, prior)
	if err != nil {
		// The prior grants, overlaid with each reference in force that
		// differs from them: a new grant whose revoke failed, a restored
		// grant's new reference, a replacement still in force.
		next := *cur.svc
		next.Grants = make(map[GrantKind]string, len(prior)+len(grants))
		for k, ref := range prior {
			next.Grants[k] = ref
		}
		for k, ref := range grants {
			next.Grants[k] = ref
		}
		next.Cleanup = cleanup || incomplete
		return nil, p.settleNext(ctx, VerbRedeploy, cur, &next, ErrGrant, err)
	}
	next := *cur.svc
	next.Signed, next.Manifest, next.Grants, next.Cleanup = *sm, *m, grants, false
	next.Revision++
	return p.launch(ctx, VerbRedeploy, cur, &next, true)
}

// Undeploy halts a service, revokes its grants and removes it.
func (p *Plane) Undeploy(ctx context.Context, tenant, name string) error {
	cur, err := p.claim(ctx, VerbUndeploy, tenant, name)
	if err != nil {
		return err
	}
	if err := p.launcher.Halt(ctx, cur.svc); err != nil {
		return p.settle(ctx, VerbUndeploy, cur, ErrLaunch, err)
	}
	// A rollout's filled slots that are not active run beside it.
	for slot, rel := range cur.svc.Slots {
		if slot == cur.svc.Slot {
			continue
		}
		if err := p.launcher.Halt(ctx, slotInstance(cur.svc, slot, &rel)); err != nil {
			return p.settle(ctx, VerbUndeploy, cur, ErrLaunch, fmt.Errorf("halt slot %s: %w", slot, err))
		}
	}
	// Mark the record Cleanup before the first revoke, so a record left
	// part way (a crash, a lost write) is never launched with grants
	// revoked; each revoke that succeeds is then dropped from the record,
	// so a redeploy grants it anew rather than reusing a dead reference.
	next := *cur.svc
	next.Grants = make(map[GrantKind]string, len(cur.svc.Grants))
	for k, ref := range cur.svc.Grants {
		next.Grants[k] = ref
	}
	next.Cleanup, next.UpdatedAt = true, p.now()
	sctx, cancel := settleCtx(ctx)
	defer cancel()
	cur, err = p.store.swap(sctx, string(VerbUndeploy), cur, &next)
	if err != nil {
		return err
	}
	var errs []error
	for i := len(GrantKinds) - 1; i >= 0; i-- {
		k := GrantKinds[i]
		ref, ok := next.Grants[k]
		if !ok {
			continue
		}
		if err := p.grants[k].Revoke(ctx, &next.Manifest, ref); err != nil {
			errs = append(errs, fmt.Errorf("revoke %s: %w", k, err))
			continue
		}
		step := next
		step.Grants = make(map[GrantKind]string, len(next.Grants))
		for held, r := range next.Grants {
			if held != k {
				step.Grants[held] = r
			}
		}
		step.UpdatedAt = p.now()
		st, err := p.store.swap(sctx, string(VerbUndeploy), cur, &step)
		if err != nil {
			errs = append(errs, fmt.Errorf("revoke %s: checkpoint: %w", k, err))
			break
		}
		cur, next = st, step
	}
	if len(errs) > 0 {
		// Keep the grants still in force for a retried undeploy, and the
		// record out of start, resume and restart.
		return p.settleNext(ctx, VerbUndeploy, cur, &next, ErrGrant, errors.Join(errs...))
	}
	return p.store.remove(sctx, string(VerbUndeploy), &next)
}

// Start launches a service an operator stopped.
func (p *Plane) Start(ctx context.Context, tenant, name string) (*Service, error) {
	return p.relaunch(ctx, VerbStart, tenant, name, false)
}

// Resume launches a service the platform halted.
func (p *Plane) Resume(ctx context.Context, tenant, name string) (*Service, error) {
	return p.relaunch(ctx, VerbResume, tenant, name, false)
}

// Restart halts a running or halted service and launches it again.
func (p *Plane) Restart(ctx context.Context, tenant, name string) (*Service, error) {
	return p.relaunch(ctx, VerbRestart, tenant, name, true)
}

// Stop halts a running service; start launches it again.
func (p *Plane) Stop(ctx context.Context, tenant, name string) (*Service, error) {
	cur, err := p.claim(ctx, VerbStop, tenant, name)
	if err != nil {
		return nil, err
	}
	if err := p.launcher.Halt(ctx, cur.svc); err != nil {
		return nil, p.settle(ctx, VerbStop, cur, ErrLaunch, err)
	}
	next := *cur.svc
	next.State, next.Verb, next.Reason, next.UpdatedAt = StateStopped, "", "", p.now()
	// The containers are down: settle that even if ctx ends now.
	sctx, cancel := settleCtx(ctx)
	defer cancel()
	st, err := p.store.swap(sctx, string(VerbStop), cur, &next)
	if err != nil {
		return nil, err
	}
	return st.svc, nil
}

// MarkHalted records that the platform halted a running service (a crash,
// a lost node), so resume or restart can launch it again. It also recovers
// a stale claim: a service transitioning since longer than Config.StaleClaim,
// whose verb was interrupted before it settled, is settled halted with its
// grants kept. A fresher transitioning service is refused with
// ErrTransition.
func (p *Plane) MarkHalted(ctx context.Context, tenant, name, reason string) (*Service, error) {
	cur, err := p.store.load(ctx, "halt", tenant, name)
	if err != nil {
		return nil, err
	}
	stale := cur.svc.State == StateTransitioning && cur.svc.UpdatedAt.Before(p.now().Add(-p.staleClaim))
	if cur.svc.State != StateRunning && !stale {
		return nil, p.fail("halt", cur.svc, ErrTransition, &TransitionError{Verb: "halt", State: cur.svc.State})
	}
	next := *cur.svc
	next.State, next.Verb, next.Reason, next.UpdatedAt = StateHalted, "", reason, p.now()
	st, err := p.store.swap(ctx, "halt", cur, &next)
	if err != nil {
		return nil, err
	}
	return st.svc, nil
}

// Recover settles every abandoned claim on the plane: each service, of any
// tenant, transitioning since longer than Config.StaleClaim is marked
// halted through MarkHalted, so its owner can resume, redeploy or undeploy
// it. The node runs it at start and on a timer. It answers the services it
// recovered; a service another node settled first is skipped.
func (p *Plane) Recover(ctx context.Context) ([]*Service, error) {
	all, err := p.store.listAll(ctx)
	if err != nil {
		return nil, err
	}
	cutoff := p.now().Add(-p.staleClaim)
	var out []*Service
	var errs []error
	for _, svc := range all {
		if svc.State != StateTransitioning || !svc.UpdatedAt.Before(cutoff) {
			continue
		}
		reason := fmt.Sprintf("recovered: %s was interrupted before it settled", svc.Verb)
		got, err := p.MarkHalted(ctx, svc.Tenant, svc.Name, reason)
		if err != nil {
			if errors.Is(err, ErrConflict) || errors.Is(err, ErrTransition) || errors.Is(err, ErrNotFound) {
				continue
			}
			errs = append(errs, err)
			continue
		}
		out = append(out, got)
	}
	return out, errors.Join(errs...)
}

func (p *Plane) relaunch(ctx context.Context, verb Verb, tenant, name string, restart bool) (*Service, error) {
	cur, err := p.claim(ctx, verb, tenant, name)
	if err != nil {
		return nil, err
	}
	next := *cur.svc
	if restart {
		next.Restarts++
	}
	return p.launch(ctx, verb, cur, &next, restart)
}

// verify checks sm's signature and that the signer's manifest is valid.
func (p *Plane) verify(verb Verb, tenant string, sm *SignedManifest) (*Manifest, error) {
	m, err := p.verifier.Verify(sm)
	if err != nil {
		return nil, &ServiceError{Op: string(verb), Tenant: tenant, Sentinel: ErrSignature, Cause: err}
	}
	// The tenant is read from the verified payload only: a claim in an
	// unverified payload decides nothing.
	if tenant == "" || m.Tenant != tenant {
		return nil, &ServiceError{Op: string(verb), Tenant: tenant, Name: m.Name, Sentinel: ErrTenant}
	}
	if m.Class == ClassBroker {
		return nil, &ServiceError{Op: string(verb), Tenant: tenant, Name: m.Name, Sentinel: ErrBrokerClass}
	}
	return m, nil
}

// claim moves the service to transitioning under verb, if verb is legal in
// its state. The swap is the arbitration: of two nodes claiming one service,
// one wins and the other gets ErrConflict.
func (p *Plane) claim(ctx context.Context, verb Verb, tenant, name string) (*stored, error) {
	cur, err := p.store.load(ctx, string(verb), tenant, name)
	if err != nil {
		return nil, err
	}
	legal := false
	for _, s := range allowed[verb] {
		legal = legal || s == cur.svc.State
	}
	if !legal {
		return nil, p.fail(string(verb), cur.svc, ErrTransition, &TransitionError{Verb: verb, State: cur.svc.State})
	}
	if verb == VerbStart || verb == VerbResume || verb == VerbRestart {
		if err := launchable(cur.svc); err != nil {
			return nil, p.fail(string(verb), cur.svc, ErrGrant, err)
		}
	}
	next := *cur.svc
	next.State, next.Verb, next.UpdatedAt = StateTransitioning, verb, p.now()
	return p.store.swap(ctx, string(verb), cur, &next)
}

// launchable reports why svc's record may not be launched as it stands: a
// verb left its grants part way (Cleanup), or a grant kind is missing. Such
// a record is only redeployed, which grants it anew, or undeployed.
func launchable(svc *Service) error {
	if svc.Cleanup {
		return fmt.Errorf("the grants of %s/%s are part way through a change (%s): redeploy or undeploy it", svc.Tenant, svc.Name, svc.Reason)
	}
	for _, k := range GrantKinds {
		if svc.Grants[k] == "" {
			return fmt.Errorf("%s/%s holds no %s grant: redeploy or undeploy it", svc.Tenant, svc.Name, k)
		}
	}
	return nil
}

// launch halts the service first when halt is set, launches next and
// settles it running, or halted with the launcher's error.
func (p *Plane) launch(ctx context.Context, verb Verb, cur *stored, next *Service, halt bool) (*Service, error) {
	if halt {
		if err := p.launcher.Halt(ctx, cur.svc); err != nil {
			return nil, p.settleNext(ctx, verb, cur, next, ErrLaunch, err)
		}
	}
	if err := p.launcher.Launch(ctx, next); err != nil {
		return nil, p.settleNext(ctx, verb, cur, next, ErrLaunch, err)
	}
	next.State, next.Verb, next.Reason, next.UpdatedAt = StateRunning, "", "", p.now()
	sctx, cancel := settleCtx(ctx)
	defer cancel()
	st, err := p.store.swap(sctx, string(verb), cur, next)
	if err != nil {
		return nil, err
	}
	return st.svc, nil
}

// settle ends a claimed verb that failed: the service is halted, with why.
func (p *Plane) settle(ctx context.Context, verb Verb, cur *stored, sentinel, cause error) error {
	next := *cur.svc
	return p.settleNext(ctx, verb, cur, &next, sentinel, cause)
}

func (p *Plane) settleNext(ctx context.Context, verb Verb, cur *stored, next *Service, sentinel, cause error) error {
	next.State, next.Verb, next.Reason, next.UpdatedAt = StateHalted, "", cause.Error(), p.now()
	sctx, cancel := settleCtx(ctx)
	defer cancel()
	if _, err := p.store.swap(sctx, string(verb), cur, next); err != nil {
		cause = errors.Join(cause, err)
	}
	return p.fail(string(verb), next, sentinel, cause)
}

// grant ensures every grant kind for m in order. Each grant is stored, as
// a transitioning checkpoint of base swapped against the latest record,
// before the next one starts, so an interrupted deploy leaves a record
// that MarkHalted and Undeploy can act on; the only window left is a Grant
// that succeeded right before a crash, ahead of its checkpoint write. A
// lost checkpoint is handled as a failed grant.
//
// On success refs holds every grant. On a failure grant undoes, in reverse,
// what this call changed: it revokes each grant it made where prior held
// none, and gives back base's grant where a held one was replaced (a new
// role, a moved name, a renewed SVID), so the prior manifest's grants are
// in force again. refs then holds each reference in force that differs
// from prior: a made grant whose revoke failed, a restored grant's new
// reference, or a replacement whose restore failed. incomplete reports
// that some change could not be undone, so the record's grants no longer
// match its manifest. last is always the latest stored record.
func (p *Plane) grant(ctx context.Context, verb Verb, cur *stored, base *Service, m *Manifest, prior map[GrantKind]string) (last *stored, refs map[GrantKind]string, incomplete bool, err error) {
	out := make(map[GrantKind]string, len(GrantKinds))
	var changed []GrantKind
	rollback := func(cause error) (*stored, map[GrantKind]string, bool, error) {
		errs := []error{cause}
		rctx, cancel := settleCtx(ctx)
		defer cancel()
		left := map[GrantKind]string{}
		incomplete := false
		for i := len(changed) - 1; i >= 0; i-- {
			k := changed[i]
			if _, held := prior[k]; !held {
				if rerr := p.grants[k].Revoke(rctx, m, out[k]); rerr != nil {
					errs = append(errs, fmt.Errorf("revoke %s: %w", k, rerr))
					left[k], incomplete = out[k], true
				}
				continue
			}
			ref, rerr := p.grants[k].Grant(rctx, &base.Manifest, out[k])
			if rerr != nil {
				errs = append(errs, fmt.Errorf("restore %s: %w", k, rerr))
				left[k], incomplete = out[k], true
				continue
			}
			if ref != prior[k] {
				left[k] = ref
			}
		}
		return cur, left, incomplete, errors.Join(errs...)
	}
	for _, k := range GrantKinds {
		ref, err := p.grants[k].Grant(ctx, m, prior[k])
		if err != nil {
			return rollback(fmt.Errorf("grant %s: %w", k, err))
		}
		out[k] = ref
		if held, ok := prior[k]; !ok || held != ref {
			changed = append(changed, k)
		}
		st, err := p.checkpoint(ctx, verb, cur, base, prior, out)
		if err != nil {
			return rollback(fmt.Errorf("grant %s: checkpoint: %w", k, err))
		}
		cur = st
	}
	return cur, out, false, nil
}

// checkpoint stores base, still transitioning, holding prior overlaid with
// the grants made so far. A redeploy's checkpoint is marked Cleanup: its
// grants are the new manifest's while its manifest is still base's, so a
// record left at it (a crash, a lost settlement) is not launched as is.
func (p *Plane) checkpoint(ctx context.Context, verb Verb, cur *stored, base *Service, prior, made map[GrantKind]string) (*stored, error) {
	next := *base
	next.Grants = make(map[GrantKind]string, len(prior)+len(made))
	for k, ref := range prior {
		next.Grants[k] = ref
	}
	for k, ref := range made {
		next.Grants[k] = ref
	}
	next.Cleanup = next.Cleanup || verb == VerbRedeploy
	next.State, next.UpdatedAt = StateTransitioning, p.now()
	sctx, cancel := settleCtx(ctx)
	defer cancel()
	return p.store.swap(sctx, string(verb), cur, &next)
}

// settleTimeout bounds a cleanup or settlement write.
const settleTimeout = 10 * time.Second

// settleCtx is the context cleanup and settlement run on: ctx's values
// without its cancellation, bounded by settleTimeout, so a request cancelled
// mid-verb still leaves the record in a state the next verb can act on.
func settleCtx(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), settleTimeout)
}

func (p *Plane) fail(op string, svc *Service, sentinel, cause error) error {
	return &ServiceError{Op: op, Tenant: svc.Tenant, Name: svc.Name, Sentinel: sentinel, Cause: cause}
}
