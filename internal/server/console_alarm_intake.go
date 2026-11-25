package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/forgelinehq/forgeline/internal/authn"
	"github.com/forgelinehq/forgeline/internal/docstore"
)

// Alarm intake (design note 051). A CloudWatch alarm tells a workflow that something is wrong: alarm, SNS topic, SQS queue, and
// Forgeline polls the queue. It polls, instead of being called, because SNS can only deliver to a public HTTPS address and a
// Forgeline server is usually not one (local first; nothing listens for the world). The queue is the trust boundary: only SNS may
// send to it (its policy says so; the setup guide gives it), and a message whose account is not the queue's own account is ignored.
// A message that is an alarm going to ALARM starts every graph workflow whose `on_alarm` names it, with the alarm's details as the
// state value "trigger", which agents read as data and never as instructions.

const (
	idAlarmIntake       = "alarm-intake"
	kindAlarmSeen       = "alarmseen"
	trigAlarm           = "alarm"
	maxAlarmRunsPerHour = 6 // per workflow: alarms flap, and a flapping alarm must not start a run each time
	maxAlarmPatterns    = 10
	alarmSeenKeep       = 24 * time.Hour
)

var (
	// alarmPoll and alarmPause are variables so a test can run the loop quickly.
	alarmPause     = time.Second
	alarmBackoffAt = []time.Duration{5 * time.Second, 15 * time.Second, time.Minute, 5 * time.Minute}

	sqsURLRE       = regexp.MustCompile(`^https://sqs\.([a-z]{2}(?:-[a-z]+)+-[0-9])\.amazonaws\.com/([0-9]{12})/([A-Za-z0-9_-]{1,80}(?:\.fifo)?)$`)
	alarmPatternRE = regexp.MustCompile(`^[A-Za-z0-9:_./@ *-]{1,255}$`)
)

type alarmIntakeSettings struct {
	Enabled   bool      `json:"enabled"`
	QueueURL  string    `json:"queue_url"`
	UpdatedAt time.Time `json:"updated_at"`
	UpdatedBy string    `json:"updated_by"`
}

// alarmStatus is what the poller has been doing, for the settings page.
type alarmStatus struct {
	mu        sync.Mutex
	Polling   bool      `json:"polling"`
	LastPoll  time.Time `json:"last_poll"`
	LastError string    `json:"last_error,omitempty"`
	Received  int       `json:"received"`
	Started   int       `json:"started"`
	Ignored   int       `json:"ignored"`
}

func (s *Server) loadAlarmIntake(ctx context.Context) alarmIntakeSettings {
	var st alarmIntakeSettings
	_ = s.docs.Get(ctx, kindSettings, idAlarmIntake, &st)
	return st
}

// parseQueue splits a queue URL into its region and account.
func parseQueue(u string) (region, account string, ok bool) {
	m := sqsURLRE.FindStringSubmatch(u)
	if m == nil {
		return "", "", false
	}
	return m[1], m[2], true
}

// checkAlarmPatterns validates the names a workflow follows.
func checkAlarmPatterns(patterns []string) string {
	if len(patterns) > maxAlarmPatterns {
		return fmt.Sprintf("on_alarm: at most %d alarm names", maxAlarmPatterns)
	}
	for _, p := range patterns {
		if !alarmPatternRE.MatchString(p) || strings.TrimSpace(p) == "" {
			return fmt.Sprintf("on_alarm: %q is not an alarm name (letters, digits and : _ . / @ - and spaces, * for any characters, at most 255)", p)
		}
	}
	return ""
}

// alarmMatches says whether an alarm name matches a pattern in which * stands for any characters.
func alarmMatches(pattern, name string) bool {
	parts := strings.Split(pattern, "*")
	if len(parts) == 1 {
		return pattern == name
	}
	if !strings.HasPrefix(name, parts[0]) {
		return false
	}
	name = name[len(parts[0]):]
	for _, mid := range parts[1 : len(parts)-1] {
		i := strings.Index(name, mid)
		if i < 0 {
			return false
		}
		name = name[i+len(mid):]
	}
	return strings.HasSuffix(name, parts[len(parts)-1])
}

// --- one message ---

