package runtime

import (
	"fmt"
	"strings"
)

// StackFrame is one trace frame, innermost first; CallLine is where the next-inner call occurs (the innermost frame displays UncaughtError.ErrorLine instead).
type StackFrame struct {
	Name     string
	CallLine int
	Repeat   int
}

// UncaughtError is the canonical top-level error contract for both backends; Error() is the rendered form so consumers print it verbatim.
type UncaughtError struct {
	Class        string
	Message      string
	ErrorLine    int
	Frames       []StackFrame
	TopLevelLine int
}

func (u *UncaughtError) Error() string { return u.Render() }

func (u *UncaughtError) Render() string {
	var sb strings.Builder
	sb.WriteString("uncaught ")
	sb.WriteString(u.Class)
	if u.Message != "" {
		sb.WriteString(": ")
		sb.WriteString(u.Message)
	}
	if len(u.Frames) > 0 {
		first := u.Frames[0]
		writeFrameLine(&sb, first.Name, u.ErrorLine)
		if r := normalizeRepeat(first.Repeat); r > 1 {
			writeFrameRepeat(&sb, first.Name, first.CallLine, r)
		}
		for _, f := range u.Frames[1:] {
			if r := normalizeRepeat(f.Repeat); r > 1 {
				writeFrameRepeat(&sb, f.Name, f.CallLine, r)
			} else {
				writeFrameLine(&sb, f.Name, f.CallLine)
			}
		}
	}
	writeFrameLine(&sb, "<top level>", u.TopLevelLine)
	return sb.String()
}

func writeFrameLine(sb *strings.Builder, name string, line int) {
	if name == "" {
		name = "<closure>"
	}
	if line > 0 {
		fmt.Fprintf(sb, "\n  at %s (line %d)", name, line)
	} else {
		fmt.Fprintf(sb, "\n  at %s", name)
	}
}

func writeFrameRepeat(sb *strings.Builder, name string, line, repeat int) {
	if name == "" {
		name = "<closure>"
	}
	if line > 0 {
		fmt.Fprintf(sb, "\n  at %s (line %d) [x%d]", name, line, repeat)
	} else {
		fmt.Fprintf(sb, "\n  at %s [x%d]", name, repeat)
	}
}

func normalizeRepeat(r int) int {
	if r < 1 {
		return 1
	}
	return r
}

// CollapseFrames merges consecutive identical (Name, CallLine) frames into one with accumulated Repeat.
func CollapseFrames(frames []StackFrame) []StackFrame {
	out := make([]StackFrame, 0, len(frames))
	for _, f := range frames {
		r := normalizeRepeat(f.Repeat)
		if n := len(out); n > 0 && out[n-1].Name == f.Name && out[n-1].CallLine == f.CallLine {
			out[n-1].Repeat += r
			continue
		}
		f.Repeat = r
		out = append(out, f)
	}
	return out
}

// RenderDestructorFailure formats a contained destructor cleanup failure: prefix + class/message + real frames only (no uncaught header, no top-level line).
func RenderDestructorFailure(className string, err Error) string {
	var sb strings.Builder
	sb.WriteString("destructor for ")
	sb.WriteString(className)
	sb.WriteString(": ")
	sb.WriteString(err.Inspect())
	if len(err.TraceFrames) > 0 {
		frames := err.TraceFrames
		first := frames[0]
		writeFrameLine(&sb, first.Name, err.ErrorLine)
		if r := normalizeRepeat(first.Repeat); r > 1 {
			writeFrameRepeat(&sb, first.Name, first.CallLine, r)
		}
		for _, f := range frames[1:] {
			if r := normalizeRepeat(f.Repeat); r > 1 {
				writeFrameRepeat(&sb, f.Name, f.CallLine, r)
			} else {
				writeFrameLine(&sb, f.Name, f.CallLine)
			}
		}
	}
	return sb.String()
}

// WithModuleFrame records that err escaped a module's top level during import; the importer supplies the top-level line.
func WithModuleFrame(err Error, module string) Error {
	err.TraceFrames = append(append([]StackFrame(nil), err.TraceFrames...), StackFrame{Name: "<module " + module + ">", CallLine: err.TopLevelLine})
	err.TopLevelLine = 0
	return err
}
