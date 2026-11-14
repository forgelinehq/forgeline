// Package blueprint is the typed model behind the design canvas (design note 035): what should exist in a
// cloud account, as blocks and the links between them. It validates a blueprint, says in plain words what it
// means (Checks), and generates OpenTofu code for it (Tofu). It makes no network calls.
package blueprint

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/forgelinehq/forgeline/internal/template"
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
	KindDatabase  = "database"  // an RDS database, MySQL or Postgres (note 039)
	KindCache     = "cache"     // a Redis cache (ElastiCache)
	KindSecret    = "secret"    // a Secrets Manager secret whose value a person sets in AWS
	KindAgent     = "agent"     // an AI agent on Amazon Bedrock AgentCore Runtime, from a container image in ECR (note 040)
	KindTemplate  = "template"  // a reviewed template from the library, drawn as one block (note 040, step 2)
)

// Kinds lists the block kinds in the order the canvas offers them. Template blocks are offered one per template
// (GET /v1/templates), not as a kind of their own.
var Kinds = []string{KindFrontDoor, KindService, KindTable, KindBucket, KindQueue, KindLogs, KindDatabase, KindCache, KindSecret, KindAgent, KindExisting}

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
	// Scaling: between Min and Max tasks, keeping average CPU near CPUTarget percent. Off when Max is 0.
	Min       int `json:"min,omitempty"`
	Max       int `json:"max,omitempty"`
	CPUTarget int `json:"cpu_target,omitempty"`

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

	// database
	Engine     string `json:"engine,omitempty"` // mysql or postgres
	Size       string `json:"size,omitempty"`   // database and cache: small, medium or large
	StorageGB  int    `json:"storage_gb,omitempty"`
	MultiAZ    bool   `json:"multi_az,omitempty"`
	BackupDays int    `json:"backup_days,omitempty"`

	// cache
	Nodes int `json:"nodes,omitempty"`

	// secret
	Description string `json:"description,omitempty"`

	// existing
	Existing *Existing `json:"existing,omitempty"`

	// template: which template (name@version) and its inputs, checked against the template's schema.
	Template string         `json:"template,omitempty"`
	Inputs   map[string]any `json:"inputs,omitempty"`

	// Source is where an app's or agent's code lives (note 040, step 4): the blueprint then makes a private image
	// repository for it and a role only that repository's branch can use from GitHub Actions to push images.
	Source *Source `json:"source,omitempty"`
}

// Existing names something already in the account.
type Existing struct {
	Type string `json:"type"` // table, bucket, queue, database, cache or secret
	Name string `json:"name"`
}

// Link connects two blocks. What it means follows from their kinds: a front door sends traffic to a service; a
// service uses a table, bucket, queue or existing resource (Access), or writes its logs to a logs block.
// Source is a GitHub repository, branch and folder that holds a block's code.
type Source struct {
	Repo   string `json:"repo"`             // owner/name
	Branch string `json:"branch,omitempty"` // default main
	Path   string `json:"path,omitempty"`   // the folder with the Dockerfile; default the top
}

// BranchOr is the branch, or main when none is set.
func (s Source) BranchOr() string {
	if s.Branch == "" {
		return "main"
	}
	return s.Branch
}

// PathOr is the folder, or "." for the top of the repository.
func (s Source) PathOr() string {
	if s.Path == "" {
		return "."
	}
	return s.Path
}

type Link struct {
	From   string `json:"from"`
	To     string `json:"to"`
	Access string `json:"access,omitempty"` // read or readwrite, for a service using a table, bucket or queue
	Path   string `json:"path,omitempty"`   // for a front door: the path it sends to this service (/api/*); empty is the default
}

