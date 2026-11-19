package agentfile

import (
	"bytes"
	"fmt"
	"sort"
	"strings"

	yaml "go.yaml.in/yaml/v3"
)

// DockerVersion is the Docker Agent configuration version these files are written for (its docs, November 2026).
const DockerVersion = 16

// ModelSource is what Forgeline knows about the provider of an agent's model. Kind is a Forgeline provider kind
// (anthropic, openai, openai-compatible, gemini, azure-openai, bedrock), or empty for the assistant's own connection.
type ModelSource struct {
	ProviderID, Kind, BaseURL, Model string
}

// RemoteMCP is a remote MCP server an agent uses, with only the tools the agent has (their names on the server).
type RemoteMCP struct {
	ID, URL, AuthHeader, AuthScheme, AuthSecret string
	Tools                                       []string
}

// LeftOut is a tool a copy cannot have, and why.
type LeftOut struct {
	Tool, Why string
}

type dockerFile struct {
	Version   int                       `yaml:"version"`
	Metadata  dockerMeta                `yaml:"metadata"`
	Providers map[string]dockerProvider `yaml:"providers,omitempty"`
	Models    map[string]dockerModel    `yaml:"models,omitempty"`
	MCPs      map[string]dockerMCP      `yaml:"mcps,omitempty"`
	Agents    map[string]dockerAgent    `yaml:"agents"`
}

type dockerMeta struct {
	Description string   `yaml:"description,omitempty"`
	Version     string   `yaml:"version"`
	Readme      string   `yaml:"readme,omitempty"`
	Tags        []string `yaml:"tags,omitempty"`
}

type dockerProvider struct {
	BaseURL  string `yaml:"base_url"`
	TokenKey string `yaml:"token_key,omitempty"`
}

type dockerModel struct {
	Provider string `yaml:"provider"`
	Model    string `yaml:"model"`
	BaseURL  string `yaml:"base_url,omitempty"`
	TokenKey string `yaml:"token_key,omitempty"`
}

type dockerMCP struct {
	Remote dockerRemote `yaml:"remote"`
}

type dockerRemote struct {
	URL           string            `yaml:"url"`
	TransportType string            `yaml:"transport_type"`
	Headers       map[string]string `yaml:"headers,omitempty"`
}

type dockerAgent struct {
	Model       string          `yaml:"model"`
	Description string          `yaml:"description,omitempty"`
	Instruction string          `yaml:"instruction"`
	Skills      []dockerSkill   `yaml:"skills,omitempty"`
	Toolsets    []dockerToolset `yaml:"toolsets,omitempty"`
}

type dockerSkill struct {
	Name         string `yaml:"name"`
	Description  string `yaml:"description"`
	Instructions string `yaml:"instructions"`
}

type dockerToolset struct {
	Type  string   `yaml:"type"`
	Ref   string   `yaml:"ref"`
	Tools []string `yaml:"tools,omitempty"`
}