// cloudWatchAlarm is the part of an alarm notification Forgeline reads.
type cloudWatchAlarm struct {
	AlarmName        string `json:"AlarmName"`
	NewStateValue    string `json:"NewStateValue"`
	NewStateReason   string `json:"NewStateReason"`
	AlarmDescription string `json:"AlarmDescription"`
	AWSAccountID     string `json:"AWSAccountId"`
	Region           string `json:"Region"`
	StateChangeTime  string `json:"StateChangeTime"`
	Trigger          struct {
		Namespace  string `json:"Namespace"`
		MetricName string `json:"MetricName"`
		Dimensions []struct {
			Name  string `json:"name"`
			Value string `json:"value"`
		} `json:"Dimensions"`
	} `json:"Trigger"`
}

// parseAlarm reads an alarm out of a queue message body: SNS wraps the alarm in a notification whose Message is a string of JSON;
// a queue subscribed with raw delivery holds the alarm itself.
func parseAlarm(body string) (cloudWatchAlarm, error) {
	var envelope struct {
		Type    string `json:"Type"`
		Message string `json:"Message"`
	}
	inner := body
	if json.Unmarshal([]byte(body), &envelope) == nil && envelope.Type == "Notification" && envelope.Message != "" {
		inner = envelope.Message
	}
	var a cloudWatchAlarm
	if err := json.Unmarshal([]byte(inner), &a); err != nil {
		return a, errors.New("not an alarm notification")
	}
	if a.AlarmName == "" {
		return a, errors.New("no alarm name: not an alarm notification")
	}
	return a, nil
}

// handleAlarm starts the workflows that follow this alarm and returns how many it started.
func (s *Server) handleAlarm(ctx context.Context, a cloudWatchAlarm, queueAccount string) (started int, note string) {
	if a.NewStateValue != "ALARM" {
		return 0, "state " + a.NewStateValue + ": nothing to start"
	}
	if a.AWSAccountID != "" && a.AWSAccountID != queueAccount {
		_ = s.audit(ctx, Principal{Name: "alarm intake"}, "", "alarm.refused", "alarm/"+truncate(a.AlarmName, 120),
			fmt.Sprintf("from account %s, not the queue's account %s; ignored", a.AWSAccountID, queueAccount), false)
		return 0, "from another account"
	}
	all, err := s.listWorkflows(ctx)
	if err != nil {
		return 0, "could not read the workflows"
	}
	dims := map[string]string{}
	for _, d := range a.Trigger.Dimensions {
		dims[truncate(d.Name, 64)] = truncate(d.Value, 200)
	}
	payload, _ := json.Marshal(map[string]any{"alarm": truncate(a.AlarmName, 255), "state": a.NewStateValue, "reason": truncate(a.NewStateReason, 500),
		"description": truncate(a.AlarmDescription, 300), "region": a.Region, "account": a.AWSAccountID, "time": a.StateChangeTime,
		"namespace": truncate(a.Trigger.Namespace, 120), "metric": truncate(a.Trigger.MetricName, 120), "dimensions": dims})
	var skipped []string
	for _, wf := range all {
		v, ok := wf.version(wf.Current)
		if wf.Disabled || wf.ArchivedAt != nil || !ok || v.Graph == nil || len(v.OnAlarm) == 0 {
			continue
		}
		match := false
		for _, p := range v.OnAlarm {
			match = match || alarmMatches(p, a.AlarmName)
		}
		if !match {
			continue
		}
		if s.recentRuns(ctx, wf.ID, trigAlarm, time.Hour) >= maxAlarmRunsPerHour {
			skipped = append(skipped, wf.ID+" (too many runs this hour)")
			continue
		}
		r, err := s.startTriggered(ctx, wf, v, trigAlarm, string(payload))
		if err != nil {
			skipped = append(skipped, wf.ID+" ("+truncate(err.Error(), 80)+")")
			continue
		}
		started++
		_ = s.audit(ctx, Principal{Name: "workflow:" + wf.ID, Role: authn.Role(v.RunAs)}, "", "ai.workflow.run", "workflow/"+wf.ID,
			fmt.Sprintf("v%d after the alarm %s, run %s", v.N, truncate(a.AlarmName, 120), r.ID), true)
	}
	detail := fmt.Sprintf("%s went to ALARM; started %d workflow(s)", truncate(a.AlarmName, 120), started)
	if len(skipped) > 0 {
		detail += "; skipped " + strings.Join(skipped, ", ")
	}
	_ = s.audit(ctx, Principal{Name: "alarm intake"}, "", "alarm.received", "alarm/"+truncate(a.AlarmName, 120), detail, true)
	return started, detail
}

