package curl

import (
	"encoding/base64"
	"fmt"
	"strings"

	"restman/internal/model"
)

func FormatLanguage(lang string, req model.Request) string {
	switch strings.ToLower(strings.TrimSpace(lang)) {
	case "", "curl":
		return Format(req)
	case "javascript", "js", "fetch":
		return formatFetch(req)
	case "node", "axios":
		return formatNode(req)
	case "python":
		return formatPython(req)
	case "go":
		return formatGo(req)
	case "java":
		return formatJava(req)
	case "csharp", "c#":
		return formatCSharp(req)
	case "php":
		return formatPHP(req)
	case "ruby":
		return formatRuby(req)
	case "swift":
		return formatSwift(req)
	case "kotlin":
		return formatKotlin(req)
	case "rust":
		return formatRust(req)
	case "powershell":
		return formatPowerShell(req)
	case "wget":
		return formatWget(req)
	default:
		return Format(req)
	}
}

func snippetHeaders(req model.Request) [][2]string {
	var out [][2]string
	switch req.Auth.Type {
	case model.AuthBasic:
		token := base64.StdEncoding.EncodeToString([]byte(req.Auth.Username + ":" + req.Auth.Password))
		out = append(out, [2]string{"Authorization", "Basic " + token})
	case model.AuthBearer:
		if req.Auth.Token != "" {
			out = append(out, [2]string{"Authorization", "Bearer " + req.Auth.Token})
		}
	case model.AuthAPIKey:
		if req.Auth.Key != "" && !strings.EqualFold(req.Auth.In, "query") {
			out = append(out, [2]string{req.Auth.Key, req.Auth.Value})
		}
	}
	for _, header := range req.Headers {
		if header.Enabled && header.Key != "" {
			out = append(out, [2]string{header.Key, header.Value})
		}
	}
	return out
}

func snippetBody(req model.Request) string {
	switch req.Body.Mode {
	case model.BodyRaw:
		return req.Body.Raw
	case model.BodyURLEncoded:
		parts := make([]string, 0, len(req.Body.URLEncoded))
		for _, field := range req.Body.URLEncoded {
			if field.Enabled && field.Key != "" {
				parts = append(parts, field.Key+"="+field.Value)
			}
		}
		return strings.Join(parts, "&")
	case model.BodyGraphQL:
		if req.Body.GraphQL != nil {
			return req.Body.GraphQL.Query
		}
	}
	return ""
}

func q(value string) string { return fmt.Sprintf("%q", value) }

func formatFetch(req model.Request) string {
	return fmt.Sprintf("await fetch(%s, {\n  method: %s,\n  headers: %s%s\n});", q(req.URL), q(methodOf(req)), jsHeaders(req), bodyField("body", snippetBody(req)))
}

func formatNode(req model.Request) string {
	return fmt.Sprintf("const axios = require(\"axios\");\n\nawait axios({\n  method: %s,\n  url: %s,\n  headers: %s%s\n});", q(methodOf(req)), q(req.URL), jsHeaders(req), bodyField("data", snippetBody(req)))
}

func jsHeaders(req model.Request) string {
	parts := make([]string, 0)
	for _, header := range snippetHeaders(req) {
		parts = append(parts, fmt.Sprintf("%s: %s", q(header[0]), q(header[1])))
	}
	if len(parts) == 0 {
		return "{}"
	}
	return "{ " + strings.Join(parts, ", ") + " }"
}

func bodyField(name, body string) string {
	if body == "" {
		return ""
	}
	return fmt.Sprintf(",\n  %s: %s", name, q(body))
}

func formatPython(req model.Request) string {
	lines := []string{
		"import requests",
		fmt.Sprintf("response = requests.request(%s, %s,", q(methodOf(req)), q(req.URL)),
	}
	if headers := snippetHeaders(req); len(headers) > 0 {
		lines = append(lines, "    headers={"+pairs(headers, q)+"},")
	}
	if body := snippetBody(req); body != "" {
		lines = append(lines, "    data="+q(body)+",")
	}
	lines = append(lines, ")")
	return strings.Join(lines, "\n")
}