// EnvName is the environment variable a copy reads a secret from: the name in capitals, other characters as _.
func EnvName(name string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(name) {
		if r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	s := strings.Trim(b.String(), "_")
	if s == "" || s[0] >= '0' && s[0] <= '9' {
		s = "KEY_" + s
	}
	return s
}

// dockerModelRef maps Forgeline's model to Docker Agent's: a provider/model string, or the name of a models: entry.
func dockerModelRef(m ModelSource, f *dockerFile) (ref, note string) {
	switch m.Kind {
	case "anthropic":
		return "anthropic/" + m.Model, ""
	case "openai":
		return "openai/" + m.Model, ""
	case "gemini":
		return "google/" + m.Model, ""
	case "bedrock":
		return "amazon-bedrock/" + m.Model, ""
	case "azure-openai":
		f.Models = map[string]dockerModel{"main": {Provider: "azure", Model: m.Model, BaseURL: m.BaseURL, TokenKey: "AZURE_API_KEY"}}
		return "main", ""
	case "openai-compatible":
		id := SkillName(m.ProviderID)
		f.Providers = map[string]dockerProvider{id: {BaseURL: m.BaseURL, TokenKey: EnvName(m.ProviderID) + "_API_KEY"}}
		return id + "/" + m.Model, ""
	}
	// The assistant's own connection: Anthropic models by their bare names.
	if m.Model == "" {
		return "anthropic/claude-sonnet-5-5", "It used the assistant's model in Forgeline; this copy asks Claude Sonnet 5.5. Change model: to taste."
	}
	return "anthropic/" + m.Model, ""
}

// DockerYAML is the agent as a Docker Agent file (note 046, decision 2): one root agent, its skills inline (so the
// file is the whole agent), its remote MCP servers with sign-in headers as ${env.NAME}, and what was left out said at
// the top and in the readme.
func DockerYAML(a Agent, m ModelSource, mcps []RemoteMCP, left []LeftOut) ([]byte, error) {
	f := dockerFile{Version: DockerVersion, MCPs: map[string]dockerMCP{}}
	ref, note := dockerModelRef(m, &f)
	ag := dockerAgent{Model: ref, Description: a.Description, Instruction: strings.TrimSpace(a.Instructions) + "\n"}
	for _, sk := range a.Skills {
		desc := strings.TrimSpace(sk.Description)
		if desc == "" {
			desc = sk.Title
		}
		ag.Skills = append(ag.Skills, dockerSkill{Name: SkillName(sk.ID), Description: desc, Instructions: strings.TrimSpace(sk.Body) + "\n"})
	}
	sort.Slice(mcps, func(i, j int) bool { return mcps[i].ID < mcps[j].ID })
	var envs []string
	for _, s := range mcps {
		id := SkillName(s.ID)
		r := dockerRemote{URL: s.URL, TransportType: "streamable"}
		if s.AuthHeader != "" && s.AuthSecret != "" {
			env := EnvName(s.AuthSecret)
			v := "${env." + env + "}"
			if s.AuthScheme != "" {
				v = s.AuthScheme + " " + v
			}
			r.Headers = map[string]string{s.AuthHeader: v}
			envs = append(envs, env)
		}
		f.MCPs[id] = dockerMCP{Remote: r}
		ag.Toolsets = append(ag.Toolsets, dockerToolset{Type: "mcp", Ref: id, Tools: s.Tools})
	}
	if len(f.MCPs) == 0 {
		f.MCPs = nil
	}
	f.Agents = map[string]dockerAgent{"root": ag}

	var readme strings.Builder
	fmt.Fprintf(&readme, "%s, version %d of a Forgeline agent (fingerprint %s).\n", a.Name, a.Version, a.Fingerprint)
	if note != "" {
		readme.WriteString("\n" + note + "\n")
	}
	if len(left) > 0 {
		readme.WriteString("\nLeft out, because they need Forgeline:\n")
		for _, l := range left {
			fmt.Fprintf(&readme, "- %s: %s\n", l.Tool, l.Why)
		}
	}
	f.Metadata = dockerMeta{Description: a.Description, Version: fmt.Sprintf("%d.0.0", a.Version), Readme: readme.String(), Tags: []string{"forgeline"}}

	var b bytes.Buffer
	fmt.Fprintf(&b, "# %s, exported from Forgeline: version %d, fingerprint %s.\n", a.Name, a.Version, a.Fingerprint)
	b.WriteString("# Run it with: docker agent run agent.yaml\n")
	if key := modelEnv(m, ref); key != "" {
		envs = append([]string{key}, envs...)
	}
	if len(envs) > 0 {
		b.WriteString("# It reads these environment variables: " + strings.Join(envs, ", ") + "\n")
	}
	if len(left) > 0 {
		b.WriteString("# Left out (they need Forgeline): ")
		var names []string
		for _, l := range left {
			names = append(names, l.Tool)
		}
		b.WriteString(strings.Join(names, ", ") + "\n")
	}
	enc := yaml.NewEncoder(&b)
	enc.SetIndent(2)
	if err := enc.Encode(f); err != nil {
		return nil, err
	}
	return b.Bytes(), enc.Close()
}

// modelEnv is the environment variable the model's key comes from, as Docker Agent reads it.
func modelEnv(m ModelSource, ref string) string {
	switch {
	case m.Kind == "openai-compatible":
		return EnvName(m.ProviderID) + "_API_KEY"
	case m.Kind == "azure-openai":
		return "AZURE_API_KEY"
	case m.Kind == "bedrock":
		return "AWS_BEARER_TOKEN_BEDROCK or AWS credentials"
	case strings.HasPrefix(ref, "anthropic/"):
		return "ANTHROPIC_API_KEY"
	case strings.HasPrefix(ref, "openai/"):
		return "OPENAI_API_KEY"
	case strings.HasPrefix(ref, "google/"):
		return "GOOGLE_API_KEY"
	}
	return ""
}
