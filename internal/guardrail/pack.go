package guardrail

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// Policy packs (design note 040, step 6): rule sets an organization adds on top of the built-in rules. A pack is run
// by one of three tools, each in a container with no network, on a cut-down copy of the plan (Reduce). This file says
// what a pack is, what the tool is given, and how its answer becomes findings; running the container is the server's
// part.

// The tools a pack can use.
const (
	ToolConftest = "conftest" // the organization's own rules, written in Rego
	ToolTrivy    = "trivy"    // a broad set of known misconfigurations, each with a severity
	ToolCheckov  = "checkov"  // a broader set, without severities when it runs offline
)

// Tool is a pinned release of a tool: the approver sees the version, and the image is named by digest so the same
// bytes run everywhere.
type Tool struct {
	Name    string `json:"name"`
	Label   string `json:"label"`
	Version string `json:"version"`
	Image   string `json:"image"`
}

// Tools are the releases in use. The digests are the multi-platform index digests (amd64 and arm64).
var Tools = map[string]Tool{
	ToolConftest: {Name: ToolConftest, Label: "Conftest", Version: "0.71.1",
		Image: "openpolicyagent/conftest@sha256:0e77cdf7c1fcde035f3c579e4aaa6664ee2a8be5777eaa9af9f520cdd02280c0"},
	ToolTrivy: {Name: ToolTrivy, Label: "Trivy", Version: "0.76.2",
		Image: "aquasec/trivy@sha256:8f6bb947dbc528902e11f2f5be52f7b519f6a39d5d148f41166f2040499768fb"},
	ToolCheckov: {Name: ToolCheckov, Label: "Checkov", Version: "3.4.5",
		Image: "bridgecrew/checkov@sha256:122602cf32cc72286155d39f1f234b7b594818f7b8d7fdf490701620e521368b"},
}

// Limits on what an administrator can store.
const (
	MaxPacks        = 10
	MaxRuleFiles    = 10
	MaxRuleFileSize = 32 << 10
	maxListed       = 50
	maxPackFindings = 100
)

// Pack is one rule set an administrator turned on.
type Pack struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Tool    string `json:"tool"`
	Enabled bool   `json:"enabled"`
	// DenyAt (Trivy only) is the severity from which a finding denies: critical, high, medium or low. Empty: every
	// finding is a warning.
	DenyAt string `json:"deny_at,omitempty"`
	// Deny lists rule ids that deny whatever their severity (Trivy and Checkov), for example CKV_AWS_38.
	Deny []string `json:"deny,omitempty"`
	// Skip lists rule ids the organization decided not to apply.
	Skip []string `json:"skip,omitempty"`
	// Rules (Conftest only) are the Rego files, by file name. A deny rule denies; a warn rule warns.
	Rules map[string]string `json:"rules,omitempty"`
}

// PackRun is the record of one pack on one plan: the approver sees which version checked it.
type PackRun struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Tool     string `json:"tool"`
	Version  string `json:"version"`
	Image    string `json:"image"`
	Denies   int    `json:"denies"`
	Warnings int    `json:"warnings"`
	Skipped  int    `json:"skipped"`
	// Error is set when the pack could not be run or read. The plan is then denied: a pack that did not run is never
	// a pass.
	Error string `json:"error,omitempty"`
}

