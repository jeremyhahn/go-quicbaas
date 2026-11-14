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
// container runtime implements it.
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
}

// Plane serves the seven lifecycle verbs.
type Plane struct {
	store    *Store
	verifier Verifier
	launcher Launcher
	grants   map[GrantKind]Grant
	now      func() time.Time
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
	return &Plane{store: cfg.Store, verifier: cfg.Verifier, launcher: cfg.Launcher, grants: grants, now: now}, nil
}

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
// grant revokes the ones made and releases the name; a failed launch leaves
// the service deployed and halted.
func (p *Plane) Deploy(ctx context.Context, sm *SignedManifest) (*Service, error) {
	m, err := p.verify(VerbDeploy, sm)
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
	grants, err := p.grant(ctx, m, nil)
	if err != nil {
		if rmErr := p.store.remove(ctx, string(VerbDeploy), svc); rmErr != nil {
			err = errors.Join(err, rmErr)
		}
		return nil, p.fail(string(VerbDeploy), svc, ErrGrant, err)
	}
	next := *svc
	next.Grants = grants
	return p.launch(ctx, VerbDeploy, cur, &next, false)
}

// Redeploy replaces a deployed service's manifest with sm, re-applies its
// grants for the new manifest and launches it.
func (p *Plane) Redeploy(ctx context.Context, sm *SignedManifest) (*Service, error) {
	m, err := p.verify(VerbRedeploy, sm)
	if err != nil {
		return nil, err
	}
	cur, err := p.claim(ctx, VerbRedeploy, m.Tenant, m.Name)
	if err != nil {
		return nil, err
	}
	prior := cur.svc.Grants
	grants, err := p.grant(ctx, m, prior)
	if err != nil {
		return nil, p.settle(ctx, VerbRedeploy, cur, ErrGrant, err)
	}
	next := *cur.svc
	next.Signed, next.Manifest, next.Grants = *sm, *m, grants
	next.Revision++
	return p.launch(ctx, VerbRedeploy, cur, &next, true)
}

// Undeploy halts a service, revokes its grants and removes it.
func (p *Plane) Undeploy(ctx context.Context, tenant, name string) error {
	cur, err := p.claim(ctx, VerbUndeploy, tenant, name)
	if err != nil {
		return err
	}
	svc := cur.svc
	if err := p.launcher.Halt(ctx, svc); err != nil {
		return p.settle(ctx, VerbUndeploy, cur, ErrLaunch, err)
	}
	var errs []error
	for i := len(GrantKinds) - 1; i >= 0; i-- {
		k := GrantKinds[i]
		if ref, ok := svc.Grants[k]; ok {
			if err := p.grants[k].Revoke(ctx, &svc.Manifest, ref); err != nil {
				errs = append(errs, fmt.Errorf("revoke %s: %w", k, err))
			}
		}
	}
	if len(errs) > 0 {
		return p.settle(ctx, VerbUndeploy, cur, ErrGrant, errors.Join(errs...))
	}
	return p.store.remove(ctx, string(VerbUndeploy), svc)
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
	st, err := p.store.swap(ctx, string(VerbStop), cur, &next)
	if err != nil {
		return nil, err
	}
	return st.svc, nil
}

// MarkHalted records that the platform halted a running service (a crash,
// a lost node), so resume or restart can launch it again.
func (p *Plane) MarkHalted(ctx context.Context, tenant, name, reason string) (*Service, error) {
	cur, err := p.store.load(ctx, "halt", tenant, name)
	if err != nil {
		return nil, err
	}
	if cur.svc.State != StateRunning {
		return nil, p.fail("halt", cur.svc, ErrTransition, &TransitionError{Verb: "halt", State: cur.svc.State})
	}
	next := *cur.svc
	next.State, next.Reason, next.UpdatedAt = StateHalted, reason, p.now()
	st, err := p.store.swap(ctx, "halt", cur, &next)
	if err != nil {
		return nil, err
	}
	return st.svc, nil
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
func (p *Plane) verify(verb Verb, sm *SignedManifest) (*Manifest, error) {
	m, err := p.verifier.Verify(sm)
	if err != nil {
		return nil, &ServiceError{Op: string(verb), Sentinel: ErrSignature, Cause: err}
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
	next := *cur.svc
	next.State, next.Verb, next.UpdatedAt = StateTransitioning, verb, p.now()
	return p.store.swap(ctx, string(verb), cur, &next)
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
	st, err := p.store.swap(ctx, string(verb), cur, next)
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
	if _, err := p.store.swap(ctx, string(verb), cur, next); err != nil {
		cause = errors.Join(cause, err)
	}
	return p.fail(string(verb), next, sentinel, cause)
}

// grant ensures every grant kind for m in order. On a failure it revokes
// the grants this call made (not the ones prior already held) and returns
// the error.
func (p *Plane) grant(ctx context.Context, m *Manifest, prior map[GrantKind]string) (map[GrantKind]string, error) {
	out := make(map[GrantKind]string, len(GrantKinds))
	var made []GrantKind
	for _, k := range GrantKinds {
		ref, err := p.grants[k].Grant(ctx, m, prior[k])
		if err != nil {
			errs := []error{fmt.Errorf("grant %s: %w", k, err)}
			for i := len(made) - 1; i >= 0; i-- {
				if rerr := p.grants[made[i]].Revoke(ctx, m, out[made[i]]); rerr != nil {
					errs = append(errs, fmt.Errorf("revoke %s: %w", made[i], rerr))
				}
			}
			return nil, errors.Join(errs...)
		}
		out[k] = ref
		if _, held := prior[k]; !held {
			made = append(made, k)
		}
	}
	return out, nil
}

func (p *Plane) fail(op string, svc *Service, sentinel, cause error) error {
	return &ServiceError{Op: op, Tenant: svc.Tenant, Name: svc.Name, Sentinel: sentinel, Cause: cause}
}
