package evaluator

import (
	"fmt"
	"geblang/internal/ast"
	"geblang/internal/desugar"
	"geblang/internal/embedfold"
	"geblang/internal/ffi"
	"geblang/internal/lexer"
	"geblang/internal/modules"
	"geblang/internal/parser"
	"geblang/internal/runtime"
	"geblang/internal/semantic"
	"os"
	"path/filepath"
	"strings"

	yamllib "gopkg.in/yaml.v3"
)

type packageManifest struct {
	Path         string
	Root         string
	Name         string
	Version      string
	Source       string
	Paths        []string
	Dependencies map[string]packageDependency
	Extensions   map[string]*extConfig
	Permissions  permissionsBlock
}

type packageDependency struct {
	Path string `yaml:"path"`
}

func (d *packageDependency) UnmarshalYAML(value *yamllib.Node) error {
	switch value.Kind {
	case yamllib.ScalarNode:
		d.Path = value.Value
		return nil
	case yamllib.MappingNode:
		type dependency packageDependency
		var parsed dependency
		if err := value.Decode(&parsed); err != nil {
			return err
		}
		*d = packageDependency(parsed)
		return nil
	default:
		return fmt.Errorf("dependency must be a path string or mapping")
	}
}

type packageManifestFile struct {
	Name         string                       `yaml:"name"`
	Version      string                       `yaml:"version"`
	Source       string                       `yaml:"source"`
	Paths        []string                     `yaml:"paths"`
	ModulePaths  []string                     `yaml:"modulePaths"`
	Dependencies map[string]packageDependency `yaml:"dependencies"`
	Extensions   map[string]*extConfig        `yaml:"extensions"`
	Permissions  permissionsBlock             `yaml:"permissions"`
	Package      struct {
		Name    string `yaml:"name"`
		Version string `yaml:"version"`
	} `yaml:"package"`
}

type permissionsBlock struct {
	FFI *ffi.PolicyConfig `yaml:"ffi" json:"ffi"`
}

func (e *Evaluator) evalImportStatement(stmt *ast.ImportStatement, env *runtime.Environment) (signal, error) {
	alias := stmt.ModuleName()
	if alias == "" {
		return signal{}, fmt.Errorf("empty import path")
	}
	canonical := strings.Join(stmt.Path, ".")
	if imported, ok := e.importNames[alias]; ok && imported == canonical {
		if _, exists := env.Get(alias); exists {
			return signal{}, nil
		}
	}
	module, err := e.resolveImportedModule(canonical, alias)
	if err != nil {
		return signal{}, err
	}
	if err := env.Define(alias, module, true); err != nil {
		if err := env.Assign(alias, module); err != nil {
			return signal{}, err
		}
	}
	e.imports[alias] = true
	e.importNames[alias] = canonical
	return signal{}, nil
}

// Stdlib wins externally; self-import falls through to native.
func (e *Evaluator) resolveImportedModule(canonical, alias string) (*runtime.Module, error) {
	_, nativeExists := e.builtins[canonical]
	if path, perr := e.resolveModulePath(canonical); perr == nil && !e.loading[path] {
		return e.loadUserModule(canonical, alias)
	}
	if nativeExists {
		return e.builtinModuleValue(canonical, alias), nil
	}
	return e.loadUserModule(canonical, alias)
}

