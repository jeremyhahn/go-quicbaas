// Package secure re-exports go-xkms's memguard-protected buffers.
package secure

import xkmssecure "github.com/jeremyhahn/go-xkms/sdk/go/secure"

type Buffer = xkmssecure.Buffer

var NewBufferFromBytes = xkmssecure.NewBufferFromBytes
