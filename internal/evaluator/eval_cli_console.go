package evaluator

import (
	"bufio"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"fmt"
	"geblang/internal/ast"
	"geblang/internal/native"
	"geblang/internal/runtime"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"unicode"

	"golang.org/x/text/width"
)

func secretsGetEnv(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
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

func secretsRequireEnv(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	name, err := singleStringValue(call, args)
	if err != nil {
		return nil, err
	}
	value, ok := os.LookupEnv(name)
	if !ok {
		return nil, fmt.Errorf("required secret environment variable %s is not set", name)
	}
	return runtime.String{Value: value}, nil
}

func secretsReadFile(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	path, err := singleStringValue(call, args)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return runtime.String{Value: strings.TrimRight(string(data), "\r\n")}, nil
}

func constantTimeComparableEqual(left []byte, right []byte) bool {
	key := []byte("geblang.secrets.constantTimeEqual.v1")
	leftMAC := hmac.New(sha256.New, key)
	_, _ = leftMAC.Write(left)
	rightMAC := hmac.New(sha256.New, key)
	_, _ = rightMAC.Write(right)
	digestEqual := subtle.ConstantTimeCompare(leftMAC.Sum(nil), rightMAC.Sum(nil))
	lengthEqual := constantTimeIntEqual(len(left), len(right))
	return digestEqual&lengthEqual == 1
}

func constantTimeIntEqual(left int, right int) int {
	diff := uint64(left ^ right)
	diff |= diff >> 32
	diff |= diff >> 16
	diff |= diff >> 8
	diff |= diff >> 4
	diff |= diff >> 2
	diff |= diff >> 1
	return int((diff & 1) ^ 1)
}

func secureRandomBytes(call *ast.CallExpression, args []runtime.Value) ([]byte, error) {
	size, err := singleIntValue(call, args)
	if err != nil {
		return nil, err
	}
	if size < 0 || size > 1<<20 {
		return nil, fmt.Errorf("%s byte count out of range", call.Callee.String())
	}
	data := make([]byte, size)
	if _, err := rand.Read(data); err != nil {
		return nil, err
	}
	return data, nil
}

func secretComparableBytes(value runtime.Value) ([]byte, error) {
	switch value := value.(type) {
	case runtime.String:
		return []byte(value.Value), nil
	case runtime.Bytes:
		return value.Value, nil
	default:
		return nil, fmt.Errorf("secret comparison expects string or bytes")
	}
}

func schemaValidate(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("%s expects value and schema", call.Callee.String())
	}
	schema, ok := args[1].(runtime.Dict)
	if !ok {
		return nil, fmt.Errorf("%s schema must be dict", call.Callee.String())
	}
	errors := []runtime.Value{}
	validateValueAgainstSchema(args[0], schema, "$", &errors)
	entries := map[string]runtime.DictEntry{}
	putDict(entries, "valid", runtime.Bool{Value: len(errors) == 0})
	putDict(entries, "errors", &runtime.List{Elements: errors})
	return runtime.Dict{Entries: entries}, nil
}

func validateValueAgainstSchema(value runtime.Value, schema runtime.Dict, path string, errors *[]runtime.Value) {
	if typeName, ok := dictStringField(schema, "type"); ok {
		if !schemaTypeMatches(value, typeName) {
			*errors = append(*errors, runtime.String{Value: path + ": expected " + typeName + ", got " + value.TypeName()})
			return
		}
	}
	if enumValue, ok := dictField(schema, "enum"); ok {
		if values, ok := enumValue.(*runtime.List); ok {
			found := false
			for _, allowed := range values.Elements {
				if valuesEqualSimple(value, allowed) {
					found = true
					break
				}
			}
			if !found {
				*errors = append(*errors, runtime.String{Value: path + ": value is not in enum"})
			}
		}
	}
	if propertiesValue, ok := dictField(schema, "properties"); ok {
		properties, ok := propertiesValue.(runtime.Dict)
		if !ok {
			*errors = append(*errors, runtime.String{Value: path + ": schema.properties must be dict"})
			return
		}
		object, ok := value.(runtime.Dict)
		if !ok {
			*errors = append(*errors, runtime.String{Value: path + ": expected dict for properties"})
			return
		}
		required := schemaRequiredFields(schema)
		properties.ForEachEntry(func(_ string, entry runtime.DictEntry) bool {
			key, ok := entry.Key.(runtime.String)
			if !ok {
				*errors = append(*errors, runtime.String{Value: path + ": schema property keys must be strings"})
				return true
			}
			propertySchema, ok := entry.Value.(runtime.Dict)
			if !ok {
				*errors = append(*errors, runtime.String{Value: path + "." + key.Value + ": property schema must be dict"})
				return true
			}
			propertyValue, exists := object.GetEntry(dictKey(key))
			if !exists {
				if required[key.Value] {
					*errors = append(*errors, runtime.String{Value: path + "." + key.Value + ": required field is missing"})
				}
				return true
			}
			validateValueAgainstSchema(propertyValue.Value, propertySchema, path+"."+key.Value, errors)
			return true
		})
	}
	if itemsValue, ok := dictField(schema, "items"); ok {
		itemSchema, ok := itemsValue.(runtime.Dict)
		if !ok {
			*errors = append(*errors, runtime.String{Value: path + ": schema.items must be dict"})
			return
		}
		list, ok := value.(*runtime.List)
		if !ok {
			*errors = append(*errors, runtime.String{Value: path + ": expected list for items"})
			return
		}
		for i, item := range list.Elements {
			validateValueAgainstSchema(item, itemSchema, fmt.Sprintf("%s[%d]", path, i), errors)
		}
	}
}