func (e *Evaluator) evalFromImportStatement(stmt *ast.FromImportStatement, env *runtime.Environment) (signal, error) {
	canonical := strings.Join(stmt.Path, ".")
	if canonical == "" {
		return signal{}, fmt.Errorf("empty import path")
	}
	preferUser := false
	if path, perr := e.resolveModulePath(canonical); perr == nil && !e.loading[path] {
		preferUser = true
	}
	if !preferUser {
		if _, ok := e.builtins[canonical]; ok {
			moduleClasses := e.builtinModuleValue(canonical, "").Exports
			functions := e.builtins[canonical]
			for _, item := range stmt.Names {
				if item.Name == nil {
					continue
				}
				name := item.Name.Value
				local := item.Local()
				value, ok := e.resolveBuiltinExport(moduleClasses, functions, canonical, name)
				if !ok {
					return signal{}, fmt.Errorf("from %s import %s: %s is not exported", canonical, name, name)
				}
				if err := env.DefineImported(local, value, canonical+"."+name); err != nil {
					return signal{}, err
				}
			}
			e.imports[canonical] = true
			return signal{}, nil
		}
	}
	module, err := e.loadUserModule(canonical, "")
	if err != nil {
		return signal{}, err
	}
	for _, item := range stmt.Names {
		if item.Name == nil {
			continue
		}
		name := item.Name.Value
		local := item.Local()
		value, ok := module.Exports[name]
		if !ok {
			if _, hasNative := e.builtins[canonical]; hasNative {
				if v, found := e.resolveBuiltinExport(e.builtinModuleValue(canonical, "").Exports, e.builtins[canonical], canonical, name); found {
					if err := env.DefineImported(local, v, canonical+"."+name); err != nil {
						return signal{}, err
					}
					continue
				}
			}
			return signal{}, fmt.Errorf("from %s import %s: %s is not exported", canonical, name, name)
		}
		if err := env.DefineImported(local, value, canonical+"."+name); err != nil {
			return signal{}, err
		}
	}
	return signal{}, nil
}

// resolveBuiltinExport finds a named symbol on a native module: a
// class registered in builtinModuleValue's Exports, or a registry
// function wrapped as a callable runtime.Function value.
func (e *Evaluator) resolveBuiltinExport(classes map[string]runtime.Value, functions map[string]builtinFunc, canonical, name string) (runtime.Value, bool) {
	if value, ok := classes[name]; ok {
		return value, true
	}
	if fn, ok := functions[name]; ok {
		return e.wrapBuiltinAsFunction(canonical, name, fn), true
	}
	return nil, false
}

func (e *Evaluator) wrapBuiltinAsFunction(canonical, name string, fn builtinFunc) runtime.Function {
	syntheticCall := &ast.CallExpression{
		Callee: &ast.SelectorExpression{
			Object: &ast.Identifier{Value: canonical},
			Name:   &ast.Identifier{Value: name},
		},
	}
	return runtime.Function{
		Name: canonical + "." + name,
		Native: func(_ *runtime.Instance, args []runtime.Value) (runtime.Value, error) {
			return fn(syntheticCall, args)
		},
	}
}

// BuiltinModule lets the bytecode loader fall back to native on self-import.
func (e *Evaluator) BuiltinModule(canonical, alias string) *runtime.Module {
	if _, ok := e.builtins[canonical]; !ok {
		return nil
	}
	// Builtin object classes are installed lazily; ensure they exist before
	// reading the class-export fields so module Exports are populated.
	if e.httpRequestClass == nil || e.httpResponseClass == nil {
		if err := e.installBuiltinTypes(runtime.NewEnvironment()); err != nil {
			return nil
		}
	}
	return e.builtinModuleValue(canonical, alias)
}

