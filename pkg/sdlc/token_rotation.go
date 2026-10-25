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

package sdlc

import (
	"context"
	"errors"
	"strconv"
	"sync"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/secrets"
	"github.com/jeremyhahn/go-quicbaas/pkg/util/id"
)

// =============================================================================
// Forge token rotation
// =============================================================================
//
// The job lives here, in the domain that owns the forge, and USES the secrets
// store and the scheduler. A storage layer importing this domain would be an
// inverted edge.
//
// Nothing about the storage half is new: SecretsService already versions,
// reads back and rolls back, which is exactly what makes an unverifiable
// replacement survivable.

// tokenSecretKey is the field the token itself is stored under.
const tokenSecretKey = "token"

// metadataRotationSlot records which rotation produced the stored token. It is
// what makes the NEXT token's name derivable from the stored state alone.
const metadataRotationSlot = "rotation_slot"

// metadataTokenName records the forge-side name of the stored token, so
// revocation never has to guess.
const metadataTokenName = "token_name"

// metadataPendingRollback records, on the intent record, the store version a
// failed rotation could not roll back to. While it stands, the store holds an
// UNVERIFIED replacement and the previous credential is live on the forge but
// recorded nowhere else: recovery retries the rollback before anything is
// revoked, and no new rotation may run over it — a rotation that did would
// revoke the replacement, overwrite this record, and lose the live original's
// identity permanently.
const metadataPendingRollback = "pending_rollback_version"

// metadataRevokePending records, on the intent record, the OLD token a
// verified rotation still owes a revoke for. From the moment the record
// stops being staged until the revoke lands, this is the ONLY place the old
// credential's identity exists: the store's current version already names
// the replacement. Erasing it before the revoke — which the post-verify
// write once did — left a live privileged forge token that nothing named,
// with recovery reading the rotation as complete and the next rotation
// overwriting the record.
const metadataRevokePending = "revoke_pending_token"

// metadataIntentOwner records, on the intent record, the forge account owning
// the tokens the record names — the account recovery must act as.
const metadataIntentOwner = "owner_username"

// rotationIntentSuffix names the sibling record holding a rotation's intent.
//
// A sibling rather than another version of the credential itself: the intent is
// not a credential, and interleaving the two would make the token's version
// history alternate between real secrets and bookkeeping.
const rotationIntentSuffix = ".rotation-intent"

// identityScope is the minimum a replacement needs to verify itself.
//
// Without it the new token cannot answer whoami, verification cannot run, and
// rotation would have to revoke the old credential on faith.
const identityScope = "read:user"

// TokenSecretStore is the slice of the secrets service rotation needs.
//
// Narrow on purpose: rotation needs to write a version, read the current one,
// enumerate versions, roll back — and, on one recovery arm only, destroy a
// path outright so a failed FIRST rotation resolves to the absence a first
// boot starts from rather than to a revoked credential posing as current.
// *secrets.Manager satisfies this as it stands, and a narrow port keeps the
// job testable without a secret store.
// Get and ListVersions MUST report an ABSENT secret with an error satisfying
// errors.Is(err, secrets.ErrSecretNotFound), and report every other failure
// with an error that does not. Rotation's safety depends on telling "there is
// no credential here" apart from "I could not read the credential": the first
// is an ordinary first rotation, the second means nothing about the stored
// state can be trusted. (*secrets.Manager honors this since the
// EncryptedBackend classification fix, go-quicbaas#109.)
type TokenSecretStore interface {
	// Store commits BY COPY: implementations must not retain the caller's
	// map or value slices past the call — rotation hands Store a borrowed
	// view into an enclave it destroys on return (review 991). Every real
	// backend already commits this way (deep copy or synchronous marshal).
	Store(ctx context.Context, path string, data map[string][]byte, metadata map[string]string) error
	Get(ctx context.Context, path string) (map[string][]byte, map[string]string, error)

	// UseSecret is the GUARDED-USE read: it exposes the value stored under
	// key at path to fn for the duration of the call only, and the
	// implementation MUST wipe every byte the call fetched before returning
	// — the caller never owns the bytes. fn MUST NOT retain value (or any
	// slice aliasing it) beyond its own return. An absent path reports as
	// Get does (errors.Is secrets.ErrSecretNotFound); a record present
	// without a nonempty value under key reports with an error satisfying
	// errors.Is(err, secrets.ErrSecretKeyAbsent) and fn is never called.
	// This is the seam recovery verifies credentials through, so the token
	// is never materialized as a string or retained in an ordinary map
	// (review 959); custody across the REST of the credential's lifetime
	// (mint responses, transport) is go-quicbaas#97.
	UseSecret(ctx context.Context, path, key string, fn func(value []byte) error) error

	ListVersions(ctx context.Context, path string) ([]*interfaces.SecretVersionMetadata, error)
	Rollback(ctx context.Context, path string, version int) error
	Destroy(ctx context.Context, path string) error
}

// readStoredToken reads a credential record's METADATA, separating ABSENT
// from UNREADABLE.
//
// Conflating the two is what made recovery able to destroy a live credential:
// a failed read looked identical to "nothing stored", so the guard that should
// have protected the in-force token was skipped and revocation proceeded. The
// distinction was always available — pkg/secrets reports absence with
// ErrSecretNotFound — and it now gets used.
//
// The secret DATA is deliberately not returned: every decision this file
// makes on a stored record reads metadata, and the one moment the credential
// bytes themselves are needed (recovery's verification) goes through the
// store's guarded UseSecret instead. The copies Get materialized here are
// wiped before returning, so a metadata read never leaves token bytes live
// in this process's heap (review 959).
func readStoredToken(ctx context.Context, store TokenSecretStore, path string) (
	metadata map[string]string, absent bool, err error) {
	data, meta, err := store.Get(ctx, path)
	for _, v := range data {
		for i := range v {
			v[i] = 0
		}
	}
	switch {
	case err == nil:
		return meta, false, nil
	case errors.Is(err, secrets.ErrSecretNotFound):
		return nil, true, nil
	default:
		return nil, false, err
	}
}

// TokenRotationSpec describes one rotatable forge credential.
type TokenRotationSpec struct {
	// Purpose names what the credential is for, and is the stable half of the
	// token's forge-side name — see RotationTokenName.
	Purpose string

	// Username is the forge account the token belongs to.
	Username string

	// SecretPath is where the token lives in the secrets service.
	SecretPath string

	// Scopes the replacement should carry. identityScope is always added:
	// a token that cannot verify itself cannot be rotated to safely.
	Scopes []string
}

// RotationResult reports what one rotation did.
type RotationResult struct {
	// NewTokenName is the forge-side name of the replacement.
	NewTokenName string

	// RevokedTokenName is the name of the credential destroyed, empty when
	// there was nothing to revoke (a first rotation).
	RevokedTokenName string

	// Slot is the rotation slot the replacement occupies.
	Slot uint64
}

