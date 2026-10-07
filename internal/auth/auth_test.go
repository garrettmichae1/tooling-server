package auth

import "testing"

func TestEqual(t *testing.T) {
	const want = "test-token-must-be-at-least-32-bytes"
	if !Equal(want, want) {
		t.Fatal("same token rejected")
	}
	if Equal(want, want+"x") || Equal(want, "short") || Equal(want, "") {
		t.Fatal("mismatched token accepted")
	}
	if _, ok := Token("bearer " + want); ok {
		t.Fatal("lowercase scheme accepted")
	}
	got, ok := Token("Bearer " + want)
	if !ok || got != want {
		t.Fatal("bearer parse")
	}
	if _, ok := Token("Bearer " + want + " extra"); ok {
		t.Fatal("token with space accepted")
	}
}
