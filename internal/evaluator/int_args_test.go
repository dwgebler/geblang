package evaluator

import (
	"math/big"
	"testing"

	"geblang/internal/runtime"
)

// The VM passes small integers as runtime.SmallInt; native argument helpers must accept both representations.
func TestNativeIntHelpersAcceptBothIntRepresentations(t *testing.T) {
	values := []runtime.Value{runtime.SmallInt{Value: 1025}, runtime.Int{Value: big.NewInt(1025)}}
	for _, v := range values {
		if n, err := intField(v, "port"); err != nil || n != 1025 {
			t.Errorf("intField(%T) = %d, %v", v, n, err)
		}
		if n, err := runtimeInt64(v, "port"); err != nil || n != 1025 {
			t.Errorf("runtimeInt64(%T) = %d, %v", v, n, err)
		}
		if n, err := handleID(v, "x"); err != nil || n != 1025 {
			t.Errorf("handleID(%T) = %d, %v", v, n, err)
		}
		if n, err := fileHandleID(v); err != nil || n != 1025 {
			t.Errorf("fileHandleID(%T) = %d, %v", v, n, err)
		}
		if n, err := logHandleID(v); err != nil || n != 1025 {
			t.Errorf("logHandleID(%T) = %d, %v", v, n, err)
		}
		if n, err := traceHandleID(v); err != nil || n != 1025 {
			t.Errorf("traceHandleID(%T) = %d, %v", v, n, err)
		}
		if n, err := collectionsIntArg(v, "end"); err != nil || n.Int64() != 1025 {
			t.Errorf("collectionsIntArg(%T) = %v, %v", v, n, err)
		}
	}
	for _, bad := range []runtime.Value{runtime.String{Value: "1025"}, runtime.Null{}} {
		if _, err := intField(bad, "port"); err == nil {
			t.Errorf("intField(%T) accepted a non-int", bad)
		}
	}
	huge, _ := new(big.Int).SetString("99999999999999999999", 10)
	if _, err := intField(runtime.Int{Value: huge}, "port"); err == nil {
		t.Errorf("intField accepted an out-of-range int")
	}
}

func TestEnvelopeAddressStripsDisplayNames(t *testing.T) {
	cases := map[string]string{
		"App <noreply@example.com>":    "noreply@example.com",
		"noreply@example.com":          "noreply@example.com",
		"\"Ada, L\" <ada@example.com>": "ada@example.com",
	}
	for in, want := range cases {
		got, err := envelopeAddress(in)
		if err != nil || got != want {
			t.Errorf("envelopeAddress(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	if _, err := envelopeAddress("not an address"); err == nil {
		t.Errorf("envelopeAddress accepted an invalid address")
	}
}
