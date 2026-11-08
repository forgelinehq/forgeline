// Package blueprint is the typed model behind the design canvas (design note 035): what should exist in a
// cloud account, as blocks and the links between them. It validates a blueprint, says in plain words what it
// means (Checks), and generates OpenTofu code for it (Tofu). It makes no network calls.
package blueprint

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// Block kinds.
const (
	KindFrontDoor = "frontdoor" // a public application load balancer
	KindService   = "service"   // containers on ECS Fargate
	KindTable     = "table"     // a DynamoDB table
	KindBucket    = "bucket"    // an S3 bucket
	KindQueue     = "queue"     // an SQS queue
	KindLogs      = "logs"      // a CloudWatch log group
	KindExisting  = "existing"  // something that already exists: drawn, linked to, never created or changed
)

// Kinds lists the block kinds in the order the canvas offers them.
var Kinds = []string{KindFrontDoor, KindService, KindTable, KindBucket, KindQueue, KindLogs, KindExisting}

// Access a service has to what it uses.
const (
	AccessRead      = "read"
	AccessReadWrite = "readwrite"
)

const (
	MaxBlocks = 30
	MaxLinks  = 60
	MaxEnv    = 20
)

// Blueprint is one design: what should exist in one account and region.
type Blueprint struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Description string  `json:"description,omitempty"`
	Target      Target  `json:"target"`
	Blocks      []Block `json:"blocks"`
	Links       []Link  `json:"links"`
}

// Target is where a blueprint is built.
type Target struct {
	Cloud   string `json:"cloud"`   // "aws"
	Account string `json:"account"` // the console's account id ("primary" or an added account)
	Region  string `json:"region"`
}

// Block is one thing in the design. Only the fields of its kind may be set.
type Block struct {
	ID   string `json:"id"`
	Kind string `json:"kind"`
	Name string `json:"name"`
	// Where the canvas draws it. Not used by the code.
	X int `json:"x,omitempty"`
	Y int `json:"y,omitempty"`

	// service
	Image  string            `json:"image,omitempty"`
	CPU    int               `json:"cpu,omitempty"`    // Fargate CPU units: 256, 512, 1024, 2048, 4096
	Memory int               `json:"memory,omitempty"` // MiB, a size Fargate allows for the CPU
	Count  int               `json:"count,omitempty"`  // tasks to run, 0 to 10
	Port   int               `json:"port,omitempty"`   // the port the container listens on
	Env    map[string]string `json:"env,omitempty"`    // plain settings, not secrets

	// frontdoor
	HealthPath  string `json:"health_path,omitempty"`
	Certificate string `json:"certificate,omitempty"` // an ACM certificate ARN; empty means plain HTTP

	// table
	PartitionKey  string `json:"partition_key,omitempty"`
	PartitionType string `json:"partition_type,omitempty"` // S or N
	SortKey       string `json:"sort_key,omitempty"`
	SortType      string `json:"sort_type,omitempty"`

	// bucket
	Versioning bool `json:"versioning,omitempty"`

	// queue and logs
	RetentionDays int `json:"retention_days,omitempty"`

	// existing
	Existing *Existing `json:"existing,omitempty"`
}

// Existing names something already in the account.
type Existing struct {
	Type string `json:"type"` // table, bucket or queue
	Name string `json:"name"`
}

// Link connects two blocks. What it means follows from their kinds: a front door sends traffic to a service; a
// service uses a table, bucket, queue or existing resource (Access), or writes its logs to a logs block.
type Link struct {
	From   string `json:"from"`
	To     string `json:"to"`
	Access string `json:"access,omitempty"` // read or readwrite, for a service using data
}

