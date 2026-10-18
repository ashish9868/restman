package importer

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"restman/internal/model"
	"restman/internal/workspace"
)

type collectionFile struct {
	Info struct {
		PostmanID   string `json:"_postman_id"`
		UID         string `json:"uid"`
		Name        string `json:"name"`
		Description any    `json:"description"`
		Schema      string `json:"schema"`
	} `json:"info"`
	Item     []item          `json:"item"`
	Variable []pmKV          `json:"variable"`
	Auth     json.RawMessage `json:"auth"`
	Event    []pmEvent       `json:"event"`
}

type item struct {
	ID          string          `json:"id"`
	UID         string          `json:"uid"`
	PostmanID   string          `json:"_postman_id"`
	Name        string          `json:"name"`
	Description any             `json:"description"`
	Item        []item          `json:"item"`
	Request     json.RawMessage `json:"request"`
	Auth        json.RawMessage `json:"auth"`
	Event       []pmEvent       `json:"event"`
	Response    []pmExample     `json:"response"`
}

type pmExample struct {
	ID     string          `json:"id"`
	Name   string          `json:"name"`
	Status string          `json:"status"`
	Code   int             `json:"code"`
	Header json.RawMessage `json:"header"`
	Body   string          `json:"body"`
}

type pmKV struct {
	Key         string  `json:"key"`
	Value       pmValue `json:"value"`
	Disabled    bool    `json:"disabled"`
	Enabled     *bool   `json:"enabled"`
	Description string  `json:"description"`
	Type        string  `json:"type"`
	Src         pmValue `json:"src"`
}

type pmValue struct {
	Text string
}

func (v *pmValue) UnmarshalJSON(data []byte) error {
	if len(data) == 0 || string(data) == "null" {
		return nil
	}
	switch data[0] {
	case '"':
		return json.Unmarshal(data, &v.Text)
	case '[':
		var items []pmValue
		if err := json.Unmarshal(data, &items); err != nil {
			v.Text = string(data)
			return nil
		}
		if len(items) > 0 {
			v.Text = items[0].Text
		}
		return nil
	default:
		v.Text = string(data)
		return nil
	}
}

type pmEvent struct {
	Listen string   `json:"listen"`
	Script pmScript `json:"script"`
}

type pmScript struct {
	Exec string
}

