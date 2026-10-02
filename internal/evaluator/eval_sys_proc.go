package evaluator

import (
	"bytes"
	"context"
	"fmt"
	"geblang/internal/ast"
	"geblang/internal/modules"
	"geblang/internal/native"
	"geblang/internal/runtime"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"
)

func cleanupProcess(process *processHandle) {
	if process.cancel != nil {
		process.cancel()
	}
	if process.stdin != nil {
		_ = process.stdin.Close()
	}
	if process.stdout != nil {
		_ = process.stdout.Close()
	}
	if process.stderr != nil {
		_ = process.stderr.Close()
	}
	if process.cmd != nil && process.cmd.Process != nil && process.cmd.ProcessState == nil {
		_ = process.cmd.Process.Kill()
		_ = process.cmd.Wait()
	}
}

func (e *Evaluator) processObjectClasses() []*runtime.Class {
	resultClass := &runtime.Class{
		Name:   "Result",
		Module: "process",
		Fields: []runtime.Field{
			{Name: "code"}, {Name: "stdout"}, {Name: "stderr"}, {Name: "timedOut"},
		},
		Methods: map[string][]runtime.Function{},
	}
	resultClass.Methods["isok"] = []runtime.Function{{Name: "isOk", Native: func(this *runtime.Instance, _ []runtime.Value) (runtime.Value, error) {
		code, ok := this.Fields["code"].(runtime.Int)
		if !ok {
			return runtime.Bool{Value: false}, nil
		}
		return runtime.Bool{Value: code.Value.Sign() == 0}, nil
	}}}
	resultClass.Methods["code"] = []runtime.Function{{Name: "code", Native: func(this *runtime.Instance, _ []runtime.Value) (runtime.Value, error) {
		return this.Fields["code"], nil
	}}}
	resultClass.Methods["stdout"] = []runtime.Function{{Name: "stdout", Native: func(this *runtime.Instance, _ []runtime.Value) (runtime.Value, error) {
		return this.Fields["stdout"], nil
	}}}
	resultClass.Methods["stderr"] = []runtime.Function{{Name: "stderr", Native: func(this *runtime.Instance, _ []runtime.Value) (runtime.Value, error) {
		return this.Fields["stderr"], nil
	}}}
	resultClass.Methods["timedout"] = []runtime.Function{{Name: "timedOut", Native: func(this *runtime.Instance, _ []runtime.Value) (runtime.Value, error) {
		return this.Fields["timedOut"], nil
	}}}

	processClass := &runtime.Class{
		Name:    "Process",
		Module:  "process",
		Fields:  []runtime.Field{{Name: "handle"}},
		Methods: map[string][]runtime.Function{},
	}
	getHandle := func(this *runtime.Instance) (*processHandle, error) {
		return e.processHandle(this.Fields["handle"])
	}
	processClass.Methods["write"] = []runtime.Function{{Name: "write", Native: func(this *runtime.Instance, args []runtime.Value) (runtime.Value, error) {
		if len(args) != 1 {
			return nil, fmt.Errorf("Process.write expects 1 argument")
		}
		proc, err := getHandle(this)
		if err != nil {
			return nil, err
		}
		text, ok := args[0].(runtime.String)
		if !ok {
			return nil, fmt.Errorf("Process.write argument must be string")
		}
		n, err := io.WriteString(proc.stdin, text.Value)
		if err != nil {
			return nil, err
		}
		return runtime.NewInt64(int64(n)), nil
	}}}
	processClass.Methods["closestdin"] = []runtime.Function{{Name: "closeStdin", Native: func(this *runtime.Instance, _ []runtime.Value) (runtime.Value, error) {
		proc, err := getHandle(this)
		if err != nil {
			return nil, err
		}
		return runtime.Null{}, proc.stdin.Close()
	}}}
	processClass.Methods["readstdout"] = []runtime.Function{{Name: "readStdout", Native: func(this *runtime.Instance, _ []runtime.Value) (runtime.Value, error) {
		proc, err := getHandle(this)
		if err != nil {
			return nil, err
		}
		data, err := io.ReadAll(proc.stdout)
		if err != nil {
			return nil, err
		}
		return runtime.String{Value: string(data)}, nil
	}}}
	processClass.Methods["readstderr"] = []runtime.Function{{Name: "readStderr", Native: func(this *runtime.Instance, _ []runtime.Value) (runtime.Value, error) {
		proc, err := getHandle(this)
		if err != nil {
			return nil, err
		}
		data, err := io.ReadAll(proc.stderr)
		if err != nil {
			return nil, err
		}
		return runtime.String{Value: string(data)}, nil
	}}}
	processClass.Methods["readstdoutn"] = []runtime.Function{{Name: "readStdoutN", Native: func(this *runtime.Instance, args []runtime.Value) (runtime.Value, error) {
		if len(args) != 1 {
			return nil, fmt.Errorf("Process.readStdoutN expects 1 argument")
		}
		n, ok := args[0].(runtime.Int)
		if !ok {
			return nil, fmt.Errorf("Process.readStdoutN argument must be int")
		}
		proc, err := getHandle(this)
		if err != nil {
			return nil, err
		}
		buf := make([]byte, n.Value.Int64())
		nr, _ := proc.stdout.Read(buf)
		return runtime.String{Value: string(buf[:nr])}, nil
	}}}
	processClass.Methods["readstderrn"] = []runtime.Function{{Name: "readStderrN", Native: func(this *runtime.Instance, args []runtime.Value) (runtime.Value, error) {
		if len(args) != 1 {
			return nil, fmt.Errorf("Process.readStderrN expects 1 argument")
		}
		n, ok := args[0].(runtime.Int)
		if !ok {
			return nil, fmt.Errorf("Process.readStderrN argument must be int")
		}
		proc, err := getHandle(this)
		if err != nil {
			return nil, err
		}
		buf := make([]byte, n.Value.Int64())
		nr, _ := proc.stderr.Read(buf)
		return runtime.String{Value: string(buf[:nr])}, nil
	}}}
	processClass.Methods["wait"] = []runtime.Function{{Name: "wait", Native: func(this *runtime.Instance, _ []runtime.Value) (runtime.Value, error) {
		handle, err := processHandleID(this.Fields["handle"])
		if err != nil {
			return nil, err
		}
		e.processMu.Lock()
		proc, ok := e.processes[handle]
		if ok {
			delete(e.processes, handle)
		}
		e.processMu.Unlock()
		if !ok {
			return nil, fmt.Errorf("unknown process handle %d", handle)
		}
		waitErr := proc.cmd.Wait()
		if proc.cancel != nil {
			proc.cancel()
		}
		code := int64(0)
		if waitErr != nil {
			if exitErr, ok := waitErr.(*exec.ExitError); ok {
				code = int64(exitErr.ExitCode())
			} else {
				return nil, waitErr
			}
		}
		return runtime.NewInt64(code), nil
	}}}
	processClass.Methods["kill"] = []runtime.Function{{Name: "kill", Native: func(this *runtime.Instance, _ []runtime.Value) (runtime.Value, error) {
		proc, err := getHandle(this)
		if err != nil {
			return nil, err
		}
		if proc.cancel != nil {
			proc.cancel()
		}
		return runtime.Null{}, proc.cmd.Process.Kill()
	}}}
	processClass.Methods["signal"] = []runtime.Function{{Name: "signal", Native: func(this *runtime.Instance, args []runtime.Value) (runtime.Value, error) {
		if len(args) != 1 {
			return nil, fmt.Errorf("Process.signal expects 1 argument")
		}
		proc, err := getHandle(this)
		if err != nil {
			return nil, err
		}
		name, ok := args[0].(runtime.String)
		if !ok {
			return nil, fmt.Errorf("Process.signal name must be string")
		}
		sig, err := signalByName(name.Value)
		if err != nil {
			return nil, err
		}
		if proc.cancel != nil && sig == syscall.SIGKILL {
			proc.cancel()
		}
		return runtime.Null{}, proc.cmd.Process.Signal(sig)
	}}}
	processClass.Methods["pid"] = []runtime.Function{{Name: "pid", Native: func(this *runtime.Instance, _ []runtime.Value) (runtime.Value, error) {
		proc, err := getHandle(this)
		if err != nil {
			return nil, err
		}
		if proc.cmd.Process == nil {
			return nil, fmt.Errorf("process has not started")
		}
		return runtime.NewInt64(int64(proc.cmd.Process.Pid)), nil
	}}}

	return []*runtime.Class{processClass, resultClass}
}

