package users

import (
	"context"
	"errors"
	"testing"

	"nook/internal/dbtest"
)

func TestValidate(t *testing.T) {
	for _, u := range []string{"ab", "Bob", "a b", "x@y", "abcdefghijabcdefghijabcdefghijabc"} {
		if ValidateUsername(u) == nil {
			t.Errorf("ValidateUsername(%q) = nil, want error", u)
		}
	}
	for _, u := range []string{"bob", "anna.k", "a_b-c"} {
		if err := ValidateUsername(u); err != nil {
			t.Errorf("ValidateUsername(%q) = %v", u, err)
		}
	}
	for _, e := range []string{"", "a", "a@b", "A <a@b.hu>", "@b.hu"} {
		if ValidateEmail(e) == nil {
			t.Errorf("ValidateEmail(%q) = nil, want error", e)
		}
	}
	if err := ValidateEmail("anna@example.hu"); err != nil {
		t.Errorf("ValidateEmail: %v", err)
	}
}

func TestCreateAccountAndLookups(t *testing.T) {
	ctx := context.Background()
	s := NewStore(dbtest.New(t))
	u, err := s.CreateAccount(ctx, NewUser{Username: "anna", PasswordHash: "h", DisplayName: "Anna", Email: "anna@example.hu"})
	if err != nil {
		t.Fatal(err)
	}
	if u.Status != StatusActive || u.EmailVerified || u.IsAdmin {
		t.Fatalf("unexpected defaults: %+v", u)
	}
	for _, login := range []string{"anna", "ANNA", "Anna@Example.hu"} {
		got, err := s.GetByLogin(ctx, login)
		if err != nil || got.ID != u.ID {
			t.Fatalf("GetByLogin(%q) = %v, %v", login, got.ID, err)
		}
	}
	if _, err := s.CreateAccount(ctx, NewUser{Username: "other", PasswordHash: "h", DisplayName: "O", Email: "anna@example.hu"}); !errors.Is(err, ErrEmailTaken) {
		t.Fatalf("duplicate email err = %v", err)
	}
	if _, err := s.CreateAccount(ctx, NewUser{Username: "anna", PasswordHash: "h", DisplayName: "O", Email: "x@example.hu"}); !errors.Is(err, ErrUsernameTaken) {
		t.Fatalf("duplicate username err = %v", err)
	}
	if err := s.MarkEmailVerified(ctx, u.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.SetStatus(ctx, u.ID, StatusDisabled); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetByID(ctx, u.ID)
	if !got.EmailVerified || got.Status != StatusDisabled {
		t.Fatalf("after updates: %+v", got)
	}
}
