package serviceplane

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"strings"
	"testing"
)

func TestManifest_Validate(t *testing.T) {
	cases := []struct {
		field string
		edit  func(*Manifest)
	}{
		{"tenant", func(m *Manifest) { m.Tenant = " " }},
		{"name", func(m *Manifest) { m.Name = "Web_1" }},
		{"image.reference", func(m *Manifest) { m.Image.Reference = "" }},
		{"image.provenance", func(m *Manifest) { m.Image.Provenance = "" }},
		{"image.digest", func(m *Manifest) { m.Image.Digest = "sha256:abc" }},
		{"image.digest", func(m *Manifest) { m.Image.Digest = "md5:" + strings.Repeat("a", 32) }},
		{"image.digest", func(m *Manifest) { m.Image.Digest = "sha256:" + strings.Repeat("AB", 32) }},
		{"policy", func(m *Manifest) { m.Policy.Network = "" }},
		{"resources.milli_cpu", func(m *Manifest) { m.Resources.MilliCPU = 0 }},
		{"resources.memory_bytes", func(m *Manifest) { m.Resources.MemoryBytes = -1 }},
		{"role", func(m *Manifest) { m.Role = "superuser" }},
		{"role", func(m *Manifest) { m.Role = "so" }},
		{"volumes.name", func(m *Manifest) { m.Volumes = append(m.Volumes, m.Volumes[0]) }},
		{"volumes.mount_path", func(m *Manifest) { m.Volumes[0].MountPath = "data" }},
		{"volumes.size_bytes", func(m *Manifest) { m.Volumes[0].SizeBytes = 0 }},
		{"ports.protocol", func(m *Manifest) { m.Ports[0].Protocol = "sctp" }},
		{"ports.port", func(m *Manifest) { m.Ports[0].Port = 0 }},
		{"ports.port", func(m *Manifest) { m.Ports = append(m.Ports, Port{Name: "alt", Port: 8443, Protocol: "tcp"}) }},
	}
	if err := validManifest().Validate(); err != nil {
		t.Fatalf("valid manifest refused: %v", err)
	}
	for _, c := range cases {
		m := validManifest()
		c.edit(m)
		err := m.Validate()
		wantErr(t, err, ErrInvalidManifest)
		if !strings.Contains(err.Error(), c.field+":") {
			t.Errorf("%s: error %q does not name the field", c.field, err)
		}
	}
}

func TestSign_VerifiesForEachKeyType(t *testing.T) {
	_, ed, _ := ed25519.GenerateKey(rand.Reader)
	p256, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	p384, _ := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	for name, key := range map[string]crypto.Signer{"ed25519": ed, "p256": p256, "p384": p384} {
		sm, err := Sign(validManifest(), name, key)
		if err != nil {
			t.Fatalf("%s: sign: %v", name, err)
		}
		m, err := Keyring{name: key.Public()}.Verify(sm)
		if err != nil {
			t.Fatalf("%s: verify: %v", name, err)
		}
		if m.Name != "web" || m.Placement.NodeSelector["zone"] != "a" {
			t.Fatalf("%s: verified manifest = %+v", name, m)
		}
	}
}

func TestSign_RefusesWhatItCannotSign(t *testing.T) {
	_, ed, _ := ed25519.GenerateKey(rand.Reader)
	rsaKey, _ := rsa.GenerateKey(rand.Reader, 2048)
	if _, err := Sign(validManifest(), "r", rsaKey); err == nil {
		t.Fatal("an RSA key signed a manifest")
	}
	bad := validManifest()
	bad.Role = "so"
	if _, err := Sign(bad, "k", ed); err == nil {
		t.Fatal("an invalid manifest was signed")
	}
	if _, err := Sign(validManifest(), "", ed); err == nil {
		t.Fatal("a manifest was signed without a key id")
	}
}

func TestKeyring_RefusesTamperedUnknownAndWrongKey(t *testing.T) {
	pub, key, _ := ed25519.GenerateKey(rand.Reader)
	other, _, _ := ed25519.GenerateKey(rand.Reader)
	sm, err := Sign(validManifest(), "k1", key)
	if err != nil {
		t.Fatal(err)
	}
	tampered := *sm
	tampered.Payload = []byte(strings.Replace(string(sm.Payload), `"operator"`, `"admin"`, 1))
	cases := map[string]struct {
		ring Keyring
		sm   *SignedManifest
	}{
		"tampered":  {Keyring{"k1": pub}, &tampered},
		"unknown":   {Keyring{"k2": pub}, sm},
		"wrong key": {Keyring{"k1": other}, sm},
		"nil":       {Keyring{"k1": pub}, nil},
	}
	for name, c := range cases {
		if _, err := c.ring.Verify(c.sm); err == nil {
			t.Errorf("%s: verified", name)
		} else {
			wantErr(t, err, ErrSignature)
		}
	}
}

func TestKeyring_RefusesAnUnknownField(t *testing.T) {
	pub, key, _ := ed25519.GenerateKey(rand.Reader)
	payload := []byte(`{"tenant":"acme","name":"web","privileged":true}`)
	sig := ed25519.Sign(key, append([]byte(manifestDomain), payload...))
	_, err := Keyring{"k1": pub}.Verify(&SignedManifest{Payload: payload, KeyID: "k1", Signature: sig})
	wantErr(t, err, ErrInvalidManifest)
}
