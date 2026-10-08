package native

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"geblang/internal/runtime"
)

var stackFramePattern = regexp.MustCompile(`^\s*at\s+(.+?)(?:\s+\(line\s+([0-9]+)\))?\s*$`)

// UnknownMethodError is the canonical unknown-method text on both backends.
func UnknownMethodError(typeName, method string) error {
	return fmt.Errorf("unknown method %s.%s", typeName, method)
}

// UnsupportedOperandsError is the canonical bad-binary-operand text on both backends.
func UnsupportedOperandsError(operator, leftType, rightType string) error {
	return runtime.ClassifiedError{Class: "TypeError", Message: fmt.Sprintf("unsupported operands for %s: %s and %s", operator, leftType, rightType)}
}

func registerErrors(r *Registry) {
	r.Register("errors", "new", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) < 1 || len(args) > 2 {
			return nil, fmt.Errorf("errors.new expects one or two arguments")
		}
		className, ok := args[0].(runtime.String)
		if !ok {
			return nil, fmt.Errorf("errors.new: first argument must be a string class name")
		}
		msg := ""
		if len(args) == 2 {
			msgVal, ok := args[1].(runtime.String)
			if !ok {
				return nil, fmt.Errorf("errors.new: second argument must be a string message")
			}
			msg = msgVal.Value
		}
		return runtime.Error{Class: className.Value, Message: msg}, nil
	})

	r.Register("errors", "message", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 1 {
			return nil, fmt.Errorf("errors.message expects one argument")
		}
		err, ok := args[0].(runtime.Error)
		if !ok {
			return nil, fmt.Errorf("errors.message: argument must be an error, got %s", args[0].TypeName())
		}
		return runtime.String{Value: err.Message}, nil
	})

	r.Register("errors", "class", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 1 {
			return nil, fmt.Errorf("errors.class expects one argument")
		}
		err, ok := args[0].(runtime.Error)
		if !ok {
			return nil, fmt.Errorf("errors.class: argument must be an error, got %s", args[0].TypeName())
		}
		return runtime.String{Value: err.Class}, nil
	})

	r.Register("errors", "is", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 2 {
			return nil, fmt.Errorf("errors.is expects two arguments")
		}
		err, ok := args[0].(runtime.Error)
		if !ok {
			return nil, fmt.Errorf("errors.is: first argument must be an error, got %s", args[0].TypeName())
		}
		target, ok := args[1].(runtime.String)
		if !ok {
			return nil, fmt.Errorf("errors.is: second argument must be a string class name")
		}
		return runtime.Bool{Value: builtinErrorTypeMatches(err.Class, target.Value)}, nil
	})

	r.Register("errors", "wrap", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 2 {
			return nil, fmt.Errorf("errors.wrap expects two arguments")
		}
		err, ok := args[0].(runtime.Error)
		if !ok {
			return nil, fmt.Errorf("errors.wrap: first argument must be an error, got %s", args[0].TypeName())
		}
		msg, ok := args[1].(runtime.String)
		if !ok {
			return nil, fmt.Errorf("errors.wrap: second argument must be a string message")
		}
		wrapped := msg.Value
		if err.Message != "" {
			wrapped = msg.Value + ": " + err.Message
		}
		return runtime.Error{Class: err.Class, Message: wrapped}, nil
	})

	r.Register("errors", "stackTrace", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 1 {
			return nil, fmt.Errorf("errors.stackTrace expects one argument")
		}
		err, ok := args[0].(runtime.Error)
		if !ok {
			return nil, fmt.Errorf("errors.stackTrace: argument must be an error, got %s", args[0].TypeName())
		}
		return buildErrorStackTrace(err), nil
	})

	r.Register("errors", "frames", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 1 {
			return nil, fmt.Errorf("errors.frames expects one argument")
		}
		err, ok := args[0].(runtime.Error)
		if !ok {
			return nil, fmt.Errorf("errors.frames: argument must be an error, got %s", args[0].TypeName())
		}
		return errorStackFrameList(buildErrorStackTrace(err).Frames), nil
	})

	r.Register("errors", "hasStackTrace", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 1 {
			return nil, fmt.Errorf("errors.hasStackTrace expects one argument")
		}
		err, ok := args[0].(runtime.Error)
		if !ok {
			return nil, fmt.Errorf("errors.hasStackTrace: argument must be an error, got %s", args[0].TypeName())
		}
		return runtime.Bool{Value: err.HasStackTrace()}, nil
	})
}

