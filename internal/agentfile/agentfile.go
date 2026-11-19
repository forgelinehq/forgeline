// Package agentfile is how an agent leaves Forgeline (design note 046): a bundle with Forgeline's own agent.yaml and
// each pinned skill as an Agent Skills folder (agentskills.io), and a Docker Agent file. Nothing here reads a secret:
// keys only ever appear as the names of environment variables.
package agentfile

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/forgelinehq/forgeline/internal/pipeline"
	yaml "go.yaml.in/yaml/v3"
	sigsyaml "sigs.k8s.io/yaml"
)

// Version is the agent file's own version; it changes only when an old reader could misread a new file.
const Version = "v1alpha1"

// SchemaFile is where the generated schema is committed, relative to the repository root.
const SchemaFile = "schema/agent.v1alpha1.schema.json"

// File is agent.yaml: one fixed version of an agent, as Forgeline keeps it.
type File struct {
	Version      string   `json:"version" yaml:"version" enum:"v1alpha1" doc:"The agent file's version."`
	Name         string   `json:"name" yaml:"name" minLength:"1" doc:"The agent's name."`
	Description  string   `json:"description,omitempty" yaml:"description,omitempty" doc:"What it is for."`
	Model        string   `json:"model,omitempty" yaml:"model,omitempty" doc:"provider/model, or a bare model name for the assistant's connection; empty uses the assistant's model."`
	Tier         string   `json:"tier" yaml:"tier" enum:"T0,T1" doc:"T0 reads and explains; T1 may also propose changes, which a different administrator approves."`
	Tools        []string `json:"tools,omitempty" yaml:"tools,omitempty" itemPattern:"^[A-Za-z0-9_.-]{1,96}$" doc:"Forgeline tool names. Outside Forgeline only remote MCP server tools work; see README.md."`
	Skills       []Skill  `json:"skills,omitempty" yaml:"skills,omitempty" doc:"Pinned skills, each an Agent Skills folder in this bundle."`
	Instructions string   `json:"instructions" yaml:"instructions" minLength:"1" doc:"What the model receives, exactly."`
	Source       Source   `json:"source" yaml:"source" doc:"Where this copy came from."`
}

// Skill is one pinned skill and where its SKILL.md is in the bundle.
type Skill struct {
	Name    string `json:"name" yaml:"name" pattern:"^[a-z0-9]+(-[a-z0-9]+)*$" doc:"The Agent Skills name, the same as its folder."`
	Title   string `json:"title,omitempty" yaml:"title,omitempty" doc:"Its name in Forgeline."`
	Version int    `json:"version" yaml:"version" minimum:"1" doc:"The skill's version in Forgeline."`
	Path    string `json:"path" yaml:"path" pattern:"^skills/[a-z0-9-]+/SKILL\\.md$"`
	SHA256  string `json:"sha256" yaml:"sha256" pattern:"^[0-9a-f]{64}$" doc:"Of the skill's text, without the front matter."`
}

// Source names the agent, the version and its fingerprint, so a copy can be traced back. No address, no person.
type Source struct {
	Agent       string `json:"agent" yaml:"agent" doc:"The agent's id in Forgeline."`
	Version     int    `json:"version" yaml:"version" minimum:"1"`
	Fingerprint string `json:"fingerprint" yaml:"fingerprint" pattern:"^[0-9a-f]{64}$" doc:"The version's hash in Forgeline."`
	ExportedAt  string `json:"exported_at" yaml:"exported_at" doc:"RFC 3339."`
}

// Schema is the JSON Schema of agent.yaml, from the Go types.
func Schema() ([]byte, error) {
	return pipeline.SchemaFor(File{}, "Forgeline agent", "One version of a Forgeline agent, agent file version "+Version+" (design note 046).", nil)
}

var skillNameRE = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// SkillName turns a Forgeline skill id into an Agent Skills name: lower case letters, digits and single hyphens, at
// most 64 characters.
func SkillName(id string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(id) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
			dash = false
		} else if !dash && b.Len() > 0 {
			b.WriteByte('-')
			dash = true
		}
	}
	s := strings.Trim(b.String(), "-")
	if len(s) > 64 {
		s = strings.Trim(s[:64], "-")
	}
	if s == "" {
		s = "skill"
	}
	return s
}

// Hash is the sha256 of a skill's text, as agent.yaml records it.
func Hash(text string) string {
	h := sha256.Sum256([]byte(text))
	return hex.EncodeToString(h[:])
}

// SkillDoc is a skill's content for the bundle.
type SkillDoc struct {
	ID, Title, Description, Body string
	Version                      int
}

type skillFront struct {
	Name        string            `yaml:"name"`
	Description string            `yaml:"description"`
	Metadata    map[string]string `yaml:"metadata,omitempty"`
}

