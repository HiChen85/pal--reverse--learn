package palflow

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func applyWorldExtra(t *testing.T, state *State, source Source, inst Instruction) TraceStep {
	t.Helper()
	step := TraceStep{}
	r := &Runner{source: source}
	handled, err := r.evaluateWorldExtra(state, &step, inst, nil)
	if !handled || err != nil {
		t.Fatalf("world extra %04x: handled=%v err=%v", inst.Opcode, handled, err)
	}
	if step.Effect == "" {
		t.Fatal("successful instruction has no effect evidence")
	}
	return step
}

func TestWorldExtraChaseConfigurationDoesNotAdvanceCycles(t *testing.T) {
	for _, tc := range []struct {
		op, cycles, distance uint16
	}{{0x62, 0, 0}, {0x62, 65535, 0}, {0x63, 65535, 3}} {
		state := partyWalkFixture()
		original := state.World
		applyWorldExtra(t, &state, nil, ins(tc.op, tc.cycles))
		if state.World.ChaseCycles != tc.cycles || state.World.ChaseRange != tc.distance || state.World.FrameNum != original.FrameNum || state.WorldTicks != 0 || state.AutoSubsteps != 0 {
			t.Fatalf("configuration advanced world or lost WORD cycles: %+v", state)
		}
		if original.ChaseCycles != 10 || state.Frames[0].PC != 0x1235 {
			t.Fatal("configuration mutated request snapshot or failed to advance")
		}
	}
}

func TestWorldExtraEventAddressingSignedOffsetsAndAnimationWrap(t *testing.T) {
	for _, selector := range []uint16{0, 0xffff, 2} {
		w := worldFixture()
		w.Events[1] = WorldEvent{X: 99, Y: 98}
		w.Events[2] = WorldEvent{X: 2, Y: 65534, Direction: 1, Frame: 3, SpriteFrames: 3}
		state := State{World: w, Frames: []Frame{{PC: 10, EventID: 2}}}
		applyWorldExtra(t, &state, nil, ins(0x7d, selector, 65533, 3))
		got := state.World.Events[2]
		if got.X != 65535 || got.Y != 1 || got.Frame != 3 || got.Direction != 1 || state.World.Events[1] != w.Events[1] || w.Events[2].X != 2 {
			t.Fatalf("selector %04x movement or alias mismatch: %+v", selector, got)
		}
		applyWorldExtra(t, &state, nil, ins(0x7e, selector, 65534, 99))
		if state.World.Events[2].Layer != -2 || state.World.Events[2].Y != 1 {
			t.Fatalf("signed layer or ignored operand mismatch: %+v", state.World.Events[2])
		}
		applyWorldExtra(t, &state, nil, ins(0x87, selector, 100, 100))
		got = state.World.Events[2]
		if got.Frame != 0 || got.X != 65535 || got.Y != 1 || got.Direction != 1 || got.Layer != -2 {
			t.Fatalf("animation moved event or failed three-frame cycle: %+v", got)
		}
	}
	for _, tc := range []struct{ frames, auto, initial, want uint16 }{{0, 5, 3, 4}, {0, 0, 9, 9}, {4, 0, 65535, 0}} {
		w := worldFixture()
		w.Events[2] = WorldEvent{Frame: tc.initial, SpriteFrames: tc.frames, SpriteFramesAuto: tc.auto}
		state := State{World: w, Frames: []Frame{{PC: 10, EventID: 2}}}
		applyWorldExtra(t, &state, nil, ins(0x87))
		if state.World.Events[2].Frame != tc.want {
			t.Fatalf("animation count %+v: %+v", tc, state.World.Events[2])
		}
	}
}

