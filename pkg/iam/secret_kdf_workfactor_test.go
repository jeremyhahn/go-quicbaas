package iam

import (
	"crypto/rand"
	"os"
	"testing"
)

// testWorkFactor is the PBKDF2 work factor pkg/iam's test binary issues and
// verifies at. 1,000 is SP 800-132's floor, so the FIPS-only runs accept it
// too. Every client-secret test would otherwise derive at the production
// 600,000 iterations, which -race multiplies until the package outruns its
// timeout on a loaded runner (#721). The tests that are about the production
// factor itself use the explicit-factor helpers below and pay for it once.
const testWorkFactor = 1000

// productionWorkFactor and productionEqualizer are the package defaults,
// captured by package initialisation before TestMain lowers them.
var (
	productionWorkFactor = secretWorkFactor
	productionEqualizer  = timingEqualizerDigest
)

func TestMain(m *testing.M) {
	useTestWorkFactor()
	os.Exit(m.Run())
}

// useTestWorkFactor lowers the process's work factor and replaces the timing
// equalizer with a digest at the same factor, so the equalizer still costs
// what a real verification costs. The equalizer's secret is random and
// discarded, as the production one's was.
func useTestWorkFactor() {
	secretWorkFactor = testWorkFactor

	discarded := make([]byte, 32)
	if _, err := rand.Read(discarded); err != nil {
		panic("pkg/iam TestMain: " + err.Error())
	}
	digest, err := deriveSecretHashAt(string(discarded), testWorkFactor)
	if err != nil {
		panic("pkg/iam TestMain: " + err.Error())
	}
	timingEqualizerDigest = digest
}

func TestSecretWorkFactor_ProductionDefaultIsTheOWASPFactor(t *testing.T) {
	t.Parallel()

	// The seam must not move the production value: a binary that never runs
	// TestMain issues and verifies at 600,000, with the matching equalizer.
	if pbkdf2Iterations != 600000 {
		t.Fatalf("pbkdf2Iterations = %d, want 600000 (OWASP's PBKDF2-HMAC-SHA256 guidance)", pbkdf2Iterations)
	}
	if productionWorkFactor != pbkdf2Iterations {
		t.Fatalf("default secretWorkFactor = %d, want pbkdf2Iterations (%d)", productionWorkFactor, pbkdf2Iterations)
	}
	if productionEqualizer != productionTimingEqualizerDigest {
		t.Fatal("default timingEqualizerDigest must be productionTimingEqualizerDigest")
	}
	digest, err := parseSecretHashAt(productionTimingEqualizerDigest, pbkdf2Iterations)
	if err != nil {
		t.Fatalf("the production equalizer must parse at the production factor: %v", err)
	}
	if digest.iterations != pbkdf2Iterations {
		t.Errorf("production equalizer iterations = %d, want %d", digest.iterations, pbkdf2Iterations)
	}
}

func TestSecretWorkFactor_TestBinaryRunsAtTheTestFactor(t *testing.T) {
	t.Parallel()

	if secretWorkFactor != testWorkFactor {
		t.Fatalf("secretWorkFactor = %d in the test binary, want %d", secretWorkFactor, testWorkFactor)
	}

	stored, err := hashSecret("issued-in-the-test-binary")
	if err != nil {
		t.Fatalf("hashSecret: %v", err)
	}
	digest, err := parseSecretHash(stored)
	if err != nil {
		t.Fatalf("parseSecretHash: %v", err)
	}
	if digest.iterations != testWorkFactor {
		t.Errorf("issued at %d iterations, want %d", digest.iterations, testWorkFactor)
	}
	if !verifySecret(stored, "issued-in-the-test-binary") {
		t.Error("a digest issued at the test factor must verify at it")
	}

	// The equalizer moves with the factor, or every equalized path would
	// fail to parse and answer faster than a real verification.
	eq, err := parseSecretHash(timingEqualizerDigest)
	if err != nil {
		t.Fatalf("the test equalizer must parse at the test factor: %v", err)
	}
	if eq.iterations != testWorkFactor {
		t.Errorf("test equalizer iterations = %d, want %d", eq.iterations, testWorkFactor)
	}
}

func TestSecretWorkFactor_LowDigestNeverVerifiesAtProductionFactor(t *testing.T) {
	t.Parallel()

	// The care the issue asks for: a digest at the lowered factor — say one
	// leaked into a fixture or a record — is refused by a production process
	// before the KDF runs, even for the right secret.
	const secret = "low-factor-secret"
	stored, err := deriveSecretHashAt(secret, testWorkFactor)
	if err != nil {
		t.Fatalf("deriveSecretHashAt: %v", err)
	}
	if _, err := parseSecretHashAt(stored, pbkdf2Iterations); err == nil {
		t.Fatal("a digest at the test factor must not parse at the production factor")
	}
	if compareSecretHashAt(stored, secret, pbkdf2Iterations) {
		t.Fatal("a digest at the test factor must not verify at the production factor")
	}
	// And the other way: the test process refuses the production factor, so
	// the one-factor-per-process rule holds here too.
	if compareSecretHash(productionTimingEqualizerDigest, "") {
		t.Fatal("the production equalizer must not verify in the test binary")
	}
	if _, err := parseSecretHash(productionTimingEqualizerDigest); err == nil {
		t.Fatal("a digest at the production factor must not parse at the test factor")
	}
}

func TestSecretWorkFactor_ProductionRoundTrip(t *testing.T) {
	t.Parallel()

	// The one place pkg/iam's unit tier pays the production KDF: issue and
	// verify at 600,000 iterations, so the production path stays proven.
	const secret = "production-factor-secret"
	stored, err := deriveSecretHashAt(secret, pbkdf2Iterations)
	if err != nil {
		t.Fatalf("deriveSecretHashAt: %v", err)
	}
	digest, err := parseSecretHashAt(stored, pbkdf2Iterations)
	if err != nil {
		t.Fatalf("parseSecretHashAt: %v", err)
	}
	if digest.iterations != pbkdf2Iterations {
		t.Errorf("issued at %d iterations, want %d", digest.iterations, pbkdf2Iterations)
	}
	if !compareSecretHashAt(stored, secret, pbkdf2Iterations) {
		t.Fatal("a production digest must verify at the production factor")
	}
	if compareSecretHashAt(stored, "wrong", pbkdf2Iterations) {
		t.Fatal("a production digest must not verify a wrong secret")
	}
}