func builtinErrorTypeMatches(class, target string) bool {
	if target == "" || target == "Error" {
		return true
	}
	for current := class; current != ""; current = builtinErrorParent(current) {
		if current == target {
			return true
		}
	}
	return false
}

func ParseErrorStackTrace(raw string) runtime.ErrorStackTrace {
	trace := runtime.ErrorStackTrace{Raw: raw}
	lines := strings.Split(raw, "\n")
	for _, line := range lines {
		matches := stackFramePattern.FindStringSubmatch(line)
		if matches == nil {
			continue
		}
		frame := runtime.ErrorStackFrame{Function: strings.TrimSpace(matches[1])}
		if len(matches) > 2 && matches[2] != "" {
			if n, err := strconv.ParseInt(matches[2], 10, 64); err == nil {
				frame.Line = n
			}
		}
		trace.Frames = append(trace.Frames, frame)
	}
	return trace
}

// maxFrameExpansion caps how many ErrorStackFrame entries one repeated StackFrame may produce.
const maxFrameExpansion = 100

// buildErrorStackTrace prefers structured TraceFrames; falls back to string parsing.
func buildErrorStackTrace(err runtime.Error) runtime.ErrorStackTrace {
	raw := err.ResolvedStackTrace()
	if len(err.TraceFrames) == 0 {
		return ParseErrorStackTrace(raw)
	}
	var frames []runtime.ErrorStackFrame
	// innermost frame uses ErrorLine (not CallLine)
	firstName := err.TraceFrames[0].Name
	if firstName == "" {
		firstName = "<closure>"
	}
	frames = append(frames, runtime.ErrorStackFrame{Function: firstName, Line: int64(err.ErrorLine)})
	for _, sf := range err.TraceFrames[1:] {
		name := sf.Name
		if name == "" {
			name = "<closure>"
		}
		repeat := sf.Repeat
		if repeat < 1 {
			repeat = 1
		}
		if repeat > maxFrameExpansion {
			repeat = maxFrameExpansion
		}
		for i := 0; i < repeat; i++ {
			frames = append(frames, runtime.ErrorStackFrame{Function: name, Line: int64(sf.CallLine)})
		}
	}
	frames = append(frames, runtime.ErrorStackFrame{Function: "<top level>", Line: int64(err.TopLevelLine)})
	return runtime.ErrorStackTrace{Raw: raw, Frames: frames}
}

func ErrorStackTraceMethod(trace runtime.ErrorStackTrace, name string, args []runtime.Value) (runtime.Value, error) {
	switch name {
	case "frames":
		if len(args) != 0 {
			return nil, fmt.Errorf("errors.StackTrace.frames expects no arguments")
		}
		values := make([]runtime.Value, len(trace.Frames))
		for i, frame := range trace.Frames {
			values[i] = frame
		}
		return &runtime.List{Elements: values}, nil
	case "length":
		if len(args) != 0 {
			return nil, fmt.Errorf("errors.StackTrace.length expects no arguments")
		}
		return runtime.SmallInt{Value: int64(len(trace.Frames))}, nil
	case "first":
		if len(args) != 0 {
			return nil, fmt.Errorf("errors.StackTrace.first expects no arguments")
		}
		if len(trace.Frames) == 0 {
			return runtime.Null{}, nil
		}
		return trace.Frames[0], nil
	case "toList":
		if len(args) != 0 {
			return nil, fmt.Errorf("errors.StackTrace.toList expects no arguments")
		}
		return errorStackFrameList(trace.Frames), nil
	case "toString":
		if len(args) != 0 {
			return nil, fmt.Errorf("errors.StackTrace.toString expects no arguments")
		}
		return runtime.String{Value: trace.Raw}, nil
	default:
		return nil, fmt.Errorf("errors.StackTrace has no method %s", name)
	}
}