func schemaTypeMatches(value runtime.Value, typeName string) bool {
	switch typeName {
	case "number":
		return value.TypeName() == "int" || value.TypeName() == "decimal" || value.TypeName() == "float"
	case "object":
		return value.TypeName() == "dict"
	case "array":
		return value.TypeName() == "list"
	default:
		return value.TypeName() == typeName
	}
}

func schemaRequiredFields(schema runtime.Dict) map[string]bool {
	required := map[string]bool{}
	value, ok := dictField(schema, "required")
	if !ok {
		return required
	}
	list, ok := value.(*runtime.List)
	if !ok {
		return required
	}
	for _, item := range list.Elements {
		if field, ok := item.(runtime.String); ok {
			required[field.Value] = true
		}
	}
	return required
}

func (e *Evaluator) serdeParse(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("%s expects format and text", call.Callee.String())
	}
	format, ok := args[0].(runtime.String)
	if !ok {
		return nil, fmt.Errorf("%s format must be string", call.Callee.String())
	}
	text, ok := args[1].(runtime.String)
	if !ok {
		return nil, fmt.Errorf("%s text must be string", call.Callee.String())
	}
	var (
		value    runtime.Value
		parseErr *native.ParseError
	)
	switch strings.ToLower(format.Value) {
	case "json":
		value, parseErr = native.ParseJSONText(text.Value)
	case "toml":
		value, parseErr = native.ParseTOMLText(text.Value)
	case "yaml", "yml":
		value, parseErr = native.ParseYAMLText(text.Value)
	default:
		return nil, fmt.Errorf("%s unsupported format %q", call.Callee.String(), format.Value)
	}
	if parseErr != nil {
		return nil, fmt.Errorf("%s", parseErr.Message)
	}
	return value, nil
}

func (e *Evaluator) serdeStringify(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("%s expects format and value", call.Callee.String())
	}
	format, ok := args[0].(runtime.String)
	if !ok {
		return nil, fmt.Errorf("%s format must be string", call.Callee.String())
	}
	var module string
	switch strings.ToLower(format.Value) {
	case "json":
		module = "json"
	case "toml":
		module = "toml"
	case "yaml", "yml":
		module = "yaml"
	default:
		return nil, fmt.Errorf("%s unsupported format %q", call.Callee.String(), format.Value)
	}
	return e.natives.Call(module, "stringify", []runtime.Value{args[1]})
}

func (e *Evaluator) dotenvParse(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	text, err := singleStringValue(call, args)
	if err != nil {
		return nil, err
	}
	return dotenvParseText(text), nil
}

func (e *Evaluator) dotenvLoad(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	path, err := singleStringValue(call, args)
	if err != nil {
		return nil, err
	}
	data, ioErr := os.ReadFile(path)
	if ioErr != nil {
		return nil, fmt.Errorf("%s: %v", call.Callee.String(), ioErr)
	}
	return dotenvParseText(string(data)), nil
}

func (e *Evaluator) dotenvApply(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("%s expects one dict argument", call.Callee.String())
	}
	d, ok := args[0].(runtime.Dict)
	if !ok {
		return nil, fmt.Errorf("%s argument must be a dict", call.Callee.String())
	}
	d.ForEachEntry(func(_ string, entry runtime.DictEntry) bool {
		k, ok := entry.Key.(runtime.String)
		if !ok {
			return true
		}
		v, ok := entry.Value.(runtime.String)
		if !ok {
			return true
		}
		os.Setenv(k.Value, v.Value)
		return true
	})
	return runtime.Null{}, nil
}

func (e *Evaluator) dotenvLoadAndApply(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	result, err := e.dotenvLoad(call, args)
	if err != nil {
		return nil, err
	}
	_, err = e.dotenvApply(call, []runtime.Value{result})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func dotenvParseText(text string) runtime.Dict {
	entries := map[string]runtime.DictEntry{}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "export ") {
			line = strings.TrimSpace(line[7:])
		}
		eqIdx := strings.IndexByte(line, '=')
		if eqIdx < 0 {
			continue
		}
		key := strings.TrimSpace(line[:eqIdx])
		if key == "" {
			continue
		}
		value := dotenvParseValue(line[eqIdx+1:])
		kv := runtime.String{Value: key}
		entries[dictKey(kv)] = runtime.DictEntry{Key: kv, Value: runtime.String{Value: value}}
	}
	return runtime.Dict{Entries: entries}
}