var (
	idRE    = regexp.MustCompile(`^[a-z][a-z0-9-]{0,29}$`)
	keyRE   = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,255}$`)
	envRE   = regexp.MustCompile(`^[A-Z_][A-Z0-9_]{0,63}$`)
	imageRE = regexp.MustCompile(`^[a-z0-9][A-Za-z0-9._/:@-]{0,254}$`)
	certRE  = regexp.MustCompile(`^arn:aws[a-z-]*:acm:[a-z0-9-]+:\d{12}:certificate/[A-Za-z0-9-]+$`)
	// An agent's image must be in ECR: AgentCore pulls only from there. Groups: account, region, repository.
	repoRE     = regexp.MustCompile(`^[A-Za-z0-9-]{1,39}/[A-Za-z0-9._-]{1,100}$`)
	branchRE   = regexp.MustCompile(`^[A-Za-z0-9._/-]{1,100}$`)
	srcPathRE  = regexp.MustCompile(`^[A-Za-z0-9._/-]{0,200}$`)
	ecrImageRE = regexp.MustCompile(`^(\d{12})\.dkr\.ecr\.([a-z0-9-]+)\.amazonaws\.com/([a-z0-9][a-z0-9._/-]{0,254})(:[A-Za-z0-9._-]{1,128}|@sha256:[a-f0-9]{64})$`)
	regionRE   = regexp.MustCompile(`^[a-z]{2}(-[a-z]+)+-\d$`)
	accountRE  = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,39}$`)
	tableRE    = regexp.MustCompile(`^[A-Za-z0-9_.-]{3,255}$`)
	bucketRE   = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]{1,61}[a-z0-9]$`)
	queueRE    = regexp.MustCompile(`^[A-Za-z0-9_-]{1,80}(\.fifo)?$`)
	dbIDRE     = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9-]{0,62}$`)
	cacheIDRE  = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9-]{0,39}$`)
	secretRE   = regexp.MustCompile(`^[A-Za-z0-9/_+=.@-]{1,512}$`)
	pathRE     = regexp.MustCompile(`^/[A-Za-z0-9._~/*-]{0,100}$`)
)

// Instance sizes behind the plain names (note 039).
var (
	DatabaseSizes = map[string]string{"small": "db.t4g.micro", "medium": "db.t4g.small", "large": "db.t4g.medium"}
	CacheSizes    = map[string]string{"small": "cache.t4g.micro", "medium": "cache.t4g.small"}
	DatabasePorts = map[string]int{"mysql": 3306, "postgres": 5432}
)

// usesData is a link a service makes to something it uses (not traffic, not logs, not another service).
func usesData(kind string, e *Existing) bool {
	switch kind {
	case KindTable, KindBucket, KindQueue:
		return true
	case KindExisting:
		return e != nil && (e.Type == "table" || e.Type == "bucket" || e.Type == "queue")
	}
	return false
}

// connects is a link a service makes to a database, a cache or a secret: no access level, the link is the access.
func connects(kind string, e *Existing) bool {
	switch kind {
	case KindDatabase, KindCache, KindSecret:
		return true
	case KindExisting:
		return e != nil && (e.Type == "database" || e.Type == "cache" || e.Type == "secret")
	}
	return false
}

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
		if m := ecrImageRE.FindStringSubmatch(bl.Image); bl.Kind == KindAgent && m != nil && m[2] != b.Target.Region {
			return fmt.Errorf("%s.image: the image is in %s but the blueprint builds in %s; AgentCore pulls from ECR in its own region", at, m[2], b.Target.Region)
		}
		byID[bl.ID] = bl
	}
	fronted := map[string]string{} // service -> front door
	doors := map[string]int{}      // front door -> links without a path (its default service)
	paths := map[string]int{}      // front door -> links with a path
	doorPaths := map[[2]string]bool{}
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
			if l.Path == "" {
				doors[from.ID]++
			} else {
				if !pathRE.MatchString(l.Path) {
					return fmt.Errorf("%s.path: a path such as /api/*", at)
				}
				if doorPaths[[2]string{from.ID, l.Path}] {
					return fmt.Errorf("%s.path: %s already sends %s somewhere", at, from.ID, l.Path)
				}
				doorPaths[[2]string{from.ID, l.Path}] = true
				if paths[from.ID]++; paths[from.ID] > 10 {
					return fmt.Errorf("%s: a front door takes at most 10 paths", at)
				}
			}
		case l.Path != "":
			return fmt.Errorf("%s.path: only a front door's links have a path", at)
		case from.Kind == KindService && to.Kind == KindLogs:
			if l.Access != "" {
				return fmt.Errorf("%s.access: logs take no access level", at)
			}
			if logsOf[from.ID] {
				return fmt.Errorf("%s: %s already writes its logs to another logs block", at, from.ID)
			}
			logsOf[from.ID] = true
		case from.Kind == KindService && usesData(to.Kind, to.Existing):
			if l.Access != AccessRead && l.Access != AccessReadWrite {
				return fmt.Errorf("%s.access: say read or readwrite, got %q", at, l.Access)
			}
		case from.Kind == KindService && connects(to.Kind, to.Existing):
			if l.Access != "" {
				return fmt.Errorf("%s.access: a link to a database, cache or secret is the access; leave access out", at)
			}
		case from.Kind == KindAgent && usesData(to.Kind, to.Existing):
			if l.Access != AccessRead && l.Access != AccessReadWrite {
				return fmt.Errorf("%s.access: say read or readwrite, got %q", at, l.Access)
			}
		case from.Kind == KindAgent && (to.Kind == KindSecret || (to.Kind == KindExisting && to.Existing != nil && to.Existing.Type == "secret")):
			if l.Access != "" {
				return fmt.Errorf("%s.access: a link to a secret is the access; leave access out", at)
			}
		case from.Kind == KindTemplate || to.Kind == KindTemplate:
			if err := templateLink(at, from, to, l); err != nil {
				return err
			}
		case from.Kind == KindService && to.Kind == KindService:
			if l.Access != "" {
				return fmt.Errorf("%s.access: one service calling another takes no access level", at)
			}
			if from.ID == to.ID {
				return fmt.Errorf("%s: a service cannot call itself", at)
			}
			if to.Port == 0 {
				return fmt.Errorf("%s: %s needs a port to be called", at, to.ID)
			}
		default:
			return fmt.Errorf("%s: a %s cannot link to a %s (a front door sends traffic to a service; a service uses data, a database, a cache or a secret, calls another service, or writes logs; an agent uses tables, buckets, queues and secrets)", at, from.Kind, to.Kind)
		}
	}
	for i, bl := range b.Blocks {
		if bl.Kind == KindFrontDoor && doors[bl.ID] != 1 {
			return fmt.Errorf("blocks[%d]: front door %s must send its default traffic (a link without a path) to exactly one service", i, bl.ID)
		}
	}
	return nil
}

// fillChecks puts input values into a template's check text: {memory} becomes 512.
func fillChecks(text string, in map[string]any) string {
	for k, v := range in {
		val := fmt.Sprint(v)
		if f, isNum := v.(float64); isNum {
			val = strconv.FormatFloat(f, 'f', -1, 64)
		}
		text = strings.ReplaceAll(text, "{"+k+"}", val)
	}
	return text
}

func blockByID(b Blueprint, id string) Block {
	for _, bl := range b.Blocks {
		if bl.ID == id {
			return bl
		}
	}
	return Block{}
}

// kindOf is what a block is for linking: an existing block counts as what it names.
func kindOf(bl Block) string {
	if bl.Kind == KindExisting && bl.Existing != nil {
		return bl.Existing.Type
	}
	return bl.Kind
}

// templateLink checks a link to or from a template block against the roles the template offers.
func templateLink(at string, from, to Block, l Link) error {
	tb, other, dir := from, to, "out"
	if to.Kind == KindTemplate && from.Kind != KindTemplate {
		tb, other, dir = to, from, "in"
	}
	tpl, err := template.Get(tb.Template)
	if err != nil {
		return fmt.Errorf("%s: %v", at, err)
	}
	role, ok := tpl.Role(dir, kindOf(other))
	if !ok || other.Kind == KindTemplate {
		var offers []string
		for _, r := range tpl.Diagram.Links {
			if r.Direction == "out" {
				offers = append(offers, r.Name+" "+strings.Join(r.Kinds, ", "))
			} else {
				offers = append(offers, "a "+strings.Join(r.Kinds, " or ")+" "+r.Name+" it")
			}
		}
		return fmt.Errorf("%s: a %s cannot link to a %s (%s offers: %s)", at, kindOf(from), kindOf(to), tpl.Title, strings.Join(offers, "; "))
	}
	if role.Access && l.Access != AccessRead && l.Access != AccessReadWrite {
		return fmt.Errorf("%s.access: say read or readwrite, got %q", at, l.Access)
	}
	if !role.Access && l.Access != "" {
		return fmt.Errorf("%s.access: this link takes no access level", at)
	}
	return nil
}

func (bl Block) validate(at string) error {
	if strings.TrimSpace(bl.Name) == "" || len(bl.Name) > 60 {
		return fmt.Errorf("%s.name: give a name of at most 60 characters", at)
	}
	set := bl.setFields()
	allowed := map[string][]string{
		KindService:   {"image", "cpu", "memory", "count", "port", "env", "min", "max", "cpu_target", "source"},
		KindFrontDoor: {"health_path", "certificate"},
		KindTable:     {"partition_key", "partition_type", "sort_key", "sort_type"},
		KindBucket:    {"versioning"},
		KindQueue:     {"retention_days"},
		KindLogs:      {"retention_days"},
		KindDatabase:  {"engine", "size", "storage_gb", "multi_az", "backup_days"},
		KindCache:     {"size", "nodes"},
		KindSecret:    {"description"},
		KindAgent:     {"image", "env", "description", "source"},
		KindTemplate:  {"template", "inputs", "source"},
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
	if s := bl.Source; s != nil {
		switch {
		case !repoRE.MatchString(s.Repo):
			return fmt.Errorf("%s.source.repo: a GitHub repository as owner/name", at)
		case s.Branch != "" && (!branchRE.MatchString(s.Branch) || strings.Contains(s.Branch, "..") || strings.HasPrefix(s.Branch, "/") || strings.HasSuffix(s.Branch, "/")):
			return fmt.Errorf("%s.source.branch: a branch name such as main", at)
		case !srcPathRE.MatchString(s.Path) || strings.Contains(s.Path, "..") || strings.HasPrefix(s.Path, "/"):
			return fmt.Errorf("%s.source.path: a folder inside the repository, such as services/api", at)
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
		if bl.Max > 0 || bl.Min > 0 || bl.CPUTarget > 0 {
			if bl.Min < 1 || bl.Max < bl.Min || bl.Max > 20 {
				return fmt.Errorf("%s.max: scaling needs 1 <= min <= max <= 20, got min %d and max %d", at, bl.Min, bl.Max)
			}
			if bl.CPUTarget != 0 && (bl.CPUTarget < 20 || bl.CPUTarget > 90) {
				return fmt.Errorf("%s.cpu_target: a percentage from 20 to 90, got %d", at, bl.CPUTarget)
			}
			if bl.Count < bl.Min || bl.Count > bl.Max {
				return fmt.Errorf("%s.count: with scaling, start between min (%d) and max (%d)", at, bl.Min, bl.Max)
			}
		}
		for k, v := range bl.Env {
			if !envRE.MatchString(k) {
				return fmt.Errorf("%s.env: %q must be upper-case letters, digits and underscores", at, k)
			}
			if len(v) > 1024 {
				return fmt.Errorf("%s.env.%s: at most 1024 characters", at, k)
			}
		}
	case KindAgent:
		if !ecrImageRE.MatchString(bl.Image) {
			return fmt.Errorf("%s.image: AgentCore runs images from ECR only, such as 111122223333.dkr.ecr.ap-south-1.amazonaws.com/my-agent:1.0 (built for arm64)", at)
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
		if len(bl.Description) > 200 {
			return fmt.Errorf("%s.description: at most 200 characters", at)
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
	case KindDatabase:
		if _, ok := DatabasePorts[bl.Engine]; !ok {
			return fmt.Errorf("%s.engine: mysql or postgres, got %q", at, bl.Engine)
		}
		if _, ok := DatabaseSizes[bl.Size]; !ok {
			return fmt.Errorf("%s.size: small, medium or large, got %q", at, bl.Size)
		}
		if bl.StorageGB < 20 || bl.StorageGB > 1000 {
			return fmt.Errorf("%s.storage_gb: 20 to 1000, got %d", at, bl.StorageGB)
		}
		if bl.BackupDays != 0 && (bl.BackupDays < 1 || bl.BackupDays > 35) {
			return fmt.Errorf("%s.backup_days: 1 to 35, got %d", at, bl.BackupDays)
		}
	case KindCache:
		if _, ok := CacheSizes[bl.Size]; !ok {
			return fmt.Errorf("%s.size: small or medium, got %q", at, bl.Size)
		}
		if bl.Nodes < 0 || bl.Nodes > 3 {
			return fmt.Errorf("%s.nodes: 1 to 3, got %d", at, bl.Nodes)
		}
	case KindSecret:
		if len(bl.Description) > 200 || strings.ContainsAny(bl.Description, "$%{}") {
			return fmt.Errorf("%s.description: at most 200 characters, without $, %%, { or }", at)
		}
	case KindTemplate:
		tpl, err := template.Get(bl.Template)
		if err != nil {
			return fmt.Errorf("%s.template: %v", at, err)
		}
		if _, err := tpl.Inputs.Resolve(bl.Inputs); err != nil {
			return fmt.Errorf("%s.inputs: %v", at, err)
		}
	case KindExisting:
		e := bl.Existing
		if e == nil {
			return fmt.Errorf("%s.existing: say which table, bucket, queue, database, cache or secret", at)
		}
		re := map[string]*regexp.Regexp{"table": tableRE, "bucket": bucketRE, "queue": queueRE, "database": dbIDRE, "cache": cacheIDRE, "secret": secretRE}[e.Type]
		if re == nil {
			return fmt.Errorf("%s.existing.type: table, bucket, queue, database, cache or secret, got %q", at, e.Type)
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
	add(bl.Source != nil, "source")
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
	add(bl.Min != 0, "min")
	add(bl.Max != 0, "max")
	add(bl.CPUTarget != 0, "cpu_target")
	add(bl.Engine != "", "engine")
	add(bl.Size != "", "size")
	add(bl.StorageGB != 0, "storage_gb")
	add(bl.MultiAZ, "multi_az")
	add(bl.BackupDays != 0, "backup_days")
	add(bl.Nodes != 0, "nodes")
	add(bl.Description != "", "description")
	add(bl.Existing != nil, "existing")
	add(bl.Template != "", "template")
	add(len(bl.Inputs) > 0, "inputs")
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
		ok(plural(n, "Only the front door is", "Only the front doors are") + " reachable from the internet; services take traffic only from their front door and the services that call them.")
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
	for _, d := range byKind[KindDatabase] {
		days := d.BackupDays
		if days == 0 {
			days = 7
		}
		engine := map[string]string{"mysql": "MySQL", "postgres": "PostgreSQL"}[d.Engine]
		ok(fmt.Sprintf("%s (%s) is private and encrypted, keeps backups %d days, cannot be deleted by mistake, and AWS keeps its password: Forgeline never sees it.", d.Name, engine, days))
		if !d.MultiAZ {
			warn(d.Name + " runs in one availability zone; turn on multi-AZ to survive a zone failure.")
		}
	}
	for _, c := range byKind[KindCache] {
		ok(c.Name + " (Redis) is private, encrypted in transit and at rest, and only linked services can connect.")
	}
	for _, a := range byKind[KindAgent] {
		ok(a.Name + " runs on Amazon Bedrock AgentCore from its ECR image, may call Bedrock models, and can use only what it is linked to. Its role cannot issue tokens for arbitrary user ids.")
		warn(a.Name + "'s image must be built for arm64 and answer on port 8080 (/invocations and /ping), as AgentCore requires.")
	}
	for _, sc := range byKind[KindSecret] {
		warn(sc.Name + " starts empty: set its value in AWS Secrets Manager after it is built. Forgeline never reads or stores it.")
	}
	for _, l := range b.Links {
		var from, to Block
		for _, bl := range b.Blocks {
			if bl.ID == l.From {
				from = bl
			}
			if bl.ID == l.To {
				to = bl
			}
		}
		if from.Kind == KindService && to.Kind == KindService {
			ok(fmt.Sprintf("%s calls %s privately inside the account (%s is not reachable from the internet through this link).", from.Name, to.Name, to.Name))
		}
	}
	for _, s := range byKind[KindService] {
		if s.Max > 0 {
			target := s.CPUTarget
			if target == 0 {
				target = 60
			}
			ok(fmt.Sprintf("%s scales between %d and %d tasks, keeping CPU near %d%%.", s.Name, s.Min, s.Max, target))
		}
	}
	for _, l := range byKind[KindLogs] {
		ok(fmt.Sprintf("%s: logs are kept %d days.", l.Name, l.RetentionDays))
	}
	for _, tb := range byKind[KindTemplate] {
		tpl, err := template.Get(tb.Template)
		if err != nil {
			continue
		}
		in, _ := tpl.Inputs.Resolve(tb.Inputs)
		for _, c := range tpl.Checks {
			ok(tb.Name + ": " + fillChecks(c, in))
		}
		for _, l := range b.Links {
			switch {
			case l.From == tb.ID:
				if role, found := tpl.Role("out", kindOf(blockByID(b, l.To))); found {
					t := tb.Name + " " + role.Name + " " + l.To
					if l.Access != "" {
						t += map[string]string{AccessRead: " (read only)", AccessReadWrite: " (read and write)"}[l.Access]
					}
					ok(t + ".")
				}
			case l.To == tb.ID:
				if role, found := tpl.Role("in", kindOf(blockByID(b, l.From))); found {
					ok(l.From + " " + role.Name + " " + tb.Name + ".")
				}
			}
		}
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