// RotationTokenName builds a token's forge-side name.
//
// DETERMINISTIC BY CONTRACT, because RevokeByName is only useful if the name
// can be recomputed rather than remembered. Given a purpose and a slot, anyone
// — the job, a recovery pass, an operator reading the secret's metadata — can
// derive the name of the token that rotation was about to create, which is what
// turns "crashed between minting and persisting" into a lookup instead of a
// guess.
func RotationTokenName(purpose string, slot uint64) string {
	return "sdlc-" + purpose + "-v" + strconv.FormatUint(slot, 10)
}

// TokenRotator rotates forge credentials.
//
// SINGLE-FLIGHT PER PATH, PROCESS-WIDE. Rotate and RecoverOrphan are
// read-then-act protocols over the same records, and the scheduler does not
// guarantee a job fires once — a manual trigger can land beside a tick. The
// per-path lock registry is PACKAGE-LEVEL, not per rotator instance: two
// rotators constructed over the same store would otherwise hold disjoint
// locks, both pass the current/intent checks, derive the same slot and
// name, and overwrite the single intent record — the per-instance defect
// the work-item stripe registry was cured of on the same branch family.
// ACROSS processes nothing here arbitrates: the secrets store port carries
// no guarded write, so the boundary is the process — which is exactly what
// the module's boot enforces (startSDLC refuses cluster-like modes; nothing
// constructs a rotator outside that host) — until the storage-level
// compare-and-swap work lands (go-quicbaas#105).
type TokenRotator struct {
	lifecycle ForgeTokenLifecycle
	store     TokenSecretStore
}

// rotationPathLocks serializes Rotate and RecoverOrphan per secret path,
// process-wide. Striped: two paths may share a stripe and serialize
// harmlessly; one path never spans two stripes, and every rotator instance
// in the process contends on the same stripes.
var rotationPathLocks [16]sync.Mutex

// NewTokenRotator builds a rotator, failing closed on missing collaborators.
func NewTokenRotator(lifecycle ForgeTokenLifecycle, store TokenSecretStore) (*TokenRotator, error) {
	if lifecycle == nil {
		return nil, &InvalidInputError{Field: "lifecycle", Message: "must not be nil"}
	}
	if store == nil {
		return nil, &InvalidInputError{Field: "store", Message: "must not be nil"}
	}
	return &TokenRotator{lifecycle: lifecycle, store: store}, nil
}

// lockPath returns the process-wide stripe lock for a secret path.
func lockPath(path string) *sync.Mutex {
	return &rotationPathLocks[id.HashString(path)%uint64(len(rotationPathLocks))]
}

