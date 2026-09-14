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

package orchestration

import (
	"context"
	"errors"
	"strings"
	"sync"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// =============================================================================
// Orchestration KV keyspace
// =============================================================================

// Every durable orchestration record lives under a keyspace whose FIRST
// variable segment is the owning tenant. Tenant isolation is therefore a
// property of the key layout rather than of caller discipline: a read is a
// point lookup at a key derived from the caller-supplied tenant, and a list is
// a prefix scan bounded to that tenant's subtree. There is no key shape under
// which tenant A's scan can observe tenant B's records.
//
//	orch/project/{tenant}/{projectID}          -> Project (JSON)
//	orch/projectname/{tenant}/{name}           -> projectID (name index)
//	orch/agent/{tenant}/{agentID}              -> AgentRegistration (JSON)
//	orch/task/{tenant}/{taskID}                -> Task (JSON)
//	orch/taskartifact/{tenant}/{taskID}/{name} -> artifact payload (bytes)
//	orch/session/{tenant}/{sessionID}          -> SessionRecord (JSON)
//
// Keys carry identifiers only. No secret, credential, token or payload value
// is ever encoded into a key, so key material stays safe to log and to expose
// in operator tooling.
const (
	// keySeparator delimits key segments. It is forbidden inside a segment so
	// that segment boundaries are unambiguous and prefixes cannot be forged.
	keySeparator = "/"

	// projectKeyspace holds Project records.
	projectKeyspace = "orch/project/"

	// projectNameKeyspace holds the tenant-scoped name -> projectID index.
	projectNameKeyspace = "orch/projectname/"

	// agentKeyspace holds AgentRegistration records.
	agentKeyspace = "orch/agent/"

	// taskKeyspace holds Task records.
	taskKeyspace = "orch/task/"

	// taskSourceKeyspace holds the tenant+project+source -> taskID index that
	// keeps one task source from being adopted twice.
	taskSourceKeyspace = "orch/task-source/"

	// taskArtifactKeyspace holds task artifact payloads offloaded from the
	// task record because they exceed the inline limit.
	taskArtifactKeyspace = "orch/taskartifact/"

	// sessionKeyspace holds SessionRecord entries — the durable description of
	// an agent session, from which a persistent session is reconstituted after
	// a daemon restart.
	sessionKeyspace = "orch/session/"

	// accountKeyspace holds Account metadata records (never credential
	// material — that lives behind interfaces.SecretsManager, addressed by
	// Account.ID; see account_secret_store.go).
	accountKeyspace = "orch/account/"

	// accountDefaultKeyspace holds the tenant+provider(+project) -> accountID
	// default-selection pointer.
	accountDefaultKeyspace = "orch/accountdefault/"
)

// =============================================================================
// Key segment validation
// =============================================================================

// ErrInvalidKeySegment indicates an identifier cannot be used as a key segment.
var ErrInvalidKeySegment = errors.New("orchestration store: invalid key segment")

// KeySegmentError reports an identifier that cannot safely form a key segment.
type KeySegmentError struct {
	Field   string
	Value   string
	Message string
}

// Error implements the error interface.
func (e *KeySegmentError) Error() string {
	return ErrInvalidKeySegment.Error() + ": " + e.Field + ": " + e.Message
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *KeySegmentError) Unwrap() error { return ErrInvalidKeySegment }

// validateKeySegment rejects identifiers that would break the key layout's
// isolation guarantee. An empty segment collapses two keys into one; a segment
// containing the separator lets a crafted identifier escape its tenant subtree
// (a "tenant-a/x" project ID under tenant "t" would otherwise be
// indistinguishable from project "x" under tenant "t/tenant-a").
func validateKeySegment(field, value string) error {
	if value == "" {
		return &KeySegmentError{Field: field, Value: value, Message: "must not be empty"}
	}
	if strings.Contains(value, keySeparator) {
		return &KeySegmentError{
			Field:   field,
			Value:   value,
			Message: "must not contain " + keySeparator,
		}
	}
	return nil
}

// tenantPrefix returns the scan prefix covering every record of one entity
// type owned by tenantID. Callers list with this prefix and can therefore
// never observe another tenant's records.
func tenantPrefix(keyspace, tenantID string) (string, error) {
	if err := validateKeySegment("tenant_id", tenantID); err != nil {
		return "", err
	}
	return keyspace + tenantID + keySeparator, nil
}

// entityKey returns the key addressing one record. Both the tenant and the
// entity identifier are validated, so a caller cannot construct a key outside
// the tenant's subtree.
func entityKey(keyspace, tenantID, idField, id string) (string, error) {
	prefix, err := tenantPrefix(keyspace, tenantID)
	if err != nil {
		return "", err
	}
	if err := validateKeySegment(idField, id); err != nil {
		return "", err
	}
	return prefix + id, nil
}

// idFromKey returns the trailing identifier of a key produced by entityKey.
func idFromKey(key, prefix string) string {
	return strings.TrimPrefix(key, prefix)
}

// =============================================================================
// Per-key serialization
// =============================================================================

// keyedMutex serializes read-modify-write sequences per key.
//
// interfaces.KVStore exposes no guarded-swap verb — go-qrdb's KV surface
// re-exports PutIfAbsent and CompareAndDelete, a guarded create and a guarded
// delete, and no swap of a value — so a store that needs an atomic update reads
// the current record, validates it, and writes the successor. Holding a per-key
// lock across that sequence makes the update atomic against other writers in
// this process, and the record's own version counter detects a writer that
// raced from another process or node. This mirrors how KVSessionEventLog guards
// sequence monotonicity (pkg/orchestration/session_log_kv.go).
//
// DETECTION IS NOT ARBITRATION, and nothing here decides a contest. A record
// whose contenders must produce ONE WINNER rather than one survivor is decided
// before it reaches this lock, on go-qrdb's transaction plane where a compare
// and a write are a single Raft proposal — see task_claim_arbiter.go. Reaching
// for this mutex to make a claim exclusive is the defect that put nine workers
// on go-frost#6.
//
// THIS WAS A HABIT, NOT AN INCIDENT, AND THAT IS WHY THE WARNING LIVES HERE
// RATHER THAN AT ANY ONE CALL SITE. Four separate stores independently wrote a
// comment asserting that holding this lock across a probe and a write made the
// pair atomic, and each was true in one process and false across a fleet:
//
//   - task_store.go create — "the existence probe and the write are one atomic
//     sequence, so two concurrent creates of the same ID cannot both observe an
//     empty key"
//   - agent_registry.go Register — "presence is checked under the key lock
//     rather than by a blind Put"
//   - task_store.go claim — the version counter described as preventing a
//     cross-node overwrite when it only DETECTS one
//   - project_store.go and account_store.go — the same construction, for name
//     uniqueness and for a scope's default pointer
//
// Nobody questioned the sentence the first three times, because each instance
// read as a local observation about a local lock. The pattern is only visible
// from the primitive they all reached for. If you are about to write "under the
// lock, so this is atomic", the lock is this one, and the sentence is wrong the
// moment a second process exists.
type keyedMutex struct {
	mu     sync.Mutex
	locks  map[string]*sync.Mutex
	inited bool
}

// lock acquires the mutex guarding key and returns its release function.
func (k *keyedMutex) lock(key string) func() {
	k.mu.Lock()
	if !k.inited {
		k.locks = make(map[string]*sync.Mutex)
		k.inited = true
	}
	mu, ok := k.locks[key]
	if !ok {
		mu = &sync.Mutex{}
		k.locks[key] = mu
	}
	k.mu.Unlock()

	mu.Lock()
	return mu.Unlock
}

// =============================================================================
// Store audit seam
// =============================================================================

// Store mutation actions recorded on the audit trail. FedRAMP AU-2, PCI-DSS
// 10.2 and HIPAA 164.312(b) all require create/update/delete of protected
// records to be attributable, so every mutating store method emits one event.
const (
	// AuditActionCreate records the creation of a record.
	AuditActionCreate = "create"

	// AuditActionUpdate records a modification to an existing record.
	AuditActionUpdate = "update"

	// AuditActionDelete records the removal of a record.
	AuditActionDelete = "delete"

	// AuditActionReregister records that an already-registered agent
	// registered again and had its record converged onto what it reported
	// (RefreshAgent). Distinct from AuditActionUpdate because the actor is
	// the agent itself rather than an operator, and because its refusals --
	// a principal that does not own the record, an agent an operator
	// disabled -- are the identity-takeover trail an auditor reviews.
	AuditActionReregister = "reregister"

	// AuditActionUse records that a record's credential material was handed
	// to a spawn (Account selection/resolution). Distinct from
	// AuditActionUpdate because a "use" mutates only bookkeeping
	// (LastUsedAt) and is expected to happen far more often — separating the
	// action lets an auditor distinguish routine use from an operator
	// changing the record itself.
	AuditActionUse = "use"
)

// Resource types reported on store audit events.
const (
	// AuditResourceProject identifies a Project record.
	AuditResourceProject = "orchestration_project"

	// AuditResourceAgent identifies an AgentRegistration record.
	AuditResourceAgent = "orchestration_agent"

	// AuditResourceTask identifies a Task record.
	AuditResourceTask = "orchestration_task"

	// AuditResourceSession identifies a SessionRecord.
	AuditResourceSession = "orchestration_session"

	// AuditResourceAccount identifies an Account record.
	AuditResourceAccount = "orchestration_account"
)

// StoreAuditEvent describes a single mutation of a durable orchestration
// record. It deliberately carries identifiers only — never record contents —
// so the audit trail cannot become a second copy of the data.
type StoreAuditEvent struct {
	// TenantID owns the mutated record.
	TenantID string

	// Resource is the record type, one of the AuditResource* constants.
	Resource string

	// ResourceID identifies the mutated record.
	ResourceID string

	// Action is one of the AuditAction* constants.
	Action string

	// Success reports whether the mutation was applied.
	Success bool
}

// StoreAuditor receives store mutation events. The orchestration package
// defines the narrow sink it needs; the server binds it to the platform audit
// logger (pkg/iam.AuditLogger) so orchestration records land on the same
// tamper-evident trail as IAM records.
type StoreAuditor interface {
	// RecordStoreEvent appends one mutation to the audit trail. It must not
	// block the caller: implementations are expected to hand off
	// asynchronously.
	RecordStoreEvent(ctx context.Context, event StoreAuditEvent)
}

// emitStoreAudit records event when an auditor is configured. A nil auditor
// disables auditing, which is the default for unit tests and embedded use.
func emitStoreAudit(ctx context.Context, auditor StoreAuditor, event StoreAuditEvent) {
	if auditor == nil {
		return
	}
	auditor.RecordStoreEvent(ctx, event)
}

// =============================================================================
// Shared store configuration
// =============================================================================

// storeConfig holds the settings shared by the durable orchestration stores.
type storeConfig struct {
	auditor     StoreAuditor
	logger      interfaces.Logger
	inlineLimit int64
	spend       SpendAuthority
	grantor     AccountDeliveryGrantor
}

// StoreOption configures a durable orchestration store.
type StoreOption func(*storeConfig)

// WithStoreAuditor routes create/update/delete events to auditor.
func WithStoreAuditor(auditor StoreAuditor) StoreOption {
	return func(c *storeConfig) { c.auditor = auditor }
}

// WithStoreLogger installs the logger a store component reports a non-fatal
// bookkeeping failure on — a write that must not fail the operation in flight
// but must not vanish either (see accountResolver.logTouchFailure). Nil, the
// default, discards those reports.
func WithStoreLogger(logger interfaces.Logger) StoreOption {
	return func(c *storeConfig) { c.logger = logger }
}

// WithSpendAuthority installs the gate a credential release is checked
// against (see account_spend_authority.go). Nil, the default, leaves the
// release guarded only by the account's lifecycle state — which is what a
// deployment with no ledger wired has, and is why the server always wires one.
func WithSpendAuthority(spend SpendAuthority) StoreOption {
	return func(c *storeConfig) { c.spend = spend }
}

// WithAccountDeliveryGrantor installs the authority that supplies the grant
// covering each DELIVERY of a pooled provider credential (see account_lease.go).
// Nil, the default, leaves a credential release unleased — which is what a
// deployment with no lease manager has, and is why the server always wires one.
//
// It takes the one-verb AccountDeliveryGrantor rather than the full leaser
// because a resolution only ever needs a grant supplied: the control plane
// passes its issuing leaser, and a worker daemon passes the reporter that names
// the grant the control plane already issued for its release.
//
// With a grantor installed, a release that cannot obtain a grant FAILS: a
// credential nothing can revoke is the state the mechanism exists to remove.
func WithAccountDeliveryGrantor(grantor AccountDeliveryGrantor) StoreOption {
	return func(c *storeConfig) { c.grantor = grantor }
}

// WithInlineArtifactLimit sets the maximum artifact size, in bytes, embedded
// directly in a task record. Larger artifacts are written to their own key and
// referenced from the record. A non-positive limit selects InlineLimit.
func WithInlineArtifactLimit(limit int64) StoreOption {
	return func(c *storeConfig) { c.inlineLimit = limit }
}

// newStoreConfig applies opts over the package defaults.
func newStoreConfig(opts ...StoreOption) storeConfig {
	cfg := storeConfig{inlineLimit: InlineLimit}
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	if cfg.inlineLimit <= 0 {
		cfg.inlineLimit = InlineLimit
	}
	return cfg
}
