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

/**
 * Authentication state store — the single source of truth for whether the
 * current tab is signed in.
 *
 * Why this store exists
 * ---------------------
 * MainApp owns a route guard that bounces protected routes to /login whenever
 * authStatus is 'unauthenticated'. Before this store, authStatus lived inside
 * MainApp and was only ever re-evaluated on initial mount or on a cross-tab
 * `storage` event. A successful in-tab login (passkey, PIV, or self-register)
 * persisted the token and navigated to the dashboard, but authStatus stayed
 * 'unauthenticated' in THIS tab — so the guard fired on the very next tick and
 * silently bounced the freshly-authenticated user back to the login chooser.
 *
 * Hoisting authStatus into a shared reactive store fixes that: the login
 * completion paths call setAuthenticated() the moment the server returns
 * tokens, flipping the status in-tab so the guard sees 'authenticated' and
 * leaves the user on the dashboard.
 *
 * Token storage keys are owned here so every consumer reads/writes the same
 * localStorage slots ("quicbaas_token" / "quicbaas_refresh_token").
 *
 * Reactivity shape: module-scope `let x = $state(...)` + getter object — the
 * same pattern every other store in this app uses (connectionStore,
 * routerStore, sessionStore). A directly-exported $state proxy does not
 * propagate template updates across module boundaries in this Svelte 5 build.
 */

import { getSDKClient } from './sdkClient.ts';

export type AuthStatus = 'pending' | 'authenticated' | 'unauthenticated';

export const TOKEN_KEY = 'quicbaas_token';
export const REFRESH_TOKEN_KEY = 'quicbaas_refresh_token';

/**
 * Path of the session-renewal endpoint.
 *
 * NOT /api/v1/auth/refresh. On a composed node that literal path belongs to
 * the xkms module's own session-login plane, which redeems against a DIFFERENT
 * refresh-token store. This SPA's session is minted by quicbaas's login
 * ceremony (/api/v1/auth/login/{begin,finish}) and its refresh credential lives
 * in quicbaas's iam.OIDCProvider token store, so renewing at the xkms path
 * always answered 401 "invalid refresh token" and the session died at the
 * 15-minute access-token boundary (go-quicbaas#221). quicbaas's own renewal
 * verb is IAM-namespaced to clear that collision — the same move that put PIV
 * login at /api/v1/iam/login/piv (pkg/api/rest/routes_iam.go).
 *
 * Exported so authFetch's never-intercept rule reads the SAME constant: two
 * copies of this path that drift make the interceptor recurse on renewal.
 */
export const REFRESH_PATH = '/api/v1/iam/refresh';

let status = $state<AuthStatus>('pending');

/**
 * Claims carried by the session bearer that the UI reads directly.
 *
 * The server is the authority on all of them — nothing here is trusted for an
 * authorization decision, because every request is re-checked server-side. They
 * exist so the UI can address the RIGHT tenant and render the right identity
 * instead of guessing.
 */
export interface SessionClaims {
  /** Tenant the session belongs to (pkg/iam/oidc_tokens.go). */
  tenant_id?: string;
  /** Roles granted to the principal within that tenant. */
  roles?: string[];
  /** Immutable principal identifier (#275/#300). */
  uid?: string;
  /** Subject — the login identifier, which can be re-pointed; prefer uid. */
  sub?: string;
  /**
   * Per-login session identifier (pkg/iam/oidc_tokens.go, go-quicbaas#326).
   *
   * The server mints it at login and replays it on every renewal of that
   * login, so it is the ONLY claim that answers "is this credential the same
   * session as the last one". Every other claim in the bearer moves for a
   * refresh and a second login alike: uid and tenant_id are byte-identical
   * across both, while jti and exp differ across both.
   *
   * Absent on a token minted before the claim existed. Treat absence as "not
   * the same session" — see reconcileSessionIdentity.
   */
  sid?: string;
  /** Expiry, seconds since epoch. */
  exp?: number;
}

/**
 * Decode a JWT's claims segment. Pure string/Base64 work with NO signature
 * verification: the server validates the token on every request, and this only
 * decides what the UI asks for. Returns null for anything that is not a
 * three-part JWT with a parseable JSON payload — an opaque or malformed token
 * yields no claims rather than a throw.
 *
 * This is the SINGLE decoder in the SPA. Session-derived values (expiry,
 * tenant) all come through here so a second, subtly different implementation
 * cannot drift away from it.
 */
function decodeClaims(jwt: string): SessionClaims | null {
  const parts = jwt.split('.');
  const claimsSegment = parts.length === 3 ? parts[1] : undefined;
  if (claimsSegment === undefined) return null;
  try {
    const json = atob(claimsSegment.replace(/-/g, '+').replace(/_/g, '/'));
    const parsed: unknown = JSON.parse(json);
    if (typeof parsed !== 'object' || parsed === null) return null;
    return parsed as SessionClaims;
  } catch {
    return null;
  }
}

/**
 * Tenant of the session currently persisted in this tab, or '' when there is
 * no session (or the stored token carries no tenant).
 *
 * Reactive so that a login, a renewal, or a cross-tab session change moves
 * every tenant-scoped view onto the new tenant on the spot. It is DERIVED FROM
 * THE TOKEN on every read of the stored credential, never persisted as a user
 * preference: a stored preference outlives the session that justified it, and
 * an operator who signs into a different tenant would keep querying the old one
 * (go-quicbaas#309).
 */
let sessionTenant = $state('');

/**
 * Immutable identifier of the principal the stored session belongs to (#275/
 * #300), or '' when there is no session.
 *
 * Distinct from the tenant because two different principals can share one: it
 * is what lets a UI choice be scoped to the person who made it rather than to
 * the tenant they happened to be in.
 */
let sessionUID = $state('');

/**
 * Identifier of the LOGIN the stored session belongs to (the server's sid
 * claim), or '' when there is no session or the token predates the claim.
 *
 * Reactive because sessionKey is built from it: it is what makes two logins by
 * the same principal into the same tenant different sessions in the key itself,
 * rather than only by way of a counter this tab has to have moved.
 */
let sessionSID = $state('');

/**
 * Re-read the claims cache from whatever token is currently stored. Called on
 * module load, after every token write, on every session teardown, and whenever
 * the session is re-validated (which covers the cross-tab `storage` path in
 * MainApp).
 *
 * It must run on the paths that END a session too, not only those that mint
 * one: a cache that is only refreshed on a write keeps answering with the
 * tenant of a session that is already gone.
 */
