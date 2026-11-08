package bytecode_test

import "testing"

const unexportedTypeDonor = `module shapelib;
interface Shape {
    func area(): int;
}
enum Tint { Red, Blue }
class Box implements Shape {
    int side = 2;
    func area(): int { return this.side * this.side; }
}
class BigBox extends Box {
    func BigBox() { this.side = 5; }
}
export func box(): Box { return Box(); }
export func bigBox(): BigBox { return BigBox(); }
export func tint(): Tint { return Tint.Blue; }
`

const unexportedTypeFacade = `module shapefacade;
import shapelib;
export type Box = shapelib.Box;
export func describe(shapelib.Box b): string { return "box ${b.area()}"; }
`

// A qualified type naming an unexported class, interface or enum runs on both backends.
func TestParityQualifiedUnexportedTypes(t *testing.T) {
	runMultiModuleParity(t, map[string]string{"shapelib": unexportedTypeDonor},
		"import io;\nimport shapelib;\n"+
			"func show(shapelib.Box b, shapelib.Shape s, shapelib.Tint t): void { io.println(\"${b.area()} ${s.area()} ${t}\"); }\n"+
			"show(shapelib.box(), shapelib.bigBox(), shapelib.tint());\n"+
			"shapelib.Box big = shapelib.bigBox();\nio.println(big.area());\n",
		"4 25 Tint.Blue\n25\n")
}

// A module that re-exports an unexported type through a type alias loads on both backends.
func TestParityTypeAliasOfUnexportedType(t *testing.T) {
	runMultiModuleParity(t, map[string]string{"shapelib": unexportedTypeDonor, "shapefacade": unexportedTypeFacade},
		"import io;\nimport shapelib;\nimport shapefacade;\n"+
			"func show(shapefacade.Box b): void { io.println(shapefacade.describe(b)); }\n"+
			"show(shapelib.bigBox());\n",
		"box 25\n")
}
