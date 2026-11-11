package native

import (
	"fmt"
	"geblang/internal/runtime"
	nethttp "net/http"
	"time"
)

func registerDatetime(r *Registry) {
	r.Register("datetime", "nowUnix", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 0 {
			return nil, fmt.Errorf("datetime.nowUnix expects no arguments")
		}
		return runtime.NewInt64(time.Now().Unix()), nil
	})
	r.Register("datetime", "unix", func(args []runtime.Value) (runtime.Value, error) {
		seconds, err := singleInt64(args, "datetime.unix")
		if err != nil {
			return nil, err
		}
		return runtime.String{Value: time.Unix(seconds, 0).UTC().Format(time.RFC3339)}, nil
	})
	r.Register("datetime", "parse", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 1 && len(args) != 2 {
			return nil, fmt.Errorf("datetime.parse expects text and an optional layout")
		}
		text, ok := args[0].(runtime.String)
		if !ok {
			return nil, fmt.Errorf("datetime.parse text must be string")
		}
		layout := time.RFC3339
		if len(args) == 2 {
			layoutArg, ok := args[1].(runtime.String)
			if !ok {
				return nil, fmt.Errorf("datetime.parse layout must be string")
			}
			resolved, err := ResolveDateLayout(layoutArg.Value)
			if err != nil {
				return nil, fmt.Errorf("datetime.parse: %v", err)
			}
			layout = resolved
		}
		parsed, err := time.Parse(layout, text.Value)
		if err != nil {
			return nil, err
		}
		return runtime.NewInt64(parsed.Unix()), nil
	})
	r.Register("datetime", "format", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 2 {
			return nil, fmt.Errorf("datetime.format expects exactly two arguments")
		}
		sec, ok := AsInt64(args[0])
		if !ok {
			return nil, fmt.Errorf("datetime.format unix seconds must be int")
		}
		layout, ok := args[1].(runtime.String)
		if !ok {
			return nil, fmt.Errorf("datetime.format layout must be string")
		}
		goLayout, err := ResolveDateLayout(layout.Value)
		if err != nil {
			return nil, fmt.Errorf("datetime.format: %v", err)
		}
		return runtime.String{Value: time.Unix(sec, 0).UTC().Format(goLayout)}, nil
	})
	r.Register("datetime", "addSeconds", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 2 {
			return nil, fmt.Errorf("datetime.addSeconds expects exactly two arguments")
		}
		seconds, ok := AsInt64(args[0])
		if !ok {
			return nil, fmt.Errorf("datetime.addSeconds unix seconds must be int")
		}
		delta, ok := AsInt64(args[1])
		if !ok {
			return nil, fmt.Errorf("datetime.addSeconds delta must be int")
		}
		return runtime.NewInt64(seconds + delta), nil
	})
	r.Register("datetime", "addDays", func(args []runtime.Value) (runtime.Value, error) {
		seconds, days, err := twoInt64(args, "datetime.addDays")
		if err != nil {
			return nil, err
		}
		return runtime.NewInt64(time.Unix(seconds, 0).UTC().AddDate(0, 0, int(days)).Unix()), nil
	})
	r.Register("datetime", "addMonths", func(args []runtime.Value) (runtime.Value, error) {
		seconds, months, err := twoInt64(args, "datetime.addMonths")
		if err != nil {
			return nil, err
		}
		return runtime.NewInt64(time.Unix(seconds, 0).UTC().AddDate(0, int(months), 0).Unix()), nil
	})
	r.Register("datetime", "addYears", func(args []runtime.Value) (runtime.Value, error) {
		seconds, years, err := twoInt64(args, "datetime.addYears")
		if err != nil {
			return nil, err
		}
		return runtime.NewInt64(time.Unix(seconds, 0).UTC().AddDate(int(years), 0, 0).Unix()), nil
	})
	r.Register("datetime", "diff", func(args []runtime.Value) (runtime.Value, error) {
		start, end, err := twoInt64(args, "datetime.diff")
		if err != nil {
			return nil, err
		}
		delta := end - start
		if delta < 0 {
			delta = -delta
		}
		days := delta / 86400
		delta %= 86400
		hours := delta / 3600
		delta %= 3600
		minutes := delta / 60
		seconds := delta % 60
		return stringIntDict(map[string]int64{
			"days":    days,
			"hours":   hours,
			"minutes": minutes,
			"seconds": seconds,
		}), nil
	})
	r.Register("datetime", "toLocal", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 2 {
			return nil, fmt.Errorf("datetime.toLocal expects unix seconds and timezone")
		}
		sec, ok := AsInt64(args[0])
		if !ok {
			return nil, fmt.Errorf("datetime.toLocal unix seconds must be int")
		}
		tz, ok := args[1].(runtime.String)
		if !ok {
			return nil, fmt.Errorf("datetime.toLocal timezone must be string")
		}
		location, err := time.LoadLocation(tz.Value)
		if err != nil {
			return nil, fmt.Errorf("datetime.toLocal: %v", err)
		}
		return runtime.String{Value: time.Unix(sec, 0).In(location).Format(time.RFC3339)}, nil
	})
	r.Register("datetime", "toUtc", func(args []runtime.Value) (runtime.Value, error) {
		seconds, err := singleInt64(args, "datetime.toUtc")
		if err != nil {
			return nil, err
		}
		return runtime.String{Value: time.Unix(seconds, 0).UTC().Format(time.RFC3339)}, nil
	})
	r.Register("datetime", "now", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) > 1 {
			return nil, fmt.Errorf("datetime.now expects an optional timezone name")
		}
		if len(args) == 1 {
			tz, ok := args[0].(runtime.String)
			if !ok {
				return nil, fmt.Errorf("datetime.now timezone must be string")
			}
			loc, err := time.LoadLocation(tz.Value)
			if err != nil {
				return nil, fmt.Errorf("datetime.now: %v", err)
			}
			return timePartsDictWithZone(time.Now().In(loc), tz.Value), nil
		}
		return timePartsDictWithZone(time.Now().UTC(), "UTC"), nil
	})
	r.Register("datetime", "partsInZone", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 2 {
			return nil, fmt.Errorf("datetime.partsInZone expects unix seconds and timezone")
		}
		sec, ok := AsInt64(args[0])
		if !ok {
			return nil, fmt.Errorf("datetime.partsInZone unix seconds must be int")
		}
		tz, ok := args[1].(runtime.String)
		if !ok {
			return nil, fmt.Errorf("datetime.partsInZone timezone must be string")
		}
		loc, err := time.LoadLocation(tz.Value)
		if err != nil {
			return nil, fmt.Errorf("datetime.partsInZone: %v", err)
		}
		return timePartsDictWithZone(time.Unix(sec, 0).In(loc), tz.Value), nil
	})
	r.Register("datetime", "formatHTTP", func(args []runtime.Value) (runtime.Value, error) {
		seconds, err := singleInt64(args, "datetime.formatHTTP")
		if err != nil {
			return nil, err
		}
		return runtime.String{Value: time.Unix(seconds, 0).UTC().Format(nethttp.TimeFormat)}, nil
	})
	r.Register("datetime", "nowInstant", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 0 {
			return nil, fmt.Errorf("datetime.nowInstant expects no arguments")
		}
		return runtime.DateTimeInstant{Unix: time.Now().Unix()}, nil
	})
	r.Register("datetime", "Instant", func(args []runtime.Value) (runtime.Value, error) {
		switch len(args) {
		case 0:
			return runtime.DateTimeInstant{Unix: time.Now().Unix()}, nil
		case 1:
			switch value := args[0].(type) {
			case runtime.DateTimeInstant:
				return value, nil // value type: returning it is the copy
			case runtime.SmallInt:
				return runtime.DateTimeInstant{Unix: value.Value}, nil
			case runtime.Int:
				if !value.Value.IsInt64() {
					return nil, fmt.Errorf("datetime.Instant unix seconds must fit int64")
				}
				return runtime.DateTimeInstant{Unix: value.Value.Int64()}, nil
			case runtime.String:
				parsed, err := time.Parse(time.RFC3339, value.Value)
				if err != nil {
					return nil, fmt.Errorf("datetime.Instant: %v", err)
				}
				return runtime.DateTimeInstant{Unix: parsed.Unix()}, nil
			default:
				return nil, fmt.Errorf("datetime.Instant expects int, string, or datetime.Instant")
			}
		case 3, 4, 5, 6:
			ints := make([]int64, 6)
			for i, a := range args {
				v, ok := AsInt64(a)
				if !ok {
					return nil, fmt.Errorf("datetime.Instant calendar arguments must be int")
				}
				ints[i] = v
			}
			t := time.Date(int(ints[0]), time.Month(ints[1]), int(ints[2]), int(ints[3]), int(ints[4]), int(ints[5]), 0, time.UTC)
			return runtime.DateTimeInstant{Unix: t.Unix()}, nil
		default:
			return nil, fmt.Errorf("datetime.Instant expects 0 args (now), 1 (unix/string/Instant), or 3-6 (year, month, day[, hour, minute, second])")
		}
	})
	r.Register("datetime", "Duration", func(args []runtime.Value) (runtime.Value, error) {
		seconds, err := singleInt64(args, "datetime.Duration")
		if err != nil {
			return nil, err
		}
		return runtime.DateTimeDuration{Seconds: seconds}, nil
	})
	r.Register("datetime", "Zone", func(args []runtime.Value) (runtime.Value, error) {
		name, err := singleString(args, "datetime.Zone")
		if err != nil {
			return nil, err
		}
		if _, err := time.LoadLocation(name); err != nil {
			return nil, fmt.Errorf("datetime.Zone: %v", err)
		}
		return runtime.DateTimeZone{Name: name}, nil
	})
	r.Register("datetime", "sleep", func(args []runtime.Value) (runtime.Value, error) {
		ms, err := singleInt64(args, "datetime.sleep")
		if err != nil {
			return nil, err
		}
		time.Sleep(time.Duration(ms) * time.Millisecond)
		return runtime.Null{}, nil
	})
	r.Register("datetime", "make", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) < 3 || len(args) > 6 {
			return nil, fmt.Errorf("datetime.make expects 3 to 6 arguments (year, month, day[, hour, minute, second])")
		}
		ints := make([]int64, 6)
		for i, a := range args {
			v, ok := AsInt64(a)
			if !ok {
				return nil, fmt.Errorf("datetime.make arguments must be int")
			}
			ints[i] = v
		}
		t := time.Date(int(ints[0]), time.Month(ints[1]), int(ints[2]), int(ints[3]), int(ints[4]), int(ints[5]), 0, time.UTC)
		return runtime.NewInt64(t.Unix()), nil
	})
	r.Register("datetime", "formatRFC3339", func(args []runtime.Value) (runtime.Value, error) {
		seconds, err := singleInt64(args, "datetime.formatRFC3339")
		if err != nil {
			return nil, err
		}
		return runtime.String{Value: time.Unix(seconds, 0).UTC().Format(time.RFC3339)}, nil
	})
	r.Register("datetime", "formatDate", func(args []runtime.Value) (runtime.Value, error) {
		seconds, err := singleInt64(args, "datetime.formatDate")
		if err != nil {
			return nil, err
		}
		return runtime.String{Value: time.Unix(seconds, 0).UTC().Format("2006-01-02")}, nil
	})
	r.Register("datetime", "formatTime", func(args []runtime.Value) (runtime.Value, error) {
		seconds, err := singleInt64(args, "datetime.formatTime")
		if err != nil {
			return nil, err
		}
		return runtime.String{Value: time.Unix(seconds, 0).UTC().Format("15:04:05")}, nil
	})
	r.Register("datetime", "parseRFC3339", func(args []runtime.Value) (runtime.Value, error) {
		text, err := singleString(args, "datetime.parseRFC3339")
		if err != nil {
			return nil, err
		}
		parsed, err := time.Parse(time.RFC3339, text)
		if err != nil {
			return nil, fmt.Errorf("datetime.parseRFC3339: %v", err)
		}
		return runtime.NewInt64(parsed.Unix()), nil
	})
	r.Register("datetime", "weekdayName", func(args []runtime.Value) (runtime.Value, error) {
		n, err := singleInt64(args, "datetime.weekdayName")
		if err != nil {
			return nil, err
		}
		names := []string{"Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"}
		idx := int(n) % 7
		if idx < 0 {
			idx += 7
		}
		return runtime.String{Value: names[idx]}, nil
	})
	r.Register("datetime", "monthName", func(args []runtime.Value) (runtime.Value, error) {
		n, err := singleInt64(args, "datetime.monthName")
		if err != nil {
			return nil, err
		}
		if n < 1 || n > 12 {
			return nil, fmt.Errorf("datetime.monthName month must be between 1 and 12")
		}
		return runtime.String{Value: time.Month(n).String()}, nil
	})
}

