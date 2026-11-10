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
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// =============================================================================
// Credential generations — which COPY of an account's material is this?
//
// The generation is the answer to the only question the freshness check
// actually asks, and these tests hold the three properties that make the
// answer trustworthy: it is minted by the write that stores the material, it
// travels with the material to whatever materialized it, and it is never
// inferred from a clock (go-quicbaas#997).
// =============================================================================

// TestAccountSecretStore_EveryReplacementIsANewGeneration: the stamp is minted
// by the write, is strictly increasing, and is never applied to the caller's
// own envelope — a caller must not silently acquire a generation it did not
// publish.
func TestAccountSecretStore_EveryReplacementIsANewGeneration(t *testing.T) {
	store := newTestAccountSecretStore(t)
	ctx := context.Background()

	cred := &AccountCredential{AccessToken: "token-a"}
	first, err := store.Replace(ctx, "tenant-a", "acct-1", cred)
	if err != nil {
		t.Fatalf("Replace: %v", err)
	}
	if first <= 0 {
		t.Fatalf("Replace stamped generation %d, want a positive stamp", first)
	}
	if cred.Generation != 0 {
		t.Errorf("Replace mutated the caller's envelope to generation %d; it must stamp only the stored copy", cred.Generation)
	}

	stored, err := store.Get(ctx, "tenant-a", "acct-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if stored.Generation != first {
		t.Errorf("stored generation = %d, want the stamp the write reported (%d)", stored.Generation, first)
	}

	second, err := store.Replace(ctx, "tenant-a", "acct-1", &AccountCredential{AccessToken: "token-b"})
	if err != nil {
		t.Fatalf("Replace(second): %v", err)
	}
	if second <= first {
		t.Errorf("the second replacement stamped %d, want strictly greater than %d", second, first)
	}
}

// TestAccountSecretStore_GenerationsAreStrictlyIncreasingUnderConcurrency:
// two replacements landing inside one nanosecond must still be told apart. A
// repeated stamp would make the pool describe new material with a generation
// every live session already holds, and the session would never be rolled.
func TestAccountSecretStore_GenerationsAreStrictlyIncreasingUnderConcurrency(t *testing.T) {
	store := newTestAccountSecretStore(t)
	ctx := context.Background()

	const writers = 32
	stamps := make([]int64, writers)
	var wg sync.WaitGroup
	wg.Add(writers)
	for i := range writers {
		go func(i int) {
			defer wg.Done()
			generation, err := store.Replace(ctx, "tenant-a", "acct-1", &AccountCredential{AccessToken: "token"})
			if err != nil {
				t.Errorf("Replace: %v", err)
				return
			}
			stamps[i] = generation
		}(i)
	}
	wg.Wait()

	seen := make(map[int64]struct{}, writers)
	for _, stamp := range stamps {
		if stamp == 0 {
			t.Fatal("a concurrent Replace reported no generation")
		}
		if _, duplicate := seen[stamp]; duplicate {
			t.Fatalf("generation %d was minted twice; stamps must be unique per write", stamp)
		}
		seen[stamp] = struct{}{}
	}
}

// TestAccountStore_TheGenerationMirrorOnlyEverAdvances: a reconciler holding an
// envelope a newer rotation has already superseded must not walk the mirror
// back. If it could, every session holding the NEWEST material would read as
// stale and be rolled for nothing.
func TestAccountStore_TheGenerationMirrorOnlyEverAdvances(t *testing.T) {
	store := NewInMemoryAccountStore()
	ctx := context.Background()

	if err := store.Create(ctx, newTestAccount("tenant-a", "acct-1", AccountProviderClaude)); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := store.Touch(ctx, "tenant-a", "acct-1", TouchAccountOptions{SetCredentialGeneration: 200}); err != nil {
		t.Fatalf("Touch(200): %v", err)
	}
	if err := store.Touch(ctx, "tenant-a", "acct-1", TouchAccountOptions{SetCredentialGeneration: 100}); err != nil {
		t.Fatalf("Touch(100): %v", err)
	}

	got, err := store.Get(ctx, "tenant-a", "acct-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.CredentialGeneration != 200 {
		t.Errorf("CredentialGeneration = %d, want 200 — a late reconciler walked the mirror backwards", got.CredentialGeneration)
	}

	if err := store.Touch(ctx, "tenant-a", "acct-1", TouchAccountOptions{SetCredentialGeneration: 300}); err != nil {
		t.Fatalf("Touch(300): %v", err)
	}
	got, err = store.Get(ctx, "tenant-a", "acct-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.CredentialGeneration != 300 {
		t.Errorf("CredentialGeneration = %d, want 300 — a genuine rotation must advance the mirror", got.CredentialGeneration)
	}
}

