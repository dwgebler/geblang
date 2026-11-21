package bytecode_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"geblang/internal/bytecode"
	"geblang/internal/lexer"
	"geblang/internal/parser"
	"geblang/internal/runtime"
)

const decoratorReferenceSource = `import io;
import reflect;

interface Notifier { func send(): void; }
enum Color { Red, Green }
class User {}

func backoff(int attempt): int { return attempt * 10; }

func Retry(callable fn, callable policy): callable {
    return func(int n): int { return policy(n) + fn(n); };
}

@Meta(User, kinds: [Notifier, Color], map: {"u": User}, f: (int n) => n * 2)
class Svc {
    @Field(kind: User)
    string name;

    @Route(schema: User, retry: func(int a): int { return backoff(a); })
    func handle(@Param(User) string id): void {}
}

@Retry((int n) => n + 1)
func work(int n): int { return n; }

let d = reflect.decorators(Svc)[0];
io.println(d["args"]);
io.println(d["namedArgs"]["kinds"]);
io.println(d["namedArgs"]["map"]);
io.println((d["namedArgs"]["f"] as callable)(21));
let m = reflect.decorators(reflect.method(Svc, "handle"))[0];
io.println(m["namedArgs"]["schema"]);
io.println((m["namedArgs"]["retry"] as callable)(3));
io.println(reflect.parameters(reflect.method(Svc, "handle"))[0]["decorators"][0]["args"]);
io.println(reflect.fields(Svc)[0]["decorators"][0]["namedArgs"]["kind"]);
io.println(work(5));
io.println(d["namedArgs"]["kinds"][0] == Notifier);
`

func TestParityDecoratorTypeAndFunctionArguments(t *testing.T) {
	ev, vm := parityOutputs(t, decoratorReferenceSource)
	want := "[<class User>]\n[<interface Notifier>, <enum Color>]\n{\"u\": <class User>}\n42\n<class User>\n30\n[<class User>]\n<class User>\n11\ntrue\n"
	if ev != want || vm != want {
		t.Fatalf("evaluator %q, vm %q, want %q", ev, vm, want)
	}
}

func TestParityDecoratorTypeReferencesAsDictionaryKeys(t *testing.T) {
	source := `import io;
import reflect;
class User {}
@Meta({User: "user"})
func handle(): void {}
io.println(reflect.decorators(handle)[0]["args"][0][User]);
`
	ev, vm := parityOutputs(t, source)
	if ev != "user\n" || vm != ev {
		t.Fatalf("evaluator %q, VM %q", ev, vm)
	}
}

func TestParityDecoratorNonTypeNameArgumentIsRejected(t *testing.T) {
	source := `import io;
import reflect;

const LIMIT = 5;

@Meta(LIMIT)
func f(): void {}

try {
    reflect.decorators(f);
} catch (RuntimeError e) {
    io.println(e.message);
}
`
	ev, vm := parityOutputs(t, source)
	want := "decorator @Meta: argument LIMIT must be a literal, a type, or a function\n"
	if ev != want || vm != want {
		t.Fatalf("evaluator %q, vm %q, want %q", ev, vm, want)
	}
}

func TestParityInvalidDecoratorArgumentsReportErrorsAtReadSites(t *testing.T) {
	for _, argument := range []string{"LIMIT", "{User}"} {
		for _, target := range []struct{ declaration, read string }{
			{"@Meta(ARG) func f(): void {}", "reflect.decorators(f)"},
			{"class C { @Meta(ARG) string value; }", "reflect.fields(C)"},
			{"func f(@Meta(ARG) string text): void {}", "reflect.parameters(f)"},
			{"class C { func C(@Meta(ARG) string text) {} }", "reflect.constructors(C)"},
			{"interface C { func f(@Meta(ARG) string text): void; }", "reflect.interfaceMethods(C)"},
		} {
			t.Run(argument+"/"+target.read, func(t *testing.T) {
				source := "import io; import reflect; const LIMIT = 5; class User {}\n" +
					strings.ReplaceAll(target.declaration, "ARG", argument) + "\n" +
					"try { " + target.read + "; io.println(\"accepted\"); } catch (RuntimeError e) { io.println(e.message); }"
				ev, vm := parityOutputs(t, source)
				want := "decorator @Meta: argument LIMIT must be a literal, a type, or a function\n"
				if argument != "LIMIT" {
					want = "decorator @Meta: a set cannot hold type or function arguments\n"
				}
				if ev != want || vm != want {
					t.Fatalf("evaluator %q, VM %q, want %q", ev, vm, want)
				}
			})
		}
	}
}
func TestParityDecoratorReferenceArgumentsAcrossModules(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"models.gb": "module models;\n\nexport class User {}\nexport enum Role { Admin, Member }\nexport func scale(int n): int { return n * 100; }\n",
		"base.gb": "module base;\nimport models;\nfrom models import Role;\n\nexport class Local {}\n\n" +
			"export class BaseController {\n    @Route(schema: models.User, role: Role, local: Local, f: (int n) => models.scale(n))\n    func show(): void {}\n}\n",
		"api.gb": "module api;\nimport base;\nimport models;\n\n" +
			"export class Child extends base.BaseController {\n    @Route(schema: base.Local, list: [models.User])\n    func extra(): void {}\n}\n",
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	source := `import api;
import base;
import io;
import reflect;

func show(dict<string, any> d): void {
    let named = d["namedArgs"] as dict<string, any>;
    for (k in named.keys().sorted()) {
        let v = named[k as string];
        if (v instanceof callable) {
            io.println("${k}: ${(v as callable)(3)}");
        } else {
            io.println("${k}: ${v}");
        }
    }
}

show(reflect.decorators(reflect.method(base.BaseController, "show"))[0]);
show(reflect.decorators(reflect.method(api.Child, "extra"))[0]);
show(reflect.decorators(reflect.method(api.Child(), "show"))[0]);
`
	ev, vm := parityOutputsModulesDir(t, dir, source)
	inherited := "f: 300\nlocal: <class Local>\nrole: <enum Role>\nschema: <class User>\n"
	want := inherited + "list: [<class User>]\nschema: <class Local>\n" + inherited
	if ev != want || vm != want {
		t.Fatalf("evaluator %q, vm %q, want %q", ev, vm, want)
	}
}

func TestDecoratorReferenceArgumentsSurviveChunkEncoding(t *testing.T) {
	source := "class User {}\n@Meta(schema: User, f: (int n) => n)\nfunc f(): void {}\n"
	p := parser.New(lexer.New(source))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parse errors: %v", p.Errors())
	}
	chunk, err := bytecode.Compile(program, []byte(source), "test")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	data, err := bytecode.Encode(chunk)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	decoded, err := bytecode.Decode(data)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	thunks := 0
	for _, fn := range decoded.Functions {
		for _, dec := range fn.Decorators {
			for _, arg := range dec.NamedArgs {
				if thunk, ok := arg.(runtime.DecoratorThunk); ok {
					thunks++
					if int(thunk.Function) >= len(decoded.Functions) || decoded.Functions[thunk.Function].Entry == 0 {
						t.Fatalf("thunk %s points at an uncompiled function %d", thunk.Source, thunk.Function)
					}
				}
			}
		}
	}
	if thunks != 2 {
		t.Fatalf("decoded %d decorator thunks, want 2", thunks)
	}
}
