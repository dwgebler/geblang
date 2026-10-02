package native

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"strconv"
	"strings"
	"time"

	"golang.org/x/text/collate"
	"golang.org/x/text/currency"
	"golang.org/x/text/feature/plural"
	"golang.org/x/text/language"
	"golang.org/x/text/message"

	"geblang/internal/runtime"
)

type localeDateData struct {
	Formats         map[string]string `json:"formats"`
	MonthsWide      []string          `json:"monthsWide"`
	MonthsShort     []string          `json:"monthsShort"`
	DaysWide        []string          `json:"daysWide"`
	Group           string            `json:"group"`
	Decimal         string            `json:"decimal"`
	Minus           string            `json:"minus"`
	CurrencyPattern string            `json:"currencyPattern"`
}

// CLDR 32 date and number data match the version pinned by x/text v0.37.0.
//
//go:embed locale_dates.json
var localeDatesJSON []byte

var localeDates = func() map[string]localeDateData {
	var data map[string]localeDateData
	if err := json.Unmarshal(localeDatesJSON, &data); err != nil {
		panic(err)
	}
	return data
}()

func registerLocale(r *Registry) {
	r.Register("locale", "canonicalTag", localeCanonicalTag)
	r.Register("locale", "formatNumber", localeFormatNumber)
	r.Register("locale", "formatCurrency", localeFormatCurrency)
	r.Register("locale", "formatDate", localeFormatDate)
	r.Register("locale", "compare", localeCompare)
	r.Register("locale", "pluralCategory", localePluralCategory)
	r.Register("i18n_native", "interpolate", localeInterpolate)
}

func localeTag(v runtime.Value) (language.Tag, error) {
	s, ok := v.(runtime.String)
	if !ok {
		return language.Und, fmt.Errorf("locale: tag must be a string")
	}
	if s.Value == "" {
		return language.Und, fmt.Errorf("locale: tag is empty")
	}
	tag, err := language.Parse(s.Value)
	if err != nil || tag == language.Und {
		return language.Und, fmt.Errorf("locale: invalid BCP 47 tag %q", s.Value)
	}
	base, _ := tag.Base()
	switch base.String() {
	case "en", "fr", "de":
		return tag, nil
	default:
		return language.Und, fmt.Errorf("locale: unsupported tag %q", s.Value)
	}
}

func localeCanonicalTag(args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("locale.canonicalTag expects one tag")
	}
	tag, err := localeTag(args[0])
	if err != nil {
		return nil, err
	}
	return runtime.String{Value: tag.String()}, nil
}

func localeOptions(v runtime.Value, label string) (runtime.Dict, error) {
	opts, ok := v.(runtime.Dict)
	if !ok {
		return runtime.Dict{}, fmt.Errorf("%s options must be a dictionary", label)
	}
	return opts, nil
}

func localeDigitsOption(opts runtime.Dict, key string, fallback int) (int, error) {
	value, exists := dictLookup(opts, key)
	if !exists {
		return fallback, nil
	}
	n, ok := AsInt64(value)
	if !ok || n < 0 || n > 100 {
		return 0, fmt.Errorf("locale: %s must be an integer from 0 to 100", key)
	}
	return int(n), nil
}

func localeNumberOptions(opts runtime.Dict, defaultMin, defaultMax int) (int, int, bool, error) {
	minimum, err := localeDigitsOption(opts, "minFractionDigits", defaultMin)
	if err != nil {
		return 0, 0, false, err
	}
	maximum, err := localeDigitsOption(opts, "maxFractionDigits", defaultMax)
	if err != nil {
		return 0, 0, false, err
	}
	if minimum > maximum {
		return 0, 0, false, fmt.Errorf("locale: minFractionDigits exceeds maxFractionDigits")
	}
	grouping := true
	if value, exists := dictLookup(opts, "grouping"); exists {
		flag, ok := value.(runtime.Bool)
		if !ok {
			return 0, 0, false, fmt.Errorf("locale: grouping must be bool")
		}
		grouping = flag.Value
	}
	return minimum, maximum, grouping, nil
}

