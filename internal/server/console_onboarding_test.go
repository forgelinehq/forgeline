package server

import (
	"encoding/json"
	"strings"
	"testing"
)

func stepsByID(t *testing.T, b *browser) (OnboardingJSON, map[string]OnboardingStep) {
	t.Helper()
	var o OnboardingJSON
	json.Unmarshal(b.expect(200, "GET", "/v1/onboarding", nil), &o)
	by := map[string]OnboardingStep{}
	for _, s := range o.Steps {
		by[s.ID] = s
	}
	return o, by
}

func TestOnboardingStepsFollowWhatIsSetUp(t *testing.T) {
	m := newMCPHarness(t)
	m.srv.run = (&awsStub{}).run
	m.v.expect(403, "GET", "/v1/onboarding", nil)

	o, by := stepsByID(t, m.dev)
	if len(o.Steps) != 7 || o.Steps[0].ID != "admin" || o.Finished {
		t.Fatalf("steps = %+v", o)
	}
	// The harness already has several people and the assistant on; nothing else is set up.
	for id, want := range map[string]bool{"admin": true, "people": true, "assistant": true, "github": false, "cloud": false, "ai-access": false, "first": false} {
		if by[id].Done != want {
			t.Errorf("%s done = %v, want %v (%s)", id, by[id].Done, want, by[id].Detail)
		}
	}
	if by["assistant"].Link == "" || by["cloud"].Why == "" {
		t.Errorf("a step says where and why: %+v", by["cloud"])
	}

	// Each step turns done when its thing exists.
	m.admin.expect(200, "PUT", "/v1/integrations/aws", map[string]any{"enabled": true, "profile": "dev", "region": "ap-south-1"})
	m.admin.expect(200, "PUT", "/v1/integrations/aws/services", map[string]any{"off": []string{"rds"}, "read_only": []string{}})
	m.dev.expect(201, "POST", "/v1/blueprints", bpBody("Shop"))
	_, by = stepsByID(t, m.dev)
	if !by["cloud"].Done || !strings.Contains(by["cloud"].Detail, "AWS") || !by["ai-access"].Done || !by["first"].Done {
		t.Errorf("after setting up: %+v", by)
	}

	// Skipping is for administrators, audited, and finishes the list; a skipped step can come back.
	m.dev.expect(403, "PUT", "/v1/onboarding/github", map[string]any{"skip": true})
	m.admin.expect(404, "PUT", "/v1/onboarding/nope", map[string]any{"skip": true})
	var after OnboardingJSON
	json.Unmarshal(m.admin.expect(200, "PUT", "/v1/onboarding/github", map[string]any{"skip": true}), &after)
	if !after.Finished || after.Done != 6 {
		t.Errorf("after skipping GitHub: %+v", after)
	}
	json.Unmarshal(m.admin.expect(200, "PUT", "/v1/onboarding/github", map[string]any{"skip": false}), &after)
	if after.Finished {
		t.Errorf("bringing a step back reopens the list")
	}
	// A step that is done is never shown as skipped.
	m.admin.expect(200, "PUT", "/v1/onboarding/cloud", map[string]any{"skip": true})
	if _, by = stepsByID(t, m.dev); by["cloud"].Skipped || !by["cloud"].Done {
		t.Errorf("done and skipped: %+v", by["cloud"])
	}
	if a := string(m.admin.expect(200, "GET", "/v1/audit?action=onboarding", nil)); !strings.Contains(a, "onboarding.skip") {
		t.Errorf("not audited")
	}
}