function syncSessionClaims(): void {
  const claims = decodeClaims(readAuthToken());
  sessionTenant = claimString(claims?.tenant_id);
  sessionUID = claimString(claims?.uid);
  sessionSID = claimString(claims?.sid);
}

/**
 * A claim as a string, or '' for anything that is not one.
 *
 * decodeClaims parses JSON and ASSERTS the shape — the interface describes what
 * the server sends, it does not check it — so a claim can arrive as any JSON
 * value. `?? ''` would not catch that: it only replaces null and undefined, so
 * `sid: 0` or `sid: false` would survive into the cache.
 *
 * That matters for exactly one reason, and it is a fail-closed one. The
 * same-session test in reconcileSessionIdentity rejects '' precisely so that two
 * credentials carrying NO identifier never look like one another; a non-string
 * value would slip past that check and then compare equal to the next one just
 * like it, which is the leak the check exists to prevent. Narrowing to a string
 * here means every value the guard can ever see is either a real identifier or
 * the empty string.
 */
function claimString(value: unknown): string {
  return typeof value === 'string' ? value : '';
}

/**
 * Why the session ended, when it ended on its own rather than by the operator
 * signing out. The login page shows it: a session that vanishes with no
 * explanation is indistinguishable from the app being broken, and #221 was
 * exactly that experience for an hours-long supervised run.
 *
 * Empty for a deliberate sign-out and before any session has ended.
 */
let endReason = $state('');

/**
 * Monotonic session generation. Every deliberate session transition (login,
 * logout) bumps it, so an ASYNC session check can tell whether the session it
 * was validating is still the current one.
 *
 * Without this a validateSession() that was in flight when the user signed out
 * resolves afterwards and writes 'authenticated' back — resurrecting a session
 * the user just ended. The window is small but real, and it is a security
 * regression, not a cosmetic one.
 *
 * Reactive, because sessionKey is built from it: two logins by the same person
 * into the same tenant are DIFFERENT sessions, and anything cached under the
 * first must not become valid again under the second.
 */
let generation = $state(0);

/**
 * FINGERPRINT of the credential the current generation was established with, or
 * '' when there is no session. Not the credential itself — see
 * credentialFingerprint below for why the raw bearer is not retained here.
 *
 * This is what makes "which login is this" answerable. The claims cannot answer
 * it: a second login by the same principal into the same tenant carries a
 * byte-identical `uid` and `tenant_id`, so comparing claims reports "no change"
 * across what is genuinely a different login with possibly different grants.
 * Comparing the CREDENTIAL catches it, because a new login always mints a new
 * one.
 *
 * Written by persistAccessToken — the one writer of the token — so a renewal
 * this tab performs updates it WITHOUT moving the generation, which is what
 * keeps a refresh from being mistaken for a new session. A credential that
 * appears in storage without this tab writing it therefore belongs to some other
 * session, and reconciliation treats it as one.
 */
let generationCredential = '';

/**
 * The server's identifier for the login the current generation belongs to, or
 * '' when there is no session (or its credential predates the sid claim).
 *
 * Written alongside generationCredential, by the same single writer, for the
 * question the credential CANNOT answer: a credential this tab did not write is
 * either a sibling tab's renewal of THIS login or somebody's new login, and the
 * bytes look the same either way. The sid does answer it — the server keeps it
 * stable across a renewal and mints a new one for every login — so a sibling's
 * renewal can be recognised as the same session instead of costing this tab its
 * caches (go-quicbaas#326).
 *
 * Kept as a snapshot rather than read from sessionSID at the moment of
 * comparison because reconciliation re-derives sessionSID from the NEW token
 * before it decides; the value being compared against has to be the one the
 * generation was established with.
 */
let generationSID = '';

/**
 * A short, non-secret summary of a credential, used only to answer "is this the
 * same string as before".
 *
 * DELIBERATELY NOT A CRYPTOGRAPHIC HASH, and this must stay that way. The only
 * synchronous digest available in a browser is a hand-rolled one; `crypto.subtle`
 * returns a Promise, and making this async would force persistAccessToken async
 * too — which would destroy the property the whole credential-scoping design
 * rests on: a synchronous caller passes the generation it is already holding, so
 * the check is free and trivially true, and ONLY a caller that crossed an await
 * can fail it. Trading that invariant for a stronger digest would be theatre.
 *
 * It is not a security boundary. It is never compared against anything an
 * attacker supplies, and it authorises nothing; the worst a collision can do is
 * miss a cache invalidation. Do not "upgrade" it to SHA-256.
 *
 * Why fingerprint at all, given the token is in localStorage in plaintext by
 * design: holding a second reference to a raw bearer for the lifetime of the
 * session, purely to answer a sameness question, keeps a rotated credential
 * reachable after it should be collectable and adds another place it can be
 * captured in a heap snapshot or serialised into an error report. The sameness
 * question does not need the secret, so it should not hold it.
 *
 * Two FNV-1a passes seeded differently, plus the length. The width is chosen
 * rather than defaulted: a single 32-bit pass collides at roughly 2^-32 per
 * comparison, small but not nothing, and a collision here means one login's
 * cache stays in scope across a second login by the same principal in the same
 * tenant.
 *
 * What that buys, stated honestly: the two passes share a multiplier and differ
 * only in seed, so they are NOT independent hashes and this is NOT a true 64-bit
 * digest. The supportable claim is narrower — a pair that collides in one pass
 * is very unlikely to collide in the other, and the length is folded in so a
 * collision requires agreement on all three. That is empirical, not derived, and
 * it is deliberately enough for a sameness check and not offered as more. Do not
 * quote a bit-strength from this function.
 *
 * The alternatives considered were a random identifier persisted alongside the
 * credential — a second value that must stay in sync with the token, which is
 * the divergence shape that produced four separate defects in this file — or a
 * cryptographic digest, which is async (see above).
 */
function credentialFingerprint(token: string): string {
  if (token === '') return '';
  // Different offset bases, so the two passes cannot agree by construction: a
  // pair colliding in one is overwhelmingly unlikely to collide in the other.
  let a = 0x811c9dc5;
  let b = 0x01000193;
  for (let i = 0; i < token.length; i++) {
    const code = token.charCodeAt(i);
    a = Math.imul(a ^ code, 0x01000193) >>> 0;
    b = Math.imul(b ^ code, 0x01000193) >>> 0;
  }
  // Length included so two tokens must agree on all three to collide.
  return `${token.length.toString(36)}.${a.toString(36)}.${b.toString(36)}`;
}

