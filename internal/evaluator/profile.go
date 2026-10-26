package evaluator

import (
	"geblang/internal/profiling"
	"strconv"
)

// profileSample records this evaluator's Geblang call stack once per
// profiling tick, at the statement about to run. Only its own stack is
// walked: a parent evaluator may be running on another goroutine.
func (e *Evaluator) profileSample() {
	t := profiling.Tick()
	if t == e.profileTick {
		return
	}
	e.profileTick = t
	frames := make([]string, 0, len(e.callStack)+1)
	frames = append(frames, "<top level>")
	leaf := "main.<top level>"
	module := "main"
	for _, f := range e.callStack {
		// A closure records no module of its own; it keeps its caller's.
		if f.fn != nil && f.fn.DefinitionModule != "" {
			module = f.fn.DefinitionModule
		} else if f.fn != nil && f.fn.OwnerClass != nil {
			module = f.fn.OwnerClass.DefinitionModule
			if module == "" {
				module = "main"
			}
		}
		leaf = module + "." + f.name
		frames = append(frames, leaf)
	}
	if e.currentLine > 0 {
		leaf += ":" + strconv.Itoa(e.currentLine)
	} else {
		leaf = ""
	}
	profiling.Record(frames, leaf)
}
