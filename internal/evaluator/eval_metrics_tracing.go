package evaluator

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"geblang/internal/ast"
	"geblang/internal/native"
	"geblang/internal/runtime"
	"geblang/internal/version"
	"math"
	"math/big"
	"net/http"
	goruntime "runtime"
	"sort"
	"strconv"
	"strings"
	"time"
)

func (e *Evaluator) metricsInc(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) < 1 || len(args) > 3 {
		return nil, fmt.Errorf("%s expects name, optional amount, optional labels", call.Callee.String())
	}
	name, ok := args[0].(runtime.String)
	if !ok {
		return nil, fmt.Errorf("%s name must be string", call.Callee.String())
	}
	amount := 1.0
	if len(args) >= 2 {
		if labels, ok := args[1].(runtime.Dict); ok && len(args) == 2 {
			// Two-arg variant with a dict in the second slot: treat it
			// as labels with default amount = 1.
			return e.metricsIncCommit(call, name.Value, 1.0, labels)
		}
		value, err := metricNumber(args[1])
		if err != nil {
			return nil, err
		}
		amount = value
	}
	labels := runtime.Dict{Entries: map[string]runtime.DictEntry{}}
	if len(args) == 3 {
		labels, ok = args[2].(runtime.Dict)
		if !ok {
			return nil, fmt.Errorf("%s labels must be dict", call.Callee.String())
		}
	}
	return e.metricsIncCommit(call, name.Value, amount, labels)
}

func (e *Evaluator) metricsIncCommit(call *ast.CallExpression, name string, amount float64, labels runtime.Dict) (runtime.Value, error) {
	e.metricsMu.Lock()
	defer e.metricsMu.Unlock()
	if entry, ok := e.metricRegistry[name]; ok {
		if entry.kind != "counter" && entry.kind != "gauge" {
			return nil, fmt.Errorf("metric %q is a %s; use metrics.observe to record samples", name, entry.kind)
		}
		key, _, err := labelKeyFromDict(entry, labels)
		if err != nil {
			return nil, fmt.Errorf("%s: %v", call.Callee.String(), err)
		}
		entry.values[key] += amount
		return runtime.Float{Value: entry.values[key]}, nil
	}
	if labels.Len() > 0 {
		return nil, fmt.Errorf("metric %q is not declared; call metrics.counter(%q, {labels: ...}) first", name, name)
	}
	e.metrics[name] += amount
	return runtime.Float{Value: e.metrics[name]}, nil
}

func (e *Evaluator) metricsSet(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 2 && len(args) != 3 {
		return nil, fmt.Errorf("%s expects name, value, optional labels", call.Callee.String())
	}
	name, ok := args[0].(runtime.String)
	if !ok {
		return nil, fmt.Errorf("%s name must be string", call.Callee.String())
	}
	value, err := metricNumber(args[1])
	if err != nil {
		return nil, err
	}
	labels := runtime.Dict{Entries: map[string]runtime.DictEntry{}}
	if len(args) == 3 {
		labels, ok = args[2].(runtime.Dict)
		if !ok {
			return nil, fmt.Errorf("%s labels must be dict", call.Callee.String())
		}
	}
	e.metricsMu.Lock()
	defer e.metricsMu.Unlock()
	if entry, ok := e.metricRegistry[name.Value]; ok {
		if entry.kind != "gauge" && entry.kind != "counter" {
			return nil, fmt.Errorf("metric %q is a %s; use metrics.observe to record samples", name.Value, entry.kind)
		}
		key, _, err := labelKeyFromDict(entry, labels)
		if err != nil {
			return nil, fmt.Errorf("%s: %v", call.Callee.String(), err)
		}
		entry.values[key] = value
		return runtime.Float{Value: value}, nil
	}
	if labels.Len() > 0 {
		return nil, fmt.Errorf("metric %q is not declared; call metrics.gauge(%q, {labels: ...}) first", name.Value, name.Value)
	}
	e.metrics[name.Value] = value
	return runtime.Float{Value: value}, nil
}

func (e *Evaluator) metricsGet(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	name, err := singleStringValue(call, args)
	if err != nil {
		return nil, err
	}
	e.metricsMu.Lock()
	defer e.metricsMu.Unlock()
	return runtime.Float{Value: e.metrics[name]}, nil
}

