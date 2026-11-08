package server

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestLinksComeFromStacksAndEventSources(t *testing.T) {
	m := newMCPHarness(t)
	stub := &awsStub{stacks: true}
	m.srv.run = stub.run
	var off LinksJSON
	json.Unmarshal(m.v.expect(200, "GET", "/v1/infra/aws/links", nil), &off)
	if off.Enabled || len(stub.calls) != 0 || off.Stacks == nil || off.Links == nil {
		t.Fatalf("not connected must not call AWS and must give empty lists: %+v %v", off, stub.calls)
	}
	m.admin.expect(200, "PUT", "/v1/integrations/aws", map[string]any{"enabled": true, "profile": "dev", "region": "ap-south-1"})
	var d LinksJSON
	if err := json.Unmarshal(m.v.expect(200, "GET", "/v1/infra/aws/links", nil), &d); err != nil {
		t.Fatal(err)
	}
	// The deleted stack is not read; the other two are.
	byName := map[string]StackPartsJSON{}
	for _, st := range d.Stacks {
		byName[st.Name] = st
	}
	if len(d.Stacks) != 2 || stub.count("cloudformation list-stack-resources --stack-name gone") != 0 {
		t.Fatalf("stacks = %+v", d.Stacks)
	}
	shop, locked := byName["shop"], byName["locked"]
	if shop.Error != "" || len(shop.Parts) != 2 || shop.Parts[0].Logical != "Orders" || shop.Parts[0].Physical != "orders" || shop.Parts[0].Status != "DELETE_SKIPPED" || shop.Parts[1].Physical != "shop-worker" {
		t.Errorf("a deleted part must be left out, the rest kept in order: %+v", shop.Parts)
	}
	if !strings.Contains(locked.Error, "cloudformation:ListStackResources") || locked.Parts == nil {
		t.Errorf("a refused stack names the permission: %+v", locked)
	}
	want := []LinkJSON{
		{From: LinkEndJSON{"lambda", "shop-worker"}, To: LinkEndJSON{"sqs", "jobs"}, Kind: "event-source", State: "Enabled"},
		{From: LinkEndJSON{"lambda", "shop-worker"}, To: LinkEndJSON{"dynamodb", "orders"}, Kind: "event-source", State: "Disabled"},
		{From: LinkEndJSON{"lambda", "feed"}, To: LinkEndJSON{"", "arn:aws:kinesis:ap-south-1:111122223333:stream/clicks"}, Kind: "event-source", State: "Enabled"},
		// Not a table's stream, so it is not named as a table; a layer ARN is not a function.
		{From: LinkEndJSON{"lambda", "feed"}, To: LinkEndJSON{"", "arn:aws:dynamodb:ap-south-1:111122223333:global-table/clicks"}, Kind: "event-source", State: "Enabled"},
	}
	if len(d.Links) != len(want) {
		t.Fatalf("links = %+v", d.Links)
	}
	for i := range want {
		if d.Links[i] != want[i] {
			t.Errorf("link %d = %+v, want %+v", i, d.Links[i], want[i])
		}
	}
	// Only reads, and a second look within the minute costs AWS nothing.
	n := len(stub.calls)
	m.v.expect(200, "GET", "/v1/infra/aws/links", nil)
	if len(stub.calls) != n {
		t.Errorf("a second look read AWS again: %v", stub.calls[n:])
	}
	for _, c := range stub.calls {
		verb := strings.Fields(c)[1]
		if !strings.HasPrefix(verb, "list-") && !strings.HasPrefix(verb, "describe-") && !strings.HasPrefix(verb, "get-") {
			t.Errorf("ran %q; the map only reads", c)
		}
	}
}

func TestLinksDeniedNamesThePermission(t *testing.T) {
	m := newMCPHarness(t)
	stub := &awsStub{deny: map[string]bool{"lambda list-event-source-mappings": true}}
	m.srv.run = stub.run
	m.admin.expect(200, "PUT", "/v1/integrations/aws", map[string]any{"enabled": true, "profile": "dev", "region": "ap-south-1"})
	var d LinksJSON
	json.Unmarshal(m.v.expect(200, "GET", "/v1/infra/aws/links", nil), &d)
	if !strings.Contains(d.LinksError, "lambda:ListEventSourceMappings") || d.Links == nil || len(d.Links) != 0 {
		t.Errorf("links error = %q, links = %+v", d.LinksError, d.Links)
	}
	m.v.expect(404, "GET", "/v1/infra/aws/links?account=nope", nil)
}

func TestLinksReadAtMostTwentyFiveStacks(t *testing.T) {
	m := newMCPHarness(t)
	stub := &awsStub{}
	m.srv.run = stub.run
	var names []string
	for i := range 30 {
		names = append(names, "s"+string(rune('a'+i%26))+string(rune('a'+i/26)))
	}
	d := m.srv.fetchLinks(t.Context(), awsSettings{Enabled: true, Profile: "dev", Region: "ap-south-1"}, names)
	if len(d.Stacks) != linksMaxStacks || d.StacksSkipped != 5 || stub.count("cloudformation list-stack-resources") != linksMaxStacks {
		t.Errorf("stacks = %d, skipped = %d, calls = %d", len(d.Stacks), d.StacksSkipped, stub.count("cloudformation list-stack-resources"))
	}
}