// TestAccountResolver_ReportsAndReconcilesTheGenerationItMaterialized: the
// resolver hands the caller the generation it read out of the envelope, AND
// repairs the account's mirror from it. The repair is what makes a lost
// rotation marker cost a window rather than the whole detection.
func TestAccountResolver_ReportsAndReconcilesTheGenerationItMaterialized(t *testing.T) {
	store := NewInMemoryAccountStore()
	secretStore := newTestAccountSecretStore(t)
	ctx := context.Background()

	account := newTestAccount("tenant-a", "acct-1", AccountProviderClaude)
	account.RefreshPolicy = AccountCredentialModeStaticToken
	if err := store.Create(ctx, account); err != nil {
		t.Fatalf("Create: %v", err)
	}
	generation, err := secretStore.Replace(ctx, "tenant-a", "acct-1",
		&AccountCredential{AccessToken: "token-a", AccessTokenEnvKey: "CLAUDE_CODE_OAUTH_TOKEN"})
	if err != nil {
		t.Fatalf("Replace: %v", err)
	}

	// The account record knows nothing about that generation yet: exactly the
	// state a rotation whose mirror write was lost leaves behind.
	before, err := store.Get(ctx, "tenant-a", "acct-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if before.CredentialGeneration != 0 {
		t.Fatalf("CredentialGeneration = %d, want the mirror to start unwritten", before.CredentialGeneration)
	}

	resolver := NewAccountResolver(store, NewAccountTokenManager(secretStore, store, 0),
		NewAccountStrategyRegistry(NewCostTracker(nil, nil)))
	resolved, err := resolver.Resolve(ctx, AccountResolveRequest{
		TenantID: "tenant-a", Provider: AccountProviderClaude, AccountID: "acct-1",
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if resolved.Generation != generation {
		t.Errorf("ResolvedAccountCredential.Generation = %d, want the envelope's own stamp %d",
			resolved.Generation, generation)
	}

	after, err := store.Get(ctx, "tenant-a", "acct-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if after.CredentialGeneration != generation {
		t.Errorf("CredentialGeneration = %d, want the resolver to have reconciled the mirror to %d",
			after.CredentialGeneration, generation)
	}
}

// newGenerationRaceBackend is newTestACPBackendForProvider with one addition:
// onHandshake runs ONCE, inside the transport factory — that is, after the
// spawn has already resolved and materialized the account's credential and
// before the session exists to be dated. It is the window go-quicbaas#997's
// reviewer named.
func newGenerationRaceBackend(t *testing.T, onHandshake func()) (
	*ACPBackend, AccountStore, *AccountSecretStore, func() []string, func(),
) {
	t.Helper()

	factory, transportCleanup := mockTransportFactory(t)

	var mu sync.Mutex
	var captured []string
	var once sync.Once
	wrapped := func(command string, args []string, workDir string, env []string) (*StdioTransport, error) {
		once.Do(onHandshake)
		mu.Lock()
		captured = append([]string(nil), env...)
		mu.Unlock()
		return factory(command, args, workDir, env)
	}

	backend := NewACPBackend(ACPBackendConfig{Command: string(AccountProviderClaude), DefaultModel: "default-model"})
	backend.transportFactory = wrapped

	store := NewInMemoryAccountStore()
	secretStore := newTestAccountSecretStore(t)
	tokens := NewAccountTokenManager(secretStore, store, 60*time.Second)
	strategies := NewAccountStrategyRegistry(NewCostTracker(nil, nil))
	backend.SetAccountResolver(NewAccountResolver(store, tokens, strategies))

	lastEnv := func() []string {
		mu.Lock()
		defer mu.Unlock()
		return captured
	}
	return backend, store, secretStore, lastEnv, transportCleanup
}

// TestSpawn_ARotationDuringTheHandshakeLeavesTheSessionSTALE is the defect the
// timestamp comparison could not see.
//
// provisionAndSpawn resolves the credential BEFORE it starts the container, and
// the session is dated AFTER the container's opening handshake. A rotation that
// lands in between leaves the agent process holding the OLD token while the
// record claims a spawn instant later than the rotation — so a check that
// compares the account's rotation timestamp against the session's creation
// timestamp reports a container full of dead credentials as perfectly fresh,
// and the next prompt pays for it.
//
// The generation makes the window irrelevant: it is read from the very envelope
// the material came out of, so the session reports the copy it actually holds.
func TestSpawn_ARotationDuringTheHandshakeLeavesTheSessionStale(t *testing.T) {
	ctx := context.Background()

	var (
		store       AccountStore
		secretStore *AccountSecretStore
		rotatedTo   int64
		rotatedAt   time.Time
	)

	// The rotation the host performs while the container is still starting.
	rotate := func() {
		generation, err := secretStore.Replace(ctx, e2eTenant, "acct-1",
			&AccountCredential{AccessToken: "token-after-rotation", AccessTokenEnvKey: "CLAUDE_CODE_OAUTH_TOKEN"})
		if err != nil {
			t.Errorf("rotate: Replace: %v", err)
			return
		}
		rotatedTo = generation
		rotatedAt = time.Now().UTC()
		if err := store.Touch(ctx, e2eTenant, "acct-1", TouchAccountOptions{
			MarkCredentialRotated:   true,
			SetCredentialGeneration: generation,
		}); err != nil {
			t.Errorf("rotate: Touch: %v", err)
		}
	}

	backend, accountStore, secrets, lastEnv, cleanup := newGenerationRaceBackend(t, func() { rotate() })
	defer cleanup()
	store, secretStore = accountStore, secrets

	account := newTestAccount(e2eTenant, "acct-1", AccountProviderClaude)
	account.RefreshPolicy = AccountCredentialModeStaticToken
	importAccount(t, store, secretStore, account,
		&AccountCredential{AccessToken: "token-before-rotation", AccessTokenEnvKey: "CLAUDE_CODE_OAUTH_TOKEN"})

	spawnCtx, cancel := context.WithTimeout(ContextWithTenant(ctx, e2eTenant), 5*time.Second)
	defer cancel()

	sess, err := backend.Spawn(spawnCtx, &SpawnRequest{WorkDir: "/tmp", AccountID: "acct-1"})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	defer backend.Close(context.Background(), sess.ID)

	// The container really is holding the pre-rotation credential.
	if got, _ := spawnEnvValue(lastEnv(), "CLAUDE_CODE_OAUTH_TOKEN"); got != "token-before-rotation" {
		t.Fatalf("subprocess token = %q, want the pre-rotation copy — the test did not reproduce the window", got)
	}
	if rotatedTo == 0 {
		t.Fatal("the handshake hook never rotated the account")
	}
	if sess.AccountGeneration == 0 {
		t.Fatal("Session.AccountGeneration is unset; the spawn recorded no credential identity at all")
	}
	if sess.AccountGeneration >= rotatedTo {
		t.Fatalf("Session.AccountGeneration = %d, want the PRE-rotation copy (below %d) — the spawn recorded material it does not hold",
			sess.AccountGeneration, rotatedTo)
	}

	// The record the daemon would write, dated the way the backend dates it.
	record := &SessionRecord{
		SessionID:         sess.ID,
		TenantID:          e2eTenant,
		CreatedAt:         sess.CreatedAt,
		AccountID:         sess.AccountID,
		AccountGeneration: sess.AccountGeneration,
	}
	current, err := store.Get(ctx, e2eTenant, "acct-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	answer := &SessionAccountFreshness{
		SessionID:         record.SessionID,
		AccountID:         record.AccountID,
		SessionGeneration: record.AccountGeneration,
		AccountGeneration: current.CredentialGeneration,
		MaterializedAt:    time.Unix(0, record.AccountMaterializedAt()),
		RotatedAt:         current.CredentialRotatedAt,
	}
	if !staleAgainstAccount(answer) {
		t.Fatalf("a session that materialized generation %d while the account moved to %d reported fresh",
			answer.SessionGeneration, answer.AccountGeneration)
	}

	// And the proof that this needed the generation at all: the timestamps the
	// previous implementation compared say the opposite, because the session
	// was DATED after the rotation even though it was MATERIALIZED before it.
	if !answer.MaterializedAt.After(rotatedAt) {
		t.Fatalf("the session is dated %s, which is not after the rotation at %s — the window under test was not reproduced",
			answer.MaterializedAt, rotatedAt)
	}
	timestampsOnly := &SessionAccountFreshness{
		MaterializedAt: answer.MaterializedAt,
		RotatedAt:      answer.RotatedAt,
	}
	if staleAgainstAccount(timestampsOnly) {
		t.Fatal("the timestamp fallback reported this session stale; the regression this test locks no longer exists in the form it was written for")
	}
}

// TestStaleAgainstAccount_GenerationsWinAndTimestampsAreTheFloor: the verdict's
// whole decision table, stated once. The timestamp rows are the floor for
// sessions spawned before generations existed; every session created since
// carries one, including those served by a remote daemon.
func TestStaleAgainstAccount_GenerationsWinAndTimestampsAreTheFloor(t *testing.T) {
	early := time.Date(2026, 9, 22, 1, 0, 0, 0, time.UTC)
	late := time.Date(2026, 9, 22, 9, 0, 0, 0, time.UTC)

	cases := []struct {
		name   string
		answer SessionAccountFreshness
		want   bool
	}{{
		name:   "both generations known and equal is fresh",
		answer: SessionAccountFreshness{SessionGeneration: 5, AccountGeneration: 5, RotatedAt: late, MaterializedAt: early},
		want:   false,
	}, {
		name:   "the account moved past the session's copy",
		answer: SessionAccountFreshness{SessionGeneration: 5, AccountGeneration: 6, RotatedAt: early, MaterializedAt: late},
		want:   true,
	}, {
		name:   "a session holding a copy newer than the mirror is fresh",
		answer: SessionAccountFreshness{SessionGeneration: 7, AccountGeneration: 6},
		want:   false,
	}, {
		name:   "no session generation falls back to the timestamps",
		answer: SessionAccountFreshness{AccountGeneration: 6, RotatedAt: late, MaterializedAt: early},
		want:   true,
	}, {
		name:   "no account generation falls back to the timestamps",
		answer: SessionAccountFreshness{SessionGeneration: 5, RotatedAt: early, MaterializedAt: late},
		want:   false,
	}, {
		name:   "no evidence at all is never stale",
		answer: SessionAccountFreshness{},
		want:   false,
	}, {
		name:   "a rotation with no materialization instant is not evidence",
		answer: SessionAccountFreshness{RotatedAt: late},
		want:   false,
	}}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := staleAgainstAccount(&tc.answer); got != tc.want {
				t.Errorf("staleAgainstAccount(%+v) = %v, want %v", tc.answer, got, tc.want)
			}
		})
	}
}

