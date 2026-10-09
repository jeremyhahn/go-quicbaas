# Coded sentinels: why `errors.Is` does not mean what it reads

> **Status: REFERENCE** · Last verified: 2026-09-14
> Behaviour of `pkg/errors.QBError` sentinels.

`pkg/errors.QBError` is a **type alias** for go-qrdb's `errors.QRDBError`, and
`QRDBError.Is` compares the error **code**:

```go
func (e *QRDBError) Is(target error) bool {
	var de *QRDBError
	if errors.As(target, &de) {
		return e.Code == de.Code // code, not identity
	}
	if e.Err != nil {
		return errors.Is(e.Err, target)
	}
	return false
}
```

Every `Err*` value in `pkg/iam` and `pkg/service` is a `*QBError`. So:

```go
errors.Is(err, iam.ErrInviteExpired) // true for ANY error carrying Forbidden
```

That includes `ErrInviteUsed`, `ErrPIVCertRejected`, `ErrTenantSuspended`, and
any forbidden bubbling up from the store. The call site reads like idiomatic
sentinel matching and is not: it **over-matches**, so it takes a branch written
for a narrower case.

The over-match is only possible in one direction. `errors.Is(err, target)` calls
`err.Is(target)`, never the reverse, so the mechanism fires when the **error**
side is (or wraps) a `QBError`. A `QBError` compared against an `errors.New`
sentinel still falls through to a correct identity comparison on the chain.

## Which question is this site asking?

Two answers, and the point is to make the choice deliberately.

**The class.** A REST bridge answering 404 for any not-found, a loop skipping any
record the store cannot produce, an idempotent delete. Ask the class outright —
`dberrors.IsNotFound(err)`, or `errors.As` on `*QBError` plus a `Code`
comparison. If `errors.Is` reads better in place, mark it:

```go
// coded-sentinel-ok: the CLASS is the intent. This bridge answers 404 for any
// not-found the audit lookup raises.
if errors.Is(err, iam.ErrAuditEventNotFound) {
```

**The condition.** The branch means one specific thing. Give the sentinel a
dedicated typed inner error and match it with `errors.As`:

```go
// pkg/iam/errors_conditions.go
type UserNotFoundError struct{}

func (e *UserNotFoundError) Error() string { return "user not found" }

func IsUserNotFound(err error) bool {
	var target *UserNotFoundError
	return errors.As(err, &target)
}

// pkg/iam/errors.go
ErrUserNotFound = &dberrors.QBError{
	Code: dberrors.ErrNotFound,
	Op:   "iam.user",
	Err:  &UserNotFoundError{},
}
```

The code is unchanged, so every transport keeps mapping the sentinel to the same
status. Only the *classification* narrows.

Two constraints on doing this:

- **Every producer must return the sentinel.** `NewIAMError(ErrUserNotFound, …)`
  copies the code and replaces the inner error, so a caller constructing one that
  way will not be recognised by the predicate. `pkg/iam/storage_memory.go` was
  answering absent users with a bare `QBError{ErrNotFound}` for this reason and
  now returns `ErrUserNotFound`, matching the qrdb-backed store.
- **Test positively.** Assert the narrow predicate accepts its own condition and
  refuses a *different* error carrying the *same code*. Written that way, a
  failure mode added later lands in the caller's default bucket rather than
  silently joining the matched set. See
  `TestErrorsIsCannotDistinguishIAMSentinels`.

## The guard

`scripts/codedsentinel` fails the build on any `errors.Is` in `pkg/`, `cmd/` or
`sdk/` whose target is a qrdb-coded sentinel and which carries no
`coded-sentinel-ok:` reason. It runs from `make check-coded-sentinels`, which
`make test` depends on.

Resolution is syntactic — no build tags, no module loading, no dependency — so it
runs on a tree that does not compile. A sentinel is a package-level `var` whose
initializer is a composite literal of a type named `QBError` or `QRDBError`, or a
`var` assigned from one. Targets are matched as `Name` or `pkg.Name`, keyed by
package **clause** name, which errs toward reporting.

It scans non-test files. A test asserting `ErrorIs` against a coded sentinel is a
weaker assertion than it looks, but it is an assertion, not a branch: it cannot
mis-route a request or admit a token. `-include-tests` reports them for an audit.

## Origin

go-quicbaas#398 / #411 (MFA verification), then the repo-wide audit in
go-quicbaas#434, which found three consequential instances: a 422 branch on the
PIV activation route that could never run, an authentication route that described
why a proof failed, and a token-liveness rule that read a store's not-found as
"this subject is a service principal" and kept the token live.
