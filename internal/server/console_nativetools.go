package server

import (
	"context"
	"encoding/json"
	"regexp"
	"sort"
	"strings"
)

// Native tool calls (design note 052). The assistant's tools were described to the model in the prompt and the model
// asked for one by writing a block of text, which models get wrong in ways that depend on the model (XML calls, text
// around the block). Where a provider has real tool calling (Anthropic, and OpenAI-compatible APIs including Gemini),
// the same tools are also sent as tool definitions, and a tool call in the reply is turned back into the block the
// loop already understands. The conversation the model is shown stays text, so nothing about the loop, the budget, the
// permission checks or the history changes. A provider without tool calling keeps using the text block.

type helpLine struct{ name, help string }

type nativeTool struct {
	name, wire, description string
	schema                  map[string]any
}

type nativeSet struct {
	tools  []nativeTool
	byWire map[string]string
}

type nativeToolsKey struct{}

func nativeToolsFrom(ctx context.Context) *nativeSet {
	n, _ := ctx.Value(nativeToolsKey{}).(*nativeSet)
	if n == nil || len(n.tools) == 0 {
		return nil
	}
	return n
}

var wireBad = regexp.MustCompile(`[^A-Za-z0-9_-]`)

var intKeys = map[string]bool{"limit": true, "n": true, "count": true, "timeout_seconds": true, "insert_line": true, "version": true, "retention_days": true, "lines": true, "days": true}

// buildNativeSet turns the tool lines the model is told about into tool definitions. A tool whose name cannot be
// written the way providers require (letters, digits, dash, underscore, 64 at most) is left to the text block.
func buildNativeSet(lines []helpLine) *nativeSet {
	set := &nativeSet{byWire: map[string]string{}}
	for _, l := range lines {
		wire := wireBad.ReplaceAllString(l.name, "_")
		if len(wire) > 64 || wire != l.name {
			continue
		}
		if _, dup := set.byWire[wire]; dup {
			continue
		}
		schema, desc := schemaFromHelp(l.help)
		set.byWire[wire] = l.name
		set.tools = append(set.tools, nativeTool{name: l.name, wire: wire, description: truncate(desc, 1500), schema: schema})
	}
	return set
}

// schemaFromHelp reads a tool's help line, which starts with an example of its arguments as a JSON object and goes on
// to say what it does. An argument is required unless its example says it is optional or only for some cases.
func schemaFromHelp(help string) (map[string]any, string) {
	help = strings.TrimSpace(help)
	props := map[string]any{}
	var required []string
	desc := help
	if strings.HasPrefix(help, "{") {
		if end := balancedEnd(help); end > 0 {
			var ex map[string]any
			if json.Unmarshal([]byte(help[:end]), &ex) == nil {
				keys := make([]string, 0, len(ex))
				for k := range ex {
					keys = append(keys, k)
				}
				sort.Strings(keys)
				for _, k := range keys {
					props[k] = propFor(k, ex[k])
					if !optionalExample(ex[k]) {
						required = append(required, k)
					}
				}
				desc = strings.TrimSpace(help[end:])
			}
		}
	}
	schema := map[string]any{"type": "object", "properties": props}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema, desc
}

func propFor(key string, v any) map[string]any {
	switch x := v.(type) {
	case float64:
		if x == float64(int64(x)) {
			return map[string]any{"type": "integer"}
		}
		return map[string]any{"type": "number"}
	case bool:
		return map[string]any{"type": "boolean"}
	case []any:
		item := map[string]any{"type": "string"}
		if len(x) > 0 {
			item = propFor(key, x[0])
		}
		return map[string]any{"type": "array", "items": item}
	case map[string]any:
		props := map[string]any{}
		for k, e := range x {
			props[k] = propFor(k, e)
		}
		return map[string]any{"type": "object", "properties": props}
	case string:
		switch {
		case intKeys[key]:
			return map[string]any{"type": "integer", "description": x}
		case enumRE.MatchString(x):
			return map[string]any{"type": "string", "enum": strings.Split(x, "|")}
		case strings.Contains(x, `{"`) || strings.Contains(x, "{\""):
			return map[string]any{"type": "object", "description": x} // text that shows a JSON object: the argument is an object
		}
		return map[string]any{"type": "string", "description": x}
	}
	return map[string]any{"type": "string"}
}

// enumRE matches an example like "view|search|create": a choice of words.
var enumRE = regexp.MustCompile(`^[a-z][a-z_]*(\|[a-z][a-z_]*)+$`)

// optionalExample says an example value marks its argument as optional or only needed in some cases.
func optionalExample(v any) bool {
	s, ok := v.(string)
	if !ok {
		return true // a number, a list or an object in an example is a sample value, not a required one
	}
	return strings.HasPrefix(s, "optional") || strings.HasPrefix(s, "for ") || strings.Contains(s, "optional")
}

// balancedEnd is the index just after the JSON object that starts s, or 0.
func balancedEnd(s string) int {
	depth, inStr, esc := 0, false, false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case esc:
			esc = false
		case inStr && c == '\\':
			esc = true
		case c == '"':
			inStr = !inStr
		case inStr:
		case c == '{':
			depth++
		case c == '}':
			depth--
			if depth == 0 {
				return i + 1
			}
		}
	}
	return 0
}

func (n *nativeSet) anthropicTools() []map[string]any {
	out := make([]map[string]any, 0, len(n.tools))
	for _, t := range n.tools {
		out = append(out, map[string]any{"name": t.wire, "description": t.description, "input_schema": t.schema})
	}
	return out
}

func (n *nativeSet) openAITools() []map[string]any {
	out := make([]map[string]any, 0, len(n.tools))
	for _, t := range n.tools {
		out = append(out, map[string]any{"type": "function", "function": map[string]any{"name": t.wire, "description": t.description, "parameters": t.schema}})
	}
	return out
}

// asBlock writes a tool call the way the loop reads it. A name the model made up still goes through, and the loop says
// there is no such tool.
func (n *nativeSet) asBlock(wire string, args json.RawMessage) string {
	name := n.byWire[wire]
	if name == "" {
		name = wire
	}
	if len(strings.TrimSpace(string(args))) == 0 || !json.Valid(args) {
		args = json.RawMessage(`{}`)
	}
	b, _ := json.Marshal(map[string]any{"tool": name, "args": json.RawMessage(args)})
	return lookupFence + "\n" + string(b) + "\n```"
}
