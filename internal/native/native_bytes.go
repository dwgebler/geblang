package native

import (
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"geblang/internal/runtime"
	"unicode/utf8"
)

func registerBytes(r *Registry) {
	r.Register("bytes", "fromString", func(args []runtime.Value) (runtime.Value, error) {
		text, err := stringWithOptionalUTF8Encoding(args, "bytes.fromString")
		if err != nil {
			return nil, err
		}
		return runtime.Bytes{Value: []byte(text)}, nil
	})
	r.Register("bytes", "fromList", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 1 {
			return nil, fmt.Errorf("bytes.fromList expects 1 argument")
		}
		list, ok := args[0].(*runtime.List)
		if !ok {
			return nil, fmt.Errorf("bytes.fromList expects a list of int byte values")
		}
		out := make([]byte, len(list.Elements))
		for i, elem := range list.Elements {
			n, err := byteValueInt(elem, fmt.Sprintf("bytes.fromList element %d", i))
			if err != nil {
				return nil, err
			}
			out[i] = byte(n)
		}
		return runtime.Bytes{Value: out}, nil
	})
	r.Register("bytes", "toString", func(args []runtime.Value) (runtime.Value, error) {
		data, err := bytesWithOptionalUTF8Encoding(args, "bytes.toString")
		if err != nil {
			return nil, err
		}
		return BytesToUTF8String(data, "bytes.toString")
	})
	r.Register("bytes", "rawString", func(args []runtime.Value) (runtime.Value, error) {
		data, err := singleBytes(args, "bytes.rawString")
		if err != nil {
			return nil, err
		}
		return runtime.String{Value: string(data)}, nil
	})
	r.Register("bytes", "fromHex", func(args []runtime.Value) (runtime.Value, error) {
		text, err := singleString(args, "bytes.fromHex")
		if err != nil {
			return nil, err
		}
		data, err := hex.DecodeString(text)
		if err != nil {
			return nil, err
		}
		return runtime.Bytes{Value: data}, nil
	})
	r.Register("bytes", "toHex", func(args []runtime.Value) (runtime.Value, error) {
		data, err := singleBytes(args, "bytes.toHex")
		if err != nil {
			return nil, err
		}
		return runtime.String{Value: hex.EncodeToString(data)}, nil
	})
	r.Register("bytes", "fromBase64", func(args []runtime.Value) (runtime.Value, error) {
		text, err := singleString(args, "bytes.fromBase64")
		if err != nil {
			return nil, err
		}
		data, err := base64.StdEncoding.DecodeString(text)
		if err != nil {
			return nil, err
		}
		return runtime.Bytes{Value: data}, nil
	})
	r.Register("bytes", "toBase64", func(args []runtime.Value) (runtime.Value, error) {
		data, err := singleBytes(args, "bytes.toBase64")
		if err != nil {
			return nil, err
		}
		return runtime.String{Value: base64.StdEncoding.EncodeToString(data)}, nil
	})
	r.Register("bytes", "fromBase64Url", func(args []runtime.Value) (runtime.Value, error) {
		text, err := singleString(args, "bytes.fromBase64Url")
		if err != nil {
			return nil, err
		}
		data, err := decodeBase64Url(text)
		if err != nil {
			return nil, fmt.Errorf("bytes.fromBase64Url: %v", err)
		}
		return runtime.Bytes{Value: data}, nil
	})
	r.Register("bytes", "toBase64Url", func(args []runtime.Value) (runtime.Value, error) {
		data, err := singleBytes(args, "bytes.toBase64Url")
		if err != nil {
			return nil, err
		}
		return runtime.String{Value: base64.RawURLEncoding.EncodeToString(data)}, nil
	})
	r.Register("bytes", "concat", func(args []runtime.Value) (runtime.Value, error) {
		out := []byte{}
		for _, arg := range args {
			value, ok := arg.(runtime.Bytes)
			if !ok {
				return nil, fmt.Errorf("bytes.concat arguments must be bytes")
			}
			out = append(out, value.Value...)
		}
		return runtime.Bytes{Value: out}, nil
	})
}

func BytesToUTF8String(data []byte, label string) (runtime.String, error) {
	if !utf8.Valid(data) {
		return runtime.String{}, runtime.ClassifiedError{Class: "ValueError", Message: fmt.Sprintf("%s data is not valid UTF-8", label)}
	}
	return runtime.String{Value: string(data)}, nil
}
