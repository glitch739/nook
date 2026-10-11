package httpapi

import (
	"context"
	"testing"

	"nook/internal/auth"
	"nook/internal/dbtest"
	"nook/internal/groups"
	"nook/internal/pages"
	"nook/internal/permissions"
	"nook/internal/spaces"
	"nook/internal/users"
)

// The check an open realtime connection re-runs fails for every way its
// access can disappear after it opened.
func TestCollabStillAllowed(t *testing.T) {
	ctx := context.Background()
	conn := dbtest.New(t)
	us, ss := users.NewStore(conn), auth.NewSessionStore(conn)
	gs, ps := groups.NewStore(conn), pages.NewStore(conn)
	alice, _ := us.Create(ctx, "alice", "h", "Alice")
	bob, _ := us.Create(ctx, "bob", "h", "Bob")
	g, _ := gs.Create(ctx, "Team", alice.ID)
	if err := gs.AddMember(ctx, g.ID, bob.ID, groups.RoleEditor); err != nil {
		t.Fatal(err)
	}
	sp, _ := spaces.NewStore(conn).Create(ctx, "S", spaces.TypeShared, alice.ID, g.ID)
	page, _ := ps.Create(ctx, sp.ID, nil, "Doc", alice.ID, pages.KindDocument)
	sess, _ := ss.Create(ctx, bob.ID)
	a := collabAPI{pages: ps, perm: permissions.NewChecker(conn), auth: auth.NewHandler(us, ss, false)}
	edit := a.stillAllowed(sess.Token, bob.ID, page.ID, true)
	view := a.stillAllowed(sess.Token, bob.ID, page.ID, false)
	check := func(step string, wantEdit, wantView bool) {
		t.Helper()
		if edit(ctx) != wantEdit || view(ctx) != wantView {
			t.Fatalf("%s: edit=%v view=%v, want %v %v", step, edit(ctx), view(ctx), wantEdit, wantView)
		}
	}

	check("editor", true, true)
	gs.UpdateMemberRole(ctx, g.ID, bob.ID, groups.RoleViewer)
	check("demoted to viewer", false, true)
	ps.Archive(ctx, page.ID)
	check("page archived", false, false)
	ps.Restore(ctx, page.ID)
	check("page restored", false, true)
	us.SetStatus(ctx, bob.ID, users.StatusDisabled)
	check("account disabled", false, false)
	us.SetStatus(ctx, bob.ID, users.StatusActive)
	check("account active again", false, true)
	gs.RemoveMember(ctx, g.ID, bob.ID)
	check("removed from group", false, false)
	gs.AddMember(ctx, g.ID, bob.ID, groups.RoleViewer)
	check("back as viewer", false, true)
	ss.Delete(ctx, sess.Token)
	check("session revoked", false, false)
}