func (e *Evaluator) builtinModuleValue(canonical, alias string) *runtime.Module {
	exports := map[string]runtime.Value{}
	switch canonical {
	case "http":
		if e.httpRequestClass != nil {
			exports["Request"] = e.httpRequestClass
		}
		if e.httpResponseClass != nil {
			exports["Response"] = e.httpResponseClass
		}
		if e.httpClientClass != nil {
			exports["Client"] = e.httpClientClass
		}
		if e.httpBuilderClass != nil {
			exports["Builder"] = e.httpBuilderClass
		}
		if e.httpCookieJarClass != nil {
			exports["CookieJar"] = e.httpCookieJarClass
		}
		if e.httpFetchStreamClass != nil {
			exports["FetchStream"] = e.httpFetchStreamClass
		}
	case "process":
		if e.processClass != nil {
			exports["Process"] = e.processClass
		}
		if e.processResultClass != nil {
			exports["Result"] = e.processResultClass
		}
	case "db":
		if e.dbConnectionClass != nil {
			exports["Connection"] = e.dbConnectionClass
		}
		if e.dbTransactionClass != nil {
			exports["Transaction"] = e.dbTransactionClass
		}
		if e.dbStatementClass != nil {
			exports["Statement"] = e.dbStatementClass
		}
		if e.dbRowsClass != nil {
			exports["Rows"] = e.dbRowsClass
		}
	case "test":
		if e.testClass != nil {
			exports["Test"] = e.testClass
		}
	case "json":
		e.addStreamInterfaceExport(exports, "JsonStreamInterface")
	case "xml":
		e.addStreamInterfaceExport(exports, "XmlStreamInterface")
	case "yaml":
		e.addStreamInterfaceExport(exports, "YamlStreamInterface")
	case "csv":
		e.addStreamInterfaceExport(exports, "CsvStreamInterface")
	case "log":
		e.addStreamInterfaceExport(exports, "LogInterface")
	}
	return &runtime.Module{Name: alias, Canonical: canonical, Exports: exports}
}

func (e *Evaluator) addStreamInterfaceExport(exports map[string]runtime.Value, name string) {
	if e.streamIfaces == nil {
		return
	}
	if iface, ok := e.streamIfaces[strings.ToLower(name)]; ok {
		exports[name] = iface
	}
}

func (e *Evaluator) loadUserModule(canonical, alias string) (*runtime.Module, error) {
	if module, ok := e.modules[canonical]; ok {
		return module, nil
	}
	path, err := e.resolveModulePath(canonical)
	if err != nil {
		return nil, err
	}
	if e.loading[path] {
		return nil, fmt.Errorf("circular import detected for %s", canonical)
	}
	e.loading[path] = true
	defer delete(e.loading, path)

	program, err := e.parseAnalyzedModule(canonical, path)
	if err != nil {
		return nil, err
	}

	moduleEnv := runtime.NewEnvironment()
	previousPaths := e.modulePaths
	moduleDir := filepath.Dir(path)
	e.modulePaths = append([]string{moduleDir}, e.modulePaths...)
	// Declarations record their declaring module so reflect.location reports it.
	prevModule := e.currentModule
	e.currentModule = canonical
	sig, err := e.evalTopLevelStatements(program.Statements, moduleEnv)
	e.currentModule = prevModule
	e.modulePaths = previousPaths
	if err != nil {
		return nil, fmt.Errorf("evaluate module %s: %w", canonical, err)
	}
	if sig.kind != "" || sig.exited {
		return nil, fmt.Errorf("module %s cannot return, throw, break, continue, or exit during import", canonical)
	}
	exports, err := exportedValues(program, moduleEnv)
	if err != nil {
		return nil, fmt.Errorf("export module %s: %w", canonical, err)
	}
	module := &runtime.Module{Name: alias, Canonical: canonical, Exports: exports}
	e.modules[canonical] = module
	return module, nil
}

