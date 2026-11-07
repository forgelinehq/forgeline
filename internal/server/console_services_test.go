package server

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/forgelinehq/forgeline/internal/authn"
)

func TestAWSServiceSwitchMapping(t *testing.T) {
	for _, tc := range []struct{ api, want string }{
		{"s3", "s3"}, {"s3api", "s3"}, {"S3API", "s3"}, {"ecs", "ecs"}, {"ecr", "ecr"}, {"ec2", "ec2"},
		{"elbv2", "elb"}, {"logs", "cloudwatch"}, {"cloudwatch", "cloudwatch"}, {"ce", "ce"},
		{"iam", svcOther}, {"ssm", svcOther}, {"", svcOther}, {"newservice", svcOther},
	} {
		if got := awsSwitchForAPI(tc.api); got != tc.want {
			t.Errorf("api %q -> %q, want %q", tc.api, got, tc.want)
		}
	}
	for _, tc := range []struct{ inv, want string }{
		{"ecs-services", "ecs"}, {"sg", "ec2"}, {"ebs", "ec2"}, {"vpc", "ec2"}, {"cfn", "cloudformation"}, {"iam", svcOther}, {"s3-access", svcOther},
	} {
		if got := awsSwitchForInventory(tc.inv); got != tc.want {
			t.Errorf("inventory %q -> %q, want %q", tc.inv, got, tc.want)
		}
	}
	raw := func(v any) json.RawMessage { b, _ := json.Marshal(v); return b }
	for name, tc := range map[string]struct {
		call lookupCall
		want []string
		ok   bool
	}{
		"cli":              {lookupCall{Tool: "call_aws", Args: raw(map[string]string{"cli_command": "aws sqs list-queues --region ap-south-1"})}, []string{"sqs"}, true},
		"cli unparseable":  {lookupCall{Tool: "call_aws", Args: raw(map[string]string{"cli_command": "rm -rf /"})}, nil, false},
		"read script":      {lookupCall{Tool: "aws_script", Args: raw(map[string]string{"code": readScript})}, []string{"ecs"}, true},
		"proposal":         {lookupCall{Tool: toolProposeAWS, Args: raw(map[string]string{"code": changeScript})}, []string{"ecs"}, true},
		"two services":     {lookupCall{Tool: "aws_script", Args: raw(map[string]string{"code": "a = await call_boto3(service_name='sqs', operation_name='ListQueues')\nb = await call_boto3(service_name='ecs', operation_name='ListClusters')\nresult = [a, b]\nresult"})}, []string{"ecs", "sqs"}, true},
		"stack deploy":     {lookupCall{Tool: toolProposeCloud, Args: raw(map[string]string{"kind": kindStackDeploy})}, []string{"cloudformation"}, true},
		"instance action":  {lookupCall{Tool: toolProposeCloud, Args: raw(map[string]string{"kind": kindInstanceAction})}, []string{"ec2"}, true},
		"summary":          {lookupCall{Tool: "aws_summary", Args: raw(map[string]string{})}, []string{"cloudformation", "ec2"}, true},
		"filtered instead": {lookupCall{Tool: "aws_resources", Args: raw(map[string]string{})}, nil, false},
		"not aws":          {lookupCall{Tool: "list_runs", Args: raw(map[string]string{})}, nil, false},
	} {
		got, ok := awsCallServices(tc.call)
		if ok != tc.ok || strings.Join(got, ",") != strings.Join(tc.want, ",") {
			t.Errorf("%s: got %v %v, want %v %v", name, got, ok, tc.want, tc.ok)
		}
	}
}

func servicesOf(t *testing.T, v PolicyJSON) map[string]bool {
	t.Helper()
	on := map[string]bool{}
	for _, s := range v.Services {
		on[s.ID] = s.On
	}
	return on
}

func TestServiceSwitchesAreForAdministratorsAndKnownServicesOnly(t *testing.T) {
	m, _ := policyHarness(t)
	on := servicesOf(t, policyOf(t, m.dev, "aws"))
	if len(on) != len(awsServiceCatalog) {
		t.Fatalf("services = %v", on)
	}
	for id, v := range on {
		if !v {
			t.Errorf("%s starts off; nothing is off until someone turns it off", id)
		}
	}
	if v := policyOf(t, m.dev, "kubernetes"); len(v.Services) != 0 {
		t.Errorf("kubernetes has service switches: %+v", v.Services)
	}
	m.dev.expect(403, "PUT", "/v1/integrations/aws/services", map[string]any{"off": []string{"s3"}})
	m.v.expect(403, "PUT", "/v1/integrations/aws/services", map[string]any{"off": []string{"s3"}})
	m.admin.expect(400, "PUT", "/v1/integrations/aws/services", map[string]any{"off": []string{"s3", "nope"}})
	m.admin.expect(404, "PUT", "/v1/integrations/gcp/services", map[string]any{"off": []string{"s3"}})
	m.admin.expect(200, "PUT", "/v1/integrations/aws/services", map[string]any{"off": []string{"s3", "s3", "sqs"}})
	on = servicesOf(t, policyOf(t, m.dev, "aws"))
	if on["s3"] || on["sqs"] || !on["ecs"] || !on[svcOther] {
		t.Errorf("after turning off s3 and sqs: %v", on)
	}
	// Saving tool levels leaves the service switches alone, and the other way round.
	m.admin.expect(200, "PUT", "/v1/integrations/aws/policy", map[string]any{"tools": map[string]string{"call_aws": "block"}})
	if on := servicesOf(t, policyOf(t, m.dev, "aws")); on["s3"] {
		t.Error("saving tool levels turned a service back on")
	}
	m.admin.expect(200, "PUT", "/v1/integrations/aws/services", map[string]any{"off": []string{}})
	if levelOf(policyOf(t, m.dev, "aws"), "call_aws") != polBlock {
		t.Error("saving services unblocked a tool")
	}
	if on := servicesOf(t, policyOf(t, m.dev, "aws")); !on["s3"] || !on["sqs"] {
		t.Errorf("turning everything back on: %v", on)
	}
	a := string(m.admin.expect(200, "GET", "/v1/audit", nil))
	if !strings.Contains(a, "policy.services") || !strings.Contains(a, "off for the AI: [s3, sqs]") {
		t.Errorf("audit does not record the change: %s", a)
	}
}

