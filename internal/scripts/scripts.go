package scripts

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/dop251/goja"

	"restman/internal/model"
	"restman/internal/runner"
	"restman/internal/vars"
)

const maxDepth = 3
const timeout = 5 * time.Second

const prelude = `
function __expect(actual) {
  function fail(message) { throw new Error(message); }
  return {
    to: {
      equal: function (expected) {
        if (actual !== expected) fail("expected " + JSON.stringify(expected) + " but got " + JSON.stringify(actual));
      },
      eql: function (expected) {
        if (JSON.stringify(actual) !== JSON.stringify(expected)) fail("expected " + JSON.stringify(expected) + " but got " + JSON.stringify(actual));
      },
      include: function (part) {
        if (typeof actual === "string") {
          if (actual.indexOf(part) === -1) fail("expected " + JSON.stringify(actual) + " to include " + JSON.stringify(part));
          return;
        }
        if (Object.prototype.toString.call(actual) === "[object Array]") {
          for (var i = 0; i < actual.length; i++) if (actual[i] === part) return;
          fail("expected array to include " + JSON.stringify(part));
          return;
        }
        fail("include expects a string or array");
      },
      be: {
        above: function (n) { if (!(actual > n)) fail("expected " + actual + " to be above " + n); },
        below: function (n) { if (!(actual < n)) fail("expected " + actual + " to be below " + n); },
        ok: function () { if (!actual) fail("expected a truthy value"); }
      },
      have: {
        property: function (name) {
          if (actual == null || !Object.prototype.hasOwnProperty.call(actual, name)) fail("expected property " + name);
        },
        status: function (code) {
          var status = actual && (actual.code !== undefined ? actual.code : actual.status);
          if (status !== code) fail("expected status " + code + " but got " + status);
        },
        lengthOf: function (n) {
          var len = actual && actual.length;
          if (len !== n) fail("expected length " + n + " but got " + len);
        },
        jsonSchema: function (schema) { __schema(actual, schema || {}); }
      }
    }
  };
}
function __schema(value, schema) {
  function fail(message) { throw new Error(message); }
  if (schema.type === "object" && (value === null || typeof value !== "object" || Array.isArray(value))) fail("expected an object");
  if (schema.type === "array" && !Array.isArray(value)) fail("expected an array");
  if (schema.type === "string" && typeof value !== "string") fail("expected a string");
  if (schema.type === "number" && typeof value !== "number") fail("expected a number");
  var required = schema.required || [];
  for (var i = 0; i < required.length; i++) {
    if (value == null || !Object.prototype.hasOwnProperty.call(value, required[i])) fail("missing required " + required[i]);
  }
  var props = schema.properties || {};
  for (var key in props) {
    if (value && Object.prototype.hasOwnProperty.call(value, key)) __schema(value[key], props[key]);
  }
  if (schema.items && Array.isArray(value)) {
    for (var j = 0; j < value.length; j++) __schema(value[j], schema.items);
  }
  if (schema.enum) {
    var ok = false;
    for (var e = 0; e < schema.enum.length; e++) if (JSON.stringify(schema.enum[e]) === JSON.stringify(value)) ok = true;
    if (!ok) fail("value is not in enum");
  }
  if (schema.minLength != null && typeof value === "string" && value.length < schema.minLength) fail("shorter than minLength");
  if (schema.maxLength != null && typeof value === "string" && value.length > schema.maxLength) fail("longer than maxLength");
  if (schema.minimum != null && typeof value === "number" && value < schema.minimum) fail("below minimum");
  if (schema.maximum != null && typeof value === "number" && value > schema.maximum) fail("above maximum");
}
var __logs = [];
var console = { log: function () { __logs.push(Array.prototype.slice.call(arguments).map(String).join(" ")); } };
`

type Sender func(ctx context.Context, req model.Request, scope vars.Scope, depth int) (runner.Result, error)

type Sandbox struct {
	Sender    Sender
	Depth     int
	Iteration int
	Logs      []string
}

func (s *Sandbox) PreRequest(ctx context.Context, req *model.Request, scope *vars.Scope, sources []string) error {
	for _, source := range sources {
		if strings.TrimSpace(source) == "" {
			continue
		}
		if err := s.run(ctx, source, req, nil, scope, nil); err != nil {
			return err
		}
	}
	return nil
}

func (s *Sandbox) Tests(ctx context.Context, req *model.Request, resp *model.Response, scope *vars.Scope, sources []string) ([]model.TestResult, error) {
	var results []model.TestResult
	for _, source := range sources {
		if strings.TrimSpace(source) == "" {
			continue
		}
		if err := s.run(ctx, source, req, resp, scope, &results); err != nil {
			return results, err
		}
	}
	return results, nil
}

