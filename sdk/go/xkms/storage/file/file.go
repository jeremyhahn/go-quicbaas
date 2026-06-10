// Package file re-exports go-xkms's file-storage backend.
package file

import xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"

var New = xkmssdk.NewFileStorageBackend
