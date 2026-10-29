package evaluator

import (
	"fmt"
	"io"
	"strings"

	"geblang/internal/ast"
	"geblang/internal/native"
	"geblang/internal/runtime"
)

func (e *Evaluator) cliReadLine(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) > 1 {
		return nil, fmt.Errorf("cli.readLine expects an optional label")
	}
	if len(args) == 1 {
		label, ok := args[0].(runtime.String)
		if !ok {
			return nil, runtime.ClassifiedError{Class: "TypeError", Message: "cli.readLine label must be a string"}
		}
		_, _ = io.WriteString(e.promptWriter(), label.Value)
	}
	line, eof, err := e.input.readLine()
	if err != nil {
		return nil, err
	}
	if eof {
		return runtime.Null{}, nil
	}
	return runtime.String{Value: line}, nil
}

func (e *Evaluator) validateCLIValue(value runtime.Value, spec runtime.Dict) (string, error) {
	message, err := native.ValidateCLIValue(value, spec)
	if message != "" || err != nil {
		return message, err
	}
	validator, ok := dictField(spec, "validate")
	if !ok {
		return "", nil
	}
	result, err := e.callValue(validator, []runtime.Value{value})
	if err != nil {
		return "", err
	}
	switch result := result.(type) {
	case runtime.Null:
		return "", nil
	case runtime.String:
		if result.Value == "" {
			return "invalid value", nil
		}
		return result.Value, nil
	default:
		return "", runtime.ClassifiedError{Class: "TypeError", Message: "input validator must return string or null"}
	}
}

func (e *Evaluator) cliParseArgs(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("cli.parseArgs expects arguments and specification")
	}
	argv, ok := args[0].(*runtime.List)
	if !ok {
		return nil, fmt.Errorf("cli.parseArgs arguments must be a list")
	}
	spec, ok := args[1].(runtime.Dict)
	if !ok {
		return nil, fmt.Errorf("cli.parseArgs specification must be a dict")
	}
	return native.ParseArgvValidated(argv, spec, e.validateCLIValue), nil
}

func (e *Evaluator) cliPromptInt(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) < 1 || len(args) > 2 {
		return nil, fmt.Errorf("cli.promptInt expects label and optional options")
	}
	label, ok := args[0].(runtime.String)
	if !ok {
		return nil, fmt.Errorf("cli.promptInt label must be a string")
	}
	options := runtime.NewDict()
	if len(args) == 2 {
		options, ok = args[1].(runtime.Dict)
		if !ok {
			return nil, fmt.Errorf("cli.promptInt options must be a dict")
		}
	}
	if err := native.ValidateCLIRules(options); err != nil {
		return nil, err
	}
	if validator, present := dictField(options, "validate"); present && !runtime.IsCallableValue(validator) {
		return nil, runtime.ClassifiedError{Class: "TypeError", Message: "input validator must be callable"}
	}
	cancel := []string{}
	if value, present := dictField(options, "cancel"); present {
		items, ok := value.(*runtime.List)
		if !ok {
			return nil, fmt.Errorf("cli.promptInt cancel must be a list of strings")
		}
		for _, item := range items.Elements {
			text, ok := item.(runtime.String)
			if !ok {
				return nil, fmt.Errorf("cli.promptInt cancel must be a list of strings")
			}
			cancel = append(cancel, strings.ToLower(strings.TrimSpace(text.Value)))
		}
	}
	defaultValue, hasDefault := dictField(options, "default")
	if hasDefault {
		if _, ok := native.IntValueToBigInt(defaultValue); !ok {
			return nil, fmt.Errorf("cli.promptInt default must be an int")
		}
		message, err := e.validateCLIValue(defaultValue, options)
		if err != nil {
			return nil, err
		}
		if message != "" {
			return nil, runtime.ClassifiedError{Class: "ValueError", Message: "cli.promptInt default " + message}
		}
	}
	for {
		_, _ = io.WriteString(e.promptWriter(), label.Value)
		text, eof, err := e.input.readLine()
		if err != nil {
			return nil, err
		}
		if eof {
			return runtime.Null{}, nil
		}
		text = strings.TrimSpace(text)
		if text == "" && hasDefault {
			return defaultValue, nil
		}
		for _, token := range cancel {
			if strings.EqualFold(token, text) {
				return runtime.Null{}, nil
			}
		}
		value, err := native.StringNumberOrNull(text, "toIntOrNull", nil)
		if err != nil {
			return nil, err
		}
		if runtime.IsNull(value) {
			_, _ = fmt.Fprintln(e.promptWriter(), "Please enter a whole number.")
			continue
		}
		message, err := e.validateCLIValue(value, options)
		if err != nil {
			return nil, err
		}
		if message != "" {
			_, _ = fmt.Fprintln(e.promptWriter(), message)
			continue
		}
		return value, nil
	}
}