func formatGo(req model.Request) string {
	body := "nil"
	if raw := snippetBody(req); raw != "" {
		body = "strings.NewReader(" + q(raw) + ")"
	}
	lines := []string{
		fmt.Sprintf("req, err := http.NewRequest(%s, %s, %s)", q(methodOf(req)), q(req.URL), body),
	}
	for _, header := range snippetHeaders(req) {
		lines = append(lines, fmt.Sprintf("req.Header.Set(%s, %s)", q(header[0]), q(header[1])))
	}
	lines = append(lines, "resp, err := http.DefaultClient.Do(req)")
	return strings.Join(lines, "\n")
}

func formatJava(req model.Request) string {
	lines := []string{
		"HttpRequest.Builder builder = HttpRequest.newBuilder()",
		fmt.Sprintf("    .uri(URI.create(%s))", q(req.URL)),
	}
	for _, header := range snippetHeaders(req) {
		lines = append(lines, fmt.Sprintf("    .header(%s, %s)", q(header[0]), q(header[1])))
	}
	body := snippetBody(req)
	if body == "" && methodOf(req) == "GET" {
		lines = append(lines, "    .GET()")
	} else {
		lines = append(lines, fmt.Sprintf("    .method(%s, HttpRequest.BodyPublishers.ofString(%s))", q(methodOf(req)), q(body)))
	}
	lines = append(lines, "    .build();")
	return strings.Join(lines, "\n")
}

func formatCSharp(req model.Request) string {
	lines := []string{
		"var client = new HttpClient();",
		fmt.Sprintf("var request = new HttpRequestMessage(HttpMethod.Parse(%s), %s);", q(methodOf(req)), q(req.URL)),
	}
	for _, header := range snippetHeaders(req) {
		lines = append(lines, fmt.Sprintf("request.Headers.TryAddWithoutValidation(%s, %s);", q(header[0]), q(header[1])))
	}
	if body := snippetBody(req); body != "" {
		lines = append(lines, fmt.Sprintf("request.Content = new StringContent(%s);", q(body)))
	}
	lines = append(lines, "var response = await client.SendAsync(request);")
	return strings.Join(lines, "\n")
}

func formatPHP(req model.Request) string {
	lines := []string{
		fmt.Sprintf("$ch = curl_init(%s);", q(req.URL)),
		fmt.Sprintf("curl_setopt($ch, CURLOPT_CUSTOMREQUEST, %s);", q(methodOf(req))),
	}
	if headers := snippetHeaders(req); len(headers) > 0 {
		parts := make([]string, 0, len(headers))
		for _, header := range headers {
			parts = append(parts, q(header[0]+": "+header[1]))
		}
		lines = append(lines, "curl_setopt($ch, CURLOPT_HTTPHEADER, ["+strings.Join(parts, ", ")+"]);")
	}
	if body := snippetBody(req); body != "" {
		lines = append(lines, fmt.Sprintf("curl_setopt($ch, CURLOPT_POSTFIELDS, %s);", q(body)))
	}
	lines = append(lines, "curl_exec($ch);")
	return strings.Join(lines, "\n")
}

func formatRuby(req model.Request) string {
	klass := "Get"
	switch methodOf(req) {
	case "POST":
		klass = "Post"
	case "PUT":
		klass = "Put"
	case "PATCH":
		klass = "Patch"
	case "DELETE":
		klass = "Delete"
	case "HEAD":
		klass = "Head"
	}
	lines := []string{
		"require \"net/http\"",
		fmt.Sprintf("uri = URI(%s)", q(req.URL)),
		"http = Net::HTTP.new(uri.host, uri.port)",
		fmt.Sprintf("request = Net::HTTP::%s.new(uri)", klass),
	}
	for _, header := range snippetHeaders(req) {
		lines = append(lines, fmt.Sprintf("request[%s] = %s", q(header[0]), q(header[1])))
	}
	if body := snippetBody(req); body != "" {
		lines = append(lines, "request.body = "+q(body))
	}
	lines = append(lines, "response = http.request(request)")
	return strings.Join(lines, "\n")
}

