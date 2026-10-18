package app

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"restman/internal/model"
	"restman/internal/session"
)

func TestSendAndHistory(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Trace") != "from-script" {
			t.Fatalf("header %s", r.Header.Get("X-Trace"))
		}
		w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	dir := t.TempDir()
	app, err := New(filepath.Join(dir, "data"), filepath.Join(dir, "ws"))
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	col, err := app.store.CreateCollection("Demo")
	if err != nil {
		t.Fatal(err)
	}
	snap, err := app.SaveEnvironment(model.Environment{Name: "local", Variables: []model.KeyValue{{Key: "base", Value: srv.URL, Enabled: true}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.SetActiveEnvironment(snap.Environments[0].ID); err != nil {
		t.Fatal(err)
	}
	resp, err := app.Send(SendInput{
		CollectionID: col.ID,
		Request: model.Request{
			Name: "Ping", Method: "GET", URL: "{{base}}/ping",
			Auth:    model.Auth{Type: model.AuthNone},
			Scripts: model.Scripts{Prerequest: `pm.request.headers.upsert({key: "X-Trace", value: "from-script"});`, Test: `pm.test("ok", function () { pm.expect(pm.response.code).to.equal(200); });`},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Status != 200 || len(resp.Tests) != 1 || !resp.Tests[0].Pass {
		t.Fatalf("%+v", resp)
	}
	history, err := app.History(10)
	if err != nil || len(history) != 1 {
		t.Fatalf("history %+v %v", history, err)
	}
}

func TestConfigDir(t *testing.T) {
	if _, err := session.ConfigDir(); err != nil {
		t.Fatal(err)
	}
}

func TestAllowedSite(t *testing.T) {
	if !allowedSite("https://d2itechnology.com/") || !allowedSite("https://www.d2itechnology.com/about") {
		t.Fatal("company site should open")
	}
	if allowedSite("http://d2itechnology.com/") || allowedSite("https://example.com/") {
		t.Fatal("other links should stay closed")
	}
}
