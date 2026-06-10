// Package session re-exports go-xkms's SSH session abstraction.
package session

import xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"

type (
	Session     = xkmssdk.SSHSession
	DialRequest = xkmssdk.SSHSessionDialRequest
)

var Dial = xkmssdk.SSHSessionDial
