package runtime

import "testing"

func TestParameterClassNames(t *testing.T) {
	cases := []struct{ in, class, element string }{
		{"Widget", "Widget", ""},
		{"?model.Widget", "model.Widget", ""},
		{"Box<int>", "Box", ""},
		{"list<model.Widget>", "", "model.Widget"},
		{"set<?Widget>", "", "Widget"},
		{"?list<Box<string>>", "", "Box"},
		{"dict<string, Widget>", "", ""},
		{"Widget[]", "", "Widget"},
		{"?model.Widget[]", "", "model.Widget"},
		{"int[][]", "", ""},
		{"int|string", "", ""},
		{"func(int): int", "", ""},
		{"", "", ""},
	}
	for _, c := range cases {
		class, element := ParameterClassNames(c.in)
		if class != c.class || element != c.element {
			t.Errorf("ParameterClassNames(%q) = (%q, %q), want (%q, %q)", c.in, class, element, c.class, c.element)
		}
	}
}