func localeNumericValue(v runtime.Value) (*big.Rat, error) {
	switch n := v.(type) {
	case runtime.SmallInt:
		return new(big.Rat).SetInt64(n.Value), nil
	case runtime.Int:
		return new(big.Rat).SetInt(n.Value), nil
	case runtime.Decimal:
		return new(big.Rat).Set(n.Value), nil
	case runtime.Float:
		if math.IsNaN(n.Value) || math.IsInf(n.Value, 0) {
			return nil, fmt.Errorf("locale: number must be finite")
		}
		text := strconv.FormatFloat(n.Value, 'g', -1, 64)
		value, ok := new(big.Rat).SetString(text)
		if !ok {
			return nil, fmt.Errorf("locale: invalid float")
		}
		return value, nil
	default:
		return nil, fmt.Errorf("locale: value must be int, decimal, or float")
	}
}

func localeData(tag language.Tag) localeDateData {
	if data, ok := localeDates[tag.String()]; ok {
		return data
	}
	base, _ := tag.Base()
	return localeDates[base.String()]
}

func localeFormatExact(value *big.Rat, tag language.Tag, minimum, maximum int, grouping bool) string {
	data := localeData(tag)
	text := value.FloatString(maximum)
	sign := ""
	if strings.HasPrefix(text, "-") {
		sign = data.Minus
		text = text[1:]
	}
	parts := strings.SplitN(text, ".", 2)
	integer := parts[0]
	if grouping {
		var grouped strings.Builder
		for i, char := range integer {
			if i > 0 && (len(integer)-i)%3 == 0 {
				grouped.WriteString(data.Group)
			}
			grouped.WriteRune(char)
		}
		integer = grouped.String()
	}
	fraction := ""
	if len(parts) == 2 {
		fraction = parts[1]
		for len(fraction) > minimum && strings.HasSuffix(fraction, "0") {
			fraction = fraction[:len(fraction)-1]
		}
	}
	if fraction == "" {
		return sign + integer
	}
	return sign + integer + data.Decimal + fraction
}

func localeFormatNumber(args []runtime.Value) (runtime.Value, error) {
	if len(args) < 2 || len(args) > 3 {
		return nil, fmt.Errorf("locale.formatNumber expects value, tag, and options")
	}
	value, err := localeNumericValue(args[0])
	if err != nil {
		return nil, err
	}
	tag, err := localeTag(args[1])
	if err != nil {
		return nil, err
	}
	opts := runtime.NewDictHint(0)
	if len(args) == 3 {
		opts, err = localeOptions(args[2], "locale.formatNumber")
		if err != nil {
			return nil, err
		}
	}
	minimum, maximum, grouping, err := localeNumberOptions(opts, 0, 3)
	if err != nil {
		return nil, err
	}
	return runtime.String{Value: localeFormatExact(value, tag, minimum, maximum, grouping)}, nil
}

func localeFormatCurrency(args []runtime.Value) (runtime.Value, error) {
	if len(args) < 3 || len(args) > 4 {
		return nil, fmt.Errorf("locale.formatCurrency expects value, currency, tag, and options")
	}
	value, err := localeNumericValue(args[0])
	if err != nil {
		return nil, err
	}
	code, ok := args[1].(runtime.String)
	if !ok {
		return nil, fmt.Errorf("locale.formatCurrency: currency must be string")
	}
	unit, err := currency.ParseISO(code.Value)
	if err != nil || unit.String() == "XXX" {
		return nil, fmt.Errorf("locale.formatCurrency: unknown currency %q", code.Value)
	}
	tag, err := localeTag(args[2])
	if err != nil {
		return nil, err
	}
	opts := runtime.NewDictHint(0)
	if len(args) == 4 {
		opts, err = localeOptions(args[3], "locale.formatCurrency")
		if err != nil {
			return nil, err
		}
	}
	scale, _ := currency.Standard.Rounding(unit)
	minimum, maximum, grouping, err := localeNumberOptions(opts, scale, scale)
	if err != nil {
		return nil, err
	}
	data := localeData(tag)
	pattern := strings.SplitN(data.CurrencyPattern, ";", 2)[0]
	if value.Sign() < 0 {
		parts := strings.SplitN(data.CurrencyPattern, ";", 2)
		if len(parts) == 2 {
			pattern = parts[1]
		} else {
			pattern = data.Minus + pattern
		}
	}
	if !strings.Contains(pattern, "#,##0.00") {
		return nil, fmt.Errorf("locale.formatCurrency: unavailable pattern for %s", code.Value)
	}
	absolute := new(big.Rat).Abs(value)
	number := localeFormatExact(absolute, tag, minimum, maximum, grouping)
	symbol := message.NewPrinter(tag).Sprintf("%v", currency.Symbol(unit))
	formatted := strings.Replace(pattern, "#,##0.00", number, 1)
	formatted = strings.ReplaceAll(formatted, "\u00a4", symbol)
	return runtime.String{Value: formatted}, nil
}

