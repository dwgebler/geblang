package check

import "testing"

func TestAllowUnusedImportsTagInLeadingDocBlockSuppressesUnusedImports(t *testing.T) {
	cases := map[string]string{
		"tag alone":            "/** @allowUnusedImports */\nimport io;\nlet x = 1;\n",
		"tag beside doc tags":  "/**\n * @title Wiring\n * @allowUnusedImports\n */\nimport io;\nimport json;\nlet x = 1;\n",
		"before module header": "/** @allowUnusedImports */\nmodule wiring;\nimport io;\nexport let x = 1;\n",
	}
	for name, source := range cases {
		t.Run(name, func(t *testing.T) {
			diags := lintDiagnostics(t, source)

			for _, d := range diags {
				if d.Rule == "unused-import" {
					t.Fatalf("unused import reported despite the tag: %+v", d)
				}
			}
		})
	}
}

func TestAllowUnusedImportsTagOutsideLeadingDocBlockIsIgnored(t *testing.T) {
	cases := map[string]string{
		"after first statement": "import io;\n/** @allowUnusedImports */\nlet x = 1;\n",
		"line comment":          "# @allowUnusedImports\nimport io;\nlet x = 1;\n",
		"plain block comment":   "/* @allowUnusedImports */\nimport io;\nlet x = 1;\n",
		"longer tag name":       "/** @allowUnusedImportsEverywhere */\nimport io;\nlet x = 1;\n",
		"mentioned in prose":    "/**\n * @summary explains @allowUnusedImports\n */\nimport io;\nlet x = 1;\n",
	}
	for name, source := range cases {
		t.Run(name, func(t *testing.T) {
			diags := lintDiagnostics(t, source)

			if !hasDiag(diags, "unused-import", "import io is not used") {
				t.Fatalf("expected io reported unused, got %+v", diags)
			}
		})
	}
}

func TestAllowUnusedImportsTagLeavesOtherLintRulesActive(t *testing.T) {
	source := "/** @allowUnusedImports */\nimport io;\nfunc f(): int {\n    return 1;\n    let y = 2;\n}\n"

	diags := lintDiagnostics(t, source)

	if !hasRule(diags, "unreachable") {
		t.Fatalf("expected the unreachable warning to remain, got %+v", diags)
	}
}
