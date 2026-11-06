package palflow

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
)

type autoSource map[uint16]Instruction

func (s autoSource) Instruction(pc uint16) (Instruction, error) {
	inst, ok := s[pc]
	if !ok {
		return Instruction{}, fmt.Errorf("instruction %04x absent", pc)
	}
	return inst, nil
}
func (s autoSource) Message(id uint16) (string, error) {
	return "", fmt.Errorf("unexpected message lookup %d", id)
}

func autoFixture(source autoSource) (State, *Runner) {
	state := tickFixture()
	state.World.Scenes[2] = WorldScene{EventStart: 2}
	state.World.Events[2] = WorldEvent{State: 1, AutoScript: 1, SpriteFrames: 3}
	state.World.AutoMessageMode = "dos"
	return state, &Runner{source: source, options: Options{MaxSteps: 100000, MaxCallDepth: 32, MaxTextBytes: DefaultMaxTextBytes}}
}

func waitAuto(t *testing.T, r *Runner, state *State, ticks uint16, inputs ...Input) TraceStep {
	t.Helper()
	step := TraceStep{}
	handled, err := r.evaluateTicks(state, &step, ins(9, ticks), inputs)
	if !handled || err != nil {
		t.Fatalf("auto wait: %v,%v", handled, err)
	}
	return step
}

func TestAutoOpeningWalkActivateThenHide(t *testing.T) {
	state, r := autoFixture(autoSource{
		0x4455: ins(0x10, 36, 24, 0), 0x4456: ins(0x49, 4, 1),
		0x4457: ins(0x49, 0xffff, 0xffff), 0x4458: ins(0),
	})
	state.World.Scenes[1] = WorldScene{EventStart: 0}
	state.World.Scenes[2] = WorldScene{EventStart: 11}
	for id := uint16(1); id <= 11; id++ {
		state.World.Events[id] = WorldEvent{}
	}
	state.World.Events[11] = WorldEvent{State: 1, AutoScript: 0x4455, X: 1134, Y: 375, Frame: 3, SpriteFrames: 3}
	original := CloneWorld(state.World)
	step := waitAuto(t, r, &state, 8)
	event := state.World.Events[11]
	if event.X != 1152 || event.Y != 384 || event.Frame != 0 || event.State != -1 || event.AutoScript != 0x4458 || state.World.Events[4].State != 1 {
		t.Fatalf("opening auto state = %+v", event)
	}
	wantPC := []uint16{0x4455, 0x4455, 0x4455, 0x4456, 0x4457}
	var gotPC []uint16
	for i, child := range step.AutoSteps {
		gotPC = append(gotPC, child.PC)
		if child.Tick != uint16(i+1) || child.FrameNum != 100+uint32(i) || child.EventID != 11 {
			t.Fatalf("wrong event/tick evidence: %+v", child)
		}
	}
	if !slices.Equal(gotPC, wantPC) || state.AutoSubsteps != 8 || state.WorldTicks != 8 || state.World.FrameNum != 108 {
		t.Fatalf("opening scheduling = %v, %+v", gotPC, state)
	}
	if original.Events[11].State != 1 || original.Events[11].X != 1134 {
		t.Fatal("snapshot fixture unexpectedly changed")
	}
}