/**
 * Callbacks run whenever the scope that cached data belongs to changes.
 *
 * The scope has two halves and this is the one bus for both: the SESSION half
 * (login, logout, a session replaced in another tab), which this module owns and
 * announces itself; and the TENANT half, announced by whoever owns the active
 * tenant — orchestrator.svelte.ts, when the operator uses the switcher. A store
 * holding tenant-scoped data cares about exactly the same event either way:
 * what it holds is no longer what should be on screen.
 *
 * A registry rather than direct calls because the stores that hold that data
 * already import THIS module, so auth importing them back would be a cycle —
 * and because a list of stores to notify by name is a list someone has to
 * remember to update. They register; the bus announces.
 */
const scopeChangeListeners = new Set<() => void>();

/**
 * Register `fn` to run whenever the data scope changes. Returns an unsubscribe.
 *
 * This is the belt to sessionKey's braces. The key alone makes stale data
 * unreachable; this is what actually releases it and asks for the replacement,
 * so "signing out clears protected data" and "switching tenant shows the new
 * tenant" are things the code DOES rather than properties that happen to hold.
 */
export function onSessionScopeChange(fn: () => void): () => void {
  scopeChangeListeners.add(fn);
  return () => scopeChangeListeners.delete(fn);
}

/**
 * Tell every registered store that the scope changed. Exported for the tenant
 * half: the session half is announced from the transitions below.
 */
export function announceSessionScopeChange(): void {
  // Snapshot before iterating: a listener may unsubscribe during the walk. And
  // isolate failures — these listeners are what RELEASE protected data, so one
  // throwing must not stop the others from running.
  for (const fn of [...scopeChangeListeners]) {
    try {
      fn();
    } catch {
      // A listener that cannot clean up must not block the ones that can.
    }
  }
}

export const authStore = {
  get status(): AuthStatus {
    return status;
  },
  /** Why the session ended unexpectedly; '' after a deliberate sign-out. */
  get endReason(): string {
    return endReason;
  },
  /**
   * Tenant the current session belongs to, or '' when signed out. The source
   * of truth for every tenant-scoped query the SPA issues.
   */
  get tenantID(): string {
    return sessionTenant;
  },
  /**
   * Immutable identifier of the signed-in principal, or '' when signed out.
   * Use it to scope anything that must belong to one person rather than to
   * whichever tenant they are in.
   */
  get principalUID(): string {
    return sessionUID;
  },
  /**
   * Opaque key identifying WHICH session this is: the login generation, the
   * principal, the tenant, and the server's identifier for the login itself.
   * Two sessions share a key only when they are the same login — not merely the
   * same person in the same tenant.
   *
   * Two independent things make a SECOND login distinct from the first, and both
   * are here on purpose. The generation is this tab's own account of the session
   * transitions it saw or reconciled. The sid is the SERVER's, and it is what
   * makes the distinction true by construction rather than true whenever this
   * tab's bookkeeping was right: two logins carry different sids whether or not
   * anything in this module noticed the transition. Without either, signing out
   * and back in as the same person into the same tenant produced an identical
   * key, and everything cached under the first session silently became valid
   * again under the second — with no server check, and with the freshness window
   * suppressing the corrective fetch. Grants can change between two logins, so
   * that is a real re-authorization being skipped, not just a stale render.
   *
   * Deliberately NOT the token's jti: that changes on every refresh too, which
   * would discard every cache every 15 minutes for no security benefit. The
   * generation moves on session transitions only, and the sid is minted per
   * LOGIN and replayed across that login's renewals — which is exactly why it
   * can be in the key when the jti cannot.
   *
   * Anything cached or requested on behalf of a session should be tagged with
   * this and discarded when it stops matching — a tenant alone cannot tell two
   * admins apart, and a uid alone cannot tell one admin's two tenants apart.
   *
   * A NUL separator, because no part of it can contain one, so no pair
   * of distinct sessions can collide on one key. Written as an escape, never as
   * a literal: a raw NUL byte makes the source file binary to git, which costs
   * every future diff and review of this file.
   */
  get sessionKey(): string {
    return `${generation}\u0000${sessionUID}\u0000${sessionTenant}\u0000${sessionSID}`;
  },
};

// Seed the claims cache from whatever session survived a page reload, so the
// first render already addresses the right tenant instead of a placeholder.
syncSessionClaims();

// Seed generationCredential/generationSID from that SAME survived session,
// not just the claims cache above. Both default to '' and are otherwise
// updated ONLY by a write this tab itself performs (persistAccessToken) or a
// reconciliation that already found a mismatch — so a tab that inherits an
// already-valid session from localStorage (a reload, or simply opening the
// app in a tab that never itself logged in) started every one of its own
// reconcileSessionIdentity() calls believing '' was its own credential. The
// very first such call — inside refreshAccessToken, or from validateSession
// on mount — therefore ALWAYS found a "mismatch" against the real, unchanged
// token already in storage, and treated an ordinary page load as a brand new
// session: an unwarranted generation bump plus announceSessionScopeChange()
// discarding every tenant-scoped cache for no reason, on every reload
// (go-quicbaas UI-login-refused investigation, 2026-09-25). Seeding here,
// the same way persistAccessToken seeds it after a write, means the FIRST
// reconciliation in an inherited tab finds the credential it should: its own,
// unchanged since the module loaded.
generationCredential = credentialFingerprint(readAuthToken());
generationSID = sessionSID;

/**
 * Clear the session-end explanation. The login view calls it once the message
 * has been shown and the operator starts a fresh sign-in, so a stale reason
 * never rides along into the next session.
 */
export function clearSessionEndReason(): void {
  endReason = '';
}

/** Read the persisted access token, or '' when none is stored. */
export function readAuthToken(): string {
  if (typeof window === 'undefined') return '';
  return window.localStorage.getItem(TOKEN_KEY) ?? '';
}

/**
 * Persist an access token and re-derive the claims cache from it.
 *
 * The ONLY writer of TOKEN_KEY. Every path that mints or renews a session goes
 * through here, so the cached tenant can never be one token behind the
 * credential actually being sent — which is exactly the class of bug #309 was.
 */