// Rotate replaces a forge token, and never destroys the old one on faith.
//
// The order is stage, mint, store, verify, revoke. Storing before verifying
// is what makes an unverifiable replacement survivable: the credential is a
// new VERSION and the previous one is still there to roll back to. Revocation
// is last and happens only after the replacement has authenticated as the
// right account.
//
// THE INTENT RECORD IS AHEAD OF EVERY STATE IT DESCRIBES. Before the mint it
// names the token that may come to exist; before the store it is STAGED with
// the version a failed attempt must roll back to. A crash at any point
// therefore leaves a record that RecoverOrphan can act on — there is no
// window in which the store holds an unverified replacement that no record
// admits to.
func (r *TokenRotator) Rotate(ctx context.Context, spec TokenRotationSpec) (*RotationResult, error) {
	if spec.Purpose == "" {
		return nil, &InvalidInputError{Field: "purpose", Message: "must not be empty"}
	}
	if spec.Username == "" {
		return nil, &InvalidInputError{Field: "username", Message: "must not be empty"}
	}
	if spec.SecretPath == "" {
		return nil, &InvalidInputError{Field: "secret_path", Message: "must not be empty"}
	}

	lock := lockPath(spec.SecretPath)
	lock.Lock()
	defer lock.Unlock()

	// What is in force now. Nothing stored is an ordinary FIRST rotation; a
	// failed read is not, and must not be treated as one — doing so would
	// restart the slot counter at 1, mint a name that already exists on the
	// forge, and silently lose the rotation lineage.
	currentMeta, _, err := readStoredToken(ctx, r.store, spec.SecretPath)
	if err != nil {
		return nil, err
	}

	// A STAGED INTENT BLOCKS ROTATION. In that state the "current" token just
	// read may be an unverified replacement, and the intent record is the
	// only thing that knows it. Rotating over it would mint on top of the
	// replacement, revoke it as "the old token", and overwrite the intent —
	// permanently losing the identity of the still-live original credential.
	// The intent is read fail-closed: an unreadable record proves nothing.
	intent, intentAbsent, err := loadRotationIntent(ctx, r.store, spec.SecretPath)
	if err != nil {
		return nil, err
	}
	if !intentAbsent && intent.Staged {
		return nil, &RotationRecoveryPendingError{
			SecretPath: spec.SecretPath,
			Name:       intent.Name,
			Version:    strconv.Itoa(intent.PendingRollback),
		}
	}
	// A PENDING REVOKE BLOCKS ROTATION THE SAME WAY: the record is the old
	// token's only remaining identity, and the intent-before-mint write below
	// would erase it — sealing the leak the verified phase exists to prevent.
	if !intentAbsent && intent.OldToken != "" {
		return nil, &RotationRevokePendingError{
			SecretPath: spec.SecretPath,
			OldToken:   intent.OldToken,
		}
	}
	// ANY OTHER UNRESOLVED INTENT BLOCKS ROTATION TOO. An unstaged intent
	// with a name is a mint-window orphan: the token may exist on the forge,
	// and this record is its ONLY durable identity. The intent-before-mint
	// write below would overwrite it, permanently losing the name needed to
	// revoke a possibly live privileged credential. The scheduler's handler
	// recovers before it rotates, but Rotate is exported and must enforce
	// the sequencing itself rather than assume its caller did —
	// RecoverOrphan's original arm resolves exactly this shape, so the
	// refusal is transient.
	if !intentAbsent && intent.Name != "" {
		return nil, &RotationUnresolvedIntentError{
			SecretPath: spec.SecretPath,
			Name:       intent.Name,
		}
	}

	slot := nextRotationSlot(currentMeta)
	name := RotationTokenName(spec.Purpose, slot)

	// The rollback target is established BEFORE the forge is touched — and
	// the rotation ABORTS if it cannot be. Treating an enumeration failure as
	// version 0 (which this once did) meant a later verification failure
	// skipped the rollback, revoked the replacement, and left the store
	// pointing at a revoked credential: automation locked out by its own
	// safety path. Aborting here costs nothing: no token has been minted.
	previousVersion, err := latestVersion(ctx, r.store, spec.SecretPath)
	if err != nil {
		return nil, err
	}

	// INTENT BEFORE MINT. A crash after this point leaves a record naming the
	// token that may or may not exist on the forge, so recovery can revoke an
	// orphan by name instead of leaving a live credential nobody knows about.
	if err := saveRotationIntent(ctx, r.store, spec.SecretPath,
		rotationIntent{Name: name, Slot: slot, Username: spec.Username}); err != nil {
		return nil, err
	}

	minted, err := r.lifecycle.MintToken(ctx, TokenSpec{
		Username: spec.Username,
		Name:     name,
		Scopes:   withIdentityScope(spec.Scopes),
	})
	if err != nil {
		return nil, err
	}
	// THE MINT RESPONSE IS VALIDATED BEFORE ANYTHING DEPENDS ON IT, and a
	// DIVERGENT NAME IS A CONTRACT VIOLATION, not an input (ruled
	// 2026-08-23). Every recovery decision downstream compares stored
	// metadata against the intent's REQUESTED name; a forge answering a
	// different name would make a committed store write look uncommitted —
	// the requested name revoked as a 404, the intent discharged, the real
	// credential current and untracked. Rather than absorbing the rename
	// into the recorded identity, it is REFUSED: RotationTokenName is
	// deterministic by contract precisely so every party can recompute the
	// name, and a forge that renames breaks that contract deliberately or
	// not at all. The returned token is revoked here — the one moment its
	// actual name is in hand — with the intent REPOINTED AT THAT NAME
	// FIRST (review 974): the quiet revoke this once was discarded its
	// failure while the durable intent named only the requested token, so
	// a failed revoke left the real credential live with recovery hunting
	// a name the forge never knew. An empty returned name is
	// refused INDEPENDENTLY of the equality (defense in depth: an identity
	// nothing can later revoke by name must never be recorded), and nil /
	// empty-secret answers are the (nil, nil) class — claims of success
	// carrying nothing usable. The requested name itself is structurally
	// nonempty: spec.Purpose is validated at entry and RotationTokenName
	// prefixes it, so the equality can never hold vacuously.
	if minted != nil {
		// THE ENCLAVE DIES WITH THIS CALL, on every path — the adapter
		// handed us the secret in the stack-owned secure buffer (#97
		// rotation slice); Destroy wipes and releases the mlocked pages,
		// and nothing may outlive the rotation that minted it. The store
		// receives its own copy below (its custody: #97's remaining
		// checklist), and every other use is a borrowed view into the
		// enclave under a no-retain contract.
		defer minted.Secret.Destroy()
	}
	// THE MALFORMED-MINT ARMS, enumerated (extend HERE when MintedToken
	// gains a field): nil response; nil secure buffer; empty secret; empty
	// name; divergent name. The nil-buffer arm is EXPLICIT even though the
	// buffer's methods are nil-receiver-safe by contract — a refusal this
	// load-bearing must not rest on a library's defensive nil checks
	// (review 995).
	if minted == nil || minted.Secret == nil || minted.Secret.Size() == 0 ||
		minted.Name == "" || minted.Name != name {
		mintErr := newMintResponseError(name, minted)
		if minted != nil && minted.Name != "" && minted.Name != name {
			// THE ACTUAL NAME IS PERSISTED BEFORE THE CLEANUP: from the
			// save onward the durable record and the live orphan agree, so
			// a failed revoke leaves recovery retrying the RIGHT name.
			if saveErr := saveRotationIntent(ctx, r.store, spec.SecretPath,
				rotationIntent{Name: minted.Name, Slot: slot, Username: spec.Username}); saveErr != nil {
				// The record still names the requested token. The revoke is
				// attempted anyway — the actual name is in hand only here.
				// Success leaves nothing live (recovery revokes the requested
				// name as a tolerated 404 and discharges); failure of BOTH is
				// returned joined, naming the live token, because recovery
				// cannot find it and an operator must.
				if revErr := r.revokeTolerant(ctx, spec.Username, minted.Name); revErr != nil {
					return nil, errors.Join(mintErr,
						&RotationIncompleteError{Name: minted.Name, Cause: errors.Join(saveErr, revErr)})
				}
				return nil, mintErr
			}
			if revErr := r.revokeTolerant(ctx, spec.Username, minted.Name); revErr != nil {
				// The standing intent names the live token: the next
				// recovery pass retries this exact revoke and discharges
				// only after it lands.
				return nil, errors.Join(mintErr, revErr)
			}
			// Revoked. The discharge is best-effort for the reason the
			// success path's is: a lost discharge costs one recovery pass
			// (re-revoke, tolerated 404, discharge).
			_ = dischargeRotationIntent(ctx, r.store, spec.SecretPath)
		}
		return nil, mintErr
	}

	// STAGE BEFORE STORE. From the moment the replacement can be in the
	// store, the intent record says so and names the version to roll back to.
	// Persisting this AFTER a failure was the ambiguity that let recovery
	// read a stuck rotation as a completed one; persisting it before the
	// store closes the crash window between the store and the verification
	// as well. A failed staging write aborts with only the minted orphan at
	// stake, which the unstaged intent already covers.
	if err := saveRotationIntent(ctx, r.store, spec.SecretPath,
		rotationIntent{Name: name, Slot: slot, Staged: true, PendingRollback: previousVersion,
			Username: spec.Username}); err != nil {
		r.revokeQuietly(ctx, spec.Username, name)
		return nil, err
	}

	// THE STORE RECEIVES A BORROWED VIEW into the enclave, not a copy
	// (review 991): every backend commits by copy — the memory backend
	// deep-copies, the KV and encrypted backends marshal synchronously —
	// so no callee retains the view past the call, and the one plaintext
	// copy that used to sit unwiped in our heap no longer exists. The
	// real-manager first-rotation test is the proof: it reads the secret
	// back AFTER the enclave died, which only works if the store took its
	// own copy. TokenSecretStore.Store carries the copy-on-store contract.
	err = minted.Secret.WithBytes(func(secret []byte) error {
		return r.store.Store(ctx, spec.SecretPath,
			map[string][]byte{tokenSecretKey: secret},
			map[string]string{
				metadataRotationSlot: strconv.FormatUint(slot, 10),
				metadataTokenName:    minted.Name,
			})
	})
	if err != nil {
		// AN ERROR RETURN DOES NOT PROVE THE WRITE DID NOT COMMIT: a timeout
		// can fire after the backend applied it, and this path once cleared
		// the staging marker AND revoked the replacement on the strength of
		// that error — in the committed world that left a revoked,
		// unverified credential in force with a record claiming nothing was
		// in flight. Recovery state is cleared only on an OBSERVED store
		// state, never on an error return: the marker stands, nothing is
		// revoked, rotation refuses until RecoverOrphan reads the store and
		// resolves which world this is — replacement present means the write
		// committed and is unwound (rollback, revoke, discharge);
		// absent means a mint-only orphan, revoked and discharged with no
		// rollback.
		return nil, err
	}

	// VERIFY AS THE NEW TOKEN. Verifying with the credential that just minted
	// it would prove the minting worked, not that the replacement functions.
	//
	// The byte view handed to VerifyToken is OUR copy and is wiped when the
	// call returns (VerifyToken's contract is no-retain). minted.Secret
	// itself is an immutable string the mint surface returns today; its
	// custody — like the Store write above — is the go-quicbaas#97
	// secure-buffer umbrella, not this seam.
	// The borrowed view points INTO the enclave: no heap copy exists at
	// this boundary, and the deferred Destroy registered at the mint covers
	// every exit — panicking adapters included (reviews 977/982/984).
	var identity *TokenIdentity
	verifyErr := minted.Secret.WithBytes(func(secret []byte) error {
		var err error
		identity, err = r.lifecycle.VerifyToken(ctx, secret)
		return err
	})
	if verifyErr == nil && identity == nil {
		// (nil, nil) is an adapter answering nothing while claiming success.
		// Dereferencing it crashed the rotation worker AFTER the replacement
		// was persisted — a partial rotation with no error to recover from.
		// It is the same class as a failed verification and takes the same
		// rollback path.
		verifyErr = &TokenVerificationError{
			Name:  name,
			Cause: &IncompleteResponseError{Op: "VerifyToken", Missing: "identity"},
		}
	}
	if verifyErr == nil && identity.Username != spec.Username {
		verifyErr = &TokenVerificationError{
			Name:     name,
			Expected: spec.Username,
			Actual:   identity.Username,
		}
	}
	if verifyErr != nil {
		if !errors.As(verifyErr, new(*TokenVerificationError)) {
			verifyErr = &TokenVerificationError{Name: name, Cause: verifyErr}
		}
		// THE OLD TOKEN SURVIVES. An unverified replacement plus a destroyed
		// original is total access loss, so the stored version is rolled back
		// and the useless new credential is the thing that gets revoked.
		//
		// THE ROLLBACK IS CHECKED BEFORE THE REVOKE, and a failed rollback
		// STOPS the revoke: the store still points at the replacement, and
		// revoking it then would destroy the only credential the store
		// holds. The staged intent keeps the pending rollback on record for
		// RecoverOrphan; both failures return together because an operator
		// needs both.
		if previousVersion > 0 {
			if rollbackErr := r.store.Rollback(ctx, spec.SecretPath, previousVersion); rollbackErr != nil {
				return nil, &TokenRotationRecoveryError{
					Name:         name,
					Verification: verifyErr,
					Rollback:     rollbackErr,
				}
			}
		} else {
			// A FAILED FIRST ROTATION RESOLVES TO ABSENCE. There is no
			// predecessor to restore, and leaving the failed replacement
			// stored as current presented a revoked credential as live
			// rotation lineage: the NEXT rotation adopted that dead version
			// as its rollback target, and a second verification failure then
			// "restored" a token the forge had already destroyed. Absence is
			// the state a first boot starts from, so it is representable and
			// truthful. Destroyed BEFORE the marker comes off: a failed
			// destroy keeps the staged record standing, and RecoverOrphan's
			// observation arm retries this same resolution.
			if destroyErr := r.store.Destroy(ctx, spec.SecretPath); destroyErr != nil {
				return nil, errors.Join(verifyErr, backendError("Destroy", destroyErr))
			}
		}
		// The rollback or removal landed, so the staging marker comes off;
		// the intent still names the replacement so recovery can retry the
		// revoke if the quiet attempt below is lost. A failed clear returns
		// BOTH errors and leaves the marker standing — fail closed: rotation
		// refuses until a recovery pass retries the (idempotent) resolution
		// and clears it.
		if clearErr := saveRotationIntent(ctx, r.store, spec.SecretPath,
			rotationIntent{Name: name, Slot: slot, Username: spec.Username}); clearErr != nil {
			return nil, errors.Join(verifyErr, &RotationIncompleteError{Name: name, Cause: clearErr})
		}
		r.revokeQuietly(ctx, spec.Username, name)
		return nil, verifyErr
	}

	// THE MARKER COMES OFF BEFORE THE OLD TOKEN DIES — BUT THE OLD TOKEN'S
	// NAME DOES NOT. With the replacement verified, the one wrong order is
	// revoke-then-clear: a crash between the two leaves a standing marker
	// over a revoked previous credential, and the recovery pass would roll
	// back to a token that no longer works — lockout through the safety
	// path. Clearing first is right; clearing EVERYTHING was the defect:
	// from here until the revoke lands this record is the old credential's
	// only remaining identity (the store already names the replacement), so
	// the verified phase keeps it as revoke-pending. A crash or a failed
	// revoke leaves a record recovery finishes; erasing the name here left
	// a live privileged token nothing named, reached from the SUCCESS path.
	revoked := currentMeta[metadataTokenName]
	if revoked == minted.Name {
		revoked = ""
	}
	if err := saveRotationIntent(ctx, r.store, spec.SecretPath,
		rotationIntent{Name: name, Slot: slot, OldToken: revoked, Username: spec.Username}); err != nil {
		return nil, &RotationIncompleteError{Name: name, Cause: err}
	}

	// Only now is the old credential safe to destroy. A token the forge no
	// longer knows is the DESIRED end state, not a failure: refusing to
	// finish over an already-dead credential wedged every later pass on a
	// discharge that never came. A failed revoke returns with the old token
	// still named revoke-pending — the next recovery pass retries it.
	if revoked != "" {
		if err := r.revokeTolerant(ctx, spec.Username, revoked); err != nil {
			return nil, err
		}
	}

	// The intent is discharged — the one write allowed to erase the old
	// token's name, because the revoke has landed. Left behind it would make
	// the next recovery pass hunt a token that was rotated in cleanly; the
	// write is best-effort because a lost discharge costs exactly that hunt:
	// the recovery arm re-revokes (a 404, tolerated) and discharges.
	_ = dischargeRotationIntent(ctx, r.store, spec.SecretPath)

	return &RotationResult{NewTokenName: minted.Name, RevokedTokenName: revoked, Slot: slot}, nil
}