// SkillMD is a skill as an Agent Skills SKILL.md: front matter with name and description (at most 1024 characters, as
// the spec says), Forgeline's id and version under metadata, then the skill's text.
func SkillMD(sk SkillDoc) (string, error) {
	desc := strings.TrimSpace(sk.Description)
	if desc == "" {
		desc = sk.Title
	}
	if len(desc) > 1024 {
		desc = desc[:1021] + "..."
	}
	front, err := yaml.Marshal(skillFront{Name: SkillName(sk.ID), Description: desc,
		Metadata: map[string]string{"forgeline-skill": sk.ID, "forgeline-version": fmt.Sprint(sk.Version)}})
	if err != nil {
		return "", err
	}
	return "---\n" + string(front) + "---\n\n" + strings.TrimSpace(sk.Body) + "\n", nil
}

// Agent is what a bundle or a Docker file is made from: one agent version, its pinned skills, and the model's provider.
type Agent struct {
	ID, Name, Description, Instructions, Tier, Model, Fingerprint string
	Version                                                       int
	Tools                                                         []string
	Skills                                                        []SkillDoc
	At                                                            time.Time
}

// FileOf is agent.yaml for an agent version.
func FileOf(a Agent) File {
	f := File{Version: Version, Name: a.Name, Description: a.Description, Model: a.Model, Tier: a.Tier, Tools: a.Tools, Instructions: a.Instructions,
		Source: Source{Agent: a.ID, Version: a.Version, Fingerprint: a.Fingerprint, ExportedAt: a.At.UTC().Format(time.RFC3339)}}
	for _, sk := range a.Skills {
		n := SkillName(sk.ID)
		f.Skills = append(f.Skills, Skill{Name: n, Title: sk.Title, Version: sk.Version, Path: "skills/" + n + "/SKILL.md", SHA256: Hash(strings.TrimSpace(sk.Body) + "\n")})
	}
	return f
}

// Marshal writes agent.yaml, fields in the order of the type.
func Marshal(f File) ([]byte, error) {
	var b bytes.Buffer
	b.WriteString("# A Forgeline agent (design note 046). Schema: " + SchemaFile + "\n")
	enc := yaml.NewEncoder(&b)
	enc.SetIndent(2)
	if err := enc.Encode(f); err != nil {
		return nil, err
	}
	return b.Bytes(), enc.Close()
}

// Parse reads agent.yaml strictly: an unknown field is an error, and so is anything Check finds.
func Parse(data []byte) (File, error) {
	var f File
	if err := sigsyaml.UnmarshalStrict(data, &f); err != nil {
		return File{}, fmt.Errorf("agent.yaml: %w", err)
	}
	return f, Check(f)
}

// Check says what is wrong with an agent file, where.
func Check(f File) error {
	switch {
	case f.Version != Version:
		return fmt.Errorf("agent.yaml: version: want %q, got %q", Version, f.Version)
	case strings.TrimSpace(f.Name) == "" || len(f.Name) > 60:
		return fmt.Errorf("agent.yaml: name: 1 to 60 characters")
	case f.Tier != "T0" && f.Tier != "T1":
		return fmt.Errorf("agent.yaml: tier: T0 or T1, got %q", f.Tier)
	case strings.TrimSpace(f.Instructions) == "":
		return fmt.Errorf("agent.yaml: instructions: empty")
	}
	seen := map[string]bool{}
	for i, s := range f.Skills {
		switch {
		case !skillNameRE.MatchString(s.Name) || len(s.Name) > 64:
			return fmt.Errorf("agent.yaml: skills[%d].name: %q is not an Agent Skills name", i, s.Name)
		case s.Path != "skills/"+s.Name+"/SKILL.md":
			return fmt.Errorf("agent.yaml: skills[%d].path: want skills/%s/SKILL.md", i, s.Name)
		case seen[s.Name]:
			return fmt.Errorf("agent.yaml: skills[%d]: %q is listed twice", i, s.Name)
		case s.Version < 1:
			return fmt.Errorf("agent.yaml: skills[%d].version: at least 1", i)
		}
		seen[s.Name] = true
	}
	return nil
}

// Bundle is the zip a person downloads: agent.yaml, skills/<name>/SKILL.md for each skill, and README.md.
func Bundle(a Agent, readme string) ([]byte, error) {
	f := FileOf(a)
	if err := Check(f); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	z := zip.NewWriter(&buf)
	put := func(name string, data []byte) error {
		w, err := z.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Deflate, Modified: a.At.UTC()})
		if err != nil {
			return err
		}
		_, err = w.Write(data)
		return err
	}
	y, err := Marshal(f)
	if err != nil {
		return nil, err
	}
	if err := put("agent.yaml", y); err != nil {
		return nil, err
	}
	for i, sk := range a.Skills {
		md, err := SkillMD(sk)
		if err != nil {
			return nil, err
		}
		if err := put(f.Skills[i].Path, []byte(md)); err != nil {
			return nil, err
		}
	}
	if err := put("README.md", []byte(readme)); err != nil {
		return nil, err
	}
	if err := z.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
