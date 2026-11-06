package palflow

import (
	"reflect"
	"strings"
	"testing"
)

func partyWalkFixture() State {
	w := &WorldState{
		Scene: 1, PartyRoles: []uint16{0}, PartyOffsetX: 0, PartyOffsetY: 0,
		ViewportX: 23, ViewportY: 13, Direction: 1,
		PartyPositions: []WorldPosition{{X: 100, Y: 200}}, PartyFrames: []uint16{17},
		Trail: []WorldTrail{{X: 10, Y: 20, Direction: 0}, {X: 11, Y: 21, Direction: 1}, {X: 12, Y: 22, Direction: 2}, {X: 13, Y: 23, Direction: 3}, {X: 14, Y: 24, Direction: 0}},
		Roles: map[uint16][75]uint16{0: {}}, Events: map[uint16]WorldEvent{},
		Scenes:   map[uint16]WorldScene{1: {EventStart: 0}, 2: {EventStart: 0}},
		FrameNum: 100, ChaseCycles: 10, ChaseRange: 4,
	}
	return State{World: w, Frames: []Frame{{PC: 0x1234, Entry: 0x1230, NextEntry: 0x1230, EventID: 2}}, CurEquipPart: -1, ScriptSuccess: true}
}

func detachPartyState(state State) State {
	out := state
	out.World = CloneWorld(state.World)
	out.Frames = append([]Frame(nil), state.Frames...)
	return out
}

func runPartyWalk(t *testing.T, state *State, source Source, limit int, operands [3]uint16, inputs []Input) (TraceStep, error) {
	t.Helper()
	r := &Runner{source: source, options: Options{MaxSteps: limit}}
	step := TraceStep{}
	handled, err := r.evaluatePartyWalk(state, &step, Instruction{Opcode: 0x70, Operands: operands}, inputs)
	if !handled {
		t.Fatal("party walk handler not selected")
	}
	return step, err
}

func TestPartyWalkClampsBothAxesShiftsTrailAndStopsStanding(t *testing.T) {
	state := partyWalkFixture()
	original := state.World
	before := detachPartyState(state)
	step, err := runPartyWalk(t, &state, nil, 100, [3]uint16{1, 1, 0}, nil)
	if err != nil {
		t.Fatal(err)
	}
	w := state.World
	if w.ViewportX != 32 || w.ViewportY != 16 || w.PartyX != 32 || w.PartyY != 16 || w.Direction != 3 || w.PartyFrames[0] != 9 || w.PartyStepFrame != 0 || w.PartyPositions[0] != (WorldPosition{}) {
		t.Fatalf("wrong final leader state: %+v", w)
	}
	if state.WorldTicks != 3 || w.FrameNum != 103 || w.ChaseCycles != 7 || state.Frames[0].PC != 0x1235 || state.Frames[0].NextEntry != 0x1230 {
		t.Fatalf("wrong internal update/PC state: %+v", state)
	}
	wantTrail := []WorldTrail{{X: 31, Y: 16, Direction: 3}, {X: 27, Y: 15, Direction: 3}, {X: 23, Y: 13, Direction: 1}, {X: 10, Y: 20, Direction: 0}, {X: 11, Y: 21, Direction: 1}}
	if !reflect.DeepEqual(w.Trail, wantTrail) {
		t.Fatalf("trail=%+v want%+v", w.Trail, wantTrail)
	}
	if !reflect.DeepEqual(original, before.World) || original == w {
		t.Fatal("walk aliases or mutates supplied world")
	}
	record := step.PartyWalk
	if record == nil || record.Speed != 2 || record.TargetMapX != 32 || record.TargetMapY != 16 || record.InitialStepFrame != 0 || record.FinalStepFrame != 0 || record.StandingFrame != 9 || len(record.Steps) != 3 || step.Ticks != 3 {
		t.Fatalf("incomplete walk evidence: %+v", record)
	}
	for i, want := range []struct {
		x, y         int
		frame, phase uint16
	}{{27, 15, 10, 1}, {31, 16, 9, 2}, {32, 16, 11, 3}} {
		got := record.Steps[i]
		if got.Sequence != i+1 || got.AfterViewportX != want.x || got.AfterViewportY != want.y || got.WalkingFrame != want.frame || got.StepFrame != want.phase || got.BeforeWorldFrame != uint32(100+i) || got.AfterWorldFrame != uint32(101+i) {
			t.Fatalf("movement%d=%+v", i+1, got)
		}
	}
}

