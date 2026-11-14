package serviceplane

import (
	"errors"
	"fmt"
)

// Sentinels for errors.Is. Every error the package returns wraps one of them.
var (
	// ErrInvalidManifest marks a manifest that fails validation.
	ErrInvalidManifest = errors.New("serviceplane: invalid manifest")
	// ErrSignature marks a manifest whose signature does not verify.
	ErrSignature = errors.New("serviceplane: manifest signature does not verify")
	// ErrNotFound marks a service that is not deployed.
	ErrNotFound = errors.New("serviceplane: service not found")
	// ErrExists marks a deploy of a name that is already deployed.
	ErrExists = errors.New("serviceplane: service already deployed")
	// ErrConflict marks a write that lost the store's arbitration.
	ErrConflict = errors.New("serviceplane: concurrent write to the service")
	// ErrTransition marks a verb that is not legal in the service's state.
	ErrTransition = errors.New("serviceplane: verb not allowed in this state")
	// ErrGrant marks a grant or revoke that failed.
	ErrGrant = errors.New("serviceplane: grant failed")
	// ErrLaunch marks a launcher call that failed.
	ErrLaunch = errors.New("serviceplane: launcher failed")
	// ErrStore marks a store read or write that failed.
	ErrStore = errors.New("serviceplane: store unavailable")
	// ErrConfig marks a Plane or Store built without a required part.
	ErrConfig = errors.New("serviceplane: incomplete configuration")
	// ErrTenant marks a verified manifest whose tenant is not the one the
	// caller acts in, or a verb called with no tenant.
	ErrTenant = errors.New("serviceplane: manifest is for another tenant")
)

// ManifestError names the manifest field that failed validation.
type ManifestError struct {
	Field  string
	Reason string
}

func (e *ManifestError) Error() string {
	return fmt.Sprintf("serviceplane: invalid manifest: %s: %s", e.Field, e.Reason)
}

// Unwrap returns ErrInvalidManifest.
func (e *ManifestError) Unwrap() error { return ErrInvalidManifest }

// SignatureError says why a signed manifest was refused.
type SignatureError struct {
	KeyID  string
	Reason string
}

func (e *SignatureError) Error() string {
	return fmt.Sprintf("serviceplane: manifest signature by key %q refused: %s", e.KeyID, e.Reason)
}

// Unwrap returns ErrSignature.
func (e *SignatureError) Unwrap() error { return ErrSignature }

// TransitionError names the verb refused and the state that refused it.
type TransitionError struct {
	Verb  Verb
	State State
}

func (e *TransitionError) Error() string {
	return fmt.Sprintf("serviceplane: %s is not allowed while the service is %s", e.Verb, e.State)
}

// Unwrap returns ErrTransition.
func (e *TransitionError) Unwrap() error { return ErrTransition }

// ServiceError carries the tenant and name a verb acted on, the sentinel that
// classifies the failure and the cause below it.
type ServiceError struct {
	Op       string
	Tenant   string
	Name     string
	Sentinel error
	Cause    error
}

func (e *ServiceError) Error() string {
	msg := fmt.Sprintf("%s: %s %s/%s", e.Sentinel, e.Op, e.Tenant, e.Name)
	if e.Cause != nil {
		msg += ": " + e.Cause.Error()
	}
	return msg
}

// Unwrap returns the sentinel and the cause, so errors.Is matches either.
func (e *ServiceError) Unwrap() []error {
	if e.Cause == nil {
		return []error{e.Sentinel}
	}
	return []error{e.Sentinel, e.Cause}
}
