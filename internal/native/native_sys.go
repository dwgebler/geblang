package native

import (
	"fmt"
	"geblang/internal/runtime"
	"strings"
)

func registerSys(r *Registry) {
	r.Register("sys", "hostname", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 0 {
			return nil, fmt.Errorf("sys.hostname expects no arguments")
		}
		name, err := sysHostname()
		if err != nil {
			return nil, fmt.Errorf("sys.hostname: %v", err)
		}
		return runtime.String{Value: name}, nil
	})
	r.Register("sys", "pid", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 0 {
			return nil, fmt.Errorf("sys.pid expects no arguments")
		}
		return runtime.NewInt64(int64(sysPid())), nil
	})
	r.Register("sys", "goroutineId", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 0 {
			return nil, fmt.Errorf("sys.goroutineId expects no arguments")
		}
		return runtime.NewInt64(sysGoroutineID()), nil
	})
	r.Register("sys", "platform", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 0 {
			return nil, fmt.Errorf("sys.platform expects no arguments")
		}
		return runtime.String{Value: sysPlatform()}, nil
	})
	r.Register("sys", "arch", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 0 {
			return nil, fmt.Errorf("sys.arch expects no arguments")
		}
		return runtime.String{Value: sysArch()}, nil
	})
	r.Register("sys", "cpuCount", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 0 {
			return nil, fmt.Errorf("sys.cpuCount expects no arguments")
		}
		return runtime.NewInt64(int64(sysCPUCount())), nil
	})
	r.Register("sys", "tmpdir", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 0 {
			return nil, fmt.Errorf("sys.tmpdir expects no arguments")
		}
		return runtime.String{Value: sysTmpDir()}, nil
	})
	r.Register("sys", "homedir", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 0 {
			return nil, fmt.Errorf("sys.homedir expects no arguments")
		}
		dir, err := sysHomeDir()
		if err != nil {
			return nil, fmt.Errorf("sys.homedir: %v", err)
		}
		return runtime.String{Value: dir}, nil
	})
	r.Register("sys", "username", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 0 {
			return nil, fmt.Errorf("sys.username expects no arguments")
		}
		name, err := sysUsername()
		if err != nil {
			return nil, fmt.Errorf("sys.username: %v", err)
		}
		return runtime.String{Value: name}, nil
	})
	r.Register("sys", "environ", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 0 {
			return nil, fmt.Errorf("sys.environ expects no arguments")
		}
		entries := map[string]runtime.DictEntry{}
		for _, kv := range sysEnviron() {
			eq := strings.IndexByte(kv, '=')
			var k, v string
			if eq < 0 {
				k = kv
			} else {
				k = kv[:eq]
				v = kv[eq+1:]
			}
			keyValue := runtime.String{Value: k}
			entries[DictKey(keyValue)] = runtime.DictEntry{Key: keyValue, Value: runtime.String{Value: v}}
		}
		return runtime.Dict{Entries: entries}, nil
	})
}

func registerArgs(r *Registry) {
	r.Register("args", "parse", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 2 {
			return nil, fmt.Errorf("args.parse expects argv list and schema dict")
		}
		argv, ok := args[0].(*runtime.List)
		if !ok {
			return nil, fmt.Errorf("args.parse first argument must be a list")
		}
		schema, ok := args[1].(runtime.Dict)
		if !ok {
			return nil, fmt.Errorf("args.parse second argument must be a dict")
		}
		return ParseArgv(argv, schema), nil
	})
	r.Register("args", "help", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 2 {
			return nil, fmt.Errorf("args.help expects program name and schema dict")
		}
		name, ok := args[0].(runtime.String)
		if !ok {
			return nil, fmt.Errorf("args.help first argument must be a string")
		}
		schema, ok := args[1].(runtime.Dict)
		if !ok {
			return nil, fmt.Errorf("args.help second argument must be a dict")
		}
		return runtime.String{Value: HelpText(name.Value, schema)}, nil
	})
}
