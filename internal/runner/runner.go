package runner

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Azure/go-ntlmssp"

	"restman/internal/auth"
	"restman/internal/model"
	"restman/internal/vars"
)

const maxBody = 1 << 20

type Result struct {
	Response model.Response
	Scope    vars.Scope
	Request  model.Request
}

type Options struct {
	Settings model.Settings
	Jar      http.CookieJar
	Scope    vars.Scope
	Auth     model.Auth
	Before   func(req *model.Request, scope *vars.Scope) error
	After    func(req *model.Request, resp *model.Response, scope *vars.Scope) error
	Depth    int
	Send     func(ctx context.Context, req model.Request, scope vars.Scope, depth int) (Result, error)
}

func Execute(ctx context.Context, req model.Request, opt Options) (Result, error) {
	req.Normalize()
	scope := opt.Scope
	if opt.Before != nil {
		if err := opt.Before(&req, &scope); err != nil {
			return Result{Response: model.Response{Error: err.Error()}, Scope: scope, Request: req}, nil
		}
	}
	resolved := substituteRequest(req, scope)
	resolvedAuth := substituteAuth(opt.Auth, scope)
	if resolved.Protocol == model.ProtocolWebSocket {
		return Result{}, fmt.Errorf("websocket requests use the socket connection")
	}
	if strings.TrimSpace(resolved.URL) == "" {
		return Result{Response: model.Response{Error: "URL is empty"}, Scope: scope, Request: resolved}, nil
	}

	httpReq, err := buildRequest(ctx, resolved, resolvedAuth)
	if err != nil {
		return Result{Response: model.Response{Error: err.Error()}, Scope: scope, Request: resolved}, nil
	}
	payload, err := replayable(httpReq)
	if err != nil {
		return Result{Response: model.Response{Error: err.Error()}, Scope: scope, Request: resolved}, nil
	}
	if err := sign(httpReq, resolvedAuth, payload); err != nil {
		return Result{Response: model.Response{Error: err.Error()}, Scope: scope, Request: resolved}, nil
	}
	settings, useJar, version := effective(opt, resolved)
	var hops []model.Redirect
	client, err := newClient(settings, jarFor(opt.Jar, useJar), version, &hops)
	if err != nil {
		return Result{Response: model.Response{Error: err.Error()}, Scope: scope, Request: resolved}, nil
	}
	if resolvedAuth.Type == model.AuthNTLM {
		client.Transport = ntlmssp.Negotiator{RoundTripper: client.Transport}
	}

	start := time.Now()
	var dnsStart, connStart, tlsStart time.Time
	var timing model.Timing
	trace := &httptrace.ClientTrace{
		DNSStart: func(httptrace.DNSStartInfo) { dnsStart = time.Now() },
		DNSDone: func(httptrace.DNSDoneInfo) {
			if !dnsStart.IsZero() {
				timing.DNSMS = time.Since(dnsStart).Milliseconds()
			}
		},
		ConnectStart: func(_, _ string) { connStart = time.Now() },
		ConnectDone: func(_, _ string, _ error) {
			if !connStart.IsZero() {
				timing.ConnectMS = time.Since(connStart).Milliseconds()
			}
		},
		TLSHandshakeStart: func() { tlsStart = time.Now() },
		TLSHandshakeDone: func(_ tls.ConnectionState, _ error) {
			if !tlsStart.IsZero() {
				timing.TLSMS = time.Since(tlsStart).Milliseconds()
			}
		},
		GotFirstResponseByte: func() { timing.TTFBMS = time.Since(start).Milliseconds() },
	}
	httpReq = httpReq.WithContext(httptrace.WithClientTrace(httpReq.Context(), trace))

	resp, err := client.Do(httpReq)
	timing.TotalMS = time.Since(start).Milliseconds()
	if err != nil {
		return Result{Response: model.Response{Error: err.Error(), Timing: timing, Redirects: hops}, Scope: scope, Request: resolved}, nil
	}
	if resolvedAuth.Type == model.AuthDigest && resp.StatusCode == http.StatusUnauthorized {
		challenge := resp.Header.Get("WWW-Authenticate")
		header, digestErr := auth.DigestHeader(resolvedAuth.Username, resolvedAuth.Password, httpReq.Method, httpReq.URL.RequestURI(), challenge, authNonce(), "00000001")
		if digestErr == nil {
			retry, retryErr := buildRequest(ctx, resolved, model.Auth{Type: model.AuthNone})
			if retryErr == nil {
				if _, retryErr = replayable(retry); retryErr == nil {
					retry.Header.Set("Authorization", header)
					next, doErr := client.Do(retry)
					if doErr == nil {
						_, _ = io.Copy(io.Discard, resp.Body)
						resp.Body.Close()
						resp = next
						timing.TotalMS = time.Since(start).Milliseconds()
					}
				}
			}
		}
	}
	if err != nil {
		return Result{Response: model.Response{Error: err.Error(), Timing: timing, Redirects: hops}, Scope: scope, Request: resolved}, nil
	}
	defer resp.Body.Close()

	out := readResponse(resp, timing)
	out.Redirects = hops
	if opt.After != nil {
		if err := opt.After(&resolved, &out, &scope); err != nil {
			out.ScriptError = err.Error()
		}
	}
	return Result{Response: out, Scope: scope, Request: resolved}, nil
}