func (s *pmScript) UnmarshalJSON(data []byte) error {
	var raw struct {
		Exec json.RawMessage `json:"exec"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	s.Exec = jsonText(raw.Exec)
	if strings.HasPrefix(s.Exec, "[") {
		var lines []string
		if json.Unmarshal(raw.Exec, &lines) == nil {
			s.Exec = strings.Join(lines, "\n")
		}
	}
	return nil
}

func ImportCollection(store *workspace.Store, raw []byte) (*model.Collection, error) {
	if isV1(raw) {
		return importV1(store, raw)
	}
	var file collectionFile
	if err := json.Unmarshal(raw, &file); err != nil {
		return nil, err
	}
	if file.Info.Name == "" {
		return nil, fmt.Errorf("not a Postman collection")
	}
	external := firstText(file.Info.PostmanID, file.Info.UID)
	if err := replaceCollection(store, external); err != nil {
		return nil, err
	}
	col, err := store.CreateCollection(file.Info.Name)
	if err != nil {
		return nil, err
	}
	col.PostmanID = external
	col.Description = descriptionText(file.Info.Description)
	col.Auth = parseAuth(file.Auth)
	if col.Auth.Type == "" {
		col.Auth.Type = model.AuthNone
	}
	col.Variables = convertKV(file.Variable)
	col.Scripts = scriptsFrom(file.Event)
	if err := store.SaveCollection(*col); err != nil {
		return nil, err
	}
	for _, child := range file.Item {
		if err := importItem(store, col.ID, child); err != nil {
			return nil, err
		}
	}
	saved, _ := store.Collection(col.ID)
	return &saved, nil
}

func importItem(store *workspace.Store, parentID string, it item) error {
	external := firstText(it.PostmanID, it.ID, it.UID)
	if len(it.Item) > 0 && len(it.Request) == 0 {
		folder, err := store.CreateFolder(parentID, it.Name)
		if err != nil {
			return err
		}
		folder.PostmanID = external
		folder.Description = descriptionText(it.Description)
		folder.Auth = parseAuth(it.Auth)
		if folder.Auth.Type == "" {
			folder.Auth.Type = model.AuthInherit
		}
		folder.Scripts = scriptsFrom(it.Event)
		if err := store.SaveFolder(*folder); err != nil {
			return err
		}
		for _, child := range it.Item {
			if err := importItem(store, folder.ID, child); err != nil {
				return err
			}
		}
		return nil
	}
	req, err := store.CreateRequest(parentID, it.Name, model.ProtocolHTTP)
	if err != nil {
		return err
	}
	parsed := parseRequest(it.Request)
	parsed.ID = req.ID
	parsed.PostmanID = external
	parsed.Name = it.Name
	if parsed.Description == "" {
		parsed.Description = descriptionText(it.Description)
	}
	if parsed.Auth.Type == "" {
		parsed.Auth = parseAuth(it.Auth)
	}
	if parsed.Auth.Type == "" {
		parsed.Auth.Type = model.AuthInherit
	}
	parsed.Scripts = scriptsFrom(it.Event)
	parsed.Examples = examplesFrom(it.Response)
	parsed.Normalize()
	return store.SaveRequest(parentID, parsed)
}

func ExportCollection(store *workspace.Store, collectionID string) ([]byte, error) {
	col, ok := store.Collection(collectionID)
	if !ok {
		return nil, fmt.Errorf("collection not found")
	}
	info := map[string]any{
		"name":        col.Name,
		"_postman_id": externalID(col.PostmanID, col.ID),
		"schema":      "https://schema.getpostman.com/json/collection/v2.1.0/collection.json",
	}
	if col.Description != "" {
		info["description"] = col.Description
	}
	payload := map[string]any{
		"info":     info,
		"item":     exportChildren(store, collectionID),
		"variable": exportKV(col.Variables),
	}
	if authn := exportAuth(col.Auth, false); authn != nil {
		payload["auth"] = authn
	}
	if events := exportEvents(col.Scripts); len(events) > 0 {
		payload["event"] = events
	}
	return json.MarshalIndent(payload, "", "  ")
}

func exportChildren(store *workspace.Store, parentID string) []any {
	tree := findNode(store.Tree(), parentID)
	if tree == nil {
		return []any{}
	}
	out := make([]any, 0, len(tree.Children))
	for _, child := range tree.Children {
		switch child.Kind {
		case "folder":
			folder, _ := store.Folder(child.ID)
			item := map[string]any{
				"name": folder.Name,
				"item": exportChildren(store, child.ID),
			}
			if id := externalID(folder.PostmanID, folder.ID); id != "" {
				item["id"] = id
			}
			if folder.Description != "" {
				item["description"] = folder.Description
			}
			putAuth(item, folder.Auth)
			if events := exportEvents(folder.Scripts); len(events) > 0 {
				item["event"] = events
			}
			out = append(out, item)
		case "request":
			req, err := store.GetRequest(child.ID)
			if err != nil {
				continue
			}
			item := map[string]any{
				"name":    req.Name,
				"request": exportRequest(req),
			}
			if id := externalID(req.PostmanID, req.ID); id != "" {
				item["id"] = id
			}
			if req.Description != "" {
				item["description"] = req.Description
			}
			if events := exportEvents(req.Scripts); len(events) > 0 {
				item["event"] = events
			}
			if examples := exportExamples(req); len(examples) > 0 {
				item["response"] = examples
			}
			out = append(out, item)
		}
	}
	return out
}

func findNode(nodes []model.TreeNode, id string) *model.TreeNode {
	for i := range nodes {
		if nodes[i].ID == id {
			return &nodes[i]
		}
		if found := findNode(nodes[i].Children, id); found != nil {
			return found
		}
	}
	return nil
}

func ImportEnvironment(raw []byte) (model.Environment, error) {
	var env model.Environment
	if err := json.Unmarshal(raw, &env); err == nil && env.Name != "" && env.Variables != nil {
		return env, nil
	}
	var postman struct {
		ID     string `json:"id"`
		Name   string `json:"name"`
		Values []pmKV `json:"values"`
	}
	if err := json.Unmarshal(raw, &postman); err != nil {
		return model.Environment{}, err
	}
	if postman.Name == "" {
		return model.Environment{}, fmt.Errorf("not a Postman environment")
	}
	return model.Environment{Name: postman.Name, Variables: convertKV(postman.Values)}, nil
}

func ExportEnvironment(env model.Environment) ([]byte, error) {
	values := make([]map[string]any, 0, len(env.Variables))
	for _, item := range env.Variables {
		row := map[string]any{"key": item.Key, "value": item.Value, "enabled": item.Enabled}
		if item.Description != "" {
			row["description"] = item.Description
		}
		values = append(values, row)
	}
	payload := map[string]any{
		"name": env.Name, "values": values, "_postman_variable_scope": "environment",
	}
	if env.ID != "" {
		payload["id"] = env.ID
	}
	return json.MarshalIndent(payload, "", "  ")
}

func parseRequest(raw json.RawMessage) model.Request {
	req := model.Request{Method: "GET", Protocol: model.ProtocolHTTP, Auth: model.Auth{Type: model.AuthInherit}, Body: model.Body{Mode: model.BodyNone}}
	if len(raw) == 0 {
		return req
	}
	if raw[0] == '"' {
		_ = json.Unmarshal(raw, &req.URL)
		return req
	}
	var body struct {
		Method      string          `json:"method"`
		Description any             `json:"description"`
		Header      json.RawMessage `json:"header"`
		URL         json.RawMessage `json:"url"`
		Body        struct {
			Mode       string          `json:"mode"`
			Raw        string          `json:"raw"`
			File       json.RawMessage `json:"file"`
			Options    json.RawMessage `json:"options"`
			URLEncoded []pmKV          `json:"urlencoded"`
			FormData   []pmKV          `json:"formdata"`
			GraphQL    json.RawMessage `json:"graphql"`
		} `json:"body"`
		Auth json.RawMessage `json:"auth"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return req
	}
	if body.Method != "" {
		req.Method = body.Method
	}
	req.Description = descriptionText(body.Description)
	req.URL = urlText(body.URL)
	req.Headers = parseKVList(body.Header)
	req.Auth = parseAuth(body.Auth)
	switch body.Body.Mode {
	case "raw":
		req.Body.Mode = model.BodyRaw
		req.Body.Raw = body.Body.Raw
		req.Body.Language = rawLanguage(body.Body.Options)
		if req.Body.Language == "" {
			req.Body.Language = "text"
		}
	case "urlencoded":
		req.Body.Mode = model.BodyURLEncoded
		req.Body.URLEncoded = convertKV(body.Body.URLEncoded)
	case "formdata":
		req.Body.Mode = model.BodyFormData
		req.Body.FormData = convertForm(body.Body.FormData)
	case "graphql":
		req.Body.Mode = model.BodyGraphQL
		req.Body.GraphQL = parseGraphQL(body.Body.GraphQL)
		req.Method = "POST"
	case "file":
		req.Body.Mode = model.BodyBinary
		req.Body.FilePath = fileSrc(body.Body.File)
	default:
		req.Body.Mode = model.BodyNone
	}
	req.Params = urlParams(body.URL)
	return req
}

