// Package guardrail checks an OpenTofu plan before anyone may send it for approval (design note 040, step 1).
//
// It reads `tofu show -json` of a plan, and the code that was planned for values the plan cannot know yet (an IAM
// policy naming a resource that does not exist yet). Each rule says what is wrong, where, and how bad: a deny stops
// the plan from being sent for approval; a warning is shown to the approver. The rules are deliberately few and
// plain; they are the floor every blueprint, template and generic block stands on, whatever made the code.
package guardrail

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// Levels.
const (
	Deny = "deny"
	Warn = "warn"
)

// Finding is one rule a plan breaks.
type Finding struct {
	Rule    string `json:"rule"`    // a short stable name, for example public-ingress
	Level   string `json:"level"`   // deny or warn
	Address string `json:"address"` // the resource, for example aws_security_group.web
	Text    string `json:"text"`    // what is wrong, in plain words
}

// Version names this set of rules; the approver sees which version passed.
const Version = "builtin-2"

// Denied reports whether any finding is a deny.
func Denied(fs []Finding) bool {
	for _, f := range fs {
		if f.Level == Deny {
			return true
		}
	}
	return false
}

type change struct {
	Address string `json:"address"`
	// ModuleAddress is set for a resource inside a module (a template): main.tf.json does not hold its code.
	ModuleAddress string `json:"module_address"`
	Mode          string `json:"mode"`
	Type          string `json:"type"`
	Name          string `json:"name"`
	Change        struct {
		Actions      []string        `json:"actions"`
		After        map[string]any  `json:"after"`
		AfterUnknown json.RawMessage `json:"after_unknown"`
	} `json:"change"`
}

