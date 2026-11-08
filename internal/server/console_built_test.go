package server

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/forgelinehq/forgeline/internal/blueprint"
)

// A state the way OpenTofu writes it, with an attribute that must never leave the server.
const realState = `{"version":4,"resources":[
 {"mode":"data","type":"aws_vpc","name":"default","instances":[{"attributes":{"id":"vpc-1"}}]},
 {"mode":"managed","type":"aws_s3_bucket","name":"media","instances":[{"attributes":{"id":"assets-111122223333","arn":"arn:aws:s3:::assets-111122223333","bucket":"assets-111122223333"}}]},
 {"mode":"managed","type":"aws_s3_bucket","name":"gone","instances":[{"attributes":{"id":"deleted-by-hand","bucket":"deleted-by-hand"}}]},
 {"mode":"managed","type":"aws_iam_role","name":"app_task","instances":[{"attributes":{"id":"fl-x-app-task","arn":"arn:aws:iam::1:role/fl-x-app-task","name":"fl-x-app-task","password":"SECRET-VALUE"}}]},
 {"mode":"managed","type":"aws_ecs_cluster","name":"main","instances":[{"attributes":{"id":"arn:aws:ecs:x:1:cluster/fl","name":"fl"}}]}]}`

func TestStateResourcesOnlyNamesIdsAndARNs(t *testing.T) {
	res := stateResources(realState, []blueprint.Block{{ID: "media"}, {ID: "app"}})
	b, _ := json.Marshal(res)
	if strings.Contains(string(b), "SECRET-VALUE") || strings.Contains(string(b), "vpc-1") {
		t.Fatalf("leaked an attribute or a data source: %s", b)
	}
	if len(res) != 4 || res[0].Address != "aws_ecs_cluster.main" || res[0].Block != "" {
		t.Fatalf("resources = %+v", res)
	}
	byAddr := map[string]BuiltResource{}
	for _, r := range res {
		byAddr[r.Address] = r
	}
	if m := byAddr["aws_s3_bucket.media"]; m.Block != "media" || m.Name != "assets-111122223333" || m.ARN == "" {
		t.Errorf("bucket = %+v", m)
	}
	if r := byAddr["aws_iam_role.app_task"]; r.Block != "app" || r.Name != "fl-x-app-task" {
		t.Errorf("role = %+v", r)
	}
	if got := stateResources(`{"addresses":["aws_sqs_queue.jobs"]}`, nil); len(got) != 1 || got[0].Type != "aws_sqs_queue" {
		t.Errorf("stand-in state: %+v", got)
	}
}

