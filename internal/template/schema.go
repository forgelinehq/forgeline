package template

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"
)

// Schema is the JSON Schema subset a template's inputs use (design note 040). Anything outside it makes the template
// invalid, so the canvas form and Forgeline's check never miss a rule the template's code relies on.
type Schema struct {
	Type       string           `json:"type"`
	Properties map[string]*Prop `json:"properties"`
	Required   []string         `json:"required"`
	Order      []string         `json:"-"` // property names in file order, for the form
}

// Prop is one input.
type Prop struct {
	Type                 string   `json:"type"`
	Title                string   `json:"title,omitempty"`
	Description          string   `json:"description,omitempty"`
	Default              any      `json:"default,omitempty"`
	Enum                 []any    `json:"enum,omitempty"`
	Pattern              string   `json:"pattern,omitempty"`
	MaxLength            int      `json:"maxLength,omitempty"`
	Minimum              *float64 `json:"minimum,omitempty"`
	Maximum              *float64 `json:"maximum,omitempty"`
	AdditionalProperties *Prop    `json:"additionalProperties,omitempty"` // object: the schema of every value
	MaxProperties        int      `json:"maxProperties,omitempty"`
	PropertyNames        *Prop    `json:"propertyNames,omitempty"` // object: a pattern every key matches

	re *regexp.Regexp
}

var (
	schemaKeys = map[string]bool{"type": true, "properties": true, "required": true, "additionalProperties": true}
	propKeys   = map[string]bool{"type": true, "title": true, "description": true, "default": true, "enum": true, "pattern": true,
		"maxLength": true, "minimum": true, "maximum": true, "additionalProperties": true, "maxProperties": true, "propertyNames": true}
	inputNameRE = regexp.MustCompile(`^[a-z][a-z0-9_]{0,39}$`)
)

// ParseSchema reads and checks an inputs schema.
func ParseSchema(raw json.RawMessage) (*Schema, error) {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		return nil, fmt.Errorf("inputs: not a JSON object: %w", err)
	}
	for k := range top {
		if !schemaKeys[k] {
			return nil, fmt.Errorf("inputs: %q is not a keyword templates may use", k)
		}
	}
	if string(top["additionalProperties"]) != "false" {
		return nil, fmt.Errorf(`inputs: set "additionalProperties": false, so unknown inputs are refused`)
	}
	var s Schema
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, fmt.Errorf("inputs: %w", err)
	}
	if s.Type != "object" {
		return nil, fmt.Errorf(`inputs: the type must be "object"`)
	}
	// Keep the file's order for the form.
	var props map[string]json.RawMessage
	json.Unmarshal(top["properties"], &props)
	s.Order = keyOrder(top["properties"])
	if len(s.Order) == 0 || len(s.Order) > 40 {
		return nil, fmt.Errorf("inputs: declare 1 to 40 inputs")
	}
	for _, name := range s.Order {
		if !inputNameRE.MatchString(name) || name == "name" || name == "tags" {
			return nil, fmt.Errorf("inputs.%s: names are lower case with underscores, and name and tags are Forgeline's", name)
		}
		if err := checkProp("inputs."+name, props[name], s.Properties[name], true); err != nil {
			return nil, err
		}
	}
	for _, r := range s.Required {
		if s.Properties[r] == nil {
			return nil, fmt.Errorf("inputs.required: %q is not an input", r)
		}
	}
	return &s, nil
}

// checkProp checks one property's keywords and compiles its pattern. nested is false for the value or key schema of a
// map, which may only be a string.
func checkProp(at string, raw json.RawMessage, p *Prop, top bool) error {
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(raw, &keys); err != nil || p == nil {
		return fmt.Errorf("%s: not a JSON object", at)
	}
	for k := range keys {
		if !propKeys[k] {
			return fmt.Errorf("%s: %q is not a keyword templates may use", at, k)
		}
	}
	switch p.Type {
	case "string", "integer", "number", "boolean":
	case "object":
		if !top || p.AdditionalProperties == nil || p.AdditionalProperties.Type != "string" || p.MaxProperties < 1 || p.MaxProperties > 50 ||
			p.PropertyNames == nil || p.PropertyNames.Pattern == "" {
			return fmt.Errorf("%s: an object input is a map of strings: give additionalProperties (a string), maxProperties (1 to 50) and propertyNames.pattern", at)
		}
		if err := checkProp(at+".additionalProperties", keys["additionalProperties"], p.AdditionalProperties, false); err != nil {
			return err
		}
		if err := checkProp(at+".propertyNames", keys["propertyNames"], p.PropertyNames, false); err != nil {
			return err
		}
	default:
		if p.Type == "" && !top {
			p.Type = "string"
			break
		}
		return fmt.Errorf("%s: type %q is not one templates may use (string, integer, number, boolean, or a map of strings)", at, p.Type)
	}
	if p.Type == "string" {
		if p.MaxLength <= 0 && len(p.Enum) == 0 {
			return fmt.Errorf("%s: a text input needs maxLength or enum", at)
		}
	} else if p.Pattern != "" || p.MaxLength != 0 {
		return fmt.Errorf("%s: pattern and maxLength are for text", at)
	}
	if (p.Minimum != nil || p.Maximum != nil) && p.Type != "integer" && p.Type != "number" {
		return fmt.Errorf("%s: minimum and maximum are for numbers", at)
	}
	if p.Pattern != "" {
		re, err := regexp.Compile(p.Pattern)
		if err != nil || !strings.HasPrefix(p.Pattern, "^") || !strings.HasSuffix(p.Pattern, "$") {
			return fmt.Errorf("%s: pattern must be a valid expression anchored with ^ and $", at)
		}
		p.re = re
	}
	for _, e := range p.Enum {
		if err := p.check("", e); err != nil {
			return fmt.Errorf("%s: enum value %v does not fit the input itself", at, e)
		}
	}
	if p.Default != nil {
		if err := p.check("", p.Default); err != nil {
			return fmt.Errorf("%s: the default does not fit: %v", at, err)
		}
	}
	return nil
}

