package bytecode_test

import "testing"

// A typed throw from a cross-module inherited __deserialize factory is caught with the same class and message on both backends.
func TestParityInheritedDeserializeTypedThrow(t *testing.T) {
	donor := "module inheritfactory;\n" +
		"export class Base {\n" +
		"    int amount;\n" +
		"    func Base(int amount) { this.amount = amount; }\n" +
		"    static func __deserialize(dict<string, any> d): Base {\n" +
		"        let a = d[\"amount\"] as int;\n" +
		"        if (a < 0) { throw ValueError(\"amount must be non-negative\"); }\n" +
		"        return Base(a);\n" +
		"    }\n" +
		"}\n"
	main := "import io;\nimport json;\nimport inheritfactory;\n" +
		"class Wallet extends inheritfactory.Base {\n" +
		"    func Wallet(int a) { parent(a); }\n" +
		"}\n" +
		"try {\n" +
		"    let w = json.parseAs(\"{\\\"amount\\\": -5}\", Wallet);\n" +
		"    io.println(\"no fault\");\n" +
		"} catch (ValueError e) {\n" +
		"    io.println(\"caught ${e}\");\n" +
		"}\n"
	runMultiModuleParity(t, map[string]string{"inheritfactory": donor}, main, "caught ValueError: amount must be non-negative\n")
}
