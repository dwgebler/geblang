package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestMoneyAndInheritedBoundMethodsCachedAndBuiltBinary(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "geblang")
	if output, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("build geblang: %v\n%s", err, output)
	}
	project := t.TempDir()
	cacheHome := filepath.Join(project, "cache")
	cacheEnv := append(os.Environ(), "XDG_CACHE_HOME="+cacheHome)
	files := map[string]string{
		"geblang.yaml": "name: app\nversion: 0.0.0\n",
		"bound_fixture.gb": `module bound_fixture;
export class Bag<T> { func collect(T ...values): int { return values.length(); } }
export class Box {
 func accumulate(list<int> values = [], int n = 1): int { values.push(n); return values.length(); }
 func gather(int ...rest): int { return rest.length(); }
}
`,
		"app.gb": `module app;
import money;
import io;
import json;
import bound_fixture;
class Counter extends bound_fixture.Box {}
class Bag extends bound_fixture.Bag<int> {}
class Price extends money.Money { func Price(any value, string code) { parent(value,code); } }
export func main(): int {
 let value=Price("900719925474099312345.67","GBP");
 let multiply=value.multiply;
 io.println(multiply(factor:2));
 io.println(multiply(rounding:"halfEven",factor:1.05));
 io.println(json.parseAs(json.stringify(value),money.Money) == value);
 for (share in money.Money(10,"GBP").split(3)) { io.println(share); }
 try { value.minorUnits=0; } catch (ImmutableError e) { io.println("immutable"); }
 let count=Counter().accumulate;
 io.println(count(n:2));
 io.println(count(n:3));
 let gather=Counter().gather;
 try { gather(rest:[3,4]); } catch (TypeError e) { io.println("invalid variadic"); }
 let bag=Bag();
 let collect=bag.collect;
 io.println(collect(values:3));
 any bad="wrong";
 try { collect(values:bad); } catch (TypeError e) { io.println("generic named"); }
 try { collect(bad); } catch (TypeError e) { io.println("generic positional"); }
 try { bag.collect(bad); } catch (TypeError e) { io.println("generic direct"); }
 return 0;
}
`,
		"runner.gb": "import app;\napp.main();\n",
	}
	want := "GBP 1801439850948198624691.34\nGBP 945755921747804277962.95\ntrue\nGBP 3.34\nGBP 3.33\nGBP 3.33\nimmutable\n1\n1\ninvalid variadic\n1\ngeneric named\ngeneric positional\ngeneric direct\n"

	for name, content := range files {
		if err := os.WriteFile(filepath.Join(project, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for run, args := range [][]string{{"run", "runner.gb"}, {"run", "runner.gb"}, {"--disable-vm", "runner.gb"}} {
		cmd := exec.Command(bin, args...)
		cmd.Dir = project
		cmd.Env = cacheEnv
		output, err := cmd.CombinedOutput()
		if err != nil || string(output) != want {
			t.Fatalf("source run %d: %v, output %q", run+1, err, output)
		}
	}
	cache, err := filepath.Glob(filepath.Join(project, ".geblang-cache", "*", "*.gbc"))
	if err != nil || len(cache) == 0 {
		t.Fatalf("expected cached bytecode: %v, files %v", err, cache)
	}
	executable := filepath.Join(project, "money-app")
	build := exec.Command(bin, "build", "--entry", "app", "--out", executable, ".")
	build.Dir = project
	build.Env = cacheEnv
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build app: %v\n%s", err, output)
	}
	launch := exec.Command(executable)
	launch.Dir = t.TempDir()
	launch.Env = cacheEnv
	output, err := launch.CombinedOutput()
	if err != nil || strings.TrimSpace(string(output)) != strings.TrimSpace(want) {
		t.Fatalf("built app: %v, output %q", err, output)
	}
}
