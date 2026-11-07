package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"
)

// Service switches for a connection (design note 030). An administrator turns AWS services on or off for
// the AI: the assistant, agents and MCP clients. An off service is left out of what they read (resources,
// findings) and refused in what they run (CLI calls, scripts, change proposals), and a proposal for it is
// refused at approval if the service was turned off after it was filed. People still see everything in the
// explorer: the switches govern what the AI may touch, not what a person may look at.
//
// Nothing stored means every service is on, which is how Forgeline behaved before. "Other services" covers
// any AWS service not in the list, so a service missing from the catalogue is never silently allowed once
// someone has chosen to turn it off.

// awsService is one switch: a plain name, the technical name people search for, the AWS API names
// (CLI and boto3) it covers, and the explorer's inventory ids.
type awsService struct {
	ID        string
	Label     string
	Tech      string
	API       []string
	Inventory []string
}

const svcOther = "other"

var awsServiceCatalog = []awsService{
	{ID: "s3", Label: "File storage", Tech: "S3", API: []string{"s3", "s3api", "s3control"}, Inventory: []string{"s3"}},
	{ID: "ecs", Label: "Containers", Tech: "ECS", API: []string{"ecs"}, Inventory: []string{"ecs", "ecs-services"}},
	{ID: "ecr", Label: "Image registry", Tech: "ECR", API: []string{"ecr"}, Inventory: []string{"ecr"}},
	{ID: "ec2", Label: "Virtual machines and networks", Tech: "EC2, EBS, VPC", API: []string{"ec2", "autoscaling"}, Inventory: []string{"ec2", "ebs", "vpc", "sg"}},
	{ID: "lambda", Label: "Functions", Tech: "Lambda", API: []string{"lambda"}, Inventory: []string{"lambda"}},
	{ID: "rds", Label: "Databases", Tech: "RDS", API: []string{"rds"}, Inventory: []string{"rds"}},
	{ID: "dynamodb", Label: "Tables", Tech: "DynamoDB", API: []string{"dynamodb"}, Inventory: []string{"dynamodb"}},
	{ID: "elb", Label: "Load balancers", Tech: "ELB", API: []string{"elbv2", "elb"}, Inventory: []string{"elb"}},
	{ID: "sqs", Label: "Queues", Tech: "SQS", API: []string{"sqs"}, Inventory: []string{"sqs"}},
	{ID: "sns", Label: "Notifications", Tech: "SNS", API: []string{"sns"}, Inventory: []string{"sns"}},
	{ID: "cloudformation", Label: "Stacks", Tech: "CloudFormation", API: []string{"cloudformation"}, Inventory: []string{"cfn"}},
	{ID: "cloudwatch", Label: "Logs and alarms", Tech: "CloudWatch", API: []string{"logs", "cloudwatch"}, Inventory: []string{"logs"}},
	{ID: "ce", Label: "Costs", Tech: "Cost Explorer", API: []string{"ce"}},
	{ID: svcOther, Label: "Other services", Tech: "anything not listed above"},
}

// awsSwitchForAPI maps an AWS API name (as the CLI or boto3 spells it) to its switch.
func awsSwitchForAPI(api string) string {
	api = strings.ToLower(strings.TrimSpace(api))
	for _, s := range awsServiceCatalog {
		for _, a := range s.API {
			if a == api {
				return s.ID
			}
		}
	}
	return svcOther
}

// awsSwitchForInventory maps an explorer or Guard service id (ecs-services, sg, iam) to its switch.
func awsSwitchForInventory(id string) string {
	id = strings.ToLower(strings.TrimSpace(id))
	for _, s := range awsServiceCatalog {
		for _, i := range s.Inventory {
			if i == id {
				return s.ID
			}
		}
	}
	return svcOther
}

func awsServiceByID(id string) (awsService, bool) {
	for _, s := range awsServiceCatalog {
		if s.ID == id {
			return s, true
		}
	}
	return awsService{}, false
}

func awsServiceName(id string) string {
	if s, ok := awsServiceByID(id); ok {
		if id == svcOther {
			return s.Label
		}
		return s.Label + " (" + s.Tech + ")"
	}
	return id
}

// awsServicesOff is the set of switched-off services. An unreadable policy is an error: callers refuse.
func (s *Server) awsServicesOff(ctx context.Context) (map[string]bool, error) {
	p, err := s.loadPolicy(ctx)
	if err != nil {
		return nil, err
	}
	off := map[string]bool{}
	for _, id := range p.Services["aws"] {
		off[id] = true
	}
	return off, nil
}

