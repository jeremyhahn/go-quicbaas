// Package session re-exports go-xkms's SSH session abstraction.
package session

import xkmssession "github.com/jeremyhahn/go-xkms/pkg/ssh/session"

type (
	Session     = xkmssession.Session
	DialRequest = xkmssession.DialRequest
)

var Dial = xkmssession.Dial