func effective(opt Options, req model.Request) (model.Settings, bool, string) {
	settings := opt.Settings
	options := req.Options
	if options.TimeoutSeconds > 0 {
		settings.TimeoutSeconds = options.TimeoutSeconds
	}
	switch options.FollowRedirects {
	case "yes":
		settings.FollowRedirects = true
	case "no":
		settings.FollowRedirects = false
	}
	switch options.Insecure {
	case "yes":
		settings.InsecureSkipVerify = true
	case "no":
		settings.InsecureSkipVerify = false
	}
	switch options.Proxy {
	case "none":
		settings.ProxyURL = ""
	default:
		if strings.HasPrefix(options.Proxy, "http") {
			settings.ProxyURL = options.Proxy
		}
	}
	return settings, options.Cookies != "no", options.HTTPVersion
}

func jarFor(jar http.CookieJar, use bool) http.CookieJar {
	if !use {
		return nil
	}
	return jar
}

type hopTransport struct {
	base http.RoundTripper
	hops *[]model.Redirect
}

func (t hopTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := t.base.RoundTrip(req)
	if err == nil && resp.StatusCode >= 300 && resp.StatusCode < 400 {
		*t.hops = append(*t.hops, model.Redirect{Status: resp.StatusCode, URL: resp.Header.Get("Location")})
	}
	return resp, err
}

func newClient(settings model.Settings, jar http.CookieJar, version string, hops *[]model.Redirect) (*http.Client, error) {
	timeout := time.Duration(settings.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: settings.InsecureSkipVerify} //nolint:gosec
	if version == "1.1" {
		transport.ForceAttemptHTTP2 = false
		transport.TLSNextProto = map[string]func(string, *tls.Conn) http.RoundTripper{}
	}
	if settings.ProxyURL != "" {
		proxyURL, err := url.Parse(settings.ProxyURL)
		if err != nil {
			return nil, fmt.Errorf("proxy URL: %w", err)
		}
		transport.Proxy = http.ProxyURL(proxyURL)
	}
	if settings.ClientCertFile != "" && settings.ClientKeyFile != "" {
		cert, err := tls.LoadX509KeyPair(settings.ClientCertFile, settings.ClientKeyFile)
		if err != nil {
			return nil, fmt.Errorf("client certificate: %w", err)
		}
		transport.TLSClientConfig.Certificates = []tls.Certificate{cert}
	}
	client := &http.Client{Transport: hopTransport{base: transport, hops: hops}, Jar: jar, Timeout: timeout}
	if !settings.FollowRedirects {
		client.CheckRedirect = func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		}
	}
	return client, nil
}

func replayable(req *http.Request) ([]byte, error) {
	if req.Body == nil {
		return nil, nil
	}
	payload, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}
	req.Body = io.NopCloser(bytes.NewReader(payload))
	req.ContentLength = int64(len(payload))
	req.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(payload)), nil
	}
	return payload, nil
}

func sign(req *http.Request, authn model.Auth, payload []byte) error {
	switch authn.Type {
	case model.AuthAWS:
		if authn.AWS == nil {
			return fmt.Errorf("aws signature settings are missing")
		}
		return auth.SignAWS(req, *authn.AWS, payload, time.Now())
	case model.AuthOAuth1:
		if authn.OAuth1 == nil {
			return fmt.Errorf("oauth1 settings are missing")
		}
		return auth.SignOAuth1(req, *authn.OAuth1, time.Now(), authNonce())
	default:
		return nil
	}
}