func TestPartyWalkDirectionAndFourFrameGestures(t *testing.T) {
	for _, test := range []struct {
		name      string
		x, y      int
		direction uint16
	}{
		{"northwest", 36, 18, 1}, {"southwest", 36, 14, 0}, {"northeast", 28, 18, 2}, {"southeast", 28, 14, 3},
		{"west_axis_uses_south", 36, 16, 0}, {"north_axis", 32, 18, 2}, {"south_axis_uses_east", 32, 14, 3},
	} {
		t.Run(test.name, func(t *testing.T) {
			state := partyWalkFixture()
			state.World.ViewportX, state.World.ViewportY = test.x, test.y
			raw := state.World.Roles[0]
			raw[64] = 4
			state.World.Roles[0] = raw
			step, err := runPartyWalk(t, &state, nil, 10, [3]uint16{1, 1, 0}, nil)
			if err != nil {
				t.Fatal(err)
			}
			if len(step.PartyWalk.Steps) != 1 || state.World.Direction != test.direction || step.PartyWalk.Steps[0].WalkingFrame != test.direction*4+1 || state.World.PartyFrames[0] != test.direction*4 || state.World.PartyStepFrame != 2 {
				t.Fatalf("direction/gesture mismatch: %+v %+v", state.World, step.PartyWalk)
			}
		})
	}
}

func TestPartyWalkHalfTileAndSignedCoordinates(t *testing.T) {
	state := partyWalkFixture()
	state.World.ViewportX, state.World.ViewportY = 12, 8
	state.World.PartyStepFrame = 2
	step, err := runPartyWalk(t, &state, nil, 10, [3]uint16{0, 0, 1}, nil)
	if err != nil || len(step.PartyWalk.Steps) != 1 || state.World.ViewportX != 16 || state.World.ViewportY != 8 || step.PartyWalk.Steps[0].WalkingFrame != 11 || state.World.PartyStepFrame != 0 {
		t.Fatalf("half-tile or phase mismatch %+v %v", step.PartyWalk, err)
	}
	state = partyWalkFixture()
	state.World.ViewportX, state.World.ViewportY = -5, -3
	step, err = runPartyWalk(t, &state, nil, 10, [3]uint16{}, nil)
	if err != nil || len(step.PartyWalk.Steps) != 2 || step.PartyWalk.Steps[0].AfterViewportX != -1 || step.PartyWalk.Steps[0].AfterViewportY != -1 || step.PartyWalk.Steps[0].TrailHead.X != 65531 || step.PartyWalk.Steps[0].TrailHead.Y != 65533 || state.World.ViewportX != 0 || state.World.ViewportY != 0 {
		t.Fatalf("signed path mismatch %+v %v", step.PartyWalk, err)
	}
}

func TestPartyWalkAtTargetStillUpdatesStandingPhaseWithoutTick(t *testing.T) {
	state := partyWalkFixture()
	state.World.ViewportX, state.World.ViewportY = 32, 16
	state.World.PartyStepFrame = 1
	before := detachPartyState(state)
	for _, phase := range []uint16{2, 0} {
		step, err := runPartyWalk(t, &state, nil, 1, [3]uint16{1, 1, 0}, nil)
		if err != nil || len(step.PartyWalk.Steps) != 0 || state.World.PartyFrames[0] != 3 || state.World.PartyStepFrame != phase || state.World.FrameNum != 100 || state.WorldTicks != 0 || !reflect.DeepEqual(state.World.Trail, before.World.Trail) || !reflect.DeepEqual(state.World.PartyPositions, before.World.PartyPositions) {
			t.Fatalf("standing-only call mismatch %+v %v", step.PartyWalk, err)
		}
	}
}