func TestAnOffServiceIsRefusedBeforeItRunsAndLeftOutOfWhatTheAIReads(t *testing.T) {
	m, stub := policyHarness(t)
	ctx := context.Background()
	cli := func(cmd string) (string, error) { // call_aws is for administrators
		return m.srv.runLookup(ctx, Principal{Name: "ada", Role: authn.Admin}, lookupCall{Tool: "call_aws", Args: json.RawMessage(`{"cli_command":"` + cmd + `"}`)})
	}
	if _, err := cli("aws sqs list-queues"); err != nil {
		t.Fatalf("before turning anything off: %v", err)
	}
	m.admin.expect(200, "PUT", "/v1/integrations/aws/services", map[string]any{"off": []string{"sqs", "ec2", svcOther}})

	before := len(stub.calls)
	for _, cmd := range []string{"aws sqs list-queues", "aws iam list-users"} { // listed, and "other"
		if _, err := cli(cmd); err == nil || !strings.Contains(err.Error(), "turned off for the AI") {
			t.Errorf("%s was not refused: %v", cmd, err)
		}
	}
	if _, err := m.srv.runLookup(ctx, polDev, lookupCall{Tool: "aws_summary", Args: json.RawMessage(`{}`)}); err == nil {
		t.Error("aws_summary reads instances, and EC2 is off")
	}
	if len(stub.calls) != before {
		t.Errorf("a refused call reached the command line: %v", stub.calls[before:])
	}
	// An agent whose own tool list includes the tool is not exempt.
	agentCtx := context.WithValue(ctx, agentScopeKey{}, &agentScope{tools: map[string]bool{"call_aws": true}, label: "t@1"})
	if _, err := m.srv.runLookup(agentCtx, Principal{Name: "ada", Role: authn.Admin}, lookupCall{Tool: "call_aws", Args: json.RawMessage(`{"cli_command":"aws sqs list-queues"}`)}); err == nil {
		t.Error("an agent used a switched-off service")
	}
	// A service that is on still works.
	if _, err := cli("aws ecs list-clusters"); err != nil {
		t.Errorf("ecs is on: %v", err)
	}
	// Resources are listed without the off services, and the AI is told why.
	out, err := m.srv.runLookup(ctx, polDev, lookupCall{Tool: "aws_resources", Args: json.RawMessage(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "turned these off for the AI: Virtual machines and networks (EC2, EBS, VPC), Queues (SQS), Other services") {
		t.Errorf("no note about off services:\n%s", out)
	}
	for _, label := range []string{"EC2 instances", "Security groups", "SQS queues", "EBS volumes"} {
		if strings.Contains(out, "\n"+label) {
			t.Errorf("%s is listed although it is off:\n%s", label, out)
		}
	}
	// The refusals are audited against AWS.
	n := 0
	for _, e := range activityOf(t, m.admin, "aws") {
		if e.Action == "policy.refused" && strings.Contains(e.Detail, "turned off for the AI") {
			n++
		}
	}
	if n != 4 {
		t.Errorf("%d refusals audited, want 4", n)
	}
	// An unreadable policy refuses rather than showing everything.
	m.srv.docs.Put(ctx, kindSettings, idPolicy, "this is not a policy")
	if _, err := m.srv.awsServicesOff(ctx); err == nil {
		t.Error("an unreadable policy gave a set of services")
	}
}

func TestScriptsAndProposalsForAnOffServiceAreRefusedAndApprovalChecksAgain(t *testing.T) {
	m := newMCPHarness(t)
	m.turnOn()
	ctx := context.Background()

	// Filed while ECS is on.
	_, id := m.propose(m.dev, changeScript)
	if id == "" {
		t.Fatal("no proposal")
	}
	var p AIProposal
	json.Unmarshal(m.v.expect(200, "GET", "/v1/ai/proposals/"+id, nil), &p)

	// An unreadable policy refuses the approval rather than applying blind.
	m.srv.docs.Put(ctx, kindSettings, idPolicy, "this is not a policy")
	m.admin2.expect(409, "POST", "/v1/ai/proposals/"+id+"/approve", map[string]any{"hash": p.Hash})
	if err := m.srv.docs.Delete(ctx, kindSettings, idPolicy); err != nil {
		t.Fatal(err)
	}
	m.admin.expect(200, "PUT", "/v1/integrations/aws/services", map[string]any{"off": []string{"ecs"}})

	// New scripts and proposals for ECS are refused before they reach the MCP server.
	if _, err := m.srv.runLookup(ctx, Principal{Name: "ada", Role: authn.Admin}, lookupCall{Tool: "aws_script", Args: json.RawMessage(`{"code":` + jsonString(readScript) + `}`)}); err == nil || !strings.Contains(err.Error(), "Containers (ECS) is turned off") {
		t.Errorf("read script: %v", err)
	}
	if _, err := m.srv.runLookup(ctx, polDev, lookupCall{Tool: toolProposeAWS, Args: json.RawMessage(`{"title":"t","reason":"r","code":` + jsonString(changeScript) + `}`)}); err == nil {
		t.Error("a proposal for a switched-off service was filed")
	}
	// The one filed earlier cannot be approved now, and nothing runs.
	m.admin2.expect(409, "POST", "/v1/ai/proposals/"+id+"/approve", map[string]any{"hash": p.Hash})
	if m.mcp.count("aws___run_script") != 0 {
		t.Fatalf("scripts run = %d", m.mcp.count("aws___run_script"))
	}
	// Turned back on, the same approval goes through.
	m.admin.expect(200, "PUT", "/v1/integrations/aws/services", map[string]any{"off": []string{}})
	m.admin2.expect(202, "POST", "/v1/ai/proposals/"+id+"/approve", map[string]any{"hash": p.Hash})
	if done := m.waitProposal(id, apApplying); done.Status != apApplied {
		t.Errorf("after turning ECS back on: %+v", done)
	}
}

func jsonString(s string) string { b, _ := json.Marshal(s); return string(b) }

func TestFindingsAboutAnOffServiceAreLeftOutOfWhatTheAIReads(t *testing.T) {
	m, _ := policyHarness(t)
	ctx := context.Background()
	for _, f := range []GuardFinding{
		{ID: "f1", Title: "Security group open to the internet", Severity: sevHigh, Account: "primary", AWSAccount: "111122223333", Service: "sg", Name: "web-sg", Status: "open"},
		{ID: "f2", Title: "Queue not encrypted", Severity: sevMedium, Account: "primary", AWSAccount: "111122223333", Service: "sqs", Name: "jobs", Status: "open"},
		{ID: "f3", Title: "Unpinned image", Severity: sevMedium, Account: "pipelines", Service: "sg", Name: "build", Status: "open"}, // not an AWS finding
	} {
		if err := m.srv.docs.Put(ctx, kindGuardFinding, f.ID, f); err != nil {
			t.Fatal(err)
		}
	}
	read := func() string {
		out, err := m.srv.runLookup(ctx, polDev, lookupCall{Tool: "guard_findings", Args: json.RawMessage(`{}`)})
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	if out := read(); !strings.Contains(out, "web-sg") || !strings.Contains(out, "jobs") {
		t.Fatalf("before: %s", out)
	}
	m.admin.expect(200, "PUT", "/v1/integrations/aws/services", map[string]any{"off": []string{"ec2"}})
	out := read()
	if strings.Contains(out, "web-sg") || !strings.Contains(out, "jobs") || !strings.Contains(out, "build") || !strings.Contains(out, "1 more are about services") {
		t.Errorf("with EC2 off: %s", out)
	}
}

func TestAnMcpClientCannotProposeAChangeToAnOffService(t *testing.T) {
	m := newMCPHarness(t)
	m.turnOn()
	dev := m.newToken(m.dev, map[string]any{"name": "d"})
	args := map[string]any{"kind": "instance-action", "instance_id": "i-0123456789abcdef0", "action": "stop", "reason": "It is idle over the weekend"}
	m.admin.expect(200, "PUT", "/v1/integrations/aws/services", map[string]any{"off": []string{"ec2"}})
	if out, isErr := m.tool(dev, toolProposeCloud, args); !isErr || !strings.Contains(out, "turned off for the AI") {
		t.Errorf("an instance change was proposed with EC2 off: %q", out)
	}
	var list struct{ Changes []CloudChange }
	json.Unmarshal(m.admin.expect(200, "GET", "/v1/cloud/changes", nil), &list)
	if len(list.Changes) != 0 {
		t.Errorf("a proposal was filed: %+v", list.Changes)
	}
	m.admin.expect(200, "PUT", "/v1/integrations/aws/services", map[string]any{"off": []string{"cloudformation"}}) // a different service
	if out, isErr := m.tool(dev, toolProposeCloud, args); isErr || !strings.Contains(out, "Nothing has run") {
		t.Errorf("with only stacks off, an instance change should be filed: %q", out)
	}
}
