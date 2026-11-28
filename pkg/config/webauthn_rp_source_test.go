package config

import (
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestValidateWebAuthnRPIDSource covers issue #2231: a node reached by IP
// address (server.host is a routable IP literal) needs an explicit
// iam.webauthn.rp_id, because the default RP ID is the server host and a
// browser refuses an IP address as an RP ID.
func TestValidateWebAuthnRPIDSource(t *testing.T) {
	tests := []struct {
		name    string
		host    string
		rpID    string
		wantErr bool
	}{
		{name: "LAN IPv4 host, no rp_id", host: "192.168.1.50", wantErr: true},
		{name: "IPv6 host, no rp_id", host: "fd00::50", wantErr: true},
		{name: "bracketed IPv6 host, no rp_id", host: "[fd00::50]", wantErr: true},
		{name: "LAN IPv4 host, explicit rp_id", host: "192.168.1.50", rpID: "pilot.example.com"},
		{name: "IPv6 host, explicit rp_id", host: "fd00::50", rpID: "pilot.example.com"},
		{name: "DNS host, no rp_id", host: "pilot.example.com"},
		{name: "loopback IPv4 maps to localhost", host: "127.0.0.1"},
		{name: "loopback IPv6 maps to localhost", host: "::1"},
		{name: "IPv4 wildcard maps to localhost", host: "0.0.0.0"},
		{name: "IPv6 wildcard maps to localhost", host: "::"},
		{name: "empty host maps to localhost", host: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := Default()
			cfg.Server.Host = tt.host
			cfg.IAM.WebAuthn.RPID = tt.rpID

			v := NewValidator()
			v.validateWebAuthnRPIDSource(cfg)
			if !tt.wantErr {
				assert.Empty(t, v.errors)
				return
			}
			require.Len(t, v.errors, 1)
			assert.Equal(t, "iam.webauthn.rp_id", v.errors[0].Field)
			assert.Contains(t, v.errors[0].Message, tt.host)
			assert.Contains(t, v.errors[0].Message, "Set iam.webauthn.rp_id")
		})
	}
}

// TestValidate_IPHostWithoutRPIDFailsAtLoad proves the rule is wired into
// the load-time validator: the whole configuration is refused, and the
// message names the setting to change.
func TestValidate_IPHostWithoutRPIDFailsAtLoad(t *testing.T) {
	cfg := Default()
	cfg.Server.Host = "10.0.0.7"
	cfg.IAM.WebAuthn.RPID = ""

	err := Validate(cfg)
	require.Error(t, err)
	var verrs ValidationErrors
	require.True(t, errors.As(err, &verrs), "want ValidationErrors, got %T", err)
	var found bool
	for _, e := range verrs {
		if e.Field == "iam.webauthn.rp_id" && strings.Contains(e.Message, "10.0.0.7") {
			found = true
		}
	}
	assert.True(t, found, "no iam.webauthn.rp_id error in %v", err)
}

func TestWebAuthnIssuerHost(t *testing.T) {
	for host, want := range map[string]string{
		"":                  "localhost",
		"0.0.0.0":           "localhost",
		"::":                "localhost",
		"127.0.0.1":         "localhost",
		"::1":               "localhost",
		"localhost":         "localhost",
		"192.168.1.50":      "192.168.1.50",
		"pilot.example.com": "pilot.example.com",
	} {
		assert.Equal(t, want, WebAuthnIssuerHost(host), "host %q", host)
	}
}