func (e *Evaluator) newProcessResult(raw runtime.Value) (runtime.Value, error) {
	dict, ok := raw.(runtime.Dict)
	if !ok {
		return raw, nil
	}
	inst := &runtime.Instance{Class: e.processResultClass, Fields: map[string]runtime.Value{}}
	if v, ok := dictField(dict, "code"); ok {
		inst.Fields["code"] = v
	}
	if v, ok := dictField(dict, "stdout"); ok {
		inst.Fields["stdout"] = v
	}
	if v, ok := dictField(dict, "stderr"); ok {
		inst.Fields["stderr"] = v
	}
	if v, ok := dictField(dict, "timedOut"); ok {
		inst.Fields["timedOut"] = v
	}
	return inst, nil
}

func sysCwd(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 0 {
		return nil, fmt.Errorf("%s expects no arguments", call.Callee.String())
	}
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	return runtime.String{Value: cwd}, nil
}

func sysGetenv(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	name, err := singleStringValue(call, args)
	if err != nil {
		return nil, err
	}
	value, ok := os.LookupEnv(name)
	if !ok {
		return runtime.Null{}, nil
	}
	return runtime.String{Value: value}, nil
}

// sysBundleDir returns the bundle extract directory for a built binary, or ""
// when not running from a bundle, so programs resolve embedded resources
// against it (and against the project dir when empty).
func sysBundleDir(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 0 {
		return nil, fmt.Errorf("%s expects no arguments", call.Callee.String())
	}
	return runtime.String{Value: os.Getenv("GEBLANG_BUNDLE_DIR")}, nil
}

