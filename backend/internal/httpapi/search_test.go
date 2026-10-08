package httpapi

import (
	"net/url"
	"strings"
	"testing"
)

func TestSearchEndpoint(t *testing.T) {
	e := newTestEnv(t, RegOpen)
	anna := e.client()
	if code := e.do(t, anna, "POST", "/api/auth/register", m{"email": "anna@example.hu", "username": "anna", "display_name": "Anna", "password": "hosszujelszo"}, nil); code != 201 {
		t.Fatalf("register anna = %d", code)
	}
	var spaces []spaceResponse
	e.do(t, anna, "GET", "/api/spaces", nil, &spaces)
	if len(spaces) != 1 {
		t.Fatalf("spaces = %+v", spaces)
	}
	if code := e.do(t, anna, "POST", "/api/spaces/"+spaces[0].ID+"/pages", m{"title": "Tanúbeszéd vázlat", "kind": "document"}, nil); code != 201 {
		t.Fatalf("create page = %d", code)
	}

	var res []searchResultResponse
	if code := e.do(t, anna, "GET", "/api/search?q="+url.QueryEscape("tanubesz"), nil, &res); code != 200 {
		t.Fatalf("search = %d", code)
	}
	if len(res) != 1 || res[0].Title != "Tanúbeszéd vázlat" || res[0].SpaceID != spaces[0].ID {
		t.Fatalf("results = %+v", res)
	}
	if !strings.Contains(res[0].TitleMarked, "\x02Tanúbeszéd\x03") {
		t.Fatalf("title_marked = %q", res[0].TitleMarked)
	}

	// Empty query: empty list, not null and not an error.
	res = nil
	if code := e.do(t, anna, "GET", "/api/search?q=", nil, &res); code != 200 || res == nil || len(res) != 0 {
		t.Fatalf("empty query = %d %+v", code, res)
	}
	if code := e.do(t, anna, "GET", "/api/search?q="+strings.Repeat("a", 201), nil, nil); code != 400 {
		t.Fatalf("overlong query = %d", code)
	}

	// Another user never sees anna's page.
	bob := e.client()
	if code := e.do(t, bob, "POST", "/api/auth/register", m{"email": "bob@example.hu", "username": "bob", "display_name": "Bob", "password": "hosszujelszo"}, nil); code != 201 {
		t.Fatalf("register bob = %d", code)
	}
	res = nil
	e.do(t, bob, "GET", "/api/search?q=tanubesz", nil, &res)
	if len(res) != 0 {
		t.Fatalf("bob sees anna's page: %+v", res)
	}

	if code := e.do(t, e.client(), "GET", "/api/search?q=x", nil, nil); code != 401 {
		t.Fatalf("anonymous search = %d", code)
	}
}