// TestSessionRecord_TheGenerationMovesWithTheAccountSegment: a rotation records
// the REPLACEMENT account's copy, and a resume records the copy the restored
// process re-materialized. Both keep the scalar and the open segment in step,
// because the freshness check reads one and cost attribution reads the other.
func TestSessionRecord_TheGenerationMovesWithTheAccountSegment(t *testing.T) {
	spawn := time.Date(2026, 9, 22, 1, 0, 0, 0, time.UTC).UnixNano()
	rotated := time.Date(2026, 9, 22, 5, 0, 0, 0, time.UTC).UnixNano()

	record := &SessionRecord{
		SessionID:         "session-1",
		CreatedAt:         spawn,
		AccountID:         "acct-a",
		AccountGeneration: 11,
	}
	record.OpenAccountSegment("acct-b", LeastUsageAccountStrategyName, "usage limit reached", rotated, 22)

	if record.AccountGeneration != 22 {
		t.Errorf("AccountGeneration = %d, want the replacement account's copy 22", record.AccountGeneration)
	}
	if len(record.AccountHistory) != 2 {
		t.Fatalf("AccountHistory has %d segments, want 2", len(record.AccountHistory))
	}
	if record.AccountHistory[0].Generation != 11 {
		t.Errorf("the backfilled segment's generation = %d, want the copy the session started on (11)",
			record.AccountHistory[0].Generation)
	}
	if record.AccountHistory[1].Generation != 22 {
		t.Errorf("the open segment's generation = %d, want 22", record.AccountHistory[1].Generation)
	}

	// A resume re-materializes the SAME account, so the copy advances without
	// the account changing.
	record.SetAccountGeneration(33)
	if record.AccountGeneration != 33 || record.AccountHistory[1].Generation != 33 {
		t.Errorf("after a resume: scalar = %d, open segment = %d, want both 33",
			record.AccountGeneration, record.AccountHistory[1].Generation)
	}

	// A generation that does not advance the record is not evidence of
	// anything: overwriting a known copy with an unknown one would blind the
	// check entirely.
	record.SetAccountGeneration(0)
	record.SetAccountGeneration(12)
	if record.AccountGeneration != 33 {
		t.Errorf("AccountGeneration = %d, want a non-advancing generation to be ignored", record.AccountGeneration)
	}
	(*SessionRecord)(nil).SetAccountGeneration(1)
}