func localeDatePattern(date time.Time, data localeDateData, style string) (string, error) {
	pattern, ok := data.Formats[style]
	if !ok {
		return "", fmt.Errorf("locale.formatDate: invalid style %q", style)
	}
	weekday := int(date.Weekday())
	month := int(date.Month()) - 1
	var output strings.Builder
	for i := 0; i < len(pattern); {
		j := i + 1
		for j < len(pattern) && pattern[j] == pattern[i] {
			j++
		}
		token := pattern[i:j]
		switch token {
		case "y":
			output.WriteString(strconv.Itoa(date.Year()))
		case "yy":
			output.WriteString(fmt.Sprintf("%02d", date.Year()%100))
		case "M":
			output.WriteString(strconv.Itoa(int(date.Month())))
		case "MM":
			output.WriteString(fmt.Sprintf("%02d", date.Month()))
		case "MMM":
			output.WriteString(data.MonthsShort[month])
		case "MMMM":
			output.WriteString(data.MonthsWide[month])
		case "d":
			output.WriteString(strconv.Itoa(date.Day()))
		case "dd":
			output.WriteString(fmt.Sprintf("%02d", date.Day()))
		case "EEEE":
			output.WriteString(data.DaysWide[weekday])
		default:
			output.WriteString(token)
		}
		i = j
	}
	return output.String(), nil
}

func localeFormatDate(args []runtime.Value) (runtime.Value, error) {
	if len(args) < 2 || len(args) > 4 {
		return nil, fmt.Errorf("locale.formatDate expects instant, tag, style, and zone")
	}
	instant, ok := args[0].(runtime.DateTimeInstant)
	if !ok {
		return nil, fmt.Errorf("locale.formatDate: value must be datetime.Instant")
	}
	tag, err := localeTag(args[1])
	if err != nil {
		return nil, err
	}
	style := runtime.String{Value: "medium"}
	if len(args) >= 3 {
		style, ok = args[2].(runtime.String)
		if !ok {
			return nil, fmt.Errorf("locale.formatDate: style must be string")
		}
	}
	zone := runtime.String{Value: "UTC"}
	if len(args) == 4 {
		zone, ok = args[3].(runtime.String)
		if !ok {
			return nil, fmt.Errorf("locale.formatDate: zone must be string")
		}
	}
	location, err := time.LoadLocation(zone.Value)
	if err != nil {
		return nil, fmt.Errorf("locale.formatDate: invalid zone %q: %w", zone.Value, err)
	}
	data := localeData(tag)
	formatted, err := localeDatePattern(time.Unix(instant.Unix, 0).In(location), data, style.Value)
	if err != nil {
		return nil, err
	}
	return runtime.String{Value: formatted}, nil
}

