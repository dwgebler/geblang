package bytecode

import "testing"

func TestTypeAssertOperandsShiftAsConstants(t *testing.T) {
	instruction := Instruction{Op: OpTypeAssert, Operands: []int64{3, 4}}
	shiftInstructionOperands(&instruction, 0, 10)
	if instruction.Operands[0] != 13 || instruction.Operands[1] != 14 {
		t.Fatalf("operands not shifted as constants: %v", instruction.Operands)
	}
}

func TestPrimMethodSlotOperandsShiftOnlyName(t *testing.T) {
	for _, op := range []Op{OpPrimMethodGlobal, OpPrimMethodLocal} {
		instruction := Instruction{Op: op, Operands: []int64{2, 5, 1, primMethodGet}}
		shiftInstructionOperands(&instruction, 0, 10)
		if got := instruction.Operands; got[0] != 2 || got[1] != 15 || got[2] != 1 || got[3] != primMethodGet {
			t.Fatalf("op %d: operands %v, want slot and argc unshifted and name shifted", op, got)
		}
	}
}
