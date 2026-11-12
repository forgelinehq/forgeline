package server

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Runs and scans are listed newest first and pruned oldest first, whatever their ids: the ids used to be
// random, so the store's order was random, the Team page's "last mission" was a random run and pruning
// deleted random runs.

func TestWorkflowRunsListNewestFirstWhateverTheirIDs(t *testing.T) {
	m := newMCPHarness(t)
	ctx := context.Background()
	now := time.Now().UTC()
	// An id that sorts last in the store but started first, and one that sorts first but started last.
	old := WFRun{ID: "ffffffffffffffffffffffffffffffff", Workflow: "wf", WorkflowName: "Wf", Status: wfSucceeded, StartedAt: now.Add(-2 * time.Hour), By: "ada"}
	fin := now.Add(-90 * time.Minute)
	old.FinishedAt = &fin
	newer := WFRun{ID: "00000000000000000000000000000000", Workflow: "wf", WorkflowName: "Wf", Status: wfFailed, StartedAt: now.Add(-time.Minute), By: "ada"}
	fin2 := now
	newer.FinishedAt = &fin2
	for _, r := range []WFRun{old, newer} {
		if err := m.srv.docs.Put(ctx, kindWorkflowRun, r.ID, r); err != nil {
			t.Fatal(err)
		}
	}
	var l struct{ Runs []WFRun }
	json.Unmarshal(m.admin.expect(200, "GET", "/v1/ai/workflow-runs", nil), &l)
	if len(l.Runs) != 2 || l.Runs[0].ID != newer.ID {
		t.Errorf("runs should be newest first, got %v", ids(l.Runs))
	}
	var team TeamJSON
	json.Unmarshal(m.admin.expect(200, "GET", "/v1/ai/team", nil), &team)
	if team.Mission == nil || team.Mission.ID != newer.ID {
		t.Errorf("the team's last mission should be the newest run, got %+v", team.Mission)
	}
}

func TestWorkflowRunIDsFollowTime(t *testing.T) {
	a := timeID()
	time.Sleep(2 * time.Millisecond)
	b := timeID()
	if !(a < b) || len(a) != 28 || strings.Trim(a, "0123456789abcdef") != "" {
		t.Errorf("ids should be hex and ordered by time: %q then %q", a, b)
	}
}

func TestWorkflowPruneKeepsTheNewestRuns(t *testing.T) {
	m := newMCPHarness(t)
	ctx := context.Background()
	now := time.Now().UTC()
	// Random-looking ids whose store order is the reverse of their time order.
	for i := 0; i < maxWFRuns+5; i++ {
		id := strings.Repeat("f", 24) + pad8(maxWFRuns+5-i) // the newest run has the lowest id
		fin := now.Add(-time.Duration(maxWFRuns+5-i) * time.Minute)
		r := WFRun{ID: id, Workflow: "wf", Status: wfSucceeded, StartedAt: fin.Add(-time.Minute), FinishedAt: &fin}
		if err := m.srv.docs.Put(ctx, kindWorkflowRun, id, r); err != nil {
			t.Fatal(err)
		}
	}
	m.srv.pruneRuns(ctx)
	var l struct{ Runs []WFRun }
	json.Unmarshal(m.admin.expect(200, "GET", "/v1/ai/workflow-runs", nil), &l)
	if len(l.Runs) != maxWFRuns {
		t.Fatalf("kept %d runs, want %d", len(l.Runs), maxWFRuns)
	}
	// The newest (started 1 minute ago, id ...00000001) must survive; the oldest must go.
	if l.Runs[0].ID != strings.Repeat("f", 24)+pad8(1) {
		t.Errorf("the newest run was pruned; first kept is %s", l.Runs[0].ID)
	}
	for _, r := range l.Runs {
		if r.ID == strings.Repeat("f", 24)+pad8(maxWFRuns+5) {
			t.Error("the oldest run should have been pruned")
		}
	}
}

