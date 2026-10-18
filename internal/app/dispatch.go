package app

import (
	"encoding/json"
	"fmt"

	"restman/internal/model"
)

type call struct {
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

func (a *App) Dispatch(method string, params json.RawMessage) (any, error) {
	if len(params) == 0 {
		params = []byte(`{}`)
	}
	switch method {
	case "Bootstrap":
		return a.Bootstrap()
	case "OpenWorkspace":
		var in struct {
			Path string `json:"path"`
		}
		if err := json.Unmarshal(params, &in); err != nil {
			return nil, err
		}
		return a.OpenWorkspace(in.Path)
	case "CreateCollection":
		var in struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal(params, &in); err != nil {
			return nil, err
		}
		return a.CreateCollection(in.Name)
	case "CreateFolder":
		var in struct {
			ParentID string `json:"parentId"`
			Name     string `json:"name"`
		}
		if err := json.Unmarshal(params, &in); err != nil {
			return nil, err
		}
		return a.CreateFolder(in.ParentID, in.Name)
	case "CreateRequest":
		var in struct {
			ParentID string `json:"parentId"`
			Name     string `json:"name"`
			Protocol string `json:"protocol"`
		}
		if err := json.Unmarshal(params, &in); err != nil {
			return nil, err
		}
		return a.CreateRequest(in.ParentID, in.Name, in.Protocol)
	case "GetRequest":
		var in struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(params, &in); err != nil {
			return nil, err
		}
		return a.GetRequest(in.ID)
	case "SaveRequest":
		var in struct {
			ParentID string        `json:"parentId"`
			Request  model.Request `json:"request"`
		}
		if err := json.Unmarshal(params, &in); err != nil {
			return nil, err
		}
		return a.SaveRequest(in.ParentID, in.Request)
	case "Delete":
		var in struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(params, &in); err != nil {
			return nil, err
		}
		return a.Delete(in.ID)
	case "DuplicateRequest":
		var in struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(params, &in); err != nil {
			return nil, err
		}
		return a.DuplicateRequest(in.ID)
	case "Move":
		var in struct {
			ID       string `json:"id"`
			ParentID string `json:"parentId"`
			Index    int    `json:"index"`
		}
		if err := json.Unmarshal(params, &in); err != nil {
			return nil, err
		}
		return a.Move(in.ID, in.ParentID, in.Index)
	case "SaveEnvironment":
		var env model.Environment
		if err := json.Unmarshal(params, &env); err != nil {
			return nil, err
		}
		return a.SaveEnvironment(env)
	case "DeleteEnvironment":
		var in struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(params, &in); err != nil {
			return nil, err
		}
		return a.DeleteEnvironment(in.ID)
	case "SetActiveEnvironment":
		var in struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(params, &in); err != nil {
			return nil, err
		}
		return a.SetActiveEnvironment(in.ID)
	case "SaveGlobals":
		var in struct {
			Variables []model.KeyValue `json:"variables"`
		}
		if err := json.Unmarshal(params, &in); err != nil {
			return nil, err
		}
		return a.SaveGlobals(in.Variables)
	case "SaveSettings":
		var settings model.Settings
		if err := json.Unmarshal(params, &settings); err != nil {
			return nil, err
		}
		return a.SaveSettings(settings)
	case "SaveTabs":
		var in struct {
			Tabs string `json:"tabs"`
		}
		if err := json.Unmarshal(params, &in); err != nil {
			return nil, err
		}
		return nil, a.SaveTabs(in.Tabs)
	case "Send":
		var in SendInput
		if err := json.Unmarshal(params, &in); err != nil {
			return nil, err
		}
		return a.Send(in)
	case "Cancel":
		var in struct {
			TabID string `json:"tabId"`
		}
		if err := json.Unmarshal(params, &in); err != nil {
			return nil, err
		}
		a.Cancel(in.TabID)
		return nil, nil
	case "Run":
		var in RunInput
		if err := json.Unmarshal(params, &in); err != nil {
			return nil, err
		}
		return a.Run(in)
	case "Duplicate":
		var in struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(params, &in); err != nil {
			return nil, err
		}
		return a.Duplicate(in.ID)
	case "GetCollection":
		var in struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(params, &in); err != nil {
			return nil, err
		}
		return a.GetCollection(in.ID)
	case "GetFolder":
		var in struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(params, &in); err != nil {
			return nil, err
		}
		return a.GetFolder(in.ID)
	case "SaveCollection":
		var col model.Collection
		if err := json.Unmarshal(params, &col); err != nil {
			return nil, err
		}
		return a.SaveCollection(col)
	case "SaveFolder":
		var folder model.Folder
		if err := json.Unmarshal(params, &folder); err != nil {
			return nil, err
		}
		return a.SaveFolder(folder)
	case "ImportCurl":
		var in struct {
			Command string `json:"command"`
		}
		if err := json.Unmarshal(params, &in); err != nil {
			return nil, err
		}
		return a.ImportCurl(in.Command)
	case "ExportCurl":
		var req model.Request
		if err := json.Unmarshal(params, &req); err != nil {
			return nil, err
		}
		return a.ExportCurl(req), nil
	case "Snippet":
		var in struct {
			Language string        `json:"language"`
			Request  model.Request `json:"request"`
		}
		if err := json.Unmarshal(params, &in); err != nil {
			return nil, err
		}
		return a.Snippet(in.Language, in.Request), nil
	case "Introspect":
		var in struct {
			URL string `json:"url"`
		}
		if err := json.Unmarshal(params, &in); err != nil {
			return nil, err
		}
		return a.Introspect(in.URL)
	case "Authorize":
		var authn model.Auth
		if err := json.Unmarshal(params, &authn); err != nil {
			return nil, err
		}
		return a.Authorize(authn)
	case "ImportPostman":
		var in struct {
			Content string `json:"content"`
		}
		if err := json.Unmarshal(params, &in); err != nil {
			return nil, err
		}
		return a.ImportPostman(in.Content)
	case "ExportCollection":
		var in struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(params, &in); err != nil {
			return nil, err
		}
		return a.ExportCollection(in.ID)
	case "ImportEnvironment":
		var in struct {
			Content string `json:"content"`
		}
		if err := json.Unmarshal(params, &in); err != nil {
			return nil, err
		}
		return a.ImportEnvironment(in.Content)
	case "ExportEnvironment":
		var in struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(params, &in); err != nil {
			return nil, err
		}
		return a.ExportEnvironment(in.ID)
	case "History":
		return a.History(100)
	case "HistoryRequest":
		var in struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(params, &in); err != nil {
			return nil, err
		}
		return a.HistoryRequest(in.ID)
	case "ClearHistory":
		return nil, a.ClearHistory()
	case "DeleteHistory":
		var in struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(params, &in); err != nil {
			return nil, err
		}
		return nil, a.DeleteHistory(in.ID)
	case "SaveCookie":
		var cookie model.Cookie
		if err := json.Unmarshal(params, &cookie); err != nil {
			return nil, err
		}
		return nil, a.SaveCookie(cookie)
	case "Cookies":
		return a.Cookies()
	case "DeleteCookie":
		var in struct {
			Domain string `json:"domain"`
			Path   string `json:"path"`
			Name   string `json:"name"`
		}
		if err := json.Unmarshal(params, &in); err != nil {
			return nil, err
		}
		return nil, a.DeleteCookie(in.Domain, in.Path, in.Name)
	case "WSConnect":
		var in struct {
			TabID       string           `json:"tabId"`
			URL         string           `json:"url"`
			Headers     []model.KeyValue `json:"headers"`
			Protocols   []string         `json:"protocols"`
			PingSeconds int              `json:"pingSeconds"`
		}
		if err := json.Unmarshal(params, &in); err != nil {
			return nil, err
		}
		return nil, a.WSConnect(in.TabID, in.URL, in.Headers, in.Protocols, in.PingSeconds)
	case "WSSend":
		var in struct {
			TabID  string `json:"tabId"`
			Data   string `json:"data"`
			Binary bool   `json:"binary"`
		}
		if err := json.Unmarshal(params, &in); err != nil {
			return nil, err
		}
		return nil, a.WSSend(in.TabID, in.Data, in.Binary)
	case "WSClose":
		var in struct {
			TabID string `json:"tabId"`
		}
		if err := json.Unmarshal(params, &in); err != nil {
			return nil, err
		}
		a.WSClose(in.TabID)
		return nil, nil
	case "WSPoll":
		var in struct {
			TabID string `json:"tabId"`
			After int    `json:"after"`
		}
		if err := json.Unmarshal(params, &in); err != nil {
			return nil, err
		}
		return a.WSPoll(in.TabID, in.After), nil
	default:
		return nil, fmt.Errorf("unknown method %s", method)
	}
}
