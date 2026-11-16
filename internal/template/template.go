// Package template loads and checks blueprint templates (design note 040, step 2): reviewed, versioned OpenTofu
// modules with their inputs as a JSON Schema subset and the links they offer, drawn on the canvas as one block. It makes
// no network calls; first-party templates are compiled in.
package template

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"sort"
	"strings"
	"sync"
)

// Link role shapes: what the generator passes for each linked block.
const (
	ShapeData   = "data"   // a table, bucket or queue it uses: id, kind, ARN, value, environment name, access
	ShapeSecret = "secret" // a secret it reads: id, ARN, environment name
	ShapeSource = "source" // something that drives it (a queue that triggers it): id, ARN
	ShapeTarget = "target" // another template's block it calls: id, and the ARN from that template's named output
	ShapeWriter = "writer" // an app that sends to it: id. The app's role gets the role's actions on the template's output ARN
)

// ShapeTypes are the OpenTofu types of the variables that receive each shape.
var ShapeTypes = map[string]string{
	ShapeData:   "list(object({id=string,kind=string,arn=string,value=string,env=string,access=string}))",
	ShapeSecret: "list(object({id=string,arn=string,env=string}))",
	ShapeSource: "list(object({id=string,arn=string}))",
	ShapeTarget: "list(object({id=string,arn=string}))",
	ShapeWriter: "list(object({id=string}))",
}

// Standard variables every template module declares; Forgeline sets them.
var standardVars = map[string]string{"name": "string", "tags": "map(string)"}

// Template is one loaded, checked template.
type Template struct {
	Name      string              `json:"name"`
	Version   string              `json:"version"`
	Title     string              `json:"title"`
	Summary   string              `json:"summary"`
	Cloud     string              `json:"cloud"`
	Providers map[string]Provider `json:"providers"`
	HoldsData bool                `json:"holds_data"`
	// PassesRoleTo lists the AWS services the template hands a role to (iam:PassRole), such as lambda.amazonaws.com:
	// the role Forgeline builds with must allow each one (docs/guides/connect-aws.md).
	PassesRoleTo []string        `json:"passes_role_to"`
	Checks       []string        `json:"checks"`
	Inputs       *Schema         `json:"-"`
	RawInputs    json.RawMessage `json:"inputs"`
	Diagram      Diagram         `json:"-"`
	Code         []byte          `json:"-"` // main.tf.json
}

// Provider is a provider a template uses, pinned to the version Forgeline uses.
type Provider struct {
	Source  string `json:"source"`
	Version string `json:"version"`
}

// Diagram is how a template draws and links.
type Diagram struct {
	Icon  string     `json:"icon"`
	Links []LinkRole `json:"links"`
}

// LinkRole is a kind of link a template block offers.
type LinkRole struct {
	Name      string   `json:"name"`      // shown on the link: "uses", "is triggered by"
	Direction string   `json:"direction"` // out: the template block links to the other; in: the other links to it
	Kinds     []string `json:"kinds"`     // block kinds it accepts: table, bucket, queue, secret
	Access    bool     `json:"access"`    // the link takes read or readwrite
	Variable  string   `json:"variable"`  // the module variable that receives the linked blocks
	Shape     string   `json:"shape"`
	Output    string   `json:"output,omitempty"` // target links: the linked template's output that holds its ARN; writer links: this template's
	// Writer links (an app sends to the template): the actions the app's role gets on the ARN in Output, and the output
	// whose value the app gets as <BLOCK>_<EnvSuffix>.
	Actions   []string `json:"actions,omitempty"`
	EnvOutput string   `json:"env_output,omitempty"`
	EnvSuffix string   `json:"env_suffix,omitempty"`
	// How many blocks the role needs and takes; 0 means no limit.
	Min int `json:"min,omitempty"`
	Max int `json:"max,omitempty"`
}

// Ref is name@version.
func (t Template) Ref() string { return t.Name + "@" + t.Version }

