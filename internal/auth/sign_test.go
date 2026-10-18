package auth

import (
	"net/http"
	"testing"
	"time"

	"restman/internal/model"
)

func TestDigestHeader(t *testing.T) {
	header, err := DigestHeader("ada", "secret", "GET", "/pets", `Digest realm="api", nonce="abc", qop="auth"`, "nonce", "00000001")
	if err != nil {
		t.Fatal(err)
	}
	if header == "" || header == "Digest " {
		t.Fatal(header)
	}
	again, err := DigestHeader("ada", "secret", "GET", "/pets", `Digest realm="api", nonce="abc", qop="auth"`, "nonce", "00000001")
	if err != nil || again != header {
		t.Fatalf("not stable: %s %s", header, again)
	}
}

func TestAWSSignatureStable(t *testing.T) {
	req, err := http.NewRequest(http.MethodGet, "https://example.test/pets?id=1", nil)
	if err != nil {
		t.Fatal(err)
	}
	cred := model.AWSAuth{AccessKey: "AKID", SecretKey: "secret", Region: "us-east-1", Service: "execute-api"}
	when := time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)
	if err := SignAWS(req, cred, nil, when); err != nil {
		t.Fatal(err)
	}
	if req.Header.Get("Authorization") == "" || req.Header.Get("x-amz-date") != "20200102T030405Z" {
		t.Fatal(req.Header)
	}
}
