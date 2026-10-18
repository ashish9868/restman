package model

import "encoding/json"

const (
	ProtocolHTTP      = "http"
	ProtocolWebSocket = "websocket"

	AuthInherit = "inherit"
	AuthNone    = "none"
	AuthBasic   = "basic"
	AuthBearer  = "bearer"
	AuthAPIKey  = "apikey"
	AuthOAuth2  = "oauth2"
	AuthDigest  = "digest"
	AuthAWS     = "awsv4"
	AuthOAuth1  = "oauth1"
	AuthNTLM    = "ntlm"

	BodyNone       = "none"
	BodyRaw        = "raw"
	BodyURLEncoded = "urlencoded"
	BodyFormData   = "formdata"
	BodyBinary     = "binary"
	BodyGraphQL    = "graphql"
)

type KeyValue struct {
	ID          string `json:"id,omitempty"`
	Key         string `json:"key"`
	Value       string `json:"value"`
	Description string `json:"description,omitempty"`
	Enabled     bool   `json:"enabled"`
}

func (k *KeyValue) UnmarshalJSON(data []byte) error {
	var raw struct {
		ID          string `json:"id"`
		Key         string `json:"key"`
		Value       string `json:"value"`
		Description string `json:"description"`
		Enabled     *bool  `json:"enabled"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	k.ID = raw.ID
	k.Key = raw.Key
	k.Value = raw.Value
	k.Description = raw.Description
	k.Enabled = raw.Enabled == nil || *raw.Enabled
	return nil
}

type FormField struct {
	ID          string `json:"id,omitempty"`
	Key         string `json:"key"`
	Value       string `json:"value"`
	Description string `json:"description,omitempty"`
	Enabled     bool   `json:"enabled"`
	Type        string `json:"type"`
	FilePath    string `json:"filePath,omitempty"`
}

func (f *FormField) UnmarshalJSON(data []byte) error {
	var raw struct {
		ID          string `json:"id"`
		Key         string `json:"key"`
		Value       string `json:"value"`
		Description string `json:"description"`
		Enabled     *bool  `json:"enabled"`
		Type        string `json:"type"`
		FilePath    string `json:"filePath"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	f.ID = raw.ID
	f.Key = raw.Key
	f.Value = raw.Value
	f.Description = raw.Description
	f.Enabled = raw.Enabled == nil || *raw.Enabled
	f.Type = raw.Type
	if f.Type == "" {
		f.Type = "text"
	}
	f.FilePath = raw.FilePath
	return nil
}

type OAuth2 struct {
	GrantType    string `json:"grantType"`
	AuthURL      string `json:"authUrl,omitempty"`
	TokenURL     string `json:"tokenUrl"`
	ClientID     string `json:"clientId"`
	ClientSecret string `json:"clientSecret,omitempty"`
	Scopes       string `json:"scopes,omitempty"`
	RedirectURL  string `json:"redirectUrl,omitempty"`
	AccessToken  string `json:"accessToken,omitempty"`
	RefreshToken string `json:"refreshToken,omitempty"`
	TokenType    string `json:"tokenType,omitempty"`
	UsePKCE      bool   `json:"usePkce,omitempty"`
	Username     string `json:"username,omitempty"`
	Password     string `json:"password,omitempty"`
}

type AWSAuth struct {
	AccessKey    string `json:"accessKey"`
	SecretKey    string `json:"secretKey"`
	Region       string `json:"region"`
	Service      string `json:"service"`
	SessionToken string `json:"sessionToken,omitempty"`
}

type OAuth1 struct {
	ConsumerKey    string `json:"consumerKey"`
	ConsumerSecret string `json:"consumerSecret"`
	Token          string `json:"token,omitempty"`
	TokenSecret    string `json:"tokenSecret,omitempty"`
	Realm          string `json:"realm,omitempty"`
}

type Auth struct {
	Type     string   `json:"type"`
	Username string   `json:"username,omitempty"`
	Password string   `json:"password,omitempty"`
	Token    string   `json:"token,omitempty"`
	Key      string   `json:"key,omitempty"`
	Value    string   `json:"value,omitempty"`
	In       string   `json:"in,omitempty"`
	OAuth2   *OAuth2  `json:"oauth2,omitempty"`
	AWS      *AWSAuth `json:"aws,omitempty"`
	OAuth1   *OAuth1  `json:"oauth1,omitempty"`
}

type RequestOptions struct {
	TimeoutSeconds  int    `json:"timeoutSeconds,omitempty"`
	FollowRedirects string `json:"followRedirects,omitempty"`
	Insecure        string `json:"insecure,omitempty"`
	Cookies         string `json:"cookies,omitempty"`
	HTTPVersion     string `json:"httpVersion,omitempty"`
	Proxy           string `json:"proxy,omitempty"`
	Subprotocols    string `json:"subprotocols,omitempty"`
	PingSeconds     int    `json:"pingSeconds,omitempty"`
}

type Example struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	Status     int      `json:"status"`
	StatusText string   `json:"statusText,omitempty"`
	Headers    []Header `json:"headers,omitempty"`
	Body       string   `json:"body,omitempty"`
}

