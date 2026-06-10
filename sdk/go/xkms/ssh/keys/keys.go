// Package keys re-exports go-xkms's SSH key handling.
package keys

import xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"

type Key = xkmssdk.SSHKey

var ParsePEM = xkmssdk.SSHParsePEM
