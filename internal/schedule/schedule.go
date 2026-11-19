// Package schedule is when workflows and teams run by themselves (design note 047): either every N minutes, or a
// five-field cron expression in a time zone ("0 9 * * 1-5" in Europe/Berlin: weekdays at nine). Nothing may run more
// often than every MinMinutes. It has no dependencies beyond the standard library.
package schedule

import (
	"fmt"
	"math/bits"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata" // time zones work in a container without /usr/share/zoneinfo
)

const (
	MinMinutes = 15          // the most often anything runs by itself
	MaxMinutes = 7 * 24 * 60 // an interval of at most a week
	// CatchUp is how late a run may still start after its time (the server was down, or busy): within it, a missed time
	// runs once; older ones are skipped, never queued.
	CatchUp = time.Hour
)

// Spec is a schedule as it is stored: Every (minutes) or Cron with TZ (empty: UTC). Both empty: only by hand.
type Spec struct {
	Every int    `json:"every_minutes,omitempty"`
	Cron  string `json:"cron,omitempty"`
	TZ    string `json:"tz,omitempty"`
}

// On says whether the spec runs by itself at all.
func (s Spec) On() bool { return s.Every > 0 || s.Cron != "" }

// Cron is a parsed expression: one bit set per allowed value of each field.
type Cron struct {
	min, hour, dom, month, dow uint64
	domAny, dowAny             bool // * in day of month / day of week: cron's "either" rule needs to know
}

var monthNames = map[string]int{"jan": 1, "feb": 2, "mar": 3, "apr": 4, "may": 5, "jun": 6, "jul": 7, "aug": 8, "sep": 9, "oct": 10, "nov": 11, "dec": 12}
var dayNames = map[string]int{"sun": 0, "mon": 1, "tue": 2, "wed": 3, "thu": 4, "fri": 5, "sat": 6}

// Parse reads minute hour day-of-month month day-of-week, with *, lists (1,15), ranges (1-5), steps (*/30, 9-17/2)
// and names (mon, jan). Errors say which field is wrong.
func Parse(expr string) (*Cron, error) {
	f := strings.Fields(expr)
	if len(f) != 5 {
		return nil, fmt.Errorf("cron: want 5 fields (minute hour day month weekday), got %d", len(f))
	}
	var c Cron
	var err error
	specs := []struct {
		name     string
		lo, hi   int
		names    map[string]int
		out      *uint64
		wildcard *bool
	}{
		{"minute", 0, 59, nil, &c.min, nil},
		{"hour", 0, 23, nil, &c.hour, nil},
		{"day of month", 1, 31, nil, &c.dom, &c.domAny},
		{"month", 1, 12, monthNames, &c.month, nil},
		{"weekday", 0, 7, dayNames, &c.dow, &c.dowAny},
	}
	for i, sp := range specs {
		if *sp.out, err = field(f[i], sp.lo, sp.hi, sp.names); err != nil {
			return nil, fmt.Errorf("cron: %s: %v", sp.name, err)
		}
		if sp.wildcard != nil {
			*sp.wildcard = f[i] == "*" || f[i] == "?"
		}
	}
	if c.dow&(1<<7) != 0 { // 7 is Sunday too
		c.dow = c.dow&^(1<<7) | 1
	}
	return &c, nil
}

func field(s string, lo, hi int, names map[string]int) (uint64, error) {
	var bits uint64
	for _, part := range strings.Split(s, ",") {
		rng, stepS, hasStep := strings.Cut(part, "/")
		step := 1
		if hasStep {
			n, err := strconv.Atoi(stepS)
			if err != nil || n < 1 {
				return 0, fmt.Errorf("%q: the step after / is a number from 1", part)
			}
			step = n
		}
		a, b := lo, hi
		if rng != "*" && rng != "?" {
			x, y, isRange := strings.Cut(rng, "-")
			var err error
			if a, err = value(x, lo, hi, names); err != nil {
				return 0, err
			}
			b = a
			if isRange {
				if b, err = value(y, lo, hi, names); err != nil {
					return 0, err
				}
				if b < a {
					return 0, fmt.Errorf("%q: a range goes from low to high", part)
				}
			} else if hasStep {
				b = hi // 5/15 means from 5 on, every 15
			}
		}
		for v := a; v <= b; v += step {
			bits |= 1 << v
		}
	}
	return bits, nil
}