func TestBuiltListsWhatForgelineMadeAndChecksIt(t *testing.T) {
	m := newMCPHarness(t)
	m.srv.run = (&awsStub{}).run
	m.admin.expect(200, "PUT", "/v1/integrations/aws", map[string]any{"enabled": true, "profile": "dev", "region": "ap-south-1"})
	var made BlueprintJSON
	json.Unmarshal(m.dev.expect(201, "POST", "/v1/blueprints", bpBody("Shop")), &made)
	sealed, err := m.srv.vault.Seal("blueprint-state/"+made.ID, realState)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	m.srv.docs.Put(t.Context(), kindBPState, made.ID, blueprintState{ID: made.ID, AWSAccount: "111122223333", Region: "ap-south-1", Serial: 2, Sealed: sealed, UpdatedAt: now, UpdatedBy: "grace"})
	later := now.Add(time.Minute)
	for _, c := range []CloudChange{
		{ID: "c1", Kind: kindBlueprintApply, Blueprint: made.ID, Status: stSucceeded, Title: "Build blueprint Shop", ProposedBy: "dev", DecidedBy: "grace", FinishedAt: &now, ProposedAt: now},
		{ID: "c2", Kind: kindStackDeploy, Stack: "demo", Status: stSucceeded, ProposedBy: "dev", DecidedBy: "grace", FinishedAt: &now, ProposedAt: now},
		{ID: "c3", Kind: kindStackDeploy, Stack: "old", Status: stSucceeded, FinishedAt: &now, ProposedAt: now},
		{ID: "c4", Kind: kindStackDelete, Stack: "old", Status: stSucceeded, FinishedAt: &later, ProposedAt: later},
		{ID: "c5", Kind: kindStackDeploy, Stack: "failed", Status: stFailed, FinishedAt: &now, ProposedAt: now},
	} {
		m.srv.docs.Put(t.Context(), kindCloud, c.ID, c)
	}
	m.srv.docs.Put(t.Context(), kindEnv, "e1", Environment{ID: "e1", Name: "qa", Stack: "env-qa", Status: envActive, Owner: "dev"})
	m.srv.docs.Put(t.Context(), kindEnv, "e2", Environment{ID: "e2", Name: "gone", Stack: "env-gone", Status: envDestroyed})

	m.v.expect(403, "GET", "/v1/built", nil)
	var b BuiltJSON
	json.Unmarshal(m.dev.expect(200, "GET", "/v1/built", nil), &b)
	if len(b.Blueprints) != 1 || b.Checked {
		t.Fatalf("built = %+v", b)
	}
	bp := b.Blueprints[0]
	if bp.Name != "Shop" || bp.Applies != 2 || len(bp.Resources) != 4 || len(bp.Blocks) != 3 || bp.Change == nil || bp.Change.ID != "c1" || bp.Change.DecidedBy != "grace" {
		t.Errorf("blueprint = %+v", bp)
	}
	if len(b.Stacks) != 1 || b.Stacks[0].Stack != "demo" || b.Stacks[0].Change.DecidedBy != "grace" {
		t.Errorf("stacks: a deleted or failed stack is not built: %+v", b.Stacks)
	}
	if len(b.Environments) != 1 || b.Environments[0].Name != "qa" {
		t.Errorf("environments = %+v", b.Environments)
	}
	if strings.Contains(string(m.dev.expect(200, "GET", "/v1/built", nil)), "SECRET-VALUE") {
		t.Fatal("the API leaked a state attribute")
	}

	// Checked against the account's inventory: the stub lists one bucket, so the other is missing, and kinds the
	// inventory does not list stay unchecked.
	b = BuiltJSON{}
	json.Unmarshal(m.dev.expect(200, "GET", "/v1/built?check=1", nil), &b)
	present := map[string]string{}
	for _, r := range b.Blueprints[0].Resources {
		v := "unchecked"
		if r.Present != nil {
			v = map[bool]string{true: "present", false: "missing"}[*r.Present]
		}
		present[r.Address] = v
	}
	if !b.Checked || present["aws_s3_bucket.media"] != "present" || present["aws_s3_bucket.gone"] != "missing" || present["aws_iam_role.app_task"] != "unchecked" {
		t.Errorf("check: %v", present)
	}
}

const builtWordCases = `
import { typeWords, byBlock, checkState, totals, resourceLabel } from './built.mjs';
let failed = 0;
const eq = (name, got, want) => { const g = JSON.stringify(got), w = JSON.stringify(want); if (g !== w) { failed++; console.error('FAIL ' + name + '\n  got  ' + g + '\n  want ' + w); } };
eq('types in words', [typeWords('aws_sqs_queue'), typeWords('aws_iam_role'), typeWords('aws_kinesis_stream')], ['SQS queue', 'IAM role', 'kinesis stream']);
const bp = { resources: [{ address: 'a', block: 'web' }, { address: 'b', block: 'web' }, { address: 'c' }] };
eq('grouped by block, shared under empty', Object.fromEntries(Object.entries(byBlock(bp)).map(([k, v]) => [k, v.length])), { web: 2, '': 1 });
eq('nothing checked', checkState([{}, { present: null }]), 'unchecked');
eq('all there', checkState([{ present: true }, {}]), 'present');
eq('one missing', checkState([{ present: true }, { present: false }]), 'missing');
eq('totals', totals({ blueprints: [{ resources: [{ present: false }, {}, { present: true }] }], stacks: [{}], environments: [] }), { blueprints: 1, resources: 3, stacks: 1, environments: 0, missing: 1, checked: 2 });
eq('label', [resourceLabel({ name: 'n', id: 'i', address: 'a' }), resourceLabel({ id: 'i', address: 'a' }), resourceLabel({ address: 'a' })], ['n', 'i', 'a']);
if (failed) process.exit(1);
`

func TestConsoleBuiltWords(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed")
	}
	tmp := t.TempDir()
	src, _ := uiFiles.ReadFile("ui/lib/built.js")
	os.WriteFile(filepath.Join(tmp, "built.mjs"), src, 0o600)
	test := filepath.Join(tmp, "cases.mjs")
	os.WriteFile(test, []byte(builtWordCases), 0o600)
	if out, err := exec.Command(node, test).CombinedOutput(); err != nil {
		t.Fatalf("built words failed:\n%s", out)
	}
}