func (e *Evaluator) metricsSnapshot(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 0 {
		return nil, fmt.Errorf("%s expects no arguments", call.Callee.String())
	}
	e.metricsMu.Lock()
	defer e.metricsMu.Unlock()
	entries := map[string]runtime.DictEntry{}
	for key, value := range e.metrics {
		putDict(entries, key, runtime.Float{Value: value})
	}
	return runtime.Dict{Entries: entries}, nil
}

func (e *Evaluator) metricsReset(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 0 && len(args) != 1 {
		return nil, fmt.Errorf("%s expects optional name", call.Callee.String())
	}
	e.metricsMu.Lock()
	defer e.metricsMu.Unlock()
	if len(args) == 0 {
		e.metrics = map[string]float64{}
		return runtime.Null{}, nil
	}
	name, ok := args[0].(runtime.String)
	if !ok {
		return nil, fmt.Errorf("%s name must be string", call.Callee.String())
	}
	delete(e.metrics, name.Value)
	return runtime.Null{}, nil
}

// metricsMonoStart anchors metrics/profile timestamps to a monotonic reading (UnixNano is wall clock and can go backwards).
var metricsMonoStart = time.Now()

func metricsNow(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 0 {
		return nil, fmt.Errorf("%s expects no arguments", call.Callee.String())
	}
	return runtime.NewInt64(time.Since(metricsMonoStart).Nanoseconds()), nil
}

// metricsDuration returns milliseconds (sub-ms precision) since a now() token; now() itself is an opaque monotonic nanosecond reading.
func metricsDuration(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("%s expects start timestamp", call.Callee.String())
	}
	startNs, ok := native.AsInt64(args[0])
	if !ok {
		return nil, fmt.Errorf("%s start timestamp must be int", call.Callee.String())
	}
	elapsedNs := time.Since(metricsMonoStart).Nanoseconds() - startNs
	return runtime.Float{Value: float64(elapsedNs) / 1e6}, nil
}

// metricsEntry is one declared metric in the registry. counter and
// gauge use `values` (one float per label-tuple); histogram uses
// `histStates` (running bucket counts per label-tuple) and `buckets`
// (the configured upper bounds, ascending).
type metricsEntry struct {
	kind       string
	help       string
	labelKeys  []string
	values     map[string]float64
	buckets    []float64
	histStates map[string]*histState
}

type histState struct {
	counts []uint64
	sum    float64
	count  uint64
}

// labelKeyFromDict produces a stable string key for a label-value
// tuple by sorting the entry's labelKeys and joining `name=value`
// pairs with ASCII unit separator. Missing labels are treated as the
// empty string. Extra labels not declared on the metric are rejected.
func labelKeyFromDict(entry *metricsEntry, fields runtime.Dict) (string, []string, error) {
	if len(entry.labelKeys) == 0 {
		if fields.Len() > 0 {
			return "", nil, fmt.Errorf("metric was declared with no labels")
		}
		return "", nil, nil
	}
	declared := map[string]string{}
	for _, k := range entry.labelKeys {
		declared[native.DictKey(runtime.String{Value: k})] = k
	}
	for _, k := range fields.EntryKeys() {
		if _, ok := declared[k]; !ok {
			return "", nil, fmt.Errorf("undeclared label key %q", k)
		}
	}
	values := make([]string, len(entry.labelKeys))
	for i, k := range entry.labelKeys {
		ent, ok := fields.GetEntry(native.DictKey(runtime.String{Value: k}))
		if !ok {
			values[i] = ""
			continue
		}
		s, ok := ent.Value.(runtime.String)
		if !ok {
			return "", nil, fmt.Errorf("label %q must be string", k)
		}
		values[i] = s.Value
	}
	return strings.Join(values, "\x1f"), values, nil
}