// awsCallServices says which switches a tool call touches, before it runs. ok is false for tools that are not
// per-service (they are filtered instead, or do not read an account).
func awsCallServices(c lookupCall) (switches []string, ok bool) {
	switch c.Tool {
	case "call_aws":
		var a struct {
			Command string `json:"cli_command"`
		}
		if json.Unmarshal(c.Args, &a) != nil {
			return nil, false
		}
		args, _, err := parseAWSCommand(a.Command)
		if err != nil || len(args) == 0 {
			return nil, false // the tool refuses it itself
		}
		return []string{awsSwitchForAPI(args[0])}, true
	case "aws_script", toolProposeAWS:
		var a struct {
			Code string `json:"code"`
		}
		if json.Unmarshal(c.Args, &a) != nil {
			return nil, false
		}
		calls, err := checkScript(a.Code, c.Tool == toolProposeAWS)
		if err != nil {
			return nil, false // the tool refuses it itself, with the script rule's reason
		}
		return scriptSwitches(calls), true
	case toolProposeCloud: // a stack deploy or deletion, or an instance start, stop or reboot
		var a struct {
			Kind string `json:"kind"`
		}
		if json.Unmarshal(c.Args, &a) != nil {
			return nil, false
		}
		switch a.Kind {
		case kindStackDeploy, kindStackDelete:
			return []string{"cloudformation"}, true
		case kindInstanceAction:
			return []string{"ec2"}, true
		}
		return nil, false // the tool refuses an unknown kind itself
	case "aws_summary": // stacks and instances
		return []string{"cloudformation", "ec2"}, true
	}
	return nil, false
}

func scriptSwitches(calls []scriptCall) []string {
	seen := map[string]bool{}
	var out []string
	for _, c := range calls {
		if id := awsSwitchForAPI(c.Service); !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out
}

// servicesGate refuses a call that touches a switched-off service. It runs after policyGate, in the same
// place, so every way to run a tool passes it.
func (s *Server) servicesGate(ctx context.Context, p Principal, c lookupCall) error {
	switches, ok := awsCallServices(c)
	if !ok {
		return nil
	}
	off, err := s.awsServicesOff(ctx)
	if err != nil {
		return errors.New("the access policy could not be read, so " + c.Tool + " is refused until it can")
	}
	for _, id := range switches {
		if off[id] {
			why := fmt.Sprintf("%s is turned off for the AI in Settings › Integrations › AWS, so %s is refused", awsServiceName(id), c.Tool)
			ip, _ := ctx.Value(mcpIPKey{}).(string)
			_ = s.audit(ctx, p, ip, "policy.refused", "integration/aws", why, false)
			return errors.New(why)
		}
	}
	return nil
}

// offServicesNote is the line the AI reads under a filtered result, so it can tell the person why
// something is missing instead of guessing.
func offServicesNote(off map[string]bool) string {
	if len(off) == 0 {
		return ""
	}
	var names []string
	for _, s := range awsServiceCatalog {
		if off[s.ID] {
			names = append(names, awsServiceName(s.ID))
		}
	}
	return "\nNot shown, because an administrator turned these off for the AI: " + strings.Join(names, ", ") + ".\n"
}

// --- the API ---

// AWSServiceSwitchJSON is one switch as the console shows it.
type AWSServiceSwitchJSON struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Tech  string `json:"tech"`
	On    bool   `json:"on"`
}

func servicesView(p accessPolicy) []AWSServiceSwitchJSON {
	off := map[string]bool{}
	for _, id := range p.Services["aws"] {
		off[id] = true
	}
	out := make([]AWSServiceSwitchJSON, 0, len(awsServiceCatalog))
	for _, s := range awsServiceCatalog {
		out = append(out, AWSServiceSwitchJSON{ID: s.ID, Label: s.Label, Tech: s.Tech, On: !off[s.ID]})
	}
	return out
}

func (s *Server) handlePutServices(w http.ResponseWriter, r *http.Request) {
	if r.PathValue("id") != "aws" {
		writeError(w, http.StatusNotFound, "service switches exist for AWS only")
		return
	}
	var req struct {
		Off []string `json:"off"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	seen := map[string]bool{}
	var off []string
	for i, id := range req.Off {
		if _, ok := awsServiceByID(id); !ok {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("off[%d]: %q is not an AWS service switch", i, id))
			return
		}
		if !seen[id] {
			seen[id] = true
			off = append(off, id)
		}
	}
	sort.Strings(off)
	// Read before writing; an unreadable policy is not overwritten blindly (as in handlePutPolicy).
	cur, err := s.loadPolicy(r.Context())
	if err != nil {
		writeError(w, http.StatusConflict, "the stored access policy cannot be read, so it will not be changed over; fix or remove it first")
		return
	}
	before := append([]string(nil), cur.Services["aws"]...)
	if cur.Services == nil {
		cur.Services = map[string][]string{}
	}
	if len(off) == 0 {
		delete(cur.Services, "aws")
	} else {
		cur.Services["aws"] = off
	}
	cur.UpdatedAt, cur.UpdatedBy = time.Now().UTC(), principalFrom(r.Context()).Name
	if err := s.docs.Put(r.Context(), kindSettings, idPolicy, cur); err != nil {
		s.internalError(w, "saving the policy", err)
		return
	}
	s.auditReq(r, "policy.services", "integration/aws", fmt.Sprintf("off for the AI: [%s] (was [%s])", strings.Join(off, ", "), strings.Join(before, ", ")), true)
	writeJSON(w, http.StatusOK, s.policyView(r.Context(), "aws"))
}
