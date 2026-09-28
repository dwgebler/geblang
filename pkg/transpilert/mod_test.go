package transpilert

import "testing"

func TestModIntFloorsTowardDivisorSign(t *testing.T) {
	cases := []struct{ a, b, want int64 }{
		{7, 3, 1}, {7, -3, -2}, {-7, 3, 2}, {-7, -3, -1}, {6, -3, 0},
		{-9223372036854775808, -1, 0},
	}
	for _, c := range cases {
		if got := ModInt(c.a, c.b); got != c.want {
			t.Errorf("ModInt(%d, %d) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestModFloatFloorsTowardDivisorSign(t *testing.T) {
	if got := ModFloat(7.5, -2); got != -0.5 {
		t.Errorf("ModFloat(7.5, -2) = %v, want -0.5", got)
	}
	if got := ModFloat(-7.5, 2); got != 0.5 {
		t.Errorf("ModFloat(-7.5, 2) = %v, want 0.5", got)
	}
}

func TestModByZeroRaisesRuntimeError(t *testing.T) {
	for name, fn := range map[string]func(){
		"modulo by zero":       func() { ModInt(1, 0) },
		"float modulo by zero": func() { ModFloat(1, 0) },
	} {
		func() {
			defer func() {
				e, ok := recover().(*Error)
				if !ok || !e.IsClass("RuntimeError") || e.Message != name {
					t.Errorf("%s: got %#v", name, e)
				}
			}()
			fn()
		}()
	}
}
