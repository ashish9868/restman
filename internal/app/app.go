package app

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"

	"restman/internal/auth"
	"restman/internal/curl"
	"restman/internal/importer"
	"restman/internal/model"
	"restman/internal/runner"
	"restman/internal/scripts"
	"restman/internal/session"
	"restman/internal/vars"
	"restman/internal/workspace"
	"restman/internal/ws"
)

type App struct {
	mu       sync.Mutex
	store    *workspace.Store
	session  *session.Store
	settings model.Settings
	jar      http.CookieJar
	hub      *ws.Hub
	inflight sync.Map
}

type Snapshot struct {
	WorkspaceName       string               `json:"workspaceName"`
	WorkspacePath       string               `json:"workspacePath"`
	ActiveEnvironmentID string               `json:"activeEnvironmentId"`
	Tree                []model.TreeNode     `json:"tree"`
	Environments        []model.Environment  `json:"environments"`
	Globals             []model.KeyValue     `json:"globals"`
	Settings            model.Settings       `json:"settings"`
	History             []model.HistoryEntry `json:"history"`
	Tabs                string               `json:"tabs"`
}

func New(dataDir, workspacePath string) (*App, error) {
	sess, err := session.Open(dataDir)
	if err != nil {
		return nil, err
	}
	app := &App{session: sess, hub: ws.New(), settings: model.DefaultSettings()}
	raw, err := sess.GetState("settings")
	if err != nil {
		return nil, err
	}
	if raw != "" {
		_ = json.Unmarshal([]byte(raw), &app.settings)
	}
	if workspacePath != "" {
		app.settings.WorkspacePath = workspacePath
	}
	if app.settings.WorkspacePath == "" {
		app.settings.WorkspacePath, err = session.DefaultWorkspace()
		if err != nil {
			return nil, err
		}
	}
	if err := app.open(app.settings.WorkspacePath); err != nil {
		return nil, err
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}
	app.jar = jar
	if err := app.restoreCookies(); err != nil {
		return nil, err
	}
	return app, nil
}

func (a *App) Close() error { return a.session.Close() }

func (a *App) open(path string) error {
	store, err := workspace.Open(path)
	if err != nil {
		return err
	}
	a.store = store
	a.settings.WorkspacePath = path
	return a.saveSettings()
}

func (a *App) Bootstrap() (Snapshot, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.snapshot()
}

func (a *App) snapshot() (Snapshot, error) {
	tabs, err := a.session.GetState("tabs")
	if err != nil {
		return Snapshot{}, err
	}
	history, err := a.session.History(100)
	if err != nil {
		return Snapshot{}, err
	}
	return Snapshot{
		WorkspaceName: a.store.Meta.Name, WorkspacePath: a.store.Root,
		ActiveEnvironmentID: a.store.Meta.ActiveEnvironmentID,
		Tree:                a.store.Tree(), Environments: a.store.Environments(),
		Globals: a.store.Globals.Variables, Settings: a.settings,
		History: history, Tabs: tabs,
	}, nil
}

func (a *App) OpenWorkspace(path string) (Snapshot, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.open(path); err != nil {
		return Snapshot{}, err
	}
	return a.snapshot()
}

func (a *App) CreateCollection(name string) (Snapshot, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if _, err := a.store.CreateCollection(name); err != nil {
		return Snapshot{}, err
	}
	return a.snapshot()
}

func (a *App) CreateFolder(parentID, name string) (Snapshot, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if _, err := a.store.CreateFolder(parentID, name); err != nil {
		return Snapshot{}, err
	}
	return a.snapshot()
}

func (a *App) CreateRequest(parentID, name, protocol string) (model.Request, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	parentID, err := a.ensureParent(parentID)
	if err != nil {
		return model.Request{}, err
	}
	req, err := a.store.CreateRequest(parentID, name, protocol)
	if err != nil {
		return model.Request{}, err
	}
	return *req, nil
}