// RecoverOrphan finishes what an interrupted or failed rotation left behind,
// using the intent record.
//
// Safe to run whenever: an intent naming the credential currently in force is
// a completed rotation whose discharge was lost, and is discharged. A STAGED
// intent is the stuck state — the store may hold an unverified replacement —
// and is unwound completely: rollback first, then the replacement revoked,
// then the record discharged. A stuck FIRST rotation has no predecessor to
// restore and resolves by observation: the committed replacement is verified
// and adopted, or revoked and removed so the store answers with the absence a
// first boot starts from.
func (r *TokenRotator) RecoverOrphan(ctx context.Context, spec TokenRotationSpec) error {
	lock := lockPath(spec.SecretPath)
	lock.Lock()
	defer lock.Unlock()

	intent, absent, err := loadRotationIntent(ctx, r.store, spec.SecretPath)
	if err != nil {
		return err
	}
	if absent || intent.Name == "" {
		return nil // no intent recorded, or discharged; nothing was in flight
	}

	// A STAGED INTENT IS UNWOUND BEFORE ANYTHING ELSE. In this state the
	// store may point at the unverified replacement, so the name==stored
	// guard below would read the stuck rotation as a completed one and walk
	// away. The marker is the proof it did NOT complete. The rollback is
	// retried first (idempotent — rolling back to the same version again
	// restores the same credential); the replacement is revoked only after
	// the previous credential is back in force; the record is discharged
	// last. Every failure returns with the marker intact for the pass after
	// this one.
	if intent.Staged {
		// THE AMBIGUITY IS RESOLVED BY OBSERVATION, never by an error
		// return: a staged intent may cover a replacement whose store write
		// committed (in force, unverified) or one whose write never landed
		// (a mint-only orphan). Only the store knows which, so recovery
		// reads it — fail closed on an unreadable answer.
		stagedMeta, stagedAbsent, err := readStoredToken(ctx, r.store, spec.SecretPath)
		if err != nil {
			return err
		}
		if stagedMeta[metadataTokenName] == intent.Name {
			// The write COMMITTED: the store holds the unverified
			// replacement, and the previous credential is restored before
			// anything is revoked. PendingRollback == 0 is a stuck FIRST
			// rotation with nothing to restore, and it resolves by
			// observation rather than by assumption — see the function.
			if intent.PendingRollback == 0 {
				return r.resolveCommittedFirstRotation(ctx, spec, intent)
			}
			if rollbackErr := r.store.Rollback(ctx, spec.SecretPath, intent.PendingRollback); rollbackErr != nil {
				return backendError("Rollback", rollbackErr)
			}
		}
		// Committed and rolled back, or never committed at all: either way
		// the minted token is not the credential in force, and dies before
		// the record does.
		if err := r.revokeTolerant(ctx, intentOwner(intent, spec), intent.Name); err != nil {
			return err
		}
		// AN ABSENT READ HAS TWO INDISTINGUISHABLE HISTORIES: a mint-only
		// orphan whose store write never landed, and a committed first
		// rotation whose destroy crashed after the pointer delete with
		// version material still on disk (the backend's pointer-first
		// destroy makes the path read absent from the first delete). The
		// ACTION converges, so no distinguishing is needed: sweep the path
		// before the record dies — the resumable Destroy finishes a partial
		// sweep's leftovers, answers NotFound only when nothing at all
		// remains, and discharging without it would strand destroyed-
		// credential material indefinitely against the permanent-destruction
		// contract. Gated on the ABSENT observation: a rolled-back rotation
		// reaches this point with the restored credential present, and
		// sweeping that would destroy the only credential the store holds.
		if stagedAbsent {
			if err := r.store.Destroy(ctx, spec.SecretPath); err != nil && !errors.Is(err, secrets.ErrSecretNotFound) {
				return backendError("Destroy", err)
			}
		}
		return dischargeRotationIntent(ctx, r.store, spec.SecretPath)
	}

	// A PENDING REVOKE IS THE VERIFIED PHASE: the rotation completed except
	// for destroying the old credential, and this record is that
	// credential's only remaining identity — the name==stored guard below
	// would read the phase as "completed, record stale" and walk away with
	// the old token still live. The revoke is retried (404 = already done),
	// and only a landed revoke discharges the record.
	//
	// BUT THE REVOKE IS A DESTRUCTIVE ACTION, and a destructive action needs
	// a fresh observation of the state it assumes. The record says "the
	// replacement is in force and OldToken is the predecessor" — if the
	// store no longer agrees (an operator rolled the secret back to the old
	// version, another recovery re-shaped the world), OldToken may BE the
	// credential in force, and revoking it is lockout with the recovery
	// record discharged behind it. Proceed only when the store still names
	// the replacement as current; anything else fails closed, keeps the
	// record, and re-derives on the pass after the world is understood.
	if intent.OldToken != "" {
		currentMeta, currentAbsent, err := readStoredToken(ctx, r.store, spec.SecretPath)
		if err != nil {
			return err
		}
		if currentAbsent || currentMeta[metadataTokenName] != intent.Name {
			return &RotationStateDivergedError{
				SecretPath: spec.SecretPath,
				Expected:   intent.Name,
				Stored:     currentMeta[metadataTokenName],
			}
		}
		if err := r.revokeTolerant(ctx, intentOwner(intent, spec), intent.OldToken); err != nil {
			return err
		}
		return dischargeRotationIntent(ctx, r.store, spec.SecretPath)
	}

	// FAIL CLOSED. Revoking requires PROVING the intent names an orphan rather
	// than the credential in force, and an unreadable record proves nothing.
	// Leaving an orphan alive costs a stale token until the next interval;
	// revoking the live credential costs all access, with no way back.
	currentMeta, _, err := readStoredToken(ctx, r.store, spec.SecretPath)
	if err != nil {
		return err
	}

	// The intent names the token IN FORCE, so the rotation completed and the
	// record is merely stale — a success path whose best-effort discharge was
	// lost. It is DISCHARGED here rather than left standing: Rotate refuses
	// over any unresolved intent, so a stale record left alone would refuse
	// every future rotation on the strength of a rotation that finished.
	if currentMeta[metadataTokenName] == intent.Name {
		return dischargeRotationIntent(ctx, r.store, spec.SecretPath)
	}

	if err := r.revokeTolerant(ctx, intentOwner(intent, spec), intent.Name); err != nil {
		return err
	}

	return dischargeRotationIntent(ctx, r.store, spec.SecretPath)
}

