package store

import (
	"errors"
	"sync"
	"testing"
)

// A syntactically valid bcrypt hash; these tests never verify a password, they
// only care which INSERTs land.
const fakeHash = "$2a$12$m3fH2QYv5Yvv0AZ3X6pkLO45GYzk8vAERpVSS93XaErgRsknaGlge"

func openTest(t *testing.T) *Store {
	t.Helper()
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.DB.Close() })
	return s
}

// Security review item 11. Two concurrent first-run setups must not both
// produce an admin. The previous handler counted users in Go and inserted
// afterwards, so both callers could observe zero users and both win — the
// guard has to be part of the write itself.
func TestCreateFirstAdminIsAtomic(t *testing.T) {
	s := openTest(t)

	const n = 8
	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		created []int64
	)
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start // release them together to maximise overlap
			id, err := s.CreateFirstAdmin(string(rune('a'+i))+"-admin", "", fakeHash)
			if err == nil {
				mu.Lock()
				created = append(created, id)
				mu.Unlock()
				return
			}
			if !errors.Is(err, ErrSetupComplete) {
				t.Errorf("unexpected error from CreateFirstAdmin: %v", err)
			}
		}(i)
	}
	close(start)
	wg.Wait()

	if len(created) != 1 {
		t.Fatalf("exactly one concurrent setup may succeed, got %d", len(created))
	}
	count, err := s.UserCount()
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("want 1 user in the table, got %d", count)
	}
}

// Once any account exists, setup is closed for good — including for a username
// different from the one that claimed the instance.
func TestCreateFirstAdminRefusesAfterSetup(t *testing.T) {
	s := openTest(t)

	if _, err := s.CreateFirstAdmin("jonathan", "", fakeHash); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateFirstAdmin("someone-else", "", fakeHash); !errors.Is(err, ErrSetupComplete) {
		t.Fatalf("want ErrSetupComplete for a second setup, got %v", err)
	}
	count, err := s.UserCount()
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("the second setup must not have inserted, got %d users", count)
	}
}

// A non-first user still goes through CreateUser, which must keep working —
// CreateFirstAdmin's NOT EXISTS guard is specific to the setup path.
func TestCreateUserStillWorksAfterFirstAdmin(t *testing.T) {
	s := openTest(t)

	if _, err := s.CreateFirstAdmin("jonathan", "", fakeHash); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateUser("family", "", fakeHash, "user", false); err != nil {
		t.Fatalf("admin-created accounts must not be blocked: %v", err)
	}
	count, err := s.UserCount()
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("want 2 users, got %d", count)
	}
}