func DateTimeInstantMethod(receiver runtime.DateTimeInstant, name string, args []runtime.Value) (runtime.Value, error) {
	switch name {
	case "copy":
		if len(args) != 0 {
			return nil, fmt.Errorf("datetime.Instant.copy expects no arguments")
		}
		return receiver, nil // value type: the returned value is the copy
	case "unix", "toUnix":
		if len(args) != 0 {
			return nil, fmt.Errorf("datetime.Instant.%s expects no arguments", name)
		}
		return runtime.NewInt64(receiver.Unix), nil
	case "toUnixMillis":
		if len(args) != 0 {
			return nil, fmt.Errorf("datetime.Instant.toUnixMillis expects no arguments")
		}
		return runtime.NewInt64(receiver.Unix * 1000), nil
	case "toUnixNanos":
		if len(args) != 0 {
			return nil, fmt.Errorf("datetime.Instant.toUnixNanos expects no arguments")
		}
		return runtime.NewInt64(receiver.Unix * 1_000_000_000), nil
	case "toString", "formatRFC3339", "toUtc":
		if len(args) != 0 {
			return nil, fmt.Errorf("datetime.Instant.%s expects no arguments", name)
		}
		return runtime.String{Value: time.Unix(receiver.Unix, 0).UTC().Format(time.RFC3339)}, nil
	case "formatHTTP":
		if len(args) != 0 {
			return nil, fmt.Errorf("datetime.Instant.formatHTTP expects no arguments")
		}
		return runtime.String{Value: time.Unix(receiver.Unix, 0).UTC().Format(nethttp.TimeFormat)}, nil
	case "format":
		if len(args) != 1 {
			return nil, fmt.Errorf("datetime.Instant.format expects layout")
		}
		layout, ok := args[0].(runtime.String)
		if !ok {
			return nil, fmt.Errorf("datetime.Instant.format layout must be string")
		}
		goLayout, err := ResolveDateLayout(layout.Value)
		if err != nil {
			return nil, fmt.Errorf("datetime.Instant.format: %v", err)
		}
		return runtime.String{Value: time.Unix(receiver.Unix, 0).UTC().Format(goLayout)}, nil
	case "toLocal":
		if len(args) != 1 {
			return nil, fmt.Errorf("datetime.Instant.toLocal expects timezone")
		}
		location, err := datetimeLocation(args[0])
		if err != nil {
			return nil, fmt.Errorf("datetime.Instant.toLocal: %v", err)
		}
		return runtime.String{Value: time.Unix(receiver.Unix, 0).In(location).Format(time.RFC3339)}, nil
	case "add":
		if len(args) != 1 {
			return nil, fmt.Errorf("datetime.Instant.add expects datetime.Duration")
		}
		duration, ok := args[0].(runtime.DateTimeDuration)
		if !ok {
			return nil, fmt.Errorf("datetime.Instant.add expects datetime.Duration")
		}
		return runtime.DateTimeInstant{Unix: receiver.Unix + duration.Seconds}, nil
	case "addSeconds":
		seconds, err := singleInt64(args, "datetime.Instant.addSeconds")
		if err != nil {
			return nil, err
		}
		return runtime.DateTimeInstant{Unix: receiver.Unix + seconds}, nil
	case "addDays":
		days, err := singleInt64(args, "datetime.Instant.addDays")
		if err != nil {
			return nil, err
		}
		return runtime.DateTimeInstant{Unix: time.Unix(receiver.Unix, 0).UTC().AddDate(0, 0, int(days)).Unix()}, nil
	case "addMonths":
		months, err := singleInt64(args, "datetime.Instant.addMonths")
		if err != nil {
			return nil, err
		}
		return runtime.DateTimeInstant{Unix: time.Unix(receiver.Unix, 0).UTC().AddDate(0, int(months), 0).Unix()}, nil
	case "addYears":
		years, err := singleInt64(args, "datetime.Instant.addYears")
		if err != nil {
			return nil, err
		}
		return runtime.DateTimeInstant{Unix: time.Unix(receiver.Unix, 0).UTC().AddDate(int(years), 0, 0).Unix()}, nil
	case "diff":
		if len(args) != 1 {
			return nil, fmt.Errorf("datetime.Instant.diff expects datetime.Instant")
		}
		other, ok := args[0].(runtime.DateTimeInstant)
		if !ok {
			return nil, fmt.Errorf("datetime.Instant.diff expects datetime.Instant")
		}
		delta := other.Unix - receiver.Unix
		if delta < 0 {
			delta = -delta
		}
		return runtime.DateTimeDuration{Seconds: delta}, nil
	case "sub":
		if len(args) != 1 {
			return nil, fmt.Errorf("datetime.Instant.sub expects datetime.Duration")
		}
		duration, ok := args[0].(runtime.DateTimeDuration)
		if !ok {
			return nil, fmt.Errorf("datetime.Instant.sub expects datetime.Duration")
		}
		return runtime.DateTimeInstant{Unix: receiver.Unix - duration.Seconds}, nil
	case "isBefore", "isAfter", "equals":
		if len(args) != 1 {
			return nil, fmt.Errorf("datetime.Instant.%s expects datetime.Instant", name)
		}
		other, ok := args[0].(runtime.DateTimeInstant)
		if !ok {
			return nil, fmt.Errorf("datetime.Instant.%s expects datetime.Instant", name)
		}
		switch name {
		case "isBefore":
			return runtime.Bool{Value: receiver.Unix < other.Unix}, nil
		case "isAfter":
			return runtime.Bool{Value: receiver.Unix > other.Unix}, nil
		default:
			return runtime.Bool{Value: receiver.Unix == other.Unix}, nil
		}
	case "inZone":
		if len(args) != 1 {
			return nil, fmt.Errorf("datetime.Instant.inZone expects a zone")
		}
		location, err := datetimeLocation(args[0])
		if err != nil {
			return nil, fmt.Errorf("datetime.Instant.inZone: %v", err)
		}
		return timePartsDictWithZone(time.Unix(receiver.Unix, 0).In(location), location.String()), nil
	case "parts":
		if len(args) != 0 {
			return nil, fmt.Errorf("datetime.Instant.parts expects no arguments")
		}
		return timePartsDict(time.Unix(receiver.Unix, 0).UTC()), nil
	case "year", "month", "day", "hour", "minute", "second", "weekday", "dayOfYear", "isWeekend":
		if len(args) != 0 {
			return nil, fmt.Errorf("datetime.Instant.%s expects no arguments", name)
		}
		t := time.Unix(receiver.Unix, 0).UTC()
		switch name {
		case "year":
			return runtime.NewInt64(int64(t.Year())), nil
		case "month":
			return runtime.NewInt64(int64(t.Month())), nil
		case "day":
			return runtime.NewInt64(int64(t.Day())), nil
		case "hour":
			return runtime.NewInt64(int64(t.Hour())), nil
		case "minute":
			return runtime.NewInt64(int64(t.Minute())), nil
		case "second":
			return runtime.NewInt64(int64(t.Second())), nil
		case "weekday":
			return runtime.NewInt64(int64(isoWeekday(t))), nil
		case "dayOfYear":
			return runtime.NewInt64(int64(t.YearDay())), nil
		default:
			wd := t.Weekday()
			return runtime.Bool{Value: wd == time.Saturday || wd == time.Sunday}, nil
		}
	default:
		return nil, fmt.Errorf("datetime.Instant has no method %s", name)
	}
}