// resolveCommittedFirstRotation resolves a staged FIRST rotation whose store
// write committed: the path's only credential is an unverified replacement,
// and there is no predecessor to roll back to.
//
// The old behaviour — revoke the replacement, discharge, return success —
// left the store presenting the now-revoked token as current, and the NEXT
// rotation adopted that dead version as its rollback target: a second
// verification failure then "restored" a credential the forge had already
// destroyed and reported the recovery as if a usable predecessor survived.
// Success was being reported over an incoherent end state.
//
// Both arms here end coherent, and which arm runs is decided by OBSERVATION:
//
//   - The replacement was merely UNVERIFIED, not proven bad — a crash between
//     the store write and the verification is the ordinary way here. So it is
//     verified now, exactly as Rotate would have, and a replacement that
//     authenticates as the right account is ADOPTED: the intent is
//     discharged and the store keeps a working credential. Forge and store
//     agree.
//   - A replacement that fails verification is revoked AND the stored path is
//     destroyed. Absence is representable — it is precisely the state a first
//     boot starts from — so the next rotation runs as an ordinary first
//     rotation instead of reading a revoked credential as rotation lineage.
//     Forge and store agree again: neither holds the failed token.
//
// The verification-failure policy matches Rotate's: any failure to verify —
// including a transport fault — costs the unverified replacement, never a
// predecessor (there is none).
//
// THE DESTROY COMES BEFORE THE REVOKE, and the order is load-bearing: the
// real backend's Destroy is not atomic (versions are deleted one by one
// before the current pointer), so a partial destroy can leave the path
// present but UNREADABLE. If the forge token had already been revoked, the
// next pass would fail at the observation read with nothing left to present
// — permanently incoherent, with the only credential destroyed. Destroying
// first keeps every failure recoverable: a partial destroy fails closed at
// the next pass's read with the token still live and still named by the
// staged intent (an operator-visible stall, never a silent loss); a
// completed destroy followed by a failed revoke leaves an absent path the
// staged arm resolves as never-committed (revoke retried tolerantly,
// discharge after); a failed discharge converges the same way.
func (r *TokenRotator) resolveCommittedFirstRotation(
	ctx context.Context,
	spec TokenRotationSpec,
	intent rotationIntent,
) error {
	// THE TOKEN FIELD MUST EXIST AND BE NONEMPTY before anything is decided
	// on it. A malformed or partially written record whose METADATA matches
	// the intent would otherwise send VerifyToken with nothing, and an
	// adapter that answered the expected identity for an empty credential
	// would get the record ADOPTED as current — an unusable credential with
	// its recovery intent discharged. Fail closed, keep the intent; an
	// operator (or a repaired store) resolves what a guess must not.
	//
	// THE CREDENTIAL IS TOUCHED ONLY THROUGH THE STORE'S GUARDED USE
	// (review 959): the bytes are borrowed inside UseSecret's callback for
	// exactly the verification call, never materialized as a string, never
	// retained in a map, and the store wipes every fetched byte when the
	// callback returns. Only the verification RESULT leaves the window.
	// UseSecret enforces the nonempty-field guard itself, reporting
	// secrets.ErrSecretKeyAbsent, which maps to the same fail-closed
	// refusal the inline length check used to produce.
	var identity *TokenIdentity
	var verifyErr error
	if useErr := r.store.UseSecret(ctx, spec.SecretPath, tokenSecretKey, func(token []byte) error {
		identity, verifyErr = r.lifecycle.VerifyToken(ctx, token)
		return nil
	}); useErr != nil {
		if errors.Is(useErr, secrets.ErrSecretKeyAbsent) {
			return &RotationCredentialMissingError{SecretPath: spec.SecretPath, Name: intent.Name}
		}
		return useErr
	}
	if verifyErr == nil && identity == nil {
		// The (nil, nil) adapter again — same class as a failed verification,
		// same as Rotate treats it.
		verifyErr = &TokenVerificationError{
			Name:  intent.Name,
			Cause: &IncompleteResponseError{Op: "VerifyToken", Missing: "identity"},
		}
	}
	// THE RECORDED OWNER IS AUTHORITATIVE here too (review 979): this arm
	// decides whether to ADOPT or DESTROY the stored credential, and judging
	// an alice-owned staged rotation against a later caller's account would
	// classify a valid token as wrong and destroy its only stored copy.
	owner := intentOwner(intent, spec)
	if verifyErr == nil && identity.Username != owner {
		verifyErr = &TokenVerificationError{
			Name:     intent.Name,
			Expected: owner,
			Actual:   identity.Username,
		}
	}
	if verifyErr == nil {
		// ADOPTED. The rotation had in fact succeeded everywhere but its own
		// bookkeeping; the discharge is that bookkeeping.
		return dischargeRotationIntent(ctx, r.store, spec.SecretPath)
	}

	if err := r.store.Destroy(ctx, spec.SecretPath); err != nil {
		return backendError("Destroy", err)
	}
	if err := r.revokeTolerant(ctx, intentOwner(intent, spec), intent.Name); err != nil {
		return err
	}
	return dischargeRotationIntent(ctx, r.store, spec.SecretPath)
}

