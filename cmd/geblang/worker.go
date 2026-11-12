package main

import (
	"fmt"
	"io"
	"regexp"

	"geblang/internal/native"
)

var workerModuleName = regexp.MustCompile(`^[A-Za-z_][A-Za-z_0-9]*(\.[A-Za-z_][A-Za-z_0-9]*)*$`)

func runWorkerCommand(args []string, stdout, stderr io.Writer) (int, error) {
	entry := ""
	mode := executionAuto
	trace := false
	var allowFFI, forwarded []string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--entry":
			if entry != "" || i+1 >= len(args) || !workerModuleName.MatchString(args[i+1]) {
				return 2, fmt.Errorf("worker: --entry requires one canonical module name")
			}
			i++
			entry = args[i]
		case "--disable-vm":
			mode = executionEvaluatorOnly
		case "--vm-strict", "--vm":
			mode = executionVMStrict
		case "--trace-exec":
			trace = true
		case "--allow-ffi":
			if i+1 >= len(args) {
				return 2, fmt.Errorf("worker: --allow-ffi requires a library pattern")
			}
			i++
			allowFFI = append(allowFFI, args[i])
		case "--allow-process-control":
			native.SetProcessControlEnabled(true)
		case "--allow-onnx":
			native.SetOnnxEnabled(true)
		case "--allow-browser":
			native.SetBrowserEnabled(true)
		case "--help", "-h":
			printHelp(stdout, "worker")
			return 0, nil
		default:
			forwarded = append(forwarded, args[i])
		}
	}
	if entry == "" {
		return 2, fmt.Errorf("worker: specify --entry module.name")
	}
	code, err := runModule(entry, forwarded, mode, trace, allowFFI, stdout, stderr)
	if err != nil && code == 0 {
		code = 1
	}
	return code, err
}
