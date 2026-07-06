package bytecode_test

import "testing"

// A serveFile marker (NativeObject with an uncomparable Dict payload) compares by descriptor via == and assertEquals on both backends, without panicking (findings 2.6 + 6.2).
func TestParityNativeObjectMarkerEquality(t *testing.T) {
	src := "import io;\nimport web;\nimport test;\n" +
		"let a = web.serveFileMarker({\"path\": \"/tmp/x\"});\n" +
		"let b = web.serveFileMarker({\"path\": \"/tmp/x\"});\n" +
		"let c = web.serveFileMarker({\"path\": \"/tmp/y\"});\n" +
		"io.println(a == b);\n" +
		"io.println(a == c);\n" +
		"class MarkerEqTest extends test.Test {\n" +
		"    @test func eq(): void {\n" +
		"        this.assertEquals(a, b);\n" +
		"        this.assertNotEquals(a, c);\n" +
		"    }\n" +
		"}\n" +
		"let inst = MarkerEqTest();\n" +
		"inst.eq();\n" +
		"io.println(\"ok\");\n"
	runParityStateful(t, src, "true\nfalse\nok\n")
}

// Complex values compare identically via == and assertEquals on both backends.
func TestParityComplexEquality(t *testing.T) {
	src := "import io;\nimport complex;\nimport test;\n" +
		"let z = complex.of(1.0, 2.0);\n" +
		"let w = complex.of(1.0, 2.0);\n" +
		"let d = complex.of(1.0, 3.0);\n" +
		"io.println(z == w);\n" +
		"io.println(z == d);\n" +
		"class CxEqTest extends test.Test {\n" +
		"    @test func eq(): void {\n" +
		"        this.assertEquals(complex.of(1.0, 2.0), complex.of(1.0, 2.0));\n" +
		"        this.assertNotEquals(complex.of(1.0, 2.0), complex.of(1.0, 3.0));\n" +
		"    }\n" +
		"}\n" +
		"let inst = CxEqTest();\n" +
		"inst.eq();\n" +
		"io.println(\"ok\");\n"
	runParity(t, src, "true\nfalse\nok\n")
}

// Type-vs-Class is name-only on both backends (same-named cross-module classes equal via ==/assertEquals) while class-vs-class stays module-aware (finding 2.5 pinned).
func TestParityCrossModuleTypeVsClassNameOnly(t *testing.T) {
	donor := "module cfgdonor;\n" +
		"export class Config {}\n" +
		"export func makeConfig(): any { return Config(); }\n" +
		"export func configClass(): any { return Config; }\n"
	main := "import io;\nimport test;\nimport cfgdonor;\n" +
		"class Config {}\n" +
		"let localInst = Config();\n" +
		"let remoteInst = cfgdonor.makeConfig();\n" +
		"let remoteClass = cfgdonor.configClass();\n" +
		"io.println(Config == typeof(remoteInst));\n" +
		"io.println(remoteClass == typeof(localInst));\n" +
		"io.println(Config == remoteClass);\n" +
		"class TypeClassModuleTest extends test.Test {\n" +
		"    @test func nameOnly(): void {\n" +
		"        this.assertEquals(Config, typeof(remoteInst));\n" +
		"        this.assertEquals(remoteClass, typeof(localInst));\n" +
		"    }\n" +
		"}\n" +
		"let inst = TypeClassModuleTest();\n" +
		"inst.nameOnly();\n" +
		"io.println(\"ok\");\n"
	runMultiModuleParity(t, map[string]string{"cfgdonor": donor}, main, "true\ntrue\nfalse\nok\n")
}