// Check runs every rule on a plan (`tofu show -json`). code is the planned main.tf.json, or nil.
func Check(plan, code []byte) ([]Finding, error) {
	var p struct {
		ResourceChanges []change `json:"resource_changes"`
	}
	if err := json.Unmarshal(plan, &p); err != nil {
		return nil, fmt.Errorf("guardrails could not read the plan: %w", err)
	}
	raw := rawResources(code)
	var out []Finding
	add := func(level, rule, addr, text string) {
		out = append(out, Finding{Rule: rule, Level: level, Address: addr, Text: text})
	}
	for _, rc := range p.ResourceChanges {
		if rc.Mode != "managed" {
			continue
		}
		acts := strings.Join(rc.Change.Actions, ",")
		if strings.Contains(acts, "delete") && holdsData[rc.Type] {
			verb := "deletes"
			if strings.Contains(acts, "create") {
				verb = "replaces (deletes and creates again)"
			}
			add(Warn, "deletes-data", rc.Address, "This "+verb+" "+rc.Address+", which holds data. What it holds is lost unless it was backed up.")
		}
		if !strings.Contains(acts, "create") && !strings.Contains(acts, "update") {
			continue
		}
		a := rc.Change.After
		if a == nil {
			a = map[string]any{}
		}
		unknown := unknownKeys(rc.Change.AfterUnknown)
		r := raw[rc.Type+"."+rc.Name]
		if rc.ModuleAddress != "" {
			r = nil // a template's resource: never read the root's code for it, which may name another resource alike
		}
		val := func(k string) any {
			if v, ok := a[k]; ok && v != nil {
				return v
			}
			if unknown[k] && r != nil {
				return r[k]
			}
			return nil
		}
		switch rc.Type {
		case "aws_iam_user", "aws_iam_access_key", "aws_iam_user_login_profile", "aws_iam_user_policy", "aws_iam_user_policy_attachment":
			add(Deny, "no-iam-users", rc.Address, "Creates an IAM user or long-lived keys. Use roles: services assume them, people sign in through single sign-on.")
		case "aws_vpc_security_group_ingress_rule":
			checkIngress(add, rc.Address, str(val("cidr_ipv4")), str(val("cidr_ipv6")), num(val("from_port")), num(val("to_port")), str(val("ip_protocol")))
		case "aws_security_group", "aws_security_group_rule":
			rules := []any{a}
			if rc.Type == "aws_security_group" {
				rules, _ = a["ingress"].([]any)
			} else if str(a["type"]) != "ingress" {
				rules = nil
			}
			for _, x := range rules {
				m, _ := x.(map[string]any)
				for _, c4 := range strs(m["cidr_blocks"]) {
					checkIngress(add, rc.Address, c4, "", num(m["from_port"]), num(m["to_port"]), str(m["protocol"]))
				}
				for _, c6 := range strs(m["ipv6_cidr_blocks"]) {
					checkIngress(add, rc.Address, "", c6, num(m["from_port"]), num(m["to_port"]), str(m["protocol"]))
				}
			}
		case "aws_db_instance", "aws_rds_cluster":
			if a["publicly_accessible"] == true {
				add(Deny, "public-database", rc.Address, "The database would be reachable from the internet. Databases stay private; apps reach them inside the network.")
			}
			if a["storage_encrypted"] != true && !unknown["storage_encrypted"] {
				add(Deny, "encryption", rc.Address, "The database's storage would not be encrypted.")
			}
		case "aws_elasticache_replication_group":
			if a["at_rest_encryption_enabled"] != true && a["at_rest_encryption_enabled"] != "true" {
				add(Deny, "encryption", rc.Address, "The cache would not be encrypted at rest.")
			}
			if a["transit_encryption_enabled"] != true {
				add(Deny, "encryption", rc.Address, "The cache would not encrypt traffic (TLS off).")
			}
		case "aws_ebs_volume", "aws_efs_file_system":
			if a["encrypted"] != true {
				add(Deny, "encryption", rc.Address, "The disk or file system would not be encrypted.")
			}
		case "aws_s3_bucket_public_access_block":
			for _, k := range []string{"block_public_acls", "block_public_policy", "ignore_public_acls", "restrict_public_buckets"} {
				if a[k] != true {
					add(Deny, "public-bucket", rc.Address, "The bucket's public access block has "+k+" off, so it could be made public.")
					break
				}
			}
		case "aws_iam_role_policy_attachment", "aws_iam_policy_attachment", "aws_iam_group_policy_attachment":
			if arn := str(val("policy_arn")); broadPolicies[arnName(arn)] {
				add(Deny, "broad-permissions", rc.Address, "Attaches "+arnName(arn)+", which allows almost everything. Grant only what the service uses.")
			}
		case "aws_iam_role_policy", "aws_iam_policy", "aws_iam_group_policy":
			checkPolicy(add, rc.Address, val("policy"))
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Level != out[j].Level {
			return out[i].Level == Deny
		}
		return out[i].Address < out[j].Address
	})
	return out, nil
}

// holdsData are resource types whose deletion loses what people stored.
var holdsData = map[string]bool{
	"aws_db_instance": true, "aws_rds_cluster": true, "aws_dynamodb_table": true, "aws_s3_bucket": true,
	"aws_efs_file_system": true, "aws_ebs_volume": true, "aws_elasticache_replication_group": true,
	"aws_secretsmanager_secret": true, "aws_sqs_queue": true, "aws_kinesis_stream": true,
}

var broadPolicies = map[string]bool{"AdministratorAccess": true, "PowerUserAccess": true, "IAMFullAccess": true}

func arnName(arn string) string { return arn[strings.LastIndex(arn, "/")+1:] }

// Web ports may face the internet (a load balancer); nothing else may.
func checkIngress(add func(level, rule, addr, text string), addr, v4, v6 string, from, to int, proto string) {
	if v4 != "0.0.0.0/0" && v6 != "::/0" {
		return
	}
	if (proto == "tcp" || proto == "6") && from == to && (from == 80 || from == 443) {
		add(Warn, "public-web", addr, fmt.Sprintf("Port %d is open to the whole internet. That is right for a front door, and nothing else.", from))
		return
	}
	what := fmt.Sprintf("ports %d to %d", from, to)
	if from == to {
		what = fmt.Sprintf("port %d", from)
	}
	if proto == "-1" || proto == "all" {
		what = "every port"
	}
	add(Deny, "public-ingress", addr, "Opens "+what+" to the whole internet. Only web ports (80, 443) on a front door may be.")
}

