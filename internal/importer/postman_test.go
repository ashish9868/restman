package importer

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"restman/internal/model"
	"restman/internal/workspace"
)

func TestImportExport(t *testing.T) {
	raw := []byte(`{
		"info": {"name": "Demo", "schema": "https://schema.getpostman.com/json/collection/v2.1.0/collection.json"},
		"item": [{
			"name": "List",
			"request": {
				"method": "GET",
				"header": [{"key": "Accept", "value": "application/json"}],
				"url": {"raw": "{{base}}/items", "query": [{"key": "q", "value": "1"}]}
			},
			"event": [{"listen": "test", "script": {"exec": ["pm.test('ok', function(){ pm.expect(1).to.equal(1); });"]}}]
		}],
		"variable": [{"key": "base", "value": "https://example.test"}]
	}`)
	store, err := workspace.Open(filepath.Join(t.TempDir(), "ws"))
	if err != nil {
		t.Fatal(err)
	}
	col, err := ImportCollection(store, raw)
	if err != nil {
		t.Fatal(err)
	}
	reqs := store.RequestsUnder(col.ID)
	if len(reqs) != 1 || reqs[0].URL != "{{base}}/items" || reqs[0].Scripts.Test == "" {
		t.Fatalf("%+v", reqs)
	}
	exported, err := ExportCollection(store, col.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(exported), "List") || !strings.Contains(string(exported), "{{base}}/items") {
		t.Fatalf("%s", exported)
	}
}

func TestRoundTripPreservesPostmanFields(t *testing.T) {
	raw := []byte(`{
		"info": {
			"_postman_id": "col-1",
			"name": "Demo",
			"description": {"content": "A demo", "type": "text/plain"},
			"schema": "https://schema.getpostman.com/json/collection/v2.1.0/collection.json"
		},
		"auth": {"type": "oauth2", "oauth2": [
			{"key": "grant_type", "value": "authorization_code"},
			{"key": "accessTokenUrl", "value": "https://example.test/token"},
			{"key": "clientId", "value": "cid"},
			{"key": "challengeAlgorithm", "value": "S256"}
		]},
		"item": [
			{
				"id": "folder-1",
				"name": "Group",
				"description": "Grouped",
				"item": [{
					"id": "req-1",
					"name": "Upload",
					"description": "Sends a file",
					"request": {
						"method": "POST",
						"header": "Accept: application/json",
						"url": {"raw": "{{base}}/files/:id", "query": [{"key": "q", "value": "1"}], "variable": [{"key": "id", "value": "9"}]},
						"body": {
							"mode": "formdata",
							"formdata": [
								{"key": "note", "value": "hi", "type": "text"},
								{"key": "file", "type": "file", "src": "/tmp/a.txt"}
							]
						}
					},
					"response": [{
						"id": "ex-1",
						"name": "Created",
						"code": 201,
						"status": "Created",
						"header": [{"key": "Content-Type", "value": "application/json"}],
						"body": "{\"ok\":true}"
					}],
					"event": [{"listen": "test", "script": {"exec": "pm.test('ok', function() {});"}}]
				}]
			},
			{
				"name": "Inherit",
				"request": {"method": "GET", "url": "https://example.test/inherit"}
			},
			{
				"name": "Signed",
				"request": {
					"method": "GET",
					"url": "https://example.test/aws",
					"auth": {"type": "awsv4", "awsv4": [
						{"key": "accessKey", "value": "AKIA"},
						{"key": "secretKey", "value": "secret"},
						{"key": "region", "value": "us-east-1"},
						{"key": "service", "value": "s3"}
					]}
				}
			}
		]
	}`)
	store, err := workspace.Open(filepath.Join(t.TempDir(), "ws"))
	if err != nil {
		t.Fatal(err)
	}
	col, err := ImportCollection(store, raw)
	if err != nil {
		t.Fatal(err)
	}
	if col.PostmanID != "col-1" || col.Description != "A demo" || col.Auth.Type != model.AuthOAuth2 || !col.Auth.OAuth2.UsePKCE {
		t.Fatalf("%+v", col)
	}
	reqs := store.RequestsUnder(col.ID)
	var upload model.Request
	for _, req := range reqs {
		if req.Name == "Upload" {
			upload = req
		}
	}
	if upload.PostmanID != "req-1" || upload.Description != "Sends a file" || upload.Scripts.Test == "" {
		t.Fatalf("%+v", upload)
	}
	if upload.Body.Mode != model.BodyFormData || len(upload.Body.FormData) != 2 || upload.Body.FormData[1].FilePath != "/tmp/a.txt" {
		t.Fatalf("%+v", upload.Body)
	}
	if len(upload.Examples) != 1 || upload.Examples[0].Status != 201 || upload.Examples[0].Body != `{"ok":true}` {
		t.Fatalf("%+v", upload.Examples)
	}
	var path, query bool
	for _, param := range upload.Params {
		if param.Key == "id" && param.Description == "path" {
			path = true
		}
		if param.Key == "q" {
			query = true
		}
	}
	if !path || !query {
		t.Fatalf("%+v", upload.Params)
	}
	exported, err := ExportCollection(store, col.ID)
	if err != nil {
		t.Fatal(err)
	}
	text := string(exported)
	for _, want := range []string{`"type": "oauth2"`, `"mode": "formdata"`, `"/tmp/a.txt"`, "Sends a file", "Created", "col-1", `"type": "awsv4"`, "Grouped"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %s in %s", want, text)
		}
	}
	var payload map[string]any
	if err := json.Unmarshal(exported, &payload); err != nil {
		t.Fatal(err)
	}
	items := payload["item"].([]any)
	inherit := items[1].(map[string]any)["request"].(map[string]any)
	if _, ok := inherit["auth"]; ok {
		t.Fatalf("inherit auth was exported: %+v", inherit)
	}
	again, err := ImportCollection(store, exported)
	if err != nil {
		t.Fatal(err)
	}
	if len(store.Tree()) != 1 || again.PostmanID != "col-1" {
		t.Fatalf("%+v", store.Tree())
	}
	loaded := store.RequestsUnder(again.ID)
	if len(loaded) != 3 {
		t.Fatalf("%+v", loaded)
	}
	if again.Auth.OAuth2 == nil || !again.Auth.OAuth2.UsePKCE || again.Auth.OAuth2.ClientID != "cid" {
		t.Fatalf("%+v", again.Auth)
	}
	for _, req := range loaded {
		if req.Name == "Upload" && (req.Body.FormData[1].FilePath != "/tmp/a.txt" || req.Description != "Sends a file" || len(req.Examples) != 1) {
			t.Fatalf("%+v", req)
		}
		if req.Name == "Signed" && (req.Auth.AWS == nil || req.Auth.AWS.Service != "s3") {
			t.Fatalf("%+v", req.Auth)
		}
	}
}

