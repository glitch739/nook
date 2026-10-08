package activity

import (
	"context"
	"testing"

	"nook/internal/dbtest"
	"nook/internal/groups"
	"nook/internal/pages"
	"nook/internal/spaces"
	"nook/internal/users"
)

func TestRecordCoalescesEditsAndSetsUpdatedBy(t *testing.T) {
	ctx := context.Background()
	conn := dbtest.New(t)
	u, _ := users.NewStore(conn).Create(ctx, "anna", "h", "Anna")
	g, _ := groups.NewStore(conn).Create(ctx, "G", u.ID)
	sp, _ := spaces.NewStore(conn).Create(ctx, "S", spaces.TypePrivate, u.ID, g.ID)
	ps := pages.NewStore(conn)
	p, err := ps.Create(ctx, sp.ID, nil, "Doc", u.ID, pages.KindDocument)
	if err != nil {
		t.Fatal(err)
	}
	s := NewStore(conn)
	for _, a := range []string{PageCreate, PageEdit, PageEdit, PageEdit} {
		if err := s.Record(ctx, u.ID, sp.ID, p.ID, a, nil); err != nil {
			t.Fatal(err)
		}
	}
	list, err := s.ForPage(ctx, p.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[0].Action != PageEdit || list[0].ActorName != "Anna" || list[0].PageTitle != "Doc" {
		t.Fatalf("list = %+v", list)
	}
	got, _ := ps.GetByID(ctx, p.ID)
	if got.UpdatedBy != u.ID || got.UpdatedByName != "Anna" {
		t.Fatalf("updated_by = %q %q", got.UpdatedBy, got.UpdatedByName)
	}
	if sl, _ := s.ForSpace(ctx, sp.ID, 0); len(sl) != 2 {
		t.Fatalf("space feed = %d", len(sl))
	}
}
