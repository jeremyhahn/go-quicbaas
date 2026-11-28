package serviceplane

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	qberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// State is where a service is in its lifecycle.
type State string

const (
	// StateTransitioning: a verb has claimed the service and is acting on
	// the node; every other verb is refused until it settles.
	StateTransitioning State = "transitioning"
	// StateRunning: launched.
	StateRunning State = "running"
	// StateStopped: stopped by an operator; start launches it again.
	StateStopped State = "stopped"
	// StateHalted: stopped by the platform (a failed launch, a crash, a lost
	// node); resume or restart launches it again.
	StateHalted State = "halted"
)

// GrantKind is one of the five things deploy grants a service.
type GrantKind string

const (
	GrantIdentity    GrantKind = "identity"
	GrantCertificate GrantKind = "certificate"
	GrantName        GrantKind = "name"
	GrantRBAC        GrantKind = "rbac"
	GrantSecrets     GrantKind = "secrets"
)

// GrantKinds is every grant kind, in the order deploy grants them: the
// certificate and the secrets are bound to the identity, so it comes first.
var GrantKinds = []GrantKind{GrantIdentity, GrantCertificate, GrantName, GrantRBAC, GrantSecrets}

// Service is the stored record of a deployed service.
type Service struct {
	Tenant string `json:"tenant"`
	Name   string `json:"name"`
	// Signed is the manifest as signed; Manifest is its decoded payload.
	Signed   SignedManifest `json:"signed"`
	Manifest Manifest       `json:"-"`
	State    State          `json:"state"`
	// Verb is the verb in progress while State is transitioning.
	Verb Verb `json:"verb,omitempty"`
	// Revision counts deploys and redeploys; Restarts counts restarts.
	Revision int64 `json:"revision"`
	Restarts int64 `json:"restarts"`
	// Grants holds, per kind, the reference the grant returned.
	Grants map[GrantKind]string `json:"grants,omitempty"`
	// Cleanup is set while the record's grants may not match its manifest:
	// a verb granted or revoked part way and could not undo it. Grants then
	// lists every reference that may still be in force, for undeploy to
	// revoke, and
	// start, resume and restart refuse the record until a redeploy grants
	// it anew.
	Cleanup   bool      `json:"cleanup,omitempty"`
	Reason    string    `json:"reason,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`
	// Release is the release the service runs once a rollout switched it;
	// its manifest is then the service's Manifest. Nil for a service that
	// runs the manifest it was deployed with.
	Release *Release `json:"release,omitempty"`
	// Slot is the A/B slot the service's running instance occupies; empty
	// for the instance Deploy launched.
	Slot string `json:"slot,omitempty"`
	// Slots is the release each filled slot runs, the active one included.
	Slots map[string]Release `json:"slots,omitempty"`
	// Epoch is the newest rollout claim a slot change was made under; a
	// slot change under an older one is refused (ErrFenced).
	Epoch uint64 `json:"epoch,omitempty"`
}

// Guard arbitrates writes: Create writes only an absent key, Swap replaces
// only the value held. go-qrdb's cas.Writer is the server's Guard; each call
// is one Raft proposal, so racing nodes get one winner.
type Guard interface {
	Create(ctx context.Context, key string, value []byte) (bool, error)
	Swap(ctx context.Context, key string, held, next []byte) (bool, error)
}

// Reader reads and removes records; interfaces.KVStore satisfies it.
type Reader interface {
	Get(ctx context.Context, key string) ([]byte, error)
	Scan(ctx context.Context, prefix string, fn func(key string, value []byte) error) error
	Delete(ctx context.Context, key string) error
}

// Store keeps service records in go-qrdb under serviceplane/services/.
type Store struct {
	guard Guard
	kv    Reader
}

// NewStore returns a store over guard and kv.
func NewStore(guard Guard, kv Reader) (*Store, error) {
	if guard == nil || kv == nil {
		return nil, fmt.Errorf("%w: a store needs a guard and a reader", ErrConfig)
	}
	return &Store{guard: guard, kv: kv}, nil
}

const keyPrefix = "serviceplane/services/"

func serviceKey(tenant, name string) string { return keyPrefix + tenant + "/" + name }

// stored is a record and the exact bytes it was read as, which a Swap holds.
type stored struct {
	svc *Service
	raw []byte
}