type GraphQL struct {
	Query         string `json:"query"`
	Variables     string `json:"variables,omitempty"`
	OperationName string `json:"operationName,omitempty"`
}

type Body struct {
	Mode       string      `json:"mode"`
	Raw        string      `json:"raw,omitempty"`
	Language   string      `json:"language,omitempty"`
	URLEncoded []KeyValue  `json:"urlencoded,omitempty"`
	FormData   []FormField `json:"formdata,omitempty"`
	FilePath   string      `json:"filePath,omitempty"`
	GraphQL    *GraphQL    `json:"graphql,omitempty"`
}

type Scripts struct {
	Prerequest string `json:"prerequest,omitempty"`
	Test       string `json:"test,omitempty"`
}

type Request struct {
	ID          string         `json:"id"`
	PostmanID   string         `json:"postmanId,omitempty"`
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Protocol    string         `json:"protocol"`
	Method      string         `json:"method"`
	URL         string         `json:"url"`
	Params      []KeyValue     `json:"params"`
	Headers     []KeyValue     `json:"headers"`
	Auth        Auth           `json:"auth"`
	Body        Body           `json:"body"`
	Scripts     Scripts        `json:"scripts"`
	Variables   []KeyValue     `json:"variables,omitempty"`
	Options     RequestOptions `json:"options,omitempty"`
	Examples    []Example      `json:"examples,omitempty"`
}

func (r *Request) Normalize() {
	if r.Protocol == "" {
		r.Protocol = ProtocolHTTP
	}
	if r.Method == "" {
		r.Method = "GET"
	}
	if r.Auth.Type == "" {
		r.Auth.Type = AuthInherit
	}
	if r.Body.Mode == "" {
		r.Body.Mode = BodyNone
	}
	if r.Params == nil {
		r.Params = []KeyValue{}
	}
	if r.Headers == nil {
		r.Headers = []KeyValue{}
	}
}

type Folder struct {
	ID          string     `json:"id"`
	PostmanID   string     `json:"postmanId,omitempty"`
	Name        string     `json:"name"`
	Description string     `json:"description,omitempty"`
	Auth        Auth       `json:"auth"`
	Variables   []KeyValue `json:"variables,omitempty"`
	Scripts     Scripts    `json:"scripts"`
	Order       []string   `json:"order,omitempty"`
}

type Collection struct {
	ID          string     `json:"id"`
	PostmanID   string     `json:"postmanId,omitempty"`
	Name        string     `json:"name"`
	Description string     `json:"description,omitempty"`
	Auth        Auth       `json:"auth"`
	Variables   []KeyValue `json:"variables,omitempty"`
	Scripts     Scripts    `json:"scripts"`
	Order       []string   `json:"order,omitempty"`
}

