package blueprint

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/forgelinehq/forgeline/internal/template"
)

// The OpenTofu provider this code is written for (note 035). Bump it with the generator, never on its own.
const (
	ProviderSource  = "hashicorp/aws"
	ProviderVersion = "~> 6.72"
)

type obj = map[string]any

// Tofu returns the blueprint as an OpenTofu configuration in JSON syntax (main.tf.json). The blueprint must be
// valid. Everything a person typed is escaped, so OpenTofu never evaluates it.
func (b Blueprint) Tofu() ([]byte, error) {
	if err := b.Validate(); err != nil {
		return nil, err
	}
	return b.tofu(removal{})
}

func (b Blueprint) tofu(rm removal) ([]byte, error) {
	g := gen{b: b, rm: rm, res: obj{}, data: obj{}, out: obj{}, mod: obj{}, byID: map[string]Block{}}
	sum := sha256.Sum256([]byte(b.ID))
	g.short = hex.EncodeToString(sum[:])[:6]
	for _, bl := range b.Blocks {
		g.byID[bl.ID] = bl
	}
	g.build()
	doc := obj{
		"terraform": obj{"required_providers": obj{"aws": obj{"source": ProviderSource, "version": ProviderVersion}}},
		"provider": obj{"aws": obj{"region": b.Target.Region, "default_tags": obj{"tags": obj{
			"forgeline:blueprint": esc(b.ID), "forgeline:managed-by": "forgeline",
		}}}},
	}
	if len(g.res) > 0 {
		doc["resource"] = g.res
	}
	if len(g.data) > 0 {
		doc["data"] = g.data
	}
	if len(g.out) > 0 {
		doc["output"] = g.out
	}
	if len(g.mod) > 0 {
		doc["module"] = g.mod
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(doc); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

type gen struct {
	b     Blueprint
	rm    removal // step 1 of removing everything: data blocks without their protection
	short string  // six hex characters from the blueprint id, so names do not clash between blueprints
	res   obj     // resource type -> name -> body
	data  obj
	out   obj
	mod   obj // template blocks: module name -> call
	byID  map[string]Block
}

func (g *gen) add(kind, name string, body obj) {
	m, _ := g.res[kind].(obj)
	if m == nil {
		m = obj{}
		g.res[kind] = m
	}
	m[name] = body
}

func (g *gen) addData(kind, name string, body obj) {
	m, _ := g.data[kind].(obj)
	if m == nil {
		m = obj{}
		g.data[kind] = m
	}
	m[name] = body
}

// esc keeps text a person typed from being read as an OpenTofu template.
func esc(s string) string {
	return strings.NewReplacer("${", "$${", "%{", "%%{").Replace(s)
}

// awsName is a name for AWS: fl-<blueprint>-<block>, cut to n characters.
func (g *gen) awsName(id string, n int) string {
	s := "fl-" + g.short + "-" + id
	if len(s) > n {
		s = strings.TrimRight(s[:n], "-")
	}
	return s
}

func ref(kind, name, attr string) string { return "${" + kind + "." + name + "." + attr + "}" }

func tags(bl Block) obj { return obj{"Name": esc(bl.Name), "forgeline:block": bl.ID} }

func (g *gen) build() {
	needNet, needCluster := false, false
	for _, bl := range g.b.Blocks {
		switch bl.Kind {
		case KindFrontDoor:
			needNet = true
		case KindService:
			needNet, needCluster = true, true
		case KindDatabase, KindCache:
			needNet = true
		case KindAgent:
			g.addData("aws_caller_identity", "current", obj{})
		}
	}
	// A private DNS namespace when one service calls another (Cloud Map).
	for _, l := range g.b.Links {
		if g.byID[l.From].Kind == KindService && g.byID[l.To].Kind == KindService {
			g.add("aws_service_discovery_private_dns_namespace", "main", obj{"name": g.namespace(), "vpc": "${data.aws_vpc.default.id}", "description": "Forgeline blueprint " + esc(g.b.Name)})
			break
		}
	}
	if needNet {
		g.addData("aws_vpc", "default", obj{"default": true})
		g.addData("aws_subnets", "default", obj{"filter": []obj{{"name": "vpc-id", "values": []string{"${data.aws_vpc.default.id}"}}}})
	}
	if needCluster {
		g.add("aws_ecs_cluster", "main", obj{"name": g.awsName("cluster", 255), "setting": []obj{{"name": "containerInsights", "value": "disabled"}}})
		g.addData("aws_iam_policy_document", "ecs_tasks_assume", obj{"statement": []obj{{
			"actions": []string{"sts:AssumeRole"}, "principals": []obj{{"type": "Service", "identifiers": []string{"ecs-tasks.amazonaws.com"}}},
		}}})
	}
	ids := make([]string, 0, len(g.byID))
	for id := range g.byID {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		bl := g.byID[id]
		switch bl.Kind {
		case KindTable:
			g.table(bl)
		case KindBucket:
			g.bucket(bl)
		case KindQueue:
			g.queue(bl)
		case KindLogs:
			g.add("aws_cloudwatch_log_group", bl.ID, obj{"name": "/forgeline/" + esc(g.b.ID) + "/" + bl.ID, "retention_in_days": bl.RetentionDays, "tags": tags(bl)})
		case KindExisting:
			g.existing(bl)
		case KindFrontDoor:
			g.frontDoor(bl)
		case KindDatabase:
			g.database(bl)
		case KindCache:
			g.cache(bl)
		case KindSecret:
			g.add("aws_secretsmanager_secret", bl.ID, g.protect(obj{"name": "forgeline/" + g.short + "/" + bl.ID, "description": esc(bl.Description), "recovery_window_in_days": 7,
				"lifecycle": obj{"prevent_destroy": true}, "tags": tags(bl)}))
		}
	}
	for _, id := range ids {
		if bl := g.byID[id]; bl.Kind == KindService {
			g.service(bl)
		} else if bl.Kind == KindAgent {
			g.agent(bl)
		} else if bl.Kind == KindTemplate {
			g.template(bl)
		}
		if bl := g.byID[id]; bl.Source != nil {
			g.source(bl)
		}
	}
}

func (g *gen) table(bl Block) {
	attrs := []obj{{"name": esc(bl.PartitionKey), "type": bl.PartitionType}}
	body := obj{
		"name": g.awsName(bl.ID, 255), "billing_mode": "PAY_PER_REQUEST", "hash_key": esc(bl.PartitionKey),
		"point_in_time_recovery": obj{"enabled": true}, "server_side_encryption": obj{"enabled": true},
		"deletion_protection_enabled": true, "lifecycle": obj{"prevent_destroy": true}, "tags": tags(bl),
	}
	if g.rm.unprotect {
		body["deletion_protection_enabled"] = false
	}
	body = g.protect(body)
	if bl.SortKey != "" {
		body["range_key"] = esc(bl.SortKey)
		attrs = append(attrs, obj{"name": esc(bl.SortKey), "type": bl.SortType})
	}
	body["attribute"] = attrs
	g.add("aws_dynamodb_table", bl.ID, body)
}

func (g *gen) bucket(bl Block) {
	body := obj{"bucket_prefix": g.awsName(bl.ID, 36) + "-", "lifecycle": obj{"prevent_destroy": true}, "tags": tags(bl)}
	if g.rm.unprotect {
		body["force_destroy"] = true // removing everything includes the files in it
	}
	g.add("aws_s3_bucket", bl.ID, g.protect(body))
	b := ref("aws_s3_bucket", bl.ID, "id")
	g.add("aws_s3_bucket_public_access_block", bl.ID, obj{"bucket": b, "block_public_acls": true, "block_public_policy": true, "ignore_public_acls": true, "restrict_public_buckets": true})
	g.add("aws_s3_bucket_ownership_controls", bl.ID, obj{"bucket": b, "rule": obj{"object_ownership": "BucketOwnerEnforced"}})
	g.add("aws_s3_bucket_server_side_encryption_configuration", bl.ID, obj{"bucket": b, "rule": []obj{{"apply_server_side_encryption_by_default": obj{"sse_algorithm": "AES256"}}}})
	if bl.Versioning {
		g.add("aws_s3_bucket_versioning", bl.ID, obj{"bucket": b, "versioning_configuration": obj{"status": "Enabled"}})
	}
}

func (g *gen) queue(bl Block) {
	body := obj{"name": g.awsName(bl.ID, 80), "sqs_managed_sse_enabled": true, "tags": tags(bl)}
	if bl.RetentionDays > 0 {
		body["message_retention_seconds"] = bl.RetentionDays * 86400
	}
	g.add("aws_sqs_queue", bl.ID, body)
}

// existing reads the resource at plan time, so a wrong name fails the preview instead of the apply.
func (g *gen) existing(bl Block) {
	switch bl.Existing.Type {
	case "table":
		g.addData("aws_dynamodb_table", bl.ID, obj{"name": bl.Existing.Name})
	case "bucket":
		g.addData("aws_s3_bucket", bl.ID, obj{"bucket": bl.Existing.Name})
	case "queue":
		g.addData("aws_sqs_queue", bl.ID, obj{"name": bl.Existing.Name})
	case "database":
		g.addData("aws_db_instance", bl.ID, obj{"db_instance_identifier": bl.Existing.Name})
	case "cache":
		g.addData("aws_elasticache_replication_group", bl.ID, obj{"replication_group_id": bl.Existing.Name})
	case "secret":
		g.addData("aws_secretsmanager_secret", bl.ID, obj{"name": bl.Existing.Name})
	}
}

// namespace is the private DNS name services call each other by: <id>.<namespace>.
func (g *gen) namespace() string { return "fl-" + g.short + ".internal" }

func (g *gen) subnetGroupNet() obj { return obj{"subnet_ids": "${data.aws_subnets.default.ids}"} }

// database is an RDS instance whose password AWS creates and keeps in Secrets Manager (manage_master_user_password):
// Forgeline never sees it. Encrypted, private, protected from deletion, with backups and a final snapshot.
func (g *gen) database(bl Block) {
	sub := g.subnetGroupNet()
	sub["name"], sub["tags"] = g.awsName(bl.ID, 255), tags(bl)
	g.add("aws_db_subnet_group", bl.ID, sub)
	sg := bl.ID + "_db"
	g.add("aws_security_group", sg, obj{"name": g.awsName(bl.ID+"-db", 255), "description": "Database " + bl.ID + ": only linked services may connect", "vpc_id": "${data.aws_vpc.default.id}", "tags": tags(bl)})
	days := bl.BackupDays
	if days == 0 {
		days = 7
	}
	db := obj{
		"identifier": g.awsName(bl.ID, 63), "engine": bl.Engine, "instance_class": DatabaseSizes[bl.Size],
		"allocated_storage": bl.StorageGB, "storage_type": "gp3", "storage_encrypted": true,
		"db_name": "app", "username": "app_admin", "manage_master_user_password": true,
		"multi_az": bl.MultiAZ, "backup_retention_period": days, "publicly_accessible": false,
		"deletion_protection": true, "skip_final_snapshot": false, "final_snapshot_identifier": g.awsName(bl.ID+"-final", 255), "copy_tags_to_snapshot": true,
		"db_subnet_group_name": ref("aws_db_subnet_group", bl.ID, "name"), "vpc_security_group_ids": []string{ref("aws_security_group", sg, "id")},
		"lifecycle": obj{"prevent_destroy": true}, "tags": tags(bl),
	}
	if g.rm.unprotect {
		db["deletion_protection"], db["skip_final_snapshot"] = false, !g.rm.finalSnapshot
		if !g.rm.finalSnapshot {
			delete(db, "final_snapshot_identifier")
		}
	}
	g.add("aws_db_instance", bl.ID, g.protect(db))
}

// protect drops prevent_destroy in step 1 of removing everything, so step 2 may delete what is left.
func (g *gen) protect(body obj) obj {
	if g.rm.unprotect {
		delete(body, "lifecycle")
	}
	return body
}

// cache is a Redis replication group, encrypted at rest and in transit, private.
func (g *gen) cache(bl Block) {
	sub := g.subnetGroupNet()
	sub["name"] = g.awsName(bl.ID, 255)
	g.add("aws_elasticache_subnet_group", bl.ID, sub)
	sg := bl.ID + "_cache"
	g.add("aws_security_group", sg, obj{"name": g.awsName(bl.ID+"-cache", 255), "description": "Cache " + bl.ID + ": only linked services may connect", "vpc_id": "${data.aws_vpc.default.id}", "tags": tags(bl)})
	nodes := bl.Nodes
	if nodes == 0 {
		nodes = 1
	}
	g.add("aws_elasticache_replication_group", bl.ID, obj{
		"replication_group_id": strings.ToLower(g.awsName(bl.ID, 40)), "description": "Forgeline: " + esc(bl.Name), "engine": "redis",
		"node_type": CacheSizes[bl.Size], "num_cache_clusters": nodes, "automatic_failover_enabled": nodes > 1, "port": 6379,
		"at_rest_encryption_enabled": true, "transit_encryption_enabled": true,
		"subnet_group_name": ref("aws_elasticache_subnet_group", bl.ID, "name"), "security_group_ids": []string{ref("aws_security_group", sg, "id")}, "tags": tags(bl),
	})
}

func (g *gen) frontDoor(bl Block) {
	sg := bl.ID + "_lb"
	g.add("aws_security_group", sg, obj{"name": g.awsName(bl.ID+"-lb", 255), "description": "Front door " + bl.ID + ": open to the internet", "vpc_id": "${data.aws_vpc.default.id}", "tags": tags(bl)})
	ports := []int{80}
	if bl.Certificate != "" {
		ports = append(ports, 443)
	}
	for _, p := range ports {
		g.add("aws_vpc_security_group_ingress_rule", fmt.Sprintf("%s_%d", sg, p), obj{"security_group_id": ref("aws_security_group", sg, "id"), "cidr_ipv4": "0.0.0.0/0", "ip_protocol": "tcp", "from_port": p, "to_port": p})
	}
	g.add("aws_vpc_security_group_egress_rule", sg, obj{"security_group_id": ref("aws_security_group", sg, "id"), "cidr_ipv4": "0.0.0.0/0", "ip_protocol": "-1"})
	g.add("aws_lb", bl.ID, obj{"name": g.awsName(bl.ID, 32), "load_balancer_type": "application", "internal": false,
		"security_groups": []string{ref("aws_security_group", sg, "id")}, "subnets": "${data.aws_subnets.default.ids}", "tags": tags(bl)})
	svc := g.servedBy(bl.ID)
	health := bl.HealthPath
	if health == "" {
		health = "/"
	}
	g.add("aws_lb_target_group", bl.ID, obj{"name": g.awsName(bl.ID, 32), "port": svc.Port, "protocol": "HTTP", "target_type": "ip",
		"vpc_id": "${data.aws_vpc.default.id}", "health_check": obj{"path": health, "matcher": "200-399"}, "tags": tags(bl)})
	forward := []obj{{"type": "forward", "target_group_arn": ref("aws_lb_target_group", bl.ID, "arn")}}
	// Paths: each to its own target group, by a listener rule on the listener that forwards.
	listener := bl.ID + "_http"
	if bl.Certificate != "" {
		listener = bl.ID + "_https"
	}
	prio := 100
	for _, l := range g.b.Links {
		if l.From != bl.ID || l.Path == "" {
			continue
		}
		to := g.byID[l.To]
		tg := bl.ID + "_" + to.ID
		g.add("aws_lb_target_group", tg, obj{"name": g.awsName(bl.ID+"-"+to.ID, 32), "port": to.Port, "protocol": "HTTP", "target_type": "ip",
			"vpc_id": "${data.aws_vpc.default.id}", "health_check": obj{"path": health, "matcher": "200-399"}, "tags": tags(to)})
		g.add("aws_lb_listener_rule", tg, obj{"listener_arn": ref("aws_lb_listener", listener, "arn"), "priority": prio,
			"action":    []obj{{"type": "forward", "target_group_arn": ref("aws_lb_target_group", tg, "arn")}},
			"condition": []obj{{"path_pattern": obj{"values": []string{l.Path}}}}})
		prio++
	}
	scheme := "http"
	if bl.Certificate != "" {
		scheme = "https"
		g.add("aws_lb_listener", bl.ID+"_https", obj{"load_balancer_arn": ref("aws_lb", bl.ID, "arn"), "port": 443, "protocol": "HTTPS",
			"ssl_policy": "ELBSecurityPolicy-TLS13-1-2-2021-06", "certificate_arn": bl.Certificate, "default_action": forward})
		g.add("aws_lb_listener", bl.ID+"_http", obj{"load_balancer_arn": ref("aws_lb", bl.ID, "arn"), "port": 80, "protocol": "HTTP",
			"default_action": []obj{{"type": "redirect", "redirect": obj{"port": "443", "protocol": "HTTPS", "status_code": "HTTP_301"}}}})
	} else {
		g.add("aws_lb_listener", bl.ID+"_http", obj{"load_balancer_arn": ref("aws_lb", bl.ID, "arn"), "port": 80, "protocol": "HTTP", "default_action": forward})
	}
	g.out[bl.ID+"_url"] = obj{"description": "Where " + esc(bl.Name) + " answers", "value": scheme + "://" + ref("aws_lb", bl.ID, "dns_name")}
}

// servedBy is the service a front door sends traffic to (Validate makes sure there is exactly one).
func (g *gen) servedBy(door string) Block {
	for _, l := range g.b.Links {
		if l.From == door && l.Path == "" {
			return g.byID[l.To]
		}
	}
	return Block{}
}

// Actions a service gets on what it uses.
var grants = map[string]map[string][]string{
	"table": {
		AccessRead:      {"dynamodb:BatchGetItem", "dynamodb:DescribeTable", "dynamodb:GetItem", "dynamodb:Query", "dynamodb:Scan"},
		AccessReadWrite: {"dynamodb:BatchGetItem", "dynamodb:BatchWriteItem", "dynamodb:DeleteItem", "dynamodb:DescribeTable", "dynamodb:GetItem", "dynamodb:PutItem", "dynamodb:Query", "dynamodb:Scan", "dynamodb:UpdateItem"},
	},
	"bucket": {
		AccessRead:      {"s3:GetObject", "s3:ListBucket"},
		AccessReadWrite: {"s3:DeleteObject", "s3:GetObject", "s3:ListBucket", "s3:PutObject"},
	},
	"queue": {
		AccessRead:      {"sqs:ChangeMessageVisibility", "sqs:DeleteMessage", "sqs:GetQueueAttributes", "sqs:ReceiveMessage"},
		AccessReadWrite: {"sqs:ChangeMessageVisibility", "sqs:DeleteMessage", "sqs:GetQueueAttributes", "sqs:ReceiveMessage", "sqs:SendMessage"},
	},
}

// dataRef is how the code names a table, bucket or queue a service uses: what kind it is, its ARN, and the
// value passed to the containers (a table or bucket name, a queue URL).
func (g *gen) dataRef(bl Block) (kind, arn, value string) {
	if bl.Kind == KindExisting {
		switch bl.Existing.Type {
		case "table":
			return "table", "${data.aws_dynamodb_table." + bl.ID + ".arn}", "${data.aws_dynamodb_table." + bl.ID + ".name}"
		case "bucket":
			return "bucket", "${data.aws_s3_bucket." + bl.ID + ".arn}", "${data.aws_s3_bucket." + bl.ID + ".id}"
		default:
			return "queue", "${data.aws_sqs_queue." + bl.ID + ".arn}", "${data.aws_sqs_queue." + bl.ID + ".url}"
		}
	}
	switch bl.Kind {
	case KindTable:
		return "table", ref("aws_dynamodb_table", bl.ID, "arn"), ref("aws_dynamodb_table", bl.ID, "name")
	case KindBucket:
		return "bucket", ref("aws_s3_bucket", bl.ID, "arn"), ref("aws_s3_bucket", bl.ID, "id")
	}
	return "queue", ref("aws_sqs_queue", bl.ID, "arn"), ref("aws_sqs_queue", bl.ID, "url")
}

// envKey turns a block id into an environment name: payments-db is PAYMENTS_DB.
func envKey(id string) string { return strings.ToUpper(strings.ReplaceAll(id, "-", "_")) }

func envName(kind, id string) string {
	prefix := map[string]string{"table": "TABLE_", "bucket": "BUCKET_", "queue": "QUEUE_URL_"}[kind]
	return prefix + strings.ToUpper(strings.ReplaceAll(id, "-", "_"))
}

func (g *gen) service(bl Block) {
	exec, task := bl.ID+"_exec", bl.ID+"_task"
	g.add("aws_iam_role", exec, obj{"name": g.awsName(bl.ID+"-exec", 64), "assume_role_policy": "${data.aws_iam_policy_document.ecs_tasks_assume.json}", "tags": tags(bl)})
	g.add("aws_iam_role_policy_attachment", exec, obj{"role": ref("aws_iam_role", exec, "name"), "policy_arn": "arn:aws:iam::aws:policy/service-role/AmazonECSTaskExecutionRolePolicy"})
	g.add("aws_iam_role", task, obj{"name": g.awsName(bl.ID+"-task", 64), "assume_role_policy": "${data.aws_iam_policy_document.ecs_tasks_assume.json}", "tags": tags(bl)})

	env := map[string]string{}
	for k, v := range bl.Env {
		env[k] = esc(v)
	}
	logGroup, door, doorPath := "", "", ""
	var statements []obj
	var secrets []obj       // injected by ECS at start from Secrets Manager; Forgeline never sees the values
	var secretARNs []string // what the execution role may read
	var callers []string    // services that call this one
	sg := bl.ID + "_svc"
	for _, l := range g.b.Links {
		if l.To == bl.ID {
			switch from := g.byID[l.From]; from.Kind {
			case KindFrontDoor:
				door, doorPath = from.ID, l.Path
			case KindService:
				callers = append(callers, from.ID)
			}
			continue
		}
		if l.From != bl.ID {
			continue
		}
		to := g.byID[l.To]
		key := envKey(to.ID)
		switch {
		case to.Kind == KindLogs:
			logGroup = ref("aws_cloudwatch_log_group", to.ID, "name")
			continue
		case to.Kind == KindService:
			env[key+"_URL"] = fmt.Sprintf("http://%s.%s:%d", to.ID, g.namespace(), to.Port)
			continue
		case to.Kind == KindDatabase:
			port := DatabasePorts[to.Engine]
			g.add("aws_vpc_security_group_ingress_rule", to.ID+"_db_from_"+bl.ID, obj{"security_group_id": ref("aws_security_group", to.ID+"_db", "id"),
				"referenced_security_group_id": ref("aws_security_group", sg, "id"), "ip_protocol": "tcp", "from_port": port, "to_port": port})
			env[key+"_HOST"], env[key+"_PORT"], env[key+"_NAME"] = ref("aws_db_instance", to.ID, "address"), fmt.Sprint(port), "app"
			arn := "${aws_db_instance." + to.ID + ".master_user_secret[0].secret_arn}"
			secrets = append(secrets, obj{"name": key + "_USERNAME", "valueFrom": arn + ":username::"}, obj{"name": key + "_PASSWORD", "valueFrom": arn + ":password::"})
			secretARNs = append(secretARNs, arn)
			continue
		case to.Kind == KindCache:
			g.add("aws_vpc_security_group_ingress_rule", to.ID+"_cache_from_"+bl.ID, obj{"security_group_id": ref("aws_security_group", to.ID+"_cache", "id"),
				"referenced_security_group_id": ref("aws_security_group", sg, "id"), "ip_protocol": "tcp", "from_port": 6379, "to_port": 6379})
			env[key+"_HOST"], env[key+"_PORT"], env[key+"_TLS"] = ref("aws_elasticache_replication_group", to.ID, "primary_endpoint_address"), "6379", "true"
			continue
		case to.Kind == KindSecret:
			secrets = append(secrets, obj{"name": key, "valueFrom": ref("aws_secretsmanager_secret", to.ID, "arn")})
			secretARNs = append(secretARNs, ref("aws_secretsmanager_secret", to.ID, "arn"))
			continue
		case to.Kind == KindExisting && to.Existing.Type == "database":
			env[key+"_HOST"], env[key+"_PORT"] = "${data.aws_db_instance."+to.ID+".address}", "${data.aws_db_instance."+to.ID+".port}"
			continue
		case to.Kind == KindExisting && to.Existing.Type == "cache":
			env[key+"_HOST"], env[key+"_PORT"] = "${data.aws_elasticache_replication_group."+to.ID+".primary_endpoint_address}", "6379"
			continue
		case to.Kind == KindExisting && to.Existing.Type == "secret":
			secrets = append(secrets, obj{"name": key, "valueFrom": "${data.aws_secretsmanager_secret." + to.ID + ".arn}"})
			secretARNs = append(secretARNs, "${data.aws_secretsmanager_secret."+to.ID+".arn}")
			continue
		}
		kind, arn, value := g.dataRef(to)
		res := []string{arn}
		switch kind {
		case "table":
			res = append(res, arn+"/index/*")
		case "bucket":
			res = append(res, arn+"/*")
		}
		statements = append(statements, obj{"Effect": "Allow", "Action": grants[kind][l.Access], "Resource": res})
		env[envName(kind, to.ID)] = value
	}
	if len(statements) > 0 {
		policy, _ := json.Marshal(obj{"Version": "2012-10-17", "Statement": statements})
		g.add("aws_iam_role_policy", task, obj{"name": "uses", "role": ref("aws_iam_role", task, "id"), "policy": string(policy)})
	}
	if len(secretARNs) > 0 {
		// Only the execution role (ECS, at start) may read the secrets this service links to, and nothing else.
		policy, _ := json.Marshal(obj{"Version": "2012-10-17", "Statement": []obj{{"Effect": "Allow", "Action": []string{"secretsmanager:GetSecretValue"}, "Resource": secretARNs}}})
		g.add("aws_iam_role_policy", exec+"_secrets", obj{"name": "secrets", "role": ref("aws_iam_role", exec, "id"), "policy": string(policy)})
	}
	if logGroup == "" {
		g.add("aws_cloudwatch_log_group", bl.ID, obj{"name": "/forgeline/" + esc(g.b.ID) + "/" + bl.ID, "retention_in_days": 30, "tags": tags(bl)})
		logGroup = ref("aws_cloudwatch_log_group", bl.ID, "name")
	}

	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var envList []obj
	for _, k := range keys {
		envList = append(envList, obj{"name": k, "value": env[k]})
	}
	container := obj{
		"name": "app", "image": esc(bl.Image), "essential": true, "environment": envList,
		"logConfiguration": obj{"logDriver": "awslogs", "options": obj{"awslogs-group": logGroup, "awslogs-region": g.b.Target.Region, "awslogs-stream-prefix": bl.ID}},
	}
	if bl.Port > 0 {
		container["portMappings"] = []obj{{"containerPort": bl.Port, "protocol": "tcp"}}
	}
	if len(secrets) > 0 {
		container["secrets"] = secrets
	}
	var cbuf bytes.Buffer
	enc := json.NewEncoder(&cbuf)
	enc.SetEscapeHTML(false)
	enc.Encode([]obj{container})
	g.add("aws_ecs_task_definition", bl.ID, obj{
		"family": g.awsName(bl.ID, 255), "requires_compatibilities": []string{"FARGATE"}, "network_mode": "awsvpc",
		"cpu": fmt.Sprint(bl.CPU), "memory": fmt.Sprint(bl.Memory),
		"execution_role_arn": ref("aws_iam_role", exec, "arn"), "task_role_arn": ref("aws_iam_role", task, "arn"),
		"container_definitions": strings.TrimSpace(cbuf.String()), "tags": tags(bl),
	})

	g.add("aws_security_group", sg, obj{"name": g.awsName(bl.ID+"-svc", 255), "description": "Service " + bl.ID + ": only its front door and the services that call it may connect", "vpc_id": "${data.aws_vpc.default.id}", "tags": tags(bl)})
	g.add("aws_vpc_security_group_egress_rule", sg, obj{"security_group_id": ref("aws_security_group", sg, "id"), "cidr_ipv4": "0.0.0.0/0", "ip_protocol": "-1"})
	svc := obj{
		"name": g.awsName(bl.ID, 255), "cluster": "${aws_ecs_cluster.main.id}", "task_definition": ref("aws_ecs_task_definition", bl.ID, "arn"),
		"desired_count": bl.Count, "launch_type": "FARGATE",
		// A public address only so the task can pull its image without a NAT gateway; inbound traffic is
		// still limited by the security group to the front door.
		"network_configuration": obj{"subnets": "${data.aws_subnets.default.ids}", "security_groups": []string{ref("aws_security_group", sg, "id")}, "assign_public_ip": true},
		"tags":                  tags(bl),
	}
	if door != "" {
		g.add("aws_vpc_security_group_ingress_rule", sg+"_from_"+door, obj{"security_group_id": ref("aws_security_group", sg, "id"),
			"referenced_security_group_id": ref("aws_security_group", door+"_lb", "id"), "ip_protocol": "tcp", "from_port": bl.Port, "to_port": bl.Port})
		tg := door
		if doorPath != "" {
			tg = door + "_" + bl.ID
		}
		svc["load_balancer"] = []obj{{"target_group_arn": ref("aws_lb_target_group", tg, "arn"), "container_name": "app", "container_port": bl.Port}}
		listener := door + "_http"
		if g.byID[door].Certificate != "" {
			listener = door + "_https"
		}
		deps := []string{"aws_lb_listener." + listener}
		if doorPath != "" {
			deps = append(deps, "aws_lb_listener_rule."+tg)
		}
		svc["depends_on"] = deps
	}
	if len(callers) > 0 {
		// Other services find this one by name in the blueprint's private namespace, and only they may connect.
		g.add("aws_service_discovery_service", bl.ID, obj{"name": bl.ID, "dns_config": obj{"namespace_id": "${aws_service_discovery_private_dns_namespace.main.id}",
			"dns_records": []obj{{"ttl": 10, "type": "A"}}, "routing_policy": "MULTIVALUE"}, "tags": tags(bl)})
		svc["service_registries"] = obj{"registry_arn": ref("aws_service_discovery_service", bl.ID, "arn")}
		sort.Strings(callers)
		for _, c := range callers {
			g.add("aws_vpc_security_group_ingress_rule", sg+"_from_"+c, obj{"security_group_id": ref("aws_security_group", sg, "id"),
				"referenced_security_group_id": ref("aws_security_group", c+"_svc", "id"), "ip_protocol": "tcp", "from_port": bl.Port, "to_port": bl.Port})
		}
	}
	if bl.Max > 0 {
		// Scaling on average CPU; the service's own count is only where it starts.
		svc["lifecycle"] = obj{"ignore_changes": []string{"desired_count"}}
		target := bl.CPUTarget
		if target == 0 {
			target = 60
		}
		resource := "service/${aws_ecs_cluster.main.name}/${aws_ecs_service." + bl.ID + ".name}"
		g.add("aws_appautoscaling_target", bl.ID, obj{"min_capacity": bl.Min, "max_capacity": bl.Max, "resource_id": resource,
			"scalable_dimension": "ecs:service:DesiredCount", "service_namespace": "ecs"})
		g.add("aws_appautoscaling_policy", bl.ID, obj{"name": g.awsName(bl.ID+"-cpu", 255), "policy_type": "TargetTrackingScaling",
			"resource_id": ref("aws_appautoscaling_target", bl.ID, "resource_id"), "scalable_dimension": ref("aws_appautoscaling_target", bl.ID, "scalable_dimension"),
			"service_namespace": ref("aws_appautoscaling_target", bl.ID, "service_namespace"),
			"target_tracking_scaling_policy_configuration": obj{"target_value": target, "scale_in_cooldown": 120, "scale_out_cooldown": 60,
				"predefined_metric_specification": obj{"predefined_metric_type": "ECSServiceAverageCPUUtilization"}}})
	}
	g.add("aws_ecs_service", bl.ID, svc)
}

// agentName is the AgentCore runtime's name: letters, digits and underscores only, at most 48.
func (g *gen) agentName(id string) string {
	return "fl_" + g.short + "_" + strings.ReplaceAll(id, "-", "_")
}

// agent is an AgentCore runtime from an ECR image, with an execution role that has exactly what AWS documents a
// runtime needs (pull its own image, write its logs and traces, call Bedrock models, get its own workload token)
// and what the block is linked to. It is reachable only through AWS's API with IAM, never a public port.
// https://docs.aws.amazon.com/bedrock-agentcore/latest/devguide/runtime-permissions.html
func (g *gen) agent(bl Block) {
	m := ecrImageRE.FindStringSubmatch(bl.Image)
	region, acct, name := g.b.Target.Region, "${data.aws_caller_identity.current.account_id}", g.agentName(bl.ID)
	repo := "arn:aws:ecr:" + m[2] + ":" + m[1] + ":repository/" + m[3]
	logs := "arn:aws:logs:" + region + ":" + acct + ":log-group:/aws/bedrock-agentcore/runtimes/"
	trust, _ := json.Marshal(obj{"Version": "2012-10-17", "Statement": []obj{{
		"Effect": "Allow", "Principal": obj{"Service": "bedrock-agentcore.amazonaws.com"}, "Action": "sts:AssumeRole",
		"Condition": obj{"StringEquals": obj{"aws:SourceAccount": acct}, "ArnLike": obj{"aws:SourceArn": "arn:aws:bedrock-agentcore:" + region + ":" + acct + ":*"}},
	}}})
	role := bl.ID + "_agent"
	g.add("aws_iam_role", role, obj{"name": g.awsName(bl.ID+"-agent", 64), "assume_role_policy": string(trust), "tags": tags(bl)})
	statements := []obj{
		{"Sid": "PullOwnImage", "Effect": "Allow", "Action": []string{"ecr:BatchGetImage", "ecr:GetDownloadUrlForLayer"}, "Resource": []string{repo}},
		{"Sid": "EcrToken", "Effect": "Allow", "Action": []string{"ecr:GetAuthorizationToken"}, "Resource": "*"},
		{"Sid": "Logs", "Effect": "Allow", "Action": []string{"logs:CreateLogGroup", "logs:DescribeLogStreams"}, "Resource": []string{logs + "*"}},
		{"Sid": "LogGroups", "Effect": "Allow", "Action": []string{"logs:DescribeLogGroups"}, "Resource": []string{"arn:aws:logs:" + region + ":" + acct + ":log-group:*"}},
		{"Sid": "LogEvents", "Effect": "Allow", "Action": []string{"logs:CreateLogStream", "logs:PutLogEvents"}, "Resource": []string{logs + "*:log-stream:*"}},
		{"Sid": "Traces", "Effect": "Allow", "Action": []string{"xray:GetSamplingRules", "xray:GetSamplingTargets", "xray:PutTelemetryRecords", "xray:PutTraceSegments"}, "Resource": "*"},
		{"Sid": "Metrics", "Effect": "Allow", "Action": []string{"cloudwatch:PutMetricData"}, "Resource": "*", "Condition": obj{"StringEquals": obj{"cloudwatch:namespace": "bedrock-agentcore"}}},
		// Not GetWorkloadAccessTokenForUserId: it issues tokens for any user id without checking an identity provider.
		{"Sid": "OwnWorkloadToken", "Effect": "Allow", "Action": []string{"bedrock-agentcore:GetWorkloadAccessToken", "bedrock-agentcore:GetWorkloadAccessTokenForJWT"},
			"Resource": []string{"arn:aws:bedrock-agentcore:" + region + ":" + acct + ":workload-identity-directory/default",
				"arn:aws:bedrock-agentcore:" + region + ":" + acct + ":workload-identity-directory/default/workload-identity/" + name + "-*"}},
		{"Sid": "Models", "Effect": "Allow", "Action": []string{"bedrock:InvokeModel", "bedrock:InvokeModelWithResponseStream"},
			"Resource": []string{"arn:aws:bedrock:*::foundation-model/*", "arn:aws:bedrock:" + region + ":" + acct + ":*"}},
	}
	env := map[string]string{}
	for k, v := range bl.Env {
		env[k] = esc(v)
	}
	var secretARNs []string
	for _, l := range g.b.Links {
		if l.From != bl.ID {
			continue
		}
		to := g.byID[l.To]
		key := envKey(to.ID)
		switch {
		case to.Kind == KindSecret:
			// AgentCore has no secret injection: the agent reads the value itself, by this ARN, at run time.
			secretARNs = append(secretARNs, ref("aws_secretsmanager_secret", to.ID, "arn"))
			env[key+"_SECRET_ARN"] = ref("aws_secretsmanager_secret", to.ID, "arn")
			continue
		case to.Kind == KindExisting && to.Existing.Type == "secret":
			secretARNs = append(secretARNs, "${data.aws_secretsmanager_secret."+to.ID+".arn}")
			env[key+"_SECRET_ARN"] = "${data.aws_secretsmanager_secret." + to.ID + ".arn}"
			continue
		}
		kind, arn, value := g.dataRef(to)
		res := []string{arn}
		switch kind {
		case "table":
			res = append(res, arn+"/index/*")
		case "bucket":
			res = append(res, arn+"/*")
		}
		statements = append(statements, obj{"Effect": "Allow", "Action": grants[kind][l.Access], "Resource": res})
		env[envName(kind, to.ID)] = value
	}
	if len(secretARNs) > 0 {
		statements = append(statements, obj{"Sid": "LinkedSecrets", "Effect": "Allow", "Action": []string{"secretsmanager:GetSecretValue"}, "Resource": secretARNs})
	}
	policy, _ := json.Marshal(obj{"Version": "2012-10-17", "Statement": statements})
	g.add("aws_iam_role_policy", role, obj{"name": "agentcore-runtime", "role": ref("aws_iam_role", role, "id"), "policy": string(policy)})
	body := obj{
		"agent_runtime_name": name, "description": esc(orDefault(bl.Description, bl.Name)), "role_arn": ref("aws_iam_role", role, "arn"),
		"agent_runtime_artifact": obj{"container_configuration": obj{"container_uri": bl.Image}},
		"network_configuration":  obj{"network_mode": "PUBLIC"},
		"protocol_configuration": obj{"server_protocol": "HTTP"},
		"tags":                   tags(bl),
		// The role must hold its permissions before AgentCore checks it.
		"depends_on": []string{"aws_iam_role_policy." + role},
	}
	if len(env) > 0 {
		body["environment_variables"] = env
	}
	g.add("aws_bedrockagentcore_agent_runtime", bl.ID, body)
	g.out[bl.ID+"_agent_arn"] = obj{"description": "The AgentCore runtime of " + esc(bl.Name) + "; call it with InvokeAgentRuntime", "value": ref("aws_bedrockagentcore_agent_runtime", bl.ID, "agent_runtime_arn")}
}

func orDefault(s, d string) string {
	if strings.TrimSpace(s) == "" {
		return d
	}
	return s
}

// ImageRepoName is the private ECR repository the blueprint makes for a block with a source.
func (b Blueprint) ImageRepoName(blockID string) string {
	g := gen{b: b}
	sum := sha256.Sum256([]byte(b.ID))
	g.short = hex.EncodeToString(sum[:])[:6]
	return strings.ToLower(g.awsName(blockID, 200))
}

// PushRoleName is the role GitHub Actions in the block's repository and branch may use to push its images.
func (b Blueprint) PushRoleName(blockID string) string {
	g := gen{b: b}
	sum := sha256.Sum256([]byte(b.ID))
	g.short = hex.EncodeToString(sum[:])[:6]
	return g.awsName(blockID+"-push", 64)
}

// source makes a private image repository for a block's code and a role that only GitHub Actions in that repository,
// on that branch, can take (through GitHub's OIDC provider, which must already be set up in the account) to push
// images to it and nothing else. Forgeline never pushes to the repository; running a new image is a blueprint
// change that is previewed and approved.
func (g *gen) source(bl Block) {
	repo := bl.ID + "_src"
	g.add("aws_ecr_repository", repo, obj{
		"name": g.b.ImageRepoName(bl.ID), "image_tag_mutability": "IMMUTABLE",
		"image_scanning_configuration": obj{"scan_on_push": true}, "encryption_configuration": obj{"encryption_type": "AES256"},
		// Images are built from the repository again at any time, so removing the blueprint may remove them.
		"force_delete": true, "tags": tags(bl),
	})
	rules, _ := json.Marshal(obj{"rules": []obj{{"rulePriority": 1, "description": "Keep the 30 newest images",
		"selection": obj{"tagStatus": "any", "countType": "imageCountMoreThan", "countNumber": 30}, "action": obj{"type": "expire"}}}})
	g.add("aws_ecr_lifecycle_policy", repo, obj{"repository": ref("aws_ecr_repository", repo, "name"), "policy": string(rules)})
	// GitHub's OIDC provider is named by its address: building its ARN needs no IAM read permission (looking it up with a
	// data source needs iam:ListOpenIDConnectProviders, which a builder role usually lacks). If it is missing, IAM
	// refuses the role at apply and says so.
	g.addData("aws_caller_identity", "current", obj{})
	trust, _ := json.Marshal(obj{"Version": "2012-10-17", "Statement": []obj{{
		"Effect": "Allow", "Principal": obj{"Federated": "arn:aws:iam::${data.aws_caller_identity.current.account_id}:oidc-provider/token.actions.githubusercontent.com"}, "Action": "sts:AssumeRoleWithWebIdentity",
		"Condition": obj{
			"StringEquals": obj{"token.actions.githubusercontent.com:aud": "sts.amazonaws.com"},
			"StringLike":   obj{"token.actions.githubusercontent.com:sub": githubSubjects(bl.Source.Repo, bl.Source.BranchOr())},
		},
	}}})
	role := bl.ID + "_push"
	g.add("aws_iam_role", role, obj{"name": g.b.PushRoleName(bl.ID), "assume_role_policy": string(trust), "max_session_duration": 3600, "tags": tags(bl)})
	policy, _ := json.Marshal(obj{"Version": "2012-10-17", "Statement": []obj{
		{"Sid": "EcrToken", "Effect": "Allow", "Action": []string{"ecr:GetAuthorizationToken"}, "Resource": "*"},
		{"Sid": "PushOwnImages", "Effect": "Allow", "Action": []string{"ecr:BatchCheckLayerAvailability", "ecr:BatchGetImage", "ecr:CompleteLayerUpload",
			"ecr:GetDownloadUrlForLayer", "ecr:InitiateLayerUpload", "ecr:PutImage", "ecr:UploadLayerPart"}, "Resource": []string{ref("aws_ecr_repository", repo, "arn")}},
	}})
	g.add("aws_iam_role_policy", role, obj{"name": "push-images", "role": ref("aws_iam_role", role, "id"), "policy": string(policy)})
	g.out[bl.ID+"_image_repository"] = obj{"description": "Where images of " + esc(bl.Name) + " are pushed", "value": ref("aws_ecr_repository", repo, "repository_url")}
	g.out[bl.ID+"_push_role"] = obj{"description": "The role GitHub Actions in " + bl.Source.Repo + " uses to push them", "value": ref("aws_iam_role", role, "arn")}
}

// githubSubjects are the token subjects GitHub Actions sends for a repository's branch. GitHub now puts the owner's
// and repository's numeric ids in it (repo:owner@123/name@456:ref:refs/heads/main; found on real AWS, where the older
// form alone was refused); older repositories still send repo:owner/name:ref:refs/heads/main. Only the ids are
// wildcards: the owner, name and branch must match exactly, and a branch name cannot contain ":", so no other
// repository or branch matches.
func githubSubjects(repo, branch string) []string {
	owner, name, _ := strings.Cut(repo, "/")
	return []string{
		"repo:" + repo + ":ref:refs/heads/" + branch,
		"repo:" + owner + "@*/" + name + "@*:ref:refs/heads/" + branch,
	}
}

// ModulePath is where a template's module sits next to main.tf.json.
func ModulePath(t template.Template) string { return "templates/" + t.Name + "/" + t.Version }

// Modules returns the template modules the blueprint's code calls, by path, to be written next to main.tf.json.
func (b Blueprint) Modules() (map[string]string, error) {
	out := map[string]string{}
	for _, bl := range b.Blocks {
		if bl.Kind != KindTemplate {
			continue
		}
		t, err := template.Get(bl.Template)
		if err != nil {
			return nil, err
		}
		out[ModulePath(t)+"/main.tf.json"] = string(t.Code)
	}
	return out, nil
}

// escValue makes a checked input safe to pass to OpenTofu: text cannot become an expression.
func escValue(v any) any {
	switch x := v.(type) {
	case string:
		return esc(x)
	case map[string]any:
		m := obj{}
		for k, e := range x {
			m[esc(k)] = escValue(e)
		}
		return m
	}
	return v
}

// template calls a template block's module with its checked inputs and, for each link role, the linked blocks in
// the role's shape. What the module may do with them is the reviewed template's code.
func (g *gen) template(bl Block) {
	t, err := template.Get(bl.Template)
	if err != nil {
		return // Validate refused it already
	}
	in, err := t.Inputs.Resolve(bl.Inputs)
	if err != nil {
		return
	}
	call := obj{"source": "./" + ModulePath(t), "name": g.awsName(bl.ID, 64), "tags": tags(bl)}
	for k, v := range in {
		call[k] = escValue(v)
	}
	lists := map[string][]obj{}
	for _, r := range t.Diagram.Links {
		lists[r.Variable] = []obj{}
	}
	for _, l := range g.b.Links {
		switch {
		case l.From == bl.ID:
			to := g.byID[l.To]
			r, ok := t.Role("out", kindOf(to))
			if !ok {
				continue
			}
			switch r.Shape {
			case template.ShapeData:
				kind, arn, value := g.dataRef(to)
				lists[r.Variable] = append(lists[r.Variable], obj{"id": to.ID, "kind": kind, "arn": arn, "value": value, "env": envName(kind, to.ID), "access": l.Access})
			case template.ShapeSecret:
				arn := ref("aws_secretsmanager_secret", to.ID, "arn")
				if to.Kind == KindExisting {
					arn = "${data.aws_secretsmanager_secret." + to.ID + ".arn}"
				}
				lists[r.Variable] = append(lists[r.Variable], obj{"id": to.ID, "arn": arn, "env": envKey(to.ID) + "_SECRET_ARN"})
			}
		case l.To == bl.ID:
			from := g.byID[l.From]
			r, ok := t.Role("in", kindOf(from))
			if !ok {
				continue
			}
			_, arn, _ := g.dataRef(from)
			lists[r.Variable] = append(lists[r.Variable], obj{"id": from.ID, "arn": arn})
		}
	}
	for k, v := range lists {
		call[k] = v
	}
	g.mod[bl.ID] = call
	var code struct {
		Output map[string]struct {
			Description string `json:"description"`
		} `json:"output"`
	}
	json.Unmarshal(t.Code, &code)
	for name, o := range code.Output {
		g.out[bl.ID+"_"+name] = obj{"description": esc(bl.Name) + ": " + o.Description, "value": "${module." + bl.ID + "." + name + "}"}
	}
}

// ModulesFor returns the template modules a generated main.tf.json calls, by path, from the module sources in it. It
// refuses any source that is not a library template, so only reviewed code is ever written next to the plan.
func ModulesFor(code []byte) (map[string]string, error) {
	var doc struct {
		Module map[string]struct {
			Source string `json:"source"`
		} `json:"module"`
	}
	if err := json.Unmarshal(code, &doc); err != nil {
		return nil, err
	}
	out := map[string]string{}
	for name, m := range doc.Module {
		rest, ok := strings.CutPrefix(m.Source, "./templates/")
		parts := strings.Split(rest, "/")
		if !ok || len(parts) != 2 {
			return nil, fmt.Errorf("module %s: %q is not a library template", name, m.Source)
		}
		t, err := template.Get(parts[0] + "@" + parts[1])
		if err != nil {
			return nil, fmt.Errorf("module %s: %w", name, err)
		}
		out[ModulePath(t)+"/main.tf.json"] = string(t.Code)
	}
	return out, nil
}
