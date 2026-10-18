package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"restman/internal/model"
)

func TestResolveInherit(t *testing.T) {
	got := Resolve(
		model.Auth{Type: model.AuthInherit},
		[]model.Auth{{Type: model.AuthInherit}, {Type: model.AuthBearer, Token: "folder"}},
		model.Auth{Type: model.AuthBasic, Username: "root"},
	)
	if got.Type != model.AuthBearer || got.Token != "folder" {
		t.Fatalf("%+v", got)
	}
}

func TestClientCredentials(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		if r.Form.Get("grant_type") != "client_credentials" {
			t.Fatalf("grant %s", r.Form.Get("grant_type"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"tok","token_type":"Bearer"}`))
	}))
	defer srv.Close()
	oauth := &model.OAuth2{GrantType: "client_credentials", TokenURL: srv.URL, ClientID: "id", ClientSecret: "secret"}
	if err := FetchToken(t.Context(), srv.Client(), oauth, nil); err != nil {
		t.Fatal(err)
	}
	if oauth.AccessToken != "tok" {
		t.Fatalf("token %s", oauth.AccessToken)
	}
}
