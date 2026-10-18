package runner

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"restman/internal/model"
	"restman/internal/vars"
)

func TestExecuteJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer secret" {
			t.Fatalf("auth %s", r.Header.Get("Authorization"))
		}
		if r.URL.Query().Get("q") != "cats" {
			t.Fatalf("query %s", r.URL.RawQuery)
		}
		body, _ := io.ReadAll(r.Body)
		if string(body) != `{"a":1}` {
			t.Fatalf("body %s", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	req := model.Request{
		Method: http.MethodPost,
		URL:    "{{base}}/items",
		Params: []model.KeyValue{{Key: "q", Value: "{{q}}", Enabled: true}},
		Auth:   model.Auth{Type: model.AuthBearer, Token: "{{token}}"},
		Body:   model.Body{Mode: model.BodyRaw, Language: "json", Raw: `{"a":1}`},
	}
	result, err := Execute(t.Context(), req, Options{
		Settings: model.DefaultSettings(),
		Scope: vars.Scope{Environment: map[string]string{
			"base": srv.URL, "q": "cats", "token": "secret",
		}},
		Auth: req.Auth,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Response.Status != 200 || !strings.Contains(result.Response.Body, "ok") {
		t.Fatalf("%+v", result.Response)
	}
	if result.Response.Timing.TotalMS < 0 {
		t.Fatal("missing timing")
	}
}

func TestGraphQLBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), "hero") || !strings.Contains(string(body), "Luke") {
			t.Fatalf("body %s", body)
		}
		_, _ = w.Write([]byte(`{"data":{"hero":"Luke"}}`))
	}))
	defer srv.Close()
	req := model.Request{
		Method: http.MethodPost,
		URL:    srv.URL,
		Body: model.Body{Mode: model.BodyGraphQL, GraphQL: &model.GraphQL{
			Query:     `query { hero }`,
			Variables: `{"name":"Luke"}`,
		}},
	}
	result, err := Execute(t.Context(), req, Options{Settings: model.DefaultSettings(), Auth: model.Auth{Type: model.AuthNone}})
	if err != nil {
		t.Fatal(err)
	}
	if result.Response.Status != 200 {
		t.Fatalf("%+v", result.Response)
	}
}
