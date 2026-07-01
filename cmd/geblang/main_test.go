package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"geblang/internal/evaluator"
	"geblang/internal/sourcedoc"
	geblangver "geblang/internal/version"
)

func TestCleanBytecodeCacheRemovesCacheRoot(t *testing.T) {
	dir := t.TempDir()
	// macOS temp dirs are under /var -> /private/var; canonicalize to match os.Getwd().
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		dir = resolved
	}
	previous, err := os.Getwd()
	if err != nil {
		t.Fatalf("get cwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir temp: %v", err)
	}
	defer func() {
		if err := os.Chdir(previous); err != nil {
			t.Fatalf("restore cwd: %v", err)
		}
	}()

	cacheFile := filepath.Join(bytecodeCacheDir(), "chunk.gbc")
	if err := os.MkdirAll(filepath.Dir(cacheFile), 0o755); err != nil {
		t.Fatalf("mkdir cache: %v", err)
	}
	if err := os.WriteFile(cacheFile, []byte("cached"), 0o644); err != nil {
		t.Fatalf("write cache: %v", err)
	}

	if err := cleanBytecodeCache(); err != nil {
		t.Fatalf("clean cache: %v", err)
	}
	if _, err := os.Stat(bytecodeCacheRoot()); !os.IsNotExist(err) {
		t.Fatalf("cache root should not exist after clean, err=%v", err)
	}
}

func TestBytecodeCacheStatsCountsFiles(t *testing.T) {
	dir := t.TempDir()
	// macOS temp dirs are under /var -> /private/var; canonicalize to match os.Getwd().
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		dir = resolved
	}
	previous, err := os.Getwd()
	if err != nil {
		t.Fatalf("get cwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir temp: %v", err)
	}
	defer func() {
		if err := os.Chdir(previous); err != nil {
			t.Fatalf("restore cwd: %v", err)
		}
	}()

	stats, err := bytecodeCacheStats()
	if err != nil {
		t.Fatalf("empty stats: %v", err)
	}
	if stats.Root != bytecodeCacheRoot() || stats.Files != 0 || stats.Bytes != 0 {
		t.Fatalf("empty stats: %#v", stats)
	}

	if err := os.MkdirAll(bytecodeCacheDir(), 0o755); err != nil {
		t.Fatalf("mkdir cache: %v", err)
	}
	if err := os.WriteFile(filepath.Join(bytecodeCacheDir(), "a.gbc"), []byte("abc"), 0o644); err != nil {
		t.Fatalf("write cache a: %v", err)
	}
	if err := os.WriteFile(filepath.Join(bytecodeCacheDir(), "b.gbc"), []byte("de"), 0o644); err != nil {
		t.Fatalf("write cache b: %v", err)
	}

	stats, err = bytecodeCacheStats()
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if stats.Files != 2 || stats.Bytes != 5 {
		t.Fatalf("stats: %#v", stats)
	}
}

func TestParseCacheStatsArgs(t *testing.T) {
	config, err := parseCacheStatsArgs([]string{"--json"})
	if err != nil {
		t.Fatalf("parse cache stats args: %v", err)
	}
	if !config.JSON {
		t.Fatalf("config: %#v", config)
	}
	if _, err := parseCacheStatsArgs([]string{"--missing"}); err == nil {
		t.Fatal("expected unknown option error")
	}
}

func TestWriteCacheStatsJSON(t *testing.T) {
	var out bytes.Buffer
	err := writeCacheStatsJSON(&out, cacheStats{Root: ".geblang-cache", Files: 2, Bytes: 5})
	if err != nil {
		t.Fatalf("write cache stats json: %v", err)
	}
	output := out.String()
	for _, want := range []string{`"root": ".geblang-cache"`, `"files": 2`, `"bytes": 5`} {
		if !strings.Contains(output, want) {
			t.Fatalf("cache stats json missing %q: %q", want, output)
		}
	}
}

func TestVersionIsCurrentRelease(t *testing.T) {
	if version != geblangver.Geblang {
		t.Fatalf("version: got %q, want %q", version, geblangver.Geblang)
	}
}

func TestPrintHelpShowsTopLevelCommands(t *testing.T) {
	var out bytes.Buffer

	if !printHelp(&out, "") {
		t.Fatal("top-level help should be known")
	}
	output := out.String()
	for _, want := range []string{
		"Usage: geblang <command>",
		"geblang repl",
		"geblang -m <module>",
		"geblang test",
		"geblang check",
		"geblang init",
		"geblang doctor",
		"geblang doc",
		"geblang cache stats",
		"geblang --allow-ffi <path-or-glob>",
		"geblang help [topic]",
		"Use `geblang help <topic>` or `geblang <command> --help`",
		"Topics: repl, run, module, build, install, fmt, lsp, dap, test, check, init, doctor, doc, cache, completion",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("help output missing %q: %q", want, output)
		}
	}
}

func TestPrintHelpShowsTopicDetails(t *testing.T) {
	tests := map[string]string{
		"repl":   "usage: geblang repl",
		"run":    "VM execution is attempted by default",
		"module": "invokes its exported main",
		"test":   "geblang test --tag integration tests/",
		"check":  "--json writes a structured report",
		"init":   "minimal geblang.yaml",
		"doctor": "--json writes a structured report",
		"doc":    "Generates source API documentation",
		"cache":  "--json writes structured cache stats",
	}
	for topic, want := range tests {
		var out bytes.Buffer
		if !printHelp(&out, topic) {
			t.Fatalf("topic %q should be known", topic)
		}
		if !strings.Contains(out.String(), want) {
			t.Fatalf("topic %q output missing %q: %q", topic, want, out.String())
		}
	}
}

func TestPrintRunAndModuleHelpShowAllowFFI(t *testing.T) {
	for _, topic := range []string{"run", "module"} {
		var out bytes.Buffer
		if !printHelp(&out, topic) {
			t.Fatalf("topic %q should be known", topic)
		}
		if !strings.Contains(out.String(), "--allow-ffi <path-or-glob>") {
			t.Fatalf("topic %q output missing --allow-ffi: %q", topic, out.String())
		}
	}
}

func TestIsHelpArg(t *testing.T) {
	for _, arg := range []string{"--help", "-help", "-h", "help"} {
		if !isHelpArg(arg) {
			t.Fatalf("%q should be a help arg", arg)
		}
	}
	if isHelpArg("--json") {
		t.Fatal("--json should not be a help arg")
	}
}

func TestPrintHelpRejectsUnknownTopic(t *testing.T) {
	if printHelp(&bytes.Buffer{}, "missing") {
		t.Fatal("unknown help topic should be rejected")
	}
}

func TestParseDocArgs(t *testing.T) {
	config, err := parseDocArgs([]string{"--format", "json", "--out", "api.md", "src"})
	if err != nil {
		t.Fatalf("parse doc args: %v", err)
	}
	if config.format != "json" || config.out != "api.md" || config.path != "src" {
		t.Fatalf("config: %#v", config)
	}
	config, err = parseDocArgs([]string{"--json", "src"})
	if err != nil {
		t.Fatalf("parse doc json shortcut: %v", err)
	}
	if config.format != "json" {
		t.Fatalf("config: %#v", config)
	}
	if _, err := parseDocArgs([]string{"--missing", "src"}); err == nil {
		t.Fatal("expected unknown option error")
	}
	if _, err := parseDocArgs([]string{}); err == nil {
		t.Fatal("expected missing path error")
	}
}

func TestCollectDocReportUsesExportsAndDocblocks(t *testing.T) {
	dir := t.TempDir()
	source := `module app.routes;

## Lists users for the API.
export @route("GET", "/users")
async func listUsers(int page = 1): list<string> {
    return ["ada"];
}

## Internal helper should be hidden when exports exist.
func hidden(): string {
    return "hidden";
}

/** Controller for user pages. */
export @controller("/users")
class UserController<T> extends BaseController implements Handler {
    let string title;

    ## Shows a user by id.
    func show(int id): Response {
        return Response.text("ok");
    }

    static func make(): string {
        return "ok";
    }
}

export interface Named extends Jsonable {
    ## Returns the public name.
    func name(): string;
}
`
	path := filepath.Join(dir, "routes.gb")
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}

	report, err := sourcedoc.Collect(dir)
	if err != nil {
		t.Fatalf("collect doc report: %v", err)
	}
	if len(report.Files) != 1 {
		t.Fatalf("files: %#v", report.Files)
	}
	file := report.Files[0]
	if file.Module != "app.routes" {
		t.Fatalf("module: %q", file.Module)
	}
	if len(file.Symbols) != 3 {
		t.Fatalf("symbols: %#v", file.Symbols)
	}
	if file.Symbols[0].Name != "listUsers" || !file.Symbols[0].Async {
		t.Fatalf("function symbol: %#v", file.Symbols[0])
	}
	if !strings.Contains(file.Symbols[0].Doc, "Lists users") {
		t.Fatalf("function doc: %#v", file.Symbols[0])
	}
	if got := strings.Join(file.Symbols[0].Decorators, ","); got != `route("GET", "/users")` {
		t.Fatalf("decorators: %q", got)
	}
	class := file.Symbols[1]
	if class.Name != "UserController" || class.Extends != "BaseController" {
		t.Fatalf("class: %#v", class)
	}
	if len(class.Fields) != 1 || class.Fields[0].Signature != "let string title" {
		t.Fatalf("fields: %#v", class.Fields)
	}
	if len(class.Methods) != 2 || class.Methods[0].Doc != "Shows a user by id." || !class.Methods[1].Static {
		t.Fatalf("methods: %#v", class.Methods)
	}
	if file.Symbols[2].Methods[0].Doc != "Returns the public name." {
		t.Fatalf("interface method docs: %#v", file.Symbols[2].Methods)
	}
}

func TestWriteDocMarkdownAndJSON(t *testing.T) {
	report := sourcedoc.Report{Files: []sourcedoc.File{{
		Path:   "src/routes.gb",
		Module: "app.routes",
		Symbols: []sourcedoc.Item{{
			Kind:       "function",
			Name:       "index",
			Signature:  "func index(): Response",
			Doc:        "Handles the home page.",
			Decorators: []string{`route("GET", "/")`},
			Exported:   true,
		}},
	}}}

	var markdown bytes.Buffer
	sourcedoc.WriteMarkdown(&markdown, report)
	for _, want := range []string{
		"# API Documentation",
		"Module: `app.routes`",
		"### Function `index`",
		"func index(): Response",
		"Decorators: `route(\"GET\", \"/\")`",
		"Handles the home page.",
	} {
		if !strings.Contains(markdown.String(), want) {
			t.Fatalf("markdown missing %q: %q", want, markdown.String())
		}
	}

	var encoded bytes.Buffer
	if err := sourcedoc.WriteJSON(&encoded, report); err != nil {
		t.Fatalf("write json: %v", err)
	}
	var decoded sourcedoc.Report
	if err := json.Unmarshal(encoded.Bytes(), &decoded); err != nil {
		t.Fatalf("decode json: %v", err)
	}
	if decoded.Files[0].Symbols[0].Name != "index" {
		t.Fatalf("decoded: %#v", decoded)
	}
}

func TestParseInitArgs(t *testing.T) {
	config, err := parseInitArgs([]string{"--name", "acme.tools", "--source", "lib", "--force"})
	if err != nil {
		t.Fatalf("parse init args: %v", err)
	}
	if config.name != "acme.tools" || config.source != "lib" || !config.force {
		t.Fatalf("config: %#v", config)
	}
	if _, err := parseInitArgs([]string{"--missing"}); err == nil {
		t.Fatal("expected unknown option error")
	}
}

