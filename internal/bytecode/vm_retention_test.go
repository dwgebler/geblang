package bytecode_test

import (
	"io"
	"runtime"
	"strings"
	"testing"

	"geblang/internal/bytecode"
	"geblang/internal/lexer"
	"geblang/internal/parser"
	values "geblang/internal/runtime"
)

func TestResetVMReleasesCompletedCallArguments(t *testing.T) {
	source := `func consume(string payload): int { return payload.length(); }`
	p := parser.New(lexer.New(source))
	chunk, err := bytecode.Compile(p.ParseProgram(), []byte(source), "retention")
	if err != nil {
		t.Fatal(err)
	}
	vm := bytecode.NewVM(chunk, io.Discard)
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)

	func() {
		result, err := vm.CallFunctionFast(0, []values.Value{values.String{Value: strings.Repeat("x", 16<<20)}})
		if err != nil {
			t.Fatal(err)
		}
		if result.Inspect() != "16777216" {
			t.Fatalf("consumed length = %s", result.Inspect())
		}
	}()
	vm.ResetForReuse()
	runtime.GC()
	runtime.GC()
	runtime.ReadMemStats(&after)
	runtime.KeepAlive(vm)

	if retained := int64(after.HeapAlloc) - int64(before.HeapAlloc); retained > 8<<20 {
		t.Fatalf("completed call retained %d bytes", retained)
	}
}

func TestReturnReleasesPoppedOperandsAndLocals(t *testing.T) {
	source := `func build(): int {
    let big = "x".repeat(16777216);
    let joined = big + "y";
    return joined.length();
}`
	p := parser.New(lexer.New(source))
	chunk, err := bytecode.Compile(p.ParseProgram(), []byte(source), "retention")
	if err != nil {
		t.Fatal(err)
	}
	vm := bytecode.NewVM(chunk, io.Discard)
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)

	result, err := vm.CallFunctionFast(0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Inspect() != "16777217" {
		t.Fatalf("joined length = %s", result.Inspect())
	}
	runtime.GC()
	runtime.GC()
	runtime.ReadMemStats(&after)
	runtime.KeepAlive(vm)

	if retained := int64(after.HeapAlloc) - int64(before.HeapAlloc); retained > 8<<20 {
		t.Fatalf("returned call retained %d bytes", retained)
	}
}
