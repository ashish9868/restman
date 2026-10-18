package curl

import (
	"fmt"
	"strings"

	"restman/internal/model"
)

func Parse(input string) (model.Request, error) {
	tokens, err := split(input)
	if err != nil {
		return model.Request{}, err
	}
	if len(tokens) == 0 || tokens[0] != "curl" {
		return model.Request{}, fmt.Errorf("command must start with curl")
	}
	req := model.Request{
		Name: "Imported cURL", Protocol: model.ProtocolHTTP, Method: "GET",
		Auth: model.Auth{Type: model.AuthNone}, Body: model.Body{Mode: model.BodyNone},
	}
	var data []string
	get := false
	for i := 1; i < len(tokens); i++ {
		token := tokens[i]
		take := func() (string, error) {
			i++
			if i >= len(tokens) {
				return "", fmt.Errorf("%s needs a value", token)
			}
			return tokens[i], nil
		}
		switch token {
		case "-X", "--request":
			method, err := take()
			if err != nil {
				return model.Request{}, err
			}
			req.Method = strings.ToUpper(method)
		case "-H", "--header":
			header, err := take()
			if err != nil {
				return model.Request{}, err
			}
			key, value, _ := strings.Cut(header, ":")
			req.Headers = append(req.Headers, model.KeyValue{Key: strings.TrimSpace(key), Value: strings.TrimSpace(value), Enabled: true})
		case "-d", "--data", "--data-raw", "--data-binary", "--data-urlencode":
			body, err := take()
			if err != nil {
				return model.Request{}, err
			}
			data = append(data, body)
		case "-u", "--user":
			user, err := take()
			if err != nil {
				return model.Request{}, err
			}
			name, pass, _ := strings.Cut(user, ":")
			req.Auth = model.Auth{Type: model.AuthBasic, Username: name, Password: pass}
		case "-G", "--get":
			get = true
		case "--url":
			raw, err := take()
			if err != nil {
				return model.Request{}, err
			}
			req.URL = raw
		default:
			if strings.HasPrefix(token, "-") {
				continue
			}
			if req.URL == "" {
				req.URL = token
			}
		}
	}
	if req.URL == "" {
		return model.Request{}, fmt.Errorf("curl command has no URL")
	}
	if len(data) > 0 {
		req.Body = model.Body{Mode: model.BodyRaw, Language: "text", Raw: strings.Join(data, "&")}
		if req.Method == "GET" && !get {
			req.Method = "POST"
		}
	}
	req.Normalize()
	return req, nil
}

func Format(req model.Request) string {
	lines := []string{"curl " + shell(req.URL)}
	if req.Method != "" && req.Method != "GET" {
		lines = append(lines, "  -X "+req.Method)
	}
	if req.Auth.Type == model.AuthBasic {
		lines = append(lines, "  -u "+shell(req.Auth.Username+":"+req.Auth.Password))
	}
	if req.Auth.Type == model.AuthBearer && req.Auth.Token != "" {
		lines = append(lines, "  -H "+shell("Authorization: Bearer "+req.Auth.Token))
	}
	for _, header := range req.Headers {
		if header.Enabled && header.Key != "" {
			lines = append(lines, "  -H "+shell(header.Key+": "+header.Value))
		}
	}
	if req.Body.Mode == model.BodyRaw && req.Body.Raw != "" {
		lines = append(lines, "  --data-raw "+shell(req.Body.Raw))
	}
	return strings.Join(lines, " \\\n")
}

func split(input string) ([]string, error) {
	var tokens []string
	var current strings.Builder
	var quote rune
	escaped := false
	flush := func() {
		if current.Len() > 0 || quote != 0 {
			tokens = append(tokens, current.String())
			current.Reset()
		}
	}
	for _, r := range input {
		switch {
		case escaped:
			current.WriteRune(r)
			escaped = false
		case r == '\\' && quote != '\'':
			escaped = true
		case quote != 0:
			if r == quote {
				quote = 0
				flush()
				continue
			}
			current.WriteRune(r)
		case r == '\'' || r == '"':
			quote = r
		case r == ' ' || r == '\n' || r == '\t':
			flush()
		default:
			current.WriteRune(r)
		}
	}
	if quote != 0 {
		return nil, fmt.Errorf("unclosed quote in curl command")
	}
	flush()
	return tokens, nil
}

func shell(value string) string {
	if strings.ContainsAny(value, " \t\n'\"\\$") {
		return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
	}
	return value
}