func (e *Evaluator) parseAnalyzedModule(canonical string, path string) (*ast.Program, error) {
	if program, ok := e.modulePrograms[path]; ok {
		return program, nil
	}
	source, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read module %s: %w", canonical, err)
	}
	p := parser.New(lexer.New(string(source)))
	program := p.ParseProgram()
	if len(p.Errors()) > 0 {
		return nil, fmt.Errorf("parse module %s: %s", canonical, strings.Join(p.Errors(), "\n"))
	}
	if _, diags := embedfold.Fold(program, path, embedfold.Inline); len(diags) > 0 {
		d := diags[0]
		return nil, fmt.Errorf("module %s: %s:%d:%d: %s", canonical, path, d.Line, d.Column, d.Message)
	}
	if diagnostics := semantic.New().Analyze(program); len(diagnostics) > 0 {
		errorMessages := make([]string, 0, len(diagnostics))
		for _, diagnostic := range diagnostics {
			if diagnostic.Severity == semantic.SeverityWarning {
				fmt.Fprintf(e.stderr, "warning: module %s: %s\n", canonical, diagnostic.Message)
				continue
			}
			errorMessages = append(errorMessages, diagnostic.Message)
		}
		if len(errorMessages) > 0 {
			return nil, fmt.Errorf("analyze module %s: %s", canonical, strings.Join(errorMessages, "\n"))
		}
	}
	if err := desugar.Dataclasses(program); err != nil {
		return nil, fmt.Errorf("desugar module %s: %w", canonical, err)
	}
	if err := desugar.Memoize(program); err != nil {
		return nil, fmt.Errorf("desugar module %s: %w", canonical, err)
	}
	e.modulePrograms[path] = program
	return program, nil
}

func (e *Evaluator) resolveModulePath(canonical string) (string, error) {
	resolver := modules.NewResolver(e.modulePaths)
	return resolver.Resolve(canonical)
}

type packageModuleRoot struct {
	path     string
	manifest *packageManifest
}

func (e *Evaluator) moduleSearchPaths() []string {
	paths := append([]string(nil), e.modulePaths...)
	if env := os.Getenv("GEBLANG_PATH"); env != "" {
		paths = append(paths, filepath.SplitList(env)...)
	}
	return paths
}

func (e *Evaluator) packageModuleRoots() ([]packageModuleRoot, error) {
	roots := []packageModuleRoot{}
	seenRoots := map[string]bool{}
	seenManifests := map[string]bool{}
	for _, base := range e.moduleSearchPaths() {
		if base == "" {
			base = "."
		}
		manifest, err := e.findPackageManifest(base)
		if err != nil {
			return nil, err
		}
		if manifest == nil {
			continue
		}
		if err := e.collectPackageModuleRoots(manifest, seenManifests, seenRoots, &roots); err != nil {
			return nil, err
		}
	}
	return roots, nil
}

func (e *Evaluator) collectPackageModuleRoots(manifest *packageManifest, seenManifests map[string]bool, seenRoots map[string]bool, roots *[]packageModuleRoot) error {
	if manifest == nil {
		return nil
	}
	if seenManifests[manifest.Path] {
		return nil
	}
	seenManifests[manifest.Path] = true
	for _, moduleRoot := range manifest.moduleRoots() {
		if seenRoots[moduleRoot] {
			continue
		}
		seenRoots[moduleRoot] = true
		*roots = append(*roots, packageModuleRoot{path: moduleRoot, manifest: manifest})
	}
	for name, dependency := range manifest.Dependencies {
		if dependency.Path == "" {
			return fmt.Errorf("package %s dependency %s has no path", manifestName(manifest), name)
		}
		dependencyRoot := filepath.Clean(filepath.Join(manifest.Root, dependency.Path))
		dependencyManifest, err := e.findPackageManifest(dependencyRoot)
		if err != nil {
			return err
		}
		if dependencyManifest == nil {
			dependencyManifest = &packageManifest{
				Path:         filepath.Clean(filepath.Join(dependencyRoot, "geblang.yaml")),
				Root:         dependencyRoot,
				Name:         name,
				Dependencies: map[string]packageDependency{},
			}
		}
		if err := e.collectPackageModuleRoots(dependencyManifest, seenManifests, seenRoots, roots); err != nil {
			return err
		}
	}
	return nil
}

