package bytecode_test

import (
	"io"
	"testing"

	"geblang/internal/bytecode"
	"geblang/internal/lexer"
	"geblang/internal/parser"
)

const reflectFieldsBenchSource = `import reflect;
class Base {
    @deprecated
    string id;
    int version;
    string created;
}
class Item extends Base {
    string name;
    int count;
    float price;
    ?string note;
}
func probe(): int {
    return reflect.fields(Item, inherited: true).length();
}
`

func BenchmarkReflectFieldsInherited(b *testing.B) {
	program := parser.New(lexer.New(reflectFieldsBenchSource)).ParseProgram()
	chunk, err := bytecode.Compile(program, []byte(reflectFieldsBenchSource), "bench")
	if err != nil {
		b.Fatal(err)
	}
	vm := bytecode.NewVM(chunk, io.Discard)
	if err := vm.Run(); err != nil {
		b.Fatal(err)
	}
	index := -1
	for i, fn := range chunk.Functions {
		if fn.Name == "probe" {
			index = i
		}
	}
	if index < 0 {
		b.Fatal("probe not found")
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := vm.CallFunctionFast(int64(index), nil); err != nil {
			b.Fatal(err)
		}
	}
}