func (a *App) GetRequest(id string) (model.Request, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.store.GetRequest(id)
}

func (a *App) SaveRequest(parentID string, req model.Request) (Snapshot, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	parentID, err := a.ensureParent(parentID)
	if err != nil {
		return Snapshot{}, err
	}
	if err := a.store.SaveRequest(parentID, req); err != nil {
		return Snapshot{}, err
	}
	return a.snapshot()
}

func (a *App) Delete(id string) (Snapshot, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.store.Delete(id); err != nil {
		return Snapshot{}, err
	}
	return a.snapshot()
}

func (a *App) DuplicateRequest(id string) (model.Request, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	req, err := a.store.DuplicateRequest(id)
	if err != nil {
		return model.Request{}, err
	}
	return *req, nil
}

func (a *App) Duplicate(id string) (Snapshot, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if _, err := a.store.DuplicateNode(id); err != nil {
		return Snapshot{}, err
	}
	return a.snapshot()
}

func (a *App) GetCollection(id string) (model.Collection, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	col, ok := a.store.Collection(id)
	if !ok {
		return model.Collection{}, fmt.Errorf("collection not found")
	}
	return col, nil
}

func (a *App) GetFolder(id string) (model.Folder, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	folder, ok := a.store.Folder(id)
	if !ok {
		return model.Folder{}, fmt.Errorf("folder not found")
	}
	return folder, nil
}

func (a *App) SaveCollection(col model.Collection) (Snapshot, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	current, ok := a.store.Collection(col.ID)
	if !ok {
		return Snapshot{}, fmt.Errorf("collection not found")
	}
	col.Order = current.Order
	if err := a.store.SaveCollection(col); err != nil {
		return Snapshot{}, err
	}
	return a.snapshot()
}

func (a *App) SaveFolder(folder model.Folder) (Snapshot, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	current, ok := a.store.Folder(folder.ID)
	if !ok {
		return Snapshot{}, fmt.Errorf("folder not found")
	}
	folder.Order = current.Order
	if err := a.store.SaveFolder(folder); err != nil {
		return Snapshot{}, err
	}
	return a.snapshot()
}

func (a *App) ImportCurl(command string) (model.Request, error) {
	return curl.Parse(command)
}

func (a *App) ExportCurl(req model.Request) string {
	return curl.Format(req)
}

func (a *App) Snippet(lang string, req model.Request) string {
	return curl.FormatLanguage(lang, req)
}

func (a *App) Introspect(rawURL string) (string, error) {
	req := model.Request{
		Name: "Introspection", Protocol: model.ProtocolHTTP, Method: "POST", URL: rawURL,
		Auth: model.Auth{Type: model.AuthNone},
		Body: model.Body{Mode: model.BodyGraphQL, GraphQL: &model.GraphQL{Query: introspectionQuery}},
	}
	resp, err := a.Send(SendInput{Request: req})
	if err != nil {
		return "", err
	}
	if resp.Error != "" {
		return "", fmt.Errorf("%s", resp.Error)
	}
	return resp.Body, nil
}

func (a *App) Move(id, parentID string, index int) (Snapshot, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.store.Move(id, parentID, index); err != nil {
		return Snapshot{}, err
	}
	return a.snapshot()
}

func (a *App) SaveEnvironment(env model.Environment) (Snapshot, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.store.SaveEnvironment(env); err != nil {
		return Snapshot{}, err
	}
	return a.snapshot()
}

func (a *App) DeleteEnvironment(id string) (Snapshot, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.store.DeleteEnvironment(id); err != nil {
		return Snapshot{}, err
	}
	return a.snapshot()
}

func (a *App) SetActiveEnvironment(id string) (Snapshot, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.store.SetActiveEnvironment(id); err != nil {
		return Snapshot{}, err
	}
	return a.snapshot()
}