func (e *Evaluator) sysPackageRoot(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 0 {
		return nil, fmt.Errorf("%s expects no arguments", call.Callee.String())
	}
	for _, base := range e.modulePaths {
		manifest, err := modules.NewResolver([]string{base}).FindManifest(base)
		if err != nil {
			return nil, err
		}
		if manifest != nil {
			return runtime.String{Value: manifest.Root}, nil
		}
	}
	return runtime.String{Value: ""}, nil
}

func (e *Evaluator) sysArgs(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 0 {
		return nil, fmt.Errorf("%s expects no arguments", call.Callee.String())
	}
	values := make([]runtime.Value, 0, len(e.args))
	for _, arg := range e.args {
		values = append(values, runtime.String{Value: arg})
	}
	return &runtime.List{Elements: values}, nil
}

func sysRun(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("%s expects at least one argument", call.Callee.String())
	}
	spec, err := processSpecFromCombinedArgs(call, args)
	if err != nil {
		return nil, err
	}
	return runProcessSpec(spec)
}

func sysRunWithOptions(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	spec, err := processSpecFromOptions(call, args)
	if err != nil {
		return nil, err
	}
	return runProcessSpec(spec)
}

func runProcessSpec(spec processSpec) (runtime.Value, error) {
	cmd, cancel, ctx := commandFromSpec(spec)
	if cancel != nil {
		defer cancel()
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	exitCode := int64(0)
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = int64(exitErr.ExitCode())
		} else {
			return nil, err
		}
	}
	entries := map[string]runtime.DictEntry{}
	put := func(key string, value runtime.Value) {
		keyValue := runtime.String{Value: key}
		entries[dictKey(keyValue)] = runtime.DictEntry{Key: keyValue, Value: value}
	}
	put("code", runtime.NewInt64(exitCode))
	put("stdout", runtime.String{Value: stdout.String()})
	put("stderr", runtime.String{Value: stderr.String()})
	put("timedOut", runtime.Bool{Value: ctx.Err() == context.DeadlineExceeded})
	return runtime.Dict{Entries: entries}, nil
}

func sysShell(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	command, err := singleStringValue(call, args)
	if err != nil {
		return nil, err
	}
	shellCall := &ast.CallExpression{Callee: call.Callee}
	return sysRun(shellCall, []runtime.Value{runtime.String{Value: "sh"}, runtime.String{Value: "-c"}, runtime.String{Value: command}})
}

