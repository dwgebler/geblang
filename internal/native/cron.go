package native

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"geblang/internal/runtime"
)

// Hand-rolled standard 5-field cron parser:
//
//   minute hour day-of-month month day-of-week
//
// Each field accepts `*`, comma-separated lists, ranges (a-b), and
// step expressions (a/n, a-b/n, */n). Month and day-of-week accept
// three-letter case-insensitive names (jan-dec, sun-sat). For
// day-of-week, 0 and 7 are both Sunday.
//
// Special strings: @hourly / @daily / @weekly / @monthly / @yearly
// (and @annually as an alias). @reboot is intentionally rejected
// because it has no meaningful next-firing time.
//
// Day-of-month and day-of-week are OR-combined (Vixie semantics):
// when both fields are restricted, a match in either fires the job.

type cronSpec struct {
	raw        string
	minute     fieldSet
	hour       fieldSet
	dayOfMonth fieldSet
	month      fieldSet
	dayOfWeek  fieldSet
	special    string
	andDays    bool
}

type fieldSet struct {
	values     map[int]bool
	restricted bool
}

func registerCron(r *Registry) {
	r.Register("cron", "parse", func(args []runtime.Value) (runtime.Value, error) {
		spec, err := singleCronSpec(args, "cron.parse")
		if err != nil {
			return nil, err
		}
		return cronSpecDict(spec), nil
	})
	r.Register("cron", "isValid", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 1 {
			return nil, fmt.Errorf("cron.isValid expects exactly one argument")
		}
		s, ok := args[0].(runtime.String)
		if !ok {
			return runtime.Bool{Value: false}, nil
		}
		if _, err := parseCron(s.Value); err != nil {
			return runtime.Bool{Value: false}, nil
		}
		return runtime.Bool{Value: true}, nil
	})
	r.Register("cron", "nextAfter", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) < 2 || len(args) > 3 {
			return nil, fmt.Errorf("cron.nextAfter expects (spec, unixSeconds, opts?)")
		}
		spec, err := cronSpecFromValue(args[0], "cron.nextAfter")
		if err != nil {
			return nil, err
		}
		base, err := unixSecondsValue(args[1], "cron.nextAfter")
		if err == nil {
			err = cronDayMode(spec, args[2:])
		}
		if err != nil {
			return nil, err
		}
		next, err := nextAfterCron(spec, base)
		if err != nil {
			return nil, err
		}
		return runtime.SmallInt{Value: next}, nil
	})
	r.Register("cron", "nextN", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) < 3 || len(args) > 4 {
			return nil, fmt.Errorf("cron.nextN expects (spec, unixSeconds, count, opts?)")
		}
		spec, err := cronSpecFromValue(args[0], "cron.nextN")
		if err != nil {
			return nil, err
		}
		base, err := unixSecondsValue(args[1], "cron.nextN")
		if err == nil {
			err = cronDayMode(spec, args[3:])
		}
		if err != nil {
			return nil, err
		}
		count, err := intValue(args[2], "cron.nextN count")
		if err != nil {
			return nil, err
		}
		if count < 0 {
			return nil, fmt.Errorf("cron.nextN count must be non-negative")
		}
		out := make([]runtime.Value, 0, count)
		cursor := base
		for i := int64(0); i < count; i++ {
			next, err := nextAfterCron(spec, cursor)
			if err != nil {
				return nil, err
			}
			out = append(out, runtime.SmallInt{Value: next})
			cursor = next
		}
		return &runtime.List{Elements: out}, nil
	})
}

func cronDayMode(spec *cronSpec, args []runtime.Value) error {
	if len(args) == 0 {
		return nil
	}
	opts, ok := args[0].(runtime.Dict)
	if !ok {
		return fmt.Errorf("cron options must be a dict")
	}
	value, exists := LookupDictEntry(opts, runtime.String{Value: "dayMatch"})
	if !exists {
		return nil
	}
	mode, ok := value.Value.(runtime.String)
	if !ok || (mode.Value != "or" && mode.Value != "and") {
		return fmt.Errorf("cron dayMatch must be or or and")
	}
	spec.andDays = mode.Value == "and"
	return nil
}

// ---- helpers ----

func singleCronSpec(args []runtime.Value, label string) (*cronSpec, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("%s expects exactly one argument", label)
	}
	return cronSpecFromValue(args[0], label)
}