func formatSwift(req model.Request) string {
	lines := []string{
		fmt.Sprintf("var request = URLRequest(url: URL(string: %s)!)", q(req.URL)),
		fmt.Sprintf("request.httpMethod = %s", q(methodOf(req))),
	}
	for _, header := range snippetHeaders(req) {
		lines = append(lines, fmt.Sprintf("request.setValue(%s, forHTTPHeaderField: %s)", q(header[1]), q(header[0])))
	}
	if body := snippetBody(req); body != "" {
		lines = append(lines, fmt.Sprintf("request.httpBody = %s.data(using: .utf8)", q(body)))
	}
	return strings.Join(lines, "\n")
}

func formatKotlin(req model.Request) string {
	lines := []string{"val request = Request.Builder()", fmt.Sprintf("    .url(%s)", q(req.URL))}
	for _, header := range snippetHeaders(req) {
		lines = append(lines, fmt.Sprintf("    .header(%s, %s)", q(header[0]), q(header[1])))
	}
	body := snippetBody(req)
	switch methodOf(req) {
	case "GET":
		lines = append(lines, "    .get()")
	case "POST":
		lines = append(lines, fmt.Sprintf("    .post(%s.toRequestBody())", q(body)))
	default:
		lines = append(lines, fmt.Sprintf("    .method(%s, %s.toRequestBody())", q(methodOf(req)), q(body)))
	}
	lines = append(lines, "    .build()")
	return strings.Join(lines, "\n")
}

func formatRust(req model.Request) string {
	call := strings.ToLower(methodOf(req))
	switch call {
	case "get", "post", "put", "patch", "delete", "head":
	default:
		call = "request"
	}
	line := "client." + call + "(" + q(req.URL) + ")"
	if call == "request" {
		line = "client.request(" + q(methodOf(req)) + ", " + q(req.URL) + ")"
	}
	lines := []string{"let client = reqwest::Client::new();", "let response = " + line}
	for _, header := range snippetHeaders(req) {
		lines = append(lines, fmt.Sprintf("    .header(%s, %s)", q(header[0]), q(header[1])))
	}
	if body := snippetBody(req); body != "" {
		lines = append(lines, "    .body("+q(body)+")")
	}
	lines = append(lines, "    .send()")
	lines = append(lines, "    .await?;")
	return strings.Join(lines, "\n")
}

func formatPowerShell(req model.Request) string {
	lines := []string{fmt.Sprintf("Invoke-RestMethod -Method %s -Uri %s", methodOf(req), q(req.URL))}
	if headers := snippetHeaders(req); len(headers) > 0 {
		parts := make([]string, 0, len(headers))
		for _, header := range headers {
			parts = append(parts, fmt.Sprintf("%s = %s", header[0], q(header[1])))
		}
		lines = append(lines, "    -Headers @{ "+strings.Join(parts, "; ")+" }")
	}
	if body := snippetBody(req); body != "" {
		lines = append(lines, "    -Body "+q(body))
	}
	return strings.Join(lines, " `\n")
}

func formatWget(req model.Request) string {
	lines := []string{"wget", "  --method=" + methodOf(req)}
	for _, header := range snippetHeaders(req) {
		lines = append(lines, "  --header="+shell(header[0]+": "+header[1]))
	}
	if body := snippetBody(req); body != "" {
		lines = append(lines, "  --body-data="+shell(body))
	}
	lines = append(lines, "  "+shell(req.URL))
	return strings.Join(lines, " \\\n")
}

func methodOf(req model.Request) string {
	if req.Method == "" {
		return "GET"
	}
	return strings.ToUpper(req.Method)
}

func pairs(headers [][2]string, quote func(string) string) string {
	parts := make([]string, 0, len(headers))
	for _, header := range headers {
		parts = append(parts, quote(header[0])+": "+quote(header[1]))
	}
	return strings.Join(parts, ", ")
}