func TestInitPackageWritesManifestAndSourceDir(t *testing.T) {
	dir := t.TempDir()
	// macOS temp dirs are under /var -> /private/var; canonicalize to match os.Getwd().
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		dir = resolved
	}
	previous, err := os.Getwd()
	if err != nil {
		t.Fatalf("get cwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir temp: %v", err)
	}
	defer func() {
		if err := os.Chdir(previous); err != nil {
			t.Fatalf("restore cwd: %v", err)
		}
	}()

	path, err := initPackage(initConfig{name: "acme.tools", source: "lib"})
	if err != nil {
		t.Fatalf("init package: %v", err)
	}
	if path != filepath.Join(dir, "geblang.yaml") {
		t.Fatalf("manifest path: got %q", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	want := "name: acme.tools\nversion: 0.1.0\nsource: lib\npaths: []\ndependencies: {}\n"
	if string(data) != want {
		t.Fatalf("manifest:\ngot  %q\nwant %q", string(data), want)
	}
	if info, err := os.Stat(filepath.Join(dir, "lib")); err != nil || !info.IsDir() {
		t.Fatalf("source dir: info=%v err=%v", info, err)
	}
	if _, err := initPackage(initConfig{name: "acme.tools", source: "lib"}); err == nil {
		t.Fatal("expected existing manifest error")
	}
}

func TestPackageNameFromDir(t *testing.T) {
	if got := packageNameFromDir("/tmp/Acme Tools_App"); got != "acme.tools.app" {
		t.Fatalf("package name: got %q", got)
	}
}

func TestCollectAndWriteDoctorReportShowsManifestCacheAndGo(t *testing.T) {
	dir := t.TempDir()
	// macOS temp dirs are under /var -> /private/var; canonicalize to match os.Getwd().
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		dir = resolved
	}
	previous, err := os.Getwd()
	if err != nil {
		t.Fatalf("get cwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir temp: %v", err)
	}
	defer func() {
		if err := os.Chdir(previous); err != nil {
			t.Fatalf("restore cwd: %v", err)
		}
	}()
	if err := os.WriteFile(filepath.Join(dir, "geblang.yaml"), []byte("name: acme.tools\nversion: 0.1.0\nsource: src\n"), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	if err := os.MkdirAll(bytecodeCacheDir(), 0o755); err != nil {
		t.Fatalf("mkdir cache: %v", err)
	}
	if err := os.WriteFile(filepath.Join(bytecodeCacheDir(), "chunk.gbc"), []byte("abc"), 0o644); err != nil {
		t.Fatalf("write cache: %v", err)
	}

	report, err := collectDoctorReport(func(name string) (string, error) {
		if name != "go" {
			t.Fatalf("unexpected lookup %q", name)
		}
		return "/usr/bin/go", nil
	})
	if err != nil {
		t.Fatalf("doctor: %v", err)
	}
	var out bytes.Buffer
	writeDoctorReport(&out, report)
	output := out.String()
	for _, want := range []string{
		"geblang: " + geblangver.Geblang,
		"working directory: " + dir,
		"go: /usr/bin/go",
		"manifest: " + filepath.Join(dir, "geblang.yaml"),
		"package: acme.tools 0.1.0",
		"source: src",
		"cache: .geblang-cache files=1 bytes=3",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("doctor output missing %q: %q", want, output)
		}
	}
}

func TestParseDoctorArgs(t *testing.T) {
	config, err := parseDoctorArgs([]string{"--json"})
	if err != nil {
		t.Fatalf("parse doctor args: %v", err)
	}
	if !config.JSON {
		t.Fatalf("config: %#v", config)
	}
	if _, err := parseDoctorArgs([]string{"--missing"}); err == nil {
		t.Fatal("expected unknown option error")
	}
}

func TestWriteDoctorJSON(t *testing.T) {
	var out bytes.Buffer
	err := writeDoctorJSON(&out, doctorReport{
		GeblangVersion:   "0.1.0",
		WorkingDirectory: "/work",
		GoFound:          true,
		GoPath:           "/usr/bin/go",
		Manifest:         &doctorManifest{Path: "/work/geblang.yaml", Name: "acme.tools", Version: "0.1.0", Source: "src"},
		Cache:            doctorCacheSnapshot{Root: ".geblang-cache", Files: 1, Bytes: 3},
	})
	if err != nil {
		t.Fatalf("write doctor json: %v", err)
	}
	output := out.String()
	for _, want := range []string{
		`"geblangVersion": "0.1.0"`,
		`"workingDirectory": "/work"`,
		`"goPath": "/usr/bin/go"`,
		`"manifest": {`,
		`"name": "acme.tools"`,
		`"files": 1`,
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("doctor json missing %q: %q", want, output)
		}
	}
}

func TestParseCheckArgs(t *testing.T) {
	config, err := parseCheckArgs([]string{"--json", "--strict", "examples"})
	if err != nil {
		t.Fatalf("parse check args: %v", err)
	}
	if config.Path != "examples" || !config.JSON || !config.Lint || !config.Strict {
		t.Fatalf("config: %#v", config)
	}
	config, err = parseCheckArgs([]string{"--no-lint", "examples"})
	if err != nil {
		t.Fatalf("parse check args no lint: %v", err)
	}
	if config.Lint {
		t.Fatalf("lint should be disabled: %#v", config)
	}
	if _, err := parseCheckArgs([]string{"one.gb", "two.gb"}); err == nil {
		t.Fatal("expected path count error")
	}
}

func TestWriteCheckJSON(t *testing.T) {
	var out bytes.Buffer
	err := writeCheckJSON(&out, checkResult{
		Checked: 1,
		Diagnostics: []checkDiagnostic{
			{File: "bad.gb", Severity: "error", Rule: "parse", Message: "bad syntax"},
		},
	})
	if err != nil {
		t.Fatalf("write json: %v", err)
	}
	output := out.String()
	for _, want := range []string{`"checked": 1`, `"file": "bad.gb"`, `"message": "bad syntax"`} {
		if !strings.Contains(output, want) {
			t.Fatalf("json output missing %q: %q", want, output)
		}
	}
}

func TestRunREPLKeepsSessionState(t *testing.T) {
	input := strings.NewReader("let x = 41;\nx + 1;\n:vars\n:quit\n")
	var out bytes.Buffer
	var errOut bytes.Buffer

	code := runREPL(input, &out, &errOut, replConfig{})
	if code != 0 {
		t.Fatalf("exit code: got %d, stderr=%q", code, errOut.String())
	}
	output := out.String()
	if !strings.Contains(output, "42\n") {
		t.Fatalf("output missing expression result: %q", output)
	}
	if !strings.Contains(output, "geb> x\n") {
		t.Fatalf("output missing variable listing: %q", output)
	}
	if errOut.Len() != 0 {
		t.Fatalf("stderr: %q", errOut.String())
	}
}

func TestRunREPLPrintsFinalExpressionAfterStatementsAndAllowsRepeatedImports(t *testing.T) {
	input := strings.NewReader("import re; re.match(\"[0-9]*\", \"123x\");\nimport re; re.match(\"[0-9]*\", \"123x\");\n:quit\n")
	var out bytes.Buffer
	var errOut bytes.Buffer

	code := runREPL(input, &out, &errOut, replConfig{})
	if code != 0 {
		t.Fatalf("exit code: got %d, stderr=%q", code, errOut.String())
	}
	output := out.String()
	expected := "{\"groups\": [\"123\"], \"named\": {}, \"text\": \"123\"}\n"
	if got := strings.Count(output, expected); got != 2 {
		t.Fatalf("output match count: got %d, want 2 in %q", got, output)
	}
	if errOut.Len() != 0 {
		t.Fatalf("stderr: %q", errOut.String())
	}
}

func TestRunREPLAcceptsTripleQuotedMultilineString(t *testing.T) {
	input := strings.NewReader("let string banner = \"\"\"\nline \"one\"\nline two\n\"\"\";\nbanner;\n:quit\n")
	var out bytes.Buffer
	var errOut bytes.Buffer

	code := runREPL(input, &out, &errOut, replConfig{})
	if code != 0 {
		t.Fatalf("exit code: got %d, stderr=%q", code, errOut.String())
	}
	output := out.String()
	if !strings.Contains(output, "line \"one\"\nline two\n") {
		t.Fatalf("output missing multiline string result: %q", output)
	}
	if errOut.Len() != 0 {
		t.Fatalf("stderr: %q", errOut.String())
	}
}

func TestRunREPLFormatsTypeValue(t *testing.T) {
	input := strings.NewReader("import reflect;\nreflect.typeOf(42);\n:quit\n")
	var out bytes.Buffer
	var errOut bytes.Buffer

	code := runREPL(input, &out, &errOut, replConfig{})
	if code != 0 {
		t.Fatalf("exit code: got %d, stderr=%q", code, errOut.String())
	}
	output := out.String()
	if !strings.Contains(output, "Type<int>") {
		t.Fatalf("expected Type<int> in output, got: %q", output)
	}
	if errOut.Len() != 0 {
		t.Fatalf("stderr: %q", errOut.String())
	}
}

func TestRunREPLPrettyPrint(t *testing.T) {
	script := strings.Join([]string{
		// set
		`let s = {1, 2, 3};`,
		`s;`,
		// enum variant with payload
		`enum Status { Ok(string), Err(string) }`,
		`Status.Ok("done");`,
		// datetime Instant (epoch zero)
		`import datetime;`,
		`datetime.Instant(0);`,
		// datetime Duration (90 seconds = 1m30s)
		`datetime.Duration(90);`,
		// list of strings exceeds 80 chars, should trigger multi-line output
		`let big = ["alpha", "bravo", "charlie", "delta", "echo", "foxtrot", "golf", "hotel", "india"];`,
		`big;`,
		`:quit`,
	}, "\n")
	input := strings.NewReader(script)
	var out bytes.Buffer
	var errOut bytes.Buffer

	code := runREPL(input, &out, &errOut, replConfig{})
	if code != 0 {
		t.Fatalf("exit code: got %d, stderr=%q stdout=%q", code, errOut.String(), out.String())
	}
	output := out.String()
	checks := []string{
		"set{",
		`Status.Ok("done")`,
		"Instant(1970-01-01T00:00:00Z)",
		"Duration(1m30s)",
		// multi-line list should contain a newline between bracket and first item
		"[\n",
	}
	for _, want := range checks {
		if !strings.Contains(output, want) {
			t.Errorf("expected %q in REPL output\nfull output: %s", want, output)
		}
	}
	if errOut.Len() != 0 {
		t.Fatalf("stderr: %q", errOut.String())
	}
}

func TestCheckGeblangPathChecksDirectoryWithoutExecuting(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "good.gb"), []byte(`let int x = 1;
`), 0o644); err != nil {
		t.Fatalf("write good file: %v", err)
	}
	if err := os.Mkdir(filepath.Join(dir, ".geblang-cache"), 0o755); err != nil {
		t.Fatalf("mkdir cache: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".geblang-cache", "bad.gb"), []byte(`let int x = ;`), 0o644); err != nil {
		t.Fatalf("write cache file: %v", err)
	}

	result, err := checkGeblangPath(checkConfig{Path: dir, Lint: true})
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if result.Checked != 1 {
		t.Fatalf("checked: got %d, want 1", result.Checked)
	}
	if len(result.Diagnostics) != 0 {
		t.Fatalf("diagnostics: %v", result.Diagnostics)
	}
}

