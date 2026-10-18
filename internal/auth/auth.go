package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"restman/internal/model"
)

func Resolve(request model.Auth, folders []model.Auth, collection model.Auth) model.Auth {
	if request.Type != "" && request.Type != model.AuthInherit {
		return request
	}
	for i := len(folders) - 1; i >= 0; i-- {
		if folders[i].Type != "" && folders[i].Type != model.AuthInherit {
			return folders[i]
		}
	}
	if collection.Type == "" || collection.Type == model.AuthInherit {
		return model.Auth{Type: model.AuthNone}
	}
	return collection
}

func Apply(req *http.Request, auth model.Auth) {
	switch auth.Type {
	case model.AuthBasic:
		req.SetBasicAuth(auth.Username, auth.Password)
	case model.AuthBearer:
		if auth.Token != "" {
			req.Header.Set("Authorization", "Bearer "+auth.Token)
		}
	case model.AuthAPIKey:
		if auth.Key == "" {
			return
		}
		if strings.EqualFold(auth.In, "query") {
			q := req.URL.Query()
			q.Set(auth.Key, auth.Value)
			req.URL.RawQuery = q.Encode()
			return
		}
		req.Header.Set(auth.Key, auth.Value)
	case model.AuthNTLM:
		if auth.Username != "" {
			req.SetBasicAuth(auth.Username, auth.Password)
		}
	case model.AuthOAuth2:
		if auth.OAuth2 != nil && auth.OAuth2.AccessToken != "" {
			tokenType := auth.OAuth2.TokenType
			if tokenType == "" {
				tokenType = "Bearer"
			}
			req.Header.Set("Authorization", tokenType+" "+auth.OAuth2.AccessToken)
		}
	}
}

func FetchToken(ctx context.Context, client *http.Client, oauth *model.OAuth2, open func(string) error) error {
	if oauth == nil {
		return fmt.Errorf("oauth2 settings are missing")
	}
	switch oauth.GrantType {
	case "", "client_credentials":
		return clientCredentials(ctx, client, oauth)
	case "authorization_code":
		return authorizationCode(ctx, client, oauth, open)
	case "password":
		form := url.Values{}
		form.Set("grant_type", "password")
		form.Set("username", oauth.Username)
		form.Set("password", oauth.Password)
		form.Set("client_id", oauth.ClientID)
		if oauth.ClientSecret != "" {
			form.Set("client_secret", oauth.ClientSecret)
		}
		if oauth.Scopes != "" {
			form.Set("scope", oauth.Scopes)
		}
		return readToken(ctx, client, oauth, form)
	case "implicit":
		return implicitGrant(ctx, oauth, open)
	case "device_code":
		return deviceCode(ctx, client, oauth, open)
	default:
		return fmt.Errorf("unsupported oauth2 grant %q", oauth.GrantType)
	}
}

func clientCredentials(ctx context.Context, client *http.Client, oauth *model.OAuth2) error {
	form := url.Values{}
	form.Set("grant_type", "client_credentials")
	form.Set("client_id", oauth.ClientID)
	form.Set("client_secret", oauth.ClientSecret)
	if oauth.Scopes != "" {
		form.Set("scope", oauth.Scopes)
	}
	return readToken(ctx, client, oauth, form)
}

func authorizationCode(ctx context.Context, client *http.Client, oauth *model.OAuth2, open func(string) error) error {
	redirect := oauth.RedirectURL
	var ln net.Listener
	var err error
	if redirect == "" {
		ln, err = net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			return err
		}
		redirect = fmt.Sprintf("http://127.0.0.1:%d/callback", ln.Addr().(*net.TCPAddr).Port)
	} else {
		parsed, err := url.Parse(redirect)
		if err != nil {
			return err
		}
		host := parsed.Host
		if host == "" {
			return fmt.Errorf("redirect URL needs a host")
		}
		ln, err = net.Listen("tcp", host)
		if err != nil {
			return fmt.Errorf("listen on %s: %w", host, err)
		}
	}
	defer ln.Close()

	state := randomState()
	codeCh := make(chan string, 1)
	errCh := make(chan error, 1)
	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("state") != state {
			http.Error(w, "state mismatch", http.StatusBadRequest)
			errCh <- fmt.Errorf("oauth state mismatch")
			return
		}
		if errText := r.URL.Query().Get("error"); errText != "" {
			http.Error(w, errText, http.StatusBadRequest)
			errCh <- fmt.Errorf("oauth error: %s", errText)
			return
		}
		code := r.URL.Query().Get("code")
		if code == "" {
			http.Error(w, "missing code", http.StatusBadRequest)
			errCh <- fmt.Errorf("oauth callback missing code")
			return
		}
		fmt.Fprint(w, "Restman received the authorization code. You can close this tab.")
		codeCh <- code
	})
	server := &http.Server{Handler: mux}
	go server.Serve(ln)
	defer server.Close()

	authURL, err := url.Parse(oauth.AuthURL)
	if err != nil {
		return err
	}
	q := authURL.Query()
	q.Set("response_type", "code")
	q.Set("client_id", oauth.ClientID)
	q.Set("redirect_uri", redirect)
	q.Set("state", state)
	if oauth.Scopes != "" {
		q.Set("scope", oauth.Scopes)
	}
	verifier := ""
	if oauth.UsePKCE {
		verifier = pkceVerifier()
		q.Set("code_challenge", pkceChallenge(verifier))
		q.Set("code_challenge_method", "S256")
	}
	authURL.RawQuery = q.Encode()
	if open != nil {
		if err := open(authURL.String()); err != nil {
			return err
		}
	}

	var code string
	select {
	case code = <-codeCh:
	case err := <-errCh:
		return err
	case <-time.After(2 * time.Minute):
		return fmt.Errorf("oauth login timed out")
	case <-ctx.Done():
		return ctx.Err()
	}

	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", redirect)
	form.Set("client_id", oauth.ClientID)
	if oauth.ClientSecret != "" {
		form.Set("client_secret", oauth.ClientSecret)
	}
	if verifier != "" {
		form.Set("code_verifier", verifier)
	}
	return readToken(ctx, client, oauth, form)
}

