package server

import (
	"encoding/json"
	"strings"
	"testing"
)

// The Services page lists the services before reading any of them, and each tile reads only its own
// service, so a page for one service never waits for the whole account.

func TestServicesListNeedsNoAWSRead(t *testing.T) {
	m, st := accountHarness(t)
	before := len(st.calls)
	var l struct {
		Enabled  bool                 `json:"enabled"`
		Region   string               `json:"region"`
		Services []AWSServiceInfoJSON `json:"services"`
	}
	json.Unmarshal(m.v.expect(200, "GET", "/v1/infra/aws/services", nil), &l)
	if !l.Enabled || l.Region != "ap-south-1" || len(l.Services) < 10 {
		t.Fatalf("services = %+v", l)
	}
	ids := map[string]AWSServiceInfoJSON{}
	for _, s := range l.Services {
		ids[s.ID] = s
	}
	if ids["ecs-services"].Label != "ECS services" || ids["ec2"].Category != "Compute" || !ids["s3"].Global {
		t.Errorf("ids = %+v", ids)
	}
	if len(st.calls) != before {
		t.Errorf("listing the services should not call AWS, got %v", st.calls[before:])
	}
	m.v.expect(404, "GET", "/v1/infra/aws/services?account=ghost", nil)
}

func TestServiceReadCallsOnlyThatService(t *testing.T) {
	m, st := accountHarness(t)
	var inv AWSInventoryJSON
	json.Unmarshal(m.v.expect(200, "GET", "/v1/infra/aws/resources?service=s3", nil), &inv)
	if len(inv.Services) != 1 || inv.Services[0].ID != "s3" || inv.Services[0].Resources[0].Name != "bucket-of-111122223333" {
		t.Fatalf("s3 only: %+v", inv.Services)
	}
	if st.called("dev", "s3api list-buckets") != 1 || st.called("dev", "ec2 describe-instances") != 0 || st.called("dev", "lambda list-functions") != 0 {
		t.Errorf("only S3 should have been read: %v", st.calls)
	}
	// Asked again, it answers from its own cache entry.
	m.v.expect(200, "GET", "/v1/infra/aws/resources?service=s3", nil)
	if st.called("dev", "s3api list-buckets") != 1 {
		t.Errorf("S3 was read again: %v", st.calls)
	}
	// ECS services share the ECS collector but come back on their own.
	json.Unmarshal(m.v.expect(200, "GET", "/v1/infra/aws/resources?service=ecs-services", nil), &inv)
	if len(inv.Services) != 1 || inv.Services[0].ID != "ecs-services" {
		t.Errorf("ecs-services: %+v", inv.Services)
	}
	if msg := string(m.v.expect(404, "GET", "/v1/infra/aws/resources?service=ghost", nil)); !strings.Contains(msg, "no such service") {
		t.Errorf("unknown service: %s", msg)
	}
	// Refresh reads that one service again, nothing else.
	m.v.expect(200, "GET", "/v1/infra/aws/resources?service=s3&refresh=1", nil)
	if st.called("dev", "s3api list-buckets") != 2 || st.called("dev", "ec2 describe-instances") != 0 {
		t.Errorf("refresh: %v", st.calls)
	}
}

func TestServiceReadUsesTheWholeInventoryWhenItWasRead(t *testing.T) {
	m, st := accountHarness(t)
	m.v.expect(200, "GET", "/v1/infra/aws/resources", nil)
	n := st.called("dev", "s3api list-buckets")
	var inv AWSInventoryJSON
	json.Unmarshal(m.v.expect(200, "GET", "/v1/infra/aws/resources?service=s3", nil), &inv)
	if len(inv.Services) != 1 || inv.Services[0].ID != "s3" || inv.Services[0].Count != 1 {
		t.Fatalf("s3 from the inventory: %+v", inv.Services)
	}
	if st.called("dev", "s3api list-buckets") != n {
		t.Errorf("S3 was read again although the inventory was fresh: %v", st.calls)
	}
	// A different account's service is its own read.
	addAccount(m.admin, 201, "Staging", "staging", "staging")
	json.Unmarshal(m.v.expect(200, "GET", "/v1/infra/aws/resources?service=s3&account=staging", nil), &inv)
	if inv.Services[0].Resources[0].Name != "bucket-of-444455556666" || st.called("staging", "ec2 describe-instances") != 0 {
		t.Errorf("staging s3: %+v, calls %v", inv.Services, st.calls)
	}
}
