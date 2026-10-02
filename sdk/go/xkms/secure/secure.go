// Package secure re-exports go-xkms's protected-memory surface, so a
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
// so Wipe could be replaced with a no-op or WipeAll with one that returns nil,
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

	// NestedBorrowError is returned when a borrow is opened inside another
	// borrow of the same value.
	NestedBorrowError = xkmssecure.NestedBorrowError

	// EmptyBorrowError is returned when a borrow yields no bytes.
	EmptyBorrowError = xkmssecure.EmptyBorrowError
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

// WipeAll destroys every live locked-memory enclave the process holds --
// every outstanding Buffer and every plaintext a Secret.Open produced -- and
// the process sealer's key material, so every live Secret becomes
// unopenable. It returns how many enclaves were still held. It belongs to the
// binary that owns the process, deferred at startup so it runs on every
// orderly exit; library code never calls it. It replaces the memguard-era
// CatchInterrupt/EnablePurge/Purge lifecycle: the binary drives its own
// signal handling and graceful shutdown, which runs the deferred WipeAll.
func WipeAll() (int, error) { return xkmssecure.WipeAll() }