// mirrorFailingAccountStore fails only the Touch calls that carry a generation
// mirror, and passes everything else through. It is the fault injection for the
// one failure the design cannot prevent: the material is published and the
// second, independent write that describes it is lost.
type mirrorFailingAccountStore struct {
	AccountStore

	fail bool
	err  error
}

func (s *mirrorFailingAccountStore) Touch(ctx context.Context, tenantID, accountID string, opts TouchAccountOptions) error {
	if s.fail && opts.SetCredentialGeneration > 0 {
		return s.err
	}
	return s.AccountStore.Touch(ctx, tenantID, accountID, opts)
}

// TestAccountTokenManager_ALostRotationMarkerSurvivesInTheEnvelope is the fault
// injection for the partial failure the generation design is built around.
//
// A renewal writes TWO things: the new envelope, and a metadata Touch that
// mirrors its generation onto the account record. The second write can be
// lost. When it is, the renewal is reported as failed — the record would
// otherwise go on describing the previous credential — but the rotation is
// NOT lost: the generation lives inside the envelope, written by the same Put
// as the material, so the authority (AccountTokenManager.CredentialGeneration)
// already names the new copy, and the next resolution both hands that copy out
// with its generation and repairs the mirror.
func TestAccountTokenManager_ALostRotationMarkerSurvivesInTheEnvelope(t *testing.T) {
	ctx := context.Background()
	inner := NewInMemoryAccountStore()
	broken := &mirrorFailingAccountStore{
		AccountStore: inner,
		fail:         true,
		err:          errors.New("the metadata store is unreachable"),
	}
	secretStore := newTestAccountSecretStore(t)

	account := &Account{
		ID: "acct-1", TenantID: "tenant-a",
		Provider: AccountProviderClaude, RefreshPolicy: AccountRefreshOAuth2,
	}
	if err := inner.Create(ctx, account); err != nil {
		t.Fatalf("Create: %v", err)
	}
	seeded, err := secretStore.Replace(ctx, "tenant-a", "acct-1", &AccountCredential{
		AccessToken:   "old-token",
		RefreshToken:  "refresh-1",
		TokenEndpoint: "https://auth.example.com/v1/oauth/token",
		ExpiresAt:     time.Now().Add(-time.Minute),
	})
	if err != nil {
		t.Fatalf("seed envelope: %v", err)
	}

	tokens := NewAccountTokenManager(secretStore, broken, 0)
	tokens.RegisterRefresher(AccountProviderClaude, &countingRefresher{})
	resolver := NewAccountResolver(broken, tokens, NewAccountStrategyRegistry(NewCostTracker(nil, nil)))

	_, err = resolver.Resolve(ctx, AccountResolveRequest{
		TenantID: "tenant-a", Provider: AccountProviderClaude, AccountID: "acct-1",
	})
	var stampFailed *AccountRefreshStampFailedError
	if !errors.As(err, &stampFailed) {
		t.Fatalf("Resolve error = %v, want the lost metadata write reported as AccountRefreshStampFailedError", err)
	}

	stored, err := secretStore.Get(ctx, "tenant-a", "acct-1")
	if err != nil {
		t.Fatalf("Get envelope: %v", err)
	}
	if stored.AccessToken != "renewed-token" {
		t.Fatalf("stored access token = %q, want the renewal to have landed", stored.AccessToken)
	}
	if stored.Generation <= seeded {
		t.Fatalf("the renewed envelope's generation = %d, want it past the seeded %d", stored.Generation, seeded)
	}
	// THE POINT: the authority names the new copy even though every metadata
	// write describing it failed.
	current, err := tokens.CredentialGeneration(ctx, account)
	if err != nil {
		t.Fatalf("CredentialGeneration: %v", err)
	}
	if current != stored.Generation {
		t.Fatalf("CredentialGeneration = %d, want the stored envelope's %d — a lost metadata write must not lose the rotation",
			current, stored.Generation)
	}
	if got, err := inner.Get(ctx, "tenant-a", "acct-1"); err != nil {
		t.Fatalf("Get account: %v", err)
	} else if got.CredentialGeneration != 0 {
		t.Fatalf("CredentialGeneration = %d, want the fault injection to have suppressed every mirror write", got.CredentialGeneration)
	}

	// With the metadata store healthy, the next resolution repairs the mirror
	// off the envelope — no rotation required, and no operator action.
	broken.fail = false
	resolved, err := resolver.Resolve(ctx, AccountResolveRequest{
		TenantID: "tenant-a", Provider: AccountProviderClaude, AccountID: "acct-1",
	})
	if err != nil {
		t.Fatalf("Resolve(after recovery): %v", err)
	}
	if resolved.Generation != stored.Generation {
		t.Errorf("ResolvedAccountCredential.Generation = %d, want the stored envelope's %d", resolved.Generation, stored.Generation)
	}
	repaired, err := inner.Get(ctx, "tenant-a", "acct-1")
	if err != nil {
		t.Fatalf("Get account: %v", err)
	}
	if repaired.CredentialGeneration != stored.Generation {
		t.Errorf("CredentialGeneration = %d, want the envelope's %d — the mirror was never reconciled",
			repaired.CredentialGeneration, stored.Generation)
	}
}