func (a *App) SaveGlobals(variables []model.KeyValue) (Snapshot, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.store.SaveGlobals(variables); err != nil {
		return Snapshot{}, err
	}
	return a.snapshot()
}

func (a *App) SaveSettings(settings model.Settings) (Snapshot, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	settings.WorkspacePath = a.settings.WorkspacePath
	a.settings = settings
	if a.settings.TimeoutSeconds <= 0 {
		a.settings.TimeoutSeconds = 30
	}
	if err := a.saveSettings(); err != nil {
		return Snapshot{}, err
	}
	return a.snapshot()
}

func (a *App) SaveTabs(tabs string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.session.SetState("tabs", tabs)
}

type SendInput struct {
	TabID        string            `json:"tabId"`
	CollectionID string            `json:"collectionId"`
	FolderID     string            `json:"folderId"`
	Request      model.Request     `json:"request"`
	Data         map[string]string `json:"data,omitempty"`
	Iteration    int               `json:"iteration,omitempty"`
}

type RunInput struct {
	ID          string   `json:"id"`
	Iterations  int      `json:"iterations"`
	DelayMS     int      `json:"delayMs"`
	StopOnError bool     `json:"stopOnError"`
	DataCSV     string   `json:"dataCsv"`
	RequestIDs  []string `json:"requestIds"`
}

