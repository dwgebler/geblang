package bytecode_test

// Parity tests run each snippet through both the evaluator and the VM and
// assert that the printed output matches.  They pin the intersection of
// features that both execution paths are expected to support identically.

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"geblang/internal/ast"
	"geblang/internal/bcloader"
	"geblang/internal/bytecode"
	"geblang/internal/evaluator"
	"geblang/internal/lexer"
	"geblang/internal/parser"
	"geblang/internal/runtime"
	"geblang/internal/semantic"

	"golang.org/x/crypto/ssh"
)

// newHarnessLoader builds a bcloader.Loader for parity tests: analyze-then-compile each module (no .gbc cache), resolving native modules via the evaluator.
func newHarnessLoader(stdout io.Writer, stateful bytecode.StatefulNativeCaller) *bcloader.Loader {
	return bcloader.New(stdout, nil, stateful, bcloader.Options{
		Compile: func(canonical, sourcePath string, source []byte, program *ast.Program, modulePaths []string) (bytecode.Chunk, error) {
			if diagnostics := semantic.New().Analyze(program); len(diagnostics) > 0 {
				messages := make([]string, 0, len(diagnostics))
				for _, d := range diagnostics {
					messages = append(messages, d.Message)
				}
				return bytecode.Chunk{}, fmt.Errorf("analyze module %s: %s", canonical, strings.Join(messages, "\n"))
			}
			return bytecode.Compile(program, source, canonical)
		},
		LookupBuiltin: func(canonical, alias string) *runtime.Module {
			if e, ok := stateful.(*evaluator.Evaluator); ok {
				return e.BuiltinModule(canonical, alias)
			}
			return nil
		},
	})
}


// runParityWithStdlib is like runParityStateful but additionally wires
// a bytecode-side module loader so source-distributed stdlib modules
// (time.scheduler, async.rate, ...) can be imported in the test source.
func runParityWithStdlib(t *testing.T, source string, want string) {
	t.Helper()

	p := parser.New(lexer.New(source))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parse errors: %v", p.Errors())
	}

	var evOut bytes.Buffer
	if _, err := evaluator.New(&evOut).Eval(program); err != nil {
		t.Fatalf("evaluator error: %v", err)
	}

	src := []byte(source)
	chunk, err := bytecode.Compile(program, src, "parity")
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}
	var vmOut bytes.Buffer
	stateful := evaluator.New(&vmOut)
	loader := newHarnessLoader(&vmOut, stateful)
	loader.SetMainChunk(chunk)
	vm := bytecode.NewVMWithModuleLoader(chunk, &vmOut, loader)
	loader.SetMainVM(vm)
	vm.SetStatefulNativeCaller(stateful)
	/* Without the dispatcher wiring, the evaluator can't reach
	 * back into the VM for test.mock + RunTestClass. Production
	 * (cmd/geblang/main.go) sets this; the parity harness
	 * mirrors that so feature-parity coverage is honest. */
	stateful.SetMethodDispatcher(vm)
	if err := vm.Run(); err != nil {
		t.Fatalf("vm error: %v", err)
	}

	if evOut.String() != vmOut.String() {
		t.Errorf("output mismatch:\n  evaluator: %q\n  vm:        %q", evOut.String(), vmOut.String())
	}
	if want != "" && evOut.String() != want {
		t.Errorf("wrong output: got %q, want %q", evOut.String(), want)
	}
}

