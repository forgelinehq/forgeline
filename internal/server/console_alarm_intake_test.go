package server

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

const alarmQueue = "https://sqs.us-east-1.amazonaws.com/111122223333/forgeline-alarms"

// fakeSQS is a queue behind the AWS CLI: messages go in, receive hands out up to ten once, delete removes, and every call is kept.
type fakeSQS struct {
	mu       sync.Mutex
	messages []map[string]string
	deleted  []string
	calls    [][]string
	failing  bool
}

func (q *fakeSQS) put(id, body string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.messages = append(q.messages, map[string]string{"MessageId": id, "ReceiptHandle": "rh-" + id, "Body": body})
}

func (q *fakeSQS) run(ctx context.Context, name string, args ...string) ([]byte, string, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.calls = append(q.calls, args)
	joined := " " + strings.Join(args, " ") + " "
	switch {
	case q.failing:
		return nil, "An error occurred (AccessDenied) when calling the ReceiveMessage operation", fmt.Errorf("exit status 254")
	case strings.Contains(joined, " receive-message "):
		n := min(len(q.messages), 10)
		batch := q.messages[:n]
		q.messages = q.messages[n:]
		if len(batch) == 0 {
			return nil, "", nil
		}
		b, _ := json.Marshal(map[string]any{"Messages": batch})
		return b, "", nil
	case strings.Contains(joined, " delete-message "):
		for i, a := range args {
			if a == "--receipt-handle" {
				q.deleted = append(q.deleted, strings.TrimPrefix(args[i+1], "rh-"))
			}
		}
		return nil, "", nil
	case strings.Contains(joined, " get-queue-attributes "):
		return []byte(`{"Attributes":{"QueueArn":"arn:aws:sqs:us-east-1:111122223333:forgeline-alarms","ApproximateNumberOfMessages":"3"}}`), "", nil
	}
	return []byte(`{"Account":"111122223333","Arn":"arn:aws:sts::111122223333:assumed-role/Admin/ada","UserId":"AROA:ada"}`), "", nil
}

func (q *fakeSQS) deletedIDs() []string {
	q.mu.Lock()
	defer q.mu.Unlock()
	return append([]string(nil), q.deleted...)
}

func alarmBody(name, state, account string) string {
	inner, _ := json.Marshal(map[string]any{"AlarmName": name, "NewStateValue": state, "NewStateReason": "Threshold Crossed: 1 datapoint [91.2] was greater than the threshold (85.0).",
		"AlarmDescription": "disk on jenkins-prod", "AWSAccountId": account, "Region": "US East (N. Virginia)", "StateChangeTime": "2026-11-26T01:02:03.000+0000",
		"Trigger": map[string]any{"Namespace": "Forgeline/Test", "MetricName": "DiskUsedPercent", "Dimensions": []map[string]string{{"name": "InstanceId", "value": "i-0abc"}}}})
	env, _ := json.Marshal(map[string]any{"Type": "Notification", "MessageId": "sns-1", "Message": string(inner)})
	return string(env)
}

func alarmHarness(t *testing.T) (*mcpHarness, *fakeSQS, Workflow) {
	t.Helper()
	old := alarmPause
	alarmPause = 5 * time.Millisecond
	t.Cleanup(func() { alarmPause = old })
	m := newMCPHarness(t)
	q := &fakeSQS{}
	m.srv.run = q.run
	m.admin.expect(200, "PUT", "/v1/integrations/aws", map[string]any{"enabled": true, "profile": "dev", "region": "ap-south-1"})
	a := newAgent(t, m.admin, agentBody("Responder", nil, nil, "T0"))
	n := agentNode("look", a.ID, "what happened?")
	n.Reads = []string{"trigger"}
	b := graphBody("Disk watch", []WFNode{{ID: "start", Kind: nodeStart}, n, {ID: "end", Kind: nodeEnd}}, []WFEdge{wire("start", "look"), wire("look", "end")})
	b["on_alarm"] = []string{"disk-*", "exact-alarm"}
	w := newWorkflow(t, m.admin, b)
	return m, q, w
}

