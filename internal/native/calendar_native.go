package native

import (
	"fmt"
	"math"
	"sort"
	"time"

	"geblang/internal/runtime"
)

func registerCalendarNative(r *Registry) {
	r.Register("calendar_native", "addPeriod", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 5 {
			return nil, fmt.Errorf("calendar_native.addPeriod expects instant, years, months, days, zone")
		}
		values := [4]int64{}
		for i := range values {
			value, ok := AsInt64(args[i])
			if !ok {
				return nil, fmt.Errorf("calendar_native.addPeriod arguments must be int")
			}
			values[i] = value
		}
		location, err := calendarLocation(args[4])
		if err != nil {
			return nil, err
		}
		local := time.Unix(values[0], 0).In(location)
		year, month, day := local.Date()
		year += int(values[1])
		day = calendarClampDay(year, month, day)
		target := time.Date(year, month+time.Month(values[2]), 1, 0, 0, 0, 0, time.UTC)
		year, month = target.Year(), target.Month()
		day = calendarClampDay(year, month, day)
		target = time.Date(year, month, day+int(values[3]), 0, 0, 0, 0, time.UTC)
		resolved, err := calendarResolve(target.Year(), target.Month(), target.Day(),
			local.Hour(), local.Minute(), local.Second(), location)
		if err != nil {
			return nil, fmt.Errorf("calendar_native.addPeriod: %w", err)
		}
		return runtime.NewInt64(resolved), nil
	})
	r.Register("calendar_native", "candidate", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 6 {
			return nil, fmt.Errorf("calendar_native.candidate expects start, frequency, interval, index, zone, weekdays")
		}
		start, ok := AsInt64(args[0])
		if !ok {
			return nil, fmt.Errorf("calendar_native.candidate start must be int")
		}
		frequency, ok := args[1].(runtime.String)
		if !ok {
			return nil, fmt.Errorf("calendar_native.candidate frequency must be string")
		}
		interval, ok := AsInt64(args[2])
		if !ok || interval <= 0 {
			return nil, fmt.Errorf("calendar_native.candidate interval must be positive")
		}
		index, ok := AsInt64(args[3])
		if !ok || index < 0 {
			return nil, fmt.Errorf("calendar_native.candidate index must be nonnegative")
		}
		if index > math.MaxInt64/interval/7 {
			return nil, fmt.Errorf("calendar_native.candidate range is too large")
		}
		location, err := calendarLocation(args[4])
		if err != nil {
			return nil, err
		}
		weekdays, err := calendarWeekdays(args[5])
		if err != nil {
			return nil, err
		}
		local := time.Unix(start, 0).In(location)
		year, month, day := local.Date()
		var target time.Time
		switch frequency.Value {
		case "daily":
			target = time.Date(year, month, day+int(index*interval), 0, 0, 0, 0, time.UTC)
		case "weekly":
			if len(weekdays) == 0 {
				weekdays = []int{calendarISOWeekday(local)}
			}
			week := index / int64(len(weekdays))
			offset := int(week*interval*7) + weekdays[index%int64(len(weekdays))] - calendarISOWeekday(local)
			target = time.Date(year, month, day+offset, 0, 0, 0, 0, time.UTC)
		case "monthly":
			target = time.Date(year, month+time.Month(index*interval), 1, 0, 0, 0, 0, time.UTC)
			if day > calendarDaysInMonth(target.Year(), target.Month()) {
				return runtime.Null{}, nil
			}
			target = target.AddDate(0, 0, day-1)
		case "yearly":
			target = time.Date(year+int(index*interval), month, 1, 0, 0, 0, 0, time.UTC)
			if day > calendarDaysInMonth(target.Year(), target.Month()) {
				return runtime.Null{}, nil
			}
			target = target.AddDate(0, 0, day-1)
		default:
			return nil, fmt.Errorf("calendar_native.candidate unsupported frequency %s", frequency.Value)
		}
		resolved, err := calendarResolve(target.Year(), target.Month(), target.Day(),
			local.Hour(), local.Minute(), local.Second(), location)
		if err != nil {
			return nil, fmt.Errorf("calendar_native.candidate: %w", err)
		}
		return runtime.NewInt64(resolved), nil
	})
	r.Register("calendar_native", "indexNear", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 6 {
			return nil, fmt.Errorf("calendar_native.indexNear expects start, from, frequency, interval, zone, weekdays")
		}
		start, ok := AsInt64(args[0])
		if !ok {
			return nil, fmt.Errorf("calendar_native.indexNear start must be int")
		}
		from, ok := AsInt64(args[1])
		if !ok {
			return nil, fmt.Errorf("calendar_native.indexNear from must be int")
		}
		frequency, ok := args[2].(runtime.String)
		if !ok {
			return nil, fmt.Errorf("calendar_native.indexNear frequency must be string")
		}
		interval, ok := AsInt64(args[3])
		if !ok || interval <= 0 {
			return nil, fmt.Errorf("calendar_native.indexNear interval must be positive")
		}
		location, err := calendarLocation(args[4])
		if err != nil {
			return nil, err
		}
		weekdays, err := calendarWeekdays(args[5])
		if err != nil {
			return nil, err
		}
		a := time.Unix(start, 0).In(location)
		b := time.Unix(from, 0).In(location)
		var index int64
		switch frequency.Value {
		case "daily":
			index = calendarDayNumber(b) - calendarDayNumber(a)
			index = index/interval - 1
		case "weekly":
			width := int64(len(weekdays))
			if width == 0 {
				width = 1
			}
			index = ((calendarDayNumber(b)-calendarDayNumber(a))/7/interval - 1) * width
		case "monthly":
			index = int64((b.Year()-a.Year())*12+int(b.Month()-a.Month()))/interval - 1
		case "yearly":
			index = int64(b.Year()-a.Year())/interval - 1
		default:
			return nil, fmt.Errorf("calendar_native.indexNear unsupported frequency %s", frequency.Value)
		}
		if index < 0 {
			index = 0
		}
		return runtime.NewInt64(index), nil
	})
	r.Register("calendar_native", "localDate", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 2 {
			return nil, fmt.Errorf("calendar_native.localDate expects instant and zone")
		}
		unix, ok := AsInt64(args[0])
		if !ok {
			return nil, fmt.Errorf("calendar_native.localDate instant must be int")
		}
		location, err := calendarLocation(args[1])
		if err != nil {
			return nil, err
		}
		local := time.Unix(unix, 0).In(location)
		return mailDict(map[string]runtime.Value{
			"date":    runtime.String{Value: local.Format("2006-01-02")},
			"weekday": runtime.NewInt64(int64(calendarISOWeekday(local))),
		}), nil
	})
	r.Register("calendar_native", "validDate", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 1 {
			return nil, fmt.Errorf("calendar_native.validDate expects a date string")
		}
		value, ok := args[0].(runtime.String)
		if !ok {
			return nil, fmt.Errorf("calendar_native.validDate expects a date string")
		}
		parsed, err := time.Parse("2006-01-02", value.Value)
		return runtime.Bool{Value: err == nil && parsed.Format("2006-01-02") == value.Value}, nil
	})
}