// runParityModulesDir runs mainSource (loader-wired VM) against modules under dir on both backends and asserts identical output.
func runParityModulesDir(t *testing.T, dir string, mainSource string, want string) {
	t.Helper()

	p := parser.New(lexer.New(mainSource))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parse errors: %v", p.Errors())
	}

	var evOut bytes.Buffer
	ev := evaluator.NewWithArgsAndModulePaths(&evOut, nil, []string{dir})
	if _, err := ev.Eval(program); err != nil {
		t.Fatalf("evaluator error: %v", err)
	}

	chunk, err := bytecode.Compile(program, []byte(mainSource), "parity")
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}
	var vmOut bytes.Buffer
	stateful := evaluator.NewWithArgsAndModulePaths(&vmOut, nil, []string{dir})
	loader := newHarnessLoader(&vmOut, stateful)
	loader.SetModulePaths([]string{dir})
	loader.SetMainChunk(chunk)
	vm := bytecode.NewVMWithModuleLoader(chunk, &vmOut, loader)
	loader.SetMainVM(vm)
	vm.SetModulePaths([]string{dir})
	vm.SetStatefulNativeCaller(stateful)
	stateful.SetMethodDispatcher(vm)
	if err := vm.Run(); err != nil {
		t.Fatalf("vm error: %v", err)
	}

	if evOut.String() != vmOut.String() {
		t.Errorf("output mismatch:\n  evaluator: %q\n  vm:        %q", evOut.String(), vmOut.String())
	}
	if want != "" && evOut.String() != want {
		t.Errorf("wrong output: got %q, want %q", evOut.String(), want)
	}
}

// runParity compiles and runs source through both the evaluator and the VM,
// then checks that their outputs agree.  If want is non-empty it also asserts
// the exact expected output.
// runErrorParity runs source through both the evaluator and VM, expects both
// to produce errors, and checks that each error contains all substrings in
// wantSubstrings.  Use this for error-path tests where the two engines format
// messages differently but must include the same key information.
func runErrorParity(t *testing.T, source string, wantSubstrings ...string) {
	t.Helper()

	p := parser.New(lexer.New(source))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parse errors: %v", p.Errors())
	}

	var evOut bytes.Buffer
	_, evErr := evaluator.New(&evOut).Eval(program)
	if evErr == nil {
		t.Fatal("evaluator: expected error, got nil")
	}

	src := []byte(source)
	chunk, err := bytecode.Compile(program, src, "parity")
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}
	var vmOut bytes.Buffer
	vmErr := bytecode.NewVM(chunk, &vmOut).Run()
	if vmErr == nil {
		t.Fatal("vm: expected error, got nil")
	}

	for _, sub := range wantSubstrings {
		if !strings.Contains(evErr.Error(), sub) {
			t.Errorf("evaluator error missing %q: %v", sub, evErr)
		}
		if !strings.Contains(vmErr.Error(), sub) {
			t.Errorf("vm error missing %q: %v", sub, vmErr)
		}
	}
}

func runParity(t *testing.T, source string, want string) {
	t.Helper()

	p := parser.New(lexer.New(source))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parse errors: %v", p.Errors())
	}

	var evOut bytes.Buffer
	if _, err := evaluator.New(&evOut).Eval(program); err != nil {
		t.Fatalf("evaluator error: %v", err)
	}

	src := []byte(source)
	chunk, err := bytecode.Compile(program, src, "parity")
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}
	var vmOut bytes.Buffer
	if err := bytecode.NewVM(chunk, &vmOut).Run(); err != nil {
		t.Fatalf("vm error: %v", err)
	}

	if evOut.String() != vmOut.String() {
		t.Errorf("output mismatch:\n  evaluator: %q\n  vm:        %q", evOut.String(), vmOut.String())
	}
	if want != "" && evOut.String() != want {
		t.Errorf("wrong output: got %q, want %q", evOut.String(), want)
	}
}

// runParityStateful is like runParity but wires an evaluator as the VM's
// StatefulNativeCaller so modules like schema and serde work in both paths.
func runParityStateful(t *testing.T, source string, want string) {
	t.Helper()

	p := parser.New(lexer.New(source))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parse errors: %v", p.Errors())
	}

	var evOut bytes.Buffer
	if _, err := evaluator.New(&evOut).Eval(program); err != nil {
		t.Fatalf("evaluator error: %v", err)
	}

	src := []byte(source)
	chunk, err := bytecode.Compile(program, src, "parity")
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}
	var vmOut bytes.Buffer
	vm := bytecode.NewVM(chunk, &vmOut)
	vm.SetStatefulNativeCaller(evaluator.New(&vmOut))
	if err := vm.Run(); err != nil {
		t.Fatalf("vm error: %v", err)
	}

	if evOut.String() != vmOut.String() {
		t.Errorf("output mismatch:\n  evaluator: %q\n  vm:        %q", evOut.String(), vmOut.String())
	}
	if want != "" && evOut.String() != want {
		t.Errorf("wrong output: got %q, want %q", evOut.String(), want)
	}
}