func TestPartyWalkSharedBudgetAndWholeInstructionRollback(t *testing.T) {
	for _, limit := range []int{17, 18} {
		state := partyWalkFixture()
		state.ExecutedSteps, state.PlayerSubsteps, state.WorldTicks, state.AutoSubsteps = 2, 3, 4, 5
		before := detachPartyState(state)
		step, err := runPartyWalk(t, &state, nil, limit, [3]uint16{1, 1, 0}, nil)
		if limit == 18 {
			if err != nil || state.WorldTicks != 7 || state.AutoSubsteps != 5 || len(step.PartyWalk.Steps) != 3 {
				t.Fatalf("exact shared budget rejected: %+v %v", step, err)
			}
		} else if err == nil || !reflect.DeepEqual(state, before) || step.PartyWalk != nil {
			t.Fatalf("budget failure partially committed: %+v %v", state, err)
		}
	}
}

func TestPartyWalkAutoInputsAreRecordedAndFailedLaterTickRollsBack(t *testing.T) {
	source := tables{scripts: []Instruction{{}, ins(6, 0, 0)}}
	for _, count := range []int{1, 3} {
		state := partyWalkFixture()
		state.World.Scenes[2] = WorldScene{EventStart: 1}
		state.World.Events[1] = WorldEvent{State: 1, AutoScript: 1}
		before := detachPartyState(state)
		inputs := make([]Input, count)
		for i := range inputs {
			inputs[i] = Input{Kind: InputRandom, Value: 50}
		}
		step, err := runPartyWalk(t, &state, source, 7, [3]uint16{1, 1, 0}, inputs)
		if count == 1 {
			if err == nil || !strings.Contains(err.Error(), "movement 2") || !reflect.DeepEqual(state, before) || step.PartyWalk != nil {
				t.Fatalf("missing later auto input did not rollback: %+v %v", state, err)
			}
			continue
		}
		if err != nil || state.InputIndex != 3 || state.AutoSubsteps != 3 || state.WorldTicks != 3 {
			t.Fatalf("auto updates missing: %+v %v", state, err)
		}
		for i, move := range step.PartyWalk.Steps {
			if len(move.AutoSteps) != 1 || move.AutoSteps[0].Input == nil || move.AutoSteps[0].Input.Index != i || move.AutoSteps[0].FrameNum != uint32(100+i) {
				t.Fatalf("auto evidence for movement%d: %+v", i+1, move)
			}
		}
	}
}

func TestPartyWalkRefusesUnsupportedContextsAndWrappingTargets(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*State)
		a      [3]uint16
	}{
		{"multiple_members", func(s *State) { s.World.PartyRoles = []uint16{0, 1} }, [3]uint16{1, 1, 0}},
		{"followers", func(s *State) { s.World.FollowerSprites = []uint16{4} }, [3]uint16{1, 1, 0}},
		{"battle", func(s *State) { s.World.InBattle = true }, [3]uint16{1, 1, 0}},
		{"pending_scene", func(s *State) { s.World.SceneLoadPending = true }, [3]uint16{1, 1, 0}},
		{"scene_changed", func(s *State) { s.World.SceneChanged = true }, [3]uint16{1, 1, 0}},
		{"phase", func(s *State) { s.World.PartyStepFrame = 4 }, [3]uint16{1, 1, 0}},
		{"trail", func(s *State) { s.World.Trail = s.World.Trail[:4] }, [3]uint16{1, 1, 0}},
		{"missing_role", func(s *State) { delete(s.World.Roles, 0) }, [3]uint16{1, 1, 0}},
		{"invalid_viewport", func(s *State) { s.World.ViewportX = -32769 }, [3]uint16{1, 1, 0}},
		{"target_wrap", func(s *State) {}, [3]uint16{65535, 1, 0}},
		{"offset_target_wrap", func(s *State) { s.World.PartyOffsetX = -32768 }, [3]uint16{}},
		{"pc_overflow", func(s *State) { s.Frames[0].PC = 65535 }, [3]uint16{1, 1, 0}},
	} {
		t.Run(test.name, func(t *testing.T) {
			state := partyWalkFixture()
			test.change(&state)
			before := detachPartyState(state)
			step, err := runPartyWalk(t, &state, nil, 100, test.a, nil)
			if err == nil || !reflect.DeepEqual(state, before) || step.PartyWalk != nil {
				t.Fatalf("unsupported context accepted or partially committed: %+v %v", state, err)
			}
		})
	}
}