func (a *App) Send(in SendInput) (model.Response, error) {
	a.mu.Lock()
	settings := a.settings
	scope := a.scopeFor(in.Request, in.CollectionID, in.FolderID)
	if len(in.Data) > 0 {
		scope.Data = in.Data
	}
	resolved := a.resolveAuth(in.Request, in.CollectionID, in.FolderID)
	pre, post := a.scriptLists(in.Request, in.CollectionID, in.FolderID)
	jar := a.jar
	a.mu.Unlock()

	ctx, cancel := context.WithCancel(context.Background())
	if in.TabID != "" {
		a.inflight.Store(in.TabID, cancel)
	}
	defer func() {
		cancel()
		if in.TabID != "" {
			a.inflight.Delete(in.TabID)
		}
	}()

	box := scripts.Sandbox{Depth: 0, Iteration: in.Iteration, Sender: func(ctx context.Context, req model.Request, scope vars.Scope, depth int) (runner.Result, error) {
		return runner.Execute(ctx, req, runner.Options{Settings: settings, Jar: jar, Scope: scope, Auth: req.Auth, Depth: depth})
	}}
	result, err := runner.Execute(ctx, in.Request, runner.Options{
		Settings: settings,
		Jar:      jar,
		Scope:    scope,
		Auth:     resolved,
		Before: func(req *model.Request, scope *vars.Scope) error {
			return box.PreRequest(ctx, req, scope, pre)
		},
		After: func(req *model.Request, resp *model.Response, scope *vars.Scope) error {
			tests, err := box.Tests(ctx, req, resp, scope, post)
			resp.Tests = tests
			return err
		},
	})
	if err != nil {
		return model.Response{}, err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	_ = a.persistScope(in.CollectionID, result.Scope)
	a.captureCookies(result)
	_ = a.session.AddHistory(model.HistoryEntry{
		SentAt: time.Now().UTC().Format(time.RFC3339), Method: result.Request.Method,
		URL: result.Request.URL, Status: result.Response.Status, DurationMS: result.Response.Timing.TotalMS,
		Name: in.Request.Name, RequestID: in.Request.ID,
	}, in.Request)
	return result.Response, nil
}

func (a *App) Cancel(tabID string) {
	if cancel, ok := a.inflight.Load(tabID); ok {
		cancel.(context.CancelFunc)()
	}
}

func (a *App) Run(in RunInput) ([]model.RunResult, error) {
	if in.Iterations < 1 {
		in.Iterations = 1
	}
	a.mu.Lock()
	requests := a.store.RequestsUnder(in.ID)
	parents := map[string]string{}
	collections := map[string]string{}
	for _, req := range requests {
		parents[req.ID] = a.store.ParentID(req.ID)
		collections[req.ID] = a.store.CollectionID(req.ID)
	}
	a.mu.Unlock()
	if len(in.RequestIDs) > 0 {
		chosen := map[string]bool{}
		for _, id := range in.RequestIDs {
			chosen[id] = true
		}
		filtered := make([]model.Request, 0, len(requests))
		for _, req := range requests {
			if chosen[req.ID] {
				filtered = append(filtered, req)
			}
		}
		requests = filtered
	}
	rows := parseCSV(in.DataCSV)
	results := make([]model.RunResult, 0, len(requests)*in.Iterations)
	for iteration := 0; iteration < in.Iterations; iteration++ {
		var data map[string]string
		if len(rows) > 0 {
			data = rows[iteration%len(rows)]
		}
		for index, req := range requests {
			folderID := parents[req.ID]
			collectionID := collections[req.ID]
			if a.storeFolderIsCollectionParent(folderID) {
				folderID = ""
			}
			resp, err := a.Send(SendInput{CollectionID: collectionID, FolderID: folderID, Request: req, Data: data, Iteration: iteration})
			item := model.RunResult{
				RequestID: req.ID, Name: req.Name, Method: req.Method, URL: req.URL,
				Status: resp.Status, DurationMS: resp.Timing.TotalMS, Tests: resp.Tests, Error: resp.Error,
			}
			if err != nil && item.Error == "" {
				item.Error = err.Error()
			}
			if resp.ScriptError != "" && item.Error == "" {
				item.Error = resp.ScriptError
			}
			failed := item.Error != ""
			for _, test := range item.Tests {
				if !test.Pass {
					failed = true
				}
			}
			results = append(results, item)
			if in.StopOnError && failed {
				return results, nil
			}
			if in.DelayMS > 0 && (iteration < in.Iterations-1 || index < len(requests)-1) {
				time.Sleep(time.Duration(in.DelayMS) * time.Millisecond)
			}
		}
	}
	return results, nil
}

func (a *App) storeFolderIsCollectionParent(parentID string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	_, ok := a.store.Collection(parentID)
	return ok
}

func (a *App) Authorize(authn model.Auth) (model.Auth, error) {
	if authn.Type != model.AuthOAuth2 || authn.OAuth2 == nil {
		return authn, fmt.Errorf("oauth2 auth is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := auth.FetchToken(ctx, &http.Client{Timeout: 30 * time.Second}, authn.OAuth2, openBrowser); err != nil {
		return authn, err
	}
	return authn, nil
}

func (a *App) ImportPostman(content string) (Snapshot, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if _, err := importer.ImportCollection(a.store, []byte(content)); err != nil {
		return Snapshot{}, err
	}
	return a.snapshot()
}

func (a *App) ExportCollection(id string) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	data, err := importer.ExportCollection(a.store, id)
	return string(data), err
}

func (a *App) ImportEnvironment(content string) (Snapshot, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	env, err := importer.ImportEnvironment([]byte(content))
	if err != nil {
		return Snapshot{}, err
	}
	env.ID = ""
	if err := a.store.SaveEnvironment(env); err != nil {
		return Snapshot{}, err
	}
	return a.snapshot()
}

func (a *App) ExportEnvironment(id string) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, env := range a.store.Environments() {
		if env.ID == id {
			data, err := importer.ExportEnvironment(env)
			return string(data), err
		}
	}
	return "", fmt.Errorf("environment not found")
}

func (a *App) History(limit int) ([]model.HistoryEntry, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.session.History(limit)
}

func (a *App) HistoryRequest(id string) (model.Request, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.session.HistoryRequest(id)
}

func (a *App) ClearHistory() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.session.ClearHistory()
}

func (a *App) DeleteHistory(id string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.session.DeleteHistory(id)
}

