package auth

import (
	"crypto/hmac"
	"crypto/md5"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"restman/internal/model"
)

func DigestHeader(username, password, method, uri, challenge, cnonce, nc string) (string, error) {
	params := parseChallenge(challenge)
	realm := params["realm"]
	nonce := params["nonce"]
	if realm == "" || nonce == "" {
		return "", fmt.Errorf("digest challenge is missing realm or nonce")
	}
	qop := params["qop"]
	if strings.Contains(qop, ",") {
		qop = "auth"
	}
	ha1 := md5hex(username + ":" + realm + ":" + password)
	ha2 := md5hex(method + ":" + uri)
	var response string
	if qop != "" {
		response = md5hex(strings.Join([]string{ha1, nonce, nc, cnonce, qop, ha2}, ":"))
	} else {
		response = md5hex(ha1 + ":" + nonce + ":" + ha2)
	}
	parts := []string{
		fmt.Sprintf(`username="%s"`, username),
		fmt.Sprintf(`realm="%s"`, realm),
		fmt.Sprintf(`nonce="%s"`, nonce),
		fmt.Sprintf(`uri="%s"`, uri),
		fmt.Sprintf(`response="%s"`, response),
		fmt.Sprintf(`algorithm="%s"`, or(params["algorithm"], "MD5")),
	}
	if qop != "" {
		parts = append(parts, "qop="+qop, "nc="+nc, fmt.Sprintf(`cnonce="%s"`, cnonce))
	}
	if params["opaque"] != "" {
		parts = append(parts, fmt.Sprintf(`opaque="%s"`, params["opaque"]))
	}
	return "Digest " + strings.Join(parts, ", "), nil
}

func SignAWS(req *http.Request, cred model.AWSAuth, payload []byte, now time.Time) error {
	if cred.AccessKey == "" || cred.SecretKey == "" || cred.Region == "" || cred.Service == "" {
		return fmt.Errorf("aws signature needs access key, secret, region, and service")
	}
	amzDate := now.UTC().Format("20060102T150405Z")
	dateStamp := now.UTC().Format("20060102")
	payloadHash := sha256hex(payload)
	req.Header.Set("x-amz-date", amzDate)
	req.Header.Set("x-amz-content-sha256", payloadHash)
	if cred.SessionToken != "" {
		req.Header.Set("x-amz-security-token", cred.SessionToken)
	}
	if req.Header.Get("Host") == "" {
		req.Header.Set("Host", req.URL.Host)
	}
	signed := []string{"host", "x-amz-content-sha256", "x-amz-date"}
	if cred.SessionToken != "" {
		signed = append(signed, "x-amz-security-token")
	}
	sort.Strings(signed)
	var canonicalHeaders strings.Builder
	for _, key := range signed {
		canonicalHeaders.WriteString(key)
		canonicalHeaders.WriteString(":")
		canonicalHeaders.WriteString(strings.TrimSpace(req.Header.Get(headerName(key))))
		canonicalHeaders.WriteString("\n")
	}
	canonical := strings.Join([]string{
		req.Method,
		canonicalPath(req.URL.EscapedPath()),
		canonicalQuery(req.URL.Query()),
		canonicalHeaders.String(),
		strings.Join(signed, ";"),
		payloadHash,
	}, "\n")
	scope := dateStamp + "/" + cred.Region + "/" + cred.Service + "/aws4_request"
	stringToSign := "AWS4-HMAC-SHA256\n" + amzDate + "\n" + scope + "\n" + sha256hex([]byte(canonical))
	key := hmacSHA256(hmacSHA256(hmacSHA256(hmacSHA256([]byte("AWS4"+cred.SecretKey), dateStamp), cred.Region), cred.Service), "aws4_request")
	signature := hex.EncodeToString(hmacSHA256(key, stringToSign))
	req.Header.Set("Authorization", fmt.Sprintf("AWS4-HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s", cred.AccessKey, scope, strings.Join(signed, ";"), signature))
	return nil
}