func value(s string, lo, hi int, names map[string]int) (int, error) {
	if v, ok := names[strings.ToLower(s)]; ok {
		return v, nil
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < lo || n > hi {
		return 0, fmt.Errorf("%q: a number from %d to %d", s, lo, hi)
	}
	return n, nil
}

func has(set uint64, v int) bool { return set&(1<<v) != 0 }

// dayMatches is cron's rule: with both day fields restricted, either may match; otherwise the restricted one must.
func (c *Cron) dayMatches(t time.Time) bool {
	d, w := has(c.dom, t.Day()), has(c.dow, int(t.Weekday()))
	switch {
	case c.domAny && c.dowAny:
		return true
	case c.domAny:
		return w
	case c.dowAny:
		return d
	}
	return d || w
}

// Next is the first time after t (to the minute) that the expression matches, in loc. Zero when none comes within
// five years (31 February).
func (c *Cron) Next(t time.Time, loc *time.Location) time.Time {
	t = t.In(loc).Truncate(time.Minute).Add(time.Minute)
	end := t.AddDate(5, 0, 0)
	for t.Before(end) {
		switch {
		case !has(c.month, int(t.Month())):
			t = time.Date(t.Year(), t.Month()+1, 1, 0, 0, 0, 0, loc)
		case !c.dayMatches(t):
			t = time.Date(t.Year(), t.Month(), t.Day()+1, 0, 0, 0, 0, loc)
		case !has(c.hour, t.Hour()):
			t = time.Date(t.Year(), t.Month(), t.Day(), t.Hour()+1, 0, 0, 0, loc)
		case !has(c.min, t.Minute()):
			t = t.Add(time.Minute)
		default:
			return t
		}
	}
	return time.Time{}
}

// minGap is the shortest time between two runs: the shortest gap between the minutes it runs at, around the hour, when
// it runs in consecutive hours; otherwise between runs in the same hour.
func (c *Cron) minGap() int {
	mins := []int{}
	for m := 0; m < 60; m++ {
		if has(c.min, m) {
			mins = append(mins, m)
		}
	}
	gap := 60
	for i := 1; i < len(mins); i++ {
		gap = min(gap, mins[i]-mins[i-1])
	}
	if bits.OnesCount64(c.hour) > 1 || len(mins) > 1 {
		gap = min(gap, mins[0]+60-mins[len(mins)-1])
	}
	return gap
}

// Check says what is wrong with a spec, or loads its zone.
func Check(s Spec) (*time.Location, error) {
	switch {
	case s.Every != 0 && s.Cron != "":
		return nil, fmt.Errorf("schedule: either every_minutes or cron, not both")
	case s.Every != 0 && (s.Every < MinMinutes || s.Every > MaxMinutes):
		return nil, fmt.Errorf("schedule: every %d to %d minutes", MinMinutes, MaxMinutes)
	case s.Cron == "" && s.TZ != "":
		return nil, fmt.Errorf("schedule: a time zone goes with a cron expression")
	}
	loc, err := time.LoadLocation(s.TZ)
	if err != nil || s.TZ == "Local" {
		return nil, fmt.Errorf("schedule: tz: %q is not a time zone such as Europe/Berlin", s.TZ)
	}
	if s.Cron != "" {
		c, err := Parse(s.Cron)
		if err != nil {
			return nil, fmt.Errorf("schedule: %v", err)
		}
		if g := c.minGap(); g < MinMinutes {
			return nil, fmt.Errorf("schedule: cron: it would run %d minutes apart; at most every %d minutes", g, MinMinutes)
		}
		if c.Next(time.Now(), loc).IsZero() {
			return nil, fmt.Errorf("schedule: cron: it never runs (no such date)")
		}
	}
	return loc, nil
}

// NextTimes are the next n times a spec runs after t, given when it last ran. Empty when it only runs by hand.
func NextTimes(s Spec, last, t time.Time, n int) []time.Time {
	var out []time.Time
	if s.Every > 0 {
		next := last.Add(time.Duration(s.Every) * time.Minute)
		if next.Before(t) {
			next = t
		}
		for i := 0; i < n; i++ {
			out = append(out, next.Add(time.Duration(i*s.Every)*time.Minute))
		}
		return out
	}
	loc, err := Check(s)
	if err != nil || s.Cron == "" {
		return nil
	}
	c, _ := Parse(s.Cron)
	for at := t; len(out) < n; {
		at = c.Next(at, loc)
		if at.IsZero() {
			break
		}
		out = append(out, at)
	}
	return out
}

// Due says whether a spec should start now, given when it last started by itself: an interval that has passed, or a
// cron time since then that is at most CatchUp ago. A time missed by longer is skipped (it is not run late).
func Due(s Spec, last, now time.Time) bool {
	if s.Every > 0 {
		return now.Sub(last) >= time.Duration(s.Every)*time.Minute
	}
	if s.Cron == "" {
		return false
	}
	loc, err := Check(s)
	if err != nil {
		return false
	}
	c, _ := Parse(s.Cron)
	from := now.Add(-CatchUp)
	if last.After(from) {
		from = last
	}
	at := c.Next(from.Add(-time.Minute), loc) // a time exactly at from counts
	for !at.IsZero() && (at.Before(from) || !at.After(last)) {
		at = c.Next(at, loc)
	}
	return !at.IsZero() && !at.After(now)
}

// Describe is a spec in words, for the console and the audit log.
func Describe(s Spec) string {
	switch {
	case s.Every > 0 && s.Every%1440 == 0:
		return plural(s.Every/1440, "day")
	case s.Every > 0 && s.Every%60 == 0:
		return plural(s.Every/60, "hour")
	case s.Every > 0:
		return plural(s.Every, "minute")
	case s.Cron != "":
		tz := s.TZ
		if tz == "" {
			tz = "UTC"
		}
		return "cron " + s.Cron + " (" + tz + ")"
	}
	return "only by hand"
}

func plural(n int, unit string) string {
	if n == 1 {
		return "every " + unit
	}
	return fmt.Sprintf("every %d %ss", n, unit)
}
