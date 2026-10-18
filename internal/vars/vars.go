package vars

import (
	"fmt"
	"math/rand"
	"regexp"
	"strconv"
	"strings"
	"time"

	"restman/internal/id"
	"restman/internal/model"
)

type Scope struct {
	Request     map[string]string
	Environment map[string]string
	Folder      map[string]string
	Collection  map[string]string
	Global      map[string]string
	Data        map[string]string
}

var token = regexp.MustCompile(`\{\{([^{}]+)\}\}`)

func FromKV(items []model.KeyValue) map[string]string {
	out := map[string]string{}
	for _, item := range items {
		if !item.Enabled || item.Key == "" {
			continue
		}
		if _, exists := out[item.Key]; exists {
			continue
		}
		out[item.Key] = item.Value
	}
	return out
}

func ApplyKV(items []model.KeyValue, values map[string]string) []model.KeyValue {
	seen := map[string]bool{}
	out := make([]model.KeyValue, 0, len(items))
	for _, item := range items {
		if item.Key == "" {
			continue
		}
		if v, ok := values[item.Key]; ok {
			item.Value = v
			seen[item.Key] = true
		}
		out = append(out, item)
	}
	for key, value := range values {
		if seen[key] {
			continue
		}
		out = append(out, model.KeyValue{Key: key, Value: value, Enabled: true})
	}
	return out
}

func Lookup(name string, scope Scope) (string, bool) {
	if v, ok := scope.Request[name]; ok {
		return v, true
	}
	if v, ok := scope.Data[name]; ok {
		return v, true
	}
	if v, ok := scope.Environment[name]; ok {
		return v, true
	}
	if v, ok := scope.Folder[name]; ok {
		return v, true
	}
	if v, ok := scope.Collection[name]; ok {
		return v, true
	}
	if v, ok := scope.Global[name]; ok {
		return v, true
	}
	if v, ok := dynamic(name); ok {
		return v, true
	}
	return "", false
}

func Substitute(input string, scope Scope) string {
	current := input
	for i := 0; i < 5; i++ {
		next := token.ReplaceAllStringFunc(current, func(match string) string {
			name := strings.TrimSpace(token.FindStringSubmatch(match)[1])
			if value, ok := Lookup(name, scope); ok {
				return value
			}
			return match
		})
		if next == current {
			break
		}
		current = next
	}
	return current
}

func dynamic(name string) (string, bool) {
	switch name {
	case "$guid":
		return id.New(), true
	case "$timestamp":
		return strconv.FormatInt(time.Now().Unix(), 10), true
	case "$isoTimestamp":
		return time.Now().UTC().Format(time.RFC3339), true
	case "$randomInt":
		return fmt.Sprintf("%d", rand.Intn(1000)), true
	default:
		return "", false
	}
}