func urlText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	if raw[0] == '"' {
		var text string
		_ = json.Unmarshal(raw, &text)
		return text
	}
	var obj struct {
		Raw string `json:"raw"`
	}
	_ = json.Unmarshal(raw, &obj)
	return obj.Raw
}

func urlParams(raw json.RawMessage) []model.KeyValue {
	if len(raw) == 0 || raw[0] == '"' {
		return nil
	}
	var obj struct {
		Query    []pmKV `json:"query"`
		Variable []pmKV `json:"variable"`
	}
	_ = json.Unmarshal(raw, &obj)
	path := convertKV(obj.Variable)
	for i := range path {
		if path[i].Description == "" {
			path[i].Description = "path"
		}
	}
	return append(path, convertKV(obj.Query)...)
}

func parseAuth(raw json.RawMessage) model.Auth {
	if len(raw) == 0 || string(raw) == "null" {
		return model.Auth{}
	}
	var envelope struct {
		Type   string `json:"type"`
		Basic  []pmKV `json:"basic"`
		Bearer []pmKV `json:"bearer"`
		APIKey []pmKV `json:"apikey"`
		OAuth2 []pmKV `json:"oauth2"`
		Digest []pmKV `json:"digest"`
		NTLM   []pmKV `json:"ntlm"`
		AWS    []pmKV `json:"awsv4"`
		OAuth1 []pmKV `json:"oauth1"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return model.Auth{}
	}
	values := map[string]string{}
	take := func(items []pmKV) {
		for _, item := range items {
			values[item.Key] = item.Value.Text
		}
	}
	switch envelope.Type {
	case "noauth":
		return model.Auth{Type: model.AuthNone}
	case "basic":
		take(envelope.Basic)
		return model.Auth{Type: model.AuthBasic, Username: values["username"], Password: values["password"]}
	case "bearer":
		take(envelope.Bearer)
		return model.Auth{Type: model.AuthBearer, Token: values["token"]}
	case "apikey":
		take(envelope.APIKey)
		in := values["in"]
		if in == "" {
			in = "header"
		}
		return model.Auth{Type: model.AuthAPIKey, Key: values["key"], Value: values["value"], In: in}
	case "digest":
		take(envelope.Digest)
		return model.Auth{Type: model.AuthDigest, Username: values["username"], Password: values["password"]}
	case "ntlm":
		take(envelope.NTLM)
		username := values["username"]
		if domain := values["domain"]; domain != "" && !strings.Contains(username, `\`) {
			username = domain + `\` + username
		}
		return model.Auth{Type: model.AuthNTLM, Username: username, Password: values["password"]}
	case "awsv4":
		take(envelope.AWS)
		return model.Auth{Type: model.AuthAWS, AWS: &model.AWSAuth{
			AccessKey: values["accessKey"], SecretKey: values["secretKey"], Region: values["region"],
			Service: values["service"], SessionToken: values["sessionToken"],
		}}
	case "oauth1":
		take(envelope.OAuth1)
		return model.Auth{Type: model.AuthOAuth1, OAuth1: &model.OAuth1{
			ConsumerKey: values["consumerKey"], ConsumerSecret: values["consumerSecret"],
			Token: values["token"], TokenSecret: values["tokenSecret"], Realm: values["realm"],
		}}
	case "oauth2":
		take(envelope.OAuth2)
		return model.Auth{Type: model.AuthOAuth2, OAuth2: &model.OAuth2{
			GrantType:    values["grant_type"],
			AuthURL:      values["authUrl"],
			TokenURL:     firstText(values["accessTokenUrl"], values["tokenUrl"]),
			ClientID:     values["clientId"],
			ClientSecret: values["clientSecret"],
			Scopes:       firstText(values["scope"], values["scopes"]),
			RedirectURL:  firstText(values["redirect_uri"], values["redirectUri"]),
			AccessToken:  values["accessToken"],
			RefreshToken: values["refreshToken"],
			TokenType:    values["tokenType"],
			Username:     values["username"],
			Password:     values["password"],
			UsePKCE:      truthy(values["usePkce"]) || values["challengeAlgorithm"] == "S256",
		}}
	default:
		if envelope.Type == "" {
			return model.Auth{}
		}
		return model.Auth{Type: envelope.Type}
	}
}

func exportAuth(authn model.Auth, inheritOmits bool) any {
	switch authn.Type {
	case "", model.AuthInherit:
		if inheritOmits {
			return nil
		}
		return map[string]any{"type": "noauth"}
	case model.AuthNone:
		return map[string]any{"type": "noauth"}
	case model.AuthBasic:
		return map[string]any{"type": "basic", "basic": pairs("username", authn.Username, "password", authn.Password)}
	case model.AuthBearer:
		return map[string]any{"type": "bearer", "bearer": pairs("token", authn.Token)}
	case model.AuthAPIKey:
		in := authn.In
		if in == "" {
			in = "header"
		}
		return map[string]any{"type": "apikey", "apikey": pairs("key", authn.Key, "value", authn.Value, "in", in)}
	case model.AuthDigest:
		return map[string]any{"type": "digest", "digest": pairs("username", authn.Username, "password", authn.Password)}
	case model.AuthNTLM:
		domain, user := splitDomain(authn.Username)
		return map[string]any{"type": "ntlm", "ntlm": pairs("username", user, "password", authn.Password, "domain", domain)}
	case model.AuthAWS:
		aws := authn.AWS
		if aws == nil {
			aws = &model.AWSAuth{}
		}
		return map[string]any{"type": "awsv4", "awsv4": pairs(
			"accessKey", aws.AccessKey, "secretKey", aws.SecretKey, "region", aws.Region,
			"service", aws.Service, "sessionToken", aws.SessionToken,
		)}
	case model.AuthOAuth1:
		oauth := authn.OAuth1
		if oauth == nil {
			oauth = &model.OAuth1{}
		}
		return map[string]any{"type": "oauth1", "oauth1": pairs(
			"consumerKey", oauth.ConsumerKey, "consumerSecret", oauth.ConsumerSecret,
			"token", oauth.Token, "tokenSecret", oauth.TokenSecret, "realm", oauth.Realm,
			"signatureMethod", "HMAC-SHA1", "version", "1.0",
		)}
	case model.AuthOAuth2:
		oauth := authn.OAuth2
		if oauth == nil {
			oauth = &model.OAuth2{}
		}
		fields := pairs(
			"grant_type", oauth.GrantType, "authUrl", oauth.AuthURL, "accessTokenUrl", oauth.TokenURL,
			"clientId", oauth.ClientID, "clientSecret", oauth.ClientSecret, "scope", oauth.Scopes,
			"redirect_uri", oauth.RedirectURL, "accessToken", oauth.AccessToken, "refreshToken", oauth.RefreshToken,
			"tokenType", oauth.TokenType, "username", oauth.Username, "password", oauth.Password,
		)
		if oauth.UsePKCE {
			fields = append(fields, map[string]string{"key": "challengeAlgorithm", "value": "S256", "type": "string"})
		}
		return map[string]any{"type": "oauth2", "oauth2": fields}
	default:
		return map[string]any{"type": "noauth"}
	}
}

func putAuth(dst map[string]any, authn model.Auth) {
	if exported := exportAuth(authn, true); exported != nil {
		dst["auth"] = exported
	}
}

func exportRequest(req model.Request) map[string]any {
	query, path := splitParams(req.Params)
	urlObj := map[string]any{"raw": req.URL}
	if len(query) > 0 {
		urlObj["query"] = exportParams(query)
	}
	if len(path) > 0 {
		urlObj["variable"] = exportParams(path)
	}
	out := map[string]any{
		"method": req.Method,
		"header": exportKV(req.Headers),
		"url":    urlObj,
	}
	if req.Description != "" {
		out["description"] = req.Description
	}
	putAuth(out, req.Auth)
	switch req.Body.Mode {
	case model.BodyRaw:
		out["body"] = map[string]any{"mode": "raw", "raw": req.Body.Raw, "options": map[string]any{"raw": map[string]any{"language": req.Body.Language}}}
	case model.BodyURLEncoded:
		out["body"] = map[string]any{"mode": "urlencoded", "urlencoded": exportKV(req.Body.URLEncoded)}
	case model.BodyFormData:
		out["body"] = map[string]any{"mode": "formdata", "formdata": exportForm(req.Body.FormData)}
	case model.BodyBinary:
		out["body"] = map[string]any{"mode": "file", "file": map[string]any{"src": req.Body.FilePath}}
	case model.BodyGraphQL:
		out["body"] = map[string]any{"mode": "graphql", "graphql": exportGraphQL(req.Body.GraphQL)}
	}
	return out
}

func exportExamples(req model.Request) []any {
	out := make([]any, 0, len(req.Examples))
	for _, example := range req.Examples {
		headers := make([]map[string]string, 0, len(example.Headers))
		for _, header := range example.Headers {
			headers = append(headers, map[string]string{"key": header.Key, "value": header.Value})
		}
		status := example.StatusText
		if status == "" && example.Status != 0 {
			status = http.StatusText(example.Status)
		}
		item := map[string]any{
			"name":            example.Name,
			"status":          status,
			"code":            example.Status,
			"header":          headers,
			"body":            example.Body,
			"originalRequest": exportRequest(req),
		}
		if example.ID != "" {
			item["id"] = example.ID
		}
		out = append(out, item)
	}
	return out
}

func examplesFrom(items []pmExample) []model.Example {
	out := make([]model.Example, 0, len(items))
	for _, item := range items {
		name := item.Name
		if name == "" {
			name = "Example"
		}
		headers := make([]model.Header, 0)
		for _, header := range parseKVList(item.Header) {
			headers = append(headers, model.Header{Key: header.Key, Value: header.Value})
		}
		out = append(out, model.Example{
			ID: item.ID, Name: name, Status: item.Code, StatusText: item.Status, Headers: headers, Body: item.Body,
		})
	}
	return out
}

func convertKV(items []pmKV) []model.KeyValue {
	out := make([]model.KeyValue, 0, len(items))
	for _, item := range items {
		if item.Key == "" && item.Value.Text == "" {
			continue
		}
		enabled := !item.Disabled
		if item.Enabled != nil {
			enabled = *item.Enabled
		}
		out = append(out, model.KeyValue{Key: item.Key, Value: item.Value.Text, Description: item.Description, Enabled: enabled})
	}
	return out
}

func convertForm(items []pmKV) []model.FormField {
	out := make([]model.FormField, 0, len(items))
	for _, item := range items {
		enabled := !item.Disabled
		if item.Enabled != nil {
			enabled = *item.Enabled
		}
		kind := item.Type
		if kind == "" {
			kind = "text"
		}
		path := item.Src.Text
		if path == "" && kind == "file" {
			path = item.Value.Text
		}
		out = append(out, model.FormField{
			Key: item.Key, Value: item.Value.Text, Description: item.Description, Enabled: enabled, Type: kind, FilePath: path,
		})
	}
	return out
}

func exportKV(items []model.KeyValue) []map[string]any {
	return exportFields(items, false)
}

func exportParams(items []model.KeyValue) []map[string]any {
	return exportFields(items, true)
}

func exportFields(items []model.KeyValue, params bool) []map[string]any {
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		row := map[string]any{"key": item.Key, "value": item.Value, "disabled": !item.Enabled}
		if item.Description != "" && (!params || (item.Description != "path" && item.Description != "query")) {
			row["description"] = item.Description
		}
		out = append(out, row)
	}
	return out
}

func exportForm(fields []model.FormField) []map[string]any {
	out := make([]map[string]any, 0, len(fields))
	for _, field := range fields {
		kind := field.Type
		if kind == "" {
			kind = "text"
		}
		row := map[string]any{"key": field.Key, "type": kind, "disabled": !field.Enabled}
		if kind == "file" {
			row["src"] = field.FilePath
		} else {
			row["value"] = field.Value
		}
		if field.Description != "" {
			row["description"] = field.Description
		}
		out = append(out, row)
	}
	return out
}

func splitParams(params []model.KeyValue) (query, path []model.KeyValue) {
	for _, item := range params {
		if item.Description == "path" {
			path = append(path, item)
			continue
		}
		query = append(query, item)
	}
	return query, path
}

func scriptsFrom(events []pmEvent) model.Scripts {
	var scripts model.Scripts
	for _, event := range events {
		switch event.Listen {
		case "prerequest":
			scripts.Prerequest = event.Script.Exec
		case "test":
			scripts.Test = event.Script.Exec
		}
	}
	return scripts
}

func exportEvents(scripts model.Scripts) []any {
	events := []any{}
	if scripts.Prerequest != "" {
		events = append(events, event("prerequest", scripts.Prerequest))
	}
	if scripts.Test != "" {
		events = append(events, event("test", scripts.Test))
	}
	return events
}

func event(listen, source string) map[string]any {
	return map[string]any{"listen": listen, "script": map[string]any{"type": "text/javascript", "exec": strings.Split(source, "\n")}}
}

func descriptionText(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case map[string]any:
		if content, ok := typed["content"].(string); ok {
			return content
		}
	}
	return ""
}

func parseKVList(raw json.RawMessage) []model.KeyValue {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	if raw[0] == '"' {
		var text string
		_ = json.Unmarshal(raw, &text)
		return parseHeaderString(text)
	}
	var items []pmKV
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil
	}
	return convertKV(items)
}

func parseHeaderString(text string) []model.KeyValue {
	var out []model.KeyValue
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "//") {
			continue
		}
		key, val, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		out = append(out, model.KeyValue{Key: strings.TrimSpace(key), Value: strings.TrimSpace(val), Enabled: true})
	}
	return out
}

func parseGraphQL(raw json.RawMessage) *model.GraphQL {
	if len(raw) == 0 || string(raw) == "null" {
		return &model.GraphQL{}
	}
	var body struct {
		Query         string          `json:"query"`
		Variables     json.RawMessage `json:"variables"`
		OperationName string          `json:"operationName"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return &model.GraphQL{}
	}
	return &model.GraphQL{Query: body.Query, Variables: jsonText(body.Variables), OperationName: body.OperationName}
}

