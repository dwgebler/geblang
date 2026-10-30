package native

import (
	"fmt"
	"strings"

	"geblang/internal/runtime"
)

// stringBuilderCell holds the builder behind a pointer: the handle is GC-reclaimed when unreferenced, and dispose nils b so use-after-dispose is caught.
type stringBuilderCell struct{ b *strings.Builder }

func sbLookup(arg runtime.Value, label string) (*strings.Builder, error) {
	obj, ok := arg.(runtime.NativeObject)
	if !ok || obj.Kind != "StringBuilder" {
		return nil, fmt.Errorf("%s expects a StringBuilder handle", label)
	}
	cell, ok := obj.Payload.(*stringBuilderCell)
	if !ok || cell.b == nil {
		return nil, fmt.Errorf("%s: unknown StringBuilder handle", label)
	}
	return cell.b, nil
}

func registerStringBuilder(r *Registry) {
	r.Register("strbuilder", "new", func(args []runtime.Value) (runtime.Value, error) {
		var initial string
		switch len(args) {
		case 0:
		case 1:
			s, ok := args[0].(runtime.String)
			if !ok {
				return nil, fmt.Errorf("strbuilder.new initial value must be string")
			}
			initial = s.Value
		default:
			return nil, fmt.Errorf("strbuilder.new expects 0 or 1 arguments")
		}
		cell := &stringBuilderCell{b: &strings.Builder{}}
		if initial != "" {
			cell.b.WriteString(initial)
		}
		return runtime.NativeObject{Kind: "StringBuilder", Payload: cell}, nil
	})
	r.Register("strbuilder", "append", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 2 {
			return nil, fmt.Errorf("strbuilder.append expects 2 arguments")
		}
		b, err := sbLookup(args[0], "strbuilder.append")
		if err != nil {
			return nil, err
		}
		s, ok := args[1].(runtime.String)
		if !ok {
			return nil, fmt.Errorf("strbuilder.append value must be string")
		}
		b.WriteString(s.Value)
		return args[0], nil
	})
	r.Register("strbuilder", "appendLine", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 2 {
			return nil, fmt.Errorf("strbuilder.appendLine expects 2 arguments")
		}
		b, err := sbLookup(args[0], "strbuilder.appendLine")
		if err != nil {
			return nil, err
		}
		s, ok := args[1].(runtime.String)
		if !ok {
			return nil, fmt.Errorf("strbuilder.appendLine value must be string")
		}
		b.WriteString(s.Value)
		b.WriteByte('\n')
		return args[0], nil
	})
	r.Register("strbuilder", "build", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 1 {
			return nil, fmt.Errorf("strbuilder.build expects 1 argument")
		}
		b, err := sbLookup(args[0], "strbuilder.build")
		if err != nil {
			return nil, err
		}
		return runtime.String{Value: b.String()}, nil
	})
	r.Register("strbuilder", "length", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 1 {
			return nil, fmt.Errorf("strbuilder.length expects 1 argument")
		}
		b, err := sbLookup(args[0], "strbuilder.length")
		if err != nil {
			return nil, err
		}
		return runtime.NewInt64(int64(b.Len())), nil
	})
	r.Register("strbuilder", "clear", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 1 {
			return nil, fmt.Errorf("strbuilder.clear expects 1 argument")
		}
		b, err := sbLookup(args[0], "strbuilder.clear")
		if err != nil {
			return nil, err
		}
		b.Reset()
		return args[0], nil
	})
	r.Register("strbuilder", "dispose", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 1 {
			return nil, fmt.Errorf("strbuilder.dispose expects 1 argument")
		}
		obj, ok := args[0].(runtime.NativeObject)
		if !ok || obj.Kind != "StringBuilder" {
			return nil, fmt.Errorf("strbuilder.dispose expects a StringBuilder handle")
		}
		// Nil the builder so use-after-dispose is caught; the cell is GC-reclaimed once unreferenced.
		if cell, ok := obj.Payload.(*stringBuilderCell); ok {
			cell.b = nil
		}
		return runtime.Null{}, nil
	})
}

// StringByteLength counts stored UTF-8 bytes without constructing a bytes value.
func StringByteLength(value runtime.String, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 0 {
		return nil, fmt.Errorf("string.byteLength expects no arguments")
	}
	return runtime.SmallInt{Value: int64(len(value.Value))}, nil
}