// seen says whether a queue message was handled before (a queue can deliver one twice), and remembers it if not.
func (s *Server) seenAlarm(ctx context.Context, id string) bool {
	if id == "" {
		return false
	}
	if err := s.docs.Create(ctx, kindAlarmSeen, id, struct {
		At time.Time `json:"at"`
	}{time.Now().UTC()}); errors.Is(err, docstore.ErrExists) {
		return true
	}
	return false
}

// pruneSeen forgets messages handled long ago, so the record does not grow for ever.
func (s *Server) pruneSeen(ctx context.Context) {
	docs, err := s.docs.List(ctx, kindAlarmSeen, docstore.ListOptions{})
	if err != nil {
		return
	}
	for _, d := range docs {
		var v struct {
			At time.Time `json:"at"`
		}
		if json.Unmarshal(d.Body, &v) == nil && time.Since(v.At) > alarmSeenKeep {
			_ = s.docs.Delete(ctx, kindAlarmSeen, d.ID)
		}
	}
}

// --- the poller ---

// pollAlarms reads one batch from the queue, handles each message, deletes it, and returns how many there were.
func (s *Server) pollAlarms(ctx context.Context, st alarmIntakeSettings) (int, error) {
	region, account, ok := parseQueue(st.QueueURL)
	if !ok {
		return 0, errors.New("the queue address is not an SQS queue URL")
	}
	aws := s.loadAWS(ctx)
	if !aws.Enabled {
		return 0, errors.New("AWS is not connected: the alarm intake uses the AWS connection's credentials")
	}
	aws.Region = region
	lctx := context.WithValue(ctx, cmdLimitKey{}, 60*time.Second) // the long poll waits up to 20 seconds
	out, err := s.aws(lctx, aws, "sqs", "receive-message", "--queue-url", st.QueueURL, "--max-number-of-messages", "10",
		"--wait-time-seconds", "20", "--visibility-timeout", "120")
	if err != nil {
		return 0, err
	}
	var res struct {
		Messages []struct {
			MessageID     string `json:"MessageId"`
			ReceiptHandle string `json:"ReceiptHandle"`
			Body          string `json:"Body"`
		} `json:"Messages"`
	}
	if len(strings.TrimSpace(string(out))) > 0 {
		if err := json.Unmarshal(out, &res); err != nil {
			return 0, errors.New("the queue's answer could not be read")
		}
	}
	s.alarms.mu.Lock()
	s.alarms.LastPoll, s.alarms.LastError = time.Now().UTC(), ""
	s.alarms.mu.Unlock()
	for _, m := range res.Messages {
		s.alarms.mu.Lock()
		s.alarms.Received++
		s.alarms.mu.Unlock()
		if s.seenAlarm(ctx, m.MessageID) {
			// delivered again: already handled
		} else if a, err := parseAlarm(m.Body); err != nil {
			s.alarms.mu.Lock()
			s.alarms.Ignored++
			s.alarms.mu.Unlock()
			_ = s.audit(ctx, Principal{Name: "alarm intake"}, "", "alarm.refused", "queue/"+truncate(m.MessageID, 80), "a message in the queue that is not an alarm: "+err.Error()+"; deleted", false)
		} else {
			n, _ := s.handleAlarm(ctx, a, account)
			s.alarms.mu.Lock()
			if n > 0 {
				s.alarms.Started += n
			} else {
				s.alarms.Ignored++
			}
			s.alarms.mu.Unlock()
		}
		if _, err := s.aws(ctx, aws, "sqs", "delete-message", "--queue-url", st.QueueURL, "--receipt-handle", m.ReceiptHandle); err != nil {
			s.log.Warn("deleting an alarm message", "err", err) // it will be delivered again and skipped as seen
		}
	}
	return len(res.Messages), nil
}