func exportGraphQL(gql *model.GraphQL) map[string]any {
	if gql == nil {
		gql = &model.GraphQL{}
	}
	out := map[string]any{"query": gql.Query, "operationName": gql.OperationName}
	var parsed any
	if json.Unmarshal([]byte(gql.Variables), &parsed) == nil && parsed != nil && !isJSONString(gql.Variables) {
		out["variables"] = parsed
	} else {
		out["variables"] = gql.Variables
	}
	return out
}

func isJSONString(text string) bool {
	text = strings.TrimSpace(text)
	return strings.HasPrefix(text, `"`)
}

func rawLanguage(raw json.RawMessage) string {
	var options struct {
		Raw struct {
			Language string `json:"language"`
		} `json:"raw"`
	}
	_ = json.Unmarshal(raw, &options)
	return options.Raw.Language
}

func fileSrc(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	if raw[0] == '"' {
		return jsonText(raw)
	}
	var file struct {
		Src pmValue `json:"src"`
	}
	if json.Unmarshal(raw, &file) == nil && file.Src.Text != "" {
		return file.Src.Text
	}
	return jsonText(raw)
}

func jsonText(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	if raw[0] == '"' {
		var text string
		if json.Unmarshal(raw, &text) == nil {
			return text
		}
	}
	if raw[0] == '[' {
		var lines []string
		if json.Unmarshal(raw, &lines) == nil {
			return strings.Join(lines, "\n")
		}
	}
	return string(raw)
}

