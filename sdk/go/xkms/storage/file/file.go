// Package file re-exports go-xkms's file-storage backend.
package file

import xkmsfile "github.com/jeremyhahn/go-xkms/pkg/storage/file"

var New = xkmsfile.New