func processSpecFromCombinedArgs(call *ast.CallExpression, args []runtime.Value) (processSpec, error) {
	if len(args) < 1 {
		return processSpec{}, fmt.Errorf("%s expects at least one argument", call.Callee.String())
	}
	command, ok := args[0].(runtime.String)
	if !ok {
		return processSpec{}, fmt.Errorf("%s command must be string", call.Callee.String())
	}
	// Two forms: ("cmd", ["arg1", "arg2"]) or ("cmd", "arg1", "arg2", ...)
	if len(args) == 2 {
		if list, ok := args[1].(*runtime.List); ok {
			commandArgs := make([]string, 0, len(list.Elements))
			for _, elem := range list.Elements {
				s, ok := elem.(runtime.String)
				if !ok {
					return processSpec{}, fmt.Errorf("%s argument list elements must be strings", call.Callee.String())
				}
				commandArgs = append(commandArgs, s.Value)
			}
			return processSpec{command: command.Value, args: commandArgs}, nil
		}
	}
	commandArgs := make([]string, 0, len(args)-1)
	for _, arg := range args[1:] {
		s, ok := arg.(runtime.String)
		if !ok {
			return processSpec{}, fmt.Errorf("%s arguments must be strings", call.Callee.String())
		}
		commandArgs = append(commandArgs, s.Value)
	}
	return processSpec{command: command.Value, args: commandArgs}, nil
}

func processSpecFromOptions(call *ast.CallExpression, args []runtime.Value) (processSpec, error) {
	if len(args) != 1 {
		return processSpec{}, fmt.Errorf("%s expects exactly one argument", call.Callee.String())
	}
	options, ok := args[0].(runtime.Dict)
	if !ok {
		return processSpec{}, fmt.Errorf("%s options must be dict", call.Callee.String())
	}
	command, ok := dictStringField(options, "command")
	if !ok || command == "" {
		return processSpec{}, fmt.Errorf("%s options.command must be string", call.Callee.String())
	}
	spec := processSpec{command: command}
	if value, ok := dictField(options, "args"); ok {
		list, ok := value.(*runtime.List)
		if !ok {
			return processSpec{}, fmt.Errorf("%s options.args must be list<string>", call.Callee.String())
		}
		spec.args = make([]string, 0, len(list.Elements))
		for _, element := range list.Elements {
			text, ok := element.(runtime.String)
			if !ok {
				return processSpec{}, fmt.Errorf("%s options.args must be list<string>", call.Callee.String())
			}
			spec.args = append(spec.args, text.Value)
		}
	}
	if cwd, ok := dictStringField(options, "cwd"); ok {
		spec.cwd = cwd
	}
	if value, ok := dictField(options, "env"); ok {
		env, ok := value.(runtime.Dict)
		if !ok {
			return processSpec{}, fmt.Errorf("%s options.env must be dict<string, string>", call.Callee.String())
		}
		spec.env = map[string]string{}
		for _, __dk := range env.EntryKeys() {
			entry, _ := env.GetEntry(__dk)
			key, keyOK := entry.Key.(runtime.String)
			value, valueOK := entry.Value.(runtime.String)
			if !keyOK || !valueOK {
				return processSpec{}, fmt.Errorf("%s options.env must be dict<string, string>", call.Callee.String())
			}
			spec.env[key.Value] = value.Value
		}
	}
	if value, ok := dictField(options, "timeoutMs"); ok {
		n, ok := native.AsInt64(value)
		if !ok {
			return processSpec{}, fmt.Errorf("%s options.timeoutMs must be int", call.Callee.String())
		}
		if n < 0 {
			return processSpec{}, fmt.Errorf("%s options.timeoutMs must be >= 0", call.Callee.String())
		}
		spec.timeout = time.Duration(n) * time.Millisecond
	}
	return spec, nil
}

func commandFromSpec(spec processSpec) (*exec.Cmd, context.CancelFunc, context.Context) {
	ctx := context.Background()
	var cancel context.CancelFunc
	if spec.timeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, spec.timeout)
	}
	cmd := exec.CommandContext(ctx, spec.command, spec.args...)
	if spec.cwd != "" {
		cmd.Dir = spec.cwd
	}
	if len(spec.env) > 0 {
		env := os.Environ()
		for key, value := range spec.env {
			env = append(env, key+"="+value)
		}
		cmd.Env = env
	}
	return cmd, cancel, ctx
}