func ErrorMethod(err runtime.Error, name string, args []runtime.Value) (runtime.Value, error) {
	switch name {
	case "getMessage":
		if len(args) != 0 {
			return nil, fmt.Errorf("%s.getMessage expects no arguments", err.Class)
		}
		return runtime.String{Value: err.Message}, nil
	case "getClass":
		if len(args) != 0 {
			return nil, fmt.Errorf("%s.getClass expects no arguments", err.Class)
		}
		return runtime.String{Value: err.Class}, nil
	case "stackTrace":
		if len(args) != 0 {
			return nil, fmt.Errorf("%s.stackTrace expects no arguments", err.Class)
		}
		return buildErrorStackTrace(err), nil
	case "frames":
		if len(args) != 0 {
			return nil, fmt.Errorf("%s.frames expects no arguments", err.Class)
		}
		return errorStackFrameList(buildErrorStackTrace(err).Frames), nil
	case "hasStackTrace":
		if len(args) != 0 {
			return nil, fmt.Errorf("%s.hasStackTrace expects no arguments", err.Class)
		}
		return runtime.Bool{Value: err.HasStackTrace()}, nil
	default:
		return nil, fmt.Errorf("%s has no method %s", err.Class, name)
	}
}

func ErrorStackFrameMethod(frame runtime.ErrorStackFrame, name string, args []runtime.Value) (runtime.Value, error) {
	switch name {
	case "function":
		if len(args) != 0 {
			return nil, fmt.Errorf("errors.Frame.function expects no arguments")
		}
		return runtime.String{Value: frame.Function}, nil
	case "line":
		if len(args) != 0 {
			return nil, fmt.Errorf("errors.Frame.line expects no arguments")
		}
		return runtime.NewInt64(frame.Line), nil
	case "toDict":
		if len(args) != 0 {
			return nil, fmt.Errorf("errors.Frame.toDict expects no arguments")
		}
		return errorStackFrameDict(frame), nil
	case "toString":
		if len(args) != 0 {
			return nil, fmt.Errorf("errors.Frame.toString expects no arguments")
		}
		if frame.Line > 0 {
			return runtime.String{Value: fmt.Sprintf("at %s (line %d)", frame.Function, frame.Line)}, nil
		}
		return runtime.String{Value: "at " + frame.Function}, nil
	default:
		return nil, fmt.Errorf("errors.Frame has no method %s", name)
	}
}

func errorStackFrameList(frames []runtime.ErrorStackFrame) *runtime.List {
	values := make([]runtime.Value, len(frames))
	for i, frame := range frames {
		values[i] = errorStackFrameDict(frame)
	}
	return &runtime.List{Elements: values}
}

func errorStackFrameDict(frame runtime.ErrorStackFrame) runtime.Dict {
	functionKey := runtime.String{Value: "function"}
	lineKey := runtime.String{Value: "line"}
	return runtime.Dict{Entries: map[string]runtime.DictEntry{
		DictKey(functionKey): {Key: functionKey, Value: runtime.String{Value: frame.Function}},
		DictKey(lineKey):     {Key: lineKey, Value: runtime.NewInt64(frame.Line)},
	}}
}

func builtinErrorParent(class string) string {
	switch class {
	case "TimeoutError", "TlsError":
		return "IOError"
	case "RuntimeError", "TypeError", "ValueError", "IOError", "ParseError", "MatchError", "ImmutableError":
		return "Error"
	default:
		return ""
	}
}
