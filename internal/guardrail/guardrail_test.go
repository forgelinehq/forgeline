package guardrail

import (
	"encoding/json"
	"strings"
	"testing"
)

// rc is one resource change in a hand-made plan.
type rc struct {
	addr    string
	actions []string
	after   map[string]any
	unknown map[string]any
}

func plan(t *testing.T, rcs ...rc) []byte {
	t.Helper()
	var out []map[string]any
	for _, r := range rcs {
		typ, name, _ := strings.Cut(r.addr, ".")
		acts := r.actions
		if acts == nil {
			acts = []string{"create"}
		}
		out = append(out, map[string]any{"address": r.addr, "mode": "managed", "type": typ, "name": name,
			"change": map[string]any{"actions": acts, "after": r.after, "after_unknown": r.unknown}})
	}
	b, _ := json.Marshal(map[string]any{"resource_changes": out})
	return b
}

func rules(fs []Finding) string {
	var s []string
	for _, f := range fs {
		s = append(s, f.Level+":"+f.Rule)
	}
	return strings.Join(s, ",")
}

func TestRules(t *testing.T) {
	policy := func(stmt string) map[string]any {
		return map[string]any{"policy": `{"Version":"2012-10-17","Statement":` + stmt + `}`}
	}
	cases := []struct {
		name string
		rc   rc
		want string
	}{
		{"a web port on the internet is a warning", rc{addr: "aws_vpc_security_group_ingress_rule.door_lb_443", after: map[string]any{"cidr_ipv4": "0.0.0.0/0", "ip_protocol": "tcp", "from_port": 443, "to_port": 443}}, "warn:public-web"},
		{"ssh to the internet is denied", rc{addr: "aws_vpc_security_group_ingress_rule.ssh", after: map[string]any{"cidr_ipv4": "0.0.0.0/0", "ip_protocol": "tcp", "from_port": 22, "to_port": 22}}, "deny:public-ingress"},
		{"ipv6 too", rc{addr: "aws_vpc_security_group_ingress_rule.db6", after: map[string]any{"cidr_ipv6": "::/0", "ip_protocol": "tcp", "from_port": 3306, "to_port": 3306}}, "deny:public-ingress"},
		{"every port", rc{addr: "aws_vpc_security_group_ingress_rule.all", after: map[string]any{"cidr_ipv4": "0.0.0.0/0", "ip_protocol": "-1"}}, "deny:public-ingress"},
		{"a port range that includes 80 is not a web port", rc{addr: "aws_vpc_security_group_ingress_rule.range", after: map[string]any{"cidr_ipv4": "0.0.0.0/0", "ip_protocol": "tcp", "from_port": 0, "to_port": 65535}}, "deny:public-ingress"},
		{"a range from 80 to 443 is not one web port", rc{addr: "aws_vpc_security_group_ingress_rule.r2", after: map[string]any{"cidr_ipv4": "0.0.0.0/0", "ip_protocol": "tcp", "from_port": 80, "to_port": 443}}, "deny:public-ingress"},
		{"udp on port 80 is not web", rc{addr: "aws_vpc_security_group_ingress_rule.u", after: map[string]any{"cidr_ipv4": "0.0.0.0/0", "ip_protocol": "udp", "from_port": 80, "to_port": 80}}, "deny:public-ingress"},
		{"inline group rules", rc{addr: "aws_security_group.old", after: map[string]any{"ingress": []any{map[string]any{"cidr_blocks": []any{"0.0.0.0/0"}, "protocol": "tcp", "from_port": 3389, "to_port": 3389}}}}, "deny:public-ingress"},
		{"a separate group rule", rc{addr: "aws_security_group_rule.r", after: map[string]any{"type": "ingress", "cidr_blocks": []any{"0.0.0.0/0"}, "protocol": "tcp", "from_port": 5432, "to_port": 5432}}, "deny:public-ingress"},
		{"an egress rule is fine", rc{addr: "aws_security_group_rule.e", after: map[string]any{"type": "egress", "cidr_blocks": []any{"0.0.0.0/0"}, "protocol": "-1", "from_port": 0, "to_port": 0}}, ""},
		{"inside the network is fine", rc{addr: "aws_vpc_security_group_ingress_rule.db", after: map[string]any{"referenced_security_group_id": "sg-1", "ip_protocol": "tcp", "from_port": 3306, "to_port": 3306}}, ""},
		{"a public database", rc{addr: "aws_db_instance.db", after: map[string]any{"publicly_accessible": true, "storage_encrypted": true}}, "deny:public-database"},
		{"an unencrypted database", rc{addr: "aws_db_instance.db", after: map[string]any{"publicly_accessible": false, "storage_encrypted": false}}, "deny:encryption"},
		{"encryption left out is unencrypted", rc{addr: "aws_rds_cluster.c", after: map[string]any{}}, "deny:encryption"},
		{"a private encrypted database", rc{addr: "aws_db_instance.db", after: map[string]any{"publicly_accessible": false, "storage_encrypted": true}}, ""},
		{"a cache without TLS", rc{addr: "aws_elasticache_replication_group.r", after: map[string]any{"at_rest_encryption_enabled": true, "transit_encryption_enabled": false}}, "deny:encryption"},
		{"a cache unencrypted at rest", rc{addr: "aws_elasticache_replication_group.r", after: map[string]any{"at_rest_encryption_enabled": false, "transit_encryption_enabled": true}}, "deny:encryption"},
		{"an unencrypted disk", rc{addr: "aws_ebs_volume.v", after: map[string]any{"encrypted": false}}, "deny:encryption"},
		{"a bucket that could be public", rc{addr: "aws_s3_bucket_public_access_block.b", after: map[string]any{"block_public_acls": true, "block_public_policy": false, "ignore_public_acls": true, "restrict_public_buckets": true}}, "deny:public-bucket"},
		{"a closed bucket", rc{addr: "aws_s3_bucket_public_access_block.b", after: map[string]any{"block_public_acls": true, "block_public_policy": true, "ignore_public_acls": true, "restrict_public_buckets": true}}, ""},
		{"an IAM user", rc{addr: "aws_iam_user.u", after: map[string]any{"name": "bot"}}, "deny:no-iam-users"},
		{"access keys", rc{addr: "aws_iam_access_key.k", after: map[string]any{}}, "deny:no-iam-users"},
		{"administrator access", rc{addr: "aws_iam_role_policy_attachment.a", after: map[string]any{"policy_arn": "arn:aws:iam::aws:policy/AdministratorAccess"}}, "deny:broad-permissions"},
		{"the ECS execution policy is fine", rc{addr: "aws_iam_role_policy_attachment.x", after: map[string]any{"policy_arn": "arn:aws:iam::aws:policy/service-role/AmazonECSTaskExecutionRolePolicy"}}, ""},
		{"every action", rc{addr: "aws_iam_role_policy.p", after: policy(`[{"Effect":"Allow","Action":"*","Resource":"arn:aws:s3:::b"}]`)}, "deny:broad-permissions"},
		{"every resource", rc{addr: "aws_iam_role_policy.p", after: policy(`[{"Effect":"Allow","Action":["s3:GetObject"],"Resource":"*"}]`)}, "deny:broad-permissions"},
		{"actions that only take every resource", rc{addr: "aws_iam_role_policy.p", after: policy(`[{"Effect":"Allow","Action":["ecr:GetAuthorizationToken"],"Resource":"*"},{"Effect":"Allow","Action":["xray:PutTraceSegments","xray:PutTelemetryRecords"],"Resource":"*"}]`)}, ""},
		{"one scoped action with them is still broad", rc{addr: "aws_iam_role_policy.p", after: policy(`[{"Effect":"Allow","Action":["ecr:GetAuthorizationToken","ecr:BatchGetImage"],"Resource":"*"}]`)}, "deny:broad-permissions"},
		{"a resourceless action named as a wildcard is broad", rc{addr: "aws_iam_role_policy.p", after: policy(`[{"Effect":"Allow","Action":["xray:*"],"Resource":"*"}]`)}, "deny:broad-permissions,warn:broad-permissions"},
		{"NotAction", rc{addr: "aws_iam_policy.p", after: policy(`{"Effect":"Allow","NotAction":"iam:*","Resource":"arn:aws:s3:::b"}`)}, "deny:broad-permissions"},
		{"every action of one service", rc{addr: "aws_iam_role_policy.p", after: policy(`[{"Effect":"Allow","Action":"s3:*","Resource":"arn:aws:s3:::b"}]`)}, "warn:broad-permissions"},
		{"a deny statement may be broad", rc{addr: "aws_iam_role_policy.p", after: policy(`[{"Effect":"Deny","Action":"*","Resource":"*"}]`)}, ""},
		{"a named policy", rc{addr: "aws_iam_role_policy.p", after: policy(`[{"Effect":"Allow","Action":["sqs:SendMessage"],"Resource":["arn:aws:sqs:ap-south-1:1:q"]}]`)}, ""},
		{"an unknown policy is a warning", rc{addr: "aws_iam_role_policy.p", after: map[string]any{}, unknown: map[string]any{"policy": true}}, "warn:unchecked-policy"},
		{"deleting a table", rc{addr: "aws_dynamodb_table.t", actions: []string{"delete"}}, "warn:deletes-data"},
		{"replacing a database", rc{addr: "aws_db_instance.db", actions: []string{"delete", "create"}, after: map[string]any{"publicly_accessible": false, "storage_encrypted": true}}, "warn:deletes-data"},
		{"deleting a log group is not data", rc{addr: "aws_cloudwatch_log_group.l", actions: []string{"delete"}}, ""},
		{"a no-op is not checked", rc{addr: "aws_iam_user.u", actions: []string{"no-op"}}, ""},
	}
	for _, c := range cases {
		fs, err := Check(plan(t, c.rc), nil)
		if err != nil {
			t.Fatal(err)
		}
		if got := rules(fs); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

// A policy that names resources not built yet is unknown in the plan; the planned code still shows it.
func TestUnknownPolicyReadFromCode(t *testing.T) {
	code := []byte(`{"resource":{"aws_iam_role_policy":{"p":{"policy":"{\"Statement\":[{\"Effect\":\"Allow\",\"Action\":\"*\",\"Resource\":\"${aws_sqs_queue.q.arn}\"}]}"}}}}`)
	p := plan(t, rc{addr: "aws_iam_role_policy.p", after: map[string]any{}, unknown: map[string]any{"policy": true}})
	fs, _ := Check(p, code)
	if rules(fs) != "deny:broad-permissions" {
		t.Errorf("got %v", fs)
	}
	ok := []byte(`{"resource":{"aws_iam_role_policy":{"p":{"policy":"{\"Statement\":[{\"Effect\":\"Allow\",\"Action\":[\"sqs:SendMessage\"],\"Resource\":[\"${aws_sqs_queue.q.arn}\"]}]}"}}}}`)
	if fs, _ := Check(p, ok); len(fs) != 0 || Denied(fs) {
		t.Errorf("a named policy from the code: %v", fs)
	}
}

// A resource inside a module (a template) never borrows the root's code for a resource with the same type and name:
// its unknown policy is reported as unchecked, not read from someone else's.
func TestModuleResourceDoesNotUseRootCode(t *testing.T) {
	root := []byte(`{"resource":{"aws_iam_role_policy":{"fn":{"policy":"{\"Statement\":[{\"Effect\":\"Allow\",\"Action\":[\"sqs:SendMessage\"],\"Resource\":[\"x\"]}]}"}}}}`)
	b, _ := json.Marshal(map[string]any{"resource_changes": []map[string]any{{"address": "module.jobs.aws_iam_role_policy.fn", "module_address": "module.jobs",
		"mode": "managed", "type": "aws_iam_role_policy", "name": "fn", "change": map[string]any{"actions": []string{"create"}, "after": map[string]any{}, "after_unknown": map[string]any{"policy": true}}}}})
	fs, _ := Check(b, root)
	if rules(fs) != "warn:unchecked-policy" {
		t.Errorf("a module's unknown policy = %v", fs)
	}
}

func TestDeniesComeFirstAndBadPlans(t *testing.T) {
	fs, _ := Check(plan(t,
		rc{addr: "aws_vpc_security_group_ingress_rule.web", after: map[string]any{"cidr_ipv4": "0.0.0.0/0", "ip_protocol": "tcp", "from_port": 80, "to_port": 80}},
		rc{addr: "aws_iam_user.u", after: map[string]any{}}), nil)
	if len(fs) != 2 || fs[0].Level != Deny || !Denied(fs) {
		t.Errorf("order: %v", fs)
	}
	if _, err := Check([]byte("not json"), nil); err == nil {
		t.Errorf("a broken plan must be an error, not a pass")
	}
}