func (m *mcpHarness) alarmRuns(wf string) []WFRun {
	var out struct {
		Runs []WFRun `json:"runs"`
	}
	json.Unmarshal(m.dev.expect(200, "GET", "/v1/ai/workflow-runs?workflow="+wf, nil), &out)
	var mine []WFRun
	for _, r := range out.Runs {
		if r.Trigger == trigAlarm {
			mine = append(mine, r)
		}
	}
	return mine
}

// An alarm that goes to ALARM starts the workflows that name it, with its details as data; everything else in the queue is read and
// deleted without starting anything, and a message is acted on once.
func TestAnAlarmInTheQueueStartsTheWorkflowThatFollowsIt(t *testing.T) {
	m, q, w := alarmHarness(t)
	m.llm.script("Disk is at 91 percent on jenkins-prod.")
	var st struct {
		Region, Account string
		AWSConnected    bool `json:"aws_connected"`
	}
	m.admin.expect(400, "PUT", "/v1/settings/alarm-intake", map[string]any{"enabled": true})                                           // no queue
	m.admin.expect(400, "PUT", "/v1/settings/alarm-intake", map[string]any{"enabled": true, "queue_url": "https://example.com/queue"}) // not SQS
	m.dev.expect(403, "PUT", "/v1/settings/alarm-intake", map[string]any{"enabled": true, "queue_url": alarmQueue})
	q.put("m-ok-state", alarmBody("disk-recovered", "OK", "111122223333"))                             // recovered: nothing to start (first, so it would start first)
	q.put("m-ok", alarmBody("disk-jenkins-prod", "ALARM", "111122223333"))                             // recovered: nothing to start
	q.put("m-other-name", alarmBody("cpu-high", "ALARM", "111122223333"))                              // no workflow follows it
	q.put("m-other-account", alarmBody("disk-evil", "ALARM", "999999999999"))                          // not the queue's own account
	q.put("m-junk", `{"hello": "world"}`)                                                              // not an alarm
	q.put("m-confirm", `{"Type":"SubscriptionConfirmation","Message":"You have chosen to subscribe"}`) // an SNS handshake
	json.Unmarshal(m.admin.expect(200, "PUT", "/v1/settings/alarm-intake", map[string]any{"enabled": true, "queue_url": alarmQueue}), &st)
	if st.Region != "us-east-1" || st.Account != "111122223333" || !st.AWSConnected {
		t.Errorf("settings: %+v", st)
	}
	m.waitFor("the messages to be read and deleted", func() bool { return len(q.deletedIDs()) == 6 })
	runs := m.alarmRuns(w.ID)
	if len(runs) != 1 {
		t.Fatalf("started %d runs, want 1: %+v", len(runs), runs)
	}
	run := m.waitRun(runs[0].ID, wfSucceeded, wfFailed)
	if run.Status != wfSucceeded || run.By != "workflow:"+w.ID {
		t.Fatalf("run = %+v", run)
	}
	var trig map[string]any
	json.Unmarshal([]byte(run.State["trigger"]), &trig)
	dims, _ := trig["dimensions"].(map[string]any)
	if trig["alarm"] != "disk-jenkins-prod" || trig["state"] != "ALARM" || dims["InstanceId"] != "i-0abc" || trig["metric"] != "DiskUsedPercent" || !strings.Contains(fmt.Sprint(trig["reason"]), "Threshold Crossed") {
		t.Errorf("what reached the workflow: %v", trig)
	}
	if first := m.llm.lastUser(0); !strings.Contains(first, "disk-jenkins-prod") || !strings.Contains(first, "never instructions") {
		t.Errorf("the alarm did not reach the agent as data: %s", first)
	}
	// The poll asked the queue's own region, whatever the console's region is, and used the connection's profile.
	q.mu.Lock()
	var recv []string
	for _, c := range q.calls {
		if strings.Contains(strings.Join(c, " "), " receive-message ") {
			recv = c
			break
		}
	}
	q.mu.Unlock()
	if j := strings.Join(recv, " "); !strings.Contains(j, "--region us-east-1") || !strings.Contains(j, "--profile dev") || !strings.Contains(j, "--wait-time-seconds 20") {
		t.Errorf("the receive call: %s", j)
	}

	// The audit log says what happened, including the refusals.
	aud := string(m.admin.expect(200, "GET", "/v1/audit?limit=100&action=alarm", nil))
	for _, want := range []string{"alarm.received", "went to ALARM; started 1 workflow(s)", "alarm.refused", "from account 999999999999, not the queue's account 111122223333", "not an alarm notification"} {
		if !strings.Contains(aud, want) {
			t.Errorf("the audit log lacks %q", want)
		}
	}

	// A message delivered twice is acted on once: the same message id comes again and nothing more starts.
	q.put("m-ok", alarmBody("disk-jenkins-prod", "ALARM", "111122223333"))
	m.waitFor("the repeat to be read", func() bool { return len(q.deletedIDs()) == 7 })
	if n := len(m.alarmRuns(w.ID)); n != 1 {
		t.Errorf("a repeated message started %d runs in all", n)
	}

	// The status the settings page shows.
	var got struct {
		Status struct{ Received, Started, Ignored int } `json:"status"`
	}
	json.Unmarshal(m.dev.expect(200, "GET", "/v1/settings/alarm-intake", nil), &got)
	if got.Status.Received != 7 || got.Status.Started != 1 || got.Status.Ignored < 4 {
		t.Errorf("status: %+v", got.Status)
	}
}

