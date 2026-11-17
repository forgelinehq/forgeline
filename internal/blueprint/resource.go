package blueprint

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/forgelinehq/forgeline/internal/guardrail"
)

// The generic resource block (design note 040, step 7): any resource type of the AWS provider that an administrator
// allowed for the blueprint's account, with its settings checked against the provider's own schema. Forgeline chooses
// none of its settings, so what keeps it safe is the same as for everything else: the administrator's allow list,
// the guardrails and policy packs on the plan, and a different administrator's approval.
//
// Its links are references: a link from a resource block to another block sets one of its settings (Link.Attribute)
// to one of the other block's attributes (Link.Output), so it can name a queue or a topic another block makes.

// KindResource is a block of any allowed resource type.
const KindResource = "resource"

const (
	maxResourceSettings = 60
	maxResourceJSON     = 32 << 10
	maxResourceDepth    = 8
)

var (
	resourceTypeRE = regexp.MustCompile(`^aws_[a-z0-9_]{1,80}$`)
	attrNameRE     = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)
)

// neverAllowed are resource types no administrator can allow: long-lived credentials and people in IAM, which the
// guardrails deny anyway, and account-wide settings no blueprint should own.
var neverAllowed = []string{"aws_iam_user", "aws_iam_group", "aws_iam_access_key", "aws_iam_policy_attachment",
	"aws_iam_service_specific_credential", "aws_iam_signing_certificate", "aws_iam_account_", "aws_iam_virtual_mfa_device",
	"aws_organizations_", "aws_account_"}

// NeverAllowed says why a resource type may not be allowed, or "" when it may.
func NeverAllowed(typ string) string {
	for _, p := range neverAllowed {
		if typ == p || (strings.HasSuffix(p, "_") && strings.HasPrefix(typ, p)) || strings.HasPrefix(typ, p+"_") {
			return typ + " is never allowed: it makes IAM users, long-lived keys or account-wide settings. Use roles, and change account settings in AWS"
		}
	}
	return ""
}

// What a resource block's link may read from a block Forgeline makes, by kind: the name of the output and the
// attribute of the resource that holds it.
var kindOutputs = map[string]map[string]string{
	KindQueue:     {"arn": "arn", "url": "url", "name": "name"},
	KindTable:     {"arn": "arn", "name": "name"},
	KindBucket:    {"arn": "arn", "name": "bucket"},
	KindLogs:      {"arn": "arn", "name": "name"},
	KindSecret:    {"arn": "arn", "name": "name"},
	KindFrontDoor: {"arn": "arn", "dns_name": "dns_name"},
}

// existingTypes are the data sources an existing block reads (see gen.existing), for links to it.
var existingTypes = map[string]string{"table": "aws_dynamodb_table", "bucket": "aws_s3_bucket", "queue": "aws_sqs_queue", "secret": "aws_secretsmanager_secret"}

