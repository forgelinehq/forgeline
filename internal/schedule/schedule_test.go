package schedule

import (
	"strings"
	"testing"
	"time"
)

var berlin, _ = time.LoadLocation("Europe/Berlin")

func at(s string) time.Time {
	t, err := time.ParseInLocation("2006-01-02 15:04", s, berlin)
	if err != nil {
		panic(err)
	}
	return t
}

func TestNext(t *testing.T) {
	for _, c := range []struct{ expr, from, want string }{
		{"0 9 * * 1-5", "2026-11-20 08:00", "2026-11-20 09:00"}, // a Friday
		{"0 9 * * 1-5", "2026-11-20 09:00", "2026-11-23 09:00"}, // then Monday
		{"30 7 1 * *", "2026-11-20 08:00", "2026-12-01 07:30"},
		{"0 */6 * * *", "2026-11-20 13:10", "2026-11-20 18:00"},
		{"0 9 * * sun", "2026-11-20 08:00", "2026-11-22 09:00"},
		{"0 9 * * 7", "2026-11-20 08:00", "2026-11-22 09:00"},
		{"0 0 29 feb *", "2026-11-20 08:00", "2028-02-29 00:00"},
		{"0 9 13 * fri", "2026-11-14 08:00", "2026-11-20 09:00"}, // either day field
		{"15,45 10 * dec mon", "2026-11-20 08:00", "2026-12-07 10:15"},
	} {
		cr, err := Parse(c.expr)
		if err != nil {
			t.Fatalf("%s: %v", c.expr, err)
		}
		if got := cr.Next(at(c.from), berlin); !got.Equal(at(c.want)) {
			t.Errorf("%s after %s: got %s, want %s", c.expr, c.from, got.In(berlin).Format("2006-01-02 15:04 Mon"), c.want)
		}
	}
	// Daylight saving: 09:00 in Berlin is 08:00 UTC in winter and 07:00 UTC in summer.
	cr, _ := Parse("0 9 * * *")
	if w, s := cr.Next(at("2026-12-01 10:00"), berlin).UTC().Hour(), cr.Next(at("2027-07-01 10:00"), berlin).UTC().Hour(); w != 8 || s != 7 {
		t.Errorf("DST: %d %d", w, s)
	}
}

func TestCheckRefusesWhatIsWrong(t *testing.T) {
	for spec, want := range map[Spec]string{
		{Cron: "0 9 * *"}:                       "5 fields",
		{Cron: "61 9 * * *"}:                    "minute",
		{Cron: "0 9-7 * * *"}:                   "low to high",
		{Cron: "*/5 * * * *"}:                   "5 minutes apart",
		{Cron: "0,10 9 * * *"}:                  "10 minutes apart",
		{Cron: "0 0 31 2 *"}:                    "never runs",
		{Cron: "0 9 * * *", TZ: "Mars/Olympus"}: "not a time zone",
		{Cron: "0 9 * * *", TZ: "Local"}:        "not a time zone",
		{Every: 5}:                              "every 15 to",
		{Every: 60, Cron: "0 9 * * *"}:          "not both",
		{Every: 60, TZ: "Europe/Berlin"}:        "time zone goes with",
		{Cron: "0 9 * * xyz"}:                   "weekday",
		{Cron: "0/0 9 * * *"}:                   "step",
	} {
		if _, err := Check(spec); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%+v: got %v, want %q", spec, err, want)
		}
	}
	for _, ok := range []Spec{{}, {Every: 15}, {Cron: "*/15 * * * *"}, {Cron: "0 9 * * 1-5", TZ: "Europe/Berlin"}, {Cron: "0 9,17 * * *", TZ: "Asia/Kolkata"}} {
		if _, err := Check(ok); err != nil {
			t.Errorf("%+v: %v", ok, err)
		}
	}
}

func TestDue(t *testing.T) {
	s := Spec{Cron: "0 9 * * *", TZ: "Europe/Berlin"}
	for _, c := range []struct {
		last, now string
		want      bool
	}{
		{"2026-11-19 09:00", "2026-11-20 08:59", false},
		{"2026-11-19 09:00", "2026-11-20 09:00", true},
		{"2026-11-19 09:00", "2026-11-20 09:40", true},  // late, within the hour
		{"2026-11-19 09:00", "2026-11-20 10:30", false}, // missed by more than an hour: skipped
		{"2026-11-20 09:00", "2026-11-20 09:30", false}, // already ran
		{"0001-01-01 00:00", "2026-11-20 09:10", true},  // never ran
		{"2026-11-20 08:59", "2026-11-20 09:00", true},
	} {
		last := at(c.last)
		if c.last[0] == '0' {
			last = time.Time{}
		}
		if got := Due(s, last, at(c.now)); got != c.want {
			t.Errorf("last %s now %s: %v", c.last, c.now, got)
		}
	}
	if !Due(Spec{Every: 60}, at("2026-11-20 08:00"), at("2026-11-20 09:00")) || Due(Spec{Every: 60}, at("2026-11-20 08:30"), at("2026-11-20 09:00")) || Due(Spec{}, time.Time{}, at("2026-11-20 09:00")) {
		t.Error("interval")
	}
	next := NextTimes(s, time.Time{}, at("2026-11-20 10:00"), 3)
	if len(next) != 3 || !next[0].Equal(at("2026-11-21 09:00")) || !next[2].Equal(at("2026-11-23 09:00")) {
		t.Errorf("next %v", next)
	}
	if Describe(Spec{Every: 360}) != "every 6 hours" || Describe(s) != "cron 0 9 * * * (Europe/Berlin)" || Describe(Spec{}) != "only by hand" {
		t.Error("describe")
	}
}