func TestWorldExtraPartyOffsetWrapsViewportAndRecordsPriorAnchor(t *testing.T) {
	state := partyWalkFixture()
	w := state.World
	w.ViewportX, w.ViewportY = 32767, -32768
	w.Direction = 2
	w.Roles[0] = [75]uint16{64: 4}
	input, original := w, CloneWorld(w)
	applyWorldExtra(t, &state, nil, ins(0x6e, 1, 65535, 8191))
	w = state.World
	if w.ViewportX != -32768 || w.ViewportY != 32767 || w.Layer != -8 || w.PartyX != -32768 || w.PartyY != 32767 || w.PartyFrames[0] != 9 || w.PartyStepFrame != 1 {
		t.Fatalf("signed viewport/layer/gesture mismatch: %+v", w)
	}
	if w.Trail[0] != (WorldTrail{X: 32767, Y: 32768, Direction: 2}) || !reflect.DeepEqual(w.Trail[1:5], original.Trail[:4]) || w.FrameNum != original.FrameNum || state.WorldTicks != 0 {
		t.Fatalf("party offset moved time or recorded the wrong trail: %+v", w)
	}
	if !reflect.DeepEqual(input, original) {
		t.Fatal("party offset changed the caller's world snapshot")
	}
}

func TestWorldExtraZeroPartyOffsetStillShiftsTrailWithoutGestures(t *testing.T) {
	w := worldFixture()
	w.Trail = []WorldTrail{{X: 7}, {X: 8}, {X: 9}, {X: 10}, {X: 11}}
	w.Roles, w.PartyRoles, w.PartyFrames = nil, nil, nil
	w.Direction = 65535
	before := CloneWorld(w)
	state := State{World: w, Frames: []Frame{{PC: 10}}}
	applyWorldExtra(t, &state, nil, ins(0x6e, 0, 0, 4096))
	if state.World.Layer != -32768 || state.World.Trail[0] != (WorldTrail{X: 300, Y: 65516, Direction: 65535}) || !reflect.DeepEqual(state.World.Trail[1:5], before.Trail[:4]) || state.World.PartyStepFrame != before.PartyStepFrame || !reflect.DeepEqual(w, before) {
		t.Fatalf("zero displacement incorrectly ran gesture validation or skipped trail: %+v", state.World)
	}
}

func TestWorldExtraPartyOverlapUsesLeaderPositionAndStandingGestures(t *testing.T) {
	state := multiPartyWalkFixture()
	w := state.World
	w.ViewportX, w.ViewportY = -2, -3
	w.Direction, w.PartyStepFrame = 3, 1
	w.PartyPositions = []WorldPosition{{X: 65535, Y: 0}, {X: 10, Y: 11}, {X: 12, Y: 13}, {X: 14, Y: 15}, {X: 16, Y: 17}}
	original, before := w, CloneWorld(w)
	applyWorldExtra(t, &state, nil, ins(0xa1))
	w = state.World
	wantTrail := WorldTrail{X: 65533, Y: 65533, Direction: 3}
	for i, trail := range w.Trail[:5] {
		if trail != wantTrail {
			t.Fatalf("trail %d = %+v", i, trail)
		}
	}
	if !reflect.DeepEqual(w.PartyPositions, []WorldPosition{{X: 65535}, {X: 65535, Y: 65535}, {X: 65535, Y: 65535}, {X: 14, Y: 15}, {X: 16, Y: 17}}) || !reflect.DeepEqual(w.PartyFrames, []uint16{9, 12, 9, 9, 9}) || w.PartyStepFrame != 2 || w.PartyX != -3 || w.PartyY != -3 || w.FrameNum != before.FrameNum || !reflect.DeepEqual(original, before) {
		t.Fatalf("overlap changed follower positions or lost standing phase: %+v", w)
	}
}

type followerLoad struct {
	Scene, Players bool
	Followers      []uint16
}

type followerResourceSource struct {
	tables
	Calls       []followerLoad
	FailAt      int
	ReturnNil   bool
	ChangeParty bool
	Loaded      *WorldState
}