var (
	idRE      = regexp.MustCompile(`^[a-z][a-z0-9-]{0,29}$`)
	keyRE     = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,255}$`)
	envRE     = regexp.MustCompile(`^[A-Z_][A-Z0-9_]{0,63}$`)
	imageRE   = regexp.MustCompile(`^[a-z0-9][A-Za-z0-9._/:@-]{0,254}$`)
	certRE    = regexp.MustCompile(`^arn:aws[a-z-]*:acm:[a-z0-9-]+:\d{12}:certificate/[A-Za-z0-9-]+$`)
	regionRE  = regexp.MustCompile(`^[a-z]{2}(-[a-z]+)+-\d$`)
	accountRE = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,39}$`)
	tableRE   = regexp.MustCompile(`^[A-Za-z0-9_.-]{3,255}$`)
	bucketRE  = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]{1,61}[a-z0-9]$`)
	queueRE   = regexp.MustCompile(`^[A-Za-z0-9_-]{1,80}(\.fifo)?$`)
)

// Fargate's allowed memory sizes (MiB) for each CPU size.
var fargateMemory = map[int][]int{
	256:  {512, 1024, 2048},
	512:  {1024, 2048, 3072, 4096},
	1024: {2048, 3072, 4096, 5120, 6144, 7168, 8192},
	2048: rangeMiB(4096, 16384),
	4096: rangeMiB(8192, 30720),
}

func rangeMiB(lo, hi int) []int {
	var out []int
	for m := lo; m <= hi; m += 1024 {
		out = append(out, m)
	}
	return out
}

// Retention periods CloudWatch Logs accepts (days).
var logRetention = []int{1, 3, 5, 7, 14, 30, 60, 90, 120, 150, 180, 365, 400, 545, 731, 1096, 1827, 2192, 2557, 2922, 3288, 3653}

// Validate reports the first problem it finds, saying where (blocks[2].port: ...). A valid blueprint can be
// turned into code.
func (b Blueprint) Validate() error {
	if strings.TrimSpace(b.Name) == "" || len(b.Name) > 80 {
		return fmt.Errorf("name: give a name of at most 80 characters")
	}
	if len(b.Description) > 2000 {
		return fmt.Errorf("description: at most 2000 characters")
	}
	if b.Target.Cloud != "aws" {
		return fmt.Errorf("target.cloud: only aws is supported, got %q", b.Target.Cloud)
	}
	if !accountRE.MatchString(b.Target.Account) {
		return fmt.Errorf("target.account: %q is not an account id", b.Target.Account)
	}
	if !regionRE.MatchString(b.Target.Region) {
		return fmt.Errorf("target.region: %q is not an AWS region", b.Target.Region)
	}
	if len(b.Blocks) > MaxBlocks {
		return fmt.Errorf("blocks: at most %d, got %d", MaxBlocks, len(b.Blocks))
	}
	if len(b.Links) > MaxLinks {
		return fmt.Errorf("links: at most %d, got %d", MaxLinks, len(b.Links))
	}
	byID := map[string]Block{}
	for i, bl := range b.Blocks {
		at := fmt.Sprintf("blocks[%d]", i)
		if !idRE.MatchString(bl.ID) {
			return fmt.Errorf("%s.id: %q must be lower-case letters, digits and dashes, starting with a letter, at most 30", at, bl.ID)
		}
		if _, dup := byID[bl.ID]; dup {
			return fmt.Errorf("%s.id: %q is used twice", at, bl.ID)
		}
		if err := bl.validate(at); err != nil {
			return err
		}
		byID[bl.ID] = bl
	}
	fronted := map[string]string{} // service -> front door
	doors := map[string]int{}
	logsOf := map[string]bool{}
	seen := map[[2]string]bool{}
	for i, l := range b.Links {
		at := fmt.Sprintf("links[%d]", i)
		from, ok := byID[l.From]
		if !ok {
			return fmt.Errorf("%s.from: no block %q", at, l.From)
		}
		to, ok := byID[l.To]
		if !ok {
			return fmt.Errorf("%s.to: no block %q", at, l.To)
		}
		if seen[[2]string{l.From, l.To}] {
			return fmt.Errorf("%s: %s and %s are already linked", at, l.From, l.To)
		}
		seen[[2]string{l.From, l.To}] = true
		switch {
		case from.Kind == KindFrontDoor && to.Kind == KindService:
			if l.Access != "" {
				return fmt.Errorf("%s.access: a front door sends traffic; access is only for data", at)
			}
			if to.Port == 0 {
				return fmt.Errorf("%s: %s needs a port to take traffic from a front door", at, to.ID)
			}
			if d, ok := fronted[to.ID]; ok {
				return fmt.Errorf("%s: %s already takes traffic from %s", at, to.ID, d)
			}
			fronted[to.ID] = from.ID
			doors[from.ID]++
		case from.Kind == KindService && to.Kind == KindLogs:
			if l.Access != "" {
				return fmt.Errorf("%s.access: logs take no access level", at)
			}
			if logsOf[from.ID] {
				return fmt.Errorf("%s: %s already writes its logs to another logs block", at, from.ID)
			}
			logsOf[from.ID] = true
		case from.Kind == KindService && (to.Kind == KindTable || to.Kind == KindBucket || to.Kind == KindQueue || to.Kind == KindExisting):
			if l.Access != AccessRead && l.Access != AccessReadWrite {
				return fmt.Errorf("%s.access: say read or readwrite, got %q", at, l.Access)
			}
		default:
			return fmt.Errorf("%s: a %s cannot link to a %s (a front door sends traffic to a service; a service uses data or writes logs)", at, from.Kind, to.Kind)
		}
	}
	for i, bl := range b.Blocks {
		if bl.Kind == KindFrontDoor && doors[bl.ID] != 1 {
			return fmt.Errorf("blocks[%d]: front door %s must send traffic to exactly one service", i, bl.ID)
		}
	}
	return nil
}

func (bl Block) validate(at string) error {
	if strings.TrimSpace(bl.Name) == "" || len(bl.Name) > 60 {
		return fmt.Errorf("%s.name: give a name of at most 60 characters", at)
	}
	set := bl.setFields()
	allowed := map[string][]string{
		KindService:   {"image", "cpu", "memory", "count", "port", "env"},
		KindFrontDoor: {"health_path", "certificate"},
		KindTable:     {"partition_key", "partition_type", "sort_key", "sort_type"},
		KindBucket:    {"versioning"},
		KindQueue:     {"retention_days"},
		KindLogs:      {"retention_days"},
		KindExisting:  {"existing"},
	}
	ok, known := allowed[bl.Kind]
	if !known {
		return fmt.Errorf("%s.kind: %q is not one of %s", at, bl.Kind, strings.Join(Kinds, ", "))
	}
	for _, f := range set {
		if !slices.Contains(ok, f) {
			return fmt.Errorf("%s.%s: a %s has no %s", at, f, bl.Kind, f)
		}
	}
	switch bl.Kind {
	case KindService:
		if !imageRE.MatchString(bl.Image) {
			return fmt.Errorf("%s.image: give a container image such as nginx:1.29 or an ECR address", at)
		}
		mems, ok := fargateMemory[bl.CPU]
		if !ok {
			return fmt.Errorf("%s.cpu: Fargate allows 256, 512, 1024, 2048 or 4096, got %d", at, bl.CPU)
		}
		if !slices.Contains(mems, bl.Memory) {
			return fmt.Errorf("%s.memory: %d MiB is not allowed with %d CPU; allowed: %v", at, bl.Memory, bl.CPU, mems)
		}
		if bl.Count < 0 || bl.Count > 10 {
			return fmt.Errorf("%s.count: 0 to 10 tasks, got %d", at, bl.Count)
		}
		if bl.Port < 0 || bl.Port > 65535 {
			return fmt.Errorf("%s.port: 1 to 65535, got %d", at, bl.Port)
		}
		if len(bl.Env) > MaxEnv {
			return fmt.Errorf("%s.env: at most %d settings", at, MaxEnv)
		}
		for k, v := range bl.Env {
			if !envRE.MatchString(k) {
				return fmt.Errorf("%s.env: %q must be upper-case letters, digits and underscores", at, k)
			}
			if len(v) > 1024 {
				return fmt.Errorf("%s.env.%s: at most 1024 characters", at, k)
			}
		}
	case KindFrontDoor:
		if bl.HealthPath != "" && (!strings.HasPrefix(bl.HealthPath, "/") || len(bl.HealthPath) > 200 || strings.ContainsAny(bl.HealthPath, " \t\n\"\\$%")) {
			return fmt.Errorf("%s.health_path: a path such as /health", at)
		}
		if bl.Certificate != "" && !certRE.MatchString(bl.Certificate) {
			return fmt.Errorf("%s.certificate: an ACM certificate ARN (arn:aws:acm:<region>:<account>:certificate/<id>)", at)
		}
	case KindTable:
		if !keyRE.MatchString(bl.PartitionKey) {
			return fmt.Errorf("%s.partition_key: give the name of the key every item has", at)
		}
		if bl.PartitionType != "S" && bl.PartitionType != "N" {
			return fmt.Errorf("%s.partition_type: S (text) or N (number)", at)
		}
		if bl.SortKey != "" || bl.SortType != "" {
			if !keyRE.MatchString(bl.SortKey) || bl.SortKey == bl.PartitionKey {
				return fmt.Errorf("%s.sort_key: a key name different from the partition key", at)
			}
			if bl.SortType != "S" && bl.SortType != "N" {
				return fmt.Errorf("%s.sort_type: S (text) or N (number)", at)
			}
		}
	case KindQueue:
		if bl.RetentionDays != 0 && (bl.RetentionDays < 1 || bl.RetentionDays > 14) {
			return fmt.Errorf("%s.retention_days: SQS keeps messages 1 to 14 days, got %d", at, bl.RetentionDays)
		}
	case KindLogs:
		if !slices.Contains(logRetention, bl.RetentionDays) {
			return fmt.Errorf("%s.retention_days: CloudWatch keeps logs for one of %v days", at, logRetention)
		}
	case KindExisting:
		e := bl.Existing
		if e == nil {
			return fmt.Errorf("%s.existing: say which table, bucket or queue", at)
		}
		re := map[string]*regexp.Regexp{"table": tableRE, "bucket": bucketRE, "queue": queueRE}[e.Type]
		if re == nil {
			return fmt.Errorf("%s.existing.type: table, bucket or queue, got %q", at, e.Type)
		}
		if !re.MatchString(e.Name) {
			return fmt.Errorf("%s.existing.name: %q is not a valid %s name", at, e.Name, e.Type)
		}
	}
	return nil
}

// setFields names the kind-specific fields that are set.
func (bl Block) setFields() []string {
	var out []string
	add := func(on bool, name string) {
		if on {
			out = append(out, name)
		}
	}
	add(bl.Image != "", "image")
	add(bl.CPU != 0, "cpu")
	add(bl.Memory != 0, "memory")
	add(bl.Count != 0, "count")
	add(bl.Port != 0, "port")
	add(len(bl.Env) > 0, "env")
	add(bl.HealthPath != "", "health_path")
	add(bl.Certificate != "", "certificate")
	add(bl.PartitionKey != "", "partition_key")
	add(bl.PartitionType != "", "partition_type")
	add(bl.SortKey != "", "sort_key")
	add(bl.SortType != "", "sort_type")
	add(bl.Versioning, "versioning")
	add(bl.RetentionDays != 0, "retention_days")
	add(bl.Existing != nil, "existing")
	return out
}

// Check is one plain-words statement about a blueprint.
type Check struct {
	Level string `json:"level"` // ok or warn
	Text  string `json:"text"`
}

// Checks says what a valid blueprint means for the person reading it. It is computed from the document only.
func (b Blueprint) Checks() []Check {
	var out []Check
	ok := func(t string) { out = append(out, Check{"ok", t}) }
	warn := func(t string) { out = append(out, Check{"warn", t}) }
	byKind := map[string][]Block{}
	for _, bl := range b.Blocks {
		byKind[bl.Kind] = append(byKind[bl.Kind], bl)
	}
	fronted, logged := map[string]bool{}, map[string]bool{}
	for _, l := range b.Links {
		for _, bl := range b.Blocks {
			if bl.ID == l.From && bl.Kind == KindFrontDoor {
				fronted[l.To] = true
			}
			if bl.ID == l.To && bl.Kind == KindLogs {
				logged[l.From] = true
			}
		}
	}
	if n := len(byKind[KindFrontDoor]); n > 0 {
		ok(plural(n, "Only the front door is", "Only the front doors are") + " reachable from the internet; services take traffic only from their front door.")
	} else {
		ok("Nothing in it is reachable from the internet.")
	}
	for _, d := range byKind[KindFrontDoor] {
		if d.Certificate == "" {
			warn(d.Name + " uses plain HTTP. Add a certificate for HTTPS before real users send it anything private.")
		}
	}
	if len(byKind[KindTable])+len(byKind[KindBucket]) > 0 {
		ok("Tables and buckets keep their data: removing one from the blueprint will not delete it.")
	}
	for _, s := range byKind[KindBucket] {
		if !s.Versioning {
			warn(s.Name + " keeps no old versions of files, so an overwritten file cannot be brought back.")
		}
	}
	for _, s := range byKind[KindService] {
		if !fronted[s.ID] && s.Count > 0 {
			warn(s.Name + " runs, but no front door sends it traffic. That is fine for a worker that reads a queue.")
		}
		if s.Count == 0 {
			warn(s.Name + " is set to run no tasks.")
		}
		if !logged[s.ID] {
			ok(s.Name + " writes its logs to its own log group, kept 30 days.")
		}
	}
	for _, l := range byKind[KindLogs] {
		ok(fmt.Sprintf("%s: logs are kept %d days.", l.Name, l.RetentionDays))
	}
	if len(byKind[KindExisting]) > 0 {
		ok(plural(len(byKind[KindExisting]), "One existing resource is", fmt.Sprintf("%d existing resources are", len(byKind[KindExisting]))) + " used as it is and never changed.")
	}
	if len(b.Blocks) > 0 {
		warn("One region only (" + b.Target.Region + "): if that region has an outage, so does this.")
	}
	return out
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