func DateTimeDurationMethod(receiver runtime.DateTimeDuration, name string, args []runtime.Value) (runtime.Value, error) {
	switch name {
	case "seconds", "inSeconds":
		if len(args) != 0 {
			return nil, fmt.Errorf("datetime.Duration.%s expects no arguments", name)
		}
		return runtime.NewInt64(receiver.Seconds), nil
	case "inMillis":
		if len(args) != 0 {
			return nil, fmt.Errorf("datetime.Duration.inMillis expects no arguments")
		}
		return runtime.NewInt64(receiver.Seconds * 1000), nil
	case "inNanos":
		if len(args) != 0 {
			return nil, fmt.Errorf("datetime.Duration.inNanos expects no arguments")
		}
		return runtime.NewInt64(receiver.Seconds * 1_000_000_000), nil
	case "abs":
		if len(args) != 0 {
			return nil, fmt.Errorf("datetime.Duration.abs expects no arguments")
		}
		s := receiver.Seconds
		if s < 0 {
			s = -s
		}
		return runtime.DateTimeDuration{Seconds: s}, nil
	case "negate":
		if len(args) != 0 {
			return nil, fmt.Errorf("datetime.Duration.negate expects no arguments")
		}
		return runtime.DateTimeDuration{Seconds: -receiver.Seconds}, nil
	case "add", "sub":
		if len(args) != 1 {
			return nil, fmt.Errorf("datetime.Duration.%s expects datetime.Duration", name)
		}
		other, ok := args[0].(runtime.DateTimeDuration)
		if !ok {
			return nil, fmt.Errorf("datetime.Duration.%s expects datetime.Duration", name)
		}
		if name == "add" {
			return runtime.DateTimeDuration{Seconds: receiver.Seconds + other.Seconds}, nil
		}
		return runtime.DateTimeDuration{Seconds: receiver.Seconds - other.Seconds}, nil
	case "toDict":
		if len(args) != 0 {
			return nil, fmt.Errorf("datetime.Duration.toDict expects no arguments")
		}
		return durationPartsDict(receiver.Seconds), nil
	case "toString":
		if len(args) != 0 {
			return nil, fmt.Errorf("datetime.Duration.toString expects no arguments")
		}
		return runtime.String{Value: fmt.Sprintf("%ds", receiver.Seconds)}, nil
	default:
		return nil, fmt.Errorf("datetime.Duration has no method %s", name)
	}
}

