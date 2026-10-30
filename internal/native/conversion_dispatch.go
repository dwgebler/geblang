package native

import (
	"fmt"

	"geblang/internal/runtime"
)

// conversionKeys are the native serialize/deserialize functions that need the calling backend's ConversionContext (instance __serialize or class deserialization).
var conversionKeys = map[string]bool{
	"json.stringify": true,
	"json.write":     true,
	"yaml.stringify": true,
	"toml.stringify": true,
	"json.parseAs":   true,
	"yaml.parseAs":   true,
	"toml.parseAs":   true,
	"xml.parseAs":    true,
}

func isConversionKey(key string) bool { return conversionKeys[key] }

// callConversion runs a conversion native with the calling backend's context, threaded from the per-backend registry (never a process-global).
func callConversion(ctx ConversionContext, key string, args []runtime.Value) (runtime.Value, error) {
	switch key {
	case "json.write":
		return jsonWriteCtx(ctx, args)
	case "json.stringify":
		return jsonStringifyCtx(ctx, args)
	case "yaml.stringify":
		return yamlStringifyCtx(ctx, args)
	case "toml.stringify":
		return tomlStringifyCtx(ctx, args)
	case "json.parseAs":
		return jsonParseAsCtx(ctx, args)
	case "yaml.parseAs":
		return yamlParseAsCtx(ctx, args)
	case "toml.parseAs":
		return tomlParseAsCtx(ctx, args)
	case "xml.parseAs":
		return xmlParseAsCtx(ctx, args)
	}
	return nil, fmt.Errorf("unsupported native call %s", key)
}

func deserializeIntoClassCtx(ctx ConversionContext, class runtime.Value, value runtime.Value) (runtime.Value, error) {
	if ctx.ClassDeserializer == nil {
		return nil, fmt.Errorf("class deserialization is unavailable: no active interpreter")
	}
	return ctx.ClassDeserializer(class, value)
}
