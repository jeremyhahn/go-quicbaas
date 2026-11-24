package kms

import "strings"

const (
	// xkmsTenantKeyPrefix is the prefix of a tenant-scoped service key ID,
	// "tenant/<tenant>/<key>". It must equal pkg/service kmsTenantKeyPrefix,
	// which builds those IDs.
	xkmsTenantKeyPrefix = "tenant/"

	// xkmsTenantNamePrefix is the prefix of the backend name a tenant-scoped
	// key gets in go-xkms, "tenant.<tenant>.<key>". go-xkms refuses "/" in a
	// key name, so the service ID cannot be handed over unchanged.
	xkmsTenantNamePrefix = "tenant."
)

// xkmsKeyName maps a service key ID to the name go-xkms stores it under.
// "tenant/<t>/<rest>" (both parts non-empty) becomes "tenant.<t>.<rest>";
// any other ID, including every system key, is returned unchanged.
func xkmsKeyName(keyID string) string {
	rest, ok := strings.CutPrefix(keyID, xkmsTenantKeyPrefix)
	if !ok {
		return keyID
	}
	tenant, key, ok := strings.Cut(rest, "/")
	if !ok || tenant == "" || key == "" {
		return keyID
	}
	return xkmsTenantNamePrefix + tenant + "." + key
}

// serviceKeyID is the inverse of xkmsKeyName. A tenant ID never contains
// ".", so splitting at the first "." after the prefix is exact.
func serviceKeyID(name string) string {
	rest, ok := strings.CutPrefix(name, xkmsTenantNamePrefix)
	if !ok {
		return name
	}
	tenant, key, ok := strings.Cut(rest, ".")
	if !ok || tenant == "" || key == "" {
		return name
	}
	return xkmsTenantKeyPrefix + tenant + "/" + key
}

// checkUntenantedKeyName refuses a key ID that is not tenant-scoped but
// starts with the backend's tenant name prefix: it would share a backend
// name with a tenant's key.
func checkUntenantedKeyName(keyID string) error {
	if !strings.HasPrefix(keyID, xkmsTenantKeyPrefix) && strings.HasPrefix(keyID, xkmsTenantNamePrefix) {
		return ErrReservedKeyName
	}
	return nil
}
