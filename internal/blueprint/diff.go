package blueprint

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
)

// Diff says in plain words what changed from old to new: blocks added, removed or changed (naming the
// fields), and links added or removed. Positions on the canvas are not changes.
func Diff(old, new Blueprint) []string {
	var out []string
	oldBy, newBy := map[string]Block{}, map[string]Block{}
	for _, b := range old.Blocks {
		oldBy[b.ID] = b
	}
	for _, b := range new.Blocks {
		newBy[b.ID] = b
	}
	name := func(id string) string {
		if b, ok := newBy[id]; ok {
			return b.Name
		}
		return oldBy[id].Name
	}
	for _, b := range new.Blocks {
		o, ok := oldBy[b.ID]
		if !ok {
			out = append(out, fmt.Sprintf("Adds %s (%s)", b.Name, b.Kind))
			continue
		}
		if fields := changedFields(o, b); len(fields) > 0 {
			out = append(out, fmt.Sprintf("Changes %s: %s", b.Name, strings.Join(fields, ", ")))
		}
	}
	for _, b := range old.Blocks {
		if _, ok := newBy[b.ID]; !ok {
			out = append(out, "Removes "+b.Name)
		}
	}
	key := func(l Link) string { return l.From + ">" + l.To }
	oldL, newL := map[string]Link{}, map[string]Link{}
	for _, l := range old.Links {
		oldL[key(l)] = l
	}
	for _, l := range new.Links {
		newL[key(l)] = l
	}
	var keys []string
	for k := range newL {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		l := newL[k]
		o, ok := oldL[k]
		switch {
		case !ok:
			out = append(out, "Links "+name(l.From)+" to "+name(l.To)+accessWords(l.Access))
		case o.Access != l.Access:
			out = append(out, "Changes the link from "+name(l.From)+" to "+name(l.To)+accessWords(l.Access))
		}
	}
	keys = keys[:0]
	for k := range oldL {
		if _, ok := newL[k]; !ok {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	for _, k := range keys {
		out = append(out, "Unlinks "+name(oldL[k].From)+" from "+name(oldL[k].To))
	}
	return out
}

func accessWords(a string) string {
	switch a {
	case AccessRead:
		return " (reads)"
	case AccessReadWrite:
		return " (reads and writes)"
	}
	return ""
}

// changedFields names the JSON fields that differ, leaving out the position.
func changedFields(a, b Block) []string {
	var out []string
	ta := reflect.TypeOf(a)
	va, vb := reflect.ValueOf(a), reflect.ValueOf(b)
	for i := 0; i < ta.NumField(); i++ {
		f := ta.Field(i)
		tag, _, _ := strings.Cut(f.Tag.Get("json"), ",")
		if tag == "x" || tag == "y" || tag == "id" {
			continue
		}
		if !reflect.DeepEqual(va.Field(i).Interface(), vb.Field(i).Interface()) {
			out = append(out, tag)
		}
	}
	return out
}
