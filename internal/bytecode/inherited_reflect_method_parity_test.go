package bytecode_test

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParityReflectInheritedMethodsAcrossModules(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"base.gb": "module base;\n\nexport class BaseController {\n    @Route(\"/show\")\n    func show(): void {}\n\n" +
			"    @Factory(\"make\")\n    static func make(): int { return 1; }\n}\n",
		"api.gb": "module api;\nimport base;\n\nexport class Child extends base.BaseController {\n    @Route(\"/extra\")\n    func extra(): void {}\n}\n",
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	source := `import api;
import io;
import reflect;

class Local {
    @Factory("local")
    static func build(): int { return 2; }
}
class LocalChild extends Local {}

func firstArg(any target): any {
    return reflect.decorators(target)[0]["args"][0];
}

io.println(firstArg(reflect.method(api.Child, "show")));
io.println(firstArg(reflect.method(api.Child(), "show")));
io.println(firstArg(reflect.method(api.Child, "extra")));
io.println(firstArg(reflect.staticMethod(api.Child, "make")));
io.println(firstArg(reflect.staticMethod(LocalChild, "build")));
io.println(reflect.method(api.Child, "missing"));
io.println(reflect.staticMethod(LocalChild, "missing"));
`
	ev, vm := parityOutputsModulesDir(t, dir, source)
	want := "/show\n/show\n/extra\nmake\nlocal\nnull\nnull\n"
	if ev != want || vm != want {
		t.Fatalf("evaluator %q, vm %q, want %q", ev, vm, want)
	}
}