func (s *Sandbox) run(ctx context.Context, source string, req *model.Request, resp *model.Response, scope *vars.Scope, results *[]model.TestResult) error {
	vm := goja.New()
	timer := time.AfterFunc(timeout, func() { vm.Interrupt("script timed out") })
	defer timer.Stop()
	if _, err := vm.RunString(prelude); err != nil {
		return err
	}
	pm := vm.NewObject()
	pm.Set("environment", scopeAPI(scope.Environment))
	pm.Set("globals", scopeAPI(scope.Global))
	pm.Set("collectionVariables", scopeAPI(scope.Collection))
	pm.Set("variables", map[string]any{
		"get": func(name string) string {
			value, _ := vars.Lookup(name, *scope)
			return value
		},
	})
	pm.Set("info", map[string]any{
		"requestName": req.Name,
		"requestId":   req.ID,
		"eventName":   eventName(resp),
	})
	pm.Set("execution", map[string]any{
		"iteration": s.Iteration,
		"location":  []string{req.Name},
		"request":   map[string]any{"id": req.ID, "name": req.Name, "method": req.Method, "url": req.URL},
	})
	cookies := map[string]string{}
	if resp != nil {
		for _, cookie := range resp.Cookies {
			cookies[cookie.Name] = cookie.Value
		}
	}
	pm.Set("cookies", map[string]any{
		"get": func(name string) string { return cookies[name] },
		"has": func(name string) bool { _, ok := cookies[name]; return ok },
	})
	pm.Set("request", requestAPI(vm, req))
	if resp != nil {
		pm.Set("response", responseAPI(vm, resp))
	}
	pm.Set("expect", func(call goja.FunctionCall) goja.Value {
		fn, _ := goja.AssertFunction(vm.Get("__expect"))
		out, _ := fn(goja.Undefined(), call.Arguments...)
		return out
	})
	pm.Set("test", func(name string, fn goja.Value) {
		if results == nil {
			return
		}
		callable, ok := goja.AssertFunction(fn)
		if !ok {
			*results = append(*results, model.TestResult{Name: name, Error: "test callback is not a function"})
			return
		}
		_, err := callable(goja.Undefined())
		if err != nil {
			*results = append(*results, model.TestResult{Name: name, Error: err.Error()})
			return
		}
		*results = append(*results, model.TestResult{Name: name, Pass: true})
	})
	pm.Set("sendRequest", func(call goja.FunctionCall) goja.Value {
		return s.sendRequest(ctx, vm, call, scope)
	})
	if err := vm.Set("pm", pm); err != nil {
		return err
	}
	_, err := vm.RunString(source)
	syncRequest(vm, req)
	s.Logs = append(s.Logs, exportLogs(vm)...)
	if resp != nil {
		resp.Console = append([]string{}, s.Logs...)
	}
	return err
}

func eventName(resp *model.Response) string {
	if resp == nil {
		return "prerequest"
	}
	return "test"
}