func (a *App) SaveCookie(cookie model.Cookie) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if cookie.Path == "" {
		cookie.Path = "/"
	}
	if err := a.session.SaveCookies([]model.Cookie{cookie}); err != nil {
		return err
	}
	return a.restoreCookies()
}

func (a *App) Cookies() ([]model.Cookie, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.session.Cookies()
}

func (a *App) DeleteCookie(domain, path, name string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.session.DeleteCookie(domain, path, name); err != nil {
		return err
	}
	return a.restoreCookies()
}

func (a *App) WSConnect(tabID, rawURL string, headers []model.KeyValue, protocols []string, pingSeconds int) error {
	return a.hub.Connect(tabID, rawURL, headers, protocols, time.Duration(pingSeconds)*time.Second)
}

func (a *App) WSSend(tabID, data string, binary bool) error {
	return a.hub.Send(tabID, data, binary)
}

func (a *App) WSClose(tabID string) { a.hub.Close(tabID) }

func (a *App) WSPoll(tabID string, after int) []model.WSMessage {
	return a.hub.Poll(tabID, after)
}

func parseCSV(text string) []map[string]string {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	rows, err := csv.NewReader(bytes.NewReader([]byte(text))).ReadAll()
	if err != nil || len(rows) < 2 {
		return nil
	}
	out := make([]map[string]string, 0, len(rows)-1)
	for _, row := range rows[1:] {
		item := map[string]string{}
		for i, header := range rows[0] {
			if i < len(row) && strings.TrimSpace(header) != "" {
				item[strings.TrimSpace(header)] = row[i]
			}
		}
		out = append(out, item)
	}
	return out
}

const introspectionQuery = "query IntrospectionQuery { __schema { queryType { name } types { name kind description fields { name } } } }"

func (a *App) ensureParent(parentID string) (string, error) {
	if parentID != "" {
		return parentID, nil
	}
	for _, node := range a.store.Tree() {
		if node.Name == "Scratch" {
			return node.ID, nil
		}
	}
	col, err := a.store.CreateCollection("Scratch")
	if err != nil {
		return "", err
	}
	return col.ID, nil
}

func (a *App) scopeFor(req model.Request, collectionID, folderID string) vars.Scope {
	scope := vars.Scope{
		Request:     vars.FromKV(req.Variables),
		Environment: map[string]string{},
		Folder:      map[string]string{},
		Collection:  map[string]string{},
		Global:      vars.FromKV(a.store.Globals.Variables),
	}
	if env := a.store.ActiveEnvironment(); env != nil {
		scope.Environment = vars.FromKV(env.Variables)
	}
	if collectionID == "" {
		collectionID = a.store.CollectionID(req.ID)
	}
	if folderID == "" && req.ID != "" {
		parent := a.store.ParentID(req.ID)
		if _, ok := a.store.Folder(parent); ok {
			folderID = parent
		}
	}
	for _, folder := range a.folders(folderID) {
		for _, item := range folder.Variables {
			if item.Enabled && item.Key != "" {
				scope.Folder[item.Key] = item.Value
			}
		}
	}
	if col, ok := a.store.Collection(collectionID); ok {
		scope.Collection = vars.FromKV(col.Variables)
	}
	return scope
}

func (a *App) folders(folderID string) []model.Folder {
	if folderID == "" {
		return nil
	}
	ids := append(a.store.Ancestors(folderID), folderID)
	var folders []model.Folder
	for _, id := range ids {
		if folder, ok := a.store.Folder(id); ok {
			folders = append(folders, folder)
		}
	}
	return folders
}

func (a *App) resolveAuth(req model.Request, collectionID, folderID string) model.Auth {
	if collectionID == "" {
		collectionID = a.store.CollectionID(req.ID)
	}
	if folderID == "" && req.ID != "" {
		parent := a.store.ParentID(req.ID)
		if _, ok := a.store.Folder(parent); ok {
			folderID = parent
		}
	}
	var folderAuths []model.Auth
	for _, folder := range a.folders(folderID) {
		folderAuths = append(folderAuths, folder.Auth)
	}
	colAuth := model.Auth{Type: model.AuthNone}
	if col, ok := a.store.Collection(collectionID); ok {
		colAuth = col.Auth
	}
	return auth.Resolve(req.Auth, folderAuths, colAuth)
}