func (e *Evaluator) sysStart(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("%s expects at least one argument", call.Callee.String())
	}
	spec, err := processSpecFromCombinedArgs(call, args)
	if err != nil {
		return nil, err
	}
	return e.startProcessSpec(spec)
}

func (e *Evaluator) sysStartWithOptions(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	spec, err := processSpecFromOptions(call, args)
	if err != nil {
		return nil, err
	}
	return e.startProcessSpec(spec)
}

func (e *Evaluator) startProcessSpec(spec processSpec) (runtime.Value, error) {
	cmd, cancel, _ := commandFromSpec(spec)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		if cancel != nil {
			cancel()
		}
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		if cancel != nil {
			cancel()
		}
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		if cancel != nil {
			cancel()
		}
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		if cancel != nil {
			cancel()
		}
		return nil, err
	}
	e.processMu.Lock()
	defer e.processMu.Unlock()
	e.nextProcID++
	e.processes[e.nextProcID] = &processHandle{cmd: cmd, stdin: stdin, stdout: stdout, stderr: stderr, cancel: cancel}
	return runtime.NewInt64(e.nextProcID), nil
}

func (e *Evaluator) sysProcessWrite(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("%s expects exactly two arguments", call.Callee.String())
	}
	process, err := e.processHandle(args[0])
	if err != nil {
		return nil, err
	}
	text, ok := args[1].(runtime.String)
	if !ok {
		return nil, fmt.Errorf("%s content must be string", call.Callee.String())
	}
	n, err := io.WriteString(process.stdin, text.Value)
	if err != nil {
		return nil, err
	}
	return runtime.NewInt64(int64(n)), nil
}

func (e *Evaluator) sysProcessCloseStdin(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	process, err := e.singleProcessHandle(call, args)
	if err != nil {
		return nil, err
	}
	return runtime.Null{}, process.stdin.Close()
}

func (e *Evaluator) sysProcessReadStdout(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	process, err := e.singleProcessHandle(call, args)
	if err != nil {
		return nil, err
	}
	data, err := io.ReadAll(process.stdout)
	if err != nil {
		return nil, err
	}
	return runtime.String{Value: string(data)}, nil
}

func (e *Evaluator) sysProcessReadStderr(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	process, err := e.singleProcessHandle(call, args)
	if err != nil {
		return nil, err
	}
	data, err := io.ReadAll(process.stderr)
	if err != nil {
		return nil, err
	}
	return runtime.String{Value: string(data)}, nil
}

func (e *Evaluator) sysProcessReadStdoutN(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	process, size, err := e.processReadNArgs(call, args)
	if err != nil {
		return nil, err
	}
	return readProcessPipeN(call, process.stdout, size)
}

func (e *Evaluator) sysProcessReadStderrN(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	process, size, err := e.processReadNArgs(call, args)
	if err != nil {
		return nil, err
	}
	return readProcessPipeN(call, process.stderr, size)
}

func (e *Evaluator) processReadNArgs(call *ast.CallExpression, args []runtime.Value) (*processHandle, int64, error) {
	if len(args) != 2 {
		return nil, 0, fmt.Errorf("%s expects process and byte count", call.Callee.String())
	}
	process, err := e.processHandle(args[0])
	if err != nil {
		return nil, 0, err
	}
	n, ok := toInt64(args[1])
	if !ok {
		return nil, 0, fmt.Errorf("%s byte count must be int", call.Callee.String())
	}
	if n < 0 || n > 1<<30 {
		return nil, 0, fmt.Errorf("%s byte count out of range", call.Callee.String())
	}
	return process, n, nil
}

func readProcessPipeN(call *ast.CallExpression, reader io.Reader, n int64) (runtime.Value, error) {
	buf := make([]byte, n)
	read, err := reader.Read(buf)
	if err != nil && err != io.EOF {
		return nil, err
	}
	return runtime.String{Value: string(buf[:read])}, nil
}