// TestAccountTokenManager_CredentialGenerationReadsTheAuthority: the current
// generation comes from where the material lives — the envelope for an inline
// account, the secret's current version for a referenced one — so a secret
// rewritten by its owner, which moves no account field, is still a new
// generation.
func TestAccountTokenManager_CredentialGenerationReadsTheAuthority(t *testing.T) {
	ctx := context.Background()

	t.Run("inline", func(t *testing.T) {
		secretStore := newTestAccountSecretStore(t)
		account := &Account{ID: "acct-1", TenantID: "tenant-a", Provider: AccountProviderClaude}
		first, err := secretStore.Replace(ctx, "tenant-a", "acct-1", &AccountCredential{AccessToken: "one"})
		if err != nil {
			t.Fatalf("Replace: %v", err)
		}
		tokens := NewAccountTokenManager(secretStore, NewInMemoryAccountStore(), 0)
		if got, err := tokens.CredentialGeneration(ctx, account); err != nil || got != first {
			t.Fatalf("CredentialGeneration = %d, %v; want the envelope's %d", got, err, first)
		}
		second, err := secretStore.Replace(ctx, "tenant-a", "acct-1", &AccountCredential{AccessToken: "two"})
		if err != nil {
			t.Fatalf("Replace: %v", err)
		}
		if got, err := tokens.CredentialGeneration(ctx, account); err != nil || got != second || second <= first {
			t.Fatalf("CredentialGeneration after a rotation = %d, %v; want the new envelope's %d", got, err, second)
		}
	})

	t.Run("secret reference", func(t *testing.T) {
		resolver := newMemorySecretResolver()
		resolver.put("tenant-a", "agents/claude/claude1/oauth_token", "token-a")
		tokens, _, account := refAccountFixture(t, resolver, AccountProviderClaude, "agents/claude/claude1/oauth_token", "")
		first, err := tokens.CredentialGeneration(ctx, account)
		if err != nil || first == 0 {
			t.Fatalf("CredentialGeneration = %d, %v; want the secret's version", first, err)
		}
		resolver.put("tenant-a", "agents/claude/claude1/oauth_token", "token-b")
		second, err := tokens.CredentialGeneration(ctx, account)
		if err != nil || second <= first {
			t.Fatalf("CredentialGeneration after the secret was rewritten = %d, %v; want past %d", second, err, first)
		}
		// The spawn path reads the same generation off the same version.
		cred, err := tokens.Resolve(ctx, account)
		if err != nil || cred.Generation != second {
			t.Errorf("Resolve generation = %+v, %v; want %d", cred, err, second)
		}
	})
}

