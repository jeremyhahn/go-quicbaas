// Package scope re-exports go-xkms's SSH scope helpers.
package scope

import xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"

type Scope = xkmssdk.SSHScope

const Service = xkmssdk.SSHScopeService
