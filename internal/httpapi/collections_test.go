package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"bailing/internal/app"
	"bailing/internal/auth"
	"bailing/internal/config"
	"bailing/internal/store"
)

func collectionsServer(t *testing.T) (*Server, *http.ServeMux, *auth.User) {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(`INSERT INTO users(id, name, pw_hash, created_at) VALUES(1,'a','x',0),(2,'b','x',0)`); err != nil {
		t.Fatal(err)
	}
	s := &Server{app: &app.App{Cfg: config.Default(), DB: db}}
	mux := http.NewServeMux()
	s.musicRoutes(mux)
	return s, mux, &auth.User{ID: 1, Name: "a"}
}

func call(t *testing.T, mux http.Handler, u *auth.User, method, path, body string) map[string]any {
	t.Helper()
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r = r.WithContext(auth.WithUser(r.Context(), u))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	var out map[string]any
	json.Unmarshal(w.Body.Bytes(), &out)
	if w.Code != http.StatusOK {
		out["_status"] = float64(w.Code)
	}
	return out
}

func TestFavorites(t *testing.T) {
	_, mux, u := collectionsServer(t)
	call(t, mux, u, "POST", "/api/favorites", `{"kind":"station","item":{"source":"radio","id":"s1","title":"A"}}`)
	call(t, mux, u, "POST", "/api/favorites", `{"kind":"track","items":[{"source":"local","id":"t1"},{"source":"lx:kw","id":"9"}]}`)
	// re-adding updates instead of duplicating
	call(t, mux, u, "POST", "/api/favorites", `{"kind":"station","item":{"source":"radio","id":"s1","title":"B"}}`)

	all := call(t, mux, u, "GET", "/api/favorites", "")["items"].([]any)
	if len(all) != 3 {
		t.Fatalf("want 3 favourites, got %d", len(all))
	}
	st := call(t, mux, u, "GET", "/api/favorites?kind=station", "")["items"].([]any)
	if len(st) != 1 || st[0].(map[string]any)["item"].(map[string]any)["title"] != "B" {
		t.Fatalf("station favourites: %v", st)
	}
	if r := call(t, mux, u, "POST", "/api/favorites", `{"kind":"track","item":{"id":"x"}}`); r["_status"] != float64(400) {
		t.Fatalf("missing source should be rejected: %v", r)
	}
	call(t, mux, u, "POST", "/api/favorites/remove", `{"source":"radio","id":"s1"}`)
	if n := len(call(t, mux, u, "GET", "/api/favorites", "")["items"].([]any)); n != 2 {
		t.Fatalf("after remove: %d", n)
	}
	other := &auth.User{ID: 2}
	if n := len(call(t, mux, other, "GET", "/api/favorites", "")["items"].([]any)); n != 0 {
		t.Fatalf("favourites leaked to another user: %d", n)
	}
}

func TestPlaylists(t *testing.T) {
	_, mux, u := collectionsServer(t)
	r := call(t, mux, u, "POST", "/api/playlists", `{"name":"  旅行  ","items":[{"source":"local","id":"t1"}]}`)
	id := int(r["id"].(float64))
	if r["name"] != "旅行" {
		t.Fatalf("name not trimmed: %v", r)
	}
	p := "/api/playlists/" + strconv.Itoa(id)
	r = call(t, mux, u, "POST", p+"/items", `{"items":[{"source":"local","id":"t1"},{"source":"radio","id":"s1"}]}`)
	if r["added"] != float64(1) || r["skipped"] != float64(1) {
		t.Fatalf("append: %v", r)
	}
	call(t, mux, u, "PUT", p, `{"name":"Trip","items":[{"source":"radio","id":"s1"},{"source":"local","id":"t1"}]}`)
	got := call(t, mux, u, "GET", p, "")
	items := got["items"].([]any)
	if got["name"] != "Trip" || len(items) != 2 || items[0].(map[string]any)["id"] != "s1" {
		t.Fatalf("after update: %v", got)
	}
	list := call(t, mux, u, "GET", "/api/playlists", "")["playlists"].([]any)
	if len(list) != 1 || list[0].(map[string]any)["count"] != float64(2) {
		t.Fatalf("list: %v", list)
	}
	other := &auth.User{ID: 2}
	if r := call(t, mux, other, "GET", p, ""); r["_status"] != float64(404) {
		t.Fatalf("other user must not see the playlist: %v", r)
	}
	call(t, mux, u, "DELETE", p, "")
	if r := call(t, mux, u, "GET", p, ""); r["_status"] != float64(404) {
		t.Fatalf("deleted playlist still readable: %v", r)
	}
}