// TestAccountTokenManager_CredentialGenerationFailsWhenTheAuthorityDoes: a
// nil account is refused, a missing envelope is its typed not-found, and a
// referenced secret that cannot be read is the reference's own typed error —
// never a zero that a caller might read as "nothing changed" without knowing
// the read failed.
func TestAccountTokenManager_CredentialGenerationFailsWhenTheAuthorityDoes(t *testing.T) {
	ctx := context.Background()
	tokens := NewAccountTokenManager(newTestAccountSecretStore(t), NewInMemoryAccountStore(), 0)

	var input *InputValidationError
	if _, err := tokens.CredentialGeneration(ctx, nil); !errors.As(err, &input) {
		t.Errorf("CredentialGeneration(nil) = %v, want InputValidationError", err)
	}
	var missing *AccountCredentialNotFoundError
	if _, err := tokens.CredentialGeneration(ctx, &Account{ID: "ghost", TenantID: "tenant-a"}); !errors.As(err, &missing) {
		t.Errorf("CredentialGeneration(no envelope) = %v, want AccountCredentialNotFoundError", err)
	}

	resolver := newMemorySecretResolver()
	refTokens, _, account := refAccountFixture(t, resolver, AccountProviderClaude, "agents/claude/absent", "")
	var unresolved *AccountSecretRefUnresolvedError
	if _, err := refTokens.CredentialGeneration(ctx, account); !errors.As(err, &unresolved) {
		t.Errorf("CredentialGeneration(absent secret) = %v, want AccountSecretRefUnresolvedError", err)
	}
	unwired, _, account := refAccountFixture(t, nil, AccountProviderClaude, "agents/claude/claude1/oauth_token", "")
	var notConfigured *AccountSecretResolverNotConfiguredError
	if _, err := unwired.CredentialGeneration(ctx, account); !errors.As(err, &notConfigured) {
		t.Errorf("CredentialGeneration(no resolver) = %v, want AccountSecretResolverNotConfiguredError", err)
	}
}