func (a *App) scriptLists(req model.Request, collectionID, folderID string) (pre, post []string) {
	if collectionID == "" {
		collectionID = a.store.CollectionID(req.ID)
	}
	if folderID == "" && req.ID != "" {
		parent := a.store.ParentID(req.ID)
		if _, ok := a.store.Folder(parent); ok {
			folderID = parent
		}
	}
	folders := a.folders(folderID)
	if col, ok := a.store.Collection(collectionID); ok {
		pre = append(pre, col.Scripts.Prerequest)
	}
	for _, folder := range folders {
		pre = append(pre, folder.Scripts.Prerequest)
	}
	pre = append(pre, req.Scripts.Prerequest)
	post = append(post, req.Scripts.Test)
	for i := len(folders) - 1; i >= 0; i-- {
		post = append(post, folders[i].Scripts.Test)
	}
	if col, ok := a.store.Collection(collectionID); ok {
		post = append(post, col.Scripts.Test)
	}
	return pre, post
}

func (a *App) persistScope(collectionID string, scope vars.Scope) error {
	if env := a.store.ActiveEnvironment(); env != nil {
		env.Variables = vars.ApplyKV(env.Variables, scope.Environment)
		if err := a.store.SaveEnvironment(*env); err != nil {
			return err
		}
	}
	if err := a.store.SaveGlobals(vars.ApplyKV(a.store.Globals.Variables, scope.Global)); err != nil {
		return err
	}
	if collectionID != "" {
		if col, ok := a.store.Collection(collectionID); ok {
			col.Variables = vars.ApplyKV(col.Variables, scope.Collection)
			return a.store.SaveCollection(col)
		}
	}
	return nil
}

func (a *App) captureCookies(result runner.Result) {
	parsed, _ := url.Parse(result.Request.URL)
	cookies := result.Response.Cookies
	for i := range cookies {
		if cookies[i].Domain == "" && parsed != nil {
			cookies[i].Domain = parsed.Hostname()
		}
		if cookies[i].Path == "" {
			cookies[i].Path = "/"
		}
	}
	_ = a.session.SaveCookies(cookies)
}

func (a *App) restoreCookies() error {
	cookies, err := a.session.Cookies()
	if err != nil {
		return err
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		return err
	}
	grouped := map[string][]*http.Cookie{}
	for _, cookie := range cookies {
		host := cookie.Domain
		if host == "" {
			continue
		}
		key := "https://" + host
		grouped[key] = append(grouped[key], &http.Cookie{
			Name: cookie.Name, Value: cookie.Value, Domain: cookie.Domain, Path: cookie.Path,
			Secure: cookie.Secure, HttpOnly: cookie.HTTPOnly,
		})
	}
	for raw, list := range grouped {
		u, err := url.Parse(raw)
		if err != nil {
			continue
		}
		jar.SetCookies(u, list)
	}
	a.jar = jar
	return nil
}

func (a *App) saveSettings() error {
	data, err := json.Marshal(a.settings)
	if err != nil {
		return err
	}
	return a.session.SetState("settings", string(data))
}

func (a *App) OpenURL(target string) error {
	if !allowedSite(target) {
		return fmt.Errorf("link is not allowed")
	}
	return openBrowser(target)
}

func allowedSite(target string) bool {
	parsed, err := url.Parse(target)
	if err != nil || parsed.Scheme != "https" {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	return host == "d2itechnology.com" || host == "www.d2itechnology.com"
}

func openBrowser(target string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", target)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", target)
	default:
		cmd = exec.Command("xdg-open", target)
	}
	return cmd.Start()
}
