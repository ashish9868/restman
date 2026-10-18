package id

import (
	"crypto/rand"
	"fmt"
	"regexp"
	"strings"
)

func New() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

var nonSlug = regexp.MustCompile(`[^a-z0-9]+`)

func Slug(name string) string {
	s := strings.ToLower(strings.TrimSpace(name))
	s = strings.Trim(nonSlug.ReplaceAllString(s, "-"), "-")
	if s == "" {
		return "item"
	}
	return s
}
