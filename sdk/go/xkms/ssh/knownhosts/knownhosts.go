// Package knownhosts re-exports go-xkms's known_hosts store.
package knownhosts

import xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"

type Store = xkmssdk.SSHKnownHostsStore

var NewBackendStore = xkmssdk.NewSSHKnownHostsBackendStore

// Pin is a single pinned host-key envelope.
type Pin = xkmssdk.SSHKnownHostsPin

// PinFromPublicKey builds a Pin from a host public key. Persist it via
// Store.Add.
var PinFromPublicKey = xkmssdk.SSHPinFromPublicKey

// ImportKnownHostsFile imports an OpenSSH known_hosts file into a Store.
var ImportKnownHostsFile = xkmssdk.SSHImportKnownHostsFile

// ValidateFingerprint returns nil for a well-formed OpenSSH "SHA256:<base64>"
// host-key fingerprint and *InvalidFingerprintError otherwise.
var ValidateFingerprint = xkmssdk.SSHValidateFingerprint

// InvalidFingerprintError reports a malformed fingerprint string.
type InvalidFingerprintError = xkmssdk.SSHInvalidFingerprintError
