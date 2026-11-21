package palflow

import (
	"math"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func tickFixture() State {
	world := worldFixture()
	world.Scenes[1] = WorldScene{EventStart: 1}
	world.Scenes[2] = WorldScene{EventStart: 4}
	world.Events[1] = WorldEvent{State: 1, AutoScript: 900} // Outside current scene.
	world.Events[2] = WorldEvent{State: 1}
	world.Events[3] = WorldEvent{State: 0, AutoScript: 901}
	world.Events[4] = WorldEvent{State: -1, AutoScript: 902}
	world.Events[5] = WorldEvent{State: 1, AutoScript: 903} // Outside current scene.
	world.FrameNum = 100
	world.ChaseCycles = 10
	world.ChaseRange = 4
	return State{World: world, Frames: []Frame{{PC: 0x1234, EventID: 2}}}
}

func runTicks(t *testing.T, state *State, frames uint16) TraceStep {
	t.Helper()
	runner := &Runner{options: Options{MaxSteps: 100000}}
	step := TraceStep{}
	handled, err := runner.evaluateTicks(state, &step, Instruction{Opcode: 9, Operands: [3]uint16{frames}}, nil)
	if !handled || err != nil {
		t.Fatalf("evaluateTicks(%d) = %v, %v", frames, handled, err)
	}
	return step
}

func TestTicksZeroWaitMeansOneAndHiddenAutosStayUnchanged(t *testing.T) {
	state := tickFixture()
	vanished := WorldEvent{State: 2, VanishTime: -15, AutoScript: 904}
	state.World.Events[4] = vanished
	original := state.World
	before := CloneWorld(original)
	step := runTicks(t, &state, 0)
	if step.Ticks != 1 || state.WorldTicks != 1 || state.World.FrameNum != 101 || state.World.ChaseCycles != 9 || state.World.ChaseRange != 4 || state.Frames[0].PC != 0x1235 {
		t.Fatalf("wait state = %+v, step = %+v", state, step)
	}
	if state.World.Events[4] != vanished || !reflect.DeepEqual(state.World.Events, original.Events) {
		t.Fatal("trigger-disabled update changed hidden/vanished events")
	}
	if !reflect.DeepEqual(original, before) || state.World == original {
		t.Fatal("wait mutated or retained the supplied world snapshot")
	}
	if step.Screen == nil || !step.Screen.ClearDialogRequested || step.Screen.Rendered || step.Screen.Kind != "frame_wait" {
		t.Fatalf("wait presentation request = %+v", step.Screen)
	}
}

func TestTicksCounterWrapAndChaseRangeThreshold(t *testing.T) {
	for _, test := range []struct {
		name                                 string
		cycles, ticks, wantCycles, wantRange uint16
	}{
		{"not yet expired", 10, 9, 1, 4},
		{"exactly expired", 10, 10, 0, 1},
		{"expired then wrapped", 10, 12, 65534, 1},
		{"zero immediately wraps", 0, 1, 65535, 4},
		{"zero never reaches zero in max wait", 0, 65535, 1, 4},
		{"maximal countdown expires", 65535, 65535, 0, 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			state := tickFixture()
			state.World.ChaseCycles = test.cycles
			state.World.FrameNum = math.MaxUint32 - 2
			step := runTicks(t, &state, test.ticks)
			wantFrame := uint32(math.MaxUint32 - 2)
			wantFrame += uint32(test.ticks)
			if state.World.ChaseCycles != test.wantCycles || state.World.ChaseRange != test.wantRange || state.World.FrameNum != wantFrame || step.Ticks != test.ticks || state.WorldTicks != int(test.ticks) {
				t.Fatalf("counter state = %+v", state.World)
			}
		})
	}
}

func TestTicksEmptySceneAndExactCombinedBudget(t *testing.T) {
	state := tickFixture()
	state.World.Scenes[2] = WorldScene{EventStart: 1}
	state.World.Events = nil
	state.ExecutedSteps, state.PlayerSubsteps, state.WorldTicks = 4, 3, 2
	runner := &Runner{options: Options{MaxSteps: 15}}
	step := TraceStep{}
	handled, err := runner.evaluateTicks(&state, &step, Instruction{Opcode: 9, Operands: [3]uint16{5}}, nil)
	if !handled || err != nil || state.WorldTicks != 7 {
		t.Fatalf("exact budget or empty scene rejected = %v, %v, %+v", handled, err, state)
	}
}