func pairs(items ...string) []map[string]string {
	out := make([]map[string]string, 0, len(items)/2)
	for i := 0; i+1 < len(items); i += 2 {
		out = append(out, map[string]string{"key": items[i], "value": items[i+1], "type": "string"})
	}
	return out
}

func splitDomain(username string) (string, string) {
	domain, user, ok := strings.Cut(username, `\`)
	if !ok {
		return "", username
	}
	return domain, user
}

func truthy(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "true", "1", "yes":
		return true
	default:
		return false
	}
}

func firstText(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func externalID(postmanID, ownID string) string {
	if postmanID != "" {
		return postmanID
	}
	return ownID
}

func replaceCollection(store *workspace.Store, external string) error {
	existing, ok := store.CollectionByExternalID(external)
	if !ok {
		return nil
	}
	return store.Delete(existing.ID)
}

func isV1(raw []byte) bool {
	var probe map[string]json.RawMessage
	if json.Unmarshal(raw, &probe) != nil {
		return false
	}
	if _, ok := probe["info"]; ok {
		return false
	}
	_, requests := probe["requests"]
	_, folders := probe["folders"]
	return requests || folders
}

type v1File struct {
	ID          string      `json:"id"`
	Name        string      `json:"name"`
	Description any         `json:"description"`
	Order       []string    `json:"order"`
	Folders     []v1Folder  `json:"folders"`
	Requests    []v1Request `json:"requests"`
}

type v1Folder struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description any      `json:"description"`
	Order       []string `json:"order"`
	Folder      *string  `json:"folder"`
}

type v1Request struct {
	ID               string          `json:"id"`
	Name             string          `json:"name"`
	Description      any             `json:"description"`
	URL              string          `json:"url"`
	Method           string          `json:"method"`
	Headers          string          `json:"headers"`
	HeaderData       []pmKV          `json:"headerData"`
	DataMode         string          `json:"dataMode"`
	Data             json.RawMessage `json:"data"`
	RawModeData      string          `json:"rawModeData"`
	DataOptions      json.RawMessage `json:"dataOptions"`
	Tests            string          `json:"tests"`
	PreRequestScript string          `json:"preRequestScript"`
	QueryParams      []pmKV          `json:"queryParams"`
	PathVariableData []pmKV          `json:"pathVariableData"`
	Auth             json.RawMessage `json:"auth"`
	Folder           *string         `json:"folder"`
	Responses        []v1Response    `json:"responses"`
}

type v1Response struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Status       string `json:"status"`
	ResponseCode struct {
		Code int    `json:"code"`
		Name string `json:"name"`
	} `json:"responseCode"`
	Headers []pmKV `json:"headers"`
	Text    string `json:"text"`
}

func importV1(store *workspace.Store, raw []byte) (*model.Collection, error) {
	var file v1File
	if err := json.Unmarshal(raw, &file); err != nil {
		return nil, err
	}
	if file.Name == "" {
		return nil, fmt.Errorf("not a Postman collection")
	}
	if err := replaceCollection(store, file.ID); err != nil {
		return nil, err
	}
	col, err := store.CreateCollection(file.Name)
	if err != nil {
		return nil, err
	}
	col.PostmanID = file.ID
	col.Description = descriptionText(file.Description)
	col.Auth = model.Auth{Type: model.AuthNone}
	if err := store.SaveCollection(*col); err != nil {
		return nil, err
	}
	folders := map[string]string{}
	pending := append([]v1Folder{}, file.Folders...)
	for len(pending) > 0 {
		next := make([]v1Folder, 0)
		progress := false
		for _, folder := range pending {
			parent := col.ID
			if folder.Folder != nil && *folder.Folder != "" {
				id, ok := folders[*folder.Folder]
				if !ok {
					next = append(next, folder)
					continue
				}
				parent = id
			}
			created, err := store.CreateFolder(parent, folder.Name)
			if err != nil {
				return nil, err
			}
			created.PostmanID = folder.ID
			created.Description = descriptionText(folder.Description)
			created.Auth = model.Auth{Type: model.AuthInherit}
			if err := store.SaveFolder(*created); err != nil {
				return nil, err
			}
			folders[folder.ID] = created.ID
			progress = true
		}
		if !progress {
			for _, folder := range next {
				created, err := store.CreateFolder(col.ID, folder.Name)
				if err != nil {
					return nil, err
				}
				created.PostmanID = folder.ID
				created.Description = descriptionText(folder.Description)
				created.Auth = model.Auth{Type: model.AuthInherit}
				if err := store.SaveFolder(*created); err != nil {
					return nil, err
				}
				folders[folder.ID] = created.ID
			}
			break
		}
		pending = next
	}
	byID := map[string]v1Request{}
	for _, req := range file.Requests {
		byID[req.ID] = req
	}
	placed := map[string]bool{}
	place := func(ids []string, parent string) error {
		for _, id := range ids {
			req, ok := byID[id]
			if !ok || placed[id] {
				continue
			}
			if err := importV1Request(store, parent, req); err != nil {
				return err
			}
			placed[id] = true
		}
		return nil
	}
	for _, folder := range file.Folders {
		parent := folders[folder.ID]
		if parent == "" {
			parent = col.ID
		}
		if err := place(folder.Order, parent); err != nil {
			return nil, err
		}
	}
	if err := place(file.Order, col.ID); err != nil {
		return nil, err
	}
	for _, req := range file.Requests {
		if placed[req.ID] {
			continue
		}
		parent := col.ID
		if req.Folder != nil && *req.Folder != "" {
			if id := folders[*req.Folder]; id != "" {
				parent = id
			}
		}
		if err := importV1Request(store, parent, req); err != nil {
			return nil, err
		}
	}
	saved, _ := store.Collection(col.ID)
	return &saved, nil
}

func importV1Request(store *workspace.Store, parentID string, src v1Request) error {
	created, err := store.CreateRequest(parentID, src.Name, model.ProtocolHTTP)
	if err != nil {
		return err
	}
	req := *created
	req.PostmanID = src.ID
	req.Description = descriptionText(src.Description)
	req.URL = src.URL
	if src.Method != "" {
		req.Method = src.Method
	}
	if len(src.HeaderData) > 0 {
		req.Headers = convertKV(src.HeaderData)
	} else {
		req.Headers = parseHeaderString(src.Headers)
	}
	path := convertKV(src.PathVariableData)
	for i := range path {
		if path[i].Description == "" {
			path[i].Description = "path"
		}
	}
	req.Params = append(path, convertKV(src.QueryParams)...)
	req.Auth = parseAuth(src.Auth)
	if req.Auth.Type == "" {
		req.Auth.Type = model.AuthInherit
	}
	applyV1Body(&req, src)
	req.Scripts = model.Scripts{Prerequest: src.PreRequestScript, Test: src.Tests}
	for _, response := range src.Responses {
		name := response.Name
		if name == "" {
			name = "Example"
		}
		status := response.Status
		if status == "" {
			status = response.ResponseCode.Name
		}
		headers := make([]model.Header, 0, len(response.Headers))
		for _, header := range convertKV(response.Headers) {
			headers = append(headers, model.Header{Key: header.Key, Value: header.Value})
		}
		req.Examples = append(req.Examples, model.Example{
			ID: response.ID, Name: name, Status: response.ResponseCode.Code, StatusText: status, Headers: headers, Body: response.Text,
		})
	}
	req.Normalize()
	return store.SaveRequest(parentID, req)
}

func applyV1Body(req *model.Request, src v1Request) {
	switch src.DataMode {
	case "urlencoded":
		req.Body.Mode = model.BodyURLEncoded
		req.Body.URLEncoded = parseKVList(src.Data)
	case "params", "formdata":
		req.Body.Mode = model.BodyFormData
		req.Body.FormData = convertForm(mustKV(src.Data))
	case "binary", "file":
		req.Body.Mode = model.BodyBinary
		req.Body.FilePath = fileSrc(src.Data)
	case "graphql":
		req.Body.Mode = model.BodyGraphQL
		req.Body.GraphQL = parseGraphQL(src.Data)
		req.Method = "POST"
	default:
		if src.RawModeData == "" && src.DataMode == "" {
			req.Body.Mode = model.BodyNone
			return
		}
		req.Body.Mode = model.BodyRaw
		req.Body.Raw = src.RawModeData
		req.Body.Language = rawLanguage(src.DataOptions)
		if req.Body.Language == "" {
			req.Body.Language = "text"
		}
	}
}

func mustKV(raw json.RawMessage) []pmKV {
	var items []pmKV
	_ = json.Unmarshal(raw, &items)
	return items
}
