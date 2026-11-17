package native

import (
	"testing"

	"geblang/internal/runtime"
)

func TestDecompressBudgetDefaultsAndDisable(t *testing.T) {
	data := runtime.Bytes{Value: []byte("x")}
	_, budget, err := bytesWithDecompressBudget([]runtime.Value{data}, "t")
	if err != nil || budget == nil || budget.limit != 256<<20 {
		t.Fatalf("default budget = %+v, %v", budget, err)
	}
	_, budget, err = bytesWithDecompressBudget([]runtime.Value{data, runtime.NewInt64(0)}, "t")
	if err != nil || budget != nil {
		t.Fatalf("maxBytes 0 should disable the cap: %+v, %v", budget, err)
	}
	if _, _, err := bytesWithDecompressBudget([]runtime.Value{data, runtime.NewInt64(-1)}, "t"); err == nil {
		t.Fatal("negative maxBytes accepted")
	}
}