func cronSpecFromValue(v runtime.Value, label string) (*cronSpec, error) {
	s, ok := v.(runtime.String)
	if !ok {
		return nil, fmt.Errorf("%s spec must be string", label)
	}
	spec, err := parseCron(s.Value)
	if err != nil {
		return nil, fmt.Errorf("%s: %s", label, err.Error())
	}
	return spec, nil
}

func unixSecondsValue(v runtime.Value, label string) (int64, error) {
	if n, ok := AsInt64(v); ok {
		return n, nil
	}
	return 0, fmt.Errorf("%s unix-seconds must be int", label)
}

func intValue(v runtime.Value, label string) (int64, error) {
	if n, ok := AsInt64(v); ok {
		return n, nil
	}
	return 0, fmt.Errorf("%s must be int", label)
}

func cronSpecDict(spec *cronSpec) runtime.Value {
	d := runtime.NewDictHint(7)
	putCronStringEntry(&d, "spec", spec.raw)
	if spec.special != "" {
		putCronStringEntry(&d, "special", spec.special)
	} else {
		putCronNullEntry(&d, "special")
	}
	putCronFieldEntry(&d, "minute", spec.minute)
	putCronFieldEntry(&d, "hour", spec.hour)
	putCronFieldEntry(&d, "dayOfMonth", spec.dayOfMonth)
	putCronFieldEntry(&d, "month", spec.month)
	putCronFieldEntry(&d, "dayOfWeek", spec.dayOfWeek)
	return d
}

func putCronStringEntry(d *runtime.Dict, key, value string) {
	k := runtime.String{Value: key}
	d.PutEntry(DictKey(k), runtime.DictEntry{Key: k, Value: runtime.String{Value: value}})
}

func putCronNullEntry(d *runtime.Dict, key string) {
	k := runtime.String{Value: key}
	d.PutEntry(DictKey(k), runtime.DictEntry{Key: k, Value: runtime.Null{}})
}

func putCronFieldEntry(d *runtime.Dict, key string, f fieldSet) {
	k := runtime.String{Value: key}
	values := make([]runtime.Value, 0, len(f.values))
	for v := 0; v < 60; v++ {
		if f.values[v] {
			values = append(values, runtime.SmallInt{Value: int64(v)})
		}
	}
	d.PutEntry(DictKey(k), runtime.DictEntry{Key: k, Value: &runtime.List{Elements: values}})
}

// ---- parsing ----

func parseCron(raw string) (*cronSpec, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return nil, fmt.Errorf("empty spec")
	}
	if strings.HasPrefix(s, "@") {
		return parseCronSpecial(raw, s)
	}
	fields := strings.Fields(s)
	if len(fields) != 5 {
		return nil, fmt.Errorf("expected 5 fields, got %d", len(fields))
	}
	minute, err := parseCronField(fields[0], 0, 59, nil)
	if err != nil {
		return nil, fmt.Errorf("minute: %s", err.Error())
	}
	hour, err := parseCronField(fields[1], 0, 23, nil)
	if err != nil {
		return nil, fmt.Errorf("hour: %s", err.Error())
	}
	dom, err := parseCronField(fields[2], 1, 31, nil)
	if err != nil {
		return nil, fmt.Errorf("day-of-month: %s", err.Error())
	}
	month, err := parseCronField(fields[3], 1, 12, monthNames)
	if err != nil {
		return nil, fmt.Errorf("month: %s", err.Error())
	}
	dow, err := parseCronField(fields[4], 0, 7, dowNames)
	if err != nil {
		return nil, fmt.Errorf("day-of-week: %s", err.Error())
	}
	// Normalise 7 -> 0 for Sunday.
	if dow.values[7] {
		dow.values[0] = true
		delete(dow.values, 7)
	}
	return &cronSpec{raw: raw, minute: minute, hour: hour, dayOfMonth: dom, month: month, dayOfWeek: dow}, nil
}

func parseCronSpecial(raw, s string) (*cronSpec, error) {
	switch strings.ToLower(s) {
	case "@hourly":
		return parseAndTag(raw, "@hourly", "0 * * * *")
	case "@daily", "@midnight":
		return parseAndTag(raw, strings.ToLower(s), "0 0 * * *")
	case "@weekly":
		return parseAndTag(raw, "@weekly", "0 0 * * 0")
	case "@monthly":
		return parseAndTag(raw, "@monthly", "0 0 1 * *")
	case "@yearly", "@annually":
		return parseAndTag(raw, strings.ToLower(s), "0 0 1 1 *")
	case "@reboot":
		return nil, fmt.Errorf("@reboot has no firing schedule")
	default:
		return nil, fmt.Errorf("unknown special spec %q", s)
	}
}

