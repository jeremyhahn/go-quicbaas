// Package scope re-exports go-xkms's SSH scope helpers.
package scope

import xkmsscope "github.com/jeremyhahn/go-xkms/pkg/ssh/scope"

type Scope = xkmsscope.Scope

const Service = xkmsscope.Service