function persistAccessToken(
  token: string,
  forGeneration: number,
  requireCurrentCredential = false,
): boolean {
  // A credential is always FOR a session, and the caller has to say which. A
  // decision made before an await is not a decision about the session that
  // exists after it: a renewal that started under session A and returns after
  // the operator signed out, or signed into B, would otherwise write A's rotated
  // bearer over current storage — resurrecting A, or clobbering B.
  //
  // Requiring the generation is what makes that unwriteable rather than
  // merely checked. A synchronous caller passes the generation it is already
  // holding and the test is trivially true; only a caller that crossed an await
  // can fail it, which is exactly the caller that should.
  if (generation !== forGeneration) return false;
  // The generation check above is THIS TAB'S bookkeeping and cannot see a
  // SIBLING tab (or window) replacing the session in SHARED storage while a
  // renewal's round trip was in flight — a renewal that started under a dead
  // session and only succeeds server-side well after a sibling tab's fresh
  // login would otherwise overwrite that fresh login with a rotated bearer
  // for the session it is not attached to. requireCurrentCredential — passed
  // only by the renewal path (commitRenewal), never by a login — closes that:
  // generationCredential moves only when THIS tab itself writes, so it still
  // names the pre-renewal credential throughout the round trip, and a mismatch
  // against what is ACTUALLY in storage right now means someone else already
  // replaced it. A login skips this check on purpose: it is the operator's own
  // deliberate action in THIS tab and must always win, cross-tab replacement
  // included — that is what "signing in starts a new session" means
  // (go-quicbaas UI-login-refused investigation, 2026-09-25).
  if (requireCurrentCredential && credentialFingerprint(readAuthToken()) !== generationCredential) {
    return false;
  }
  if (typeof window !== 'undefined') {
    window.localStorage.setItem(TOKEN_KEY, token);
  }
  syncSessionClaims();
  // This tab wrote this credential, so it belongs to the generation in effect.
  // Recording it here is what separates a renewal (same session, new bytes) from
  // a credential that arrived from somewhere else.
  generationCredential = credentialFingerprint(token);
  // syncSessionClaims above re-derived sessionSID from this very token, so this
  // is the login the generation now belongs to.
  generationSID = sessionSID;
  return true;
}

/**
 * Commit a completed renewal for the session identified by `forGeneration`.
 *
 * The three writes a successful renewal makes — the access token, the rotated
 * refresh credential, and the authenticated status — happen together or not at
 * all. They are here, in one place, under one test, because a late response must
 * commit NOTHING: leaving the status or the refresh credential behind while
 * skipping the access token would be a session half-resurrected, which is worse
 * than either outcome on its own.
 *
 * Returns the committed token, or '' when the session moved on while the server
 * was answering.
 */
/**
 * Store the refresh credential a session was issued, or CLEAR the slot when it
 * was issued none.
 *
 * The single writer, for the same reason persistAccessToken is: two paths issue
 * a session — a login and a renewal — and they disagreed. The renewal cleared on
 * an omitted credential, with the reasoning written out below; the login left
 * whatever was there, which after a second login is the PREVIOUS principal's
 * refresh credential, sitting in storage where the next renewal would redeem it
 * (go-quicbaas#314). A rule that holds only because every caller happens to
 * cooperate is not a rule.
 *
 * Absent, empty and whitespace-only are all "issued none". A credential the
 * server never really sent is not one worth keeping, and storing it would only
 * defer the failure to the next renewal, which would present it and lose the
 * session.
 */
function persistRefreshToken(token: string | undefined): void {
  if (typeof window === 'undefined') return;
  const issued = (token ?? '').trim();
  if (issued === '') {
    // The server rotates on EVERY use, so anything still held is revoked.
    // Keeping it would present a dead credential at the next boundary and trip
    // the stolen-token detection, which revokes the whole family. Dropping it
    // costs nothing that is not already lost and ends the session cleanly one
    // access-token TTL from now instead.
    window.localStorage.removeItem(REFRESH_TOKEN_KEY);
    return;
  }
  window.localStorage.setItem(REFRESH_TOKEN_KEY, issued);
}

function commitRenewal(
  forGeneration: number,
  next: string,
  rotated: string | undefined,
): string {
  if (!persistAccessToken(next, forGeneration, /* requireCurrentCredential */ true)) return '';
  persistRefreshToken(rotated);
  endReason = '';
  applyStatus('authenticated');
  return next;
}

/** Tokens returned by a successful login/registration finish. */
export interface AuthTokens {
  access_token: string;
  refresh_token?: string;
}

/**
 * Persist tokens and flip the in-tab status to 'authenticated'. Called by
 * every login-completion path (passkey, PIV, self-register) the instant the
 * server confirms the session, so the MainApp route guard no longer bounces
 * the freshly-signed-in user back to /login.
 */
/**
 * The ONLY assignment to `status` in this module.
 *
 * Deliberately trivial, and deliberately the single one: an
 * auth-status-writes-once test reads this file and fails if a second assignment
 * appears. Every transition therefore has to come through applyStatus below,
 * which is what makes "ending a session" impossible to express as a status
 * change alone.
 */
function writeStatus(next: AuthStatus): void {
  status = next;
}

/**
 * End the session. THE owner of that event — there is no other way to reach
 * 'unauthenticated'.
 *
 * Three separate defects on this branch were each one call site that flipped
 * the status and forgot part of this: an explicit sign-out, a session removed
 * in another tab, and a validation the server rejected. The third was the worst
 * — the rejected token stayed in storage, the tenant kept its name, and the
 * scoped getters kept serving the previous principal's rows after the server had
 * said the session was invalid. Patching a fourth call site was the obvious next
 * step and would have been the wrong one, so the knowledge lives here instead of
 * in each caller.
 *
 * Ending a session is five things, always together:
 *   1. the credentials are gone from storage,
 *   2. the claims cache is re-derived from that (now empty) storage,
 *   3. the generation moves, so nothing cached under the old key matches,
 *   4. the status says so, and
 *   5. every store holding scoped data is told to release it.
 *
 * `reason` explains an end the operator did not ask for — a refusal, a
 * credential that was never there. The login page renders it. A deliberate
 * sign-out passes nothing: there is no failure to explain.
 */
