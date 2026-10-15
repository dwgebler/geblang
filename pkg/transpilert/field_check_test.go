package transpilert

import "testing"

func TestExpectFieldErrorsAreCatchableAsError(t *testing.T) {
	defer func() {
		err, ok := recover().(*Error)
		if !ok || err.Class != "TypeError" || len(err.Parents) == 0 || err.Parents[0] != "Error" {
			t.Fatalf("expected TypeError with Error parent, got %#v", err)
		}
		if err.Message != "cannot assign string to int field L.count" {
			t.Fatalf("message %q", err.Message)
		}
	}()
	ExpectFieldIntFast("x", "L.count")
}