func (e *Evaluator) declareMetric(call *ast.CallExpression, args []runtime.Value, kind string) (runtime.Value, error) {
	if len(args) != 1 && len(args) != 2 {
		return nil, fmt.Errorf("%s expects name and optional opts", call.Callee.String())
	}
	name, ok := args[0].(runtime.String)
	if !ok {
		return nil, fmt.Errorf("%s name must be string", call.Callee.String())
	}
	entry := &metricsEntry{
		kind:   kind,
		values: map[string]float64{},
	}
	if kind == "histogram" {
		entry.buckets = []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}
		entry.histStates = map[string]*histState{}
	}
	if len(args) == 2 {
		opts, ok := args[1].(runtime.Dict)
		if !ok {
			return nil, fmt.Errorf("%s opts must be dict", call.Callee.String())
		}
		if helpEnt, ok := opts.GetEntry(native.DictKey(runtime.String{Value: "help"})); ok {
			if s, ok := helpEnt.Value.(runtime.String); ok {
				entry.help = s.Value
			}
		}
		if labelEnt, ok := opts.GetEntry(native.DictKey(runtime.String{Value: "labels"})); ok {
			list, ok := labelEnt.Value.(*runtime.List)
			if !ok {
				return nil, fmt.Errorf("%s opts.labels must be list of strings", call.Callee.String())
			}
			for _, item := range list.Elements {
				s, ok := item.(runtime.String)
				if !ok {
					return nil, fmt.Errorf("%s opts.labels entries must be strings", call.Callee.String())
				}
				entry.labelKeys = append(entry.labelKeys, s.Value)
			}
		}
		if bucketEnt, ok := opts.GetEntry(native.DictKey(runtime.String{Value: "buckets"})); ok {
			if kind != "histogram" {
				return nil, fmt.Errorf("%s opts.buckets is only valid for histograms", call.Callee.String())
			}
			list, ok := bucketEnt.Value.(*runtime.List)
			if !ok {
				return nil, fmt.Errorf("%s opts.buckets must be list of numbers", call.Callee.String())
			}
			entry.buckets = entry.buckets[:0]
			for _, item := range list.Elements {
				v, err := metricNumber(item)
				if err != nil {
					return nil, fmt.Errorf("%s opts.buckets: %v", call.Callee.String(), err)
				}
				entry.buckets = append(entry.buckets, v)
			}
		}
	}
	e.metricsMu.Lock()
	defer e.metricsMu.Unlock()
	if existing, ok := e.metricRegistry[name.Value]; ok && existing.kind != kind {
		return nil, fmt.Errorf("metric %q already declared as %s", name.Value, existing.kind)
	}
	e.metricRegistry[name.Value] = entry
	return runtime.String{Value: name.Value}, nil
}

func (e *Evaluator) metricsCounter(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	return e.declareMetric(call, args, "counter")
}

func (e *Evaluator) metricsGauge(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	return e.declareMetric(call, args, "gauge")
}

func (e *Evaluator) metricsHistogram(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	return e.declareMetric(call, args, "histogram")
}

// metricsObserve records a histogram sample. Signature:
//
//	metrics.observe(name, value)               // no labels
//	metrics.observe(name, value, {labelDict})  // with labels
func (e *Evaluator) metricsObserve(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 2 && len(args) != 3 {
		return nil, fmt.Errorf("%s expects name, value, and optional labels", call.Callee.String())
	}
	name, ok := args[0].(runtime.String)
	if !ok {
		return nil, fmt.Errorf("%s name must be string", call.Callee.String())
	}
	value, err := metricNumber(args[1])
	if err != nil {
		return nil, err
	}
	labels := runtime.Dict{Entries: map[string]runtime.DictEntry{}}
	if len(args) == 3 {
		labels, ok = args[2].(runtime.Dict)
		if !ok {
			return nil, fmt.Errorf("%s labels must be dict", call.Callee.String())
		}
	}
	e.metricsMu.Lock()
	defer e.metricsMu.Unlock()
	entry, ok := e.metricRegistry[name.Value]
	if !ok {
		return nil, fmt.Errorf("metric %q is not declared - call metrics.histogram(%q, ...) first", name.Value, name.Value)
	}
	if entry.kind != "histogram" {
		return nil, fmt.Errorf("metric %q is a %s, not a histogram", name.Value, entry.kind)
	}
	key, _, err := labelKeyFromDict(entry, labels)
	if err != nil {
		return nil, fmt.Errorf("%s: %v", call.Callee.String(), err)
	}
	state, ok := entry.histStates[key]
	if !ok {
		state = &histState{counts: make([]uint64, len(entry.buckets))}
		entry.histStates[key] = state
	}
	for i, upper := range entry.buckets {
		if value <= upper {
			state.counts[i]++
		}
	}
	state.sum += value
	state.count++
	return runtime.Null{}, nil
}