func TestCheckGeblangPathReportsDiagnostics(t *testing.T) {
	dir := t.TempDir()
	bad := filepath.Join(dir, "bad.gb")
	if err := os.WriteFile(bad, []byte(`let int x = "bad";`), 0o644); err != nil {
		t.Fatalf("write bad file: %v", err)
	}

	result, err := checkGeblangPath(checkConfig{Path: bad, Lint: true})
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if result.Checked != 1 {
		t.Fatalf("checked: got %d, want 1", result.Checked)
	}
	if len(result.Diagnostics) != 1 || result.Diagnostics[0].File != bad || result.Diagnostics[0].Message == "" {
		t.Fatalf("diagnostics: %v", result.Diagnostics)
	}
}

// TestCheckGeblangPathFlagsUnknownModuleMember verifies the end-to-end
// cross-module check: an unknown native member is an error, a real one
// is not. This exercises the engine-derived native symbol source.
func TestCheckGeblangPathFlagsUnknownModuleMember(t *testing.T) {
	dir := t.TempDir()
	bad := filepath.Join(dir, "bad.gb")
	if err := os.WriteFile(bad, []byte("import io;\nio.foobar();\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	result, err := checkGeblangPath(checkConfig{Path: bad, Lint: true})
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	found := false
	for _, d := range result.Diagnostics {
		if d.Rule == "import" && strings.Contains(d.Message, "io has no exported member foobar") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected unknown-member diagnostic, got %v", result.Diagnostics)
	}

	good := filepath.Join(dir, "good.gb")
	if err := os.WriteFile(good, []byte("import io;\nio.println(\"hi\");\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	goodResult, err := checkGeblangPath(checkConfig{Path: good, Lint: true})
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if len(goodResult.Diagnostics) != 0 {
		t.Fatalf("valid member should not be flagged: %v", goodResult.Diagnostics)
	}
}

// TestCheckGeblangPathFlagsUnknownClassMethod verifies the end-to-end
// cross-module unknown-method check: a method absent from a class's full
// hierarchy is flagged; a real method is not.
func TestCheckGeblangPathFlagsUnknownClassMethod(t *testing.T) {
	dir := t.TempDir()
	bad := filepath.Join(dir, "bad.gb")
	src := "class Box {\n    func open(): void {}\n}\nBox b = Box();\nb.open();\nb.smash();\n"
	if err := os.WriteFile(bad, []byte(src), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	result, err := checkGeblangPath(checkConfig{Path: bad, Lint: true})
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	found := false
	for _, d := range result.Diagnostics {
		if strings.Contains(d.Message, "Box has no method smash") {
			found = true
		}
		if strings.Contains(d.Message, "no method open") {
			t.Fatalf("real method flagged: %v", d)
		}
	}
	if !found {
		t.Fatalf("expected unknown-method diagnostic, got %v", result.Diagnostics)
	}
}

// TestCheckGeblangPathRejectsModuleTopLevelStatement verifies geblang
// check flags free-standing top-level statements in a module file
// (i.e. one that begins with `module name;`). The reverse - the same
// statement in a script file - should not trigger the diagnostic.
func TestCheckGeblangPathRejectsModuleTopLevelStatement(t *testing.T) {
	dir := t.TempDir()
	mod := filepath.Join(dir, "loud.gb")
	if err := os.WriteFile(mod, []byte(`module loud;
import io;
io.println("hello on import");
`), 0o644); err != nil {
		t.Fatalf("write module: %v", err)
	}
	result, err := checkGeblangPath(checkConfig{Path: mod, Lint: true})
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if len(result.Diagnostics) != 1 {
		t.Fatalf("diagnostics: got %d, want 1: %v", len(result.Diagnostics), result.Diagnostics)
	}
	if !strings.Contains(result.Diagnostics[0].Message, "free-standing top-level") {
		t.Fatalf("diagnostic message: got %q", result.Diagnostics[0].Message)
	}

	// Same code without `module ...;` is a script and should be fine.
	script := filepath.Join(dir, "ok.gb")
	if err := os.WriteFile(script, []byte(`import io;
io.println("hello at script start");
`), 0o644); err != nil {
		t.Fatalf("write script: %v", err)
	}
	scriptResult, err := checkGeblangPath(checkConfig{Path: script, Lint: true})
	if err != nil {
		t.Fatalf("check script: %v", err)
	}
	if len(scriptResult.Diagnostics) != 0 {
		t.Fatalf("script diagnostics: got %d, want 0: %v", len(scriptResult.Diagnostics), scriptResult.Diagnostics)
	}
}

func TestCheckGeblangPathReportsUnresolvedImports(t *testing.T) {
	dir := t.TempDir()
	bad := filepath.Join(dir, "bad_import.gb")
	if err := os.WriteFile(bad, []byte(`import does.not.exist as missing;
missing.value;
`), 0o644); err != nil {
		t.Fatalf("write bad import file: %v", err)
	}

	result, err := checkGeblangPath(checkConfig{Path: bad, Lint: false})
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if !result.HasErrors() {
		t.Fatalf("expected import error: %v", result.Diagnostics)
	}
	if len(result.Diagnostics) != 1 || result.Diagnostics[0].Rule != "import" {
		t.Fatalf("diagnostics: %v", result.Diagnostics)
	}
}

func TestCheckGeblangPathResolvesPackageImports(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src", "app")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatalf("mkdir src: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "geblang.yaml"), []byte("name: app\nsource: src\n"), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(src, "util.gb"), []byte(`module app.util;
export func answer(): int { return 42; }
`), 0o644); err != nil {
		t.Fatalf("write util: %v", err)
	}
	main := filepath.Join(root, "src", "main.gb")
	if err := os.WriteFile(main, []byte(`import app.util as util;
let int answer = util.answer();
`), 0o644); err != nil {
		t.Fatalf("write main: %v", err)
	}

	result, err := checkGeblangPath(checkConfig{Path: main, Lint: true})
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if len(result.Diagnostics) != 0 {
		t.Fatalf("diagnostics: %v", result.Diagnostics)
	}
}

func TestCheckGeblangPathValidatesModuleDeclarations(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src", "app")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatalf("mkdir src: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "geblang.yaml"), []byte("name: app\nsource: src\n"), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	good := filepath.Join(src, "good.gb")
	if err := os.WriteFile(good, []byte(`module app.good;
export func ok(): bool { return true; }
`), 0o644); err != nil {
		t.Fatalf("write good: %v", err)
	}

	result, err := checkGeblangPath(checkConfig{Path: root, Lint: true})
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if len(result.Diagnostics) != 0 {
		t.Fatalf("diagnostics: %v", result.Diagnostics)
	}
}

func TestCheckGeblangPathReportsModuleDeclarationMismatch(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src", "app")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatalf("mkdir src: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "geblang.yaml"), []byte("name: app\nsource: src\n"), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	bad := filepath.Join(src, "wrong.gb")
	if err := os.WriteFile(bad, []byte(`module app.other;
export func ok(): bool { return true; }
`), 0o644); err != nil {
		t.Fatalf("write bad: %v", err)
	}

	result, err := checkGeblangPath(checkConfig{Path: root, Lint: true})
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if !result.HasErrors() {
		t.Fatalf("expected module error: %v", result.Diagnostics)
	}
	if len(result.Diagnostics) != 1 || result.Diagnostics[0].Rule != "module" {
		t.Fatalf("diagnostics: %v", result.Diagnostics)
	}
}

func TestCheckGeblangPathReportsDuplicateModules(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src", "app")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatalf("mkdir src: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "geblang.yaml"), []byte("name: app\nsource: src\n"), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(src, "dup.gb"), []byte(`module app.dup;
export func one(): int { return 1; }
`), 0o644); err != nil {
		t.Fatalf("write dup: %v", err)
	}
	if err := os.WriteFile(filepath.Join(src, "also_dup.gb"), []byte(`module app.dup;
export func two(): int { return 2; }
`), 0o644); err != nil {
		t.Fatalf("write also dup: %v", err)
	}

	result, err := checkGeblangPath(checkConfig{Path: root, Lint: true})
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	var duplicateCount int
	for _, diagnostic := range result.Diagnostics {
		if diagnostic.Rule == "duplicate-module" {
			duplicateCount++
		}
	}
	if duplicateCount != 2 {
		t.Fatalf("expected two duplicate-module diagnostics: %v", result.Diagnostics)
	}
}

func TestCheckGeblangPathReportsLintWarnings(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "lint.gb")
	if err := os.WriteFile(source, []byte(`import math;

func done(): int {
    return 1;
    return 2;
}
`), 0o644); err != nil {
		t.Fatalf("write lint file: %v", err)
	}

	result, err := checkGeblangPath(checkConfig{Path: source, Lint: true})
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if result.HasErrors() {
		t.Fatalf("unexpected errors: %v", result.Diagnostics)
	}
	if !result.HasWarnings() {
		t.Fatalf("expected warnings: %v", result.Diagnostics)
	}
	var unused, unreachable bool
	for _, diagnostic := range result.Diagnostics {
		unused = unused || diagnostic.Rule == "unused-import"
		unreachable = unreachable || diagnostic.Rule == "unreachable"
	}
	if !unused || !unreachable {
		t.Fatalf("expected unused-import and unreachable warnings: %v", result.Diagnostics)
	}

	noLint, err := checkGeblangPath(checkConfig{Path: source, Lint: false})
	if err != nil {
		t.Fatalf("check no lint: %v", err)
	}
	if len(noLint.Diagnostics) != 0 {
		t.Fatalf("no-lint diagnostics: %v", noLint.Diagnostics)
	}
}

func TestRunREPLLoadsFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "snippet.gb")
	if err := os.WriteFile(path, []byte("let name = \"Ada\";\n"), 0o600); err != nil {
		t.Fatalf("write snippet: %v", err)
	}
	input := strings.NewReader(":load " + path + "\nname;\n:quit\n")
	var out bytes.Buffer
	var errOut bytes.Buffer

	code := runREPL(input, &out, &errOut, replConfig{moduleDir: dir})
	if code != 0 {
		t.Fatalf("exit code: got %d, stderr=%q", code, errOut.String())
	}
	if !strings.Contains(out.String(), "Ada\n") {
		t.Fatalf("output: %q", out.String())
	}
	if errOut.Len() != 0 {
		t.Fatalf("stderr: %q", errOut.String())
	}
}

func TestRunREPLRejectsVMStrictForNow(t *testing.T) {
	var out bytes.Buffer
	var errOut bytes.Buffer

	code := runREPL(strings.NewReader(":quit\n"), &out, &errOut, replConfig{mode: executionVMStrict})
	if code != 2 {
		t.Fatalf("exit code: got %d", code)
	}
	if !strings.Contains(errOut.String(), "--vm-strict is not supported") {
		t.Fatalf("stderr: %q", errOut.String())
	}
}

func TestRunREPLDirShowsImportedStdlibFunctions(t *testing.T) {
	input := strings.NewReader("import sys;\ndir(sys);\n:quit\n")
	var out bytes.Buffer
	var errOut bytes.Buffer

	code := runREPL(input, &out, &errOut, replConfig{})
	if code != 0 {
		t.Fatalf("exit code: got %d, stderr=%q", code, errOut.String())
	}
	output := out.String()
	for _, name := range []string{"args", "exit", "getenv"} {
		if !strings.Contains(output, name) {
			t.Fatalf("output missing %q: %q", name, output)
		}
	}
	if errOut.Len() != 0 {
		t.Fatalf("stderr: %q", errOut.String())
	}
}