func parseAndTag(raw, special, expanded string) (*cronSpec, error) {
	spec, err := parseCron(expanded)
	if err != nil {
		return nil, err
	}
	spec.raw = raw
	spec.special = special
	return spec, nil
}

func parseCronField(field string, min, max int, names map[string]int) (fieldSet, error) {
	out := fieldSet{values: map[int]bool{}}
	if field == "*" {
		for v := min; v <= max; v++ {
			out.values[v] = true
		}
		return out, nil
	}
	out.restricted = true
	for _, part := range strings.Split(field, ",") {
		if err := mergeCronPart(part, min, max, names, &out); err != nil {
			return fieldSet{}, err
		}
	}
	if len(out.values) == 0 {
		return fieldSet{}, fmt.Errorf("field expands to empty set")
	}
	return out, nil
}

func mergeCronPart(part string, min, max int, names map[string]int, out *fieldSet) error {
	step := 1
	body := part
	if idx := strings.Index(part, "/"); idx >= 0 {
		body = part[:idx]
		stepStr := part[idx+1:]
		s, err := strconv.Atoi(stepStr)
		if err != nil || s <= 0 {
			return fmt.Errorf("invalid step %q", stepStr)
		}
		step = s
	}
	var lo, hi int
	switch {
	case body == "*":
		lo, hi = min, max
	case strings.Contains(body, "-"):
		bounds := strings.SplitN(body, "-", 2)
		l, err := lookupCronInt(bounds[0], names)
		if err != nil {
			return err
		}
		h, err := lookupCronInt(bounds[1], names)
		if err != nil {
			return err
		}
		lo, hi = l, h
	default:
		v, err := lookupCronInt(body, names)
		if err != nil {
			return err
		}
		if step > 1 {
			// `5/3` means "from 5 to max, step 3".
			lo, hi = v, max
		} else {
			lo, hi = v, v
		}
	}
	if lo < min || hi > max {
		return fmt.Errorf("value out of range [%d, %d]: got %d-%d", min, max, lo, hi)
	}
	if lo > hi {
		return fmt.Errorf("range descending: %d-%d", lo, hi)
	}
	for v := lo; v <= hi; v += step {
		out.values[v] = true
	}
	return nil
}

func lookupCronInt(s string, names map[string]int) (int, error) {
	if names != nil {
		if v, ok := names[strings.ToLower(s)]; ok {
			return v, nil
		}
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("invalid token %q", s)
	}
	return v, nil
}

var monthNames = map[string]int{
	"jan": 1, "feb": 2, "mar": 3, "apr": 4, "may": 5, "jun": 6,
	"jul": 7, "aug": 8, "sep": 9, "oct": 10, "nov": 11, "dec": 12,
}

var dowNames = map[string]int{
	"sun": 0, "mon": 1, "tue": 2, "wed": 3, "thu": 4, "fri": 5, "sat": 6,
}

// ---- nextAfter ----

func nextAfterCron(spec *cronSpec, base int64) (int64, error) {
	// Search forward minute by minute from the next minute boundary
	// after `base`. Bail after a generous horizon so a pathological
	// spec (e.g. Feb 30) can't loop forever.
	t := time.Unix(base, 0).UTC().Truncate(time.Minute).Add(time.Minute)
	const maxYears = 5
	deadline := t.AddDate(maxYears, 0, 0)
	for t.Before(deadline) {
		if cronMatches(spec, t) {
			return t.Unix(), nil
		}
		t = t.Add(time.Minute)
	}
	return 0, fmt.Errorf("no firing time found within %d years", maxYears)
}

func cronMatches(spec *cronSpec, t time.Time) bool {
	if !spec.minute.values[t.Minute()] {
		return false
	}
	if !spec.hour.values[t.Hour()] {
		return false
	}
	if !spec.month.values[int(t.Month())] {
		return false
	}
	dom := t.Day()
	dow := int(t.Weekday())
	// Vixie semantics: if BOTH dom and dow are restricted, OR them.
	// If only one is restricted, that one decides.
	switch {
	case spec.dayOfMonth.restricted && spec.dayOfWeek.restricted:
		if spec.andDays {
			return spec.dayOfMonth.values[dom] && spec.dayOfWeek.values[dow]
		}
		return spec.dayOfMonth.values[dom] || spec.dayOfWeek.values[dow]
	case spec.dayOfMonth.restricted:
		return spec.dayOfMonth.values[dom]
	case spec.dayOfWeek.restricted:
		return spec.dayOfWeek.values[dow]
	default:
		return true
	}
}