// metricsToPrometheus emits the registered metrics in Prometheus
// text exposition format (v0.0.4). Legacy `metrics.inc/set` entries
// that are not also declared with metrics.counter/gauge appear as
// untyped at the end.
func (e *Evaluator) metricsToPrometheus(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 0 {
		return nil, fmt.Errorf("%s expects no arguments", call.Callee.String())
	}
	e.metricsMu.Lock()
	defer e.metricsMu.Unlock()
	var buf strings.Builder
	names := make([]string, 0, len(e.metricRegistry))
	for name := range e.metricRegistry {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		entry := e.metricRegistry[name]
		if entry.help != "" {
			fmt.Fprintf(&buf, "# HELP %s %s\n", name, escapePromHelp(entry.help))
		}
		fmt.Fprintf(&buf, "# TYPE %s %s\n", name, entry.kind)
		switch entry.kind {
		case "counter", "gauge":
			writeLabeledValues(&buf, name, entry)
		case "histogram":
			writeHistogram(&buf, name, entry)
		}
	}
	// Legacy flat entries that weren't promoted via counter/gauge.
	legacyNames := make([]string, 0, len(e.metrics))
	for name := range e.metrics {
		if _, declared := e.metricRegistry[name]; declared {
			continue
		}
		legacyNames = append(legacyNames, name)
	}
	sort.Strings(legacyNames)
	for _, name := range legacyNames {
		fmt.Fprintf(&buf, "# TYPE %s untyped\n", name)
		fmt.Fprintf(&buf, "%s %s\n", name, formatPromFloat(e.metrics[name]))
	}
	return runtime.String{Value: buf.String()}, nil
}

func writeLabeledValues(buf *strings.Builder, name string, entry *metricsEntry) {
	keys := make([]string, 0, len(entry.values))
	for k := range entry.values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Fprintf(buf, "%s%s %s\n", name, promLabelsFromKey(entry.labelKeys, k), formatPromFloat(entry.values[k]))
	}
	if len(keys) == 0 && len(entry.labelKeys) == 0 {
		fmt.Fprintf(buf, "%s 0\n", name)
	}
}

func writeHistogram(buf *strings.Builder, name string, entry *metricsEntry) {
	keys := make([]string, 0, len(entry.histStates))
	for k := range entry.histStates {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if len(keys) == 0 {
		keys = []string{""}
	}
	for _, k := range keys {
		state, ok := entry.histStates[k]
		if !ok {
			state = &histState{counts: make([]uint64, len(entry.buckets))}
		}
		baseLabels := promLabelsFromKey(entry.labelKeys, k)
		for i, upper := range entry.buckets {
			labelList := promLabelsAppendLE(baseLabels, formatPromFloat(upper))
			fmt.Fprintf(buf, "%s_bucket%s %d\n", name, labelList, state.counts[i])
		}
		labelList := promLabelsAppendLE(baseLabels, "+Inf")
		fmt.Fprintf(buf, "%s_bucket%s %d\n", name, labelList, state.count)
		fmt.Fprintf(buf, "%s_sum%s %s\n", name, baseLabels, formatPromFloat(state.sum))
		fmt.Fprintf(buf, "%s_count%s %d\n", name, baseLabels, state.count)
	}
}

func promLabelsFromKey(labelNames []string, key string) string {
	if len(labelNames) == 0 {
		return ""
	}
	values := strings.Split(key, "\x1f")
	if len(values) != len(labelNames) {
		return ""
	}
	pairs := make([]string, 0, len(labelNames))
	for i, name := range labelNames {
		pairs = append(pairs, fmt.Sprintf("%s=%q", name, escapePromLabel(values[i])))
	}
	return "{" + strings.Join(pairs, ",") + "}"
}

func promLabelsAppendLE(base, upper string) string {
	if base == "" {
		return fmt.Sprintf("{le=%q}", upper)
	}
	return base[:len(base)-1] + fmt.Sprintf(",le=%q}", upper)
}

func escapePromLabel(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	return s
}

func escapePromHelp(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	return s
}

func formatPromFloat(v float64) string {
	switch {
	case math.IsNaN(v):
		return "NaN"
	case math.IsInf(v, 1):
		return "+Inf"
	case math.IsInf(v, -1):
		return "-Inf"
	}
	return strconv.FormatFloat(v, 'g', -1, 64)
}

func metricNumber(value runtime.Value) (float64, error) {
	switch value := value.(type) {
	case runtime.SmallInt:
		return float64(value.Value), nil
	case runtime.Int:
		result, _ := new(big.Rat).SetInt(value.Value).Float64()
		return result, nil
	case runtime.Decimal:
		result, _ := value.Value.Float64()
		return result, nil
	case runtime.Float:
		return value.Value, nil
	default:
		return 0, fmt.Errorf("metric value must be numeric")
	}
}

func (e *Evaluator) traceStart(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) < 1 || len(args) > 3 {
		return nil, fmt.Errorf("%s expects name, optional attrs, optional opts", call.Callee.String())
	}
	name, ok := args[0].(runtime.String)
	if !ok {
		return nil, fmt.Errorf("%s name must be string", call.Callee.String())
	}
	attrs, err := optionalAttrs(call, args, 1)
	if err != nil {
		return nil, err
	}
	// Optional third arg: opts dict carrying `parent` (a span handle
	// previously returned by trace.start). When present, the new span
	// inherits the parent's traceID and records its parent's spanID.
	var parent *traceSpan
	if len(args) == 3 {
		opts, ok := args[2].(runtime.Dict)
		if !ok {
			return nil, fmt.Errorf("%s opts must be dict", call.Callee.String())
		}
		if parentEnt, ok := opts.GetEntry(native.DictKey(runtime.String{Value: "parent"})); ok {
			parentID, err := traceHandleID(parentEnt.Value)
			if err != nil {
				return nil, fmt.Errorf("%s opts.parent: %v", call.Callee.String(), err)
			}
			e.traceMu.Lock()
			parent = e.traces[parentID]
			e.traceMu.Unlock()
			if parent == nil {
				return nil, fmt.Errorf("%s opts.parent: unknown span %d", call.Callee.String(), parentID)
			}
		}
	}
	span := &traceSpan{
		name:   name.Value,
		start:  time.Now(),
		attrs:  attrs,
		events: []traceEvent{},
		spanID: randomBytes(8),
	}
	if parent != nil && len(parent.traceID) > 0 {
		span.traceID = parent.traceID
		span.parentSpanID = parent.spanID
	} else {
		span.traceID = randomBytes(16)
	}
	e.traceMu.Lock()
	defer e.traceMu.Unlock()
	e.nextTraceID++
	e.traces[e.nextTraceID] = span
	return runtime.NewInt64(e.nextTraceID), nil
}