func TestRunREPLListsStdlibModulesAndMembers(t *testing.T) {
	input := strings.NewReader(":stdlib\n:members cli\nimport cli;\n:modules\n:members cli\n:quit\n")
	var out bytes.Buffer
	var errOut bytes.Buffer

	code := runREPL(input, &out, &errOut, replConfig{})
	if code != 0 {
		t.Fatalf("exit code: got %d, stderr=%q", code, errOut.String())
	}
	output := out.String()
	for _, want := range []string{"cli\n", "parseArgs\n", "password\n", "imports:\n  cli\n", "extensions: none\n"} {
		if !strings.Contains(output, want) {
			t.Fatalf("output missing %q: %q", want, output)
		}
	}
	if errOut.Len() != 0 {
		t.Fatalf("stderr: %q", errOut.String())
	}
}

func TestRunREPLMarksRuntimeErrors(t *testing.T) {
	input := strings.NewReader("let int x = 5;\nx / 0;\n:quit\n")
	var out bytes.Buffer
	var errOut bytes.Buffer

	code := runREPL(input, &out, &errOut, replConfig{})
	if code != 0 {
		t.Fatalf("exit code: got %d, stderr=%q", code, errOut.String())
	}
	if !strings.Contains(errOut.String(), "uncaught RuntimeError: decimal division by zero\n  at <top level>") {
		t.Fatalf("stderr: %q", errOut.String())
	}
}

func TestRunREPLPrettyPrintsStructuredValues(t *testing.T) {
	input := strings.NewReader("[1, \"Ada\"];\n{\"name\": \"Ada\", \"age\": 42};\n:quit\n")
	var out bytes.Buffer
	var errOut bytes.Buffer

	code := runREPL(input, &out, &errOut, replConfig{})
	if code != 0 {
		t.Fatalf("exit code: got %d, stderr=%q", code, errOut.String())
	}
	output := out.String()
	if !strings.Contains(output, "[1, \"Ada\"]\n") {
		t.Fatalf("output missing pretty list: %q", output)
	}
	if !strings.Contains(output, "{\"age\": 42, \"name\": \"Ada\"}\n") {
		t.Fatalf("output missing pretty dict: %q", output)
	}
	if errOut.Len() != 0 {
		t.Fatalf("stderr: %q", errOut.String())
	}
}

func TestRunREPLHistoryCommandShowsSessionEntries(t *testing.T) {
	input := strings.NewReader("let x = 1;\nx;\n:history\n:quit\n")
	var out bytes.Buffer
	var errOut bytes.Buffer

	code := runREPL(input, &out, &errOut, replConfig{})
	if code != 0 {
		t.Fatalf("exit code: got %d, stderr=%q", code, errOut.String())
	}
	output := out.String()
	if !strings.Contains(output, "1  let x = 1;") || !strings.Contains(output, "2  x;") {
		t.Fatalf("history output: %q", output)
	}
	if errOut.Len() != 0 {
		t.Fatalf("stderr: %q", errOut.String())
	}
}

func TestRunREPLNullExpressionPrintsNullVoidCallDoesNot(t *testing.T) {
	// Explicit null expression must print "null"; void function call must not.
	input := strings.NewReader("import io;\nnull;\nio.println(\"hi\");\n:quit\n")
	var out bytes.Buffer
	var errOut bytes.Buffer

	code := runREPL(input, &out, &errOut, replConfig{})
	if code != 0 {
		t.Fatalf("exit code: got %d, stderr=%q", code, errOut.String())
	}
	output := out.String()
	if !strings.Contains(output, "null\n") {
		t.Fatalf("expected 'null' in output for explicit null expression; got: %q", output)
	}
	// "hi\n" comes from io.println; a second "null\n" should NOT appear for the void return.
	if strings.Count(output, "null\n") != 1 {
		t.Fatalf("expected exactly one 'null' in output (for explicit null, not void); got: %q", output)
	}
	if errOut.Len() != 0 {
		t.Fatalf("stderr: %q", errOut.String())
	}
}

func TestREPLHistoryStoreRoundTripsAndTrims(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history")
	store := &replHistoryStore{path: path}
	history := []string{"", "let x = 1;", "let x = 1;", "x;"}
	for i := 0; i < replHistoryLimit+5; i++ {
		history = append(history, "entry "+string(rune('a'+i%26)))
	}

	if err := store.Save(history); err != nil {
		t.Fatalf("save history: %v", err)
	}
	loaded, err := store.Load()
	if err != nil {
		t.Fatalf("load history: %v", err)
	}
	if len(loaded) != replHistoryLimit {
		t.Fatalf("history length: got %d, want %d", len(loaded), replHistoryLimit)
	}
	if loaded[0] == "" {
		t.Fatalf("history should not include blank entries: %v", loaded[:3])
	}
}

func TestTerminalLineReaderEditsWithArrowKeys(t *testing.T) {
	inRead, inWrite, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	defer inRead.Close()
	out, err := os.CreateTemp(t.TempDir(), "repl-out-*")
	if err != nil {
		t.Fatalf("temp output: %v", err)
	}
	defer out.Close()

	reader := &terminalLineReader{in: inRead, out: out}
	if _, err := inWrite.Write([]byte("ac\x1b[Db\n")); err != nil {
		t.Fatalf("write input: %v", err)
	}
	inWrite.Close()

	line, err := reader.ReadLine("geb> ")
	if err != nil {
		t.Fatalf("read line: %v", err)
	}
	if line != "abc" {
		t.Fatalf("line: got %q, want %q", line, "abc")
	}
}

func TestTerminalLineReaderDeleteRemovesCharacterUnderCursor(t *testing.T) {
	inRead, inWrite, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	defer inRead.Close()
	out, err := os.CreateTemp(t.TempDir(), "repl-out-*")
	if err != nil {
		t.Fatalf("temp output: %v", err)
	}
	defer out.Close()

	reader := &terminalLineReader{in: inRead, out: out}
	if _, err := inWrite.Write([]byte("abc\x1b[D\x1b[D\x1b[3~\n")); err != nil {
		t.Fatalf("write input: %v", err)
	}
	inWrite.Close()

	line, err := reader.ReadLine("geb> ")
	if err != nil {
		t.Fatalf("read line: %v", err)
	}
	if line != "ac" {
		t.Fatalf("line: got %q, want %q", line, "ac")
	}
}

func TestTerminalLineReaderDownArrowRestoresDraft(t *testing.T) {
	inRead, inWrite, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	defer inRead.Close()
	out, err := os.CreateTemp(t.TempDir(), "repl-out-*")
	if err != nil {
		t.Fatalf("temp output: %v", err)
	}
	defer out.Close()

	reader := &terminalLineReader{in: inRead, out: out, history: []string{"let x = 1;", "x;"}}
	if _, err := inWrite.Write([]byte("draft\x1b[A\x1b[B\n")); err != nil {
		t.Fatalf("write input: %v", err)
	}
	inWrite.Close()

	line, err := reader.ReadLine("geb> ")
	if err != nil {
		t.Fatalf("read line: %v", err)
	}
	if line != "draft" {
		t.Fatalf("line: got %q, want %q", line, "draft")
	}
}

func TestTerminalLineReaderCtrlCRequiresSecondPressToQuit(t *testing.T) {
	inRead, inWrite, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	defer inRead.Close()
	out, err := os.CreateTemp(t.TempDir(), "repl-out-*")
	if err != nil {
		t.Fatalf("temp output: %v", err)
	}
	defer out.Close()

	reader := &terminalLineReader{in: inRead, out: out}
	if _, err := inWrite.Write([]byte{3, 3}); err != nil {
		t.Fatalf("write input: %v", err)
	}
	inWrite.Close()

	_, err = reader.ReadLine("geb> ")
	if err != io.EOF {
		t.Fatalf("read line error: got %v, want io.EOF", err)
	}
	if _, err := out.Seek(0, 0); err != nil {
		t.Fatalf("seek output: %v", err)
	}
	data, err := io.ReadAll(out)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	if !strings.Contains(string(data), "Press Ctrl+C again to quit.") {
		t.Fatalf("output missing ctrl-c hint: %q", string(data))
	}
}

func TestCompleteREPLLineSuggestsCommandsNamesAndModuleMembers(t *testing.T) {
	session, err := evaluator.NewSession(&bytes.Buffer{}, nil, nil)
	if err != nil {
		t.Fatalf("new session: %v", err)
	}
	defer session.Close()
	evalREPLSource("import sys;\nlet sampleName = 1;\n", session, &bytes.Buffer{}, &bytes.Buffer{})

	command := completeREPLLine(":hi", session)
	if command.Replacement != ":history" {
		t.Fatalf("command completion: %#v", command)
	}
	name := completeREPLLine("sam", session)
	if name.Replacement != "sampleName" {
		t.Fatalf("name completion: %#v", name)
	}
	member := completeREPLLine("sys.ex", session)
	if member.Replacement != "sys.exit" {
		t.Fatalf("member completion: %#v", member)
	}
}

func TestRunScriptFallsBackToEvaluatorByDefault(t *testing.T) {
	dir := t.TempDir()
	// macOS temp dirs are under /var -> /private/var; canonicalize to match os.Getwd().
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		dir = resolved
	}
	previous, err := os.Getwd()
	if err != nil {
		t.Fatalf("get cwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir temp: %v", err)
	}
	defer func() {
		if err := os.Chdir(previous); err != nil {
			t.Fatalf("restore cwd: %v", err)
		}
	}()
	sourcePath := filepath.Join(dir, "fallback.gb")
	source := []byte(`import io;

class User {
    string name;

    func User(string name) {
        this.name = name;
    }

    func label(): string {
        return this.name;
    }
}

User u = User("Ada");
io.println(u.label());
`)
	program, err := parseAndAnalyze(sourcePath, string(source))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	var out bytes.Buffer
	code, err := runScript(sourcePath, nil, source, program, executionAuto, nil, &out, nil)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if code != 0 {
		t.Fatalf("exit code: got %d", code)
	}
	if out.String() != "Ada\n" {
		t.Fatalf("output: got %q", out.String())
	}
}

// TestRunScriptAbortsOnNonParityBytecodeError verifies that when the
// bytecode compiler catches a real static-analysis error (a call to
// an unknown overload, in this case) we abort the run before any
// statement is executed - we do NOT fall back to the evaluator and
// run partway through before crashing.
func TestRunScriptAbortsOnNonParityBytecodeError(t *testing.T) {
	dir := t.TempDir()
	previous, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir temp: %v", err)
	}
	defer os.Chdir(previous)
	sourcePath := filepath.Join(dir, "bad_overload.gb")
	source := []byte(`import io;

func describe(string v): string { return "s:" + v; }
func describe(int v): string { return "i:" + (v as string); }

io.println("about to call describe with a float");
io.println(describe(1.0f));
io.println("unreachable");
`)
	program, err := parseAndAnalyze(sourcePath, string(source))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	var out bytes.Buffer
	code, err := runScript(sourcePath, nil, source, program, executionAuto, nil, &out, nil)
	if err == nil {
		t.Fatal("expected static analysis to abort the run")
	}
	if code != 1 {
		t.Fatalf("exit code: got %d, want 1", code)
	}
	if !strings.Contains(err.Error(), "no matching overload for describe") {
		t.Fatalf("error message: %v", err)
	}
	if out.Len() != 0 {
		t.Fatalf("nothing should have been printed before abort, got %q", out.String())
	}
}