type Environment struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	Variables []KeyValue `json:"variables"`
}

type Globals struct {
	Variables []KeyValue `json:"variables"`
}

type WorkspaceMeta struct {
	Name                string `json:"name"`
	ActiveEnvironmentID string `json:"activeEnvironmentId,omitempty"`
}

type Settings struct {
	TimeoutSeconds     int    `json:"timeoutSeconds"`
	FollowRedirects    bool   `json:"followRedirects"`
	InsecureSkipVerify bool   `json:"insecureSkipVerify"`
	ProxyURL           string `json:"proxyUrl,omitempty"`
	ClientCertFile     string `json:"clientCertFile,omitempty"`
	ClientKeyFile      string `json:"clientKeyFile,omitempty"`
	Theme              string `json:"theme"`
	WorkspacePath      string `json:"workspacePath,omitempty"`
}

func DefaultSettings() Settings {
	return Settings{TimeoutSeconds: 30, FollowRedirects: true, Theme: "dark"}
}

type Header struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type Cookie struct {
	Name     string `json:"name"`
	Value    string `json:"value"`
	Domain   string `json:"domain"`
	Path     string `json:"path"`
	Expires  string `json:"expires,omitempty"`
	Secure   bool   `json:"secure"`
	HTTPOnly bool   `json:"httpOnly"`
}

type Timing struct {
	DNSMS     int64 `json:"dnsMs"`
	ConnectMS int64 `json:"connectMs"`
	TLSMS     int64 `json:"tlsMs"`
	TTFBMS    int64 `json:"ttfbMs"`
	TotalMS   int64 `json:"totalMs"`
}

type TestResult struct {
	Name  string `json:"name"`
	Pass  bool   `json:"pass"`
	Error string `json:"error,omitempty"`
}

type Response struct {
	Status      int          `json:"status"`
	StatusText  string       `json:"statusText"`
	Headers     []Header     `json:"headers"`
	Cookies     []Cookie     `json:"cookies"`
	Body        string       `json:"body"`
	Size        int          `json:"size"`
	Truncated   bool         `json:"truncated"`
	Timing      Timing       `json:"timing"`
	Error       string       `json:"error,omitempty"`
	Tests       []TestResult `json:"tests,omitempty"`
	ScriptError string       `json:"scriptError,omitempty"`
	Redirects   []Redirect   `json:"redirects,omitempty"`
	Console     []string     `json:"console,omitempty"`
}

type Redirect struct {
	Status int    `json:"status"`
	URL    string `json:"url"`
}

type TreeNode struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	Kind      string     `json:"kind"`
	Method    string     `json:"method,omitempty"`
	Protocol  string     `json:"protocol,omitempty"`
	URL       string     `json:"url,omitempty"`
	Variables []KeyValue `json:"variables,omitempty"`
	Children  []TreeNode `json:"children,omitempty"`
}

type HistoryEntry struct {
	ID         string `json:"id"`
	SentAt     string `json:"sentAt"`
	Method     string `json:"method"`
	URL        string `json:"url"`
	Status     int    `json:"status"`
	DurationMS int64  `json:"durationMs"`
	RequestID  string `json:"requestId,omitempty"`
	Name       string `json:"name"`
}

type RunResult struct {
	RequestID  string       `json:"requestId"`
	Name       string       `json:"name"`
	Method     string       `json:"method"`
	URL        string       `json:"url"`
	Status     int          `json:"status"`
	DurationMS int64        `json:"durationMs"`
	Error      string       `json:"error,omitempty"`
	Tests      []TestResult `json:"tests"`
}

type WSMessage struct {
	Seq       int    `json:"seq"`
	Direction string `json:"direction"`
	Data      string `json:"data"`
	Binary    bool   `json:"binary"`
	Time      string `json:"time"`
}