// revokeQuietly destroys a credential whose failure path is already being
// reported.
//
// The error is dropped deliberately: the caller is returning the reason
// rotation failed, and replacing it with a cleanup failure would hide the
// cause. The intent record is what stops the orphan being lost.
func (r *TokenRotator) revokeQuietly(ctx context.Context, username, name string) {
	_ = r.lifecycle.RevokeToken(ctx, username, name)
}

// revokeTolerant destroys a credential, treating "already gone" as done.
//
// The forge answering NotFound means the token does not exist, which is the
// state revocation exists to reach. Treating it as failure wedged recovery
// permanently on the ordinary verify-fail path: the quiet inline revoke had
// already destroyed the replacement, the next recovery pass revoked it AGAIN,
// got a 404, and returned it — so the intent was never discharged and every
// pass after repeated the loop.
func (r *TokenRotator) revokeTolerant(ctx context.Context, username, name string) error {
	if err := r.lifecycle.RevokeToken(ctx, username, name); err != nil && !errors.Is(err, ErrNotFound) {
		return err
	}
	return nil
}

// rotationIntent is the durable record of one rotation attempt, stored as the
// credential's sibling (see rotationIntentSuffix).
//
// Two states matter. UNSTAGED (Staged false) says "this token may exist on
// the forge and is not the stored credential": recovery revokes it by name.
// STAGED says "the replacement may be IN the store, unverified, and a
// rollback to PendingRollback is owed" — the state every guard in this file
// exists to unwind, and the one that blocks new rotations until recovered.
type rotationIntent struct {
	// Name is the forge-side name of the token the attempt minted or was
	// about to mint.
	Name string

	// Slot is the rotation slot the attempt occupies.
	Slot uint64

	// Staged marks that the replacement may already be stored, unverified.
	Staged bool

	// PendingRollback is the store version a staged attempt must roll back
	// to; 0 means a first rotation with nothing to restore. Meaningful only
	// when Staged.
	PendingRollback int

	// OldToken names the previous credential a VERIFIED rotation still owes
	// a revoke for. While set, this record is the old token's only remaining
	// identity — the store already names the replacement — so it is erased
	// only by the discharge that follows the revoke.
	OldToken string

	// Username is the forge account that OWNS the tokens this record names
	// (review 977): recovery revokes as the recorded owner, never as
	// whatever account a later caller's spec happens to supply. Without it,
	// a recovery run under a reconfigured account turned the forge's
	// account-scoped NotFound into "already revoked" and discharged the
	// only record naming a still-live credential under the original owner.
	Username string
}

// intentOwner returns the account recovery acts as: the recorded owner when
// the record carries one (review 977), else the caller's spec — the only
// account available for records predating the field (pre-release: no
// migration owed, release-versioning.md §1).
func intentOwner(intent rotationIntent, spec TokenRotationSpec) string {
	if intent.Username != "" {
		return intent.Username
	}
	return spec.Username
}