var (
	packIDRE   = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,30}$`)
	ruleFileRE = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,40}\.rego$`)
	ruleIDRE   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,59}$`)
	severities = map[string]int{"low": 1, "medium": 2, "high": 3, "critical": 4}
)

// Validate says what is wrong with a pack, and where.
func (p Pack) Validate() error {
	if !packIDRE.MatchString(p.ID) {
		return fmt.Errorf("id: %q must be lower-case letters, digits and dashes, at most 31", p.ID)
	}
	if n := strings.TrimSpace(p.Name); n == "" || len(p.Name) > 60 {
		return errors.New("name: needed, at most 60 characters")
	}
	if _, ok := Tools[p.Tool]; !ok {
		return fmt.Errorf("tool: %q must be conftest, trivy or checkov", p.Tool)
	}
	if p.DenyAt != "" {
		if p.Tool != ToolTrivy {
			return errors.New("deny_at: only Trivy rates its findings; list rule ids in deny instead")
		}
		if severities[p.DenyAt] == 0 {
			return fmt.Errorf("deny_at: %q must be critical, high, medium or low", p.DenyAt)
		}
	}
	for name, list := range map[string][]string{"deny": p.Deny, "skip": p.Skip} {
		if len(list) > maxListed {
			return fmt.Errorf("%s: at most %d rule ids", name, maxListed)
		}
		if len(list) > 0 && p.Tool == ToolConftest {
			return fmt.Errorf("%s: a Conftest pack's own rules say what denies; change the rules instead", name)
		}
		for i, id := range list {
			if !ruleIDRE.MatchString(id) {
				return fmt.Errorf("%s[%d]: %q is not a rule id", name, i, id)
			}
		}
	}
	if p.Tool != ToolConftest {
		if len(p.Rules) > 0 {
			return errors.New("rules: only a Conftest pack has its own rules")
		}
		return nil
	}
	if len(p.Rules) == 0 {
		return errors.New("rules: a Conftest pack needs at least one Rego file")
	}
	if len(p.Rules) > MaxRuleFiles {
		return fmt.Errorf("rules: at most %d files", MaxRuleFiles)
	}
	for name, body := range p.Rules {
		if !ruleFileRE.MatchString(name) {
			return fmt.Errorf("rules[%q]: a file name is lower-case letters, digits, dashes and underscores, ending in .rego", name)
		}
		if strings.TrimSpace(body) == "" || len(body) > MaxRuleFileSize {
			return fmt.Errorf("rules[%q]: needed, at most %d KB", name, MaxRuleFileSize>>10)
		}
	}
	return nil
}

// Where the server puts the pack's input inside the container.
const (
	PackDir    = "/in"
	PackPlan   = "tfplan.json"
	PackPolicy = "policy"
)

// Command is what the tool is asked to do, after the image name. Every tool is told not to fetch anything: the
// container has no network.
func (p Pack) Command() []string {
	plan := PackDir + "/" + PackPlan
	switch p.Tool {
	case ToolConftest:
		return []string{"test", "--policy", PackDir + "/" + PackPolicy, "--all-namespaces", "--output", "json", "--no-color", plan}
	case ToolTrivy:
		return []string{"config", "--skip-check-update", "--skip-version-check", "--format", "json", "--quiet", "--cache-dir", "/tmp/cache", plan}
	default:
		return []string{"-f", plan, "--framework", "terraform_plan", "-o", "json", "--skip-download", "--quiet", "--soft-fail"}
	}
}

// Reduce cuts a plan (`tofu show -json`) down to what a pack may read. `tofu show -json` prints sensitive values in
// plain text and carries the whole earlier state, so the copy keeps only the planned values, the changes and the
// configuration, with every value OpenTofu marks sensitive replaced. Variables, outputs' changes and the earlier
// state are left out.
func Reduce(plan []byte) ([]byte, error) {
	dec := json.NewDecoder(bytes.NewReader(plan))
	dec.UseNumber()
	var doc map[string]any
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("the plan could not be read: %w", err)
	}
	out := map[string]any{}
	for _, k := range []string{"format_version", "terraform_version", "planned_values", "resource_changes", "configuration"} {
		if v, ok := doc[k]; ok {
			out[k] = v
		}
	}
	if pv, ok := out["planned_values"].(map[string]any); ok {
		if outs, ok := pv["outputs"].(map[string]any); ok {
			for _, o := range outs {
				if om, ok := o.(map[string]any); ok && om["sensitive"] == true {
					om["value"] = redacted
				}
			}
		}
		if root, ok := pv["root_module"].(map[string]any); ok {
			reduceModule(root)
		}
	}
	if rcs, ok := out["resource_changes"].([]any); ok {
		for _, rc := range rcs {
			c, _ := rc.(map[string]any)["change"].(map[string]any)
			if c == nil {
				continue
			}
			c["before"] = redact(c["before"], c["before_sensitive"])
			c["after"] = redact(c["after"], c["after_sensitive"])
		}
	}
	return json.Marshal(out)
}

const redacted = "(sensitive)"

func reduceModule(m map[string]any) {
	if rs, ok := m["resources"].([]any); ok {
		for _, r := range rs {
			if rm, ok := r.(map[string]any); ok {
				rm["values"] = redact(rm["values"], rm["sensitive_values"])
			}
		}
	}
	if cs, ok := m["child_modules"].([]any); ok {
		for _, c := range cs {
			if cm, ok := c.(map[string]any); ok {
				reduceModule(cm)
			}
		}
	}
}

// redact replaces what the sensitivity mask marks: true hides the whole value, a map or list is followed down.
func redact(v, mask any) any {
	switch m := mask.(type) {
	case bool:
		if m && v != nil {
			return redacted
		}
	case map[string]any:
		if vm, ok := v.(map[string]any); ok {
			for k, x := range vm {
				vm[k] = redact(x, m[k])
			}
		}
	case []any:
		if vl, ok := v.([]any); ok {
			for i := range vl {
				if i < len(m) {
					vl[i] = redact(vl[i], m[i])
				}
			}
		}
	}
	return v
}

// Findings reads the tool's answer (its standard output and exit status) for a plan with these resource addresses.
// An answer it cannot read is an error, never "nothing found". skipped counts findings left out by the pack's skip
// list.
func (p Pack) Findings(stdout []byte, exit int, addresses []string) (fs []Finding, skipped int, err error) {
	var raw []packFinding
	switch p.Tool {
	case ToolConftest:
		raw, err = readConftest(stdout, exit)
	case ToolTrivy:
		raw, err = readTrivy(stdout, exit)
	case ToolCheckov:
		raw, err = readCheckov(stdout, exit)
	default:
		err = fmt.Errorf("unknown tool %q", p.Tool)
	}
	if err != nil {
		return nil, 0, err
	}
	deny, skip := map[string]bool{}, map[string]bool{}
	for _, id := range p.Deny {
		deny[id] = true
	}
	for _, id := range p.Skip {
		skip[id] = true
	}
	for _, r := range raw {
		if r.id != "" && skip[r.id] {
			skipped++
			continue
		}
		level := Warn
		if r.deny || deny[r.id] || (p.DenyAt != "" && severities[strings.ToLower(r.severity)] >= severities[p.DenyAt]) {
			level = Deny
		}
		rule := p.Tool + ":" + r.id
		if r.id == "" {
			rule = p.Tool + ":" + p.ID
		}
		addr := planAddress(r.resource, addresses)
		if addr == "" {
			addr = p.Name
		}
		text := short(r.text, 400)
		if r.severity != "" {
			text += " (" + strings.ToLower(r.severity) + ")"
		}
		fs = append(fs, Finding{Rule: rule, Level: level, Address: short(addr, 200), Text: text})
	}
	sort.SliceStable(fs, func(i, j int) bool { return fs[i].Level == Deny && fs[j].Level != Deny })
	if len(fs) > maxPackFindings {
		more := len(fs) - maxPackFindings
		level := Warn
		if fs[maxPackFindings].Level == Deny {
			level = Deny
		}
		fs = append(fs[:maxPackFindings:maxPackFindings], Finding{Rule: p.Tool + ":" + p.ID, Level: level, Address: p.Name,
			Text: fmt.Sprintf("%d more findings are not shown.", more)})
	}
	return fs, skipped, nil
}

type packFinding struct {
	id, severity, resource, text string
	deny                         bool
}

func short(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}

var hashSuffixRE = regexp.MustCompile(`_[0-9a-f]{32}$`)
var indexRE = regexp.MustCompile(`\[[^\]]*\]$`)

// planAddress finds the plan's address for the resource a tool names. Trivy rewrites the plan as code and names a
// resource inside a module "type.name_<hash>"; when exactly one address in the plan fits, that address is used.
func planAddress(name string, addresses []string) string {
	if name == "" {
		return ""
	}
	bare := hashSuffixRE.ReplaceAllString(name, "")
	found := map[string]bool{}
	for _, a := range addresses {
		if a == name {
			return a
		}
		noIndex := indexRE.ReplaceAllString(a, "")
		if noIndex == bare || strings.HasSuffix(noIndex, "."+bare) {
			found[noIndex] = true
		}
	}
	if len(found) == 1 {
		for a := range found {
			return a
		}
	}
	return bare
}

// Addresses lists the resource addresses a plan changes, for planAddress.
func Addresses(plan []byte) []string {
	var p struct {
		ResourceChanges []struct {
			Address string `json:"address"`
		} `json:"resource_changes"`
	}
	json.Unmarshal(plan, &p)
	out := make([]string, 0, len(p.ResourceChanges))
	for _, rc := range p.ResourceChanges {
		out = append(out, rc.Address)
	}
	return out
}

// Conftest prints one entry per file: failures (deny rules) and warnings (warn rules). It exits 1 when a rule
// failed. A rule may return an object with msg and address; the address is then shown.
func readConftest(stdout []byte, exit int) ([]packFinding, error) {
	if exit != 0 && exit != 1 {
		return nil, fmt.Errorf("conftest ended with status %d", exit)
	}
	var files []struct {
		Warnings, Failures []struct {
			Msg      string         `json:"msg"`
			Metadata map[string]any `json:"metadata"`
		}
	}
	if err := strictJSON(stdout, &files); err != nil || len(files) == 0 {
		return nil, errors.New("conftest's answer could not be read")
	}
	var out []packFinding
	for _, f := range files {
		for _, x := range f.Failures {
			addr, _ := x.Metadata["address"].(string)
			out = append(out, packFinding{text: x.Msg, resource: addr, deny: true})
		}
		for _, x := range f.Warnings {
			addr, _ := x.Metadata["address"].(string)
			out = append(out, packFinding{text: x.Msg, resource: addr})
		}
	}
	return out, nil
}

func readTrivy(stdout []byte, exit int) ([]packFinding, error) {
	if exit != 0 {
		return nil, fmt.Errorf("trivy ended with status %d", exit)
	}
	var doc struct {
		SchemaVersion int
		Results       []struct {
			Misconfigurations []struct {
				ID, Title, Message, Severity, Status string
				CauseMetadata                        struct{ Resource string }
			}
		}
	}
	if err := strictJSON(stdout, &doc); err != nil || doc.SchemaVersion == 0 {
		return nil, errors.New("trivy's answer could not be read")
	}
	var out []packFinding
	for _, r := range doc.Results {
		for _, m := range r.Misconfigurations {
			if m.Status != "FAIL" || m.ID == "" {
				continue
			}
			text := m.Title
			if m.Message != "" && m.Message != m.Title {
				text += ": " + m.Message
			}
			out = append(out, packFinding{id: m.ID, severity: m.Severity, resource: m.CauseMetadata.Resource, text: text})
		}
	}
	return out, nil
}

type checkovReport struct {
	Summary *struct{ Failed int } `json:"summary"`
	Results *struct {
		FailedChecks []struct {
			CheckID         string `json:"check_id"`
			CheckName       string `json:"check_name"`
			Resource        string `json:"resource"`
			ResourceAddress string `json:"resource_address"`
		} `json:"failed_checks"`
	} `json:"results"`
	// A plan with nothing Checkov knows prints only counts.
	Passed *int `json:"passed"`
	Failed *int `json:"failed"`
}

func readCheckov(stdout []byte, exit int) ([]packFinding, error) {
	if exit != 0 {
		return nil, fmt.Errorf("checkov ended with status %d", exit)
	}
	var reports []checkovReport
	if strictJSON(stdout, &reports) != nil {
		var one checkovReport
		if strictJSON(stdout, &one) != nil {
			return nil, errors.New("checkov's answer could not be read")
		}
		reports = []checkovReport{one}
	}
	if len(reports) == 0 {
		return nil, errors.New("checkov's answer could not be read")
	}
	var out []packFinding
	for _, r := range reports {
		switch {
		case r.Results != nil && r.Summary != nil:
			if r.Summary.Failed != len(r.Results.FailedChecks) {
				return nil, errors.New("checkov's answer does not add up")
			}
			for _, c := range r.Results.FailedChecks {
				addr := c.ResourceAddress
				if addr == "" {
					addr = c.Resource
				}
				out = append(out, packFinding{id: c.CheckID, resource: addr, text: c.CheckName})
			}
		case r.Passed != nil && r.Failed != nil && *r.Failed == 0:
		default:
			return nil, errors.New("checkov's answer could not be read")
		}
	}
	return out, nil
}

// strictJSON decodes exactly one JSON value: anything after it (a log line, a second document) is an error.
func strictJSON(b []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(b))
	if err := dec.Decode(v); err != nil {
		return err
	}
	if dec.More() {
		return errors.New("more than one value")
	}
	return nil
}
