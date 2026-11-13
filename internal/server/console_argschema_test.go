package server

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestArgSchemaParse(t *testing.T) {
	for _, c := range []struct {
		name, schema, wantErr string
	}{
		{"simple", `{"type":"object","properties":{"a":{"type":"string"},"n":{"type":"integer"}},"required":["a"]}`, ""},
		{"list of text", `{"type":"object","properties":{"fields":{"type":"array","items":{"type":"string","enum":["id","name"]}}}}`, ""},
		{"list without items", `{"type":"object","properties":{"f":{"type":"array"}}}`, "a list of something other"},
		{"list of objects", `{"type":"object","properties":{"f":{"type":"array","items":{"type":"object"}}}}`, "a list of something other"},
		{"list of lists", `{"type":"object","properties":{"f":{"type":"array","items":{"type":"array","items":{"type":"string"}}}}}`, "a list of something other"},
		{"object argument", `{"type":"object","properties":{"o":{"type":"object"}}}`, "unsupported type"},
		{"undeclared required", `{"type":"object","properties":{},"required":["x"]}`, "not declared"},
		{"not an object", `{"type":"string"}`, "must be"},
	} {
		_, err := parseArgSchema(json.RawMessage(c.schema))
		if c.wantErr == "" && err != nil || c.wantErr != "" && (err == nil || !strings.Contains(err.Error(), c.wantErr)) {
			t.Errorf("%s: err = %v, want %q", c.name, err, c.wantErr)
		}
	}
}

func TestArgSchemaCheck(t *testing.T) {
	sc, err := parseArgSchema(json.RawMessage(`{"type":"object","properties":{
		"repo":{"type":"string","maxLength":10},
		"page":{"type":"integer","minimum":1,"maximum":5},
		"state":{"type":"string","enum":["open","closed"]},
		"fields":{"type":"array","maxItems":3,"items":{"type":"string","enum":["id","name","url"]}},
		"ids":{"type":"array","items":{"type":"integer"}}},"required":["repo"]}`))
	if err != nil {
		t.Fatal(err)
	}
	many := make([]any, maxArgItems+1)
	for i := range many {
		many[i] = float64(i)
	}
	for _, c := range []struct {
		name    string
		args    map[string]any
		wantErr string
	}{
		{"fine", map[string]any{"repo": "x", "page": 2.0, "state": "open", "fields": []any{"id", "url"}, "ids": []any{1.0, 2.0}}, ""},
		{"missing required", map[string]any{"page": 2.0}, "is required"},
		{"unknown", map[string]any{"repo": "x", "owner": "y"}, "unknown argument"},
		{"too long", map[string]any{"repo": "abcdefghijk"}, "too long"},
		{"below minimum", map[string]any{"repo": "x", "page": 0.0}, "out of range"},
		{"above maximum", map[string]any{"repo": "x", "page": 6.0}, "out of range"},
		{"not an integer", map[string]any{"repo": "x", "page": 1.5}, "must be a integer"},
		{"not in enum", map[string]any{"repo": "x", "state": "merged"}, "allowed values"},
		{"list is text", map[string]any{"repo": "x", "fields": "id"}, "must be a list"},
		{"item not in enum", map[string]any{"repo": "x", "fields": []any{"id", "secret"}}, `"fields" item 1 is not one of`},
		{"item wrong type", map[string]any{"repo": "x", "ids": []any{1.0, "2"}}, `"ids" item 1 must be a integer`},
		{"object item", map[string]any{"repo": "x", "ids": []any{map[string]any{"a": 1.0}}}, "must be a integer"},
		{"over maxItems", map[string]any{"repo": "x", "fields": []any{"id", "id", "id", "id"}}, "too many items"},
		{"over the cap", map[string]any{"repo": "x", "ids": many}, "too many items"},
	} {
		err := sc.checkArgs(c.args)
		if c.wantErr == "" && err != nil || c.wantErr != "" && (err == nil || !strings.Contains(err.Error(), c.wantErr)) {
			t.Errorf("%s: err = %v, want %q", c.name, err, c.wantErr)
		}
	}
	if h := argHelp(sc); !strings.Contains(h, `"fields":[string]`) {
		t.Errorf("argHelp = %s", h)
	}
}