func implicitGrant(ctx context.Context, oauth *model.OAuth2, open func(string) error) error {
	redirect, ln, err := listenRedirect(oauth.RedirectURL)
	if err != nil {
		return err
	}
	defer ln.Close()
	tokenCh := make(chan string, 1)
	errCh := make(chan error, 1)
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if token := r.URL.Query().Get("access_token"); token != "" {
			fmt.Fprint(w, "Restman received the token. You can close this tab.")
			tokenCh <- token
			return
		}
		fmt.Fprint(w, `<!doctype html><p>Signing in…</p><script>const p=new URLSearchParams(location.hash.slice(1));if(p.get("access_token"))location.replace("/callback?"+p.toString());else document.body.textContent=p.get("error")||"Missing token";</script>`)
	})
	server := &http.Server{Handler: mux}
	go server.Serve(ln)
	defer server.Close()
	authURL, err := url.Parse(oauth.AuthURL)
	if err != nil {
		return err
	}
	q := authURL.Query()
	q.Set("response_type", "token")
	q.Set("client_id", oauth.ClientID)
	q.Set("redirect_uri", redirect)
	if oauth.Scopes != "" {
		q.Set("scope", oauth.Scopes)
	}
	authURL.RawQuery = q.Encode()
	if open != nil {
		if err := open(authURL.String()); err != nil {
			return err
		}
	}
	select {
	case token := <-tokenCh:
		oauth.AccessToken = token
		if oauth.TokenType == "" {
			oauth.TokenType = "Bearer"
		}
		return nil
	case err := <-errCh:
		return err
	case <-time.After(2 * time.Minute):
		return fmt.Errorf("oauth login timed out")
	case <-ctx.Done():
		return ctx.Err()
	}
}

func deviceCode(ctx context.Context, client *http.Client, oauth *model.OAuth2, open func(string) error) error {
	if client == nil {
		client = http.DefaultClient
	}
	form := url.Values{}
	form.Set("client_id", oauth.ClientID)
	if oauth.Scopes != "" {
		form.Set("scope", oauth.Scopes)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, oauth.AuthURL, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var start struct {
		DeviceCode      string `json:"device_code"`
		UserCode        string `json:"user_code"`
		VerificationURI string `json:"verification_uri"`
		VerifyURI       string `json:"verification_url"`
		Interval        int    `json:"interval"`
		Error           string `json:"error"`
	}
	if err := json.Unmarshal(body, &start); err != nil {
		return fmt.Errorf("device code: %w", err)
	}
	if start.Error != "" {
		return fmt.Errorf("device code: %s", start.Error)
	}
	page := start.VerificationURI
	if page == "" {
		page = start.VerifyURI
	}
	if open != nil && page != "" {
		_ = open(page)
	}
	if start.Interval <= 0 {
		start.Interval = 5
	}
	deadline := time.Now().Add(2 * time.Minute)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Duration(start.Interval) * time.Second):
		}
		poll := url.Values{}
		poll.Set("grant_type", "urn:ietf:params:oauth:grant-type:device_code")
		poll.Set("device_code", start.DeviceCode)
		poll.Set("client_id", oauth.ClientID)
		err := readToken(ctx, client, oauth, poll)
		if err == nil {
			return nil
		}
		if strings.Contains(err.Error(), "authorization_pending") || strings.Contains(err.Error(), "slow_down") {
			continue
		}
		return err
	}
	return fmt.Errorf("device login timed out")
}

func listenRedirect(redirect string) (string, net.Listener, error) {
	if redirect == "" {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			return "", nil, err
		}
		return fmt.Sprintf("http://127.0.0.1:%d/callback", ln.Addr().(*net.TCPAddr).Port), ln, nil
	}
	parsed, err := url.Parse(redirect)
	if err != nil {
		return "", nil, err
	}
	if parsed.Host == "" {
		return "", nil, fmt.Errorf("redirect URL needs a host")
	}
	ln, err := net.Listen("tcp", parsed.Host)
	if err != nil {
		return "", nil, fmt.Errorf("listen on %s: %w", parsed.Host, err)
	}
	return redirect, ln, nil
}

func pkceVerifier() string {
	buf := make([]byte, 32)
	_, _ = rand.Read(buf)
	return base64.RawURLEncoding.EncodeToString(buf)
}

func pkceChallenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func readToken(ctx context.Context, client *http.Client, oauth *model.OAuth2, form url.Values) error {
	if client == nil {
		client = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, oauth.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 300 {
		return fmt.Errorf("token endpoint returned %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	var payload struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		TokenType    string `json:"token_type"`
		Error        string `json:"error"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return fmt.Errorf("token response: %w", err)
	}
	if payload.Error != "" {
		return fmt.Errorf("token error: %s", payload.Error)
	}
	if payload.AccessToken == "" {
		return fmt.Errorf("token response did not include an access token")
	}
	oauth.AccessToken = payload.AccessToken
	if payload.RefreshToken != "" {
		oauth.RefreshToken = payload.RefreshToken
	}
	if payload.TokenType != "" {
		oauth.TokenType = payload.TokenType
	}
	return nil
}

func randomState() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