// startAlarmIntake runs the poller until the server stops. It does nothing while the intake is off, and notices a change of
// settings at once.
func (s *Server) startAlarmIntake(ctx context.Context) {
	s.alarmReload = make(chan struct{}, 1)
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		fails := 0
		lastPrune := time.Time{}
		wait := func(d time.Duration) bool {
			t := time.NewTimer(d)
			defer t.Stop()
			select {
			case <-ctx.Done():
				return false
			case <-s.alarmReload:
				return true
			case <-t.C:
				return true
			}
		}
		for ctx.Err() == nil {
			st := s.loadAlarmIntake(ctx)
			if !st.Enabled || st.QueueURL == "" {
				s.alarms.mu.Lock()
				s.alarms.Polling = false
				s.alarms.mu.Unlock()
				if !wait(time.Minute) {
					return
				}
				continue
			}
			s.alarms.mu.Lock()
			s.alarms.Polling = true
			s.alarms.mu.Unlock()
			if time.Since(lastPrune) > time.Hour {
				s.pruneSeen(ctx)
				lastPrune = time.Now()
			}
			if _, err := s.pollAlarms(ctx, st); err != nil {
				if ctx.Err() != nil {
					return
				}
				s.alarms.mu.Lock()
				s.alarms.LastError = truncate(err.Error(), 300)
				s.alarms.mu.Unlock()
				i := min(fails, len(alarmBackoffAt)-1)
				fails++
				if !wait(alarmBackoffAt[i]) {
					return
				}
				continue
			}
			fails = 0
			if !wait(alarmPause) {
				return
			}
		}
	}()
}

// --- the settings API ---

func (s *Server) handleGetAlarmIntake(w http.ResponseWriter, r *http.Request) {
	st := s.loadAlarmIntake(r.Context())
	s.alarms.mu.Lock()
	status := map[string]any{"polling": s.alarms.Polling, "last_poll": s.alarms.LastPoll, "last_error": s.alarms.LastError,
		"received": s.alarms.Received, "started": s.alarms.Started, "ignored": s.alarms.Ignored}
	s.alarms.mu.Unlock()
	region, account, _ := parseQueue(st.QueueURL)
	writeJSON(w, http.StatusOK, map[string]any{"enabled": st.Enabled, "queue_url": st.QueueURL, "region": region, "account": account,
		"aws_connected": s.loadAWS(r.Context()).Enabled, "updated_by": st.UpdatedBy, "updated_at": st.UpdatedAt, "status": status})
}

func (s *Server) handlePutAlarmIntake(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Enabled  bool   `json:"enabled"`
		QueueURL string `json:"queue_url"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	req.QueueURL = strings.TrimSpace(req.QueueURL)
	if req.QueueURL != "" {
		if _, _, ok := parseQueue(req.QueueURL); !ok {
			writeError(w, http.StatusBadRequest, "queue_url: an SQS queue address such as https://sqs.us-east-1.amazonaws.com/123456789012/forgeline-alarms")
			return
		}
	}
	if req.Enabled && req.QueueURL == "" {
		writeError(w, http.StatusBadRequest, "queue_url: give the queue to poll before turning the intake on")
		return
	}
	if req.Enabled && !s.loadAWS(r.Context()).Enabled {
		writeError(w, http.StatusConflict, "connect AWS first: the intake uses the AWS connection's credentials")
		return
	}
	p := principalFrom(r.Context())
	st := alarmIntakeSettings{Enabled: req.Enabled, QueueURL: req.QueueURL, UpdatedAt: time.Now().UTC(), UpdatedBy: p.Name}
	if err := s.docs.Put(r.Context(), kindSettings, idAlarmIntake, st); err != nil {
		s.internalError(w, "saving the alarm intake", err)
		return
	}
	s.auditReq(r, "alarm.intake.update", "settings/alarm-intake", fmt.Sprintf("enabled %v, queue %s", st.Enabled, st.QueueURL), true)
	select {
	case s.alarmReload <- struct{}{}:
	default:
	}
	s.handleGetAlarmIntake(w, r)
}

// handleTestAlarmIntake checks that the queue can be reached with the AWS connection's credentials, without reading a message.
func (s *Server) handleTestAlarmIntake(w http.ResponseWriter, r *http.Request) {
	st := s.loadAlarmIntake(r.Context())
	region, _, ok := parseQueue(st.QueueURL)
	if !ok {
		writeError(w, http.StatusConflict, "save the queue address first")
		return
	}
	aws := s.loadAWS(r.Context())
	if !aws.Enabled {
		writeError(w, http.StatusConflict, "connect AWS first")
		return
	}
	aws.Region = region
	out, err := s.aws(r.Context(), aws, "sqs", "get-queue-attributes", "--queue-url", st.QueueURL, "--attribute-names", "QueueArn", "ApproximateNumberOfMessages")
	if err != nil {
		writeError(w, http.StatusBadGateway, "the queue could not be reached: "+truncate(err.Error(), 300))
		return
	}
	var res struct {
		Attributes map[string]string `json:"Attributes"`
	}
	_ = json.Unmarshal(out, &res)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "queue_arn": res.Attributes["QueueArn"], "waiting": res.Attributes["ApproximateNumberOfMessages"]})
}