var (
	nameRE    = regexp.MustCompile(`^[a-z][a-z0-9-]{2,39}$`)
	versionRE = regexp.MustCompile(`^(0|[1-9]\d{0,3})\.(0|[1-9]\d{0,3})\.(0|[1-9]\d{0,3})$`)
	varRE     = regexp.MustCompile(`^[a-z][a-z0-9_]{0,39}$`)
	serviceRE = regexp.MustCompile(`^[a-z0-9-]+(\.[a-z0-9-]+)*\.amazonaws\.com$`)
	linkKinds = map[string]string{"table": ShapeData, "bucket": ShapeData, "queue": ShapeData, "secret": ShapeSecret}
	actionRE  = regexp.MustCompile(`^([a-z0-9-]{2,30}):[A-Za-z]{2,60}$`)
	suffixRE  = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,19}$`)
	// A writer link never grants actions of these services: they change who may do what.
	deniedActionPrefixes = map[string]bool{"iam": true, "sts": true, "kms": true, "organizations": true, "sso": true, "account": true}
	arnOutputRE          = regexp.MustCompile(`^\$\{(aws_[a-z0-9_]+)\.([a-z0-9_]+)\.arn\}$`)
)

// Pinned are the providers Forgeline allows, at the versions it pins. The AWS one must equal the blueprint package's
// ProviderSource and ProviderVersion (a blueprint test checks it).
var Pinned = map[string]Provider{"aws": {Source: "hashicorp/aws", Version: "~> 6.72"}}

// Load reads one template directory: template.json, diagram.json and main.tf.json.
func Load(fsys fs.FS, dir string) (Template, error) {
	read := func(name string) ([]byte, error) {
		b, err := fs.ReadFile(fsys, path.Join(dir, name))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path.Join(dir, name), err)
		}
		return b, nil
	}
	var t Template
	raw, err := read("template.json")
	if err != nil {
		return t, err
	}
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&t); err != nil {
		return t, fmt.Errorf("%s/template.json: %w", dir, err)
	}
	if err := t.checkMeta(dir); err != nil {
		return t, err
	}
	if t.Inputs, err = ParseSchema(t.RawInputs); err != nil {
		return t, fmt.Errorf("%s/template.json: %w", dir, err)
	}
	if raw, err = read("diagram.json"); err != nil {
		return t, err
	}
	dec = json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&t.Diagram); err != nil {
		return t, fmt.Errorf("%s/diagram.json: %w", dir, err)
	}
	if err := t.checkDiagram(); err != nil {
		return t, fmt.Errorf("%s/diagram.json: %w", dir, err)
	}
	if t.Code, err = read("main.tf.json"); err != nil {
		return t, err
	}
	if err := t.CheckCode(); err != nil {
		return t, fmt.Errorf("%s/main.tf.json: %w", dir, err)
	}
	if err := t.checkWriterOutputs(); err != nil {
		return t, fmt.Errorf("%s/diagram.json: %w", dir, err)
	}
	return t, nil
}

func (t *Template) checkMeta(dir string) error {
	switch {
	case !nameRE.MatchString(t.Name):
		return fmt.Errorf("%s: name must be lower case with dashes", dir)
	case !versionRE.MatchString(t.Version):
		return fmt.Errorf("%s: version must be a plain semantic version such as 1.0.0", dir)
	case path.Clean(dir) != path.Join(path.Dir(path.Clean(dir)), t.Version) || path.Base(path.Dir(path.Clean(dir))) != t.Name:
		return fmt.Errorf("%s: a template lives in <name>/<version>/", dir)
	case strings.TrimSpace(t.Title) == "" || len(t.Title) > 60 || strings.TrimSpace(t.Summary) == "" || len(t.Summary) > 400:
		return fmt.Errorf("%s: give a title (at most 60) and a summary (at most 400)", dir)
	case t.Cloud != "aws":
		return fmt.Errorf("%s: only aws templates for now", dir)
	case len(t.Providers) == 0:
		return fmt.Errorf("%s: declare the providers the code uses", dir)
	case len(t.Checks) > 20:
		return fmt.Errorf("%s: at most 20 checks", dir)
	}
	for _, s := range t.PassesRoleTo {
		if !serviceRE.MatchString(s) {
			return fmt.Errorf("%s: passes_role_to: %q is not an AWS service principal such as lambda.amazonaws.com", dir, s)
		}
	}
	for name, p := range t.Providers {
		pin, ok := Pinned[name]
		if !ok || p != pin {
			return fmt.Errorf("%s: provider %s must be %s %s, the version Forgeline pins", dir, name, pin.Source, pin.Version)
		}
	}
	return nil
}

func (t *Template) checkDiagram() error {
	seen := map[string]bool{}
	for i, r := range t.Diagram.Links {
		at := fmt.Sprintf("links[%d]", i)
		switch {
		case r.Name == "" || len(r.Name) > 40:
			return fmt.Errorf("%s: give the link a short name", at)
		case r.Direction != "out" && r.Direction != "in":
			return fmt.Errorf("%s: direction is out or in", at)
		case !varRE.MatchString(r.Variable) || seen[r.Variable] || t.Inputs.Properties[r.Variable] != nil || standardVars[r.Variable] != "":
			return fmt.Errorf("%s: variable %q must be a new lower-case name", at, r.Variable)
		case ShapeTypes[r.Shape] == "":
			return fmt.Errorf("%s: shape is data, secret, source, target or writer", at)
		case len(r.Kinds) == 0:
			return fmt.Errorf("%s: list the block kinds it accepts", at)
		}
		seen[r.Variable] = true
		if (r.Shape == ShapeTarget || r.Shape == ShapeWriter) != (r.Output != "") {
			return fmt.Errorf("%s: a target or writer link names the output that holds the ARN, and only those do", at)
		}
		if err := r.checkWriter(at); err != nil {
			return err
		}
		if r.Min < 0 || r.Max < 0 || r.Max > 10 || (r.Max > 0 && r.Min > r.Max) {
			return fmt.Errorf("%s: min and max are how many links it needs and takes, 0 to 10, min not above max", at)
		}
		for _, k := range r.Kinds {
			want := linkKinds[k]
			if r.Direction == "in" {
				want = ShapeSource // something that drives the template: only its ARN is passed
				if k == "service" {
					want = ShapeWriter // an app that sends to it
				}
			}
			if name, ok := strings.CutPrefix(k, "template:"); ok {
				want = ""
				if r.Direction == "out" && nameRE.MatchString(name) && name != t.Name {
					want = ShapeTarget
				}
			}
			if want == "" || want != r.Shape {
				return fmt.Errorf("%s: kind %q does not fit shape %s", at, k, r.Shape)
			}
		}
		if r.Access != (r.Shape == ShapeData) {
			return fmt.Errorf("%s: only data links take read or readwrite access", at)
		}
	}
	return nil
}

// checkWriter checks a writer link's actions and outputs, and that no other link sets them.
func (r LinkRole) checkWriter(at string) error {
	if r.Shape != ShapeWriter {
		if len(r.Actions) > 0 || r.EnvOutput != "" || r.EnvSuffix != "" {
			return fmt.Errorf("%s: only a writer link names actions, env_output and env_suffix", at)
		}
		return nil
	}
	if r.Direction != "in" {
		return fmt.Errorf("%s: a writer link points in: the app links to the template", at)
	}
	if len(r.Actions) == 0 || len(r.Actions) > 10 {
		return fmt.Errorf("%s: a writer link lists 1 to 10 actions", at)
	}
	for _, a := range r.Actions {
		m := actionRE.FindStringSubmatch(a)
		if m == nil {
			return fmt.Errorf("%s: action %q must be one action such as firehose:PutRecord, without wildcards", at, a)
		}
		if deniedActionPrefixes[m[1]] {
			return fmt.Errorf("%s: action %q changes who may do what; a writer link never grants it", at, a)
		}
	}
	if r.EnvOutput == "" || !suffixRE.MatchString(r.EnvSuffix) {
		return fmt.Errorf("%s: a writer link names env_output and an env_suffix such as STREAM", at)
	}
	return nil
}

// checkWriterOutputs checks that a writer link's ARN output is the ARN of a resource this template declares, so it can
// never hand an app rights on "*" or on something outside the template, and that its env_output exists.
func (t Template) checkWriterOutputs() error {
	var code struct {
		Resource map[string]map[string]json.RawMessage `json:"resource"`
		Output   map[string]struct {
			Value any `json:"value"`
		} `json:"output"`
	}
	if err := json.Unmarshal(t.Code, &code); err != nil {
		return err
	}
	for i, r := range t.Diagram.Links {
		if r.Shape != ShapeWriter {
			continue
		}
		at := fmt.Sprintf("links[%d]", i)
		o, ok := code.Output[r.Output]
		v, _ := o.Value.(string)
		m := arnOutputRE.FindStringSubmatch(v)
		if !ok || m == nil || code.Resource[m[1]][m[2]] == nil {
			return fmt.Errorf("%s: output %q must be the .arn of a resource the template declares", at, r.Output)
		}
		if _, ok := code.Output[r.EnvOutput]; !ok {
			return fmt.Errorf("%s: env_output %q is not an output of the template", at, r.EnvOutput)
		}
	}
	return nil
}

// Role returns the link role for a link between a template block and a block of kind, in direction.
func (t Template) Role(direction, kind string) (LinkRole, bool) {
	for _, r := range t.Diagram.Links {
		if r.Direction == direction && contains(r.Kinds, kind) {
			return r, true
		}
	}
	return LinkRole{}, false
}

// Data sources that would put a secret's value into the plan or state.
var secretReaders = map[string]bool{
	"aws_secretsmanager_secret_version": true, "aws_secretsmanager_secret_versions": true, "aws_ssm_parameter": true,
	"aws_ssm_parameters_by_path": true, "aws_kms_secrets": true, "aws_kms_secret": true,
}

// CheckCode checks the module: only its declared providers' resources, no providers, modules, backends or
// provisioners, no secret readers, and exactly the variables Forgeline sets.
func (t Template) CheckCode() error {
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(t.Code, &doc); err != nil {
		return fmt.Errorf("not a JSON object: %w", err)
	}
	for k := range doc {
		switch k {
		case "variable", "resource", "data", "output", "locals", "terraform":
		default:
			return fmt.Errorf("%q is not allowed in a template (it may not declare providers, modules or anything else)", k)
		}
	}
	if err := t.checkTerraformBlock(doc["terraform"]); err != nil {
		return err
	}
	for _, section := range []string{"resource", "data"} {
		var byType map[string]map[string]json.RawMessage
		if len(doc[section]) > 0 {
			if err := json.Unmarshal(doc[section], &byType); err != nil {
				return fmt.Errorf("%s: %w", section, err)
			}
		}
		for typ, items := range byType {
			prov, _, _ := strings.Cut(typ, "_")
			if _, ok := t.Providers[prov]; !ok {
				return fmt.Errorf("%s %q belongs to provider %q, which the template does not declare", section, typ, prov)
			}
			if section == "data" && secretReaders[typ] {
				return fmt.Errorf("data %q would read a secret's value into the plan", typ)
			}
			for name, body := range items {
				if hasKey(body, "provisioner") || hasKey(body, "connection") {
					return fmt.Errorf("%s.%s.%s: provisioners are not allowed", section, typ, name)
				}
				if hasKey(body, "provider") {
					return fmt.Errorf("%s.%s.%s: a template uses the blueprint's provider, not its own", section, typ, name)
				}
			}
		}
	}
	return t.checkVariables(doc["variable"])
}

func (t Template) checkTerraformBlock(raw json.RawMessage) error {
	if len(raw) == 0 {
		return errors.New(`declare terraform.required_providers`)
	}
	var tf map[string]json.RawMessage
	if err := json.Unmarshal(raw, &tf); err != nil {
		return fmt.Errorf("terraform: %w", err)
	}
	for k := range tf {
		if k != "required_providers" {
			return fmt.Errorf("terraform.%s is not allowed (no backends, no experiments)", k)
		}
	}
	var req map[string]Provider
	if err := json.Unmarshal(tf["required_providers"], &req); err != nil {
		return fmt.Errorf("terraform.required_providers: %w", err)
	}
	if len(req) != len(t.Providers) {
		return errors.New("terraform.required_providers must list exactly the template's providers")
	}
	for name, p := range req {
		if t.Providers[name] != p {
			return fmt.Errorf("terraform.required_providers.%s must match template.json", name)
		}
	}
	return nil
}

func (t Template) checkVariables(raw json.RawMessage) error {
	var vars map[string]struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(raw, &vars); err != nil || vars == nil {
		return errors.New("variable: declare the module's variables")
	}
	want := map[string]string{}
	for k, v := range standardVars {
		want[k] = v
	}
	for name, p := range t.Inputs.Properties {
		want[name] = p.tfType()
	}
	for _, r := range t.Diagram.Links {
		want[r.Variable] = ShapeTypes[r.Shape]
	}
	for name, typ := range want {
		got, ok := vars[name]
		if !ok {
			return fmt.Errorf("variable %q is not declared", name)
		}
		if strings.ReplaceAll(got.Type, " ", "") != typ {
			return fmt.Errorf("variable %q must have type %s, not %s", name, typ, got.Type)
		}
	}
	for name := range vars {
		if _, ok := want[name]; !ok {
			return fmt.Errorf("variable %q is not an input, a link or a standard variable", name)
		}
	}
	return nil
}

// hasKey says whether key appears anywhere in a JSON value.
func hasKey(raw json.RawMessage, key string) bool {
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return false
	}
	var walk func(any) bool
	walk = func(v any) bool {
		switch x := v.(type) {
		case map[string]any:
			for k, e := range x {
				if k == key || walk(e) {
					return true
				}
			}
		case []any:
			for _, e := range x {
				if walk(e) {
					return true
				}
			}
		}
		return false
	}
	return walk(v)
}

//go:embed library
var library embed.FS

var (
	libOnce sync.Once
	libAll  map[string]Template
	libErr  error
)

// Library returns the first-party templates by name@version. Loading fails as a whole if any template is invalid, so a
// broken template never ships (a test loads them all).
func Library() (map[string]Template, error) {
	libOnce.Do(func() {
		libAll = map[string]Template{}
		libErr = fs.WalkDir(library, "library", func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || path.Base(p) != "template.json" {
				return err
			}
			t, err := Load(library, path.Dir(p))
			if err != nil {
				return err
			}
			libAll[t.Ref()] = t
			return nil
		})
		if libErr == nil {
			libErr = checkTargets(libAll)
		}
	})
	return libAll, libErr
}

// checkTargets checks that every template:<name> link names a template in the library that declares the output.
func checkTargets(all map[string]Template) error {
	for _, t := range all {
		for _, r := range t.Diagram.Links {
			for _, k := range r.Kinds {
				name, ok := strings.CutPrefix(k, "template:")
				if !ok {
					continue
				}
				found := false
				for _, o := range all {
					if o.Name == name && contains(o.Outputs(), r.Output) {
						found = true
					}
				}
				if !found {
					return fmt.Errorf("%s: link %q needs template %s with output %q in the library", t.Ref(), r.Name, name, r.Output)
				}
			}
		}
	}
	return nil
}

// Outputs lists the module's output names.
func (t Template) Outputs() []string {
	var code struct {
		Output map[string]json.RawMessage `json:"output"`
	}
	json.Unmarshal(t.Code, &code)
	out := make([]string, 0, len(code.Output))
	for k := range code.Output {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Get returns one template by name@version.
func Get(ref string) (Template, error) {
	all, err := Library()
	if err != nil {
		return Template{}, err
	}
	t, ok := all[ref]
	if !ok {
		return Template{}, fmt.Errorf("there is no template %q", ref)
	}
	return t, nil
}

// List returns the templates sorted by name, newest version of each first.
func List() ([]Template, error) {
	all, err := Library()
	if err != nil {
		return nil, err
	}
	out := make([]Template, 0, len(all))
	for _, t := range all {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return newer(out[i].Version, out[j].Version)
	})
	return out, nil
}

// newer says whether semantic version a is newer than b (both already checked by versionRE).
func newer(a, b string) bool {
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < 3; i++ {
		if len(pa[i]) != len(pb[i]) {
			return len(pa[i]) > len(pb[i])
		}
		if pa[i] != pb[i] {
			return pa[i] > pb[i]
		}
	}
	return false
}