func (e *Evaluator) traceEvent(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 2 && len(args) != 3 {
		return nil, fmt.Errorf("%s expects span, name, and optional attrs", call.Callee.String())
	}
	id, err := traceHandleID(args[0])
	if err != nil {
		return nil, err
	}
	name, ok := args[1].(runtime.String)
	if !ok {
		return nil, fmt.Errorf("%s event name must be string", call.Callee.String())
	}
	attrs, err := optionalAttrs(call, args, 2)
	if err != nil {
		return nil, err
	}
	e.traceMu.Lock()
	defer e.traceMu.Unlock()
	span, ok := e.traces[id]
	if !ok {
		return nil, fmt.Errorf("unknown trace span %d", id)
	}
	span.events = append(span.events, traceEvent{name: name.Value, at: time.Now(), attrs: attrs})
	return runtime.Null{}, nil
}

func (e *Evaluator) traceEnd(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("%s expects exactly one argument", call.Callee.String())
	}
	id, err := traceHandleID(args[0])
	if err != nil {
		return nil, err
	}
	e.traceMu.Lock()
	defer e.traceMu.Unlock()
	span, ok := e.traces[id]
	if !ok {
		return nil, fmt.Errorf("unknown trace span %d", id)
	}
	if !span.ended {
		span.end = time.Now()
		span.ended = true
	}
	return traceSpanValue(id, span), nil
}

func (e *Evaluator) traceSnapshot(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 0 {
		return nil, fmt.Errorf("%s expects no arguments", call.Callee.String())
	}
	e.traceMu.Lock()
	defer e.traceMu.Unlock()
	spans := make([]runtime.Value, 0, len(e.traces))
	for id, span := range e.traces {
		spans = append(spans, traceSpanValue(id, span))
	}
	return &runtime.List{Elements: spans}, nil
}

func (e *Evaluator) traceReset(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 0 {
		return nil, fmt.Errorf("%s expects no arguments", call.Callee.String())
	}
	e.traceMu.Lock()
	defer e.traceMu.Unlock()
	e.traces = map[int64]*traceSpan{}
	return runtime.Null{}, nil
}

// traceToOtlpJson serializes every recorded span to an OTLP/HTTP
// JSON request body. Signature:
//
//	trace.toOtlpJson()          -> string  (uses default service name)
//	trace.toOtlpJson(opts)      -> string  (opts.serviceName, opts.scopeName, opts.scopeVersion, opts.resource)
//
// Spans without an OTLP traceID/spanID (created before OTLP support
// was added or imported from an older session) are assigned fresh
// IDs at serialization time so the output is always well-formed.
func (e *Evaluator) traceToOtlpJson(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) > 1 {
		return nil, fmt.Errorf("%s expects optional opts dict", call.Callee.String())
	}
	opts := otlpExportOpts{
		serviceName:  "geblang",
		scopeName:    "geblang.trace",
		scopeVersion: version.Geblang,
	}
	if len(args) == 1 {
		if err := opts.applyDict(call, args[0]); err != nil {
			return nil, err
		}
	}
	body, err := e.buildOtlpJson(opts)
	if err != nil {
		return nil, err
	}
	return runtime.String{Value: string(body)}, nil
}