func authNonce() string {
	return fmt.Sprintf("%x", time.Now().UnixNano())
}

func buildRequest(ctx context.Context, req model.Request, authn model.Auth) (*http.Request, error) {
	method := strings.ToUpper(req.Method)
	target, err := url.Parse(req.URL)
	if err != nil {
		return nil, fmt.Errorf("URL: %w", err)
	}
	q := url.Values{}
	queryParams := false
	for _, param := range req.Params {
		if !param.Enabled || param.Key == "" {
			continue
		}
		if strings.Contains(target.Path, ":"+param.Key) {
			target.Path = strings.ReplaceAll(target.Path, ":"+param.Key, url.PathEscape(param.Value))
			continue
		}
		queryParams = true
		q.Add(param.Key, param.Value)
	}
	if queryParams {
		target.RawQuery = q.Encode()
	}

	body, contentType, err := bodyReader(req)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, method, target.String(), body)
	if err != nil {
		return nil, err
	}
	for _, header := range req.Headers {
		if header.Enabled && header.Key != "" {
			httpReq.Header.Add(header.Key, header.Value)
		}
	}
	if contentType != "" && httpReq.Header.Get("Content-Type") == "" {
		httpReq.Header.Set("Content-Type", contentType)
	}
	auth.Apply(httpReq, authn)
	return httpReq, nil
}

func bodyReader(req model.Request) (io.Reader, string, error) {
	switch req.Body.Mode {
	case "", model.BodyNone:
		return nil, "", nil
	case model.BodyRaw:
		ctype := ""
		switch req.Body.Language {
		case "json", "":
			if strings.TrimSpace(req.Body.Raw) != "" {
				ctype = "application/json"
			}
		case "xml":
			ctype = "application/xml"
		case "html":
			ctype = "text/html"
		case "javascript":
			ctype = "application/javascript"
		default:
			ctype = "text/plain"
		}
		if req.Body.Language == "text" {
			ctype = "text/plain"
		}
		return strings.NewReader(req.Body.Raw), ctype, nil
	case model.BodyURLEncoded:
		form := url.Values{}
		for _, field := range req.Body.URLEncoded {
			if field.Enabled && field.Key != "" {
				form.Add(field.Key, field.Value)
			}
		}
		return strings.NewReader(form.Encode()), "application/x-www-form-urlencoded", nil
	case model.BodyFormData:
		buf := &bytes.Buffer{}
		writer := multipart.NewWriter(buf)
		for _, field := range req.Body.FormData {
			if !field.Enabled || field.Key == "" {
				continue
			}
			if field.Type == "file" {
				path := field.FilePath
				if path == "" {
					path = field.Value
				}
				file, err := os.Open(path)
				if err != nil {
					return nil, "", fmt.Errorf("form file %s: %w", field.Key, err)
				}
				part, err := writer.CreateFormFile(field.Key, filepath.Base(file.Name()))
				if err != nil {
					file.Close()
					return nil, "", err
				}
				_, copyErr := io.Copy(part, file)
				file.Close()
				if copyErr != nil {
					return nil, "", copyErr
				}
				continue
			}
			if err := writer.WriteField(field.Key, field.Value); err != nil {
				return nil, "", err
			}
		}
		if err := writer.Close(); err != nil {
			return nil, "", err
		}
		return buf, writer.FormDataContentType(), nil
	case model.BodyBinary:
		data, err := os.ReadFile(req.Body.FilePath)
		if err != nil {
			return nil, "", fmt.Errorf("binary body: %w", err)
		}
		return bytes.NewReader(data), "application/octet-stream", nil
	case model.BodyGraphQL:
		payload := map[string]any{}
		if req.Body.GraphQL != nil {
			payload["query"] = req.Body.GraphQL.Query
			if strings.TrimSpace(req.Body.GraphQL.Variables) != "" {
				var vars any
				if err := json.Unmarshal([]byte(req.Body.GraphQL.Variables), &vars); err != nil {
					return nil, "", fmt.Errorf("graphql variables: %w", err)
				}
				payload["variables"] = vars
			}
			if req.Body.GraphQL.OperationName != "" {
				payload["operationName"] = req.Body.GraphQL.OperationName
			}
		}
		data, err := json.Marshal(payload)
		if err != nil {
			return nil, "", err
		}
		return bytes.NewReader(data), "application/json", nil
	default:
		return nil, "", fmt.Errorf("unknown body mode %q", req.Body.Mode)
	}
}