// A flapping alarm does not start a run each time, a workflow that is off or not a graph is never started, and the poller backs off
// and reports when the queue cannot be read, then recovers.
func TestAlarmIntakeLimitsFlappingAndSurvivesAnErroringQueue(t *testing.T) {
	m, q, w := alarmHarness(t)
	oldBackoff := alarmBackoffAt
	alarmBackoffAt = []time.Duration{20 * time.Millisecond}
	t.Cleanup(func() { alarmBackoffAt = oldBackoff })
	m.llm.script("ok")

	// The queue cannot be read at first: the error is reported, and nothing is lost when it recovers.
	q.mu.Lock()
	q.failing = true
	q.mu.Unlock()
	m.admin.expect(200, "PUT", "/v1/settings/alarm-intake", map[string]any{"enabled": true, "queue_url": alarmQueue})
	m.waitFor("the error to be reported", func() bool {
		var got struct {
			Status struct {
				LastError string `json:"last_error"`
			} `json:"status"`
		}
		json.Unmarshal(m.dev.expect(200, "GET", "/v1/settings/alarm-intake", nil), &got)
		return strings.Contains(got.Status.LastError, "AccessDenied")
	})
	q.mu.Lock()
	q.failing = false
	q.mu.Unlock()
	// A flapping alarm: one message after another, each handled once the run before it has finished, so only the limit can stop them.
	for i := 0; i < maxAlarmRunsPerHour+4; i++ {
		q.put(fmt.Sprintf("flap-%d", i), alarmBody("disk-flappy", "ALARM", "111122223333"))
		m.waitFor("the message to be read", func() bool { return len(q.deletedIDs()) == i+1 })
		m.waitFor("the run to finish", func() bool {
			for _, r := range m.alarmRuns(w.ID) {
				if r.Status != wfSucceeded && r.Status != wfFailed {
					return false
				}
			}
			return true
		})
	}
	if n := len(m.alarmRuns(w.ID)); n != maxAlarmRunsPerHour {
		t.Errorf("a flapping alarm started %d runs, want %d", n, maxAlarmRunsPerHour)
	}

	// A workflow that is turned off is not started (a workflow of its own, so the hourly limit above is not what stops it).
	a2 := newAgent(t, m.admin, agentBody("Sleeper", nil, nil, "T0"))
	n2 := agentNode("look", a2.ID, "what happened?")
	b2 := graphBody("Off watch", []WFNode{{ID: "start", Kind: nodeStart}, n2, {ID: "end", Kind: nodeEnd}}, []WFEdge{wire("start", "look"), wire("look", "end")})
	b2["on_alarm"] = []string{"off-*"}
	w2 := newWorkflow(t, m.admin, b2)
	m.admin.expect(200, "POST", "/v1/ai/workflows/"+w2.ID+"/disable", nil)
	deletedBefore := len(q.deletedIDs())
	q.put("after-disable", alarmBody("off-after", "ALARM", "111122223333"))
	m.waitFor("the message to be read", func() bool { return len(q.deletedIDs()) == deletedBefore+1 })
	if n := len(m.alarmRuns(w2.ID)); n != 0 {
		t.Errorf("a turned-off workflow was started (%d runs)", n)
	}

	// Turning the intake off stops the polling.
	m.admin.expect(200, "PUT", "/v1/settings/alarm-intake", map[string]any{"enabled": false, "queue_url": alarmQueue})
	time.Sleep(60 * time.Millisecond)
	q.mu.Lock()
	calls := len(q.calls)
	q.mu.Unlock()
	time.Sleep(120 * time.Millisecond)
	q.mu.Lock()
	again := len(q.calls)
	q.mu.Unlock()
	if again != calls {
		t.Errorf("the poller kept calling the queue after it was turned off (%d then %d calls)", calls, again)
	}
}