// traceExportOtlp POSTs every recorded span to an OTLP/HTTP
// collector. Signature:
//
//	trace.exportOtlp(endpoint)         -> dict {status, ok}
//	trace.exportOtlp(endpoint, opts)   -> dict {status, ok}
//
// endpoint is the collector base URL (e.g. http://localhost:4318);
// `/v1/traces` is appended automatically. opts may set serviceName,
// scopeName, scopeVersion, resource (dict<string, string>), headers
// (dict<string, string>), and timeoutMs.
func (e *Evaluator) traceExportOtlp(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 && len(args) != 2 {
		return nil, fmt.Errorf("%s expects endpoint and optional opts", call.Callee.String())
	}
	endpoint, ok := args[0].(runtime.String)
	if !ok {
		return nil, fmt.Errorf("%s endpoint must be string", call.Callee.String())
	}
	opts := otlpExportOpts{
		serviceName:  "geblang",
		scopeName:    "geblang.trace",
		scopeVersion: version.Geblang,
		timeoutMs:    10000,
	}
	if len(args) == 2 {
		if err := opts.applyDict(call, args[1]); err != nil {
			return nil, err
		}
	}
	body, err := e.buildOtlpJson(opts)
	if err != nil {
		return nil, err
	}
	url := strings.TrimRight(endpoint.Value, "/") + "/v1/traces"
	req, err := http.NewRequest("POST", url, strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range opts.headers {
		req.Header.Set(k, v)
	}
	client := &http.Client{Timeout: time.Duration(opts.timeoutMs) * time.Millisecond}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	entries := map[string]runtime.DictEntry{}
	putDict(entries, "status", runtime.NewInt64(int64(resp.StatusCode)))
	putDict(entries, "ok", runtime.Bool{Value: resp.StatusCode >= 200 && resp.StatusCode < 300})
	return runtime.Dict{Entries: entries}, nil
}

type otlpExportOpts struct {
	serviceName  string
	scopeName    string
	scopeVersion string
	resource     map[string]string
	headers      map[string]string
	timeoutMs    int
}

func (o *otlpExportOpts) applyDict(call *ast.CallExpression, value runtime.Value) error {
	dict, ok := value.(runtime.Dict)
	if !ok {
		return fmt.Errorf("%s opts must be dict", call.Callee.String())
	}
	if ent, ok := dict.GetEntry(native.DictKey(runtime.String{Value: "serviceName"})); ok {
		s, ok := ent.Value.(runtime.String)
		if !ok {
			return fmt.Errorf("%s opts.serviceName must be string", call.Callee.String())
		}
		o.serviceName = s.Value
	}
	if ent, ok := dict.GetEntry(native.DictKey(runtime.String{Value: "scopeName"})); ok {
		s, ok := ent.Value.(runtime.String)
		if !ok {
			return fmt.Errorf("%s opts.scopeName must be string", call.Callee.String())
		}
		o.scopeName = s.Value
	}
	if ent, ok := dict.GetEntry(native.DictKey(runtime.String{Value: "scopeVersion"})); ok {
		s, ok := ent.Value.(runtime.String)
		if !ok {
			return fmt.Errorf("%s opts.scopeVersion must be string", call.Callee.String())
		}
		o.scopeVersion = s.Value
	}
	if ent, ok := dict.GetEntry(native.DictKey(runtime.String{Value: "resource"})); ok {
		res, ok := ent.Value.(runtime.Dict)
		if !ok {
			return fmt.Errorf("%s opts.resource must be dict<string, string>", call.Callee.String())
		}
		o.resource = stringDictFromRuntime(res)
	}
	if ent, ok := dict.GetEntry(native.DictKey(runtime.String{Value: "headers"})); ok {
		hd, ok := ent.Value.(runtime.Dict)
		if !ok {
			return fmt.Errorf("%s opts.headers must be dict<string, string>", call.Callee.String())
		}
		o.headers = stringDictFromRuntime(hd)
	}
	if ent, ok := dict.GetEntry(native.DictKey(runtime.String{Value: "timeoutMs"})); ok {
		n, ok := native.AsInt64(ent.Value)
		if !ok {
			return fmt.Errorf("%s opts.timeoutMs must be int", call.Callee.String())
		}
		o.timeoutMs = int(n)
	}
	return nil
}

func stringDictFromRuntime(d runtime.Dict) map[string]string {
	out := map[string]string{}
	d.ForEachEntry(func(_ string, ent runtime.DictEntry) bool {
		k, ok := ent.Key.(runtime.String)
		if !ok {
			return true
		}
		v, ok := ent.Value.(runtime.String)
		if !ok {
			return true
		}
		out[k.Value] = v.Value
		return true
	})
	return out
}

// buildOtlpJson constructs an OTLP/HTTP request body (ExportTraceServiceRequest)
// containing every span currently held by the evaluator.
func (e *Evaluator) buildOtlpJson(opts otlpExportOpts) ([]byte, error) {
	e.traceMu.Lock()
	defer e.traceMu.Unlock()

	// Stable ordering by handle so output is deterministic.
	ids := make([]int64, 0, len(e.traces))
	for id := range e.traces {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })

	spansJSON := make([]map[string]any, 0, len(ids))
	for _, id := range ids {
		span := e.traces[id]
		if len(span.traceID) == 0 {
			// Legacy span - assign IDs at serialization time so the
			// output is well-formed for the collector.
			span.traceID = randomBytes(16)
			span.spanID = randomBytes(8)
		}
		endNano := span.start.UnixNano()
		if span.ended {
			endNano = span.end.UnixNano()
		} else {
			endNano = time.Now().UnixNano()
		}
		spanObj := map[string]any{
			"traceId":           hex.EncodeToString(span.traceID),
			"spanId":            hex.EncodeToString(span.spanID),
			"name":              span.name,
			"kind":              1, // SPAN_KIND_INTERNAL
			"startTimeUnixNano": strconv.FormatInt(span.start.UnixNano(), 10),
			"endTimeUnixNano":   strconv.FormatInt(endNano, 10),
		}
		if len(span.parentSpanID) > 0 {
			spanObj["parentSpanId"] = hex.EncodeToString(span.parentSpanID)
		}
		if len(span.attrs) > 0 {
			spanObj["attributes"] = otlpAttributesFromMap(span.attrs)
		}
		if len(span.events) > 0 {
			events := make([]map[string]any, 0, len(span.events))
			for _, ev := range span.events {
				eventObj := map[string]any{
					"name":         ev.name,
					"timeUnixNano": strconv.FormatInt(ev.at.UnixNano(), 10),
				}
				if len(ev.attrs) > 0 {
					eventObj["attributes"] = otlpAttributesFromMap(ev.attrs)
				}
				events = append(events, eventObj)
			}
			spanObj["events"] = events
		}
		spansJSON = append(spansJSON, spanObj)
	}

	resourceAttrs := []map[string]any{
		{"key": "service.name", "value": map[string]any{"stringValue": opts.serviceName}},
	}
	for k, v := range opts.resource {
		if k == "service.name" {
			continue
		}
		resourceAttrs = append(resourceAttrs, map[string]any{
			"key":   k,
			"value": map[string]any{"stringValue": v},
		})
	}

	payload := map[string]any{
		"resourceSpans": []map[string]any{
			{
				"resource": map[string]any{"attributes": resourceAttrs},
				"scopeSpans": []map[string]any{
					{
						"scope": map[string]any{
							"name":    opts.scopeName,
							"version": opts.scopeVersion,
						},
						"spans": spansJSON,
					},
				},
			},
		},
	}
	return json.Marshal(payload)
}

