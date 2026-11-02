package server

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestAlarmsAreListedWorstFirstWithTheirDimensions(t *testing.T) {
	m := newMCPHarness(t)
	stub := &awsStub{}
	m.srv.run = stub.run
	m.admin.expect(200, "PUT", "/v1/integrations/aws", map[string]any{"enabled": true, "profile": "dev", "region": "ap-south-1"})
	var d AlarmsJSON
	if err := json.Unmarshal(m.v.expect(200, "GET", "/v1/infra/aws/alarms", nil), &d); err != nil {
		t.Fatal(err)
	}
	if !d.Enabled || d.Error != "" || d.Count != 3 || len(d.Alarms) != 3 {
		t.Fatalf("view = %+v", d)
	}
	var names []string
	for _, a := range d.Alarms {
		names = append(names, a.Name)
	}
	if got := strings.Join(names, ","); got != "a-hot,c-nodata,b-ok" {
		t.Errorf("order = %s, want ALARM, then no data, then OK", got)
	}
	hot := d.Alarms[0]
	if hot.State != "ALARM" || hot.Namespace != "AWS/RDS" || len(hot.Dimensions) != 1 || hot.Dimensions[0].Value != "orders-db" || hot.Updated == "" {
		t.Errorf("alarm = %+v", hot)
	}
}

func TestAlarmsDeniedNamesThePermission(t *testing.T) {
	m := newMCPHarness(t)
	stub := &awsStub{deny: map[string]bool{"cloudwatch describe-alarms": true}}
	m.srv.run = stub.run
	m.admin.expect(200, "PUT", "/v1/integrations/aws", map[string]any{"enabled": true, "profile": "dev", "region": "ap-south-1"})
	var d AlarmsJSON
	json.Unmarshal(m.v.expect(200, "GET", "/v1/infra/aws/alarms", nil), &d)
	if !strings.Contains(d.Error, "cloudwatch:DescribeAlarms") {
		t.Errorf("a denied role must be told which permission is missing, got %q", d.Error)
	}
	if d.Alarms == nil || len(d.Alarms) != 0 {
		t.Errorf("alarms must be an empty list, not null: %+v", d.Alarms)
	}
}

func TestAlarmsOnlyReadAndNotConnected(t *testing.T) {
	m := newMCPHarness(t)
	stub := &awsStub{}
	m.srv.run = stub.run
	var off AlarmsJSON
	json.Unmarshal(m.v.expect(200, "GET", "/v1/infra/aws/alarms", nil), &off)
	if off.Enabled || stub.count("cloudwatch") != 0 {
		t.Errorf("not connected must not call AWS: %+v", off)
	}
	m.admin.expect(200, "PUT", "/v1/integrations/aws", map[string]any{"enabled": true, "profile": "dev", "region": "ap-south-1"})
	m.v.expect(200, "GET", "/v1/infra/aws/alarms", nil)
	for _, c := range stub.calls {
		if strings.HasPrefix(c, "cloudwatch") && c != "cloudwatch describe-alarms" {
			t.Errorf("ran %q; only describe-alarms is a read we allow here", c)
		}
	}
	if stub.count("cloudwatch describe-alarms") != 1 {
		t.Errorf("want one describe-alarms call, calls = %v", stub.calls)
	}
}
