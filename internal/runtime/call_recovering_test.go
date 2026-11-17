package runtime

import (
	"strings"
	"testing"
)

func TestCallRecoveringTurnsPanicsIntoRuntimeErrors(t *testing.T) {
	_, err := CallRecovering(func() (Value, error) { panic("boom") })
	typed, ok := err.(ClassifiedError)
	if !ok || typed.Class != "RuntimeError" || !strings.Contains(typed.Message, "internal error: boom") {
		t.Fatalf("err = %#v", err)
	}
	v, err := CallRecovering(func() (Value, error) { return String{Value: "ok"}, nil })
	if err != nil || v.(String).Value != "ok" {
		t.Fatalf("passthrough = %v, %v", v, err)
	}
}