// loadRotationIntent reads the intent record, separating ABSENT from
// UNREADABLE the way readStoredToken does, and failing closed on a staging
// marker it cannot parse: guessing a rollback target risks the in-force
// credential.
func loadRotationIntent(ctx context.Context, store TokenSecretStore, secretPath string) (
	rotationIntent, bool, error) {
	data, metadata, err := store.Get(ctx, secretPath+rotationIntentSuffix)
	switch {
	case err == nil:
		// fall through to parsing
	case errors.Is(err, secrets.ErrSecretNotFound):
		return rotationIntent{}, true, nil
	default:
		return rotationIntent{}, false, err
	}

	intent := rotationIntent{Name: string(data[metadataTokenName])}
	if slotText := metadata[metadataRotationSlot]; slotText != "" {
		// The slot is informational on read; an unparseable one does not
		// block recovery, which never needs it.
		intent.Slot, _ = strconv.ParseUint(slotText, 10, 64)
	}
	intent.OldToken = metadata[metadataRevokePending]
	intent.Username = metadata[metadataIntentOwner]
	if pending, staged := metadata[metadataPendingRollback]; staged {
		version, parseErr := strconv.Atoi(pending)
		if parseErr != nil || version < 0 {
			return rotationIntent{}, false, &RotationRecoveryStateError{SecretPath: secretPath, Value: pending}
		}
		intent.Staged = true
		intent.PendingRollback = version
	}
	return intent, false, nil
}

// saveRotationIntent writes the intent record; the staging marker is present
// exactly when the intent is staged.
func saveRotationIntent(ctx context.Context, store TokenSecretStore, secretPath string, intent rotationIntent) error {
	metadata := map[string]string{
		metadataRotationSlot: strconv.FormatUint(intent.Slot, 10),
	}
	if intent.Username != "" {
		metadata[metadataIntentOwner] = intent.Username
	}
	if intent.Staged {
		metadata[metadataPendingRollback] = strconv.Itoa(intent.PendingRollback)
	}
	if intent.OldToken != "" {
		metadata[metadataRevokePending] = intent.OldToken
	}
	return store.Store(ctx, secretPath+rotationIntentSuffix,
		map[string][]byte{metadataTokenName: []byte(intent.Name)}, metadata)
}

// dischargeRotationIntent empties the intent record: nothing in flight.
func dischargeRotationIntent(ctx context.Context, store TokenSecretStore, secretPath string) error {
	return store.Store(ctx, secretPath+rotationIntentSuffix,
		map[string][]byte{metadataTokenName: nil}, nil)
}

// nextRotationSlot reads the stored slot and returns the following one.
func nextRotationSlot(metadata map[string]string) uint64 {
	if metadata == nil {
		return 1
	}
	current, err := strconv.ParseUint(metadata[metadataRotationSlot], 10, 64)
	if err != nil {
		return 1
	}
	return current + 1
}

// latestVersion returns the highest live version at the path, or the listing
// failure ITSELF.
//
// The failure is never collapsed to zero: zero means "nothing to roll back
// to", and answering it for a store that merely could not be asked converts a
// read outage into a skipped rollback and a revoked in-force credential. The
// ONE exception is ErrSecretNotFound: the real secrets.Manager reports an
// absent path as that error rather than an empty list, and an absent path is
// the ordinary first rotation — a real zero, not an outage. Failing closed on
// it minted a first token, then revoked it without ever storing it: overreach
// is as much a bug as fail-open, and it is mapped back to (0, nil) here, at
// the one place that knows which error means absence.
func latestVersion(ctx context.Context, store TokenSecretStore, path string) (int, error) {
	versions, err := store.ListVersions(ctx, path)
	if err != nil {
		if errors.Is(err, secrets.ErrSecretNotFound) {
			return 0, nil
		}
		return 0, backendError("ListVersions", err)
	}
	highest := 0
	for _, v := range versions {
		if v != nil && !v.Deleted && v.Version > highest {
			highest = v.Version
		}
	}
	return highest, nil
}

// TokenRotationRecoveryError reports a rotation whose verification failed AND
// whose rollback then failed: the store still points at the unverified
// replacement, which was therefore NOT revoked, and the intent record still
// names it for the next recovery pass.
type TokenRotationRecoveryError struct {
	// Name is the replacement token.
	Name string

	// Verification is why the rotation failed.
	Verification error

	// Rollback is why the recovery could not restore the previous credential.
	Rollback error
}

// Error implements the error interface.
func (e *TokenRotationRecoveryError) Error() string {
	return "sdlc: rotation of token " + e.Name + " failed verification (" +
		e.Verification.Error() + ") and the rollback to the previous credential also failed (" +
		e.Rollback.Error() + "); the store still holds the unverified replacement, staged for recovery, and was not revoked"
}

// Unwrap exposes both failures to errors.Is and errors.As.
func (e *TokenRotationRecoveryError) Unwrap() []error {
	return []error{e.Verification, e.Rollback}
}

// RotationIncompleteError reports a rotation that could not update its own
// recovery record. The rotation did not finish — where it stopped is in
// Cause — and the standing record keeps further rotations refused until a
// recovery pass unwinds it, which is the fail-closed half of the design.
type RotationIncompleteError struct {
	// Name is the replacement token the attempt was rotating in.
	Name string

	// Cause is the record write that failed.
	Cause error
}

// Error implements the error interface.
func (e *RotationIncompleteError) Error() string {
	return "sdlc: rotation of token " + e.Name +
		" could not update its recovery record (" + e.Cause.Error() +
		"); rotation stays refused until RecoverOrphan unwinds the standing record"
}

// Unwrap exposes the cause to errors.Is and errors.As.
func (e *RotationIncompleteError) Unwrap() error { return e.Cause }

// RotationRecoveryPendingError refuses a rotation while a failed rotation's
// rollback is still owed: the store holds an unverified replacement, and only
// the intent record knows it. RecoverOrphan clears the state.
//
// TRANSIENT, NOT TERMINAL. A rotation schedule that receives it must run
// RecoverOrphan and retry the rotation on its next interval — treating it as
// a permanent failure would leave the pipeline dead exactly when recovery is
// one call away. It carries everything an operator needs to see what is
// stuck and why.
type RotationRecoveryPendingError struct {
	// SecretPath is the credential path with the pending rollback.
	SecretPath string

	// Name is the unverified replacement the intent record names.
	Name string

	// Version is the store version the rollback is owed to.
	Version string
}

// Error implements the error interface.
func (e *RotationRecoveryPendingError) Error() string {
	return "sdlc: rotation of " + e.SecretPath + " refused: a previous rotation's rollback to version " +
		e.Version + " is still pending and the store holds the unverified replacement " + e.Name +
		"; run RecoverOrphan first"
}

// RotationRevokePendingError refuses a rotation while a verified rotation's
// revoke of the previous credential is still owed. The intent record is that
// credential's only remaining identity; rotating over it erases the name and
// leaks a live token.
//
// TRANSIENT, NOT TERMINAL, like RotationRecoveryPendingError: run
// RecoverOrphan (which retries the revoke and discharges) and retry.
type RotationRevokePendingError struct {
	// SecretPath is the credential path with the pending revoke.
	SecretPath string

	// OldToken is the previous credential still owing its revoke.
	OldToken string
}

