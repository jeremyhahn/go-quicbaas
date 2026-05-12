// Package knownhosts re-exports go-xkms's known_hosts store.
package knownhosts

import xkmskh "github.com/jeremyhahn/go-xkms/pkg/ssh/known_hosts"

type Store = xkmskh.Store

var NewBackendStore = xkmskh.NewBackendStore