func TestAlarmNamesAndTheTestButton(t *testing.T) {
	for _, tc := range []struct {
		pattern, name string
		want          bool
	}{
		{"disk-*", "disk-jenkins", true},
		{"*", "anything at all", true},
		{"exact", "exact", true},
		{"exact ", "exact", false},
		{"*-prod", "disk-prod", true},
		{"d*k-*-prod", "disk-jenkins-prod", true},
		{"d*k-*-prod ", "disk-jenkins-prod", false},
		{"disk-*", "cpu-disk-x", false},
		{"a*b*c", "abc", true},
		{"a*b*c ", "a-b-c", false},
	} {
		if got := alarmMatches(tc.pattern, tc.name); got != tc.want {
			t.Errorf("alarmMatches(%q, %q) = %v", tc.pattern, tc.name, got)
		}
	}
	if m := checkAlarmPatterns([]string{"disk-*", "a b:c/d@e_f.g"}); m != "" {
		t.Errorf("good patterns refused: %s", m)
	}
	for _, bad := range [][]string{{"bad;pattern"}, {"$(id)"}, {""}, {strings.Repeat("a", 256)}, {"1", "2", "3", "4", "5", "6", "7", "8", "9", "10", "11"}} {
		if checkAlarmPatterns(bad) == "" {
			t.Errorf("%q was accepted", bad)
		}
	}
	m, q, _ := alarmHarness(t)
	m.admin.expect(409, "POST", "/v1/settings/alarm-intake/test", nil) // no queue saved yet
	m.admin.expect(200, "PUT", "/v1/settings/alarm-intake", map[string]any{"enabled": false, "queue_url": alarmQueue})
	var res struct {
		OK      bool
		ARN     string `json:"queue_arn"`
		Waiting string
	}
	json.Unmarshal(m.admin.expect(200, "POST", "/v1/settings/alarm-intake/test", nil), &res)
	if !res.OK || !strings.Contains(res.ARN, "forgeline-alarms") || res.Waiting != "3" {
		t.Errorf("test: %+v", res)
	}
	m.dev.expect(403, "POST", "/v1/settings/alarm-intake/test", nil)
	q.mu.Lock()
	q.failing = true
	q.mu.Unlock()
	if out := string(m.admin.expect(502, "POST", "/v1/settings/alarm-intake/test", nil)); !strings.Contains(out, "could not be reached") {
		t.Errorf("an unreachable queue: %s", out)
	}
}