func (e *Evaluator) sysProcessWait(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	handle, err := singleProcessHandleID(call, args)
	if err != nil {
		return nil, err
	}
	e.processMu.Lock()
	process, ok := e.processes[handle]
	if ok {
		delete(e.processes, handle)
	}
	e.processMu.Unlock()
	if !ok {
		return nil, fmt.Errorf("unknown process handle %d", handle)
	}
	err = process.cmd.Wait()
	if process.cancel != nil {
		process.cancel()
	}
	code := int64(0)
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			code = int64(exitErr.ExitCode())
		} else {
			return nil, err
		}
	}
	return runtime.NewInt64(code), nil
}

func (e *Evaluator) sysProcessKill(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	process, err := e.singleProcessHandle(call, args)
	if err != nil {
		return nil, err
	}
	if process.cancel != nil {
		process.cancel()
	}
	return runtime.Null{}, process.cmd.Process.Kill()
}

func (e *Evaluator) sysProcessSignal(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("%s expects process and signal name", call.Callee.String())
	}
	process, err := e.processHandle(args[0])
	if err != nil {
		return nil, err
	}
	name, ok := args[1].(runtime.String)
	if !ok {
		return nil, fmt.Errorf("%s signal name must be string", call.Callee.String())
	}
	signal, err := signalByName(name.Value)
	if err != nil {
		return nil, err
	}
	if process.cancel != nil && signal == syscall.SIGKILL {
		process.cancel()
	}
	return runtime.Null{}, process.cmd.Process.Signal(signal)
}

func (e *Evaluator) sysProcessPid(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	process, err := e.singleProcessHandle(call, args)
	if err != nil {
		return nil, err
	}
	if process.cmd.Process == nil {
		return nil, fmt.Errorf("process has not started")
	}
	return runtime.NewInt64(int64(process.cmd.Process.Pid)), nil
}

func signalByName(name string) (os.Signal, error) {
	switch strings.ToUpper(strings.TrimPrefix(name, "SIG")) {
	case "TERM":
		return syscall.SIGTERM, nil
	case "KILL":
		return syscall.SIGKILL, nil
	case "INT":
		return syscall.SIGINT, nil
	case "HUP":
		return syscall.SIGHUP, nil
	case "QUIT":
		return syscall.SIGQUIT, nil
	default:
		if sig, ok := platformSignalByName(strings.ToUpper(strings.TrimPrefix(name, "SIG"))); ok {
			return sig, nil
		}
		return nil, fmt.Errorf("unsupported signal %q", name)
	}
}

func (e *Evaluator) singleProcessHandle(call *ast.CallExpression, args []runtime.Value) (*processHandle, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("%s expects exactly one argument", call.Callee.String())
	}
	return e.processHandle(args[0])
}

func (e *Evaluator) processHandle(value runtime.Value) (*processHandle, error) {
	handle, err := processHandleID(value)
	if err != nil {
		return nil, err
	}
	e.processMu.Lock()
	process, ok := e.processes[handle]
	e.processMu.Unlock()
	if !ok && e.parent != nil {
		return e.parent.processHandle(value)
	}
	if !ok {
		return nil, fmt.Errorf("unknown process handle %d", handle)
	}
	return process, nil
}

func singleProcessHandleID(call *ast.CallExpression, args []runtime.Value) (int64, error) {
	if len(args) != 1 {
		return 0, fmt.Errorf("%s expects exactly one argument", call.Callee.String())
	}
	return processHandleID(args[0])
}

func processHandleID(value runtime.Value) (int64, error) {
	id, ok := native.AsInt64(value)
	if !ok {
		return 0, fmt.Errorf("process handle must be int")
	}
	return id, nil
}

// ptyEIOReader wraps the master side of a pseudo-terminal so that
// the Linux-specific EIO returned after the child closes its end
// reads as io.EOF. POSIX defines this behaviour for ptys; Geblang
// users shouldn't see the raw errno.
type ptyEIOReader struct {
	r io.Reader
}

func (p *ptyEIOReader) Read(buf []byte) (int, error) {
	n, err := p.r.Read(buf)
	if err != nil {
		if pathErr, ok := err.(*os.PathError); ok && pathErr.Err == syscall.EIO {
			return n, io.EOF
		}
	}
	return n, err
}