func exportLogs(vm *goja.Runtime) []string {
	value := vm.Get("__logs")
	if value == nil || goja.IsUndefined(value) || goja.IsNull(value) {
		return nil
	}
	exported, ok := value.Export().([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(exported))
	for _, item := range exported {
		out = append(out, fmt.Sprint(item))
	}
	return out
}

func (s *Sandbox) sendRequest(ctx context.Context, vm *goja.Runtime, call goja.FunctionCall, scope *vars.Scope) goja.Value {
	fail := func(err error) goja.Value {
		if len(call.Arguments) > 1 {
			if cb, ok := goja.AssertFunction(call.Argument(1)); ok {
				errVal := vm.ToValue(err.Error())
				_, _ = cb(goja.Undefined(), errVal, goja.Null())
			}
		}
		panic(vm.ToValue(err.Error()))
	}
	if s.Sender == nil || s.Depth >= maxDepth {
		return fail(fmt.Errorf("pm.sendRequest depth limit is %d", maxDepth))
	}
	nested := model.Request{Method: "GET", Auth: model.Auth{Type: model.AuthNone}, Body: model.Body{Mode: model.BodyNone}}
	arg := call.Argument(0)
	if arg.ExportType() != nil && arg.ExportType().Kind().String() == "string" || arg.Export() == nil && false {
	}
	switch exported := arg.Export().(type) {
	case string:
		nested.URL = exported
	case map[string]any:
		if url, ok := exported["url"].(string); ok {
			nested.URL = url
		}
		if method, ok := exported["method"].(string); ok && method != "" {
			nested.Method = method
		}
		if body, ok := exported["body"].(string); ok && body != "" {
			nested.Body = model.Body{Mode: model.BodyRaw, Language: "text", Raw: body}
		}
		if headers, ok := exported["header"].([]any); ok {
			for _, item := range headers {
				pair, _ := item.(map[string]any)
				key, _ := pair["key"].(string)
				value, _ := pair["value"].(string)
				nested.Headers = append(nested.Headers, model.KeyValue{Key: key, Value: value, Enabled: true})
			}
		}
	default:
		if text := arg.String(); strings.HasPrefix(text, "http") {
			nested.URL = text
		}
	}
	result, err := s.Sender(ctx, nested, *scope, s.Depth+1)
	if err != nil {
		return fail(err)
	}
	*scope = result.Scope
	obj := responseAPI(vm, &result.Response)
	if len(call.Arguments) > 1 {
		if cb, ok := goja.AssertFunction(call.Argument(1)); ok {
			_, _ = cb(goja.Undefined(), goja.Null(), obj)
		}
	}
	return obj
}

func scopeAPI(values map[string]string) map[string]any {
	if values == nil {
		values = map[string]string{}
	}
	return map[string]any{
		"get":   func(key string) string { return values[key] },
		"set":   func(key, value string) { values[key] = value },
		"unset": func(key string) { delete(values, key) },
	}
}

func requestAPI(vm *goja.Runtime, req *model.Request) *goja.Object {
	obj := vm.NewObject()
	_ = obj.Set("url", req.URL)
	_ = obj.Set("method", req.Method)
	headers := vm.NewObject()
	_ = headers.Set("add", func(item map[string]any) {
		key, _ := item["key"].(string)
		value, _ := item["value"].(string)
		req.Headers = append(req.Headers, model.KeyValue{Key: key, Value: value, Enabled: true})
	})
	_ = headers.Set("upsert", func(item map[string]any) {
		key, _ := item["key"].(string)
		value, _ := item["value"].(string)
		for i := range req.Headers {
			if strings.EqualFold(req.Headers[i].Key, key) {
				req.Headers[i].Value = value
				req.Headers[i].Enabled = true
				return
			}
		}
		req.Headers = append(req.Headers, model.KeyValue{Key: key, Value: value, Enabled: true})
	})
	_ = headers.Set("remove", func(key string) {
		kept := req.Headers[:0]
		for _, header := range req.Headers {
			if !strings.EqualFold(header.Key, key) {
				kept = append(kept, header)
			}
		}
		req.Headers = kept
	})
	_ = obj.Set("headers", headers)
	body := vm.NewObject()
	_ = body.Set("raw", req.Body.Raw)
	_ = obj.Set("body", body)
	return obj
}

func syncRequest(vm *goja.Runtime, req *model.Request) {
	pm := vm.Get("pm")
	if pm == nil || goja.IsUndefined(pm) || goja.IsNull(pm) {
		return
	}
	obj := pm.ToObject(vm)
	request := obj.Get("request").ToObject(vm)
	if url := request.Get("url"); url != nil && !goja.IsUndefined(url) {
		req.URL = url.String()
	}
	if method := request.Get("method"); method != nil && !goja.IsUndefined(method) {
		req.Method = method.String()
	}
	body := request.Get("body")
	if body != nil && !goja.IsUndefined(body) && !goja.IsNull(body) {
		raw := body.ToObject(vm).Get("raw")
		if raw != nil && !goja.IsUndefined(raw) && !goja.IsNull(raw) {
			req.Body.Raw = raw.String()
			if req.Body.Mode == "" || req.Body.Mode == model.BodyNone {
				req.Body.Mode = model.BodyRaw
			}
		}
	}
}

func responseAPI(vm *goja.Runtime, resp *model.Response) *goja.Object {
	obj := vm.NewObject()
	_ = obj.Set("code", resp.Status)
	_ = obj.Set("status", resp.Status)
	_ = obj.Set("responseTime", resp.Timing.TotalMS)
	_ = obj.Set("text", func() string { return resp.Body })
	_ = obj.Set("json", func() any {
		var value any
		if err := json.Unmarshal([]byte(resp.Body), &value); err != nil {
			panic(vm.ToValue(err.Error()))
		}
		return value
	})
	headers := vm.NewObject()
	_ = headers.Set("get", func(key string) string {
		for _, header := range resp.Headers {
			if strings.EqualFold(header.Key, key) {
				return header.Value
			}
		}
		return ""
	})
	_ = obj.Set("headers", headers)
	return obj
}