func SignOAuth1(req *http.Request, cred model.OAuth1, now time.Time, nonce string) error {
	if cred.ConsumerKey == "" || cred.ConsumerSecret == "" {
		return fmt.Errorf("oauth1 needs a consumer key and secret")
	}
	params := url.Values{}
	for key, values := range req.URL.Query() {
		for _, value := range values {
			params.Add(key, value)
		}
	}
	params.Set("oauth_consumer_key", cred.ConsumerKey)
	params.Set("oauth_nonce", nonce)
	params.Set("oauth_signature_method", "HMAC-SHA1")
	params.Set("oauth_timestamp", fmt.Sprintf("%d", now.Unix()))
	params.Set("oauth_version", "1.0")
	if cred.Token != "" {
		params.Set("oauth_token", cred.Token)
	}
	base := req.Method + "&" + oauthEscape(requestBase(req)) + "&" + oauthEscape(encodeOAuth(params))
	key := oauthEscape(cred.ConsumerSecret) + "&" + oauthEscape(cred.TokenSecret)
	mac := hmac.New(sha1.New, []byte(key))
	mac.Write([]byte(base))
	signature := base64.StdEncoding.EncodeToString(mac.Sum(nil))
	params.Set("oauth_signature", signature)
	header := "OAuth " + oauthHeader(params, cred.Realm)
	req.Header.Set("Authorization", header)
	return nil
}

func parseChallenge(value string) map[string]string {
	value = strings.TrimSpace(strings.TrimPrefix(value, "Digest"))
	out := map[string]string{}
	for _, part := range strings.Split(value, ",") {
		part = strings.TrimSpace(part)
		key, val, ok := strings.Cut(part, "=")
		if !ok {
			continue
		}
		out[strings.ToLower(strings.TrimSpace(key))] = strings.Trim(val, `"`)
	}
	return out
}

func md5hex(value string) string {
	sum := md5.Sum([]byte(value))
	return hex.EncodeToString(sum[:])
}

func sha256hex(payload []byte) string {
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

func hmacSHA256(key []byte, value string) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(value))
	return mac.Sum(nil)
}

func headerName(lower string) string {
	switch lower {
	case "host":
		return "Host"
	default:
		return lower
	}
}

func canonicalPath(path string) string {
	if path == "" {
		return "/"
	}
	return path
}

func canonicalQuery(values url.Values) string {
	if len(values) == 0 {
		return ""
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var parts []string
	for _, key := range keys {
		vals := append([]string{}, values[key]...)
		sort.Strings(vals)
		for _, value := range vals {
			parts = append(parts, url.QueryEscape(key)+"="+url.QueryEscape(value))
		}
	}
	return strings.Join(parts, "&")
}

func requestBase(req *http.Request) string {
	base := *req.URL
	base.RawQuery = ""
	base.Fragment = ""
	return base.String()
}

func encodeOAuth(values url.Values) string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var parts []string
	for _, key := range keys {
		vals := append([]string{}, values[key]...)
		sort.Strings(vals)
		for _, value := range vals {
			parts = append(parts, oauthEscape(key)+"="+oauthEscape(value))
		}
	}
	return strings.Join(parts, "&")
}

func oauthHeader(values url.Values, realm string) string {
	keys := make([]string, 0, len(values))
	for key := range values {
		if strings.HasPrefix(key, "oauth_") {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	parts := []string{}
	if realm != "" {
		parts = append(parts, fmt.Sprintf(`realm="%s"`, oauthEscape(realm)))
	}
	for _, key := range keys {
		parts = append(parts, fmt.Sprintf(`%s="%s"`, key, oauthEscape(values.Get(key))))
	}
	return strings.Join(parts, ", ")
}

func oauthEscape(value string) string {
	escaped := url.QueryEscape(value)
	escaped = strings.ReplaceAll(escaped, "+", "%20")
	escaped = strings.ReplaceAll(escaped, "*", "%2A")
	escaped = strings.ReplaceAll(escaped, "%7E", "~")
	return escaped
}

func or(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func randomNonce() string {
	buf := make([]byte, 8)
	_, _ = rand.Read(buf)
	return hex.EncodeToString(buf)
}