// Resolve checks values against the schema and fills defaults. The result holds only declared inputs.
func (s *Schema) Resolve(values map[string]any) (map[string]any, error) {
	for k := range values {
		if s.Properties[k] == nil {
			return nil, fmt.Errorf("%s is not an input of this template", k)
		}
	}
	out := map[string]any{}
	for _, name := range s.Order {
		p := s.Properties[name]
		v, ok := values[name]
		if !ok || v == nil {
			if p.Default == nil {
				if contains(s.Required, name) {
					return nil, fmt.Errorf("%s is required", name)
				}
				continue
			}
			v = p.Default
		}
		if err := p.check(name, v); err != nil {
			return nil, err
		}
		out[name] = v
	}
	return out, nil
}

// check checks one value against a property.
func (p *Prop) check(name string, v any) error {
	what := name
	if what == "" {
		what = "the value"
	}
	switch p.Type {
	case "string":
		str, ok := v.(string)
		if !ok {
			return fmt.Errorf("%s must be text", what)
		}
		if p.MaxLength > 0 && len(str) > p.MaxLength {
			return fmt.Errorf("%s is longer than %d characters", what, p.MaxLength)
		}
		if p.re != nil && !p.re.MatchString(str) {
			return fmt.Errorf("%s does not have the form it needs", what)
		}
	case "integer", "number":
		f, ok := v.(float64)
		if !ok {
			if i, isInt := v.(int); isInt {
				f, ok = float64(i), true
			}
		}
		if !ok || math.IsNaN(f) || math.IsInf(f, 0) || p.Type == "integer" && f != math.Trunc(f) {
			return fmt.Errorf("%s must be a %s", what, map[string]string{"integer": "whole number", "number": "number"}[p.Type])
		}
		if p.Minimum != nil && f < *p.Minimum || p.Maximum != nil && f > *p.Maximum {
			return fmt.Errorf("%s is out of range", what)
		}
	case "boolean":
		if _, ok := v.(bool); !ok {
			return fmt.Errorf("%s must be true or false", what)
		}
	case "object":
		m, ok := v.(map[string]any)
		if !ok {
			return fmt.Errorf("%s must be a set of names and values", what)
		}
		if len(m) > p.MaxProperties {
			return fmt.Errorf("%s has more than %d entries", what, p.MaxProperties)
		}
		for k, e := range m {
			if err := p.PropertyNames.check("", k); err != nil {
				return fmt.Errorf("%s: the name %q does not have the form it needs", what, k)
			}
			if err := p.AdditionalProperties.check("", e); err != nil {
				return fmt.Errorf("%s.%s: %v", what, k, err)
			}
		}
	}
	if len(p.Enum) > 0 && !inEnum(p.Enum, v) {
		return fmt.Errorf("%s is not one of the allowed values", what)
	}
	return nil
}

func inEnum(enum []any, v any) bool {
	for _, e := range enum {
		if fmt.Sprint(e) == fmt.Sprint(v) {
			return true
		}
	}
	return false
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// tfType is the OpenTofu type a module variable must have for an input.
func (p *Prop) tfType() string {
	switch p.Type {
	case "string":
		return "string"
	case "integer", "number":
		return "number"
	case "boolean":
		return "bool"
	}
	return "map(string)"
}

// keyOrder returns an object's keys in the order they appear.
func keyOrder(raw json.RawMessage) []string {
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	if t, err := dec.Token(); err != nil || t != json.Delim('{') {
		return nil
	}
	var keys []string
	for dec.More() {
		t, err := dec.Token()
		if err != nil {
			return nil
		}
		k, _ := t.(string)
		keys = append(keys, k)
		var skip json.RawMessage
		if err := dec.Decode(&skip); err != nil {
			return nil
		}
	}
	return keys
}

// Names lists the inputs in a stable order.
func (s *Schema) Names() []string {
	names := append([]string(nil), s.Order...)
	sort.Strings(names)
	return names
}