func TestImportV1(t *testing.T) {
	raw := []byte(`{
		"id": "v1-col",
		"name": "Legacy",
		"description": "old",
		"order": ["r1"],
		"folders": [{"id": "f1", "name": "Nested", "description": "inside", "order": ["r2"]}],
		"requests": [
			{
				"id": "r1",
				"name": "Root",
				"url": "https://example.test/root",
				"method": "GET",
				"headers": "Accept: text/plain\n",
				"dataMode": "raw",
				"rawModeData": "hi",
				"preRequestScript": "pm.environment.set('a', 'b');",
				"tests": "tests['ok'] = true;"
			},
			{
				"id": "r2",
				"name": "Child",
				"folder": "f1",
				"url": "https://example.test/child",
				"method": "POST",
				"dataMode": "binary",
				"data": "/tmp/blob.bin",
				"auth": {"type": "digest", "digest": [{"key": "username", "value": "ada"}, {"key": "password", "value": "secret"}]},
				"responses": [{"name": "OK", "responseCode": {"code": 200, "name": "OK"}, "text": "done"}]
			}
		]
	}`)
	store, err := workspace.Open(filepath.Join(t.TempDir(), "ws"))
	if err != nil {
		t.Fatal(err)
	}
	col, err := ImportCollection(store, raw)
	if err != nil {
		t.Fatal(err)
	}
	if col.PostmanID != "v1-col" || col.Description != "old" {
		t.Fatalf("%+v", col)
	}
	reqs := store.RequestsUnder(col.ID)
	if len(reqs) != 2 {
		t.Fatalf("%+v", reqs)
	}
	tree := store.Tree()
	if len(tree) != 1 || len(tree[0].Children) != 2 {
		t.Fatalf("%+v", tree)
	}
	var child model.Request
	for _, req := range reqs {
		if req.Name == "Child" {
			child = req
		}
	}
	if child.Body.Mode != model.BodyBinary || child.Body.FilePath != "/tmp/blob.bin" || child.Auth.Type != model.AuthDigest {
		t.Fatalf("%+v", child)
	}
	if len(child.Examples) != 1 || child.Examples[0].Status != 200 {
		t.Fatalf("%+v", child.Examples)
	}
	exported, err := ExportCollection(store, col.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(exported), `"type": "digest"`) || !strings.Contains(string(exported), "/tmp/blob.bin") {
		t.Fatalf("%s", exported)
	}
	if _, err := ImportCollection(store, raw); err != nil {
		t.Fatal(err)
	}
	if len(store.Tree()) != 1 {
		t.Fatalf("%+v", store.Tree())
	}
}
