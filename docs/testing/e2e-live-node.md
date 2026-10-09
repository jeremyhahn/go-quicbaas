# Running the Playwright E2E Suite Against a Live Node

> **Status: REFERENCE** · Last verified: 2026-09-14
> Running the Playwright E2E suite with `make test-e2e`.

`make test-e2e` builds a daemon, boots it on a scratch data dir, and signs the
browser in as the first-boot **system admin** by walking the one-time invite it
prints. That is the right shape for CI and the wrong shape for a node that is
already up: the invite has long since been consumed, and the interesting
principal on a running node is a **tenant user**, not the system admin.

`make test-e2e-live` runs the same suite against an already-running node, signed
in as a user who already exists there. It builds nothing and starts nothing.

## Usage

```sh
export QUICBAAS_UI_URL=https://localhost:18443/ui/
export QUICBAAS_E2E_TENANT=automatethethings
export QUICBAAS_E2E_USER_EMAIL=factory-ui@automatethethings.com
export QUICBAAS_E2E_ADMIN_CERT=/path/to/admin.crt
export QUICBAAS_E2E_ADMIN_KEY=/path/to/admin.key
export QUICBAAS_E2E_CA=/path/to/ca-chain.crt

make test-e2e-live                                        # whole suite
make test-e2e-live E2E_SPEC=test/e2e/projects.spec.ts     # one spec
```

All six variables are required. A partial configuration is refused rather than
quietly falling back to the first-boot path, which would run the suite against
the wrong node as the wrong principal.

| Variable | Meaning |
|---|---|
| `QUICBAAS_UI_URL` | `https://<host>:<port>/ui/` of the running node |
| `QUICBAAS_E2E_TENANT` | tenant the user belongs to |
| `QUICBAAS_E2E_USER_EMAIL` | the existing user to sign in as |
| `QUICBAAS_E2E_ADMIN_CERT` | admin client certificate, authorized for `user_authfactors:add` |
| `QUICBAAS_E2E_ADMIN_KEY` | its private key |
| `QUICBAAS_E2E_CA` | CA the node's serving certificate chains to |

## What the setup does

`test/e2e/global-setup.ts` selects the live path when `QUICBAAS_E2E_TENANT` and
`QUICBAAS_E2E_USER_EMAIL` are set, and shares everything else with the first-boot
path — one virtual authenticator, one sign-in ceremony, one set of persisted
artifacts.

1. **Trust the CA.** The node's certificate chains to a private CA, and the suite
   answers that by trusting the CA, never by disabling verification. Node reads
   it directly (`fixtures/live_tenant.ts`); Chromium reads it from an NSS
   database built under `test/e2e/.auth/chromium-home` and handed to the browser
   as its `HOME` (`fixtures/browser.ts`). The host's own `~/.pki` is untouched.
   `certutil` (libnss3-tools) must be installed.

2. **Enrol a passkey, if the browser has none the node accepts.** A virtual
   authenticator starts empty on every launch, so the credential it holds is
   saved to `test/e2e/.auth/live-credential.json` and restored on the next run;
   enrolment happens only when the node no longer offers that credential — which
   is what a node whose credential store is in memory does after a restart.

   Enrolment itself needs `user_authfactors:add`, which no unauthenticated page
   holds, so it is driven from node with the admin client certificate:

   ```
   POST /api/v1/iam/{tenant}/webauthn/registration/begin   (mTLS)  → X-Session-Id
        navigator.credentials.create() in the page
   POST /api/v1/iam/{tenant}/webauthn/registration/finish  (mTLS, same X-Session-Id)
   ```

3. **Sign in** through the ordinary unified ceremony the SPA uses —
   `POST /api/v1/auth/login/{begin,finish}`, type `fido2` — with the tenant typed
   into the Organization field. There is no second login flow.

4. **Persist** the browser storageState and the raw tokens, exactly as the
   first-boot path does, so every spec and every REST fixture starts as that
   user.

## The refresh credential is deliberately not in storageState

The server issues a single-use, rotating refresh token. A storageState file is
replayed into a fresh browser context for *every test in the run*, so all of
them would hold the same copy: the first context to renew spends it, and every
context afterwards presents a revoked credential and is signed out mid-test —
one spec poisoning all the others for reasons unconnected to what it tested.

Global-setup therefore strips the refresh credential from the storageState it
writes. Nothing renews and nothing can be poisoned: the SPA declines to retry a
401 when it holds no refresh token (`stores/authFetch.ts` `isRenewable`) and
leaves the session alone. Every context runs on the access token minted at setup,
which bounds a run by that token's lifetime (15 minutes by default).

The raw pair still goes to `test/e2e/.auth/admin-token.json`, where a single
worker process owns it and its rotations stay coherent
(`test/e2e/fixtures/_http.ts`).

## Artifacts

Everything the run persists lives under `cmd/quicbaas/frontend/test/e2e/.auth/`,
which is git-ignored:

| File | Contents |
|---|---|
| `admin.json` | Playwright storageState — access token, no refresh credential |
| `admin-token.json` | raw access + refresh tokens for the REST fixtures |
| `live-credential.json` | the virtual authenticator's passkey, reused across runs |
| `chromium-home/.pki/nssdb` | the CA trust store handed to Chromium |