func TestAutoStopsCountedJumpAndSharedIdleCounter(t *testing.T) {
	tests := []struct {
		name                                    string
		source                                  autoSource
		ticks, initialIdle, wantEntry, wantIdle uint16
		pcs                                     []uint16
		tickNumbers                             []uint16
	}{
		{"zero retains nonzero entry", autoSource{1: ins(0)}, 2, 0, 1, 0, []uint16{1, 1}, []uint16{1, 2}},
		{"one advances only next tick", autoSource{1: ins(1), 2: ins(0)}, 2, 0, 2, 0, []uint16{1, 2}, []uint16{1, 2}},
		{"two returns jump for next tick", autoSource{1: ins(2, 3, 3), 2: ins(0), 3: ins(2, 1, 0)}, 4, 1, 2, 0, []uint16{1, 3, 1, 2}, []uint16{1, 2, 3, 4}},
		{"three jumps within tick sharing counter", autoSource{1: ins(3, 3, 2), 3: ins(9, 2), 4: ins(0)}, 2, 0, 4, 0, []uint16{1, 3, 4}, []uint16{1, 1, 2}},
		{"three threshold exits this tick", autoSource{1: ins(3, 3, 1)}, 1, 0, 2, 0, []uint16{1}, []uint16{1}},
		{"auto jump may execute table index zero", autoSource{0: ins(0), 1: ins(3, 0)}, 2, 0, 0, 0, []uint16{1, 0}, []uint16{1, 1}},
		{"count increment wraps before comparison", autoSource{1: ins(2, 3, 2)}, 1, 65535, 3, 0, []uint16{1}, []uint16{1}},
		{"auto wait threshold", autoSource{1: ins(9, 3)}, 3, 0, 2, 0, []uint16{1, 1, 1}, []uint16{1, 2, 3}},
		{"auto wait zero advances even after wrap", autoSource{1: ins(9, 0)}, 1, 65535, 2, 0, []uint16{1}, []uint16{1}},
		{"auto wait wrap may retain entry", autoSource{1: ins(9, 1)}, 1, 65535, 1, 0, []uint16{1}, []uint16{1}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			state, r := autoFixture(test.source)
			event := state.World.Events[2]
			event.AutoIdleFrame = test.initialIdle
			state.World.Events[2] = event
			step := waitAuto(t, r, &state, test.ticks)
			event = state.World.Events[2]
			var pcs, tickNumbers []uint16
			for _, child := range step.AutoSteps {
				pcs = append(pcs, child.PC)
				tickNumbers = append(tickNumbers, child.Tick)
			}
			if event.AutoScript != test.wantEntry || event.AutoIdleFrame != test.wantIdle || !slices.Equal(pcs, test.pcs) || !slices.Equal(tickNumbers, test.tickNumbers) {
				t.Fatalf("state=%+v pcs=%v ticks=%v", event, pcs, tickNumbers)
			}
		})
	}
}

func TestAutoRandomUsesExplicitInputsAndExactBoundary(t *testing.T) {
	for _, test := range []struct {
		name   string
		target uint16
		value  int
		entry  uint16
		work   int
	}{
		{"threshold equality jumps now", 3, 50, 3, 2}, {"below threshold advances next tick", 3, 49, 2, 1}, {"zero target retains entry", 0, 100, 1, 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			state, r := autoFixture(autoSource{1: ins(6, 50, test.target), 3: ins(0)})
			step := waitAuto(t, r, &state, 1, Input{Kind: InputRandom, Value: test.value})
			if state.World.Events[2].AutoScript != test.entry || state.InputIndex != 1 || state.AutoSubsteps != test.work || step.AutoSteps[0].Input == nil || step.AutoSteps[0].Input.Index != 0 {
				t.Fatalf("random scheduling = %+v, %+v", state, step)
			}
		})
	}
}

func TestAutoSchedulingUsesEventOrderAndCurrentState(t *testing.T) {
	state, r := autoFixture(autoSource{1: ins(0x49, 4, 1), 2: ins(0x49, 2, 0xffff), 3: ins(0)})
	state.World.Scenes[2] = WorldScene{EventStart: 4}
	state.World.Events[3] = WorldEvent{State: 1, AutoScript: 2}
	state.World.Events[4] = WorldEvent{State: 0, AutoScript: 3}
	step := waitAuto(t, r, &state, 2)
	var ids []uint16
	for _, child := range step.AutoSteps {
		ids = append(ids, child.EventID)
	}
	if !slices.Equal(ids, []uint16{2, 3, 4, 3, 4}) || state.World.Events[2].State != -1 {
		t.Fatalf("event order = %v", ids)
	}
	// An event activated after its turn waits until the next frame.
	state, r = autoFixture(autoSource{1: ins(0), 2: ins(0x49, 2, 1), 3: ins(0)})
	state.World.Scenes[2] = WorldScene{EventStart: 3}
	state.World.Events[2] = WorldEvent{State: 0, AutoScript: 1}
	state.World.Events[3] = WorldEvent{State: 1, AutoScript: 2}
	step = waitAuto(t, r, &state, 2)
	ids = nil
	for _, child := range step.AutoSteps {
		ids = append(ids, child.EventID)
	}
	if !slices.Equal(ids, []uint16{3, 2, 3}) {
		t.Fatalf("earlier event activation = %v", ids)
	}
}