func otlpAttributesFromMap(attrs map[string]runtime.Value) []map[string]any {
	keys := make([]string, 0, len(attrs))
	for k := range attrs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]map[string]any, 0, len(keys))
	for _, k := range keys {
		out = append(out, map[string]any{"key": k, "value": otlpAnyValue(attrs[k])})
	}
	return out
}

func otlpAnyValue(value runtime.Value) map[string]any {
	switch v := value.(type) {
	case runtime.String:
		return map[string]any{"stringValue": v.Value}
	case runtime.Bool:
		return map[string]any{"boolValue": v.Value}
	case runtime.SmallInt:
		return map[string]any{"intValue": strconv.FormatInt(v.Value, 10)}
	case runtime.Int:
		if v.Value.IsInt64() {
			return map[string]any{"intValue": strconv.FormatInt(v.Value.Int64(), 10)}
		}
		return map[string]any{"stringValue": v.Value.String()}
	case runtime.Float:
		return map[string]any{"doubleValue": v.Value}
	}
	// Fallback: encode as a JSON string so any attribute value survives.
	if data, err := json.Marshal(value); err == nil {
		return map[string]any{"stringValue": string(data)}
	}
	return map[string]any{"stringValue": fmt.Sprintf("%v", value)}
}

func optionalAttrs(call *ast.CallExpression, args []runtime.Value, index int) (map[string]runtime.Value, error) {
	attrs := map[string]runtime.Value{}
	if len(args) <= index {
		return attrs, nil
	}
	dict, ok := args[index].(runtime.Dict)
	if !ok {
		return nil, fmt.Errorf("%s attrs must be dict", call.Callee.String())
	}
	for _, __dk := range dict.EntryKeys() {
		entry, _ := dict.GetEntry(__dk)
		key, ok := entry.Key.(runtime.String)
		if !ok {
			return nil, fmt.Errorf("%s attrs keys must be strings", call.Callee.String())
		}
		attrs[key.Value] = entry.Value
	}
	return attrs, nil
}

