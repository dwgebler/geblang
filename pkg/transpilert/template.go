package transpilert

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	htmltemplate "html/template"
	"math/big"
	"os"
	pathlib "path/filepath"
	"reflect"
	"strings"
)

// The template surface mirrors internal/native/native_template.go: both sides
// parse and execute with html/template, so matching the data marshaling makes
// output (incl auto-escaping) byte-identical by construction. Template data
// follows the interpreter's ValueToTemplateData: dicts -> map[string]any, lists
// -> []any, scalars via the json fallback (Decimal -> json.Number).

// TemplateValue is the handle for template.Template / template.load.
type TemplateValue struct {
	Name string
	Text string
	Path string
}

// TemplateEngine is the handle for template.Engine.
type TemplateEngine struct{ Dir string }

// TemplateRenderString backs template.renderString(text, data).
func TemplateRenderString(text string, data any) string {
	return renderTemplateText("inline", text, data)
}

// NewTemplate backs template.Template(text, [name]).
func NewTemplate(args ...any) TemplateValue {
	if len(args) < 1 || len(args) > 2 {
		panic(NewError("RuntimeError", "template.Template expects text and optional name"))
	}
	text, ok := args[0].(string)
	if !ok {
		panic(NewError("RuntimeError", "template.Template text must be string"))
	}
	name := "inline"
	if len(args) == 2 {
		n, ok := args[1].(string)
		if !ok {
			panic(NewError("RuntimeError", "template.Template name must be string"))
		}
		name = n
	}
	return TemplateValue{Name: name, Text: text}
}

// TemplateLoad backs template.load(path).
func TemplateLoad(path string) TemplateValue {
	text, err := os.ReadFile(path)
	if err != nil {
		panic(NewError("RuntimeError", "template.load: "+err.Error()))
	}
	return TemplateValue{Name: pathlib.Base(path), Text: string(text), Path: path}
}

// NewTemplateEngine backs template.Engine([dir|dict]).
func NewTemplateEngine(args ...any) TemplateEngine {
	dir := "templates"
	if len(args) > 1 {
		panic(NewError("RuntimeError", "template.Engine expects optional directory or options dict"))
	}
	if len(args) == 1 {
		switch v := args[0].(type) {
		case string:
			dir = v
		case *OrderedDict[string, any]:
			if s := orderedDictString(v, "dir"); s != "" {
				dir = s
			} else if s := orderedDictString(v, "templates"); s != "" {
				dir = s
			}
		case *OrderedDict[string, string]:
			if s, ok := v.Get("dir"); ok && s != "" {
				dir = s
			} else if s, ok := v.Get("templates"); ok && s != "" {
				dir = s
			}
		default:
			panic(NewError("RuntimeError", "template.Engine expects string or dict"))
		}
	}
	return TemplateEngine{Dir: dir}
}

func (t TemplateValue) Name_() string { return t.Name }

func (t TemplateValue) PathOrNull() any {
	if t.Path == "" {
		return nil
	}
	return t.Path
}

func (t TemplateValue) Render(data any) string { return renderTemplateText(t.Name, t.Text, data) }

func (t TemplateValue) ToString() string { return t.Text }

func (e TemplateEngine) Dir_() string { return e.Dir }

func (e TemplateEngine) Load(path string) TemplateValue {
	full := templatePath(e.Dir, path)
	text, err := os.ReadFile(full)
	if err != nil {
		panic(NewError("RuntimeError", "template.Engine.load: "+err.Error()))
	}
	return TemplateValue{Name: path, Text: string(text), Path: full}
}

func (e TemplateEngine) Render(name string, data any) string {
	full := templatePath(e.Dir, name)
	text, err := os.ReadFile(full)
	if err != nil {
		panic(NewError("RuntimeError", "template.Engine.render: "+err.Error()))
	}
	return renderTemplateText(name, string(text), data)
}

func renderTemplateText(name, text string, data any) string {
	tmpl, err := htmltemplate.New(name).Parse(text)
	if err != nil {
		panic(NewError("RuntimeError", "template render: "+err.Error()))
	}
	var out bytes.Buffer
	if err := tmpl.Execute(&out, templateData(data)); err != nil {
		panic(NewError("RuntimeError", "template render: "+err.Error()))
	}
	return out.String()
}

// templateData mirrors ValueToTemplateData: dict -> map[string]any, list ->
// []any, every scalar via the json fallback so html/template prints it exactly
// as the interpreter does.
func templateData(v any) any {
	if m, ok := orderedDictToMap(v); ok {
		return m
	}
	switch x := v.(type) {
	case []any:
		items := make([]any, 0, len(x))
		for _, item := range x {
			items = append(items, templateData(item))
		}
		return items
	case []string:
		items := make([]any, 0, len(x))
		for _, item := range x {
			items = append(items, item)
		}
		return items
	default:
		return templateScalar(v)
	}
}

// orderedDictToMap converts any string-keyed *OrderedDict[string, V] (the dict
// literal lowers to a homogeneous value type) to map[string]any, recursing.
func orderedDictToMap(v any) (map[string]any, bool) {
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Pointer || rv.IsNil() {
		return nil, false
	}
	if !strings.HasPrefix(rv.Elem().Type().Name(), "OrderedDict[string,") {
		return nil, false
	}
	keysM := rv.MethodByName("Keys")
	valsM := rv.MethodByName("Values")
	if !keysM.IsValid() || !valsM.IsValid() {
		return nil, false
	}
	keys := keysM.Call(nil)[0]
	vals := valsM.Call(nil)[0]
	m := make(map[string]any, keys.Len())
	for i := 0; i < keys.Len(); i++ {
		m[keys.Index(i).Interface().(string)] = templateData(vals.Index(i).Interface())
	}
	return m, true
}

// templateScalar mirrors ValueToJSON's scalar handling (the interpreter's
// template fallback): Int -> int64/string on overflow, Decimal -> json.Number,
// bytes -> base64 string, others pass through.
func templateScalar(v any) any {
	switch x := v.(type) {
	case Int:
		if x.Big != nil {
			if x.Big.IsInt64() {
				return x.Big.Int64()
			}
			return x.Big.String()
		}
		return x.I64
	case *big.Int:
		if x.IsInt64() {
			return x.Int64()
		}
		return x.String()
	case *big.Rat:
		return json.Number(DecimalToString(x))
	case []byte:
		return base64.StdEncoding.EncodeToString(x)
	default:
		return v
	}
}

func templatePath(dir, name string) string {
	if name == "" {
		panic(NewError("RuntimeError", "template name is required"))
	}
	if pathlib.IsAbs(name) {
		panic(NewError("RuntimeError", "template name must be relative"))
	}
	clean := pathlib.Clean(name)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(os.PathSeparator)) {
		panic(NewError("RuntimeError", "template name escapes template directory"))
	}
	return pathlib.Join(dir, clean)
}

func orderedDictString(d *OrderedDict[string, any], key string) string {
	if v, ok := d.Get(key); ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}
