// Package dap implements the Debug Adapter Protocol for Geblang.
package dap

// Message is the base DAP message envelope.
type Message struct {
	Seq     int    `json:"seq"`
	Type    string `json:"type"` // "request", "response", "event"
	Command string `json:"command,omitempty"`
	Event   string `json:"event,omitempty"`
	// Request fields
	Arguments any `json:"arguments,omitempty"`
	// Response fields
	RequestSeq int    `json:"request_seq,omitempty"`
	Success    bool   `json:"success,omitempty"`
	Body       any    `json:"body,omitempty"`
	Message    string `json:"message,omitempty"`
}

// ---- request argument types ----

type InitializeArgs struct {
	ClientName                   string `json:"clientName"`
	AdapterID                    string `json:"adapterID"`
	PathFormat                   string `json:"pathFormat"`
	LinesStartAt1                bool   `json:"linesStartAt1"`
	ColumnsStartAt1              bool   `json:"columnsStartAt1"`
	SupportsVariableType         bool   `json:"supportsVariableType"`
	SupportsRunInTerminalRequest bool   `json:"supportsRunInTerminalRequest"`
}

type LaunchArgs struct {
	Program     string   `json:"program"`
	Cwd         string   `json:"cwd"`
	Args        []string `json:"args"`
	StopOnEntry bool     `json:"stopOnEntry"`
}

type SetBreakpointsArgs struct {
	Source      Source             `json:"source"`
	Breakpoints []SourceBreakpoint `json:"breakpoints"`
}

type SourceBreakpoint struct {
	Line      int    `json:"line"`
	Condition string `json:"condition,omitempty"`
}

type StackTraceArgs struct {
	ThreadID   int `json:"threadId"`
	StartFrame int `json:"startFrame"`
	Levels     int `json:"levels"`
}

type ScopesArgs struct {
	FrameID int `json:"frameId"`
}

type VariablesArgs struct {
	VariablesReference int `json:"variablesReference"`
}

type ContinueArgs struct {
	ThreadID     int  `json:"threadId"`
	SingleThread bool `json:"singleThread"`
}

type NextArgs struct {
	ThreadID     int  `json:"threadId"`
	SingleThread bool `json:"singleThread"`
}

type SetVariableArgs struct {
	VariablesReference int    `json:"variablesReference"`
	Name               string `json:"name"`
	Value              string `json:"value"`
}

type PauseArgs struct {
	ThreadID int `json:"threadId"`
}

type EvaluateArgs struct {
	Expression string `json:"expression"`
	FrameID    int    `json:"frameId"`
	ThreadID   int    `json:"threadId"`
	Context    string `json:"context"` // "watch", "hover", "repl"
}

type ExceptionInfoArgs struct {
	ThreadID int `json:"threadId"`
}

// ---- response body types ----

type InitializeResponseBody struct {
	SupportsConfigurationDoneRequest      bool `json:"supportsConfigurationDoneRequest"`
	SupportsSingleThreadExecutionRequests bool `json:"supportsSingleThreadExecutionRequests"`
	SupportsEvaluateForHovers             bool `json:"supportsEvaluateForHovers"`
	SupportsTerminateRequest              bool `json:"supportsTerminateRequest"`
	SupportsSetVariable                   bool `json:"supportsSetVariable"`
	SupportsConditionalBreakpoints        bool `json:"supportsConditionalBreakpoints"`
	SupportsExceptionInfoRequest          bool `json:"supportsExceptionInfoRequest"`
}

type SetBreakpointsResponseBody struct {
	Breakpoints []Breakpoint `json:"breakpoints"`
}

type Breakpoint struct {
	Verified bool `json:"verified"`
	Line     int  `json:"line"`
}

type ThreadsResponseBody struct {
	Threads []Thread `json:"threads"`
}

type Thread struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type StackTraceResponseBody struct {
	StackFrames []StackFrame `json:"stackFrames"`
	TotalFrames int          `json:"totalFrames"`
}

type StackFrame struct {
	ID     int    `json:"id"`
	Name   string `json:"name"`
	Source Source `json:"source"`
	Line   int    `json:"line"`
	Column int    `json:"column"`
}

type Source struct {
	Name string `json:"name,omitempty"`
	Path string `json:"path,omitempty"`
}

type ScopesResponseBody struct {
	Scopes []Scope `json:"scopes"`
}

type Scope struct {
	Name               string `json:"name"`
	VariablesReference int    `json:"variablesReference"`
	Expensive          bool   `json:"expensive"`
}

type VariablesResponseBody struct {
	Variables []Variable `json:"variables"`
}

type Variable struct {
	Name               string `json:"name"`
	Value              string `json:"value"`
	Type               string `json:"type,omitempty"`
	VariablesReference int    `json:"variablesReference"`
}

type SetVariableResponseBody struct {
	Value string `json:"value"`
	Type  string `json:"type,omitempty"`
}

type EvaluateResponseBody struct {
	Result             string `json:"result"`
	Type               string `json:"type,omitempty"`
	VariablesReference int    `json:"variablesReference"`
}

type ExceptionInfoResponseBody struct {
	ExceptionID string `json:"exceptionId"`
	Description string `json:"description,omitempty"`
	BreakMode   string `json:"breakMode"` // "always", "unhandled"
}

type ContinueResponseBody struct {
	AllThreadsContinued bool `json:"allThreadsContinued"`
}

// ---- event body types ----

type StoppedEventBody struct {
	Reason            string `json:"reason"`
	Description       string `json:"description,omitempty"`
	ThreadID          int    `json:"threadId"`
	AllThreadsStopped bool   `json:"allThreadsStopped"`
	Text              string `json:"text,omitempty"`
}

type OutputEventBody struct {
	Category string `json:"category"`
	Output   string `json:"output"`
}

type ExitedEventBody struct {
	ExitCode int `json:"exitCode"`
}

type ThreadEventBody struct {
	Reason   string `json:"reason"`
	ThreadID int    `json:"threadId"`
}
