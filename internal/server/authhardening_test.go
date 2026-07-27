package server

import (
	"testing"

	"golang.org/x/crypto/bcrypt"
)

// Security review item 12. The dummy hash only equalises login timing if it is
// actually a valid bcrypt hash at the same cost the app uses for real
// passwords. A typo would make CompareHashAndPassword fail instantly on a
// format error and reopen the enumeration oracle silently, so assert the shape
// rather than trusting the constant to be right.
func TestDummyHashMatchesRealCost(t *testing.T) {
	cost, err := bcrypt.Cost([]byte(dummyHash))
	if err != nil {
		t.Fatalf("dummyHash is not a valid bcrypt hash: %v", err)
	}
	if cost != 12 {
		t.Fatalf("dummyHash cost %d, but passwords are hashed at cost 12 — the miss path would be measurably faster", cost)
	}
}

// It equalises timing; it must never be a usable credential.
func TestDummyHashIsNotACredential(t *testing.T) {
	for _, guess := range []string{"", "password", "admin", "musicseer", dummyHash} {
		if bcrypt.CompareHashAndPassword([]byte(dummyHash), []byte(guess)) == nil {
			t.Fatalf("dummyHash accepted %q as a password", guess)
		}
	}
}
