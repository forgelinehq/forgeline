package agentfile

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"strings"

	yaml "go.yaml.in/yaml/v3"
)

// Limits on a bundle being read back (note 046, step 2): a bundle is a few small text files, so anything bigger is
// refused before it is unpacked.
const (
	MaxBundle     = 512 << 10 // the zip itself
	maxBundleFile = 64 << 10  // each file once unpacked
	maxEntries    = 32
)

// ReadSkill is one skill as read from a bundle: its entry in agent.yaml and its text and description from SKILL.md.
type ReadSkill struct {
	Skill
	Description string
	Body        string // the text after the front matter, trimmed, with one final newline
	Edited      bool   // the text does not match the sha256 in agent.yaml: it was changed after export
}

// ReadBundle reads a Forgeline bundle: agent.yaml (strictly, as Parse does) and the SKILL.md of every skill it lists.
// Other files (README.md) are ignored. Errors say which file and what is wrong.
func ReadBundle(data []byte) (File, []ReadSkill, error) {
	if len(data) > MaxBundle {
		return File{}, nil, fmt.Errorf("the bundle is larger than %d KiB", MaxBundle>>10)
	}
	z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return File{}, nil, fmt.Errorf("not a zip file: %v", err)
	}
	if len(z.File) > maxEntries {
		return File{}, nil, fmt.Errorf("the bundle has %d files; a bundle has at most %d", len(z.File), maxEntries)
	}
	files := map[string]*zip.File{}
	for _, f := range z.File {
		files[f.Name] = f
	}
	read := func(name string) ([]byte, error) {
		f, ok := files[name]
		if !ok {
			return nil, fmt.Errorf("%s: not in the bundle", name)
		}
		r, err := f.Open()
		if err != nil {
			return nil, fmt.Errorf("%s: %v", name, err)
		}
		defer r.Close()
		b, err := io.ReadAll(io.LimitReader(r, maxBundleFile+1))
		if err != nil {
			return nil, fmt.Errorf("%s: %v", name, err)
		}
		if len(b) > maxBundleFile {
			return nil, fmt.Errorf("%s: larger than %d KiB", name, maxBundleFile>>10)
		}
		return b, nil
	}
	y, err := read("agent.yaml")
	if err != nil {
		return File{}, nil, err
	}
	af, err := Parse(y)
	if err != nil {
		return File{}, nil, err
	}
	var skills []ReadSkill
	for _, s := range af.Skills {
		md, err := read(s.Path)
		if err != nil {
			return File{}, nil, err
		}
		front, body, err := splitSkillMD(string(md))
		if err != nil {
			return File{}, nil, fmt.Errorf("%s: %v", s.Path, err)
		}
		if front.Name != s.Name {
			return File{}, nil, fmt.Errorf("%s: name: %q, but agent.yaml says %q", s.Path, front.Name, s.Name)
		}
		skills = append(skills, ReadSkill{Skill: s, Description: front.Description, Body: body, Edited: Hash(body) != s.SHA256})
	}
	return af, skills, nil
}

// splitSkillMD separates a SKILL.md's front matter from its text.
func splitSkillMD(md string) (skillFront, string, error) {
	md = strings.ReplaceAll(md, "\r\n", "\n")
	rest, ok := strings.CutPrefix(md, "---\n")
	if !ok {
		return skillFront{}, "", fmt.Errorf("no front matter: it starts with ---")
	}
	head, body, ok := strings.Cut(rest, "\n---\n")
	if !ok {
		return skillFront{}, "", fmt.Errorf("the front matter does not end with ---")
	}
	var f skillFront
	if err := yaml.Unmarshal([]byte(head), &f); err != nil {
		return skillFront{}, "", fmt.Errorf("front matter: %v", err)
	}
	if strings.TrimSpace(f.Description) == "" {
		return skillFront{}, "", fmt.Errorf("description: empty")
	}
	body = strings.TrimSpace(body)
	if body == "" {
		return skillFront{}, "", fmt.Errorf("the skill has no text")
	}
	return f, body + "\n", nil
}
