package vars

import "testing"

func TestSubstitutePrecedence(t *testing.T) {
	scope := Scope{
		Request:     map[string]string{"name": "request"},
		Environment: map[string]string{"name": "env", "base": "https://api.test"},
		Collection:  map[string]string{"name": "col", "path": "pets"},
		Global:      map[string]string{"name": "global"},
	}
	got := Substitute("{{base}}/{{path}}?who={{name}}", scope)
	if got != "https://api.test/pets?who=request" {
		t.Fatalf("got %s", got)
	}
}

func TestDynamicGuid(t *testing.T) {
	got := Substitute("{{$guid}}", Scope{})
	if len(got) < 32 {
		t.Fatalf("guid %s", got)
	}
}

func TestUnknownLeftInPlace(t *testing.T) {
	got := Substitute("{{missing}}", Scope{})
	if got != "{{missing}}" {
		t.Fatalf("got %s", got)
	}
}