func calendarLocation(value runtime.Value) (*time.Location, error) {
	name, ok := value.(runtime.String)
	if !ok {
		return nil, fmt.Errorf("calendar: zone must be a string")
	}
	location, err := time.LoadLocation(name.Value)
	if err != nil {
		return nil, fmt.Errorf("calendar: invalid zone %s: %w", name.Value, err)
	}
	return location, nil
}

func calendarWeekdays(value runtime.Value) ([]int, error) {
	items, ok := value.(*runtime.List)
	if !ok {
		return nil, fmt.Errorf("calendar: weekdays must be a list")
	}
	days := make([]int, len(items.Elements))
	for i, item := range items.Elements {
		day, ok := AsInt64(item)
		if !ok || day < 1 || day > 7 {
			return nil, fmt.Errorf("calendar: weekdays must contain ISO days 1-7")
		}
		days[i] = int(day)
	}
	sort.Ints(days)
	for i := 1; i < len(days); i++ {
		if days[i] == days[i-1] {
			return nil, fmt.Errorf("calendar: duplicate weekday")
		}
	}
	return days, nil
}

func calendarDaysInMonth(year int, month time.Month) int {
	return time.Date(year, month+1, 0, 0, 0, 0, 0, time.UTC).Day()
}

func calendarClampDay(year int, month time.Month, day int) int {
	if last := calendarDaysInMonth(year, month); day > last {
		return last
	}
	return day
}

func calendarISOWeekday(value time.Time) int {
	weekday := int(value.Weekday())
	if weekday == 0 {
		return 7
	}
	return weekday
}

func calendarDayNumber(value time.Time) int64 {
	seconds := time.Date(value.Year(), value.Month(), value.Day(), 0, 0, 0, 0, time.UTC).Unix()
	if seconds < 0 {
		return (seconds - 86399) / 86400
	}
	return seconds / 86400
}

func calendarResolve(year int, month time.Month, day, hour, minute, second int, location *time.Location) (int64, error) {
	wall := time.Date(year, month, day, hour, minute, second, 0, time.UTC).Unix()
	offsets := map[int]struct{}{}
	for hours := -48; hours <= 48; hours += 6 {
		_, offset := time.Unix(wall+int64(hours)*3600, 0).In(location).Zone()
		offsets[offset] = struct{}{}
	}
	var selected int64
	found := false
	for offset := range offsets {
		candidate := wall - int64(offset)
		local := time.Unix(candidate, 0).In(location)
		if local.Year() == year && local.Month() == month && local.Day() == day &&
			local.Hour() == hour && local.Minute() == minute && local.Second() == second {
			if !found || candidate < selected {
				selected = candidate
				found = true
			}
		}
	}
	if found {
		return selected, nil
	}
	previous := wall - 48*3600
	_, before := time.Unix(previous, 0).In(location).Zone()
	for next := previous + 6*3600; next <= wall+48*3600; next += 6 * 3600 {
		_, after := time.Unix(next, 0).In(location).Zone()
		if after > before {
			low, high := previous, next
			for high-low > 1 {
				mid := low + (high-low)/2
				_, offset := time.Unix(mid, 0).In(location).Zone()
				if offset == before {
					low = mid
				} else {
					high = mid
				}
			}
			if wall >= high+int64(before) && wall < high+int64(after) {
				return high, nil
			}
		}
		previous, before = next, after
	}
	return 0, fmt.Errorf("cannot resolve local time")
}
