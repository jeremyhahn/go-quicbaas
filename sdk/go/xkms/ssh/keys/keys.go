// Package keys re-exports go-xkms's SSH key handling.
package keys

import xkmskeys "github.com/jeremyhahn/go-xkms/pkg/ssh/keys"

type Key = xkmskeys.Key

var ParsePEM = xkmskeys.ParsePEM