// runParityStatefulWithFile writes fileContent to a temp file, substitutes
// its path into source (replacing the placeholder "TMPFILE"), then runs
// through both evaluator and VM in stateful mode.
func runParityStatefulWithFile(t *testing.T, source string, fileContent string, want string) {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "parity_*.txt")
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	if _, err := f.WriteString(fileContent); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	f.Close()
	source = strings.ReplaceAll(source, "TMPFILE", f.Name())
	runParityStateful(t, source, want)
}

// sshTestServer runs an in-process SSH server for hermetic SSH
// parity tests. The server accepts password "secret" for any
// username, signs an ephemeral RSA host key, and handles "exec"
// requests by running a tiny set of canned commands in a
// goroutine. Returns (addr, hostKeyPath, stopFn). The
// hostKeyPath is a temporary file containing the server's host
// key in known_hosts format so the test can verify without
// relying on insecureSkipHostKey.
type sshTestServer struct {
	listener net.Listener
	hostKey  ssh.Signer
	wg       sync.WaitGroup
	stopCh   chan struct{}
}

func startSSHTestServer(t *testing.T) *sshTestServer {
	t.Helper()
	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("ssh server rsa key: %v", err)
	}
	signer, err := ssh.NewSignerFromKey(rsaKey)
	if err != nil {
		t.Fatalf("ssh server signer: %v", err)
	}
	cfg := &ssh.ServerConfig{
		PasswordCallback: func(conn ssh.ConnMetadata, password []byte) (*ssh.Permissions, error) {
			if string(password) == "secret" {
				return nil, nil
			}
			return nil, fmt.Errorf("password rejected")
		},
	}
	cfg.AddHostKey(signer)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("ssh server listen: %v", err)
	}
	srv := &sshTestServer{listener: listener, hostKey: signer, stopCh: make(chan struct{})}
	srv.wg.Add(1)
	go srv.acceptLoop(cfg)
	return srv
}

func (s *sshTestServer) acceptLoop(cfg *ssh.ServerConfig) {
	defer s.wg.Done()
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			return
		}
		go s.handleConn(conn, cfg)
	}
}

func (s *sshTestServer) handleConn(rawConn net.Conn, cfg *ssh.ServerConfig) {
	defer rawConn.Close()
	sshConn, chans, reqs, err := ssh.NewServerConn(rawConn, cfg)
	if err != nil {
		return
	}
	defer sshConn.Close()
	go ssh.DiscardRequests(reqs)
	for newCh := range chans {
		if newCh.ChannelType() != "session" {
			_ = newCh.Reject(ssh.UnknownChannelType, "unsupported channel type")
			continue
		}
		ch, reqs, err := newCh.Accept()
		if err != nil {
			continue
		}
		go s.handleSession(ch, reqs)
	}
}

func (s *sshTestServer) handleSession(ch ssh.Channel, reqs <-chan *ssh.Request) {
	defer ch.Close()
	for req := range reqs {
		switch req.Type {
		case "exec":
			cmd := string(req.Payload[4:])
			_ = req.Reply(true, nil)
			s.runCommand(ch, cmd)
			_, _ = ch.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{0}))
			return
		case "shell":
			_ = req.Reply(true, nil)
			return
		default:
			_ = req.Reply(false, nil)
		}
	}
}

func (s *sshTestServer) runCommand(ch ssh.Channel, cmd string) {
	// Tiny canned responses so the test doesn't depend on a real
	// shell. The expectations stay readable in the parity test
	// source.
	switch {
	case cmd == "echo hello":
		_, _ = ch.Write([]byte("hello\n"))
	case cmd == "echo err 1>&2":
		_, _ = ch.Stderr().Write([]byte("err\n"))
	case strings.HasPrefix(cmd, "cat"):
		// Stream stdin to stdout so spawn/stdin tests can pipe.
		_, _ = io.Copy(ch, ch)
	}
}