// TestRunScriptRunsAliasedNativeImportOnVM verifies aliased native
// imports compile and execute on the bytecode VM directly. Previously
// this case fell back to the evaluator because the compiler didn't
// resolve the alias; now the compiler maps the alias to its canonical
// path so VM-strict mode also accepts these programs (see the
// companion TestRunScriptVMStrictAcceptsAliasedNativeImport).
func TestRunScriptRunsAliasedNativeImportOnVM(t *testing.T) {
	dir := t.TempDir()
	previous, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir temp: %v", err)
	}
	defer os.Chdir(previous)
	sourcePath := filepath.Join(dir, "alias.gb")
	source := []byte(`import io;
import path as natpath;
io.println(natpath.clean("/a/../b"));
`)
	program, err := parseAndAnalyze(sourcePath, string(source))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	var out, trace bytes.Buffer
	code, err := runScript(sourcePath, nil, source, program, executionAuto, nil, &out, &trace)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if code != 0 {
		t.Fatalf("exit code: got %d, want 0", code)
	}
	if !strings.Contains(out.String(), "/b") {
		t.Fatalf("expected path.clean output, got %q", out.String())
	}
	// Confirm the script ran on the VM, not via evaluator fallback.
	if strings.Contains(trace.String(), "evaluator") {
		t.Fatalf("expected VM execution, but trace mentions evaluator: %q", trace.String())
	}
}

// TestRunScriptVMStrictAcceptsAliasedNativeImport is the companion to
// TestRunScriptVMStrictRejectsUnsupportedBytecode: under --vm-strict,
// a script that imports a native module under an alias must compile
// cleanly. Prior to the alias-aware compiler change this failed with
// "unknown bytecode name natpath".
func TestRunScriptVMStrictAcceptsAliasedNativeImport(t *testing.T) {
	dir := t.TempDir()
	previous, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir temp: %v", err)
	}
	defer os.Chdir(previous)
	sourcePath := filepath.Join(dir, "strict_alias.gb")
	source := []byte(`import io;
import path as natpath;
io.println(natpath.clean("/a/../b"));
`)
	program, err := parseAndAnalyze(sourcePath, string(source))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	var out bytes.Buffer
	code, err := runScript(sourcePath, nil, source, program, executionVMStrict, nil, &out, nil)
	if err != nil {
		t.Fatalf("run --vm-strict: %v", err)
	}
	if code != 0 {
		t.Fatalf("exit code: got %d, want 0", code)
	}
	if !strings.Contains(out.String(), "/b") {
		t.Fatalf("expected path.clean output, got %q", out.String())
	}
}

func TestRunScriptVMStrictRejectsUnsupportedBytecode(t *testing.T) {
	dir := t.TempDir()
	// macOS temp dirs are under /var -> /private/var; canonicalize to match os.Getwd().
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		dir = resolved
	}
	previous, err := os.Getwd()
	if err != nil {
		t.Fatalf("get cwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir temp: %v", err)
	}
	defer func() {
		if err := os.Chdir(previous); err != nil {
			t.Fatalf("restore cwd: %v", err)
		}
	}()
	sourcePath := filepath.Join(dir, "strict.gb")
	/* Non-literal class field defaults are still routed through the
	 * evaluator, so `--vm-strict` must reject this rather than fall
	 * back. (`static func` used to be in this slot but the compiler
	 * now lowers it directly.) */
	source := []byte(`func compute(): int { return 42; }
class Foo {
    int x = compute();
}
`)
	program, err := parseAndAnalyze(sourcePath, string(source))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	var out bytes.Buffer
	code, err := runScript(sourcePath, nil, source, program, executionVMStrict, nil, &out, nil)
	if err == nil {
		t.Fatal("expected strict VM error")
	}
	if code != 1 {
		t.Fatalf("exit code: got %d", code)
	}
	if !strings.Contains(err.Error(), "line 3:5: bytecode compiler only supports literal class field defaults") {
		t.Fatalf("error: got %v", err)
	}
}

func TestRunScriptRunsUserModuleImportsOnVM(t *testing.T) {
	dir := t.TempDir()
	// macOS temp dirs are under /var -> /private/var; canonicalize to match os.Getwd().
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		dir = resolved
	}
	previous, err := os.Getwd()
	if err != nil {
		t.Fatalf("get cwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir temp: %v", err)
	}
	defer func() {
		if err := os.Chdir(previous); err != nil {
			t.Fatalf("restore cwd: %v", err)
		}
	}()
	if err := os.WriteFile(filepath.Join(dir, "util.gb"), []byte(`import io;
io.println("imported");
export const string label = "module";
const string suffix = "!";
export func shout(string value): string {
    return value + suffix;
}
export class User {
    string name;

    func User(string name) {
        this.name = name;
    }

    func label(): string {
        return this.name + suffix;
    }
}
`), 0o644); err != nil {
		t.Fatalf("write module: %v", err)
	}
	sourcePath := filepath.Join(dir, "main.gb")
	source := []byte(`import io;
import util;
io.println(util.label);
io.println(util.shout("ok"));
let user = util.User("Ada");
io.println(user.label());
`)
	program, err := parseAndAnalyze(sourcePath, string(source))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	var out bytes.Buffer
	var trace bytes.Buffer
	code, err := runScript(sourcePath, nil, source, program, executionAuto, nil, &out, &trace)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if code != 0 {
		t.Fatalf("exit code: got %d", code)
	}
	if out.String() != "imported\nmodule\nok!\nAda!\n" {
		t.Fatalf("output: got %q", out.String())
	}
	if !strings.Contains(trace.String(), "execution=vm") {
		t.Fatalf("trace: got %q, want VM execution", trace.String())
	}
}

