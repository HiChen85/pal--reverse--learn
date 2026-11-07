package palflow

import (
	"reflect"
	"testing"
)

func TestInlineAutoTriggerSharesTextAndInstructionBudget(t *testing.T) {
	code := make([]Instruction, 22)
	code[1], code[2] = ins(9, 1), ins(0)
	code[10] = ins(4, 20)
	code[20], code[21] = ins(0xffff, 0), ins(0)
	w := &WorldState{Scene: 1, Scenes: map[uint16]WorldScene{1: {EventStart: 0}, 2: {EventStart: 1}}, Events: map[uint16]WorldEvent{1: {State: 1, AutoScript: 10}}}
	for _, textLimit := range []int{5, 4} {
		r, err := New(tables{scripts: code, messages: map[uint16]string{0: "hello"}}, Options{MaxSteps: 6, MaxTextBytes: textLimit})
		if err != nil {
			t.Fatal(err)
		}
		report, err := r.Trace(Request{Entry: 1, World: w})
		if textLimit == 5 {
			if err != nil || !report.Completed || report.TextBytes != 5 || report.State.TextBytes != 5 || !reflect.DeepEqual(report.DialogueIDs, []uint16{0}) || report.State.ExecutedSteps != 4 {
				t.Fatalf("missing inline evidence/cost: %+v %v", report, err)
			}
		} else if err == nil || report.TextBytes != 0 || report.State.WorldTicks != 0 || report.State.InputIndex != 0 || !reflect.DeepEqual(report.State.World, w) {
			t.Fatalf("inline text error committed outer wait: %+v %v", report, err)
		}
	}
	r, _ := New(tables{scripts: code, messages: map[uint16]string{0: "hello"}}, Options{MaxSteps: 5})
	if report, err := r.Trace(Request{Entry: 1, World: w}); err == nil || report.Completed {
		t.Fatal("shared work budget was bypassed")
	}
}

func TestReferenceRandomDivisionTailIsAcceptedAndRecorded(t *testing.T) {
	r, _ := New(tables{scripts: []Instruction{ins(0), ins(6, 100, 3), ins(0), ins(0)}}, Options{})
	report, err := r.Trace(Request{Entry: 1, Inputs: []Input{{Kind: InputRandom, Value: 101}}})
	if err != nil || !report.Completed || report.Trace[0].NextPC != 3 || report.InputsConsumed != 1 {
		t.Fatalf("reference tail result: %+v %v", report, err)
	}
}