func traceHandleID(value runtime.Value) (int64, error) {
	id, ok := value.(runtime.Int)
	if !ok || !id.Value.IsInt64() {
		return 0, fmt.Errorf("trace span handle must be int")
	}
	return id.Value.Int64(), nil
}

func traceSpanValue(id int64, span *traceSpan) runtime.Dict {
	entries := map[string]runtime.DictEntry{}
	putDict(entries, "id", runtime.NewInt64(id))
	putDict(entries, "name", runtime.String{Value: span.name})
	putDict(entries, "startUnixNano", runtime.NewInt64(span.start.UnixNano()))
	putDict(entries, "ended", runtime.Bool{Value: span.ended})
	if span.ended {
		putDict(entries, "endUnixNano", runtime.NewInt64(span.end.UnixNano()))
		putDict(entries, "durationNanos", runtime.NewInt64(span.end.Sub(span.start).Nanoseconds()))
	} else {
		putDict(entries, "endUnixNano", runtime.NewInt64(0))
		putDict(entries, "durationNanos", runtime.NewInt64(time.Since(span.start).Nanoseconds()))
	}
	putDict(entries, "attrs", traceAttrsValue(span.attrs))
	events := make([]runtime.Value, 0, len(span.events))
	for _, event := range span.events {
		events = append(events, traceEventValue(event))
	}
	putDict(entries, "events", &runtime.List{Elements: events})
	return runtime.Dict{Entries: entries}
}

func traceEventValue(event traceEvent) runtime.Dict {
	entries := map[string]runtime.DictEntry{}
	putDict(entries, "name", runtime.String{Value: event.name})
	putDict(entries, "unixNano", runtime.NewInt64(event.at.UnixNano()))
	putDict(entries, "attrs", traceAttrsValue(event.attrs))
	return runtime.Dict{Entries: entries}
}

func traceAttrsValue(attrs map[string]runtime.Value) runtime.Dict {
	entries := map[string]runtime.DictEntry{}
	for key, value := range attrs {
		putDict(entries, key, value)
	}
	return runtime.Dict{Entries: entries}
}

func profileMemStats(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 0 {
		return nil, fmt.Errorf("%s expects no arguments", call.Callee.String())
	}
	var stats goruntime.MemStats
	goruntime.ReadMemStats(&stats)
	entries := map[string]runtime.DictEntry{}
	putDict(entries, "alloc", runtime.NewInt64(int64(stats.Alloc)))
	putDict(entries, "totalAlloc", runtime.NewInt64(int64(stats.TotalAlloc)))
	putDict(entries, "sys", runtime.NewInt64(int64(stats.Sys)))
	putDict(entries, "heapAlloc", runtime.NewInt64(int64(stats.HeapAlloc)))
	putDict(entries, "heapSys", runtime.NewInt64(int64(stats.HeapSys)))
	putDict(entries, "heapObjects", runtime.NewInt64(int64(stats.HeapObjects)))
	putDict(entries, "numGC", runtime.NewInt64(int64(stats.NumGC)))
	putDict(entries, "goroutines", runtime.NewInt64(int64(goruntime.NumGoroutine())))
	return runtime.Dict{Entries: entries}, nil
}

func profileGC(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 0 {
		return nil, fmt.Errorf("%s expects no arguments", call.Callee.String())
	}
	goruntime.GC()
	return runtime.Null{}, nil
}