// resourceless are actions IAM does not let a policy scope to a resource: "*" is the only Resource they take.
// A statement whose actions are all of these may use "*" without giving anything broad.
var resourceless = map[string]bool{
	"ecr:GetAuthorizationToken": true,
	"xray:PutTraceSegments":     true, "xray:PutTelemetryRecords": true, "xray:GetSamplingRules": true, "xray:GetSamplingTargets": true,
	"cloudwatch:PutMetricData": true,
}

func onlyResourceless(actions []string) bool {
	if len(actions) == 0 {
		return false
	}
	for _, a := range actions {
		if !resourceless[a] {
			return false
		}
	}
	return true
}

// checkPolicy reads an IAM policy document: no Allow on every action or every resource.
func checkPolicy(add func(level, rule, addr, text string), addr string, v any) {
	s, ok := v.(string)
	if !ok || s == "" {
		add(Warn, "unchecked-policy", addr, "This IAM policy is only known when it is applied, so the guardrails could not read it. The approver should read it in the code.")
		return
	}
	var doc struct {
		Statement json.RawMessage
	}
	if json.Unmarshal([]byte(s), &doc) != nil {
		add(Warn, "unchecked-policy", addr, "This IAM policy could not be read as JSON. The approver should read it in the code.")
		return
	}
	var stmts []map[string]any
	if json.Unmarshal(doc.Statement, &stmts) != nil {
		var one map[string]any
		if json.Unmarshal(doc.Statement, &one) == nil {
			stmts = []map[string]any{one}
		}
	}
	for _, st := range stmts {
		if str(st["Effect"]) != "Allow" {
			continue
		}
		for _, act := range strs(st["Action"]) {
			if act == "*" || act == "*:*" {
				add(Deny, "broad-permissions", addr, "The policy allows every action. Grant only the actions the service uses.")
			} else if strings.HasSuffix(act, ":*") {
				add(Warn, "broad-permissions", addr, "The policy allows every action of "+strings.TrimSuffix(act, ":*")+". Consider granting only what is used.")
			}
		}
		for _, res := range strs(st["Resource"]) {
			if res == "*" && !onlyResourceless(strs(st["Action"])) {
				add(Deny, "broad-permissions", addr, "The policy applies to every resource in the account. Name the resources it may use.")
			}
		}
		if _, ok := st["NotAction"]; ok {
			add(Deny, "broad-permissions", addr, "The policy uses NotAction, which allows everything except a list. Name the actions instead.")
		}
	}
}

// rawResources indexes main.tf.json: "type.name" to its attributes as written.
func rawResources(code []byte) map[string]map[string]any {
	var c struct {
		Resource map[string]map[string]map[string]any `json:"resource"`
	}
	out := map[string]map[string]any{}
	if json.Unmarshal(code, &c) != nil {
		return out
	}
	for typ, byName := range c.Resource {
		for name, attrs := range byName {
			out[typ+"."+name] = attrs
		}
	}
	return out
}

func unknownKeys(raw json.RawMessage) map[string]bool {
	var m map[string]any
	json.Unmarshal(raw, &m)
	out := map[string]bool{}
	for k, v := range m {
		if v == true {
			out[k] = true
		}
	}
	return out
}

func str(v any) string { s, _ := v.(string); return s }

func num(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	}
	return -1
}

func strs(v any) []string {
	switch x := v.(type) {
	case string:
		return []string{x}
	case []any:
		var out []string
		for _, e := range x {
			if s, ok := e.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}
