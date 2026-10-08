package users_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"nook/internal/db"
	"nook/internal/dbtest"
	"nook/internal/groups"
	"nook/internal/pages"
	"nook/internal/spaces"
	"nook/internal/users"
)

func count(t *testing.T, conn *sql.DB, q string, args ...any) int {
	t.Helper()
	var n int
	if err := conn.QueryRow(q, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
	return n
}

func TestDeleteDisabled(t *testing.T) {
	ctx := context.Background()
	conn := dbtest.New(t)
	us, gs, ss, ps := users.NewStore(conn), groups.NewStore(conn), spaces.NewStore(conn), pages.NewStore(conn)

	admin, _ := us.Create(ctx, "admin", "h", "Admin")
	gone, _ := us.Create(ctx, "gone", "h", "Gone")

	// gone's private group/space/page: must disappear.
	pg, _ := gs.Create(ctx, "Private", gone.ID)
	psp, _ := ss.Create(ctx, "Personal", spaces.TypePrivate, gone.ID, pg.ID)
	privPage, err := ps.Create(ctx, psp.ID, nil, "secret", gone.ID, pages.KindDocument)
	if err != nil {
		t.Fatal(err)
	}

	// A group gone owns but shares with admin, and a page gone wrote in
	// admin's group: both must survive under admin.
	shared, _ := gs.Create(ctx, "Shared", gone.ID)
	if err := gs.AddMember(ctx, shared.ID, admin.ID, groups.RoleEditor); err != nil {
		t.Fatal(err)
	}
	ssp, _ := ss.Create(ctx, "Team", spaces.TypeShared, gone.ID, shared.ID)
	sharedPage, _ := ps.Create(ctx, ssp.ID, nil, "team notes", gone.ID, pages.KindDocument)
	ag, _ := gs.Create(ctx, "Admin group", admin.ID)
	asp, _ := ss.Create(ctx, "Admin space", spaces.TypePrivate, admin.ID, ag.ID)
	if err := gs.AddMember(ctx, ag.ID, gone.ID, groups.RoleEditor); err != nil {
		t.Fatal(err)
	}
	contrib, _ := ps.Create(ctx, asp.ID, nil, "contribution", gone.ID, pages.KindDocument)

	// Still active: refused, nothing changes.
	if err := us.DeleteDisabled(ctx, gone.ID, admin.ID); !errors.Is(err, users.ErrNotDisabled) {
		t.Fatalf("active account: err = %v, want ErrNotDisabled", err)
	}
	if err := us.SetStatus(ctx, gone.ID, users.StatusDisabled); err != nil {
		t.Fatal(err)
	}
	if err := us.DeleteDisabled(ctx, gone.ID, admin.ID); err != nil {
		t.Fatalf("DeleteDisabled: %v", err)
	}

	if _, err := us.GetByID(ctx, gone.ID); !errors.Is(err, db.ErrNotFound) {
		t.Fatalf("account still there: %v", err)
	}
	if n := count(t, conn, `SELECT COUNT(*) FROM pages WHERE id = ?`, privPage.ID); n != 0 {
		t.Fatal("private page survived")
	}
	if n := count(t, conn, `SELECT COUNT(*) FROM groups WHERE id = ?`, pg.ID); n != 0 {
		t.Fatal("private group survived")
	}
	for _, id := range []string{sharedPage.ID, contrib.ID} {
		if n := count(t, conn, `SELECT COUNT(*) FROM pages WHERE id = ? AND created_by = ?`, id, admin.ID); n != 1 {
			t.Fatalf("shared page %s not kept under admin", id)
		}
	}
	if n := count(t, conn, `SELECT COUNT(*) FROM groups WHERE id = ? AND owner_id = ?`, shared.ID, admin.ID); n != 1 {
		t.Fatal("shared group not handed to admin")
	}
	if n := count(t, conn, `SELECT COUNT(*) FROM group_members WHERE user_id = ?`, gone.ID); n != 0 {
		t.Fatal("memberships left behind")
	}
	if err := us.DeleteDisabled(ctx, gone.ID, admin.ID); !errors.Is(err, db.ErrNotFound) {
		t.Fatalf("second delete: %v", err)
	}
}
