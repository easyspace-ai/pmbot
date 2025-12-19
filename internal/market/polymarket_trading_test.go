package market

import (
	"testing"
)

func TestBuildHMACSignature_Deterministic(t *testing.T) {
	// Use a fixed secret (base64url) and a tiny payload; we only check determinism and non-empty output.
	// This avoids embedding any real credentials.
	secret := "c2VjcmV0" // "secret" in base64 (works for url/base64 decode branches)
	sig1 := buildHMACSignature(secret, 1700000000, "POST", "/order", []byte(`{"a":1}`))
	sig2 := buildHMACSignature(secret, 1700000000, "POST", "/order", []byte(`{"a":1}`))
	if sig1 == "" || sig2 == "" {
		t.Fatalf("expected non-empty signature")
	}
	if sig1 != sig2 {
		t.Fatalf("expected deterministic signature")
	}
}

func TestComputeBuyAmounts_Bounds(t *testing.T) {
	_, _, err := computeBuyAmounts("0.01", 0.0, 10)
	if err == nil {
		t.Fatalf("expected error for out-of-bounds price")
	}
	_, _, err = computeBuyAmounts("0.01", 0.5, 10)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
}