func DateTimeZoneMethod(receiver runtime.DateTimeZone, name string, args []runtime.Value) (runtime.Value, error) {
	switch name {
	case "name", "toString":
		if len(args) != 0 {
			return nil, fmt.Errorf("datetime.Zone.%s expects no arguments", name)
		}
		return runtime.String{Value: receiver.Name}, nil
	case "offset":
		if len(args) != 0 {
			return nil, fmt.Errorf("datetime.Zone.offset expects no arguments")
		}
		location, err := time.LoadLocation(receiver.Name)
		if err != nil {
			return nil, fmt.Errorf("datetime.Zone.offset: %v", err)
		}
		_, offset := time.Now().In(location).Zone()
		return runtime.NewInt64(int64(offset)), nil
	case "offsetAt":
		if len(args) != 1 {
			return nil, fmt.Errorf("datetime.Zone.offsetAt expects datetime.Instant")
		}
		instant, ok := args[0].(runtime.DateTimeInstant)
		if !ok {
			return nil, fmt.Errorf("datetime.Zone.offsetAt expects datetime.Instant")
		}
		location, err := time.LoadLocation(receiver.Name)
		if err != nil {
			return nil, fmt.Errorf("datetime.Zone.offsetAt: %v", err)
		}
		_, offset := time.Unix(instant.Unix, 0).In(location).Zone()
		return runtime.NewInt64(int64(offset)), nil
	default:
		return nil, fmt.Errorf("datetime.Zone has no method %s", name)
	}
}