func (s *followerResourceSource) LoadResources(w *WorldState, scene, players bool) (*WorldState, error) {
	s.Calls = append(s.Calls, followerLoad{scene, players, append([]uint16(nil), w.FollowerSprites...)})
	w = CloneWorld(w)
	if scene {
		w.SceneLoadPending = false
		w.ScreenWave, w.WaveProgression = 0, 0
	}
	w.PlayerResources = nil
	for _, role := range w.PartyRoles {
		w.PlayerResources = append(w.PlayerResources, SpriteResource{ID: w.Roles[role][2], FrameCount: 3, DecodedSize: 42})
	}
	for _, follower := range w.FollowerSprites {
		w.PlayerResources = append(w.PlayerResources, SpriteResource{ID: follower, FrameCount: 3, DecodedSize: 42})
	}
	if len(s.Calls) == s.FailAt {
		w.Events[99] = WorldEvent{State: -1}
		return w, fmt.Errorf("synthetic load failure")
	}
	if s.ReturnNil {
		return nil, nil
	}
	if s.ChangeParty {
		w.PartyRoles = nil
	}
	s.Loaded = w
	return w, nil
}

func TestWorldExtraFollowersReloadInOrderAndUseTheirOwnTrailEntries(t *testing.T) {
	state := multiPartyWalkFixture()
	original, before := state.World, CloneWorld(state.World)
	state.World.SceneLoadPending = true
	before.SceneLoadPending = true
	source := &followerResourceSource{}
	step := applyWorldExtra(t, &state, source, ins(0x98, 10, 11))
	w := state.World
	wantCalls := []followerLoad{{true, true, []uint16{10}}, {false, true, []uint16{10, 11}}}
	if !reflect.DeepEqual(source.Calls, wantCalls) || !reflect.DeepEqual(w.FollowerSprites, []uint16{10, 11}) || w.PartyPositions[3] != (WorldPosition{X: 65526, Y: 10}) || w.PartyPositions[4] != (WorldPosition{X: 65527, Y: 11}) || w.PartyFrames[3] != 9 || w.PartyFrames[4] != 0 || w.SceneLoadPending || !reflect.DeepEqual(original, before) {
		t.Fatalf("follower slots/load order mismatch: calls=%+v world=%+v", source.Calls, w)
	}
	if len(step.Resources) != 9 || step.Resources[3].ID != 10 || step.Resources[8].ID != 11 || state.WorldTicks != 0 {
		t.Fatalf("missing sequential decode evidence: %+v", step)
	}
	w.PlayerResources[0].ID = 99
	source.Loaded.PartyPositions[4].X = 99
	if step.Resources[0].ID != 0 || w.PartyPositions[4].X == 99 {
		t.Fatal("resource evidence or source output aliases mutable world")
	}
}

func TestWorldExtraSecondFollowerOnlyPreservesFirstFollowerSlot(t *testing.T) {
	state := multiPartyWalkFixture()
	state.World.PartyPositions[3] = WorldPosition{X: 500, Y: 600}
	state.World.PartyFrames[3] = 123
	source := &followerResourceSource{}
	applyWorldExtra(t, &state, source, ins(0x98, 0, 88))
	if !reflect.DeepEqual(state.World.FollowerSprites, []uint16{40, 88}) || state.World.PartyPositions[3] != (WorldPosition{X: 500, Y: 600}) || state.World.PartyFrames[3] != 123 || len(source.Calls) != 1 {
		t.Fatalf("second-only assignment rewrote first slot: %+v", state.World)
	}
}

func TestWorldExtraClearingFollowersDoesNotReloadOrDiscardDecodedResources(t *testing.T) {
	state := multiPartyWalkFixture()
	state.World.PlayerResources = []SpriteResource{{ID: 40}}
	positions := append([]WorldPosition(nil), state.World.PartyPositions...)
	applyWorldExtra(t, &state, nil, ins(0x98, 0, 0, 99))
	if len(state.World.FollowerSprites) != 0 || !reflect.DeepEqual(state.World.PlayerResources, []SpriteResource{{ID: 40}}) || !reflect.DeepEqual(state.World.PartyPositions, positions) {
		t.Fatalf("zero followers changed retained slots/resources: %+v", state.World)
	}
}