func TestTicksErrorsPreserveWholeInstruction(t *testing.T) {
	tests := []struct {
		name   string
		inst   Instruction
		alter  func(*State)
		budget int
		want   string
	}{
		{"gesture requested", Instruction{Opcode: 9, Operands: [3]uint16{1, 0, 1}}, nil, 100, "party gestures"},
		{"missing world", Instruction{Opcode: 9}, func(s *State) { s.World = nil }, 100, "explicit world"},
		{"missing frame", Instruction{Opcode: 9}, func(s *State) { s.Frames = nil }, 100, "current script frame"},
		{"missing current scene", Instruction{Opcode: 9}, func(s *State) { delete(s.World.Scenes, 1) }, 100, "current scene 1"},
		{"missing next boundary", Instruction{Opcode: 9}, func(s *State) { delete(s.World.Scenes, 2) }, 100, "next scene 2"},
		{"scene zero", Instruction{Opcode: 9}, func(s *State) { s.World.Scene = 0 }, 100, "current and next"},
		{"last PAL scene has no next slot", Instruction{Opcode: 9}, func(s *State) { s.World.Scene = 300 }, 100, "current and next"},
		{"descending boundaries", Instruction{Opcode: 9}, func(s *State) { s.World.Scenes[2] = WorldScene{EventStart: 0} }, 100, "descending"},
		{"event hole", Instruction{Opcode: 9}, func(s *State) { delete(s.World.Events, 3) }, 100, "event 3"},
		{"active auto requires source", Instruction{Opcode: 9, Operands: [3]uint16{3}}, func(s *State) { s.World.Events[2] = WorldEvent{State: 1, AutoScript: 0x4567} }, 100, "event 2 requires an instruction source"},
		{"budget includes all work", Instruction{Opcode: 9, Operands: [3]uint16{5}}, func(s *State) { s.ExecutedSteps = 4; s.PlayerSubsteps = 3; s.WorldTicks = 2 }, 14, "combined"},
		{"extreme counters do not overflow", Instruction{Opcode: 9}, func(s *State) { s.WorldTicks = math.MaxInt }, 100, "combined"},
		{"negative counters rejected", Instruction{Opcode: 9}, func(s *State) { s.PlayerSubsteps = -1 }, 100, "nonnegative"},
		{"counter overflow PC", Instruction{Opcode: 9}, func(s *State) { s.Frames[0].PC = 65535 }, 100, "program counter"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			state := tickFixture()
			if test.alter != nil {
				test.alter(&state)
			}
			before := state
			before.World = CloneWorld(state.World)
			before.Frames = slices.Clone(state.Frames)
			originalWorld := state.World
			step := TraceStep{Effect: "unchanged", Ticks: 77, Screen: &ScreenRecord{Kind: "preserved"}}
			beforeStep := step
			runner := &Runner{options: Options{MaxSteps: test.budget}}
			handled, err := runner.evaluateTicks(&state, &step, test.inst, nil)
			if !handled || err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("result = %v,%v; want %q", handled, err, test.want)
			}
			if state.World != originalWorld || !reflect.DeepEqual(state, before) || !reflect.DeepEqual(step, beforeStep) {
				t.Fatal("failed wait changed state or trace evidence")
			}
		})
	}
}

func TestTicksOnlyAdvertisesFrameWait(t *testing.T) {
	if ticksOpcodeName(9) == "" || ticksOpcodeName(8) != "" {
		t.Fatal("unexpected ticks opcode registry")
	}
	handled, err := (*Runner)(nil).evaluateTicks(nil, nil, Instruction{Opcode: 8}, nil)
	if handled || err != nil {
		t.Fatalf("unimplemented opcode handled: %v,%v", handled, err)
	}
}