func (s *sshTestServer) addr() string {
	return s.listener.Addr().String()
}

func (s *sshTestServer) port() string {
	_, port, _ := net.SplitHostPort(s.addr())
	return port
}

func (s *sshTestServer) stop() {
	_ = s.listener.Close()
	s.wg.Wait()
}

// reflect.classes() from an imported module must see entry-file classes on both backends.
func TestParityReflectClassesCrossModule(t *testing.T) {
	dir := t.TempDir()
	scan := "module scan;\n" +
		"import reflect;\n" +
		"export func sawEntry(string name): bool {\n" +
		"    for (c in reflect.classes()) {\n" +
		"        if (reflect.className(c) == name) { return true; }\n" +
		"    }\n" +
		"    return false;\n" +
		"}\n"
	if err := os.WriteFile(filepath.Join(dir, "scan.gb"), []byte(scan), 0o644); err != nil {
		t.Fatalf("write scan module: %v", err)
	}
	source := "import io;\n" +
		"import scan;\n" +
		"class EntryWidget { func EntryWidget() {} }\n" +
		"io.println(scan.sawEntry(\"EntryWidget\"));\n"

	p := parser.New(lexer.New(source))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parse errors: %v", p.Errors())
	}

	var evOut bytes.Buffer
	ev := evaluator.NewWithArgsAndModulePaths(&evOut, nil, []string{dir})
	if _, err := ev.Eval(program); err != nil {
		t.Fatalf("evaluator error: %v", err)
	}

	chunk, err := bytecode.Compile(program, []byte(source), "parity")
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}
	var vmOut bytes.Buffer
	stateful := evaluator.NewWithArgsAndModulePaths(&vmOut, nil, []string{dir})
	loader := newHarnessLoader(&vmOut, stateful)
	loader.SetModulePaths([]string{dir})
	loader.SetMainChunk(chunk)
	vm := bytecode.NewVMWithModuleLoader(chunk, &vmOut, loader)
	loader.SetMainVM(vm)
	vm.SetModulePaths([]string{dir})
	vm.SetStatefulNativeCaller(stateful)
	if err := vm.Run(); err != nil {
		t.Fatalf("vm error: %v", err)
	}

	if evOut.String() != vmOut.String() {
		t.Errorf("reflect.classes() cross-module divergence:\n  evaluator: %q\n  vm:        %q", evOut.String(), vmOut.String())
	}
	if evOut.String() != "true\n" {
		t.Errorf("entry class should be visible from an imported module; got eval=%q vm=%q", evOut.String(), vmOut.String())
	}
}

// assertImportedModuleRejected writes moduleBody as module `lib`, imports it
// from a main program, and asserts both backends fail to load it.
func assertImportedModuleRejected(t *testing.T, moduleBody string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "lib.gb"), []byte("module lib;\n"+moduleBody), 0o644); err != nil {
		t.Fatalf("write lib: %v", err)
	}
	source := "import io;\nimport lib;\nio.println(\"loaded\");\n"
	p := parser.New(lexer.New(source))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parse errors: %v", p.Errors())
	}
	var evOut bytes.Buffer
	ev := evaluator.NewWithArgsAndModulePaths(&evOut, nil, []string{dir})
	if _, err := ev.Eval(program); err == nil {
		t.Fatalf("evaluator accepted a colliding module: %q", evOut.String())
	}
	chunk, err := bytecode.Compile(program, []byte(source), "parity")
	if err != nil {
		t.Fatalf("main compile: %v", err)
	}
	var vmOut bytes.Buffer
	stateful := evaluator.NewWithArgsAndModulePaths(&vmOut, nil, []string{dir})
	loader := newHarnessLoader(&vmOut, stateful)
	loader.SetModulePaths([]string{dir})
	vm := bytecode.NewVMWithModuleLoader(chunk, &vmOut, loader)
	vm.SetModulePaths([]string{dir})
	vm.SetStatefulNativeCaller(stateful)
	if err := vm.Run(); err == nil {
		t.Fatalf("vm accepted a colliding module: %q", vmOut.String())
	}
}
