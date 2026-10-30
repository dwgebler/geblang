package native

import (
	"encoding/json"
	"fmt"
	"geblang/internal/runtime"
)

func registerJSON(r *Registry) {
	r.Register("json", "write", func(args []runtime.Value) (runtime.Value, error) { return jsonWriteCtx(ConversionContext{}, args) })
	r.Register("json", "parse", func(args []runtime.Value) (runtime.Value, error) {
		text, err := singleString(args, "json.parse")
		if err != nil {
			return nil, err
		}
		value, parseErr := ParseJSONText(text)
		if parseErr != nil {
			return nil, fmt.Errorf("%s", parseErr.Message)
		}
		return value, nil
	})
	r.Register("json", "stringify", func(args []runtime.Value) (runtime.Value, error) {
		return jsonStringifyCtx(ConversionContext{}, args)
	})
	r.Register("json", "validate", func(args []runtime.Value) (runtime.Value, error) {
		text, err := singleString(args, "json.validate")
		if err != nil {
			return nil, err
		}
		return runtime.Bool{Value: json.Valid([]byte(text)) && jsonDepthWithinLimit(text)}, nil
	})
	r.Register("json", "tryParse", func(args []runtime.Value) (runtime.Value, error) {
		text, err := singleString(args, "json.tryParse")
		if err != nil {
			return nil, err
		}
		value, parseErr := ParseJSONText(text)
		if parseErr != nil {
			return ParseResult(false, runtime.Null{}, parseErr), nil
		}
		return ParseResult(true, value, nil), nil
	})
	r.Register("json", "validateDetailed", func(args []runtime.Value) (runtime.Value, error) {
		text, err := singleString(args, "json.validateDetailed")
		if err != nil {
			return nil, err
		}
		_, parseErr := ParseJSONText(text)
		if parseErr != nil {
			return ValidationResult(false, parseErr), nil
		}
		return ValidationResult(true, nil), nil
	})
	r.Register("json", "parseAs", func(args []runtime.Value) (runtime.Value, error) {
		return jsonParseAsCtx(ConversionContext{}, args)
	})
}

func jsonStringifyCtx(ctx ConversionContext, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("json.stringify expects exactly one argument")
	}
	out, err := EncodeJSONValueCtx(ctx, args[0])
	if err != nil {
		return nil, err
	}
	return runtime.String{Value: out}, nil
}

func jsonParseAsCtx(ctx ConversionContext, args []runtime.Value) (runtime.Value, error) {
	text, class, err := parseAsArgs(args, "json.parseAs")
	if err != nil {
		return nil, err
	}
	value, parseErr := ParseJSONText(text)
	if parseErr != nil {
		return nil, fmt.Errorf("%s", parseErr.Message)
	}
	return deserializeIntoClassCtx(ctx, class, value)
}