// Error implements the error interface.
func (e *RotationRevokePendingError) Error() string {
	return "sdlc: rotation of " + e.SecretPath + " refused: the previous rotation's revoke of " +
		e.OldToken + " is still pending and this record is that credential's only remaining identity; run RecoverOrphan first"
}

// RotationUnresolvedIntentError refuses a rotation while an unstaged intent
// still names a token. That record is the only durable identity of a
// credential the mint window may have created; rotating over it would erase
// the name a revoke needs, leaking a possibly live privileged token.
//
// TRANSIENT, NOT TERMINAL, like its two siblings above: RecoverOrphan's
// original arm resolves exactly this shape (revoke-by-name, or discharge a
// record made stale by a completed rotation), and the scheduler's handler
// runs it before every rotation.
type RotationUnresolvedIntentError struct {
	// SecretPath is the credential path with the unresolved intent.
	SecretPath string

	// Name is the token the intent record names.
	Name string
}

// Error implements the error interface.
func (e *RotationUnresolvedIntentError) Error() string {
	return "sdlc: rotation of " + e.SecretPath + " refused: an unresolved intent still names token " +
		e.Name + " and is that token's only durable identity; run RecoverOrphan first"
}

// RotationStateDivergedError reports a recovery record whose assumptions the
// store no longer supports: the pending-revoke intent says the replacement is
// the credential in force, and the store names something else (or nothing).
// Revoking on the record's word alone could destroy the credential actually
// in force, so recovery fails closed, KEEPS the record, and asks again next
// pass — the divergence usually means an operator intervened, and the
// operator is who this error is for.
type RotationStateDivergedError struct {
	// SecretPath is the credential path whose record and store disagree.
	SecretPath string

	// Expected is the token the intent record says is in force.
	Expected string

	// Stored is the token the store actually names; empty when the path is
	// absent or carries no name.
	Stored string
}

// Error implements the error interface.
func (e *RotationStateDivergedError) Error() string {
	stored := e.Stored
	if stored == "" {
		stored = "(none)"
	}
	return "sdlc: recovery of " + e.SecretPath + " refused: the intent record expects " +
		e.Expected + " to be the stored credential but the store names " + stored +
		"; the world changed under the record, so nothing is revoked and the record is kept"
}

// MintResponseError reports a forge mint answer rotation refuses to build on:
// nil, an empty secret, an empty name, or a name differing from the requested
// one. The requested name is deterministic by contract (RotationTokenName) so
// every party can recompute it; a forge that renames breaks that contract,
// and absorbing the rename silently would change the recovery identity model
// — a contract change to make deliberately, never by accepting divergent
// output.
type MintResponseError struct {
	// Requested is the deterministic name the rotation asked for.
	Requested string

	// Returned is the name the forge answered, when it answered one.
	Returned string

	// Refusal classifies what made the response unusable.
	//
	// SANITIZED FACTS ONLY (review 974): this error once carried the whole
	// *MintedToken, secret included — Error() never printed it, but any
	// errors.As caller, log enricher or telemetry hook inspecting the
	// structured error could reach bearer material through it. An error
	// is a reporting surface, never a custody surface.
	Refusal MintRefusal
}

// MintRefusal classifies why a mint response was refused.
type MintRefusal int

const (
	// MintRefusalNilResponse is the (nil, nil) adapter answer.
	MintRefusalNilResponse MintRefusal = iota

	// MintRefusalEmptySecret is a response carrying no credential.
	MintRefusalEmptySecret

	// MintRefusalEmptyName is a usable secret under no name — an identity
	// nothing can later revoke.
	MintRefusalEmptyName

	// MintRefusalDivergentName is a response named other than requested —
	// the deterministic-naming contract violation (ruled 2026-08-23).
	MintRefusalDivergentName
)

// newMintResponseError classifies a refused mint response into the
// sanitized error, never retaining the response object itself.
func newMintResponseError(requested string, minted *MintedToken) *MintResponseError {
	e := &MintResponseError{Requested: requested}
	switch {
	case minted == nil:
		e.Refusal = MintRefusalNilResponse
	case minted.Secret == nil, minted.Secret.Size() == 0:
		e.Refusal = MintRefusalEmptySecret
		e.Returned = minted.Name
	case minted.Name == "":
		e.Refusal = MintRefusalEmptyName
	default:
		e.Refusal = MintRefusalDivergentName
		e.Returned = minted.Name
	}
	return e
}

// Error implements the error interface.
func (e *MintResponseError) Error() string {
	switch e.Refusal {
	case MintRefusalNilResponse:
		return "sdlc: mint of token " + e.Requested + " answered nil with no error; nothing usable was returned"
	case MintRefusalEmptySecret:
		return "sdlc: mint of token " + e.Requested + " returned no secret; a credential that cannot authenticate cannot be rotated in"
	case MintRefusalEmptyName:
		return "sdlc: mint of token " + e.Requested + " returned an empty name; an identity nothing can later revoke by name cannot be recorded"
	default:
		return "sdlc: mint of token " + e.Requested + " returned name " + e.Returned +
			"; the deterministic naming contract is broken and the divergent token was revoked rather than recorded"
	}
}

// RotationCredentialMissingError reports a stored credential record whose
// metadata matches a staged intent but which carries NO token value. Recovery
// fails closed on it: verifying an empty string proves nothing, and an
// adapter that answered success for it would get an unusable record adopted
// as the current credential with its recovery intent discharged.
type RotationCredentialMissingError struct {
	// SecretPath is the credential path with the malformed record.
	SecretPath string

	// Name is the token the staged intent names.
	Name string
}

// Error implements the error interface.
func (e *RotationCredentialMissingError) Error() string {
	return "sdlc: recovery of " + e.SecretPath + " refused: the stored record matches staged intent " +
		e.Name + " but carries no token value; nothing can be verified and nothing is adopted or revoked on a guess"
}

// RotationRecoveryStateError reports a pending-rollback record whose version
// cannot be parsed. Recovery fails closed on it: guessing a rollback target
// risks the only credential the store holds.
type RotationRecoveryStateError struct {
	// SecretPath is the credential path whose recovery record is unreadable.
	SecretPath string

	// Value is the unparseable pending-rollback version.
	Value string
}

// Error implements the error interface.
func (e *RotationRecoveryStateError) Error() string {
	return "sdlc: recovery of " + e.SecretPath + " refused: pending rollback version " +
		strconv.Quote(e.Value) + " is not a positive integer, and guessing a rollback target risks the in-force credential"
}

// withIdentityScope guarantees the replacement can verify itself.
func withIdentityScope(scopes []string) []string {
	for _, s := range scopes {
		if s == identityScope {
			return scopes
		}
	}
	return append(append([]string{}, scopes...), identityScope)
}
