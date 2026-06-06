// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC

package iam

import (
	"encoding/json"
	"testing"
)

func TestFlexInt_MarshalJSON(t *testing.T) {
	f := FlexInt(42)
	data, err := json.Marshal(f)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(data) != "42" {
		t.Errorf("got %q, want 42", string(data))
	}
}

func TestFlexInt_UnmarshalJSON(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    int
		wantErr bool
	}{
		{"int", `5`, 5, false},
		{"string int", `"7"`, 7, false},
		{"string float", `"3.0"`, 3, false},
		{"float", `4.0`, 4, false},
		{"unparseable string", `"abc"`, 0, true},
		{"object invalid", `{}`, 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var f FlexInt
			err := json.Unmarshal([]byte(tt.input), &f)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error for %q", tt.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if f.Int() != tt.want {
				t.Errorf("got %d, want %d", f.Int(), tt.want)
			}
		})
	}
}

func TestParseGrantType(t *testing.T) {
	cases := map[string]GrantType{
		"authorization_code": GrantTypeAuthorizationCode,
		"client_credentials": GrantTypeClientCredentials,
		"refresh_token":      GrantTypeRefreshToken,
		"urn:ietf:params:oauth:grant-type:device_code": GrantTypeDeviceCode,
		"implicit":      GrantTypeImplicit,
		"password":      GrantTypePassword,
		"unknown_value": GrantTypeAuthorizationCode,
	}
	for in, want := range cases {
		if got := ParseGrantType(in); got != want {
			t.Errorf("ParseGrantType(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestGrantType_MarshalUnmarshalJSON(t *testing.T) {
	g := GrantTypeClientCredentials
	data, err := json.Marshal(g)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(data) != `"client_credentials"` {
		t.Errorf("got %s", string(data))
	}

	var fromStr GrantType
	if err := json.Unmarshal([]byte(`"refresh_token"`), &fromStr); err != nil {
		t.Fatalf("unmarshal string: %v", err)
	}
	if fromStr != GrantTypeRefreshToken {
		t.Errorf("got %v from string", fromStr)
	}

	var fromNum GrantType
	if err := json.Unmarshal([]byte(`2`), &fromNum); err != nil {
		t.Fatalf("unmarshal number: %v", err)
	}
	if fromNum != GrantTypeRefreshToken {
		t.Errorf("got %v from number", fromNum)
	}

	var bad GrantType
	if err := json.Unmarshal([]byte(`{}`), &bad); err == nil {
		t.Error("expected error for object value")
	}
}

func TestTokenEndpointAuthMethod_MarshalUnmarshalJSON(t *testing.T) {
	m := TokenEndpointAuthClientSecretPost
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(data) != `"client_secret_post"` {
		t.Errorf("got %s", string(data))
	}

	strCases := map[string]TokenEndpointAuthMethod{
		"client_secret_basic":         TokenEndpointAuthClientSecretBasic,
		"client_secret_post":          TokenEndpointAuthClientSecretPost,
		"client_secret_jwt":           TokenEndpointAuthClientSecretJWT,
		"private_key_jwt":             TokenEndpointAuthPrivateKeyJWT,
		"none":                        TokenEndpointAuthNone,
		"tls_client_auth":             TokenEndpointAuthTLSClientAuth,
		"self_signed_tls_client_auth": TokenEndpointAuthSelfSignedTLSClientAuth,
		"garbage":                     TokenEndpointAuthClientSecretBasic,
	}
	for in, want := range strCases {
		var got TokenEndpointAuthMethod
		if err := json.Unmarshal([]byte(`"`+in+`"`), &got); err != nil {
			t.Fatalf("unmarshal %q: %v", in, err)
		}
		if got != want {
			t.Errorf("unmarshal %q = %v, want %v", in, got, want)
		}
	}

	var fromNum TokenEndpointAuthMethod
	if err := json.Unmarshal([]byte(`3`), &fromNum); err != nil {
		t.Fatalf("unmarshal number: %v", err)
	}
	if fromNum != TokenEndpointAuthPrivateKeyJWT {
		t.Errorf("got %v from number", fromNum)
	}

	var bad TokenEndpointAuthMethod
	if err := json.Unmarshal([]byte(`{}`), &bad); err == nil {
		t.Error("expected error for object value")
	}
}

func TestApplicationType_MarshalUnmarshalJSON(t *testing.T) {
	data, err := json.Marshal(ApplicationTypeSPA)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(data) != `"spa"` {
		t.Errorf("got %s", string(data))
	}

	strCases := map[string]ApplicationType{
		"web":     ApplicationTypeWeb,
		"native":  ApplicationTypeNative,
		"spa":     ApplicationTypeSPA,
		"service": ApplicationTypeService,
	}
	for in, want := range strCases {
		var got ApplicationType
		if err := json.Unmarshal([]byte(`"`+in+`"`), &got); err != nil {
			t.Fatalf("unmarshal %q: %v", in, err)
		}
		if got != want {
			t.Errorf("unmarshal %q = %v, want %v", in, got, want)
		}
	}

	var fromNum ApplicationType
	if err := json.Unmarshal([]byte(`1`), &fromNum); err != nil {
		t.Fatalf("unmarshal number: %v", err)
	}
	if fromNum != ApplicationTypeNative {
		t.Errorf("got %v from number", fromNum)
	}

	var bad ApplicationType
	if err := json.Unmarshal([]byte(`{}`), &bad); err == nil {
		t.Error("expected error for object value")
	}
}

func TestApplicationType_StringUnknown(t *testing.T) {
	if got := ApplicationType(99).String(); got != "unknown" {
		t.Errorf("got %q", got)
	}
}

func TestConvertMapResponse(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		in := map[string]interface{}{"id": "t1", "name": "acme"}
		out, err := convertMapResponse[Tenant](in, "ctx")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if out.ID != "t1" || out.Name != "acme" {
			t.Errorf("got %+v", out)
		}
	})

	t.Run("unmarshal error", func(t *testing.T) {
		// A string where a struct is expected fails json.Unmarshal into Tenant.
		_, err := convertMapResponse[Tenant]("not-a-struct", "ctx")
		if err == nil {
			t.Error("expected error converting string to struct")
		}
	})
}
