package bytecode_test

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParityEnumTypeValuesCompareEqual(t *testing.T) {
	dir := t.TempDir()
	donor := "module colors;\n\nexport enum Color { Red, Green }\nexport func colorType(): any { return Color; }\n"
	if err := os.WriteFile(filepath.Join(dir, "colors.gb"), []byte(donor), 0o644); err != nil {
		t.Fatal(err)
	}
	source := `import colors;
import io;

enum Color { Red, Green }
enum Shade { Red, Green }

let c = Color;
io.println(c == Color);
io.println(Color is Color);
io.println(Color == Shade);
io.println(typeof(Color.Red) == Color);
io.println([Color] == [Color]);
io.println(colors.Color == colors.colorType());
io.println(colors.Color == Color);
io.println({"k": colors.Color} == {"k": colors.colorType()});
`
	ev, vm := parityOutputsModulesDir(t, dir, source)
	want := "true\ntrue\nfalse\ntrue\ntrue\ntrue\nfalse\ntrue\n"
	if ev != want || vm != want {
		t.Fatalf("evaluator %q, vm %q, want %q", ev, vm, want)
	}
}
