package scripts

import (
	"strings"
	"testing"

	"restman/internal/model"
	"restman/internal/vars"
)

func TestPreRequestSetsHeader(t *testing.T) {
	req := model.Request{Method: "GET", URL: "https://example.test"}
	scope := vars.Scope{Environment: map[string]string{}}
	box := Sandbox{}
	err := box.PreRequest(t.Context(), &req, &scope, []string{
		`pm.environment.set("token", "abc"); pm.request.headers.upsert({key: "X-Token", value: pm.environment.get("token")}); pm.request.url = "https://example.test/pets";`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if scope.Environment["token"] != "abc" {
		t.Fatalf("env %+v", scope.Environment)
	}
	if req.URL != "https://example.test/pets" {
		t.Fatalf("url %s", req.URL)
	}
	if len(req.Headers) != 1 || req.Headers[0].Value != "abc" {
		t.Fatalf("headers %+v", req.Headers)
	}
}

func TestTests(t *testing.T) {
	req := model.Request{}
	resp := model.Response{Status: 200, Body: `{"id":7}`, Timing: model.Timing{TotalMS: 12}}
	scope := vars.Scope{Environment: map[string]string{}}
	box := Sandbox{}
	results, err := box.Tests(t.Context(), &req, &resp, &scope, []string{`
		pm.test("status", function () { pm.expect(pm.response.code).to.equal(200); });
		pm.test("id", function () { pm.expect(pm.response.json()).to.have.property("id"); });
		pm.test("bad", function () { pm.expect(pm.response.code).to.equal(201); });
	`})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 3 || !results[0].Pass || !results[1].Pass || results[2].Pass {
		t.Fatalf("%+v", results)
	}
	if !strings.Contains(results[2].Error, "201") {
		t.Fatalf("error %s", results[2].Error)
	}
}