func localeCompare(args []runtime.Value) (runtime.Value, error) {
	if len(args) < 3 || len(args) > 4 {
		return nil, fmt.Errorf("locale.compare expects two strings, tag, and options")
	}
	left, ok := args[0].(runtime.String)
	if !ok {
		return nil, fmt.Errorf("locale.compare: first value must be string")
	}
	right, ok := args[1].(runtime.String)
	if !ok {
		return nil, fmt.Errorf("locale.compare: second value must be string")
	}
	tag, err := localeTag(args[2])
	if err != nil {
		return nil, err
	}
	opts := runtime.NewDictHint(0)
	if len(args) == 4 {
		opts, err = localeOptions(args[3], "locale.compare")
		if err != nil {
			return nil, err
		}
	}
	var collateOpts []collate.Option
	for _, setting := range []struct {
		name   string
		option collate.Option
	}{{"ignoreCase", collate.IgnoreCase}, {"numeric", collate.Numeric}} {
		if value, exists := dictLookup(opts, setting.name); exists {
			flag, ok := value.(runtime.Bool)
			if !ok {
				return nil, fmt.Errorf("locale.compare: %s must be bool", setting.name)
			}
			if flag.Value {
				collateOpts = append(collateOpts, setting.option)
			}
		}
	}
	comparison := collate.New(tag, collateOpts...).CompareString(left.Value, right.Value)
	if comparison < 0 {
		comparison = -1
	} else if comparison > 0 {
		comparison = 1
	}
	return runtime.NewInt64(int64(comparison)), nil
}

func localePluralCategory(args []runtime.Value) (runtime.Value, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("locale.pluralCategory expects count and tag")
	}
	var count string
	switch value := args[0].(type) {
	case runtime.SmallInt:
		count = strconv.FormatInt(value.Value, 10)
	case runtime.Int:
		count = value.Value.String()
	default:
		return nil, fmt.Errorf("locale.pluralCategory: count must be int")
	}
	tag, err := localeTag(args[1])
	if err != nil {
		return nil, err
	}
	count = strings.TrimPrefix(count, "-")
	digits := make([]byte, len(count))
	for i := range count {
		digits[i] = count[i] - '0'
	}
	var category string
	switch plural.Cardinal.MatchDigits(tag, digits, len(digits), 0) {
	case plural.Zero:
		category = "zero"
	case plural.One:
		category = "one"
	case plural.Two:
		category = "two"
	case plural.Few:
		category = "few"
	case plural.Many:
		category = "many"
	default:
		category = "other"
	}
	return runtime.String{Value: category}, nil
}

func localeInterpolate(args []runtime.Value) (runtime.Value, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("i18n.interpolate expects message and arguments")
	}
	messageValue, ok := args[0].(runtime.String)
	if !ok {
		return nil, fmt.Errorf("i18n.interpolate: message must be string")
	}
	values, ok := args[1].(runtime.Dict)
	if !ok {
		return nil, fmt.Errorf("i18n.interpolate: arguments must be a dictionary")
	}
	input := messageValue.Value
	var output strings.Builder
	for i := 0; i < len(input); {
		if input[i] == '{' {
			if i+1 < len(input) && input[i+1] == '{' {
				output.WriteByte('{')
				i += 2
				continue
			}
			end := strings.IndexByte(input[i+1:], '}')
			if end < 0 {
				return nil, fmt.Errorf("i18n.interpolate: unclosed placeholder")
			}
			name := input[i+1 : i+1+end]
			if name == "" || !((name[0] >= 'A' && name[0] <= 'Z') ||
				(name[0] >= 'a' && name[0] <= 'z') || name[0] == '_') {
				return nil, fmt.Errorf("i18n.interpolate: invalid placeholder %q", name)
			}
			for _, char := range name[1:] {
				if !((char >= 'A' && char <= 'Z') || (char >= 'a' && char <= 'z') ||
					(char >= '0' && char <= '9') || char == '_') {
					return nil, fmt.Errorf("i18n.interpolate: invalid placeholder %q", name)
				}
			}
			value, found := dictLookup(values, name)
			if !found {
				return nil, fmt.Errorf("i18n.interpolate: missing argument %q", name)
			}
			output.WriteString(value.Inspect())
			i += end + 2
			continue
		}
		if input[i] == '}' {
			if i+1 < len(input) && input[i+1] == '}' {
				output.WriteByte('}')
				i += 2
				continue
			}
			return nil, fmt.Errorf("i18n.interpolate: unmatched closing brace")
		}
		output.WriteByte(input[i])
		i++
	}
	return runtime.String{Value: output.String()}, nil
}