func (s *Store) create(ctx context.Context, svc *Service) (*stored, error) {
	raw, err := json.Marshal(svc)
	if err != nil {
		return nil, s.fail("deploy", svc.Tenant, svc.Name, ErrStore, err)
	}
	ok, err := s.guard.Create(ctx, serviceKey(svc.Tenant, svc.Name), raw)
	if err != nil {
		return nil, s.fail("deploy", svc.Tenant, svc.Name, ErrStore, err)
	}
	if !ok {
		return nil, s.fail("deploy", svc.Tenant, svc.Name, ErrExists, nil)
	}
	return &stored{svc: svc, raw: raw}, nil
}

func (s *Store) load(ctx context.Context, op, tenant, name string) (*stored, error) {
	if tenant == "" || name == "" {
		return nil, s.fail(op, tenant, name, ErrNotFound, nil)
	}
	raw, err := s.kv.Get(ctx, serviceKey(tenant, name))
	if err != nil {
		if qberrors.IsNotFound(err) {
			return nil, s.fail(op, tenant, name, ErrNotFound, nil)
		}
		return nil, s.fail(op, tenant, name, ErrStore, err)
	}
	if raw == nil {
		return nil, s.fail(op, tenant, name, ErrNotFound, nil)
	}
	svc, err := decodeService(raw)
	if err != nil {
		return nil, s.fail(op, tenant, name, ErrStore, err)
	}
	return &stored{svc: svc, raw: raw}, nil
}

// swap replaces cur with next; losing the arbitration is ErrConflict.
func (s *Store) swap(ctx context.Context, op string, cur *stored, next *Service) (*stored, error) {
	raw, err := json.Marshal(next)
	if err != nil {
		return nil, s.fail(op, next.Tenant, next.Name, ErrStore, err)
	}
	ok, err := s.guard.Swap(ctx, serviceKey(next.Tenant, next.Name), cur.raw, raw)
	if err != nil {
		return nil, s.fail(op, next.Tenant, next.Name, ErrStore, err)
	}
	if !ok {
		return nil, s.fail(op, next.Tenant, next.Name, ErrConflict, nil)
	}
	return &stored{svc: next, raw: raw}, nil
}

func (s *Store) remove(ctx context.Context, op string, svc *Service) error {
	if err := s.kv.Delete(ctx, serviceKey(svc.Tenant, svc.Name)); err != nil && !qberrors.IsNotFound(err) {
		return s.fail(op, svc.Tenant, svc.Name, ErrStore, err)
	}
	return nil
}

// Get returns the tenant's service name.
func (s *Store) Get(ctx context.Context, tenant, name string) (*Service, error) {
	st, err := s.load(ctx, "get", tenant, name)
	if err != nil {
		return nil, err
	}
	return st.svc, nil
}

// List returns the tenant's services in name order.
func (s *Store) List(ctx context.Context, tenant string) ([]*Service, error) {
	if strings.TrimSpace(tenant) == "" || strings.Contains(tenant, "/") {
		return nil, s.fail("list", tenant, "", ErrNotFound, nil)
	}
	var out []*Service
	err := s.kv.Scan(ctx, keyPrefix+tenant+"/", func(_ string, value []byte) error {
		svc, err := decodeService(value)
		if err != nil {
			return err
		}
		out = append(out, svc)
		return nil
	})
	if err != nil {
		return nil, s.fail("list", tenant, "", ErrStore, err)
	}
	return out, nil
}

// listAll returns every tenant's services, for recovery of abandoned claims.
func (s *Store) listAll(ctx context.Context) ([]*Service, error) {
	var out []*Service
	err := s.kv.Scan(ctx, keyPrefix, func(_ string, value []byte) error {
		svc, err := decodeService(value)
		if err != nil {
			return err
		}
		out = append(out, svc)
		return nil
	})
	if err != nil {
		return nil, s.fail("recover", "", "", ErrStore, err)
	}
	return out, nil
}

func (s *Store) fail(op, tenant, name string, sentinel, cause error) error {
	return &ServiceError{Op: op, Tenant: tenant, Name: name, Sentinel: sentinel, Cause: cause}
}

func decodeService(raw []byte) (*Service, error) {
	var svc Service
	if err := json.Unmarshal(raw, &svc); err != nil {
		return nil, err
	}
	if svc.Release != nil {
		svc.Manifest = svc.Release.Manifest
		return &svc, nil
	}
	if err := json.Unmarshal(svc.Signed.Payload, &svc.Manifest); err != nil {
		return nil, err
	}
	return &svc, nil
}
