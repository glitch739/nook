package authtokens

import (
	"context"
	"errors"
	"testing"
	"time"

	"nook/internal/dbtest"
	"nook/internal/users"
)

func TestConsumeOnceAndSupersede(t *testing.T) {
	ctx := context.Background()
	conn := dbtest.New(t)
	u, err := users.NewStore(conn).Create(ctx, "anna", "h", "Anna")
	if err != nil {
		t.Fatal(err)
	}
	s := NewStore(conn)
	old, _, err := s.Create(ctx, NewParams{Purpose: PurposeReset, UserID: u.ID})
	if err != nil {
		t.Fatal(err)
	}
	secret, _, _ := s.Create(ctx, NewParams{Purpose: PurposeReset, UserID: u.ID})
	if _, err := s.Consume(ctx, PurposeReset, old); !errors.Is(err, ErrInvalid) {
		t.Fatalf("superseded token err = %v", err)
	}
	if _, err := s.Consume(ctx, PurposeVerifyEmail, secret); !errors.Is(err, ErrInvalid) {
		t.Fatalf("wrong purpose err = %v", err)
	}
	tok, err := s.Consume(ctx, PurposeReset, secret)
	if err != nil || tok.UserID != u.ID {
		t.Fatalf("Consume = %+v, %v", tok, err)
	}
	if _, err := s.Consume(ctx, PurposeReset, secret); !errors.Is(err, ErrInvalid) {
		t.Fatalf("second consume err = %v", err)
	}
	if recent, _ := s.RecentlyIssued(ctx, u.ID, PurposeReset, time.Minute); !recent {
		t.Fatal("RecentlyIssued = false")
	}
	expired, _, _ := s.Create(ctx, NewParams{Purpose: PurposeInvite, CreatedBy: u.ID, TTL: -time.Second})
	if _, err := s.Peek(ctx, PurposeInvite, expired); !errors.Is(err, ErrInvalid) {
		t.Fatalf("expired peek err = %v", err)
	}
}