func readResponse(resp *http.Response, timing model.Timing) model.Response {
	limited := io.LimitReader(resp.Body, maxBody+1)
	data, _ := io.ReadAll(limited)
	truncated := len(data) > maxBody
	if truncated {
		data = data[:maxBody]
	}
	body := string(data)
	if !utf8.Valid(data) {
		body = fmt.Sprintf("[binary response, %d bytes shown]", len(data))
	}
	headers := make([]model.Header, 0, len(resp.Header))
	for key, values := range resp.Header {
		for _, value := range values {
			headers = append(headers, model.Header{Key: key, Value: value})
		}
	}
	cookies := make([]model.Cookie, 0, len(resp.Cookies()))
	for _, cookie := range resp.Cookies() {
		expires := ""
		if !cookie.Expires.IsZero() {
			expires = cookie.Expires.UTC().Format(time.RFC3339)
		}
		cookies = append(cookies, model.Cookie{
			Name: cookie.Name, Value: cookie.Value, Domain: cookie.Domain, Path: cookie.Path,
			Expires: expires, Secure: cookie.Secure, HTTPOnly: cookie.HttpOnly,
		})
	}
	return model.Response{
		Status: resp.StatusCode, StatusText: resp.Status, Headers: headers, Cookies: cookies,
		Body: body, Size: len(data), Truncated: truncated, Timing: timing,
	}
}

func substituteRequest(req model.Request, scope vars.Scope) model.Request {
	req.URL = vars.Substitute(req.URL, scope)
	req.Method = vars.Substitute(req.Method, scope)
	for i := range req.Params {
		req.Params[i].Key = vars.Substitute(req.Params[i].Key, scope)
		req.Params[i].Value = vars.Substitute(req.Params[i].Value, scope)
	}
	for i := range req.Headers {
		req.Headers[i].Key = vars.Substitute(req.Headers[i].Key, scope)
		req.Headers[i].Value = vars.Substitute(req.Headers[i].Value, scope)
	}
	req.Body.Raw = vars.Substitute(req.Body.Raw, scope)
	req.Body.FilePath = vars.Substitute(req.Body.FilePath, scope)
	for i := range req.Body.URLEncoded {
		req.Body.URLEncoded[i].Key = vars.Substitute(req.Body.URLEncoded[i].Key, scope)
		req.Body.URLEncoded[i].Value = vars.Substitute(req.Body.URLEncoded[i].Value, scope)
	}
	for i := range req.Body.FormData {
		req.Body.FormData[i].Key = vars.Substitute(req.Body.FormData[i].Key, scope)
		req.Body.FormData[i].Value = vars.Substitute(req.Body.FormData[i].Value, scope)
		req.Body.FormData[i].FilePath = vars.Substitute(req.Body.FormData[i].FilePath, scope)
	}
	if req.Body.GraphQL != nil {
		req.Body.GraphQL.Query = vars.Substitute(req.Body.GraphQL.Query, scope)
		req.Body.GraphQL.Variables = vars.Substitute(req.Body.GraphQL.Variables, scope)
		req.Body.GraphQL.OperationName = vars.Substitute(req.Body.GraphQL.OperationName, scope)
	}
	return req
}

func substituteAuth(authn model.Auth, scope vars.Scope) model.Auth {
	authn.Username = vars.Substitute(authn.Username, scope)
	authn.Password = vars.Substitute(authn.Password, scope)
	authn.Token = vars.Substitute(authn.Token, scope)
	authn.Key = vars.Substitute(authn.Key, scope)
	authn.Value = vars.Substitute(authn.Value, scope)
	if authn.OAuth2 != nil {
		copied := *authn.OAuth2
		copied.AccessToken = vars.Substitute(copied.AccessToken, scope)
		copied.TokenURL = vars.Substitute(copied.TokenURL, scope)
		copied.AuthURL = vars.Substitute(copied.AuthURL, scope)
		copied.ClientID = vars.Substitute(copied.ClientID, scope)
		copied.ClientSecret = vars.Substitute(copied.ClientSecret, scope)
		authn.OAuth2 = &copied
	}
	return authn
}