function endSession(reason: string): void {
  if (typeof window !== 'undefined') {
    window.localStorage.removeItem(TOKEN_KEY);
    window.localStorage.removeItem(REFRESH_TOKEN_KEY);
  }
  syncSessionClaims();
  generationCredential = '';
  generationSID = '';
  generation++;
  endReason = reason;
  writeStatus('unauthenticated');
  announceSessionScopeChange();
}

/**
 * Apply an auth status. The single door every transition goes through.
 *
 * 'unauthenticated' is not a status this function can simply set: it routes to
 * endSession, which does the whole thing. That routing is the structural part —
 * a caller cannot express "mark the tab signed out but keep the data", because
 * there is no code path that does it.
 */
function applyStatus(next: AuthStatus, reason = ''): void {
  if (next === 'unauthenticated') {
    endSession(reason);
    return;
  }
  writeStatus(next);
}

export function setAuthenticated(tokens: AuthTokens): void {
  // No await between deciding and writing, so the generation in hand IS the one
  // this is authorised against and the test cannot fail. Passing it anyway is
  // what makes the rule structural: there is no way to write a credential
  // without naming a session, so a future caller inherits the guarantee instead
  // of having to remember it.
  persistAccessToken(tokens.access_token, generation);
  persistRefreshToken(tokens.refresh_token);
  generation++;
  endReason = '';
  applyStatus('authenticated');
  announceSessionScopeChange();
}

/**
 * Public name for ending the session; see endSession for what that involves.
 */
export function setUnauthenticated(reason = ''): void {
  endSession(reason);
}

/**
 * Reconcile this tab's in-memory session with what is actually in storage, and
 * report whether the IDENTITY behind the tab changed.
 *
 * Two kinds of transition reach this module, and they bump the generation under
 * different rules:
 *
 *   • DELIBERATE — this tab acted (setAuthenticated / setUnauthenticated).
 *     Always a new session, even when the same person signs back into the same
 *     tenant, because that genuinely is a second login with possibly different
 *     grants.
 *   • RECONCILED — another tab acted, and this tab learns of it here. A new
 *     session whenever the stored credential is not the one this generation was
 *     established with. Comparing the CLAIMS instead was not enough: a second
 *     login by the same principal into the same tenant carries identical claims,
 *     so the previous login's cached rows stayed in scope and the freshness
 *     window suppressed the corrective fetch (go-quicbaas#326).
 *
 * A renewal performed by THIS tab updates the recorded credential without
 * moving the generation, so it is not mistaken for a new session. A renewal
 * performed by ANOTHER tab writes a credential this tab did not, so the
 * credential alone cannot tell it from a fresh login — but the server's sid
 * can, because it is stable across a renewal and different for every login
 * (go-quicbaas#326). The sid is therefore the deciding question once the
 * credential says "not mine", and it is asked FAIL-CLOSED: a token carrying no
 * sid, or one naming a different login, is another session.
 */
function reconcileSessionIdentity(): boolean {
  const stored = readAuthToken();
  syncSessionClaims();
  // First question: is the credential in storage the one this generation belongs
  // to? A renewal this tab performed updated both together, so it matches, and
  // there is nothing to reconcile.
  const storedCredential = credentialFingerprint(stored);
  if (storedCredential === generationCredential) return false;

  // Second question, for a credential this tab did not write: is it still THIS
  // login? Only the server can say. A sid that matches means a sibling tab
  // renewed the session this tab is already in — same login, same grants, new
  // bytes — so the caches stay and the generation does not move. Anything else
  // is a different session: a sign-in, a sign-out, or a fresh login by the very
  // same principal into the very same tenant, whose uid and tenant_id are
  // byte-identical to this one's and so cannot be compared to find it.
  //
  // The empty-sid case is the one to be careful with: a token predating the
  // claim, or an opaque credential that decodes to nothing, yields ''. Two of
  // those must NOT compare equal, or every unclaimed credential would look like
  // a renewal of every other. Requiring a non-empty value is what keeps the
  // absence of evidence from being read as evidence of sameness.
  const sameLogin = sessionSID !== '' && sessionSID === generationSID;
  generationCredential = storedCredential;
  generationSID = sessionSID;
  if (sameLogin) return false;

  generation++;
  announceSessionScopeChange();
  return true;
}

/**
 * Snapshot the current session generation. Pair with endSessionIfCurrent to
 * make an async decision that belongs to THIS session unable to affect a
 * later one.
 */
export function sessionGeneration(): number {
  return generation;
}

/**
 * End the session, but only if it is still the one `snapshot` was taken from.
 *
 * The request interceptor decides "this credential is refused on arrival, so
 * the session is over" only AFTER two awaited round trips (a renewal and a
 * replay). Four things can have happened meanwhile, and the two checks below
 * are what separate them:
 *
 *   • nothing — both checks pass, the session ends with the reason attached.
 *   • the user signed in again IN THIS TAB — setAuthenticated bumped
 *     generation, so the verdict belongs to a dead session and must not
 *     clear the live one.
 *   • a sibling request in this tab already ended it — setUnauthenticated
 *     bumped generation, so this is exactly-once for free, with no flag to
 *     reset.
 *   • a SIBLING TAB (or this tab's own operator, signing in a second time)
 *     replaced the credential in shared storage while this verdict's round
 *     trip was in flight — this tab's generation cannot see that at all (it
 *     is purely this tab's own bookkeeping), but generationCredential can:
 *     it is updated by EVERY write this tab itself makes (a login or a
 *     renewal alike), so it names exactly the credential this tab still
 *     believes is its own. Comparing what is ACTUALLY in storage right now
 *     against that belief — not against a value frozen before the round
 *     trip — is what lets a verdict correctly survive this tab's OWN
 *     renewal rotating the credential (generationCredential moves WITH it)
 *     while still catching a sibling's replacement (generationCredential
 *     stays put, storage does not).
 *
 * Without the credential check, a renewal this tab began against a dead
 * refresh token could resolve 401 after a sibling tab — or this tab's own
 * operator, signing in again — had already written a live session; "the
 * generation hasn't moved" would still read true for THIS tab, and the
 * stale verdict would end the session that just started, wiping
 * localStorage out from under a login the failed renewal had nothing to do
 * with (go-quicbaas UI-login-refused investigation, 2026-09-25: the owner
 * signed in with a hardware key and every orchestrator/scheduler request
 * went out with no Authorization header at all, because a sibling tab's
 * dead-session renewal had wiped the credential moments after it was
 * written).
 *
 * Guarding on "is a refresh token still stored" instead would get the first
 * three cases wrong: it cannot see a re-login, and it silently skips the
 * cleanup when the server answered a renewal 2xx WITHOUT a replacement
 * credential (see refreshAccessToken), which is precisely a session that
 * needs ending.
 */