func TestWorldExtraFailuresPreserveStateEvidenceAndExternalAliases(t *testing.T) {
	tests := []struct {
		name   string
		inst   Instruction
		alter  func(*State)
		source *followerResourceSource
	}{
		{"missing event", ins(0x7d, 44, 1, 1), nil, nil},
		{"special event", ins(0x7e, 0x9001, 1), nil, nil},
		{"missing trail", ins(0x6e), func(s *State) { s.World.Trail = s.World.Trail[:4] }, nil},
		{"missing collision source", ins(0x6e, 1), nil, nil},
		{"invalid overlap role", ins(0xa1), func(s *State) { delete(s.World.Roles, 2) }, nil},
		{"missing loader", ins(0x98, 10), nil, nil},
		{"missing party position slot", ins(0x98, 10), func(s *State) { s.World.PartyPositions = nil }, &followerResourceSource{}},
		{"missing dormant first follower", ins(0x98, 0, 11), func(s *State) { s.World.FollowerSprites = nil }, &followerResourceSource{}},
		{"second reload fails", ins(0x98, 10, 11), nil, &followerResourceSource{FailAt: 2}},
		{"nil load result", ins(0x98, 10), nil, &followerResourceSource{ReturnNil: true}},
		{"party changed during load", ins(0x98, 10), nil, &followerResourceSource{ChangeParty: true}},
		{"PC overflow", ins(0x98, 10), func(s *State) { s.Frames[0].PC = 65535 }, &followerResourceSource{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			state := multiPartyWalkFixture()
			if tc.alter != nil {
				tc.alter(&state)
			}
			before, original := detachPartyState(state), state.World
			step := TraceStep{Effect: "untouched", Resources: []SpriteResource{{ID: 123}}}
			beforeStep := step
			var source Source
			if tc.source != nil {
				source = tc.source
			}
			r := &Runner{source: source}
			handled, err := r.evaluateWorldExtra(&state, &step, tc.inst, nil)
			if !handled || err == nil || !reflect.DeepEqual(state, before) || !reflect.DeepEqual(original, before.World) || !reflect.DeepEqual(step, beforeStep) {
				t.Fatalf("failed instruction was not atomic: handled=%v err=%v state=%+v step=%+v", handled, err, state, step)
			}
			if tc.name == "PC overflow" && len(tc.source.Calls) != 0 {
				t.Fatal("resource load occurred after an invalid fallthrough")
			}
		})
	}
}

func TestWorldExtraReservedInstructionAndRoutingBoundaries(t *testing.T) {
	r := &Runner{}
	state := State{Frames: []Frame{{PC: 10}}}
	applyWorldExtra(t, &state, nil, ins(0x78, 65535, 65535, 65535))
	if state.Frames[0].PC != 11 || state.World != nil {
		t.Fatal("reserved opcode requires or invents world state")
	}
	for _, op := range []uint16{0x3f, 0x44, 0x4c, 0x5a, 0x68, 0x69, 0x6a, 0x6b, 0x77, 0x85, 0x88, 0x8c, 0x8d, 0x92, 0x93, 0x97, 0x9b, 0x9c, 0xa3} {
		if worldExtraOpcodeName(op) != "" {
			t.Fatalf("unimplemented or separately owned opcode %04x was claimed", op)
		}
		if handled, err := r.evaluateWorldExtra(nil, nil, ins(op), nil); handled || err != nil {
			t.Fatalf("opcode %04x swallowed: handled=%v err=%v", op, handled, err)
		}
	}
	if handled, err := r.evaluateWorldExtra(nil, nil, ins(0x62), nil); !handled || err == nil || !strings.Contains(err.Error(), "frame") {
		t.Fatal("recognized opcode accepted absent execution context")
	}
}