func TestRunModuleInvokesExportedMainOnVM(t *testing.T) {
	dir := t.TempDir()
	// macOS temp dirs are under /var -> /private/var; canonicalize to match os.Getwd().
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		dir = resolved
	}
	previous, err := os.Getwd()
	if err != nil {
		t.Fatalf("get cwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir temp: %v", err)
	}
	defer func() {
		if err := os.Chdir(previous); err != nil {
			t.Fatalf("restore cwd: %v", err)
		}
	}()
	if err := os.MkdirAll(filepath.Join(dir, "src", "app"), 0o755); err != nil {
		t.Fatalf("mkdir src: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "geblang.yaml"), []byte("name: app\nsource: src\n"), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "src", "app", "cli.gb"), []byte(`module app.cli;

import io;

export func main(list<string> args): int {
    io.println("args=" + (args[0] as string) + "," + (args[1] as string));
    return 0;
}
`), 0o644); err != nil {
		t.Fatalf("write module: %v", err)
	}

	var out bytes.Buffer
	var trace bytes.Buffer
	code, err := runModule("app.cli", []string{"one", "two"}, executionAuto, true, nil, &out, &trace)
	if err != nil {
		t.Fatalf("run module: %v", err)
	}
	if code != 0 {
		t.Fatalf("exit code: got %d", code)
	}
	if out.String() != "args=one,two\n" {
		t.Fatalf("output: got %q", out.String())
	}
	if !strings.Contains(trace.String(), "execution=vm") {
		t.Fatalf("trace: got %q, want VM execution", trace.String())
	}
}

func TestCLIAllowFFIAppliesToModule(t *testing.T) {
	gebBin := filepath.Join(t.TempDir(), "geblang")
	if out, err := exec.Command("go", "build", "-o", gebBin, ".").CombinedOutput(); err != nil {
		t.Fatalf("go build geblang: %v\n%s", err, out)
	}
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "src", "app"), 0o755); err != nil {
		t.Fatalf("mkdir src: %v", err)
	}
	writePermFile(t, filepath.Join(dir, "geblang.yaml"), "name: app\nsource: src\n")
	writePermFile(t, filepath.Join(dir, "src", "app", "cli.gb"), `module app.cli;

import ffi;
import io;

export func main(list<string> args): int {
    try {
        let lib = ffi.dlopen("/no/such/module-ffi.so");
        lib.close();
        io.println("OPENED");
    } catch (PermissionError e) {
        io.println("DENIED");
    } catch (Error e) {
        io.println("ALLOWED");
    }
    return 0;
}
`)
	cacheDir := t.TempDir()

	for _, tc := range []struct {
		name string
		args []string
	}{
		{
			name: "vm",
			args: []string{"--allow-ffi", "/no/such/module-ffi.so", "-m", "app.cli"},
		},
		{
			name: "evaluator",
			args: []string{"--disable-vm", "--allow-ffi", "/no/such/module-ffi.so", "-m", "app.cli"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cmd := exec.Command(gebBin, tc.args...)
			cmd.Dir = dir
			cmd.Env = append(os.Environ(), "XDG_CACHE_HOME="+cacheDir)
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("run module: %v\n%s", err, out)
			}
			if string(out) != "ALLOWED\n" {
				t.Fatalf("output: got %q, want CLI FFI permission to apply", out)
			}
		})
	}
}

func TestRunModuleInvokesSourceStdlibModule(t *testing.T) {
	previous, err := os.Getwd()
	if err != nil {
		t.Fatalf("get cwd: %v", err)
	}
	repoRoot, err := filepath.Abs(filepath.Join(previous, "../.."))
	if err != nil {
		t.Fatalf("repo root: %v", err)
	}
	t.Setenv("GEBLANG_STDLIB", filepath.Join(repoRoot, "stdlib"))

	var out bytes.Buffer
	var trace bytes.Buffer
	code, err := runModule("http.server", []string{"--help"}, executionAuto, true, nil, &out, &trace)
	if err != nil {
		t.Fatalf("run module: %v", err)
	}
	if code != 0 {
		t.Fatalf("exit code: got %d", code)
	}
	if out.String() != "usage: geblang -m http.server [port]\n" {
		t.Fatalf("output: got %q", out.String())
	}
	if !strings.Contains(trace.String(), "execution=vm") {
		t.Fatalf("trace: got %q, want VM execution", trace.String())
	}
}

func TestRunScriptRunsReturnedModuleClosureOnVM(t *testing.T) {
	dir := t.TempDir()
	// macOS temp dirs are under /var -> /private/var; canonicalize to match os.Getwd().
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		dir = resolved
	}
	previous, err := os.Getwd()
	if err != nil {
		t.Fatalf("get cwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir temp: %v", err)
	}
	defer func() {
		if err := os.Chdir(previous); err != nil {
			t.Fatalf("restore cwd: %v", err)
		}
	}()
	if err := os.WriteFile(filepath.Join(dir, "util.gb"), []byte(`export func suffixer(string suffix): callable {
    return func(string value): string {
        return value + suffix;
    };
}
`), 0o644); err != nil {
		t.Fatalf("write module: %v", err)
	}
	sourcePath := filepath.Join(dir, "main.gb")
	source := []byte(`import io;
import util;

let bang = util.suffixer("!");
io.println(bang("ok"));
`)
	program, err := parseAndAnalyze(sourcePath, string(source))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	var out bytes.Buffer
	var trace bytes.Buffer
	code, err := runScript(sourcePath, nil, source, program, executionAuto, nil, &out, &trace)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if code != 0 {
		t.Fatalf("exit code: got %d", code)
	}
	if out.String() != "ok!\n" {
		t.Fatalf("output: got %q", out.String())
	}
	if !strings.Contains(trace.String(), "execution=vm") {
		t.Fatalf("trace: got %q, want VM execution", trace.String())
	}
}

func TestRunScriptRunsDecoratedUserModuleFunctionOnVM(t *testing.T) {
	dir := t.TempDir()
	// macOS temp dirs are under /var -> /private/var; canonicalize to match os.Getwd().
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		dir = resolved
	}
	previous, err := os.Getwd()
	if err != nil {
		t.Fatalf("get cwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir temp: %v", err)
	}
	defer func() {
		if err := os.Chdir(previous); err != nil {
			t.Fatalf("restore cwd: %v", err)
		}
	}()
	if err := os.WriteFile(filepath.Join(dir, "util.gb"), []byte(`import io;

func prefix(any next, string label): any {
    io.println("decorated");
    return func(string name): string { return label + next(name); };
}

export @prefix("Hello, ")
func greet(string name): string {
    return name;
}
`), 0o644); err != nil {
		t.Fatalf("write module: %v", err)
	}
	sourcePath := filepath.Join(dir, "main.gb")
	source := []byte(`import io;
import reflect;
import util;

io.println(util.greet("Ada"));
io.println(util.greet("Bob"));
io.println(reflect.decorators(util.greet)[0]["name"]);
io.println(reflect.decorators(util.greet)[0]["args"][0]);
`)
	program, err := parseAndAnalyze(sourcePath, string(source))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	var out bytes.Buffer
	var trace bytes.Buffer
	code, err := runScript(sourcePath, nil, source, program, executionAuto, nil, &out, &trace)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if code != 0 {
		t.Fatalf("exit code: got %d", code)
	}
	want := "decorated\nHello, Ada\nHello, Bob\nprefix\nHello, \n"
	if out.String() != want {
		t.Fatalf("output: got %q, want %q", out.String(), want)
	}
	if !strings.Contains(trace.String(), "execution=vm") {
		t.Fatalf("trace: got %q, want VM execution", trace.String())
	}
}

func TestRunScriptRunsAsyncRunOnDecoratedUserModuleFunctionOnVM(t *testing.T) {
	dir := t.TempDir()
	// macOS temp dirs are under /var -> /private/var; canonicalize to match os.Getwd().
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		dir = resolved
	}
	previous, err := os.Getwd()
	if err != nil {
		t.Fatalf("get cwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir temp: %v", err)
	}
	defer func() {
		if err := os.Chdir(previous); err != nil {
			t.Fatalf("restore cwd: %v", err)
		}
	}()
	if err := os.WriteFile(filepath.Join(dir, "util.gb"), []byte(`import io;

func passthrough(any next): any {
    io.println("decorated");
    return func(): any { return next(); };
}

export @passthrough
async func greet(): string {
    return "hello";
}
`), 0o644); err != nil {
		t.Fatalf("write module: %v", err)
	}
	sourcePath := filepath.Join(dir, "main.gb")
	source := []byte(`import async;
import io;
import util;

let task = async.run(util.greet);
io.println(typeof(task));
io.println(await task);
`)
	program, err := parseAndAnalyze(sourcePath, string(source))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	var out bytes.Buffer
	var trace bytes.Buffer
	code, err := runScript(sourcePath, nil, source, program, executionAuto, nil, &out, &trace)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if code != 0 {
		t.Fatalf("exit code: got %d", code)
	}
	want := "decorated\nTask\nhello\n"
	if out.String() != want {
		t.Fatalf("output: got %q, want %q", out.String(), want)
	}
	if !strings.Contains(trace.String(), "execution=vm") {
		t.Fatalf("trace: got %q, want VM execution", trace.String())
	}
}

func TestRunScriptRunsDecoratedUserModuleClassMethodsOnVM(t *testing.T) {
	dir := t.TempDir()
	// macOS temp dirs are under /var -> /private/var; canonicalize to match os.Getwd().
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		dir = resolved
	}
	previous, err := os.Getwd()
	if err != nil {
		t.Fatalf("get cwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir temp: %v", err)
	}
	defer func() {
		if err := os.Chdir(previous); err != nil {
			t.Fatalf("restore cwd: %v", err)
		}
	}()
	if err := os.WriteFile(filepath.Join(dir, "util.gb"), []byte(`import io;

func suffix(any next, string mark): any {
    io.println("method decorated");
    return func(): string { return next() + mark; };
}

func prefix(any next, string label): any {
    io.println("static decorated");
    return func(string name): string { return label + next(name); };
}

export class Greeter {
    string name;

    func Greeter(string name) {
        this.name = name;
    }

    @suffix("!")
    func greet(): string {
        return this.name;
    }

    @prefix("kind:")
    static func label(string name): string {
        return name;
    }
}
`), 0o644); err != nil {
		t.Fatalf("write module: %v", err)
	}
	sourcePath := filepath.Join(dir, "main.gb")
	source := []byte(`import io;
import reflect;
import util;

let greeter = util.Greeter("Ada");
io.println(greeter.greet());
io.println(greeter.greet());
io.println(util.Greeter.label("user"));
io.println(util.Greeter.label("admin"));
let method = reflect.method(util.Greeter, "greet");
io.println(reflect.decorators(method)[0]["name"]);
io.println(reflect.decorators(method)[0]["args"][0]);
let staticMethod = reflect.staticMethod(util.Greeter, "label");
io.println(reflect.decorators(staticMethod)[0]["name"]);
io.println(reflect.decorators(staticMethod)[0]["args"][0]);
`)
	program, err := parseAndAnalyze(sourcePath, string(source))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	var out bytes.Buffer
	var trace bytes.Buffer
	code, err := runScript(sourcePath, nil, source, program, executionAuto, nil, &out, &trace)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if code != 0 {
		t.Fatalf("exit code: got %d", code)
	}
	want := "method decorated\nstatic decorated\nAda!\nAda!\nkind:user\nkind:admin\nsuffix\n!\nprefix\nkind:\n"
	if out.String() != want {
		t.Fatalf("output: got %q, want %q", out.String(), want)
	}
	if !strings.Contains(trace.String(), "execution=vm") {
		t.Fatalf("trace: got %q, want VM execution", trace.String())
	}
}

func TestRunScriptRunsStatefulBuiltinOnVM(t *testing.T) {
	dir := t.TempDir()
	// macOS temp dirs are under /var -> /private/var; canonicalize to match os.Getwd().
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		dir = resolved
	}
	previous, err := os.Getwd()
	if err != nil {
		t.Fatalf("get cwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir temp: %v", err)
	}
	defer func() {
		if err := os.Chdir(previous); err != nil {
			t.Fatalf("restore cwd: %v", err)
		}
	}()
	sourcePath := filepath.Join(dir, "main.gb")
	source := []byte(`import io;
import metrics;

metrics.inc("hits");
metrics.inc("hits", 2);
io.println(metrics.get("hits"));
`)
	program, err := parseAndAnalyze(sourcePath, string(source))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	var out bytes.Buffer
	var trace bytes.Buffer
	code, err := runScript(sourcePath, nil, source, program, executionAuto, nil, &out, &trace)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if code != 0 {
		t.Fatalf("exit code: got %d", code)
	}
	if out.String() != "3\n" {
		t.Fatalf("output: got %q", out.String())
	}
	if !strings.Contains(trace.String(), "execution=vm") {
		t.Fatalf("trace: got %q, want VM execution", trace.String())
	}
}

func TestRunScriptRunsWebCallbacksOnVM(t *testing.T) {
	dir := t.TempDir()
	// macOS temp dirs are under /var -> /private/var; canonicalize to match os.Getwd().
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		dir = resolved
	}
	previous, err := os.Getwd()
	if err != nil {
		t.Fatalf("get cwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir temp: %v", err)
	}
	defer func() {
		if err := os.Chdir(previous); err != nil {
			t.Fatalf("restore cwd: %v", err)
		}
	}()
	sourcePath := filepath.Join(dir, "main.gb")
	source := []byte(`import io;
import web;

let app = web.new();

web.use(app, func(dict<string, any> request, dict<string, any> response): dict<string, any> {
    return web.withHeader(response, "X-App", "Geblang");
});

web.get(app, "/users/:id", func(dict<string, any> request): dict<string, any> {
    return {"status": 200, "body": "user " + request["params"]["id"]};
});

let response = web.handle(app, {"method": "GET", "path": "/users/42", "body": ""});
io.println(response["status"]);
io.println(response["body"]);
io.println(response["headers"]["X-App"]);
`)
	program, err := parseAndAnalyze(sourcePath, string(source))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	var out bytes.Buffer
	var trace bytes.Buffer
	code, err := runScript(sourcePath, nil, source, program, executionAuto, nil, &out, &trace)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if code != 0 {
		t.Fatalf("exit code: got %d", code)
	}
	if out.String() != "200\nuser 42\nGeblang\n" {
		t.Fatalf("output: got %q", out.String())
	}
	if !strings.Contains(trace.String(), "execution=vm") {
		t.Fatalf("trace: got %q, want VM execution", trace.String())
	}
}

func TestRunScriptRunsSourceWebRouterCallbacksOnVM(t *testing.T) {
	dir := t.TempDir()
	// macOS temp dirs are under /var -> /private/var; canonicalize to match os.Getwd().
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		dir = resolved
	}
	previous, err := os.Getwd()
	if err != nil {
		t.Fatalf("get cwd: %v", err)
	}
	repoRoot, err := filepath.Abs(filepath.Join(previous, "../.."))
	if err != nil {
		t.Fatalf("repo root: %v", err)
	}
	t.Setenv("GEBLANG_STDLIB", filepath.Join(repoRoot, "stdlib"))
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir temp: %v", err)
	}
	defer func() {
		if err := os.Chdir(previous); err != nil {
			t.Fatalf("restore cwd: %v", err)
		}
	}()
	sourcePath := filepath.Join(dir, "main.gb")
	source := []byte(`import io;
import web;
import web.auth as auth;
import web.forms as forms;
import web.http as http;
import web.middleware as middleware;
import web.router as router;
import web.session as session;
import web.sse as sse;
import web.validation as validation;

let app = router.newRouter();
router.use(app, func(dict<string, any> request, dict<string, any> response): dict<string, any> {
    return web.withHeader(response, "X-App", "Geblang");
});
router.use(app, middleware.securityHeaders());
router.use(app, middleware.requestId());
let api = router.group(app, "/api");
router.get(api, "/users/:id", func(dict<string, any> request): dict<string, any> {
    return {"status": 200, "body": "user " + request["params"]["id"]};
});

router.post(api, "/users", func(dict<string, any> request): string {
    let result = forms.validate(request, validation.object({
        "name": validation.stringField()
    }, ["name"]));
    if (!forms.isValid(result)) {
        return "invalid " + forms.firstFieldError(result, "name");
    }
    let data = forms.data(result);
    return "created " + (data["name"] as string);
});

router.get(api, "/page", func(dict<string, any> request): dict<string, any> {
    let ctx = http.context(request);
    return ctx.render("<h1>{{.title}}</h1>", {"title": "Geblang"});
});

router.get(api, "/events", func(dict<string, any> request): dict<string, any> {
    return sse.response([
        sse.comment("ready"),
        sse.event("user", "{\"id\":42}", {"id": "u42"}),
        sse.retry(5000)
    ]);
});

router.get(api, "/inspect/:id", func(dict<string, any> request): dict<string, any> {
    let req = http.requestObject(request);
    return http.responseObject(200, req.paramDefault("id", "none") + " " + req.header("x-mode")).header("X-Param", req.param("id")).toDict();
});

class UsersController {
    @middleware
    func mark(dict<string, any> request, dict<string, any> response): dict<string, any> {
        return web.withHeader(response, "X-Controller", "Users");
    }

    @route("GET", "/decorated/:id")
    func show(dict<string, any> request): dict<string, any> {
        let ctx = http.context(request);
        return http.withCookieOptions(ctx.text("decorated " + ctx.param("id") + " " + ctx.cookie("theme") + " " + ctx.queryParam("debug")), "seen", "yes", {"path": "/", "httpOnly": true, "sameSite": "Lax"});
    }
}

router.mount(api, UsersController());

let foundRequest = http.request("GET", "/api/users/42");
foundRequest["headers"] = {"X-Request-ID": "req-1"};
let response = router.handle(app, foundRequest);
let created = router.handle(app, http.requestWithBody("POST", "/api/users", "name=Ada"));
let invalid = router.handle(app, http.requestWithBody("POST", "/api/users", ""));
let page = router.handle(app, http.request("GET", "/api/page"));
let events = router.handle(app, http.request("GET", "/api/events"));
let inspectRequest = http.request("GET", "/api/inspect/abc");
inspectRequest["headers"] = {"X-Mode": "fast"};
let inspected = router.handle(app, inspectRequest);
let decoratedRequest = http.request("GET", "/api/decorated/7?debug=true");
decoratedRequest["headers"] = {"Cookie": "theme=dark"};
let decorated = router.handle(app, decoratedRequest);
let sessionResponse = session.withSession(http.text("session"), {"user": "Ada"}, "secret", {"path": "/", "httpOnly": true});
let sessionRequest = http.request("GET", "/api/session");
sessionRequest["headers"] = {"Cookie": sessionResponse["headers"]["Set-Cookie"]};
let sessionData = session.session(sessionRequest, "secret");
let csrfResponse = auth.withCsrf(http.text("csrf"), "secret", {"path": "/", "sameSite": "Strict"});
let csrfToken = csrfResponse["headers"]["Set-Cookie"].split(";")[0].split("=")[1];
let csrfRequest = http.requestWithBody("POST", "/api/users", "_csrf=" + csrfToken);
csrfRequest["headers"] = {"Cookie": csrfResponse["headers"]["Set-Cookie"]};
io.println(response["status"]);
io.println(response["body"]);
io.println(response["headers"]["X-App"]);
io.println(response["headers"]["X-Frame-Options"]);
io.println(response["headers"]["X-Request-ID"]);
io.println(created["body"]);
io.println(invalid["body"].contains("$.name"));
io.println(page["body"]);
io.println(page["headers"]["Content-Type"]);
io.println(events["headers"]["Content-Type"]);
io.println(events["body"].contains("event: user"));
io.println(events["body"].contains("retry: 5000"));
io.println(http.body(inspected));
io.println(http.header(inspected, "x-param"));
io.println(decorated["body"]);
io.println(decorated["headers"]["X-App"]);
io.println(decorated["headers"]["X-Controller"]);
io.println(decorated["headers"]["Set-Cookie"]);
io.println(sessionData["user"]);
io.println(auth.verifyCsrf(csrfRequest, "secret"));
`)
	program, err := parseAndAnalyze(sourcePath, string(source))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	var out bytes.Buffer
	var trace bytes.Buffer
	code, err := runScript(sourcePath, nil, source, program, executionAuto, nil, &out, &trace)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if code != 0 {
		t.Fatalf("exit code: got %d", code)
	}
	if out.String() != "200\nuser 42\nGeblang\nDENY\nreq-1\ncreated Ada\ntrue\n<h1>Geblang</h1>\ntext/html; charset=utf-8\ntext/event-stream; charset=utf-8\ntrue\ntrue\nabc fast\nabc\ndecorated 7 dark true\nGeblang\nUsers\nseen=yes; Path=/; SameSite=Lax; HttpOnly\nAda\ntrue\n" {
		t.Fatalf("output: got %q", out.String())
	}
	if !strings.Contains(trace.String(), "execution=vm") {
		t.Fatalf("trace: got %q, want VM execution", trace.String())
	}
}

func TestRunScriptRunsSourceWebSocketModuleOnVM(t *testing.T) {
	probe, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Skipf("local TCP sockets unavailable: %v", err)
	}
	probe.Close()

	dir := t.TempDir()
	// macOS temp dirs are under /var -> /private/var; canonicalize to match os.Getwd().
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		dir = resolved
	}
	previous, err := os.Getwd()
	if err != nil {
		t.Fatalf("get cwd: %v", err)
	}
	repoRoot, err := filepath.Abs(filepath.Join(previous, "../.."))
	if err != nil {
		t.Fatalf("repo root: %v", err)
	}
	t.Setenv("GEBLANG_STDLIB", filepath.Join(repoRoot, "stdlib"))
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir temp: %v", err)
	}
	defer func() {
		if err := os.Chdir(previous); err != nil {
			t.Fatalf("restore cwd: %v", err)
		}
	}()

	sourcePath := filepath.Join(dir, "main.gb")
	source := []byte(`import http;
import io;
import web.router as router;
import web.websocket as ws;

let app = router.newRouter();

router.get(app, "/ws", func(dict<string, any> request): dict<string, any> {
    return ws.upgrade(func(any conn): void {
        let message = ws.readJson(conn);
        ws.sendJson(conn, {"echo": message["text"]});
        ws.close(conn);
    });
});

let server = http.listen("127.0.0.1:0", func(dict<string, any> request): dict<string, any> {
    return router.handle(app, request);
});

let conn = ws.connect("ws://" + http.serverAddr(server) + "/ws");
ws.sendJson(conn, {"text": "hello"});
let reply = ws.readJson(conn);
io.println(reply["echo"]);
ws.close(conn);
http.shutdown(server, 1000);
http.close(server);
`)
	program, err := parseAndAnalyze(sourcePath, string(source))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	var out bytes.Buffer
	var trace bytes.Buffer
	code, err := runScript(sourcePath, nil, source, program, executionAuto, nil, &out, &trace)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if code != 0 {
		t.Fatalf("exit code: got %d", code)
	}
	if out.String() != "hello\n" {
		t.Fatalf("output: got %q", out.String())
	}
	if !strings.Contains(trace.String(), "execution=vm") {
		t.Fatalf("trace: got %q, want VM execution", trace.String())
	}
}

func TestRunScriptRunsSourceRedisModuleOnVM(t *testing.T) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Skipf("local TCP sockets unavailable: %v", err)
	}
	defer listener.Close()
	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		buf := make([]byte, 1024)
		for {
			n, err := conn.Read(buf)
			if err != nil {
				return
			}
			req := string(buf[:n])
			switch {
			case strings.Contains(req, "\r\nPING\r\n"):
				_, _ = conn.Write([]byte("+PONG\r\n"))
			case strings.Contains(req, "\r\nSET\r\n"):
				_, _ = conn.Write([]byte("+OK\r\n"))
			case strings.Contains(req, "\r\nGET\r\n"):
				_, _ = conn.Write([]byte("$3\r\nAda\r\n"))
			case strings.Contains(req, "\r\nEXISTS\r\n"):
				_, _ = conn.Write([]byte(":1\r\n"))
			case strings.Contains(req, "\r\nSMEMBERS\r\n"):
				_, _ = conn.Write([]byte("*2\r\n$3\r\nred\r\n$4\r\nblue\r\n"))
			case strings.Contains(req, "\r\nHGETALL\r\n"):
				_, _ = conn.Write([]byte("*4\r\n$4\r\nname\r\n$3\r\nAda\r\n$4\r\nrole\r\n$5\r\nadmin\r\n"))
			default:
				_, _ = conn.Write([]byte("-ERR unsupported\r\n"))
			}
		}
	}()

	dir := t.TempDir()
	// macOS temp dirs are under /var -> /private/var; canonicalize to match os.Getwd().
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		dir = resolved
	}
	previous, err := os.Getwd()
	if err != nil {
		t.Fatalf("get cwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir temp: %v", err)
	}
	defer func() {
		if err := os.Chdir(previous); err != nil {
			t.Fatalf("restore cwd: %v", err)
		}
	}()
	repoRoot, err := filepath.Abs(filepath.Join(previous, "../.."))
	if err != nil {
		t.Fatalf("repo root: %v", err)
	}
	t.Setenv("GEBLANG_STDLIB", filepath.Join(repoRoot, "stdlib"))
	sourcePath := filepath.Join(dir, "main.gb")
	source := []byte(`import io;
import redis;

let client = redis.connect("` + listener.Addr().String() + `");
io.println(client.ping());
io.println(client.set("user", "Ada"));
io.println(client.get("user"));
io.println(client.exists("user"));
io.println(client.smembers("colors")[1]);
let profile = client.hgetAll("profile");
io.println(profile["role"]);
client.close();
`)
	program, err := parseAndAnalyze(sourcePath, string(source))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	var out bytes.Buffer
	var trace bytes.Buffer
	code, err := runScript(sourcePath, nil, source, program, executionAuto, nil, &out, &trace)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if code != 0 {
		t.Fatalf("exit code: got %d", code)
	}
	if out.String() != "true\ntrue\nAda\ntrue\nblue\nadmin\n" {
		t.Fatalf("output: got %q", out.String())
	}
	if !strings.Contains(trace.String(), "execution=vm") {
		t.Fatalf("trace: got %q, want VM execution", trace.String())
	}
	<-done
}

func TestRunScriptTraceExecReportsEngine(t *testing.T) {
	dir := t.TempDir()
	// macOS temp dirs are under /var -> /private/var; canonicalize to match os.Getwd().
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		dir = resolved
	}
	previous, err := os.Getwd()
	if err != nil {
		t.Fatalf("get cwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir temp: %v", err)
	}
	defer func() {
		if err := os.Chdir(previous); err != nil {
			t.Fatalf("restore cwd: %v", err)
		}
	}()
	sourcePath := filepath.Join(dir, "main.gb")
	source := []byte(`import io;
io.println("fast");
`)
	program, err := parseAndAnalyze(sourcePath, string(source))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	var out bytes.Buffer
	var trace bytes.Buffer
	code, err := runScript(sourcePath, nil, source, program, executionAuto, nil, &out, &trace)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if code != 0 {
		t.Fatalf("exit code: got %d", code)
	}
	if out.String() != "fast\n" {
		t.Fatalf("output: got %q", out.String())
	}
	if trace.String() != "geblang: execution=vm\n" {
		t.Fatalf("trace: got %q", trace.String())
	}
}

func TestBuildProducesSelfContainedExecutable(t *testing.T) {
	// Build a real geblang binary to use as the bundle base.
	// Calling runBuild() directly would embed the test binary, which re-runs
	// all tests when spawned — causing an infinite recursive loop.
	gebBin := filepath.Join(t.TempDir(), "geblang")
	buildOut, err := exec.Command("go", "build", "-o", gebBin, ".").CombinedOutput()
	if err != nil {
		t.Fatalf("go build geblang: %v\n%s", err, buildOut)
	}

	pkgDir := t.TempDir()
	srcDir := filepath.Join(pkgDir, "src")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatalf("mkdir src: %v", err)
	}

	if err := os.WriteFile(filepath.Join(pkgDir, "geblang.yaml"), []byte("name: testapp\nsource: src\n"), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	source := []byte(`module testapp.main;
import io;
import sys;
export func main(list<string> args): void {
    io.println("bundle-ok");
    if (args.length() > 0) { io.println(args[0]); }
}
`)
	if err := os.WriteFile(filepath.Join(srcDir, "main.gb"), source, 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}

	outBin := filepath.Join(t.TempDir(), "testapp")
	buildCmd := exec.Command(gebBin, "build", "--entry", "testapp.main", "--out", outBin, pkgDir)
	if out, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("geblang build: %v\n%s", err, out)
	}

	if _, err := os.Stat(outBin); err != nil {
		t.Fatalf("output binary not found: %v", err)
	}

	cmd := exec.Command(outBin, "hello")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run bundle: %v\noutput: %s", err, out)
	}
	got := string(out)
	if !strings.Contains(got, "bundle-ok") {
		t.Fatalf("expected 'bundle-ok' in output, got: %q", got)
	}
	if !strings.Contains(got, "hello") {
		t.Fatalf("expected 'hello' in output, got: %q", got)
	}
}

func TestBuildIncludesVendoredPackageDependencies(t *testing.T) {
	// Build a real geblang binary to use as the bundle base.
	gebBin := filepath.Join(t.TempDir(), "geblang")
	buildOut, err := exec.Command("go", "build", "-o", gebBin, ".").CombinedOutput()
	if err != nil {
		t.Fatalf("go build geblang: %v\n%s", err, buildOut)
	}

	pkgDir := t.TempDir()
	srcDir := filepath.Join(pkgDir, "src")
	depSrcDir := filepath.Join(pkgDir, "vendor", "dep", "src")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatalf("mkdir src: %v", err)
	}
	if err := os.MkdirAll(depSrcDir, 0o755); err != nil {
		t.Fatalf("mkdir dependency src: %v", err)
	}

	manifest := []byte(`name: app
source: src
dependencies:
  dep:
    git: https://example.com/dep.git
`)
	if err := os.WriteFile(filepath.Join(pkgDir, "geblang.yaml"), manifest, 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, "vendor", "dep", "geblang.yaml"), []byte("name: dep\nsource: src\n"), 0o644); err != nil {
		t.Fatalf("write dependency manifest: %v", err)
	}

	source := []byte(`module app.main;
import io;
import dep.lib as lib;
export func main(list<string> args): void {
    io.println(lib.message());
}
`)
	if err := os.WriteFile(filepath.Join(srcDir, "main.gb"), source, 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}
	depSource := []byte(`module dep.lib;
export func message(): string {
    return "vendor-ok";
}
`)
	if err := os.WriteFile(filepath.Join(depSrcDir, "lib.gb"), depSource, 0o644); err != nil {
		t.Fatalf("write dependency source: %v", err)
	}

	outBin := filepath.Join(t.TempDir(), "app")
	buildCmd := exec.Command(gebBin, "build", "--entry", "app.main", "--out", outBin, pkgDir)
	if out, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("geblang build: %v\n%s", err, out)
	}

	cmd := exec.Command(outBin)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run bundle: %v\noutput: %s", err, out)
	}
	if got := string(out); !strings.Contains(got, "vendor-ok") {
		t.Fatalf("expected vendored dependency output, got: %q", got)
	}
}

// TestReplInsertSemicolonsRespectsNesting guards a regression where
// multi-line container literals (lists of dicts, parenthesised expressions
// broken across lines, ...) had a `;` injected mid-literal because the
// line-ending token (`}`, `]`) is a statement-ender at the lexer level.
// The injector now tracks bracket nesting and only inserts at depth 0.
func TestReplInsertSemicolonsRespectsNesting(t *testing.T) {
	/* Each input ends in a newline because the REPL prompt loop
	 * always feeds the line that way; the injector only runs at
	 * `\n` boundaries. */
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "list of dicts spanning lines (the reported bug)",
			in: "let rows = [\n" +
				"    {\"name\": \"Alice\", \"role\": \"admin\"},\n" +
				"    {\"name\": \"Bob\",   \"role\": \"viewer\"}\n" +
				"];\n",
			want: "let rows = [\n" +
				"    {\"name\": \"Alice\", \"role\": \"admin\"},\n" +
				"    {\"name\": \"Bob\",   \"role\": \"viewer\"}\n" +
				"];\n",
		},
		{
			name: "function body still gets trailing semicolon",
			in: "func greet(): string {\n" +
				"    return \"hi\"\n" +
				"}\n",
			want: "func greet(): string {\n" +
				"    return \"hi\";\n" +
				"};\n",
		},
		{
			name: "multi-line function call args",
			in: "foo(\n" +
				"    1,\n" +
				"    2,\n" +
				"    3\n" +
				")\n",
			want: "foo(\n" +
				"    1,\n" +
				"    2,\n" +
				"    3\n" +
				");\n",
		},
		{
			name: "nested list of dicts with trailing element on its own line",
			in: "let data = [\n" +
				"    {\"a\": 1},\n" +
				"    {\"b\": [\n" +
				"        {\"c\": 2}\n" +
				"    ]}\n" +
				"];\n",
			want: "let data = [\n" +
				"    {\"a\": 1},\n" +
				"    {\"b\": [\n" +
				"        {\"c\": 2}\n" +
				"    ]}\n" +
				"];\n",
		},
		{
			name: "as-bool cast at end of REPL line",
			in:   "1 as bool\n",
			want: "1 as bool;\n",
		},
	}
	for _, tc := range cases {
		got := replInsertSemicolons(tc.in)
		if got != tc.want {
			t.Errorf("%s:\n  input:\n%s\n  got:\n%s\n  want:\n%s",
				tc.name, tc.in, got, tc.want)
		}
	}
}

// TestUndefinedBareTypeFailsBothBackends proves the analyzer's
// unknown-type error gates compilation on both the VM (`geblang <file>`)
// and the evaluator (`geblang test <file>`), and that a valid program
// runs clean on both.
func TestUndefinedBareTypeFailsBothBackends(t *testing.T) {
	gebBin := filepath.Join(t.TempDir(), "geblang")
	if out, err := exec.Command("go", "build", "-o", gebBin, ".").CombinedOutput(); err != nil {
		t.Fatalf("go build geblang: %v\n%s", err, out)
	}

	dir := t.TempDir()
	bad := filepath.Join(dir, "bad.gb")
	if err := os.WriteFile(bad, []byte("func f(NotARealType x): void {}\nf(1);\n"), 0o644); err != nil {
		t.Fatalf("write bad: %v", err)
	}
	good := filepath.Join(dir, "good.gb")
	if err := os.WriteFile(good, []byte("import io;\nfunc f(int x): void { io.println(\"${x}\"); }\nf(1);\n"), 0o644); err != nil {
		t.Fatalf("write good: %v", err)
	}

	for _, args := range [][]string{{bad}, {"test", bad}} {
		out, err := exec.Command(gebBin, args...).CombinedOutput()
		if err == nil {
			t.Fatalf("expected %v to fail, got success:\n%s", args, out)
		}
		if !strings.Contains(string(out), "unknown type") || !strings.Contains(string(out), "NotARealType") {
			t.Fatalf("expected unknown-type error for %v, got:\n%s", args, out)
		}
	}

	for _, args := range [][]string{{good}, {"test", good}} {
		if out, err := exec.Command(gebBin, args...).CombinedOutput(); err != nil {
			t.Fatalf("expected %v to succeed, got: %v\n%s", args, err, out)
		}
	}
}

// TestRunSubcommandAliasesBareScriptForm: `geblang run <script>` mirrors the bare form.
func TestRunSubcommandAliasesBareScriptForm(t *testing.T) {
	gebBin := filepath.Join(t.TempDir(), "geblang")
	if out, err := exec.Command("go", "build", "-o", gebBin, ".").CombinedOutput(); err != nil {
		t.Fatalf("go build geblang: %v\n%s", err, out)
	}

	dir := t.TempDir()
	script := filepath.Join(dir, "hello.gb")
	src := "import io;\nimport sys;\nio.println(\"ran:${sys.args()}\");\n"
	if err := os.WriteFile(script, []byte(src), 0o644); err != nil {
		t.Fatalf("write script: %v", err)
	}

	bare, err := exec.Command(gebBin, script).CombinedOutput()
	if err != nil {
		t.Fatalf("bare run failed: %v\n%s", err, bare)
	}
	viaRun, err := exec.Command(gebBin, "run", script).CombinedOutput()
	if err != nil {
		t.Fatalf("run subcommand failed: %v\n%s", err, viaRun)
	}
	if string(bare) != string(viaRun) {
		t.Fatalf("run output differs from bare:\nbare=%q\nrun=%q", bare, viaRun)
	}

	withArgs, err := exec.Command(gebBin, "run", script, "a", "b").CombinedOutput()
	if err != nil {
		t.Fatalf("run with args failed: %v\n%s", err, withArgs)
	}
	if !strings.Contains(string(withArgs), `["a", "b"]`) {
		t.Fatalf("run did not forward script args, got:\n%s", withArgs)
	}

	if out, err := exec.Command(gebBin, "run").CombinedOutput(); err == nil {
		t.Fatalf("expected `run` without a script to fail, got:\n%s", out)
	}

	// `run test` must open a file named test, not dispatch to the test runner.
	out, err := exec.Command(gebBin, "run", "test").CombinedOutput()
	if err == nil {
		t.Fatalf("expected `run test` to fail reading a file, got:\n%s", out)
	}
	if !strings.Contains(string(out), "read test") {
		t.Fatalf("expected `run test` to attempt a file read, got:\n%s", out)
	}
}

// TestBuildRuntimeFlagEmbedsSuppliedRuntime: `build --runtime <bin>` embeds the bundle into the given runtime.
func TestBuildRuntimeFlagEmbedsSuppliedRuntime(t *testing.T) {
	tmp := t.TempDir()
	gebBin := filepath.Join(tmp, "geblang")
	if out, err := exec.Command("go", "build", "-o", gebBin, ".").CombinedOutput(); err != nil {
		t.Fatalf("go build geblang: %v\n%s", err, out)
	}
	proj := filepath.Join(tmp, "proj")
	if err := os.MkdirAll(filepath.Join(proj, "src"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(proj, "geblang.yaml"), []byte("name: x\nversion: 0.1.0\nsource: src\n"), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	src := "module main;\nimport io;\nexport func main(list<string> args): int { io.println(\"runtime-flag ok\"); return 0; }\n"
	if err := os.WriteFile(filepath.Join(proj, "src", "main.gb"), []byte(src), 0o644); err != nil {
		t.Fatalf("write main: %v", err)
	}
	out := filepath.Join(tmp, "app")
	if o, err := exec.Command(gebBin, "build", "--runtime", gebBin, "--entry", "main", "--out", out, proj).CombinedOutput(); err != nil {
		t.Fatalf("build --runtime failed: %v\n%s", err, o)
	}
	got, err := exec.Command(out).CombinedOutput()
	if err != nil {
		t.Fatalf("run built binary: %v\n%s", err, got)
	}
	if !strings.Contains(string(got), "runtime-flag ok") {
		t.Fatalf("unexpected output: %s", got)
	}
}
