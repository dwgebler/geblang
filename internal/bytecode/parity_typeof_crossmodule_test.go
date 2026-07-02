package bytecode_test

import "testing"

// typeofDonor returns plain values typed `any` across a module boundary.
const typeofDonor = "module bmod;\nimport bytes;\n" +
	"export func makeBytes(): any { return bytes.fromHex(\"fffe\"); }\n" +
	"export func makeList(): any { return [1, 2, 3]; }\n" +
	"export func makeString(): any { return \"hi\"; }\n"

// typeof(x) == "name" holds on both backends for cross- and same-module values.
func TestParityCrossModuleTypeofEquality(t *testing.T) {
	main := "import io;\nimport bytes;\nimport bmod;\n" +
		"let cv = bmod.makeBytes();\n" +
		"let sv = bytes.fromHex(\"fffe\");\n" +
		"let a = typeof(cv) == \"bytes\";\n" +
		"let b = typeof(sv) == \"bytes\";\n" +
		"io.println(a);\n" +
		"io.println(b);\n" +
		"io.println((cv as bytes).length());\n" +
		"io.println(typeof(bmod.makeList()) == \"list\");\n" +
		"io.println(typeof(bmod.makeString()) == \"string\");\n"
	runMultiModuleParity(t, map[string]string{"bmod": typeofDonor}, main, "true\ntrue\n2\ntrue\ntrue\n")
}

// Guards runtime.ValuesEqual (shared assertEquals path) for Type-vs-String.
func TestParityCrossModuleTypeofAssertEquals(t *testing.T) {
	main := "import io;\nimport test;\nimport bmod;\n" +
		"class TypeofAssertTest extends test.Test {\n" +
		"    @test func crossBytes(): void {\n" +
		"        let v = bmod.makeBytes();\n" +
		"        this.assertEquals(\"bytes\", typeof(v));\n" +
		"        this.assertEquals(typeof(v), \"bytes\");\n" +
		"        this.assertEquals(2, (v as bytes).length());\n" +
		"    }\n" +
		"    @test func crossList(): void {\n" +
		"        this.assertEquals(\"list\", typeof(bmod.makeList()));\n" +
		"    }\n" +
		"    @test func crossString(): void {\n" +
		"        this.assertEquals(\"string\", typeof(bmod.makeString()));\n" +
		"    }\n" +
		"}\n" +
		"let inst = TypeofAssertTest();\n" +
		"inst.crossBytes();\n" +
		"inst.crossList();\n" +
		"inst.crossString();\n" +
		"io.println(\"ok\");\n"
	runMultiModuleParity(t, map[string]string{"bmod": typeofDonor}, main, "ok\n")
}

// A class value and typeof(instance) of the same name compare equal via both == and assertEquals, on both backends.
func TestParityAssertEqualsClassVsTypeof(t *testing.T) {
	src := "import io;\nimport test;\n" +
		"class Widget {}\n" +
		"class Gadget {}\n" +
		"class ClassTypeofTest extends test.Test {\n" +
		"    @test func classEqualsTypeof(): void {\n" +
		"        let w = Widget();\n" +
		"        this.assertEquals(Widget, typeof(w));\n" +
		"        this.assertEquals(typeof(w), Widget);\n" +
		"        this.assertNotEquals(Gadget, typeof(w));\n" +
		"        this.assertTrue(Widget == typeof(w));\n" +
		"        this.assertFalse(Gadget == typeof(w));\n" +
		"    }\n" +
		"}\n" +
		"let inst = ClassTypeofTest();\n" +
		"inst.classEqualsTypeof();\n" +
		"io.println(\"ok\");\n"
	runParity(t, src, "ok\n")
}