func TestGuardScansListNewestFirstWhateverTheirIDs(t *testing.T) {
	m := newMCPHarness(t)
	ctx := context.Background()
	now := time.Now().UTC()
	m.srv.docs.Put(ctx, kindGuardScan, "ffff", GuardScan{ID: "ffff", Status: "done", StartedAt: now.Add(-2 * time.Hour)})
	m.srv.docs.Put(ctx, kindGuardScan, "0000", GuardScan{ID: "0000", Status: "done", StartedAt: now.Add(-time.Minute)})
	var l struct{ Scans []GuardScan }
	json.Unmarshal(m.admin.expect(200, "GET", "/v1/guard/scans", nil), &l)
	if len(l.Scans) != 2 || l.Scans[0].ID != "0000" {
		t.Errorf("scans should be newest first, got %+v", l.Scans)
	}
}

func ids(runs []WFRun) []string {
	out := make([]string, len(runs))
	for i, r := range runs {
		out[i] = r.ID
	}
	return out
}

func pad8(n int) string {
	s := strings.Repeat("0", 8) + strconv.Itoa(n)
	return s[len(s)-8:]
}

// Cloud changes and AI proposals have random ids: lists must still be newest first, and a limit must keep the newest.
func TestChangesAndProposalsListNewestFirst(t *testing.T) {
	m := newMCPHarness(t)
	ctx := context.Background()
	now := time.Now().UTC()
	for i, id := range []string{"zzzz-old", "mmmm-new", "aaaa-mid"} {
		at := map[string]time.Time{"zzzz-old": now.Add(-3 * time.Hour), "mmmm-new": now.Add(-time.Minute), "aaaa-mid": now.Add(-time.Hour)}[id] // neither id order is the time order
		_ = i
		if err := m.srv.docs.Put(ctx, kindCloud, id, CloudChange{ID: id, Kind: kindStackDeploy, Status: stSucceeded, ProposedAt: at, Preview: []ChangePreview{}}); err != nil {
			t.Fatal(err)
		}
		if err := m.srv.docs.Put(ctx, kindAIProposal, id, AIProposal{ID: id, Status: apApplied, ProposedAt: at}); err != nil {
			t.Fatal(err)
		}
	}
	var cl struct{ Changes []CloudChange }
	json.Unmarshal(m.admin.expect(200, "GET", "/v1/cloud/changes", nil), &cl)
	var got []string
	for _, c := range cl.Changes {
		got = append(got, c.ID)
	}
	if strings.Join(got, ",") != "mmmm-new,aaaa-mid,zzzz-old" {
		t.Errorf("changes order = %v", got)
	}
	if two, _ := m.srv.loadChanges(ctx, 2); len(two) != 2 || two[0].ID != "mmmm-new" || two[1].ID != "aaaa-mid" {
		t.Errorf("a limit must keep the newest: %+v", two)
	}
	var pl struct{ Proposals []AIProposal }
	json.Unmarshal(m.admin.expect(200, "GET", "/v1/ai/proposals", nil), &pl)
	got = nil
	for _, p := range pl.Proposals {
		got = append(got, p.ID)
	}
	if strings.Join(got, ",") != "mmmm-new,aaaa-mid,zzzz-old" {
		t.Errorf("proposals order = %v", got)
	}
}

// When tofu fails, its error output comes first and is never cut off by a long plan listing.
func TestTofuFailureShowsTheErrorFirst(t *testing.T) {
	plan := strings.Repeat("  + resource listing line\n", 400)
	got := tofuFailure("Error: reading IAM OIDC Provider: AccessDenied", []byte(plan))
	if !strings.HasPrefix(got, "Error: reading IAM OIDC Provider") || len(got) > 3200 {
		t.Errorf("the error must lead and the text stay short: %.120q (%d)", got, len(got))
	}
	if got := tofuFailure("", []byte("only output\nError: in stdout")); !strings.Contains(got, "Error: in stdout") {
		t.Errorf("without stderr the end of stdout is shown: %q", got)
	}
}