func (e *Evaluator) findPackageManifest(start string) (*packageManifest, error) {
	current, err := filepath.Abs(start)
	if err != nil {
		current = filepath.Clean(start)
	}
	if info, err := os.Stat(current); err == nil && !info.IsDir() {
		current = filepath.Dir(current)
	}
	for {
		for _, name := range []string{"geblang.yaml", "geblang.yml", "geblang.json"} {
			path := filepath.Join(current, name)
			if info, err := os.Stat(path); err == nil && !info.IsDir() {
				return e.loadPackageManifest(path)
			}
		}
		parent := filepath.Dir(current)
		if parent == current {
			return nil, nil
		}
		current = parent
	}
}

func (e *Evaluator) loadPackageManifest(path string) (*packageManifest, error) {
	path = filepath.Clean(path)
	if manifest, ok := e.manifests[path]; ok {
		return manifest, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var parsed packageManifestFile
	if err := yamllib.Unmarshal(data, &parsed); err != nil {
		return nil, fmt.Errorf("parse package manifest %s: %w", path, err)
	}
	name := parsed.Name
	if name == "" {
		name = parsed.Package.Name
	}
	version := parsed.Version
	if version == "" {
		version = parsed.Package.Version
	}
	paths := append([]string(nil), parsed.Paths...)
	paths = append(paths, parsed.ModulePaths...)
	manifest := &packageManifest{
		Path:         path,
		Root:         filepath.Dir(path),
		Name:         name,
		Version:      version,
		Source:       parsed.Source,
		Paths:        paths,
		Dependencies: parsed.Dependencies,
		Extensions:   parsed.Extensions,
		Permissions:  parsed.Permissions,
	}
	if manifest.Dependencies == nil {
		manifest.Dependencies = map[string]packageDependency{}
	}
	if manifest.Extensions == nil {
		manifest.Extensions = map[string]*extConfig{}
	}
	e.manifests[path] = manifest
	return manifest, nil
}

func manifestName(manifest *packageManifest) string {
	if manifest.Name != "" {
		return manifest.Name
	}
	return manifest.Root
}

func (m *packageManifest) moduleRoots() []string {
	roots := []string{}
	if m.Source != "" {
		roots = append(roots, filepath.Clean(filepath.Join(m.Root, m.Source)))
	} else {
		roots = append(roots, m.Root)
	}
	for _, path := range m.Paths {
		if path == "" {
			continue
		}
		roots = append(roots, filepath.Clean(filepath.Join(m.Root, path)))
	}
	return roots
}

func packageRelativeModuleBases(canonical, packageName string) []string {
	bases := []string{filepath.Join(strings.Split(canonical, ".")...)}
	if packageName == "" {
		return bases
	}
	if canonical == packageName {
		return append(bases, "init")
	}
	prefix := packageName + "."
	if strings.HasPrefix(canonical, prefix) {
		stripped := strings.TrimPrefix(canonical, prefix)
		bases = append(bases, filepath.Join(strings.Split(stripped, ".")...))
	}
	return bases
}

func exportedValues(program *ast.Program, env *runtime.Environment) (map[string]runtime.Value, error) {
	exports := map[string]runtime.Value{}
	for _, stmt := range program.Statements {
		exportStmt, ok := stmt.(*ast.ExportStatement)
		if !ok {
			continue
		}
		name := exportedStatementName(exportStmt.Statement)
		if name == "" {
			return nil, fmt.Errorf("unsupported export %T", exportStmt.Statement)
		}
		value, ok := env.Get(name)
		if !ok {
			return nil, fmt.Errorf("export %q was not declared", name)
		}
		exports[name] = value
	}
	return exports, nil
}

func exportedStatementName(stmt ast.Statement) string {
	switch stmt := stmt.(type) {
	case *ast.DeclarationStatement:
		return stmt.Name.Value
	case *ast.FunctionStatement:
		return stmt.Name.Value
	case *ast.ClassStatement:
		return stmt.Name.Value
	case *ast.InterfaceStatement:
		return stmt.Name.Value
	case *ast.EnumStatement:
		return stmt.Name.Value
	default:
		return ""
	}
}
