package server

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"time"
)

// CloudWatch alarms for the AWS map (design note 023, step 2). One read-only call,
// `cloudwatch describe-alarms`. The console matches an alarm to a resource only when the alarm's own
// dimensions name that resource; a resource with no alarm is shown as having none, never as healthy.

const (
	alarmsTimeout = 30 * time.Second
	alarmsMax     = 500 // alarms kept; the true count is still reported
)

// AlarmDimJSON is one dimension of an alarm's metric.
type AlarmDimJSON struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// AlarmJSON is one metric alarm.
type AlarmJSON struct {
	Name       string         `json:"name"`
	State      string         `json:"state"` // OK, ALARM or INSUFFICIENT_DATA
	Reason     string         `json:"reason,omitempty"`
	Updated    string         `json:"updated,omitempty"`
	Namespace  string         `json:"namespace,omitempty"`
	Metric     string         `json:"metric,omitempty"`
	Dimensions []AlarmDimJSON `json:"dimensions,omitempty"`
}

// AlarmsJSON is the alarms view. Error is set when they could not be read (for example the role may
// not call cloudwatch:DescribeAlarms), so the console can say so instead of showing "no alarms".
type AlarmsJSON struct {
	Enabled   bool        `json:"enabled"`
	Region    string      `json:"region,omitempty"`
	FetchedAt string      `json:"fetched_at,omitempty"`
	Error     string      `json:"error,omitempty"`
	Count     int         `json:"count"`
	Truncated bool        `json:"truncated,omitempty"`
	Alarms    []AlarmJSON `json:"alarms"`
}

func (s *Server) handleAWSAlarms(w http.ResponseWriter, r *http.Request) {
	st, acct, ok := s.accountParam(w, r)
	if !ok {
		return
	}
	// Alarms change more often than resources: kept for at most five minutes while a newer read runs.
	o := s.awsReadOpts(alarmsTimeout)
	o.maxAge = 5 * time.Minute
	d, _, err := s.awsAlarms.get(r.Context(), acct+"|"+st.Profile+"|"+st.Region, r.URL.Query().Get("refresh") == "1", o, func(ctx context.Context) (AlarmsJSON, bool) {
		d := s.fetchAlarms(ctx, st)
		return d, d.Error == ""
	})
	if err != nil {
		return // the person left
	}
	writeJSON(w, http.StatusOK, d)
}

func (s *Server) fetchAlarms(ctx context.Context, st awsSettings) AlarmsJSON {
	d := AlarmsJSON{Alarms: []AlarmJSON{}}
	if !st.Enabled {
		return d
	}
	d.Enabled, d.Region, d.FetchedAt = true, st.Region, time.Now().UTC().Format(time.RFC3339)
	ctx, cancel := context.WithTimeout(ctx, alarmsTimeout)
	defer cancel()
	raw, err := s.aws(ctx, st, "cloudwatch", "describe-alarms")
	if err != nil {
		d.Error = alarmsError(err)
		return d
	}
	var in struct {
		MetricAlarms []struct {
			AlarmName             string
			StateValue            string
			StateReason           string
			StateUpdatedTimestamp any
			Namespace             string
			MetricName            string
			Dimensions            []struct{ Name, Value string }
		}
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		d.Error = "could not read the answer: " + err.Error()
		return d
	}
	for _, a := range in.MetricAlarms {
		x := AlarmJSON{Name: a.AlarmName, State: a.StateValue, Reason: truncate(a.StateReason, 300), Updated: timeString(a.StateUpdatedTimestamp), Namespace: a.Namespace, Metric: a.MetricName}
		for _, dm := range a.Dimensions {
			x.Dimensions = append(x.Dimensions, AlarmDimJSON{Name: dm.Name, Value: dm.Value})
		}
		d.Alarms = append(d.Alarms, x)
	}
	// Alarms in ALARM first, then by name, so a cut keeps the ones that matter.
	rank := func(s string) int {
		switch s {
		case "ALARM":
			return 0
		case "INSUFFICIENT_DATA":
			return 1
		}
		return 2
	}
	sort.SliceStable(d.Alarms, func(i, j int) bool {
		if ri, rj := rank(d.Alarms[i].State), rank(d.Alarms[j].State); ri != rj {
			return ri < rj
		}
		return d.Alarms[i].Name < d.Alarms[j].Name
	})
	d.Count = len(d.Alarms)
	if len(d.Alarms) > alarmsMax {
		d.Alarms, d.Truncated = d.Alarms[:alarmsMax], true
	}
	return d
}

// alarmsError names the permission, so a denied role gets something to act on.
func alarmsError(err error) string {
	m := inventoryError(err)
	if m == "this role is not allowed to list it" {
		return "this role is not allowed to read alarms (it needs cloudwatch:DescribeAlarms)"
	}
	return m
}
