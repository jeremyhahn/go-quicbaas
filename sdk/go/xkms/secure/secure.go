// Package secure re-exports go-xkms's memguard-protected memory surface, so a
// consumer above Layer 3 can hold keys, passwords and tokens in protected
// storage without importing go-xkms, go-qrdb or raw memguard — all three of
// which the layered protected-memory rule forbids it.
//
// Types are direct aliases of github.com/jeremyhahn/go-xkms/sdk/go/secure,
// which is itself an alias of go-qrdb/sdk/go/secure. Aliases preserve type and
// pointer identity across the module boundary, so errors.As and errors.Is keep
// working and a value constructed here is the same value the lower layers see.
//
// The operations are thin forwarding declarations rather than function-valued
// variables, mirroring the upstream package. That is deliberate for a
// protected-memory surface: a package-level var is assignable by any importer,
// so Wipe could be replaced with a no-op or Purge with one that returns nil,
// silently defeating the guarantees these names exist to provide.
//
// The two containers are not interchangeable, and the distinction is the whole
// point of exporting both:
//
//   - Secret is a long-lived, enclaved container. A credential read at startup
//     and used until shutdown belongs here.
//   - Buffer is a short-lived, mlocked, canary-guarded working copy. Always
//     defer Destroy() after construction.
package secure

import (
	"context"

	xkmssecure "github.com/jeremyhahn/go-xkms/sdk/go/secure"
)

// =============================================================================
// Containers
// =============================================================================

type (
	// Secret is a long-lived, enclaved container for sensitive material.
	Secret = xkmssecure.Secret

	// Buffer is a short-lived, mlocked, canary-guarded working copy. Always
	// defer Destroy() after construction.
	Buffer = xkmssecure.Buffer
)

// =============================================================================
// Passphrase providers
// =============================================================================

type (
	// PassphraseProvider reads a passphrase directly into a Buffer.
	PassphraseProvider = xkmssecure.PassphraseProvider

	// TerminalProvider reads from /dev/tty with echo disabled.
	TerminalProvider = xkmssecure.TerminalProvider

	// StdinProvider reads from stdin until newline (CI / piped invocations).
	StdinProvider = xkmssecure.StdinProvider

	// CallbackProvider wraps a user-supplied function returning passphrase
	// bytes.
	CallbackProvider = xkmssecure.CallbackProvider
)

// =============================================================================
// Errors
// =============================================================================

type (
	// NilSecretError is returned when Open is called on a nil or destroyed
	// Secret.
	NilSecretError = xkmssecure.NilSecretError

	// NilBufferError is returned when a Buffer method is called on a nil or
	// destroyed Buffer.
	NilBufferError = xkmssecure.NilBufferError

	// OpenError wraps an underlying memguard failure to unseal.
	OpenError = xkmssecure.OpenError

	// InvalidSizeError is returned when a constructor receives a non-positive
	// size.
	InvalidSizeError = xkmssecure.InvalidSizeError

	// ProviderError is returned by PassphraseProvider implementations on I/O
	// failure.
	ProviderError = xkmssecure.ProviderError

	// NotInitializedError is returned by Purge when the subsystem has not been
	// armed by CatchInterrupt or EnablePurge.
	NotInitializedError = xkmssecure.NotInitializedError
)

// =============================================================================
// Constructors
// =============================================================================

// NewSecret seals src into a new Secret. On return src is wiped — the caller
// must not reuse the slice.
func NewSecret(src []byte) (*Secret, error) { return xkmssecure.NewSecret(src) }

// NewSecretFromBuffer seals buf into a new Secret, consuming buf.
func NewSecretFromBuffer(buf *Buffer) (*Secret, error) {
	return xkmssecure.NewSecretFromBuffer(buf)
}

// NewBuffer allocates a zero-filled mlocked Buffer of the given size.
func NewBuffer(size int) (*Buffer, error) { return xkmssecure.NewBuffer(size) }

// NewBufferFromBytes copies src into an mlocked Buffer. On return src is wiped.
func NewBufferFromBytes(src []byte) (*Buffer, error) {
	return xkmssecure.NewBufferFromBytes(src)
}

// Wipe clears a caller-owned []byte using memguard's
// compiler-optimization-resistant wipe. No-op on empty slices.
func Wipe(b []byte) { xkmssecure.Wipe(b) }

// NewTerminalProvider returns a TerminalProvider reading from /dev/tty.
func NewTerminalProvider() *TerminalProvider { return xkmssecure.NewTerminalProvider() }

// NewStdinProvider returns a StdinProvider reading from os.Stdin.
func NewStdinProvider() *StdinProvider { return xkmssecure.NewStdinProvider() }

// NewCallbackProvider returns a CallbackProvider that invokes fn to obtain
// passphrase bytes.
func NewCallbackProvider(fn func(ctx context.Context, prompt string) ([]byte, error)) *CallbackProvider {
	return xkmssecure.NewCallbackProvider(fn)
}

// =============================================================================
// Process-wide lifecycle
// =============================================================================

// CatchInterrupt wires SIGINT/SIGTERM to purge all mlocked memory before
// process exit. Call once, early in main().
//
// The installed handler hard os.Exit(1)s from inside the signal handler, so a
// daemon that drives its own graceful shutdown must use EnablePurge instead:
// CatchInterrupt's exit always wins the race and the shutdown path never runs.
func CatchInterrupt() { xkmssecure.CatchInterrupt() }

// EnablePurge arms the subsystem so Purge works, WITHOUT installing memguard's
// process signal handler. Use it in daemons that own their own signal
// handling, paired with a deferred Purge in main().
func EnablePurge() { xkmssecure.EnablePurge() }

// Purge performs an immediate, synchronous wipe of all mlocked memory. Returns
// NotInitializedError if the subsystem was never armed.
func Purge() error { return xkmssecure.Purge() }

// PurgeOrPanic is a convenience for deferred cleanup in main(). Panics if the
// subsystem was never armed.
func PurgeOrPanic() { xkmssecure.PurgeOrPanic() }
