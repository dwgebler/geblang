package native

var statefulBytecodeBuiltinModules = map[string]bool{
	"io": true, "sys": true, "secrets": true, "process": true, "procnative": true, "sshnative": true,
	"http": true, "websocket": true, "smtp": true, "web": true, "db": true, "ext": true, "ffinative": true, "net": true, "test": true, "log": true, "watch": true,
	"csv": true, "schema": true, "serde": true, "metrics": true, "trace": true, "profile": true, "path": true, "async": true, "dotenv": true, "cli": true,
	"amqp": true, "kafka": true, "dataframe": true, "onnx": true, "browser": true, "archive_native": true,
}

// IsStatefulBytecodeBuiltinModule reports whether the VM calls the module's functions through the stateful native bridge.
func IsStatefulBytecodeBuiltinModule(name string) bool {
	return statefulBytecodeBuiltinModules[name]
}

// IsStatefulBytecodeBuiltin reports whether module.name is a stateful native the VM calls directly.
func IsStatefulBytecodeBuiltin(module, name string) bool {
	if statefulBytecodeBuiltinModules[module] {
		return true
	}
	switch module {
	case "json", "xml", "yaml":
		return name == "reader" || name == "stream"
	}
	return false
}

// ArityCheckedAtCompile reports whether a call to module.name compiles to a direct native call, whose argument count is checked against the catalog before the program runs.
func ArityCheckedAtCompile(module, name string) bool {
	if module == "profiler" {
		return false
	}
	return IsPureBuiltin(module, name) || IsStatefulBytecodeBuiltin(module, name)
}