func dotenvParseValue(raw string) string {
	raw = strings.TrimLeft(raw, " \t")
	if len(raw) == 0 {
		return ""
	}
	if raw[0] == '"' {
		end := strings.LastIndexByte(raw, '"')
		if end > 0 {
			inner := raw[1:end]
			inner = strings.ReplaceAll(inner, `\n`, "\n")
			inner = strings.ReplaceAll(inner, `\t`, "\t")
			inner = strings.ReplaceAll(inner, `\r`, "\r")
			inner = strings.ReplaceAll(inner, `\"`, `"`)
			inner = strings.ReplaceAll(inner, `\\`, `\`)
			return inner
		}
	}
	if raw[0] == '\'' {
		end := strings.LastIndexByte(raw, '\'')
		if end > 0 {
			return raw[1:end]
		}
	}
	if idx := strings.Index(raw, " #"); idx >= 0 {
		raw = raw[:idx]
	}
	return strings.TrimRight(raw, " \t")
}

func (e *Evaluator) cliPrompt(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) < 1 || len(args) > 2 {
		return nil, fmt.Errorf("%s expects prompt and optional default", call.Callee.String())
	}
	prompt, ok := args[0].(runtime.String)
	if !ok {
		return nil, fmt.Errorf("%s prompt must be string", call.Callee.String())
	}
	defaultValue := ""
	if len(args) == 2 {
		value, ok := args[1].(runtime.String)
		if !ok {
			return nil, fmt.Errorf("%s default must be string", call.Callee.String())
		}
		defaultValue = value.Value
	}
	_, _ = io.WriteString(e.promptWriter(), prompt.Value)
	line, err := readConsoleLine()
	if err != nil {
		return nil, err
	}
	if line == "" && len(args) == 2 {
		line = defaultValue
	}
	return runtime.String{Value: line}, nil
}

func (e *Evaluator) cliPassword(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) > 1 {
		return nil, fmt.Errorf("%s expects optional prompt", call.Callee.String())
	}
	prompt := ""
	if len(args) == 1 {
		value, ok := args[0].(runtime.String)
		if !ok {
			return nil, fmt.Errorf("%s prompt must be string", call.Callee.String())
		}
		prompt = value.Value
	}
	_, _ = io.WriteString(e.promptWriter(), prompt)
	line, err := readConsoleSecret()
	if err != nil {
		return nil, err
	}
	_, _ = fmt.Fprintln(e.promptWriter())
	return runtime.String{Value: line}, nil
}

func (e *Evaluator) cliConfirm(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) < 1 || len(args) > 2 {
		return nil, fmt.Errorf("%s expects prompt and optional default bool", call.Callee.String())
	}
	prompt, ok := args[0].(runtime.String)
	if !ok {
		return nil, fmt.Errorf("%s prompt must be string", call.Callee.String())
	}
	defaultValue := false
	hasDefault := false
	if len(args) == 2 {
		value, ok := args[1].(runtime.Bool)
		if !ok {
			return nil, fmt.Errorf("%s default must be bool", call.Callee.String())
		}
		defaultValue = value.Value
		hasDefault = true
	}
	_, _ = io.WriteString(e.promptWriter(), prompt.Value)
	line, err := readConsoleLine()
	if err != nil {
		return nil, err
	}
	answer := strings.ToLower(strings.TrimSpace(line))
	if answer == "" && hasDefault {
		return runtime.Bool{Value: defaultValue}, nil
	}
	switch answer {
	case "y", "yes", "true", "1":
		return runtime.Bool{Value: true}, nil
	case "n", "no", "false", "0":
		return runtime.Bool{Value: false}, nil
	default:
		return nil, fmt.Errorf("%s expected yes or no", call.Callee.String())
	}
}

func (e *Evaluator) cliChoose(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) < 2 || len(args) > 3 {
		return nil, fmt.Errorf("%s expects prompt, options list, and optional default index", call.Callee.String())
	}
	prompt, ok := args[0].(runtime.String)
	if !ok {
		return nil, fmt.Errorf("%s prompt must be string", call.Callee.String())
	}
	options, ok := args[1].(*runtime.List)
	if !ok {
		return nil, fmt.Errorf("%s options must be list<string>", call.Callee.String())
	}
	choices := make([]string, 0, len(options.Elements))
	for _, option := range options.Elements {
		value, ok := option.(runtime.String)
		if !ok {
			return nil, fmt.Errorf("%s options must be list<string>", call.Callee.String())
		}
		choices = append(choices, value.Value)
	}
	if len(choices) == 0 {
		return nil, fmt.Errorf("%s options must not be empty", call.Callee.String())
	}
	defaultIndex := int64(-1)
	if len(args) == 3 {
		bi, ok := native.IntValueToBigInt(args[2])
		if !ok || !bi.IsInt64() {
			return nil, fmt.Errorf("%s default index must be int", call.Callee.String())
		}
		defaultIndex = bi.Int64()
		if defaultIndex < 0 || defaultIndex >= int64(len(choices)) {
			return nil, fmt.Errorf("%s default index out of range", call.Callee.String())
		}
	}
	_, _ = io.WriteString(e.promptWriter(), prompt.Value)
	for i, choice := range choices {
		_, _ = fmt.Fprintf(e.promptWriter(), "\n  %d) %s", i+1, choice)
	}
	_, _ = io.WriteString(e.promptWriter(), "\n> ")
	line, err := readConsoleLine()
	if err != nil {
		return nil, err
	}
	line = strings.TrimSpace(line)
	if line == "" && defaultIndex >= 0 {
		return runtime.String{Value: choices[defaultIndex]}, nil
	}
	index, err := strconv.ParseInt(line, 10, 64)
	if err == nil && index >= 1 && index <= int64(len(choices)) {
		return runtime.String{Value: choices[index-1]}, nil
	}
	for _, choice := range choices {
		if strings.EqualFold(choice, line) {
			return runtime.String{Value: choice}, nil
		}
	}
	return nil, fmt.Errorf("%s invalid choice %q", call.Callee.String(), line)
}

// cliMultiChoose presents a checkbox list and returns the selected
// options. On a TTY it runs an arrow-key UI (up/down or j/k move, space
// toggles, a toggles all, enter confirms, ctrl-c/q cancels); otherwise
// it falls back to numbered comma-separated input so piped / non-
// interactive use still works.
func (e *Evaluator) cliMultiChoose(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) < 2 || len(args) > 3 {
		return nil, fmt.Errorf("%s expects prompt, options list, and optional preselected index list", call.Callee.String())
	}
	prompt, ok := args[0].(runtime.String)
	if !ok {
		return nil, fmt.Errorf("%s prompt must be string", call.Callee.String())
	}
	options, ok := args[1].(*runtime.List)
	if !ok {
		return nil, fmt.Errorf("%s options must be list<string>", call.Callee.String())
	}
	choices := make([]string, 0, len(options.Elements))
	for _, option := range options.Elements {
		value, ok := option.(runtime.String)
		if !ok {
			return nil, fmt.Errorf("%s options must be list<string>", call.Callee.String())
		}
		choices = append(choices, value.Value)
	}
	if len(choices) == 0 {
		return nil, fmt.Errorf("%s options must not be empty", call.Callee.String())
	}
	checked := make([]bool, len(choices))
	if len(args) == 3 {
		pre, ok := args[2].(*runtime.List)
		if !ok {
			return nil, fmt.Errorf("%s preselected must be list<int>", call.Callee.String())
		}
		for _, p := range pre.Elements {
			bi, ok := native.IntValueToBigInt(p)
			if !ok || !bi.IsInt64() {
				return nil, fmt.Errorf("%s preselected must be list<int>", call.Callee.String())
			}
			if i := bi.Int64(); i >= 0 && i < int64(len(checked)) {
				checked[i] = true
			}
		}
	}
	selected, err := e.runMultiChoose(prompt.Value, choices, checked)
	if err != nil {
		return nil, err
	}
	elements := make([]runtime.Value, 0, len(selected))
	for _, s := range selected {
		elements = append(elements, runtime.String{Value: s})
	}
	return &runtime.List{Elements: elements, ElementTypes: []string{"string"}}, nil
}

type tuiKey int

const (
	keyNone tuiKey = iota
	keyUp
	keyDown
	keyToggle
	keyToggleAll
	keyConfirm
	keyCancel
)

type multiChooseState struct {
	cursor  int
	checked []bool
	done    bool
	cancel  bool
}

func (s *multiChooseState) apply(k tuiKey) {
	n := len(s.checked)
	if n == 0 {
		return
	}
	switch k {
	case keyUp:
		s.cursor = (s.cursor - 1 + n) % n
	case keyDown:
		s.cursor = (s.cursor + 1) % n
	case keyToggle:
		s.checked[s.cursor] = !s.checked[s.cursor]
	case keyToggleAll:
		all := true
		for _, c := range s.checked {
			if !c {
				all = false
				break
			}
		}
		for i := range s.checked {
			s.checked[i] = !all
		}
	case keyConfirm:
		s.done = true
	case keyCancel:
		s.cancel = true
	}
}

func selectedChoices(choices []string, checked []bool) []string {
	out := []string{}
	for i, c := range checked {
		if c {
			out = append(out, choices[i])
		}
	}
	return out
}

func (e *Evaluator) runMultiChoose(label string, choices []string, checked []bool) ([]string, error) {
	// Injected input (io.withStdin) drives the key loop directly, no termios.
	if override := consoleInputOverride(); override != nil {
		return e.multiChooseLoop(label, choices, checked, override)
	}
	fd := int(os.Stdin.Fd())
	restore, err := enterRawMode(fd)
	if err != nil {
		return e.multiChooseFallback(label, choices, checked)
	}
	defer restore()
	return e.multiChooseLoop(label, choices, checked, bufio.NewReader(os.Stdin))
}

func (e *Evaluator) multiChooseLoop(label string, choices []string, checked []bool, in *bufio.Reader) ([]string, error) {
	state := &multiChooseState{checked: checked}
	e.renderMultiChoose(label, choices, state, false)
	for {
		key, err := readTUIKey(in)
		if err == io.EOF {
			// Input exhausted (ctrl-d or end of injected stream): confirm.
			break
		}
		if err != nil {
			return nil, err
		}
		state.apply(key)
		if state.cancel {
			_, _ = fmt.Fprintln(e.promptWriter())
			return nil, fmt.Errorf("cli.multiChoose: cancelled")
		}
		if state.done {
			break
		}
		e.renderMultiChoose(label, choices, state, true)
	}
	_, _ = fmt.Fprintln(e.promptWriter())
	return selectedChoices(choices, state.checked), nil
}

func readTUIKey(in *bufio.Reader) (tuiKey, error) {
	b, err := in.ReadByte()
	if err != nil {
		return keyNone, err
	}
	switch b {
	case 0x03, 'q', 'Q':
		return keyCancel, nil
	case '\r', '\n':
		return keyConfirm, nil
	case ' ':
		return keyToggle, nil
	case 'a', 'A':
		return keyToggleAll, nil
	case 'k', 'K':
		return keyUp, nil
	case 'j', 'J':
		return keyDown, nil
	case 0x1b:
		if b2, err := in.ReadByte(); err == nil && b2 == '[' {
			if b3, err := in.ReadByte(); err == nil {
				switch b3 {
				case 'A':
					return keyUp, nil
				case 'B':
					return keyDown, nil
				}
			}
		}
	}
	return keyNone, nil
}

func (e *Evaluator) renderMultiChoose(label string, choices []string, s *multiChooseState, redraw bool) {
	if redraw {
		// Move the cursor back up to the first option row to repaint.
		_, _ = fmt.Fprintf(e.promptWriter(), "\x1b[%dA", len(choices))
	} else {
		_, _ = fmt.Fprintf(e.promptWriter(), "%s (up/down move, space toggles, enter confirms)\n", label)
	}
	for i, choice := range choices {
		box := "[ ]"
		if s.checked[i] {
			box = "[x]"
		}
		if i == s.cursor {
			_, _ = fmt.Fprintf(e.promptWriter(), "\x1b[2K\x1b[7m> %s %s\x1b[0m\n", box, choice)
		} else {
			_, _ = fmt.Fprintf(e.promptWriter(), "\x1b[2K  %s %s\n", box, choice)
		}
	}
}

func (e *Evaluator) multiChooseFallback(label string, choices []string, checked []bool) ([]string, error) {
	_, _ = io.WriteString(e.promptWriter(), label)
	for i, choice := range choices {
		mark := " "
		if checked[i] {
			mark = "x"
		}
		_, _ = fmt.Fprintf(e.promptWriter(), "\n  %d) [%s] %s", i+1, mark, choice)
	}
	_, _ = io.WriteString(e.promptWriter(), "\nSelect (comma-separated numbers; blank keeps current): ")
	line, err := readConsoleLine()
	if err != nil {
		return nil, err
	}
	line = strings.TrimSpace(line)
	if line == "" {
		return selectedChoices(choices, checked), nil
	}
	result := make([]bool, len(choices))
	for _, part := range strings.Split(line, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if idx, err := strconv.ParseInt(part, 10, 64); err == nil && idx >= 1 && idx <= int64(len(choices)) {
			result[idx-1] = true
			continue
		}
		for i, choice := range choices {
			if strings.EqualFold(choice, part) {
				result[i] = true
			}
		}
	}
	return selectedChoices(choices, result), nil
}

// ioWithStdin runs the body callable with console input served from the
// given string instead of real stdin, restoring the previous source
// afterwards. Lets tests drive interactive prompts (line input or raw
// key sequences) deterministically.
func (e *Evaluator) ioWithStdin(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("%s expects an input string and a callable body", call.Callee.String())
	}
	input, ok := args[0].(runtime.String)
	if !ok {
		return nil, fmt.Errorf("%s input must be string", call.Callee.String())
	}
	prev := setConsoleInputOverride(bufio.NewReader(strings.NewReader(input.Value)))
	defer setConsoleInputOverride(prev)
	return e.callValue(args[1], []runtime.Value{})
}

// promptWriter is where interactive prompt / UI rendering goes. With
// injected input (io.withStdin) it is discarded so scripted prompts do
// not pollute output; this runs on the evaluator on both backends, so
// io.println (which does not use it) is unaffected and parity holds.
func (e *Evaluator) promptWriter() io.Writer {
	if consoleInputOverride() != nil {
		return io.Discard
	}
	return e.stdout
}

func readConsoleLine() (string, error) {
	reader := consoleLineReader()
	if override := consoleInputOverride(); override != nil {
		reader = override
	}
	line, err := reader.ReadString('\n')
	if err != nil && err != io.EOF {
		return "", err
	}
	line = strings.TrimRight(line, "\r\n")
	if err == io.EOF && line == "" {
		return "", nil
	}
	return line, nil
}

// consoleInputState holds an optional injected input stream. When set
// (via io.withStdin), the cli readers consume it instead of the real
// stdin and skip raw-mode termios, so interactive prompts are testable.
var consoleInputState struct {
	sync.Mutex
	reader *bufio.Reader
}

func consoleInputOverride() *bufio.Reader {
	consoleInputState.Lock()
	defer consoleInputState.Unlock()
	return consoleInputState.reader
}

func setConsoleInputOverride(r *bufio.Reader) *bufio.Reader {
	consoleInputState.Lock()
	defer consoleInputState.Unlock()
	prev := consoleInputState.reader
	consoleInputState.reader = r
	return prev
}

var consoleReaderState struct {
	sync.Mutex
	file   *os.File
	reader *bufio.Reader
}

func consoleLineReader() *bufio.Reader {
	consoleReaderState.Lock()
	defer consoleReaderState.Unlock()
	if consoleReaderState.reader == nil || consoleReaderState.file != os.Stdin {
		consoleReaderState.file = os.Stdin
		consoleReaderState.reader = bufio.NewReader(os.Stdin)
	}
	return consoleReaderState.reader
}

func readConsoleSecret() (string, error) {
	if consoleInputOverride() != nil {
		return readConsoleLine()
	}
	fd := int(os.Stdin.Fd())
	restore, err := disableEcho(fd)
	if err != nil {
		return readConsoleLine()
	}
	line, readErr := readConsoleLine()
	restore()
	return line, readErr
}

func cliStyle(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("%s expects text and style options", call.Callee.String())
	}
	text, ok := args[0].(runtime.String)
	if !ok {
		return nil, fmt.Errorf("%s text must be string", call.Callee.String())
	}
	codes, err := ansiStyleCodes(args[1])
	if err != nil {
		return nil, fmt.Errorf("%s: %w", call.Callee.String(), err)
	}
	if len(codes) == 0 {
		return text, nil
	}
	return runtime.String{Value: "\x1b[" + strings.Join(codes, ";") + "m" + text.Value + "\x1b[0m"}, nil
}

func ansiStyleCodes(value runtime.Value) ([]string, error) {
	switch value := value.(type) {
	case runtime.String:
		return ansiNamedStyle(value.Value)
	case runtime.Dict:
		codes := []string{}
		if truthyDictBool(value, "bold") {
			codes = append(codes, "1")
		}
		if truthyDictBool(value, "dim") {
			codes = append(codes, "2")
		}
		if truthyDictBool(value, "italic") {
			codes = append(codes, "3")
		}
		if truthyDictBool(value, "underline") {
			codes = append(codes, "4")
		}
		if truthyDictBool(value, "inverse") {
			codes = append(codes, "7")
		}
		if fg, ok := dictStringField(value, "fg"); ok {
			code, ok := ansiColorCode(fg, false)
			if !ok {
				return nil, fmt.Errorf("unknown foreground color %q", fg)
			}
			codes = append(codes, code)
		}
		if bg, ok := dictStringField(value, "bg"); ok {
			code, ok := ansiColorCode(bg, true)
			if !ok {
				return nil, fmt.Errorf("unknown background color %q", bg)
			}
			codes = append(codes, code)
		}
		return codes, nil
	default:
		return nil, fmt.Errorf("style options must be string or dict")
	}
}

func ansiNamedStyle(name string) ([]string, error) {
	code, ok := ansiColorCode(name, false)
	if ok {
		return []string{code}, nil
	}
	switch strings.ToLower(name) {
	case "bold":
		return []string{"1"}, nil
	case "dim":
		return []string{"2"}, nil
	case "italic":
		return []string{"3"}, nil
	case "underline":
		return []string{"4"}, nil
	case "inverse":
		return []string{"7"}, nil
	case "reset", "plain", "":
		return nil, nil
	default:
		return nil, fmt.Errorf("unknown style %q", name)
	}
}

func ansiColorCode(name string, background bool) (string, bool) {
	colors := map[string]int{
		"black": 0, "red": 1, "green": 2, "yellow": 3,
		"blue": 4, "magenta": 5, "cyan": 6, "white": 7,
	}
	index, ok := colors[strings.ToLower(name)]
	if !ok {
		return "", false
	}
	base := 30
	if background {
		base = 40
	}
	return strconv.Itoa(base + index), true
}

func truthyDictBool(dict runtime.Dict, key string) bool {
	value, ok := dictField(dict, key)
	if !ok {
		return false
	}
	boolValue, ok := value.(runtime.Bool)
	return ok && boolValue.Value
}

func cliStripANSI(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	text, err := singleStringValue(call, args)
	if err != nil {
		return nil, err
	}
	return runtime.String{Value: stripANSI(text)}, nil
}

func stripANSI(text string) string {
	var out strings.Builder
	inEscape := false
	for i := 0; i < len(text); i++ {
		ch := text[i]
		if inEscape {
			if ch >= '@' && ch <= '~' {
				inEscape = false
			}
			continue
		}
		if ch == 0x1b && i+1 < len(text) && text[i+1] == '[' {
			inEscape = true
			i++
			continue
		}
		out.WriteByte(ch)
	}
	return out.String()
}

func cliTable(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) < 1 || len(args) > 2 {
		return nil, fmt.Errorf("%s expects rows and optional options", call.Callee.String())
	}
	rows, ok := args[0].(*runtime.List)
	if !ok {
		return nil, fmt.Errorf("%s rows must be list", call.Callee.String())
	}
	columns := []string{}
	headers := []string{}
	separator := "  "
	if len(args) == 2 {
		switch opts := args[1].(type) {
		case *runtime.List:
			/* Backwards-compatible legacy form: bare list of header
			 * strings. Columns are inferred from the row dict keys. */
			for _, header := range opts.Elements {
				value, ok := header.(runtime.String)
				if !ok {
					return nil, fmt.Errorf("%s headers must be list<string>", call.Callee.String())
				}
				headers = append(headers, value.Value)
			}
			columns = append([]string(nil), headers...)
		case runtime.Dict:
			/* Documented form: an options dict with `columns`,
			 * `headers`, `separator`. All keys are optional. */
			if value, ok := dictField(opts, "columns"); ok {
				list, ok := value.(*runtime.List)
				if !ok {
					return nil, fmt.Errorf("%s options.columns must be list<string>", call.Callee.String())
				}
				for _, item := range list.Elements {
					s, ok := item.(runtime.String)
					if !ok {
						return nil, fmt.Errorf("%s options.columns must be list<string>", call.Callee.String())
					}
					columns = append(columns, s.Value)
				}
			}
			if value, ok := dictField(opts, "headers"); ok {
				list, ok := value.(*runtime.List)
				if !ok {
					return nil, fmt.Errorf("%s options.headers must be list<string>", call.Callee.String())
				}
				for _, item := range list.Elements {
					s, ok := item.(runtime.String)
					if !ok {
						return nil, fmt.Errorf("%s options.headers must be list<string>", call.Callee.String())
					}
					headers = append(headers, s.Value)
				}
			}
			if value, ok := dictField(opts, "separator"); ok {
				s, ok := value.(runtime.String)
				if !ok {
					return nil, fmt.Errorf("%s options.separator must be string", call.Callee.String())
				}
				separator = s.Value
			}
		default:
			return nil, fmt.Errorf("%s second argument must be list<string> or options dict", call.Callee.String())
		}
	}
	tableRows, inferred, err := cliTableRows(rows, columns)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", call.Callee.String(), err)
	}
	if len(columns) == 0 {
		columns = inferred
	}
	if len(headers) == 0 {
		headers = columns
	}
	return runtime.String{Value: renderTableWithSeparator(headers, tableRows, separator)}, nil
}

// cliSpinnerFrames are the unicode spinner phases used by
// cli.Spinner. Falls back to ASCII when stderr is not a TTY.
var cliSpinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// cliSpinnerTick renders one frame of an ANSI spinner to stderr.
// Args: (frameIndex: int, message: string). Returns the next frame
// index. The Geblang stdlib wrapper holds the index field and calls
// this on each .tick().
func (e *Evaluator) cliSpinnerTick(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("%s expects (frameIndex, message)", call.Callee.String())
	}
	idx, ok := native.AsInt64(args[0])
	if !ok {
		return nil, fmt.Errorf("%s frameIndex must be int", call.Callee.String())
	}
	msg, ok := args[1].(runtime.String)
	if !ok {
		return nil, fmt.Errorf("%s message must be string", call.Callee.String())
	}
	frame := cliSpinnerFrames[int(idx)%len(cliSpinnerFrames)]
	_, _ = fmt.Fprintf(e.stderr, "\r%s %s", frame, msg.Value)
	return runtime.NewInt64((idx + 1) % int64(len(cliSpinnerFrames))), nil
}

// cliSpinnerStop clears the spinner line.
func (e *Evaluator) cliSpinnerStop(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) > 1 {
		return nil, fmt.Errorf("%s expects (finalMessage?)", call.Callee.String())
	}
	final := ""
	if len(args) == 1 {
		s, ok := args[0].(runtime.String)
		if !ok {
			return nil, fmt.Errorf("%s finalMessage must be string", call.Callee.String())
		}
		final = s.Value
	}
	_, _ = fmt.Fprint(e.stderr, "\r\x1b[2K")
	if final != "" {
		_, _ = fmt.Fprintln(e.stderr, final)
	}
	return runtime.Null{}, nil
}

// cliProgressRender draws an ANSI progress bar to stderr.
// Args: (current: int, total: int, width: int = 30, label: string = "").
// Renders [#####-----] 50% (5/10) label.
func (e *Evaluator) cliProgressRender(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) < 2 || len(args) > 4 {
		return nil, fmt.Errorf("%s expects (current, total, width?, label?)", call.Callee.String())
	}
	current, ok := native.AsInt64(args[0])
	if !ok {
		return nil, fmt.Errorf("%s current must be int", call.Callee.String())
	}
	total, ok := native.AsInt64(args[1])
	if !ok {
		return nil, fmt.Errorf("%s total must be int", call.Callee.String())
	}
	width := int64(30)
	if len(args) >= 3 {
		n, ok := native.AsInt64(args[2])
		if !ok {
			return nil, fmt.Errorf("%s width must be int", call.Callee.String())
		}
		width = n
	}
	label := ""
	if len(args) == 4 {
		s, ok := args[3].(runtime.String)
		if !ok {
			return nil, fmt.Errorf("%s label must be string", call.Callee.String())
		}
		label = s.Value
	}
	if total <= 0 {
		total = 1
	}
	if current < 0 {
		current = 0
	}
	if current > total {
		current = total
	}
	filled := int(width * current / total)
	pct := int(100 * current / total)
	bar := strings.Repeat("#", filled) + strings.Repeat("-", int(width)-filled)
	line := fmt.Sprintf("\r[%s] %d%% (%d/%d)", bar, pct, current, total)
	if label != "" {
		line += " " + label
	}
	_, _ = fmt.Fprint(e.stderr, line)
	return runtime.Null{}, nil
}

// cliProgressFinish clears the progress line and optionally prints
// a final message.
func (e *Evaluator) cliProgressFinish(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) > 1 {
		return nil, fmt.Errorf("%s expects (finalMessage?)", call.Callee.String())
	}
	final := ""
	if len(args) == 1 {
		s, ok := args[0].(runtime.String)
		if !ok {
			return nil, fmt.Errorf("%s finalMessage must be string", call.Callee.String())
		}
		final = s.Value
	}
	_, _ = fmt.Fprint(e.stderr, "\r\x1b[2K")
	if final != "" {
		_, _ = fmt.Fprintln(e.stderr, final)
	}
	return runtime.Null{}, nil
}

func cliTableRows(rows *runtime.List, headers []string) ([][]string, []string, error) {
	out := [][]string{}
	inferred := append([]string(nil), headers...)
	for _, row := range rows.Elements {
		switch row := row.(type) {
		case runtime.Dict:
			if len(inferred) == 0 {
				row.ForEachEntry(func(_ string, entry runtime.DictEntry) bool {
					if key, ok := entry.Key.(runtime.String); ok {
						inferred = append(inferred, key.Value)
					}
					return true
				})
				sort.Strings(inferred)
			}
			values := make([]string, len(inferred))
			for i, header := range inferred {
				if value, ok := dictField(row, header); ok {
					values[i] = value.Inspect()
				}
			}
			out = append(out, values)
		case *runtime.List:
			values := make([]string, len(row.Elements))
			for i, value := range row.Elements {
				values[i] = value.Inspect()
			}
			out = append(out, values)
		default:
			return nil, nil, fmt.Errorf("row must be dict or list, got %s", row.TypeName())
		}
	}
	return out, inferred, nil
}

func renderTable(headers []string, rows [][]string) string {
	return renderTableWithSeparator(headers, rows, "  ")
}

func renderTableWithSeparator(headers []string, rows [][]string, separator string) string {
	widths := []int{}
	if len(headers) > 0 {
		widths = make([]int, len(headers))
		for i, header := range headers {
			widths[i] = displayWidth(header)
		}
	}
	for _, row := range rows {
		if len(row) > len(widths) {
			widths = append(widths, make([]int, len(row)-len(widths))...)
		}
		for i, value := range row {
			if w := displayWidth(value); w > widths[i] {
				widths[i] = w
			}
		}
	}
	var out strings.Builder
	if len(headers) > 0 {
		writeTableRow(&out, headers, widths, separator)
		dashes := make([]string, len(widths))
		for i, width := range widths {
			dashes[i] = strings.Repeat("-", width)
		}
		writeTableRow(&out, dashes, widths, separator)
	}
	for _, row := range rows {
		writeTableRow(&out, row, widths, separator)
	}
	return strings.TrimRight(out.String(), "\n")
}

// displayWidth is the terminal column count of text, ignoring ANSI escapes.
func displayWidth(text string) int {
	cols := 0
	for _, r := range stripANSI(text) {
		switch {
		case unicode.Is(unicode.Mn, r), unicode.Is(unicode.Me, r), unicode.Is(unicode.Cf, r):
		case isWideRune(r):
			cols += 2
		default:
			cols++
		}
	}
	return cols
}

func isWideRune(r rune) bool {
	kind := width.LookupRune(r).Kind()
	return kind == width.EastAsianWide || kind == width.EastAsianFullwidth
}

func writeTableRow(out *strings.Builder, row []string, widths []int, separator string) {
	for i, width := range widths {
		if i > 0 {
			out.WriteString(separator)
		}
		value := ""
		if i < len(row) {
			value = row[i]
		}
		out.WriteString(value)
		if pad := width - displayWidth(value); pad > 0 {
			out.WriteString(strings.Repeat(" ", pad))
		}
	}
	out.WriteByte('\n')
}
