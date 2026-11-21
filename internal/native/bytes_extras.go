package native

import (
	"bytes"
	"fmt"
	"unicode/utf8"

	"geblang/internal/runtime"
)

func bytesExtraMethod(b []byte, name string, args []runtime.Value) (runtime.Value, bool, error) {
	switch name {
	case "isUtf8":
		if len(args) != 0 {
			return nil, true, fmt.Errorf("bytes.isUtf8 expects no arguments")
		}
		return runtime.Bool{Value: utf8.Valid(b)}, true, nil
	case "startsWith", "endsWith", "indexOf":
		if len(args) != 1 {
			return nil, true, fmt.Errorf("bytes.%s expects one argument", name)
		}
		needle, valid, err := byteNeedle(name, args[0])
		if err != nil {
			return nil, true, err
		}
		switch name {
		case "startsWith":
			return runtime.Bool{Value: valid && bytes.HasPrefix(b, needle)}, true, nil
		case "endsWith":
			return runtime.Bool{Value: valid && bytes.HasSuffix(b, needle)}, true, nil
		}
		if !valid {
			return runtime.SmallInt{Value: -1}, true, nil
		}
		return runtime.SmallInt{Value: int64(bytes.Index(b, needle))}, true, nil
	}
	return nil, false, nil
}

// byteNeedle accepts bytes or an int byte value; an int outside 0-255 can never match.
func byteNeedle(name string, arg runtime.Value) ([]byte, bool, error) {
	if needle, ok := arg.(runtime.Bytes); ok {
		return needle.Value, true, nil
	}
	n, ok := AsInt64(arg)
	if !ok {
		if IsInt(arg) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("bytes.%s expects bytes or int byte", name)
	}
	if n < 0 || n > 255 {
		return nil, false, nil
	}
	return []byte{byte(n)}, true, nil
}
