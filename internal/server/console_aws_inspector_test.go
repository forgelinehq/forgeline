package server

import (
	"context"
	"encoding/json"
	"github.com/forgelinehq/forgeline/internal/authn"
	"strings"
	"testing"
)

const inspectorJSON = `{"findings": [
 {"severity": "MEDIUM", "title": "CVE-2026-91418 - openjdk17", "type": "PACKAGE_VULNERABILITY", "fixAvailable": "YES", "exploitAvailable": "NO",
  "epss": {"score": 0.0123}, "inspectorScore": 5.5,
  "resources": [{"type": "AWS_ECR_CONTAINER_IMAGE", "id": "arn:aws:ecr:us-west-1:1:repository/submission-qa-auto/sha256:abc",
    "details": {"awsEcrContainerImage": {"repositoryName": "submission-qa-auto", "imageTags": ["latest"]}}}],
  "packageVulnerabilityDetails": {"vulnerabilityId": "CVE-2026-91418", "vulnerablePackages": [{"name": "openjdk17", "version": "17.0.20", "fixedInVersion": "17.0.21.0.3_p1-r0"}]}},
 {"severity": "CRITICAL", "title": "Port 22 is reachable from the internet", "type": "NETWORK_REACHABILITY", "exploitAvailable": "YES",
  "resources": [{"type": "AWS_EC2_INSTANCE", "id": "arn:aws:ec2:us-west-1:1:instance/i-0abc"}]}
], "NextToken": "more"}`

// Amazon Inspector findings for agents: read-only, worst first, counted, with what a report needs; open to viewers,
// so a workflow running as a viewer can read them.
func TestInspectorFindingsLookup(t *testing.T) {
	m := newMCPHarness(t)
	m.turnOn()
	var calls [][]string
	m.srv.run = func(_ context.Context, name string, args ...string) ([]byte, string, error) {
		calls = append(calls, args)
		if len(args) > 0 && strings.Contains(strings.Join(args, " "), "inspector2 list-findings") {
			return []byte(inspectorJSON), "", nil
		}
		return []byte(`{"Account":"111122223333","Arn":"arn:aws:sts::111122223333:assumed-role/A/x","UserId":"x"}`), "", nil
	}
	viewer := Principal{Name: "view", Role: authn.Viewer}
	out, err := m.srv.runLookup(context.WithValue(context.Background(), principalKey{}, viewer), viewer, lookupCall{Tool: "inspector_findings", Args: json.RawMessage(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"2 active findings (the first 2; there are more)", "critical 1, medium 1", "ecr container image 1",
		"- [CRITICAL] Port 22 is reachable from the internet on ec2 instance i-0abc · known exploit: yes",
		"- [MEDIUM] CVE-2026-91418 in openjdk17 17.0.20 (fixed in 17.0.21.0.3_p1-r0) on ECR image submission-qa-auto:latest · fix available: yes · known exploit: no · EPSS 0.012 · score 5.5"} {
		if !strings.Contains(out, want) {
			t.Errorf("lookup lacks %q:\n%s", want, out)
		}
	}
	if strings.Index(out, "[CRITICAL]") > strings.Index(out, "[MEDIUM]") {
		t.Error("not worst first")
	}
	last := strings.Join(calls[len(calls)-1], " ")
	if !strings.Contains(last, `"findingStatus":[{"comparison":"EQUALS","value":"ACTIVE"}]`) || !strings.Contains(last, "--max-items 1000") {
		t.Errorf("call: %s", last)
	}
	if _, err := m.srv.lookupInspectorFindings(context.Background(), json.RawMessage(`{"severity":"severe"}`)); err == nil {
		t.Error("a made-up severity was accepted")
	}
	m.srv.lookupInspectorFindings(context.Background(), json.RawMessage(`{"severity":"high"}`))
	if !strings.Contains(strings.Join(calls[len(calls)-1], " "), `"severity":[{"comparison":"EQUALS","value":"HIGH"}]`) {
		t.Errorf("severity filter not sent: %v", calls[len(calls)-1])
	}
	// Turned off for the AI with the other AWS services: refused, and AWS is not asked.
	m.admin.expect(200, "PUT", "/v1/integrations/aws/services", map[string]any{"off": []string{svcOther}})
	n := len(calls)
	if _, err := m.srv.lookupInspectorFindings(context.Background(), json.RawMessage(`{}`)); err == nil || len(calls) != n {
		t.Errorf("off for the AI: err %v, calls %d → %d", err, n, len(calls))
	}
}