func TestAutoSceneChangeFlagStopsUpdateBeforeFrameCounters(t *testing.T) {
	state, r := autoFixture(autoSource{1: ins(0)})
	state.World.SceneChanged = true
	state.World.Scenes[2] = WorldScene{EventStart: 3}
	state.World.Events[3] = WorldEvent{State: 1, AutoScript: 1}
	step := waitAuto(t, r, &state, 2)
	if state.World.FrameNum != 100 || state.World.ChaseCycles != 10 || len(step.AutoSteps) != 2 || step.AutoSteps[0].EventID != 2 || step.AutoSteps[1].EventID != 2 || state.WorldTicks != 2 {
		t.Fatalf("scene flag early return = %+v, %+v", state, step)
	}
	state, r = autoFixture(nil)
	state.World.SceneChanged = true
	event := state.World.Events[2]
	event.State = 0
	state.World.Events[2] = event
	waitAuto(t, r, &state, 2)
	if state.World.FrameNum != 102 || state.World.ChaseCycles != 8 {
		t.Fatal("scene flag alone skipped frame counters without an auto invocation")
	}
}

func TestAutoDOSMessageSkipsLookupAndOtherModesBlock(t *testing.T) {
	state, r := autoFixture(autoSource{1: ins(0xffff, 999)})
	step := waitAuto(t, r, &state, 1)
	if state.World.Events[2].AutoScript != 2 || len(step.AutoSteps) != 1 || step.AutoSteps[0].Input != nil || step.Message != nil {
		t.Fatal("DOS auto message did not just advance")
	}
	for _, mode := range []string{"", "win95", "unknown"} {
		state, r = autoFixture(autoSource{1: ins(0xffff, 999)})
		state.World.AutoMessageMode = mode
		assertAutoFailure(t, r, &state, 1, nil, "explicit dos auto-message mode")
	}
}

func assertAutoFailure(t *testing.T, r *Runner, state *State, ticks uint16, inputs []Input, want string) {
	t.Helper()
	before := *state
	before.World = CloneWorld(state.World)
	before.Frames = slices.Clone(state.Frames)
	originalWorld := state.World
	step := TraceStep{Effect: "preserved", AutoSteps: []AutoStep{{PC: 77}}}
	beforeStep := step
	handled, err := r.evaluateTicks(state, &step, ins(9, ticks), inputs)
	if !handled || err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("failure = %v,%v; want %q", handled, err, want)
	}
	if state.World != originalWorld || !reflect.DeepEqual(*state, before) || !reflect.DeepEqual(step, beforeStep) {
		t.Fatal("failed auto wait committed world/input/budget/trace prefix")
	}
}

func TestAutoFailureRollsBackWholeWaitIncludingRandomAndEarlierTicks(t *testing.T) {
	state, r := autoFixture(autoSource{1: ins(0x49, 3, 1), 2: ins(6, 50, 4), 3: ins(0x7777)})
	assertAutoFailure(t, r, &state, 3, []Input{{Kind: InputRandom, Value: 49}}, "event 2 PC 0003 opcode 7777")
	for _, test := range []struct {
		name   string
		source autoSource
		inputs []Input
		want   string
	}{
		{"random missing", autoSource{1: ins(6, 50, 3)}, nil, "explicit random input"},
		{"random wrong kind", autoSource{1: ins(6, 50, 3)}, []Input{{Kind: InputConfirm, Value: 1}}, "expected random"},
		{"random invalid value", autoSource{1: ins(6, 50, 3)}, []Input{{Kind: InputRandom, Value: 102}}, "invalid random value"},
		{"source missing", autoSource{}, nil, "instruction 0001 absent"},
		{"missing trigger subroutine source", autoSource{1: ins(4, 2)}, nil, "instruction 0002 absent"},
		{"missing target", autoSource{1: ins(0x49, 99, 1)}, nil, "target event 99"},
		{"special target", autoSource{1: ins(0x49, 0x9002, 1)}, nil, "special event addressing"},
	} {
		t.Run(test.name, func(t *testing.T) {
			state, r := autoFixture(test.source)
			assertAutoFailure(t, r, &state, 1, test.inputs, test.want)
		})
	}
}