func datetimeLocation(value runtime.Value) (*time.Location, error) {
	switch value := value.(type) {
	case runtime.String:
		return time.LoadLocation(value.Value)
	case runtime.DateTimeZone:
		return time.LoadLocation(value.Name)
	default:
		return nil, fmt.Errorf("timezone must be string or datetime.Zone")
	}
}

func durationPartsDict(seconds int64) runtime.Value {
	if seconds < 0 {
		seconds = -seconds
	}
	days := seconds / 86400
	seconds %= 86400
	hours := seconds / 3600
	seconds %= 3600
	minutes := seconds / 60
	seconds %= 60
	return stringIntDict(map[string]int64{
		"days":    days,
		"hours":   hours,
		"minutes": minutes,
		"seconds": seconds,
	})
}

func twoInt64(args []runtime.Value, label string) (int64, int64, error) {
	if len(args) != 2 {
		return 0, 0, fmt.Errorf("%s expects exactly two integer arguments", label)
	}
	l, ok := AsInt64(args[0])
	if !ok {
		return 0, 0, fmt.Errorf("%s first argument must be int", label)
	}
	r, ok := AsInt64(args[1])
	if !ok {
		return 0, 0, fmt.Errorf("%s second argument must be int", label)
	}
	return l, r, nil
}