export function endSessionIfCurrent(snapshot: number, reason: string): void {
  if (generation !== snapshot) return;
  if (credentialFingerprint(readAuthToken()) !== generationCredential) return;
  setUnauthenticated(reason);
}

/**
 * Force the status to a known value.
 *
 * NOT "without touching token storage" any more: passing 'unauthenticated' ends
 * the session properly, because that is the only thing being unauthenticated can
 * mean. The Wails path (auth is OS-level) uses it to assert 'authenticated'.
 */
export function setAuthStatus(next: AuthStatus): void {
  applyStatus(next);
}

/** Read the persisted refresh token, or '' when none is stored. */
export function readRefreshToken(): string {
  if (typeof window === 'undefined') return '';
  return window.localStorage.getItem(REFRESH_TOKEN_KEY) ?? '';
}

/**
 * Seconds until the given JWT expires, or null when the token is not a decodable
 * JWT with a numeric exp claim. Returning null (rather than 0) lets the caller
 * leave an undecodable/opaque token untouched instead of refreshing it
 * needlessly.
 */
function secondsUntilExpiry(jwt: string): number | null {
  const exp = decodeClaims(jwt)?.exp;
  if (typeof exp !== 'number') return null;
  return exp - Math.floor(Date.now() / 1000);
}

// Skew window: refresh when fewer than this many seconds remain so an in-flight
// request never races the access-token expiry.
const REFRESH_SKEW_SECONDS = 60;

// Single-flight guard so concurrent requests trigger at most one refresh call.
let _refreshInFlight: Promise<string> | null = null;
/** Session the registered renewal belongs to; -1 when none is registered. */
let _refreshGeneration = -1;
/**
 * Identity of the registered renewal, and the source of those ids.
 *
 * Renewals from two sessions can be alive at once — that is deliberate, since a
 * renewal for a departed session must not be shared with the session that
 * replaced it. Once two can overlap, "the in-flight renewal" is no longer a
 * slot any completing renewal may clear: whoever finishes first would
 * deregister whoever is actually registered, and the next caller would find an
 * empty slot and start a SECOND renewal against the same rotating refresh
 * token. The server treats the second redemption as reuse and revokes the
 * family, so the live session dies — the operator is signed out for clicking
 * around (go-quicbaas#309).
 *
 * The id is what lets a completing renewal ask whether it is still the
 * registered one before clearing. Same rule as persistAccessToken: identify
 * yourself before mutating shared state.
 */
let _refreshID = 0;
let _nextRefreshID = 1;

/**
 * Name of the cross-tab mutex guarding refresh-token redemption. One name for
 * the whole origin is correct, not a narrowing: the refresh token being
 * redeemed already scopes the critical section to one session, because two
 * DIFFERENT sessions' tabs are never trying to spend the same single-use
 * credential at once.
 */
const REFRESH_LOCK_NAME = 'quicbaas-refresh-token';

/**
 * Run `fn` under a cross-tab mutex when the browser offers one, or run it
 * directly when it does not.
 *
 * _refreshInFlight (above) serializes callers WITHIN one tab's own module
 * instance; it cannot see a SIBLING tab's concurrent redemption of the same
 * refresh token, because every open tab loads its own independent copy of
 * this module with its own copy of that variable — shared only through
 * localStorage, which two tabs can both read the SAME still-valid credential
 * from before either has written its rotation. The server treats that second
 * presentation as reuse and revokes the WHOLE token family, including the
 * fresh access token the FIRST tab's redemption just received a moment
 * earlier — which is what "the session was renewed but the server rejected
 * the new credentials" actually was: not a bad credential, a credential
 * whose sibling had already spent the refresh token it was rotated from
 * (go-quicbaas UI-login-refused investigation, 2026-09-25). Two tabs open on
 * the same dashboard poll the SAME access token's exp claim, so they cross
 * the refresh-skew boundary within moments of each other — this is the
 * ordinary shape of an operator running the dashboard in two windows, not a
 * rare interleaving.
 *
 * The Web Locks API is the browser's own cross-tab mutex: `navigator.locks
 * .request` holds the named lock for exactly the lifetime of the callback
 * and releases it automatically on return OR throw, including when the tab
 * is killed mid-redemption — no polling, no manually-cleared flag, and no
 * abandoned lock that survives a crash. Where it is unavailable (older
 * browsers, or an environment with no `navigator.locks`), redemption falls
 * back to running directly — exactly the behavior every caller already had
 * before this function existed, so the absence of the API degrades to the
 * status quo rather than to a new failure mode.
 */
async function withRefreshLock<T>(fn: () => Promise<T>): Promise<T> {
  const locks = typeof navigator === 'undefined' ? undefined : navigator.locks;
  if (!locks) return fn();
  return locks.request(REFRESH_LOCK_NAME, fn);
}

/**
 * Read the server's own explanation out of a rejected renewal, falling back to
 * the status line. Both response shapes the platform emits are accepted: the
 * IAM/OAuth2 one (`error_description`) and the generic one (`message`).
 * Returns '' when the body says nothing useful, so the caller can supply its
 * own wording rather than showing the operator an empty quote.
 */
async function refreshFailureDetail(resp: Response): Promise<string> {
  try {
    const body = (await resp.json()) as {
      error_description?: string;
      message?: string;
      error?: string;
    };
    return body.error_description ?? body.message ?? body.error ?? '';
  } catch {
    return '';
  }
}

/**
 * Exchange the stored refresh token for a fresh access token via the public
 * renewal endpoint (REFRESH_PATH), persist it, and return it. Returns '' (and
 * fails the session closed) when no refresh token is stored or the server
 * rejects it. Concurrent callers share one in-flight request.
 *
 * Every close-the-session path records WHY in the store. The operator sees it
 * on the login page instead of being bounced there with no explanation.
 */