// procSpawn starts a child process and returns a dict with handle,
// pid, and three IOStream handles (stdin, stdout, stderr). PTY mode
// (opts["pty"] == true) uses github.com/creack/pty so stdin/stdout
// share the master pty fd and stderr is null.
func (e *Evaluator) procSpawn(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) < 1 || len(args) > 3 {
		return nil, fmt.Errorf("%s expects command, optional args list, optional options", call.Callee.String())
	}
	cmdName, ok := args[0].(runtime.String)
	if !ok {
		return nil, fmt.Errorf("%s command must be string", call.Callee.String())
	}
	var cmdArgs []string
	if len(args) >= 2 {
		switch a := args[1].(type) {
		case *runtime.List:
			cmdArgs = make([]string, 0, len(a.Elements))
			for _, elem := range a.Elements {
				s, ok := elem.(runtime.String)
				if !ok {
					return nil, fmt.Errorf("%s args list elements must be strings", call.Callee.String())
				}
				cmdArgs = append(cmdArgs, s.Value)
			}
		case runtime.Null:
			cmdArgs = nil
		default:
			return nil, fmt.Errorf("%s args must be a list of strings or null", call.Callee.String())
		}
	}
	usePTY := false
	var workDir string
	var envEntries []string
	if len(args) == 3 {
		opts, ok := args[2].(runtime.Dict)
		if !ok {
			return nil, fmt.Errorf("%s options must be a dict", call.Callee.String())
		}
		if value, found := dictField(opts, "pty"); found {
			b, ok := value.(runtime.Bool)
			if !ok {
				return nil, fmt.Errorf("%s options.pty must be bool", call.Callee.String())
			}
			usePTY = b.Value
		}
		if value, found := dictField(opts, "cwd"); found {
			s, ok := value.(runtime.String)
			if !ok {
				return nil, fmt.Errorf("%s options.cwd must be string", call.Callee.String())
			}
			workDir = s.Value
		}
		if value, found := dictField(opts, "env"); found {
			if d, ok := value.(runtime.Dict); ok {
				for _, __dk := range d.EntryKeys() {
					entry, _ := d.GetEntry(__dk)
					k, kok := entry.Key.(runtime.String)
					v, vok := entry.Value.(runtime.String)
					if !kok || !vok {
						return nil, fmt.Errorf("%s options.env keys and values must be strings", call.Callee.String())
					}
					envEntries = append(envEntries, k.Value+"="+v.Value)
				}
			} else {
				return nil, fmt.Errorf("%s options.env must be a dict", call.Callee.String())
			}
		}
	}
	cmd := exec.Command(cmdName.Value, cmdArgs...)
	if workDir != "" {
		cmd.Dir = workDir
	}
	if envEntries != nil {
		cmd.Env = envEntries
	}
	handle := &processHandle{cmd: cmd}
	var stdinStream, stdoutStream, stderrStream *ioStreamHandle
	if usePTY {
		ptyFile, err := pty.Start(cmd)
		if err != nil {
			return nil, err
		}
		handle.stdin = ptyFile
		handle.stdout = ptyFile
		var ptyCloseOnce sync.Once
		handle.cancel = func() {
			ptyCloseOnce.Do(func() { _ = ptyFile.Close() })
		}
		// On Linux, reading the pty master after the child exits
		// returns EIO; translate to EOF so io.read* gives a clean
		// end-of-stream signal rather than surfacing the errno.
		ptyReader := &ptyEIOReader{r: ptyFile}
		// Both stdin and stdout streams alias the master pty fd. The
		// fd itself is closed by handle.cancel (which procWait /
		// procKill invoke); the per-stream Close() should be a no-op
		// so stopping a closed handle stays idempotent.
		stdinStream = &ioStreamHandle{name: "proc stdin (pty)", writer: ptyFile}
		stdoutStream = &ioStreamHandle{name: "proc stdout (pty)", reader: ptyReader}
		stderrStream = nil
	} else {
		stdin, err := cmd.StdinPipe()
		if err != nil {
			return nil, err
		}
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			_ = stdin.Close()
			return nil, err
		}
		stderr, err := cmd.StderrPipe()
		if err != nil {
			_ = stdin.Close()
			return nil, err
		}
		if err := cmd.Start(); err != nil {
			_ = stdin.Close()
			return nil, err
		}
		handle.stdin = stdin
		handle.stdout = stdout
		handle.stderr = stderr
		stdinStream = &ioStreamHandle{name: "proc stdin", writer: stdin, closer: stdin}
		stdoutStream = &ioStreamHandle{name: "proc stdout", reader: stdout}
		stderrStream = &ioStreamHandle{name: "proc stderr", reader: stderr}
	}
	e.processMu.Lock()
	e.nextProcID++
	procID := e.nextProcID
	e.processes[procID] = handle
	e.processMu.Unlock()
	stdinVal := e.registerIOStream(stdinStream)
	stdoutVal := e.registerIOStream(stdoutStream)
	var stderrVal runtime.Value = runtime.Null{}
	if stderrStream != nil {
		stderrVal = e.registerIOStream(stderrStream)
	}
	entries := map[string]runtime.DictEntry{}
	putDict(entries, "handle", runtime.NewInt64(procID))
	putDict(entries, "pid", runtime.NewInt64(int64(cmd.Process.Pid)))
	putDict(entries, "stdin", stdinVal)
	putDict(entries, "stdout", stdoutVal)
	putDict(entries, "stderr", stderrVal)
	return runtime.Dict{Entries: entries}, nil
}

