package palflow

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

type tables struct {
	scripts  []Instruction
	messages map[uint16]string
}

func (s tables) Instruction(pc uint16) (Instruction, error) {
	if int(pc) >= len(s.scripts) {
		return Instruction{}, fmt.Errorf("instruction %d out of bounds", pc)
	}
	return s.scripts[pc], nil
}
func (s tables) Message(id uint16) (string, error) {
	text, ok := s.messages[id]
	if !ok {
		return "", fmt.Errorf("message %d out of bounds", id)
	}
	return text, nil
}
func ins(op uint16, operands ...uint16) Instruction {
	out := Instruction{Opcode: op}
	copy(out.Operands[:], operands)
	return out
}
func run(t *testing.T, source Source, request Request) Report {
	t.Helper()
	runner, err := New(source, Options{})
	if err != nil {
		t.Fatal(err)
	}
	report, err := runner.Trace(request)
	if err != nil {
		t.Fatal(err)
	}
	return report
}
func pcList(report Report) []uint16 {
	var out []uint16
	for _, step := range report.Trace {
		out = append(out, step.PC)
	}
	return out
}
func TestStopReplacementAndExactMessage(t *testing.T) {
	text := "&1原样$文字~\n\x1b"
	source := tables{scripts: []Instruction{ins(0x7777), ins(8), ins(0xffff, 0), ins(0)}, messages: map[uint16]string{0: text}}
	report := run(t, source, Request{Entry: 1, EventID: 1})
	if !report.Completed || report.Status != "script_stopped" || report.State.ReturnedEntry != 2 {
		t.Fatalf("wrong stop: %+v", report)
	}
	if !reflect.DeepEqual(pcList(report), []uint16{1, 2, 3}) || !reflect.DeepEqual(report.DialogueIDs, []uint16{0}) || report.Trace[1].Message.Text != text {
		t.Fatal("message or replacement changed")
	}
	if report.Trace[1].PC != 2 || report.Trace[1].Opcode != 0xffff || report.Trace[1].Operands[0] != 0 {
		t.Fatal("instruction evidence missing")
	}
	report = run(t, tables{scripts: []Instruction{ins(0), ins(1)}}, Request{Entry: 1})
	if report.State.ReturnedEntry != 2 || len(report.Trace) != 1 {
		t.Fatal("stop1 must replace entry without executing next instruction")
	}
	report = run(t, source, Request{Entry: 0, EventID: 0xffff, LastEventID: 7})
	if len(report.Trace) != 0 || !report.Completed || report.State.LastEventID != 7 {
		t.Fatal("entry zero must stop without reading sentinel instruction")
	}
}
func TestCountedStopsAndJumps(t *testing.T) {
	source := tables{scripts: []Instruction{ins(0), ins(2, 4, 2), ins(0)}}
	initial := map[uint16]uint16{7: 0}
	first := run(t, source, Request{Entry: 1, EventID: 7, IdleFrames: initial})
	if first.State.ReturnedEntry != 4 || first.State.IdleFrames[7] != 1 || initial[7] != 0 {
		t.Fatal("counted stop did not increment isolated counter")
	}
	second := run(t, source, Request{Entry: 1, EventID: 7, IdleFrames: map[uint16]uint16{7: 1}})
	if second.State.ReturnedEntry != 1 || second.State.IdleFrames[7] != 0 || len(second.Trace) != 2 {
		t.Fatal("threshold must reset then fall through")
	}
	source.scripts[1] = ins(3, 1, 3)
	loop := run(t, source, Request{Entry: 1, EventID: 7, IdleFrames: initial})
	if !reflect.DeepEqual(pcList(loop), []uint16{1, 1, 1, 2}) || loop.State.IdleFrames[7] != 0 {
		t.Fatal("counted jump semantics differ")
	}
	source.scripts[1] = ins(2, 9, 0)
	noCounter := run(t, source, Request{Entry: 1})
	if noCounter.State.ReturnedEntry != 9 {
		t.Fatal("zero threshold must not dereference an event counter")
	}
	source.scripts[1] = ins(2, 9, 1)
	wrapped := run(t, source, Request{Entry: 1, EventID: 7, IdleFrames: map[uint16]uint16{7: 65535}})
	if wrapped.State.IdleFrames[7] != 0 || wrapped.State.ReturnedEntry != 9 {
		t.Fatal("USHORT idle counter overflow not preserved")
	}
}
func TestNestedCallsKeepEventContextAndIgnoreCalleeReturn(t *testing.T) {
	scripts := make([]Instruction, 9)
	scripts[1] = ins(4, 5, 2)
	scripts[2] = ins(4, 7, 0xffff)
	scripts[3] = ins(0)
	scripts[5] = ins(4, 8, 3)
	scripts[6] = ins(1)
	scripts[7] = ins(0xffff, 4)
	scripts[8] = ins(0)
	report := run(t, tables{scripts: scripts, messages: map[uint16]string{4: "nested"}}, Request{Entry: 1, EventID: 1})
	if !reflect.DeepEqual(pcList(report), []uint16{1, 5, 8, 6, 2, 7, 8, 3}) {
		t.Fatalf("unexpected call trace: %v", pcList(report))
	}
	wantEvents := []uint16{1, 2, 3, 2, 1, 3, 3, 1}
	for i, step := range report.Trace {
		if step.EventID != wantEvents[i] {
			t.Fatalf("event context at %d: %d", i, step.EventID)
		}
	}
	if report.State.LastEventID != 3 || report.State.ReturnedEntry != 1 || *report.Trace[3].ReturnedEntry != 7 {
		t.Fatal("callee return or last event semantics differ")
	}
}
func TestExplicitRandomConfirmAndStubbedBattle(t *testing.T) {
	for _, test := range []struct {
		name   string
		op     Instruction
		input  Input
		target uint16
	}{
		{"random_equal", ins(6, 50, 4), Input{InputRandom, 50}, 4},
		{"random_below", ins(6, 50, 4), Input{InputRandom, 49}, 2},
		{"confirm_no", ins(0xa, 4), Input{InputConfirm, 0}, 4},
		{"confirm_yes", ins(0xa, 4), Input{InputConfirm, 1}, 2},
		{"battle_won", ins(7, 1, 4, 5), Input{InputBattle, BattleWin}, 2},
		{"battle_lost", ins(7, 1, 4, 5), Input{InputBattle, BattleLost}, 4},
		{"battle_fled", ins(7, 1, 4, 5), Input{InputBattle, BattleFled}, 5},
	} {
		t.Run(test.name, func(t *testing.T) {
			scripts := make([]Instruction, 6)
			scripts[1] = test.op
			report := run(t, tables{scripts: scripts}, Request{Entry: 1, Inputs: []Input{test.input}})
			if report.Trace[1].PC != test.target || report.InputsConsumed != 1 || report.Trace[0].Input.Index != 0 {
				t.Fatal("incorrect input branch")
			}
			stubbed := test.input.Kind == InputBattle
			if report.BattleOutcomesStubbed != stubbed || (len(report.Assumptions) > 0) != stubbed {
				t.Fatal("battle coverage not explicit")
			}
		})
	}
}
func TestPresentationIsRecordedWithoutRendering(t *testing.T) {
	scripts := []Instruction{ins(0), ins(0x3b, 0x123, 0, 1), ins(0x3c, 17, 0x124, 1), ins(0x3d, 18, 0x125, 0), ins(0x3e, 0x126, 0, 1), ins(0)}
	report := run(t, tables{scripts: scripts}, Request{Entry: 1})
	for i, position := range []string{"center", "upper", "lower", "center_window"} {
		d := report.Trace[i].Dialog
		if d == nil || d.Position != position || d.Rendered || d.Color != byte(0x23+i) {
			t.Fatalf("wrong dialog record %d: %+v", i, d)
		}
	}
	if report.Trace[1].Dialog.Portrait != 17 || !report.Trace[1].Dialog.RNG || report.Trace[3].Dialog.RNG {
		t.Fatal("dialog operands misinterpreted")
	}
}
func TestBlockedInstructionDoesNotCommitStateOrInput(t *testing.T) {
	cases := []struct {
		name    string
		source  tables
		request Request
		reason  string
	}{
		{"unknown", tables{scripts: []Instruction{ins(0), ins(0x59)}}, Request{Entry: 1}, "not modeled"},
		{"wait_updates_world", tables{scripts: []Instruction{ins(0), ins(9)}}, Request{Entry: 1}, "PAL_GameUpdate"},
		{"missing_input", tables{scripts: []Instruction{ins(0), ins(6, 50, 4)}}, Request{Entry: 1}, "explicit random"},
		{"wrong_input", tables{scripts: []Instruction{ins(0), ins(6, 50, 4)}}, Request{Entry: 1, Inputs: []Input{{InputConfirm, 1}}}, "expected random"},
		{"invalid_input", tables{scripts: []Instruction{ins(0), ins(6, 50, 4)}}, Request{Entry: 1, Inputs: []Input{{InputRandom, 101}}}, "invalid random"},
		{"missing_counter", tables{scripts: []Instruction{ins(0), ins(3, 1, 2)}}, Request{Entry: 1, EventID: 7}, "must be supplied"},
		{"missing_message", tables{scripts: []Instruction{ins(0), ins(0xffff, 5)}}, Request{Entry: 1}, "message 5"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			runner, err := New(test.source, Options{})
			if err != nil {
				t.Fatal(err)
			}
			report, err := runner.Trace(test.request)
			if err == nil || report.Completed || report.Status != "blocked" || report.Blocker == nil || !strings.Contains(report.Blocker.Reason, test.reason) {
				t.Fatalf("wrong blocker: %+v %v", report, err)
			}
			if len(report.Trace) != 0 || report.State.InputIndex != 0 || report.State.ExecutedSteps != 0 || report.State.Frames[0].PC != 1 {
				t.Fatal("failed step committed state")
			}
		})
	}
	// A consumed random value must also roll back if its fallthrough PC overflows.
	scripts := make([]Instruction, 1<<16)
	scripts[0xffff] = ins(6, 101, 1)
	runner, _ := New(tables{scripts: scripts}, Options{})
	report, err := runner.Trace(Request{Entry: 0xffff, Inputs: []Input{{InputRandom, 100}}})
	if err == nil || report.InputsConsumed != 0 || report.State.Frames[0].PC != 0xffff || len(report.Trace) != 0 {
		t.Fatal("overflow consumed input or wrapped PC")
	}
}
func TestInvalidReusedEventContextIsBlocked(t *testing.T) {
	runner, err := New(tables{scripts: []Instruction{ins(0), ins(0)}}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	report, err := runner.Trace(Request{Entry: 1, EventID: 0xffff, LastEventID: 0xffff})
	if err == nil || report.Blocker.Kind != "invalid_context" || len(report.Trace) != 0 {
		t.Fatal("invalid prior context accepted")
	}
}
func TestLimitsAndUnusedInputsAreVisible(t *testing.T) {
	source := tables{scripts: []Instruction{ins(0), ins(3, 1)}}
	runner, err := New(source, Options{MaxSteps: 3})
	if err != nil {
		t.Fatal(err)
	}
	report, err := runner.Trace(Request{Entry: 1})
	if err == nil || report.State.ExecutedSteps != 3 || len(report.Trace) != 3 || report.Blocker.Kind != "step_limit" {
		t.Fatal("loop limit failed")
	}
	source.scripts[1] = ins(4, 1)
	runner, err = New(source, Options{MaxCallDepth: 1})
	if err != nil {
		t.Fatal(err)
	}
	report, err = runner.Trace(Request{Entry: 1})
	if err == nil || len(report.State.Frames) != 1 || report.State.ExecutedSteps != 0 {
		t.Fatal("call limit committed state")
	}
	report = run(t, tables{scripts: []Instruction{ins(0), ins(0)}}, Request{Entry: 1, Inputs: []Input{{InputConfirm, 1}}})
	if len(report.UnusedInputs) != 1 || report.InputsConsumed != 0 || !strings.Contains(strings.Join(report.Limitations, " "), "not an exact input replay") {
		t.Fatal("unused inputs hidden")
	}
}

func TestRepeatedMessageTextBudgetRejectsBeforeCommit(t *testing.T) {
	source := tables{scripts: []Instruction{ins(0), ins(0xffff, 0), ins(3, 1)}, messages: map[uint16]string{0: "文本"}}
	runner, err := New(source, Options{MaxSteps: 100, MaxTextBytes: len("文本")})
	if err != nil {
		t.Fatal(err)
	}
	report, err := runner.Trace(Request{Entry: 1})
	if err == nil || report.Blocker.Kind != "text_limit" || report.Blocker.PC != 1 {
		t.Fatalf("expected cumulative text blocker: %+v %v", report, err)
	}
	if report.TextBytes != len("文本") || report.State.ExecutedSteps != 2 || len(report.Trace) != 2 || report.State.Frames[0].PC != 1 || len(report.DialogueIDs) != 1 || report.InputsConsumed != 0 {
		t.Fatalf("over-budget message committed trace/state: %+v", report)
	}
	if report.Trace[0].Message.Text != "文本" || report.Trace[1].Opcode != 3 {
		t.Fatal("successful prefix was changed")
	}
	// The exact byte boundary succeeds, including UTF-8 multibyte characters.
	finite := tables{scripts: []Instruction{ins(0), ins(0xffff, 0), ins(0)}, messages: source.messages}
	runner, err = New(finite, Options{MaxTextBytes: len("文本")})
	if err != nil {
		t.Fatal(err)
	}
	report, err = runner.Trace(Request{Entry: 1})
	if err != nil || !report.Completed || report.TextBytes != len("文本") {
		t.Fatal("exact text budget did not stop normally")
	}
	for _, budget := range []int{-1, MaxAllowedTextBytes + 1} {
		if _, err := New(source, Options{MaxTextBytes: budget}); err == nil {
			t.Fatalf("invalid text budget %d accepted", budget)
		}
	}
	runner, err = New(source, Options{})
	if err != nil || runner.options.MaxTextBytes != DefaultMaxTextBytes {
		t.Fatal("default text budget missing")
	}
}