export async function refreshAccessToken(): Promise<string> {
  if (typeof window === 'undefined') return '';

  // Adopt what storage holds BEFORE acting on it. The refresh credential lives
  // in shared storage, so renewing without adopting first redeems whatever is
  // there — and if another tab has replaced the session, that is THEIR
  // credential. The rotated bearer would then be persisted as this generation's,
  // reconciliation would later find it matching, skip the bump, and the previous
  // login's caches would survive the replacement after all.
  //
  // This is the same reconciliation validateSession does, at the other door into
  // shared storage, asking the same single question: is the stored credential
  // the one this generation belongs to? Whatever it answers, what follows renews
  // the session actually in effect.
  reconcileSessionIdentity();

  // The session this renewal is FOR, snapshotted after adoption and before the
  // request. Everything committed below is committed for this session or not at
  // all.
  const forGeneration = generation;

  // Single-flight, scoped to the session. Concurrent callers renewing the SAME
  // session share one round trip, as before; a caller renewing a DIFFERENT one
  // does not, because that renewal's answer is not about its session. Sharing
  // across the boundary would hand a caller in the new session either the old
  // session's credential or the empty string — one request going out with the
  // wrong bearer, or with none, instead of renewing its own session.
  if (_refreshInFlight !== null && _refreshGeneration === forGeneration) {
    return _refreshInFlight;
  }

  const refreshToken = readRefreshToken();
  if (!refreshToken) {
    setUnauthenticated('Your session expired and there was no saved credential to renew it.');
    return '';
  }

  const renewalID = _nextRefreshID++;
  _refreshGeneration = forGeneration;
  _refreshID = renewalID;
  _refreshInFlight = (async (): Promise<string> => {
    const renewed = await performRenewal(forGeneration, refreshToken);
    // A credential handed back to a caller is handed back FOR a session, and
    // this is the one boundary every return inside performRenewal crosses. The
    // transient-failure path is why it has to be here rather than at the
    // returns: it answers with whatever is in storage, which after a transition
    // is the NEW session's bearer — and the SDK token factory would attach it
    // to a request the old session started. Guarding the boundary covers that
    // return and every one added later.
    //
    // The CREDENTIAL is the authoritative test, not the generation. `generation`
    // is an in-memory value that moves only once reconciliation has noticed a
    // storage write; a raw cross-tab replacement moves nothing until then. So in
    // exactly the window the paragraph above describes, the generation still
    // matches and would wave the replacement bearer through — the predicate
    // could not observe the thing its own comment named. Comparing storage
    // against the fingerprint this session owns detects the replacement
    // immediately, because it reads storage rather than waiting to be told.
    //
    // A successful renewal is not a replacement: commitRenewal wrote the new
    // credential through persistAccessToken, which updated the fingerprint with
    // it, so the two still agree.
    if (generation !== forGeneration) return '';
    if (credentialFingerprint(readAuthToken()) !== generationCredential) return '';
    return renewed;
  })().finally(() => {
    // Compare-and-clear. A renewal deregisters only the registration it made:
    // if another session's renewal has since taken the slot, this one finishing
    // says nothing about that one, and clearing would strand it — the next
    // caller would start a duplicate redemption of a refresh token the server
    // rotates, and lose the session to reuse detection.
    if (_refreshID !== renewalID) return;
    _refreshInFlight = null;
    _refreshGeneration = -1;
    _refreshID = 0;
  });

  return _refreshInFlight;
}

/**
 * Perform the renewal round trip for `forGeneration` and report the credential
 * it produced, or '' when it produced none. Commits nothing that is not scoped
 * to that session; the caller boundary above scopes what it RETURNS.
 */
async function performRenewal(
  forGeneration: number,
  refreshToken: string,
): Promise<string> {
  return withRefreshLock(async (): Promise<string> => {
    // Re-adopt storage now that the lock is held: a SIBLING TAB may have
    // redeemed and rotated this exact refresh token while this tab waited
    // its turn for the lock. Presenting `refreshToken` anyway would be a
    // SECOND redemption of an already-rotated credential — reuse, by the
    // server's own definition — which revokes the whole family, undoing the
    // sibling's successful renewal along with this one. Comparing against
    // the LIVE refresh token (not the one captured before the lock request)
    // is what lets this tab recognise "someone already did this" and adopt
    // the result instead of repeating the redemption.
    if (readRefreshToken() !== refreshToken) {
      // reconcileSessionIdentity is the existing answer to "did shared
      // storage move while I wasn't looking": it updates this tab's
      // generationCredential/generationSID to match what a sibling's
      // renewal just wrote (recognising it as the SAME session via the
      // sid claim, so the generation does not bump) or, for a genuinely
      // different session, moves the generation the normal way. Either
      // answer is correct here; this call is what lets the CALLER's own
      // generation/credential check (refreshAccessToken, just below where
      // this resolves) accept the adopted token on the first try instead of
      // waiting for the next poll to notice.
      reconcileSessionIdentity();
      return readAuthToken();
    }
    try {
      // ONLY the round trip is inside the transient-failure guard. Once the
      // server has answered, the rotation has already happened on its side and
      // the credential we presented is dead — a failure after that point is
      // NOT retryable and must not be reported as "your wifi blipped". See the
      // catch below.
      let resp: Response;
      try {
        resp = await fetch(`${window.location.origin}${REFRESH_PATH}`, {
          method: 'POST',
          headers: { 'Content-Type': 'application/json', Accept: 'application/json' },
          body: JSON.stringify({ refresh_token: refreshToken }),
        });
      } catch {
        // Network failure: the request may never have reached the server, so
        // the stored credential is probably still live. Do NOT clear the
        // session; the caller falls back to the existing token and the next
        // request retries.
        return readAuthToken();
      }
      if (!resp.ok) {
        // A rejected refresh token means the session is over — fail closed so
        // the MainApp route guard bounces the user to the login chooser.
        const detail = await refreshFailureDetail(resp);
        endSessionIfCurrent(
          forGeneration,
          detail
            ? `Your session could not be renewed: ${detail} (HTTP ${resp.status})`
            : `Your session could not be renewed (HTTP ${resp.status}).`,
        );
        return '';
      }
      const body = (await resp.json()) as {
        access_token?: string;
        refresh_token?: string;
      };
      const next = body.access_token ?? '';
      if (!next) {
        endSessionIfCurrent(
          forGeneration,
          'The server accepted the renewal but returned no access token.',
        );
        return '';
      }
      // Commits all three writes for the session this renewal began under, or
      // none of them if that session is gone. Returns '' in the latter case,
      // which the caller treats the same as any other renewal that produced no
      // usable token.
      return commitRenewal(forGeneration, next, body.refresh_token);
    } catch {
      // Reached only for a failure AFTER the server answered 2xx — a body that
      // will not parse, or storage that refuses the write. The rotation has
      // already happened server-side, so the credential in hand is dead and
      // there is nothing to retry with: end the session and say so.
      endSessionIfCurrent(
        forGeneration,
        'Your session was renewed but the new credentials could not be read.',
      );
      return '';
    }
  });
}