func stringIntDict(values map[string]int64) runtime.Dict {
	entries := map[string]runtime.DictEntry{}
	for key, value := range values {
		keyValue := runtime.String{Value: key}
		entries[DictKey(keyValue)] = runtime.DictEntry{Key: keyValue, Value: runtime.NewInt64(value)}
	}
	return runtime.DictFromEntries(entries, nil)
}

func timePartsDict(value time.Time) runtime.Dict {
	return stringIntDict(map[string]int64{
		"timestamp": int64(value.Unix()),
		"year":      int64(value.Year()),
		"month":     int64(value.Month()),
		"day":       int64(value.Day()),
		"hour":      int64(value.Hour()),
		"minute":    int64(value.Minute()),
		"second":    int64(value.Second()),
		"weekday":   int64(value.Weekday()),
	})
}

// timePartsDictWithZone is like timePartsDict but also records the zone
// name string so source-level wrappers can preserve it without a second
// lookup.
func timePartsDictWithZone(value time.Time, zone string) runtime.Dict {
	intParts := map[string]int64{
		"timestamp": int64(value.Unix()),
		"year":      int64(value.Year()),
		"month":     int64(value.Month()),
		"day":       int64(value.Day()),
		"hour":      int64(value.Hour()),
		"minute":    int64(value.Minute()),
		"second":    int64(value.Second()),
		"weekday":   int64(value.Weekday()),
	}
	entries := map[string]runtime.DictEntry{}
	for k, v := range intParts {
		key := runtime.String{Value: k}
		entries[DictKey(key)] = runtime.DictEntry{Key: key, Value: runtime.NewInt64(v)}
	}
	zoneKey := runtime.String{Value: "zone"}
	entries[DictKey(zoneKey)] = runtime.DictEntry{Key: zoneKey, Value: runtime.String{Value: zone}}
	return runtime.DictFromEntries(entries, nil)
}