// LinkOutputs lists what a resource block can link to on a block of this kind ("" for none). A resource block's own
// attributes come from its schema.
func LinkOutputs(bl Block) []string {
	m := kindOutputs[bl.Kind]
	if bl.Kind == KindExisting && bl.Existing != nil && existingTypes[bl.Existing.Type] != "" {
		m = kindOutputs[bl.Existing.Type]
	}
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// refTo is the expression a link reads: the attribute of the resource behind the block it points to.
func refTo(to Block, output string) (string, bool) {
	switch to.Kind {
	case KindResource:
		return "${" + to.ResourceType + "." + to.ID + "." + output + "}", true
	case KindExisting:
		if to.Existing == nil {
			return "", false
		}
		typ, attr := existingTypes[to.Existing.Type], kindOutputs[to.Existing.Type][output]
		if typ == "" || attr == "" {
			return "", false
		}
		return "${data." + typ + "." + to.ID + "." + attr + "}", true
	}
	attr := kindOutputs[to.Kind][output]
	if attr == "" {
		return "", false
	}
	typ := map[string]string{KindQueue: "aws_sqs_queue", KindTable: "aws_dynamodb_table", KindBucket: "aws_s3_bucket",
		KindLogs: "aws_cloudwatch_log_group", KindSecret: "aws_secretsmanager_secret", KindFrontDoor: "aws_lb"}[to.Kind]
	return "${" + typ + "." + to.ID + "." + attr + "}", true
}

// validateResource checks a resource block's shape; the settings are checked against the schema by CheckResources.
func (bl Block) validateResource(at string) error {
	if !resourceTypeRE.MatchString(bl.ResourceType) {
		return fmt.Errorf("%s.resource_type: pick a resource type, such as aws_sns_topic", at)
	}
	if why := NeverAllowed(bl.ResourceType); why != "" {
		return fmt.Errorf("%s.resource_type: %s", at, why)
	}
	if len(bl.Attributes) > maxResourceSettings {
		return fmt.Errorf("%s.attributes: at most %d settings", at, maxResourceSettings)
	}
	raw, err := json.Marshal(bl.Attributes)
	if err != nil || len(raw) > maxResourceJSON {
		return fmt.Errorf("%s.attributes: at most %d KB of settings", at, maxResourceJSON>>10)
	}
	for name, v := range bl.Attributes {
		if !attrNameRE.MatchString(name) {
			return fmt.Errorf("%s.attributes: %q is not a setting name", at, name)
		}
		if name == "region" {
			return fmt.Errorf("%s.attributes.region: a blueprint builds in one region (its target); a resource cannot choose another", at)
		}
		if depth(v) > maxResourceDepth {
			return fmt.Errorf("%s.attributes.%s: nested too deep", at, name)
		}
	}
	return nil
}

func depth(v any) int {
	d := 0
	switch x := v.(type) {
	case map[string]any:
		for _, e := range x {
			d = max(d, depth(e))
		}
		return d + 1
	case []any:
		for _, e := range x {
			d = max(d, depth(e))
		}
		return d + 1
	}
	return 0
}

// resourceLink checks a link from a resource block: it names the setting it fills and what it reads.
func resourceLink(at string, from, to Block, l Link, filled map[[2]string]bool) error {
	if l.Access != "" || l.Path != "" {
		return fmt.Errorf("%s: a resource's link fills one of its settings; it takes no access or path", at)
	}
	if !attrNameRE.MatchString(l.Attribute) {
		return fmt.Errorf("%s.attribute: name the setting of %s this link fills", at, from.ID)
	}
	if !attrNameRE.MatchString(l.Output) {
		return fmt.Errorf("%s.output: name what it reads from %s", at, to.ID)
	}
	if from.ID == to.ID {
		return fmt.Errorf("%s: a resource cannot read its own attributes", at)
	}
	if _, ok := from.Attributes[l.Attribute]; ok {
		return fmt.Errorf("%s.attribute: %s is already set on %s; set it or link it, not both", at, l.Attribute, from.ID)
	}
	key := [2]string{from.ID, l.Attribute}
	if filled[key] && !l.List {
		return fmt.Errorf("%s.attribute: another link already fills %s on %s", at, l.Attribute, from.ID)
	}
	filled[key] = true
	if _, ok := refTo(to, l.Output); !ok {
		outs := LinkOutputs(to)
		if len(outs) == 0 {
			return fmt.Errorf("%s: a resource can read only from another resource or from a queue, table, files, logs, secret or front door", at)
		}
		return fmt.Errorf("%s.output: %s offers %s", at, to.ID, strings.Join(outs, ", "))
	}
	return nil
}

// ResourceSchema is one resource type's schema from `tofu providers schema -json`, cut down to what the checks and
// the canvas form need.
type ResourceSchema struct {
	Attributes map[string]SchemaAttr  `json:"attributes,omitempty"`
	Blocks     map[string]SchemaBlock `json:"blocks,omitempty"`
}

// SchemaAttr is one attribute. Type is OpenTofu's type in JSON ("string", ["list","string"], ...); an attribute with
// a nested type has none and takes any JSON object or list.
type SchemaAttr struct {
	Type       json.RawMessage `json:"type,omitempty"`
	Required   bool            `json:"required,omitempty"`
	Optional   bool            `json:"optional,omitempty"`
	Computed   bool            `json:"computed,omitempty"`
	Sensitive  bool            `json:"sensitive,omitempty"`
	WriteOnly  bool            `json:"write_only,omitempty"`
	Deprecated bool            `json:"deprecated,omitempty"`
	Nested     bool            `json:"nested,omitempty"`
}

// SchemaBlock is a nested block: single and group take one object, list and set a list of them, map an object of them.
type SchemaBlock struct {
	Nesting  string         `json:"nesting"`
	MinItems int            `json:"min_items,omitempty"`
	MaxItems int            `json:"max_items,omitempty"`
	Block    ResourceSchema `json:"block"`
}

// Settable says whether a person may set an attribute: not one AWS only reports, and never a secret.
func (a SchemaAttr) Settable() bool {
	return (a.Required || a.Optional) && !a.Sensitive && !a.WriteOnly
}

// ParseProviderSchemas reads `tofu providers schema -json` and returns the AWS provider's resource schemas.
func ParseProviderSchemas(full []byte) (map[string]ResourceSchema, string, error) {
	var doc struct {
		ProviderSchemas map[string]struct {
			ResourceSchemas map[string]struct {
				Block rawBlock `json:"block"`
			} `json:"resource_schemas"`
		} `json:"provider_schemas"`
	}
	if err := json.Unmarshal(full, &doc); err != nil {
		return nil, "", fmt.Errorf("the provider schema could not be read: %w", err)
	}
	for addr, p := range doc.ProviderSchemas {
		if !strings.HasSuffix(addr, "/hashicorp/aws") {
			continue
		}
		out := make(map[string]ResourceSchema, len(p.ResourceSchemas))
		for typ, r := range p.ResourceSchemas {
			s := r.Block.reduce()
			// id and tags_all are the provider's own (every resource has an id; tags_all is the tags with the
			// blueprint's default tags): a person never sets them.
			for _, n := range []string{"id", "tags_all"} {
				if a, ok := s.Attributes[n]; ok {
					a.Required, a.Optional = false, false
					s.Attributes[n] = a
				}
			}
			out[typ] = s
		}
		if len(out) == 0 {
			break
		}
		return out, addr, nil
	}
	return nil, "", errors.New("the provider schema has no AWS resources")
}

type rawBlock struct {
	Attributes map[string]struct {
		Type       json.RawMessage `json:"type"`
		NestedType json.RawMessage `json:"nested_type"`
		Required   bool            `json:"required"`
		Optional   bool            `json:"optional"`
		Computed   bool            `json:"computed"`
		Sensitive  bool            `json:"sensitive"`
		WriteOnly  bool            `json:"write_only"`
		Deprecated bool            `json:"deprecated"`
	} `json:"attributes"`
	BlockTypes map[string]struct {
		NestingMode string   `json:"nesting_mode"`
		MinItems    int      `json:"min_items"`
		MaxItems    int      `json:"max_items"`
		Block       rawBlock `json:"block"`
	} `json:"block_types"`
}

func (r rawBlock) reduce() ResourceSchema {
	s := ResourceSchema{}
	for n, a := range r.Attributes {
		if s.Attributes == nil {
			s.Attributes = map[string]SchemaAttr{}
		}
		s.Attributes[n] = SchemaAttr{Type: a.Type, Required: a.Required, Optional: a.Optional, Computed: a.Computed,
			Sensitive: a.Sensitive, WriteOnly: a.WriteOnly, Deprecated: a.Deprecated, Nested: len(a.NestedType) > 0}
	}
	for n, b := range r.BlockTypes {
		if s.Blocks == nil {
			s.Blocks = map[string]SchemaBlock{}
		}
		s.Blocks[n] = SchemaBlock{Nesting: b.NestingMode, MinItems: b.MinItems, MaxItems: b.MaxItems, Block: b.Block.reduce()}
	}
	return s
}

// CheckResources checks every resource block against the provider's schema and the account's allow list: the type
// is allowed, each setting exists, may be set and has the right shape, every required setting is set or linked,
// and each link fills a setting that takes text and reads an attribute that exists.
func (b Blueprint) CheckResources(schema func(typ string) (ResourceSchema, bool), allowed func(typ string) bool) error {
	byID := map[string]Block{}
	for _, bl := range b.Blocks {
		byID[bl.ID] = bl
	}
	linked := map[string]map[string]Link{} // block -> attribute -> link
	for i, l := range b.Links {
		from := byID[l.From]
		if from.Kind != KindResource {
			continue
		}
		at := fmt.Sprintf("links[%d]", i)
		fs, ok := schema(from.ResourceType)
		if !ok {
			continue // reported with the block
		}
		a, ok := fs.Attributes[l.Attribute]
		if !ok {
			return fmt.Errorf("%s.attribute: %s has no setting %q", at, from.ResourceType, l.Attribute)
		}
		if !a.Settable() {
			return fmt.Errorf("%s.attribute: %s cannot be set%s", at, l.Attribute, whyNot(a))
		}
		switch kind := typeKind(a.Type); {
		case l.List && kind != "list":
			return fmt.Errorf("%s.attribute: %s is not a list; link it without list", at, l.Attribute)
		case !l.List && kind != "string":
			return fmt.Errorf("%s.attribute: %s takes %s, not one value; link it as a list or set it", at, l.Attribute, string(a.Type))
		}
		if to := byID[l.To]; to.Kind == KindResource {
			ts, ok := schema(to.ResourceType)
			if !ok {
				continue
			}
			if _, ok := ts.Attributes[l.Output]; !ok {
				return fmt.Errorf("%s.output: %s has no attribute %q", at, to.ResourceType, l.Output)
			}
		}
		if linked[from.ID] == nil {
			linked[from.ID] = map[string]Link{}
		}
		linked[from.ID][l.Attribute] = l
	}
	for i, bl := range b.Blocks {
		if bl.Kind != KindResource {
			continue
		}
		at := fmt.Sprintf("blocks[%d]", i)
		if !allowed(bl.ResourceType) {
			return fmt.Errorf("%s.resource_type: %s is not allowed in this account; an administrator allows resource types under Settings › Resource types", at, bl.ResourceType)
		}
		s, ok := schema(bl.ResourceType)
		if !ok {
			return fmt.Errorf("%s.resource_type: the AWS provider has no resource %s", at, bl.ResourceType)
		}
		if err := checkObject(at+".attributes", s, bl.Attributes, linked[bl.ID]); err != nil {
			return err
		}
	}
	return nil
}

func whyNot(a SchemaAttr) string {
	switch {
	case a.Sensitive || a.WriteOnly:
		return ": it holds a secret. Keep secrets in a secret block, and let the app read it"
	case !a.Required && !a.Optional:
		return ": AWS sets it"
	}
	return ""
}

// checkObject checks settings against a block schema. linked are the top-level settings links fill.
func checkObject(at string, s ResourceSchema, in map[string]any, linked map[string]Link) error {
	names := make([]string, 0, len(in))
	for n := range in {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		v := in[n]
		if a, ok := s.Attributes[n]; ok {
			if !a.Settable() {
				return fmt.Errorf("%s.%s: it cannot be set%s", at, n, whyNot(a))
			}
			if a.Nested {
				if err := plainJSON(at+"."+n, v); err != nil {
					return err
				}
				continue
			}
			if err := checkValue(at+"."+n, a.Type, v); err != nil {
				return err
			}
			continue
		}
		if nb, ok := s.Blocks[n]; ok {
			if err := checkBlock(at+"."+n, nb, v); err != nil {
				return err
			}
			continue
		}
		return fmt.Errorf("%s.%s: there is no such setting", at, n)
	}
	for n, a := range s.Attributes {
		if _, set := in[n]; a.Required && !set && linked[n].Attribute == "" {
			return fmt.Errorf("%s.%s: it is required; set it or link it", at, n)
		}
	}
	for n, nb := range s.Blocks {
		if _, set := in[n]; nb.MinItems > 0 && !set {
			return fmt.Errorf("%s.%s: it is required", at, n)
		}
	}
	return nil
}

func checkBlock(at string, nb SchemaBlock, v any) error {
	switch nb.Nesting {
	case "single", "group":
		m, ok := v.(map[string]any)
		if !ok {
			return fmt.Errorf("%s: give one object", at)
		}
		return checkObject(at, nb.Block, m, nil)
	case "list", "set":
		l, ok := v.([]any)
		if !ok {
			if m, ok := v.(map[string]any); ok {
				l = []any{m}
			} else {
				return fmt.Errorf("%s: give a list of objects", at)
			}
		}
		if len(l) < nb.MinItems || (nb.MaxItems > 0 && len(l) > nb.MaxItems) {
			return fmt.Errorf("%s: give between %d and %d", at, nb.MinItems, max(nb.MaxItems, len(l)-1))
		}
		for i, e := range l {
			m, ok := e.(map[string]any)
			if !ok {
				return fmt.Errorf("%s[%d]: give an object", at, i)
			}
			if err := checkObject(fmt.Sprintf("%s[%d]", at, i), nb.Block, m, nil); err != nil {
				return err
			}
		}
		return nil
	case "map":
		m, ok := v.(map[string]any)
		if !ok {
			return fmt.Errorf("%s: give an object of objects", at)
		}
		for k, e := range m {
			em, ok := e.(map[string]any)
			if !ok {
				return fmt.Errorf("%s.%s: give an object", at, k)
			}
			if err := checkObject(at+"."+k, nb.Block, em, nil); err != nil {
				return err
			}
		}
		return nil
	}
	return fmt.Errorf("%s: this kind of setting (%s) cannot be set here", at, nb.Nesting)
}

// typeKind sorts an OpenTofu type for links: string, list (of strings) or other.
func typeKind(t json.RawMessage) string {
	var s string
	if json.Unmarshal(t, &s) == nil {
		if s == "string" {
			return "string"
		}
		return "other"
	}
	var l []json.RawMessage
	if json.Unmarshal(t, &l) == nil && len(l) == 2 {
		var c, e string
		json.Unmarshal(l[0], &c)
		json.Unmarshal(l[1], &e)
		if (c == "list" || c == "set") && e == "string" {
			return "list"
		}
	}
	return "other"
}

// checkValue checks a JSON value against an OpenTofu type.
func checkValue(at string, t json.RawMessage, v any) error {
	var prim string
	if json.Unmarshal(t, &prim) == nil {
		ok := false
		switch prim {
		case "string":
			_, ok = v.(string)
		case "number":
			_, ok = v.(float64)
			if !ok {
				_, ok = v.(json.Number)
			}
		case "bool":
			_, ok = v.(bool)
		case "dynamic":
			return plainJSON(at, v)
		}
		if !ok {
			return fmt.Errorf("%s: give a %s", at, prim)
		}
		return nil
	}
	var l []json.RawMessage
	if json.Unmarshal(t, &l) != nil || len(l) < 2 {
		return fmt.Errorf("%s: this setting's type cannot be read", at)
	}
	var c string
	json.Unmarshal(l[0], &c)
	switch c {
	case "list", "set":
		arr, ok := v.([]any)
		if !ok {
			return fmt.Errorf("%s: give a list", at)
		}
		for i, e := range arr {
			if err := checkValue(fmt.Sprintf("%s[%d]", at, i), l[1], e); err != nil {
				return err
			}
		}
		return nil
	case "map":
		m, ok := v.(map[string]any)
		if !ok {
			return fmt.Errorf("%s: give an object", at)
		}
		for k, e := range m {
			if err := checkValue(at+"."+k, l[1], e); err != nil {
				return err
			}
		}
		return nil
	case "object":
		m, ok := v.(map[string]any)
		if !ok {
			return fmt.Errorf("%s: give an object", at)
		}
		var fields map[string]json.RawMessage
		json.Unmarshal(l[1], &fields)
		for k, e := range m {
			ft, ok := fields[k]
			if !ok {
				return fmt.Errorf("%s.%s: there is no such field", at, k)
			}
			if err := checkValue(at+"."+k, ft, e); err != nil {
				return err
			}
		}
		return nil
	case "tuple":
		arr, ok := v.([]any)
		var ts []json.RawMessage
		json.Unmarshal(l[1], &ts)
		if !ok || len(arr) != len(ts) {
			return fmt.Errorf("%s: give a list of %d", at, len(ts))
		}
		for i, e := range arr {
			if err := checkValue(fmt.Sprintf("%s[%d]", at, i), ts[i], e); err != nil {
				return err
			}
		}
		return nil
	}
	return fmt.Errorf("%s: this setting's type cannot be read", at)
}

// plainJSON accepts text, numbers, booleans, lists and objects: what JSON can hold.
func plainJSON(at string, v any) error {
	switch x := v.(type) {
	case string, float64, bool, json.Number, nil:
		return nil
	case []any:
		for i, e := range x {
			if err := plainJSON(fmt.Sprintf("%s[%d]", at, i), e); err != nil {
				return err
			}
		}
		return nil
	case map[string]any:
		for k, e := range x {
			if err := plainJSON(at+"."+k, e); err != nil {
				return err
			}
		}
		return nil
	}
	return fmt.Errorf("%s: not a JSON value", at)
}

// escDeep escapes every string in a value, keys included, so nothing a person typed is read as an expression.
func escDeep(v any) any {
	switch x := v.(type) {
	case string:
		return esc(x)
	case map[string]any:
		m := obj{}
		for k, e := range x {
			m[esc(k)] = escDeep(e)
		}
		return m
	case []any:
		l := make([]any, len(x))
		for i, e := range x {
			l[i] = escDeep(e)
		}
		return l
	}
	return v
}

// unprotected are the settings that let a resource that holds data be deleted, for step 1 of removing everything;
// they match what NeedsUnprotect looks for in the state.
func unprotected(typ string, finalSnapshot bool) obj {
	switch typ {
	case "aws_s3_bucket":
		return obj{"force_destroy": true}
	case "aws_dynamodb_table":
		return obj{"deletion_protection_enabled": false}
	case "aws_db_instance":
		return obj{"deletion_protection": false, "skip_final_snapshot": !finalSnapshot}
	}
	return nil
}

// resource generates a resource block: its settings escaped, its links as references.
func (g *gen) resource(bl Block) {
	body := obj{}
	for k, v := range bl.Attributes {
		body[k] = escDeep(v)
	}
	for _, l := range g.b.Links {
		if l.From != bl.ID {
			continue
		}
		expr, ok := refTo(g.byID[l.To], l.Output)
		if !ok {
			continue
		}
		if l.List {
			cur, _ := body[l.Attribute].([]any)
			body[l.Attribute] = append(cur, expr)
		} else {
			body[l.Attribute] = expr
		}
	}
	if g.rm.unprotect {
		for k, v := range unprotected(bl.ResourceType, g.rm.finalSnapshot) {
			body[k] = v
		}
	}
	g.add(bl.ResourceType, bl.ID, body)
}

// holdsData says whether losing this block loses what people stored in it.
func (bl Block) holdsData() bool {
	if bl.Kind == KindResource {
		return guardrail.HoldsData(bl.ResourceType)
	}
	return HoldsData(bl.Kind)
}

// ResourceLabel names a resource type in plain words: aws_sns_topic is "SNS topic".
func ResourceLabel(typ string) string {
	words := strings.Split(strings.TrimPrefix(typ, "aws_"), "_")
	for i, w := range words {
		if s, ok := labelWords[w]; ok {
			words[i] = s
		} else if i == 0 && len(w) <= 3 {
			words[i] = strings.ToUpper(w)
		}
	}
	return strings.Join(words, " ")
}

var labelWords = map[string]string{
	"s3": "S3", "sqs": "SQS", "sns": "SNS", "iam": "IAM", "kms": "KMS", "ec2": "EC2", "ecs": "ECS", "eks": "EKS",
	"ecr": "ECR", "lb": "load balancer", "vpc": "VPC", "rds": "RDS", "ssm": "SSM parameter store", "api": "API",
	"acm": "ACM", "waf": "WAF", "wafv2": "WAF", "dns": "DNS", "cloudfront": "CloudFront", "cloudwatch": "CloudWatch",
	"dynamodb": "DynamoDB", "route53": "Route 53", "apigatewayv2": "API Gateway", "api_gateway": "API Gateway",
	"sfn": "Step Functions", "lambda": "Lambda", "kinesis": "Kinesis", "glue": "Glue", "athena": "Athena",
	"cognito": "Cognito", "ses": "SES", "sesv2": "SES", "efs": "EFS", "ebs": "EBS", "elasticache": "ElastiCache",
	"opensearch": "OpenSearch", "bedrock": "Bedrock", "bedrockagent": "Bedrock agent", "scheduler": "EventBridge Scheduler",
	"cloudwatch_event": "EventBridge", "appsync": "AppSync", "secretsmanager": "Secrets Manager", "backup": "Backup",
	"budgets": "Budgets", "ce": "Cost Explorer", "xray": "X-Ray", "mq": "Amazon MQ", "msk": "MSK", "sagemaker": "SageMaker",
	"arn": "ARN", "url": "URL", "id": "ID", "ip": "IP", "tls": "TLS", "ssl": "SSL", "http": "HTTP", "https": "HTTPS",
}
