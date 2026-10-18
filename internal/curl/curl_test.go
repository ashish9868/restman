package curl

import (
	"strings"
	"testing"

	"restman/internal/model"
)

func TestRoundTrip(t *testing.T) {
	req, err := Parse(`curl -X POST https://example.test/pets -H 'Accept: application/json' -u 'ada:secret' --data-raw '{"name":"nori"}'`)
	if err != nil {
		t.Fatal(err)
	}
	if req.Method != "POST" || req.URL != "https://example.test/pets" || req.Auth.Username != "ada" || req.Body.Raw == "" {
		t.Fatalf("%+v", req)
	}
	if len(req.Headers) != 1 || req.Headers[0].Key != "Accept" {
		t.Fatalf("headers %+v", req.Headers)
	}
	formatted := Format(req)
	again, err := Parse(formatted)
	if err != nil {
		t.Fatal(err)
	}
	if again.Method != "POST" || again.URL != req.URL || again.Body.Raw != req.Body.Raw {
		t.Fatalf("formatted %s again %+v", formatted, again)
	}
}

func modelRequest() model.Request {
	return model.Request{
		Method: "POST", URL: "https://example.test/pets",
		Auth:    model.Auth{Type: model.AuthBearer, Token: "tok"},
		Headers: []model.KeyValue{{Key: "Accept", Value: "application/json", Enabled: true}},
		Body:    model.Body{Mode: model.BodyRaw, Raw: `{"name":"nori"}`},
	}
}

func TestSnippetLanguages(t *testing.T) {
	req := modelRequest()
	checks := map[string]string{
		"javascript": "fetch(",
		"node":       "axios",
		"python":     "requests.request",
		"go":         "http.NewRequest",
		"java":       "HttpRequest.newBuilder",
		"csharp":     "HttpRequestMessage",
		"php":        "curl_init",
		"ruby":       "Net::HTTP",
		"swift":      "URLRequest",
		"kotlin":     "Request.Builder",
		"rust":       "reqwest",
		"powershell": "Invoke-RestMethod",
		"wget":       "wget",
	}
	for lang, needle := range checks {
		got := FormatLanguage(lang, req)
		if !strings.Contains(got, needle) {
			t.Errorf("%s snippet %q missing %q", lang, got, needle)
		}
	}
}
