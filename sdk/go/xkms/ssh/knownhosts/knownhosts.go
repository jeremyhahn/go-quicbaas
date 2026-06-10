// Package knownhosts re-exports go-xkms's known_hosts store.
package knownhosts

import xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"

type Store = xkmssdk.SSHKnownHostsStore

var NewBackendStore = xkmssdk.NewSSHKnownHostsBackendStore