/**
 * Resolve the access token to attach to an outgoing request, refreshing it
 * pre-emptively when it is missing/expired or within the skew window. This is
 * the SDK token factory's source of truth (see sdkClient.ts): a long-lived tab
 * whose 15-minute access token expires mid-session transparently obtains a new
 * one from the refresh token instead of silently 401-ing.
 *
 * Storage is adopted BEFORE the token is read, for the same reason
 * refreshAccessToken does it: storage is shared with every sibling tab, and the
 * credential in it may be one this tab has not reconciled yet. A store snapshots
 * its scope before awaiting its SDK call, and this function runs inside that
 * call. Without the reconcile, a sibling's fresh login was attached as the
 * bearer while the scope still named this tab's old session, so the response
 * committed under a session that never issued it (go-quicbaas#369). With it,
 * the scope has already moved by the time the response lands, and the store's
 * own `scope !== currentScope()` check discards it. A sibling's renewal of the
 * same login (same sid) does not move the generation, so it costs nothing.
 *
 * This makes the reconcile at refreshAccessToken's entry redundant on this path
 * but not wrong: the second call finds a matching fingerprint and returns. It
 * stays, because refreshAccessToken must hold on its own for the callers that
 * reach it directly. validateSession's reconcile after
 * `await getValidAccessToken()` stays too: it catches a credential a sibling
 * wrote during that await.
 */
export async function getValidAccessToken(): Promise<string> {
  reconcileSessionIdentity();
  const token = readAuthToken();
  if (!token) return '';
  const remaining = secondsUntilExpiry(token);
  // Undecodable/opaque token, or comfortably valid: use it as-is.
  if (remaining === null || remaining > REFRESH_SKEW_SECONDS) return token;
  // Known to be expired or about to expire, and only worth a round-trip if we
  // actually hold a refresh token. refreshAccessToken returns the new token on
  // success, the still-valid existing token on a transient network failure, or
  // '' on a definitive rejection (after flipping the session to unauthenticated)
  // — return its result verbatim so a dead token is never attached.
  if (!readRefreshToken()) return token;
  return refreshAccessToken();
}

/**
 * Validate the persisted token against the server and set the status
 * accordingly. With no token we are unauthenticated; a server-rejected or
 * unreachable token also resolves to 'unauthenticated' so protected UI is
 * never shown without a confirmed session.
 */
export async function validateSession(): Promise<void> {
  // ONE question decides whether this function may write anything:
  //
  //     is the verdict still about the session it was made for?
  //
  // That is the session IDENTITY — sessionKey, the generation/uid/tenant triple
  // already used to scope cached data. Three narrower predicates were tried here
  // and each answered a different question wrongly:
  //
  //   • the generation alone — this function also MOVES the generation when it
  //     adopts a session from another tab, so it invalidated its own verdict and
  //     a cross-tab sign-out could not sign this tab out;
  //   • the token bytes — a refresh rotates the credential within one live
  //     session, so a verdict was discarded and the tab stranded on 'pending';
  //   • the mere presence of a credential — presence is equally true of a
  //     DIFFERENT session, so a verdict about session A committed for session B,
  //     leaving the shell protected on a credential nobody had validated.
  //
  // The identity is the thing all three were approximating. A refresh preserves
  // it, a replacement changes it, an ending changes it.
  //
  // Resolving the credential comes first, because it can renew the session or
  // fail it closed; then storage is adopted; then the credential and the identity
  // it belongs to are read TOGETHER, after every await that could move either.
  // Reading them at different moments is what let a stale token be validated
  // against a fresh identity.
  await getValidAccessToken();
  reconcileSessionIdentity();

  const token = readAuthToken();
  const validating = authStore.sessionKey;
  const commit = (next: AuthStatus, reason = ''): void => {
    // Adopt storage again before deciding. The entry reconcile above told us
    // which session we were validating; between then and now another tab can
    // have written a different principal's bearer, and nothing in memory moves
    // when it does — the storage event that would tell us arrives later. Without
    // this the verdict committed 'authenticated' while storage already held an
    // unvalidated bearer for someone else: requests would carry B's credential
    // while the caches stayed scoped to A.
    //
    // Not a second predicate. It is the SAME adoption already done at entry and
    // in refreshAccessToken, applied to the exit as well, and the one test below
    // does the deciding: a foreign credential moves the session key, so the
    // verdict is refused — and the caches are released at the same moment rather
    // than waiting for the storage event.
    reconcileSessionIdentity();
    // Compares the IDENTITY, and deliberately does not rely on the generation
    // alone being enough. It currently is: the generation moves on every
    // credential replacement and on every session end, which covers every way
    // uid or tenant can change. But that is a property of how reconciliation
    // and endSession happen to be written, not one anything enforces — and the
    // last time this comment described a narrower version of that same
    // relationship, writing it down was what exposed it as false (#326: a
    // second login by the same principal into the same tenant carries identical
    // claims, so a claims-based reconciliation saw no change at all).
    //
    // A predicate that names the property keeps guarding if that coupling is
    // ever broken; a proxy for it stops guarding silently at that moment, and
    // the failure looks exactly like the ones that took nine rounds to find
    // here. No reachable test distinguishes the two today, which is precisely
    // why it is written down rather than left to be rediscovered.
    if (authStore.sessionKey !== validating) return;
    // Through applyStatus, never a bare assignment: 'unauthenticated' here means
    // the session is over, and it has to end the way every other ending does.
    applyStatus(next, reason);
  };

  if (!token) {
    commit('unauthenticated');
    return;
  }
  try {
    const client = await getSDKClient();
    const ok = await client.auth.authValidate(token);
    commit(
      ok ? 'authenticated' : 'unauthenticated',
      ok ? '' : 'Your session is no longer valid. Please sign in again.',
    );
  } catch {
    // The server could not be asked, so the session cannot be shown as valid.
    // Failing closed here is deliberate: protected UI is never rendered on an
    // unconfirmed session.
    commit('unauthenticated', 'Your session could not be confirmed. Please sign in again.');
  }
}