// generationStub answers CredentialGeneration from a script.
type generationStub struct {
	generation int64
	err        error
}

func (g *generationStub) CredentialGeneration(context.Context, *Account) (int64, error) {
	return g.generation, g.err
}

// TestSessionAccountFreshness_TheAuthorityDecidesOverTheMirror: with the
// authority wired, a referenced account whose secret was rewritten reads as
// stale although its metadata record never moved; a read that fails is
// reported alongside a "not stale" answer.
func TestSessionAccountFreshness_TheAuthorityDecidesOverTheMirror(t *testing.T) {
	h, ctx, sessionID := newFreshnessHarness(t)
	record, err := h.service.store.GetSession(ctx, "tenant-a", sessionID)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	record.SetAccountGeneration(4)
	if err := h.service.store.PutSession(ctx, record); err != nil {
		t.Fatalf("PutSession: %v", err)
	}

	h.service.SetAccountCredentialGenerations(&generationStub{generation: 5})
	freshness, err := h.service.SessionAccountFreshness(ctx, sessionID)
	if err != nil {
		t.Fatalf("SessionAccountFreshness: %v", err)
	}
	if !freshness.Stale || freshness.SessionGeneration != 4 || freshness.AccountGeneration != 5 {
		t.Fatalf("freshness = %+v, want stale on generations 4 vs 5 read from the authority", freshness)
	}

	h.service.SetAccountCredentialGenerations(&generationStub{generation: 4})
	if freshness, err := h.service.SessionAccountFreshness(ctx, sessionID); err != nil || freshness.Stale {
		t.Fatalf("freshness = %+v, %v; want the session holding the authority's copy reported current", freshness, err)
	}

	unreachable := errors.New("the secrets store is unreachable")
	h.service.SetAccountCredentialGenerations(&generationStub{err: unreachable})
	freshness, err = h.service.SessionAccountFreshness(ctx, sessionID)
	if !errors.Is(err, unreachable) {
		t.Fatalf("SessionAccountFreshness error = %v, want the authority's failure reported", err)
	}
	if freshness == nil || freshness.Stale {
		t.Errorf("freshness = %+v, want a failed read answered as not stale", freshness)
	}
}