func (e *Evaluator) procWait(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	process, err := e.singleProcessHandle(call, args)
	if err != nil {
		return nil, err
	}
	err = process.cmd.Wait()
	if process.cancel != nil {
		process.cancel()
	}
	code := int64(0)
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			code = int64(exitErr.ExitCode())
		} else {
			return nil, err
		}
	}
	return runtime.NewInt64(code), nil
}

func (e *Evaluator) procKill(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	process, err := e.singleProcessHandle(call, args)
	if err != nil {
		return nil, err
	}
	if process.cancel != nil {
		process.cancel()
	}
	if process.cmd.Process == nil {
		return runtime.Null{}, nil
	}
	return runtime.Null{}, process.cmd.Process.Kill()
}

func (e *Evaluator) procSignal(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("%s expects (process, signalName)", call.Callee.String())
	}
	process, err := e.processHandle(args[0])
	if err != nil {
		return nil, err
	}
	name, ok := args[1].(runtime.String)
	if !ok {
		return nil, fmt.Errorf("%s signal name must be string", call.Callee.String())
	}
	signal, err := signalByName(name.Value)
	if err != nil {
		return nil, err
	}
	if process.cmd.Process == nil {
		return nil, fmt.Errorf("process has not started")
	}
	if process.cancel != nil && signal == syscall.SIGKILL {
		process.cancel()
	}
	return runtime.Null{}, process.cmd.Process.Signal(signal)
}

func (e *Evaluator) procPid(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	process, err := e.singleProcessHandle(call, args)
	if err != nil {
		return nil, err
	}
	if process.cmd.Process == nil {
		return nil, fmt.Errorf("process has not started")
	}
	return runtime.NewInt64(int64(process.cmd.Process.Pid)), nil
}

func sysSetenv(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("%s expects exactly two arguments", call.Callee.String())
	}
	name, ok := args[0].(runtime.String)
	if !ok {
		return nil, fmt.Errorf("%s name must be string", call.Callee.String())
	}
	value, ok := args[1].(runtime.String)
	if !ok {
		return nil, fmt.Errorf("%s value must be string", call.Callee.String())
	}
	if err := os.Setenv(name.Value, value.Value); err != nil {
		return nil, err
	}
	return runtime.Null{}, nil
}

func sysSleep(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("%s expects one argument (milliseconds)", call.Callee.String())
	}
	if v, ok := args[0].(runtime.Float); ok {
		if v.Value > 0 {
			time.Sleep(time.Duration(v.Value * float64(time.Millisecond)))
		}
		return runtime.Null{}, nil
	}
	ms, ok := native.AsInt64(args[0])
	if !ok {
		return nil, fmt.Errorf("%s expects a numeric millisecond value", call.Callee.String())
	}
	if ms > 0 {
		time.Sleep(time.Duration(ms) * time.Millisecond)
	}
	return runtime.Null{}, nil
}