func TestAutoBudgetCountsMovementAndBoundsSameTickLoops(t *testing.T) {
	for _, budget := range []int{13, 14} {
		state, r := autoFixture(autoSource{1: ins(0x10, 1, 1)})
		state.ExecutedSteps, state.PlayerSubsteps, state.WorldTicks, state.AutoSubsteps = 4, 3, 2, 1
		r.options.MaxSteps = budget
		if budget == 13 {
			assertAutoFailure(t, r, &state, 1, nil, "combined execution budget")
			continue
		}
		step := waitAuto(t, r, &state, 1)
		if state.AutoSubsteps != 3 || step.AutoSteps[0].Work != 2 || state.World.Events[2].X != 6 || state.World.Events[2].Y != 3 {
			t.Fatalf("movement budget = %+v, %+v", state, step)
		}
	}
	state, r := autoFixture(autoSource{1: ins(3, 1)})
	r.options.MaxSteps = 20
	assertAutoFailure(t, r, &state, 1, nil, "combined execution budget")
}

func TestAutoNPCMovementMatchesSnapAndAnimationRules(t *testing.T) {
	for _, test := range []struct {
		name                   string
		event                  WorldEvent
		target                 [3]uint16
		x, y, frame, direction uint16
		arrived                bool
	}{
		{"three sprite frames use four walk gestures", WorldEvent{X: 10, Y: 10, Frame: 3, SpriteFrames: 3}, [3]uint16{10, 10}, 16, 13, 0, 3, false},
		{"auto sprite frames fallback", WorldEvent{X: 10, Y: 10, Frame: 1, SpriteFramesAuto: 2}, [3]uint16{10, 10}, 16, 13, 0, 3, false},
		{"normal frames take precedence", WorldEvent{X: 10, Y: 10, Frame: 1, SpriteFrames: 2, SpriteFramesAuto: 5}, [3]uint16{10, 10}, 16, 13, 0, 3, false},
		{"no frame metadata retains gesture", WorldEvent{X: 10, Y: 10, Frame: 7}, [3]uint16{10, 10}, 16, 13, 7, 3, false},
		{"frame counter wraps before modulus", WorldEvent{X: 10, Y: 10, Frame: 65535, SpriteFrames: 7}, [3]uint16{10, 10}, 16, 13, 0, 3, false},
		{"either close axis snaps both coordinates", WorldEvent{X: 315, Y: 0, Frame: 7}, [3]uint16{10, 10}, 320, 160, 0, 3, true},
		{"west", WorldEvent{X: 110, Y: 60}, [3]uint16{3, 3}, 104, 57, 0, 1, false},
		{"north", WorldEvent{X: 80, Y: 60}, [3]uint16{3, 3}, 86, 57, 0, 2, false},
		{"south", WorldEvent{X: 110, Y: 30}, [3]uint16{3, 3}, 104, 33, 0, 0, false},
		{"already at target resets gesture and direction", WorldEvent{X: 96, Y: 48, Frame: 7, Direction: 1}, [3]uint16{3, 3}, 96, 48, 0, 3, true},
		{"coordinate wrap does not truncate target comparison", WorldEvent{X: 65530, Y: 65530}, [3]uint16{2048, 4096}, 0, 65533, 0, 3, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			event := test.event
			arrived := walkAutoNPC(&event, test.target)
			if event.X != test.x || event.Y != test.y || event.Frame != test.frame || event.Direction != test.direction || arrived != test.arrived {
				t.Fatalf("walk = %+v, arrived %v", event, arrived)
			}
		})
	}
}
