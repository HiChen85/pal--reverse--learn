package palflow

import (
	"reflect"
	"slices"
	"testing"
)

func worldFixture() *WorldState {
	return &WorldState{
		Scene: 1, Layer: 3, PartyX: 300, PartyY: -20,
		PartyOffsetX: 160, PartyOffsetY: 112,
		ViewportX: 140, ViewportY: -132,
		PartyRoles: []uint16{0}, PartyFrames: []uint16{0, 1, 2, 3, 4},
		PartyPositions: []WorldPosition{{X: 160, Y: 112}},
		Trail:          []WorldTrail{{X: 300, Y: 65516, Direction: 1}},
		Events: map[uint16]WorldEvent{
			1: {X: 100, Y: 200, Direction: 3, Frame: 2, Trigger: 30, AutoScript: 40},
			2: {X: 500, Y: 600},
		},
		Scenes: map[uint16]WorldScene{
			1: {Map: 4, OnEnter: 10, OnTeleport: 20},
			2: {Map: 5, OnEnter: 11, OnTeleport: 21},
		},
		Inventory:      map[uint16]int{60: 3},
		InventorySlots: []InventorySlot{{Item: 60, Amount: 3}},
		PlayerSprites:  map[uint16]uint16{0: 12, 1: 13, 2: 14, 3: 15, 4: 16, 5: 17},
	}
}

func TestRelativeNPCStepPreservesDirectionAndUsesAnimationFrameCount(t *testing.T) {
	for _, tc := range []struct{ frames, auto, start, want uint16 }{{3, 9, 3, 0}, {0, 5, 3, 4}, {0, 0, 3, 3}, {4, 0, 65535, 0}} {
		w := worldFixture()
		w.Events[2] = WorldEvent{X: 2, Y: 65534, Direction: 1, Frame: tc.start, SpriteFrames: tc.frames, SpriteFramesAuto: tc.auto}
		state := applyWorld(t, w, 1, 0x6c, 2, 65533, 3)
		got := state.World.Events[2]
		if got.X != 65535 || got.Y != 1 || got.Direction != 1 || got.Frame != tc.want {
			t.Fatalf("relative step: %+v", got)
		}
		if w.Events[2].X != 2 {
			t.Fatal("input event was changed")
		}
	}
}

func applyWorld(t *testing.T, world *WorldState, event uint16, op uint16, operands ...uint16) State {
	t.Helper()
	state := State{World: world, Frames: []Frame{{PC: 10, EventID: event}}}
	inst := Instruction{Opcode: op}
	copy(inst.Operands[:], operands)
	step := TraceStep{}
	handled, err := evaluateWorld(&state, &step, inst)
	if !handled || err != nil {
		t.Fatalf("evaluateWorld(%04x) = (%v, %v)", op, handled, err)
	}
	if state.Frames[0].PC != 11 || step.Effect == "" {
		t.Fatalf("successful instruction did not advance and record effect: %+v, %+v", state, step)
	}
	return state
}

func TestWorldEventInstructionsUseCorrectTargetAndSignedFields(t *testing.T) {
	tests := []struct {
		name  string
		op    uint16
		a     [3]uint16
		id    uint16
		check func(*testing.T, WorldEvent)
	}{
		{"relative zero means self", 0x12, [3]uint16{0, 65535, 25}, 1, func(t *testing.T, event WorldEvent) {
			if event.X != 299 || event.Y != 5 {
				t.Fatalf("position = %d,%d", event.X, event.Y)
			}
		}},
		{"absolute explicit target", 0x13, [3]uint16{2, 123, 456}, 2, func(t *testing.T, event WorldEvent) {
			if event.X != 123 || event.Y != 456 {
				t.Fatalf("position = %d,%d", event.X, event.Y)
			}
		}},
		{"absolute sentinel means self", 0x13, [3]uint16{65535, 23, 45}, 1, func(t *testing.T, event WorldEvent) {
			if event.X != 23 || event.Y != 45 {
				t.Fatalf("position = %d,%d", event.X, event.Y)
			}
		}},
		{"gesture always uses invoking event", 0x14, [3]uint16{2, 99, 99}, 1, func(t *testing.T, event WorldEvent) {
			if event.Frame != 2 || event.Direction != 0 {
				t.Fatalf("gesture = %+v", event)
			}
		}},
		{"direction and gesture", 0x16, [3]uint16{2, 1, 7}, 2, func(t *testing.T, event WorldEvent) {
			if event.Direction != 1 || event.Frame != 7 {
				t.Fatalf("gesture = %+v", event)
			}
		}},
		{"auto script", 0x24, [3]uint16{2, 42}, 2, func(t *testing.T, event WorldEvent) {
			if event.AutoScript != 42 {
				t.Fatalf("autoscript = %d", event.AutoScript)
			}
		}},
		{"trigger script sentinel self", 0x25, [3]uint16{65535, 43}, 1, func(t *testing.T, event WorldEvent) {
			if event.Trigger != 43 {
				t.Fatalf("trigger = %d", event.Trigger)
			}
		}},
		{"trigger mode", 0x40, [3]uint16{2, 6}, 2, func(t *testing.T, event WorldEvent) {
			if event.TriggerMode != 6 {
				t.Fatalf("trigger mode = %d", event.TriggerMode)
			}
		}},
		{"negative event state", 0x49, [3]uint16{2, 65534}, 2, func(t *testing.T, event WorldEvent) {
			if event.State != -2 {
				t.Fatalf("state = %d", event.State)
			}
		}},
		{"vanish always uses invoking event", 0x4b, [3]uint16{2, 99, 99}, 1, func(t *testing.T, event WorldEvent) {
			if event.VanishTime != -15 {
				t.Fatalf("vanish time = %d", event.VanishTime)
			}
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			original := worldFixture()
			before := CloneWorld(original)
			state := applyWorld(t, original, 1, test.op, test.a[:]...)
			test.check(t, state.World.Events[test.id])
			if !reflect.DeepEqual(original, before) {
				t.Fatal("instruction mutated the supplied snapshot")
			}
			other := uint16(1)
			if test.id == 1 {
				other = 2
			}
			if state.World.Events[other] != original.Events[other] {
				t.Fatal("instruction changed another event")
			}
		})
	}
}

func TestWorldRelativeEventPositionUsesViewAnchorNotCachedPartyPosition(t *testing.T) {
	world := worldFixture()
	world.PartyX, world.PartyY = 800, 900
	world.ViewportX, world.ViewportY = 100, 200
	state := applyWorld(t, world, 1, 0x12, 0, 5, 6)
	event := state.World.Events[1]
	if event.X != 265 || event.Y != 318 {
		t.Fatalf("relative position used a stale party cache: %+v", event)
	}
	// PAL_X and PAL_Y interpret packed coordinates as signed 16-bit values.
	world.ViewportX, world.ViewportY = 65535, 65534
	state = applyWorld(t, world, 1, 0x12, 0, 0, 0)
	event = state.World.Events[1]
	if event.X != 159 || event.Y != 110 {
		t.Fatalf("relative position did not unpack signed viewport: %+v", event)
	}
}

func TestWorldGuardedZeroEventOperandsDoNotRequireContext(t *testing.T) {
	for _, op := range []uint16{0x16, 0x24, 0x25, 0x40, 0x49} {
		t.Run(worldOpcodeName(op), func(t *testing.T) {
			world := worldFixture()
			state := applyWorld(t, world, 0, op, 0, 99, 99)
			if !reflect.DeepEqual(state.World, world) {
				t.Fatal("guarded zero operand changed world")
			}
		})
	}
}

func TestWorldPartyFramesSpriteAndBattlefield(t *testing.T) {
	world := worldFixture()
	state := applyWorld(t, world, 0, 0x15, 3, 2, 4)
	if state.World.Direction != 3 || state.World.PartyFrames[4] != 11 || world.PartyFrames[4] != 4 {
		t.Fatalf("party gesture or clone = %+v", state.World)
	}
	state = applyWorld(t, state.World, 0, 0x65, 5, 70, 0)
	if state.World.PlayerSprites[5] != 70 {
		t.Fatal("sprite was not stored")
	}
	state = applyWorld(t, state.World, 0, 0x4a, 12)
	if state.World.Battlefield != 12 {
		t.Fatal("battlefield was not stored")
	}
}

func TestWorldPlayerSpriteKeepsSuppliedRawRolesConsistent(t *testing.T) {
	world := worldFixture()
	role := [75]uint16{}
	role[2], role[3] = 12, 42
	world.Roles = map[uint16][75]uint16{0: role}
	state := applyWorld(t, world, 0, 0x65, 0, 99, 0)
	if state.World.PlayerSprites[0] != 99 || state.World.Roles[0][2] != 99 || state.World.Roles[0][3] != 42 || world.Roles[0][2] != 12 {
		t.Fatal("sprite and raw role snapshot diverged or supplied snapshot mutated")
	}
}

func TestWorldSceneTransitionsAndSelectiveScriptRewrite(t *testing.T) {
	world := worldFixture()
	state := applyWorld(t, world, 0, 0x59, 2)
	if state.World.Scene != 2 || !state.World.SceneChanged || state.World.Layer != 0 {
		t.Fatalf("scene transition = %+v", state.World)
	}
	for _, scene := range []uint16{0, 1} {
		state := applyWorld(t, world, 0, 0x59, scene)
		if !reflect.DeepEqual(state.World, world) {
			t.Fatalf("same/zero scene %d changed world", scene)
		}
	}
	for _, test := range []struct{ enter, teleport, wantEnter, wantTeleport uint16 }{
		{100, 0, 100, 20}, {0, 101, 10, 101}, {100, 101, 100, 101}, {0, 0, 0, 0},
	} {
		state := applyWorld(t, world, 0, 0x6d, 1, test.enter, test.teleport)
		scene := state.World.Scenes[1]
		if scene.OnEnter != test.wantEnter || scene.OnTeleport != test.wantTeleport || scene.Map != 4 {
			t.Fatalf("rewrite %d,%d = %+v", test.enter, test.teleport, scene)
		}
	}
	state = applyWorld(t, world, 0, 0x6d, 0, 100, 101)
	if !reflect.DeepEqual(state.World, world) {
		t.Fatal("scene zero rewrite changed world")
	}
}

func TestWorldPartyPositionRebuildsFullFormationAndTrail(t *testing.T) {
	for _, test := range []struct {
		direction uint16
		dx, dy    int
	}{{0, 16, -8}, {1, 16, 8}, {2, -16, 8}, {3, -16, -8}} {
		world := worldFixture()
		world.Direction = test.direction
		state := applyWorld(t, world, 0, 0x46, 10, 20, 1)
		got := state.World
		if got.PartyX != 336 || got.PartyY != 328 || got.ViewportX != 176 || got.ViewportY != 216 {
			t.Fatalf("coordinates = %+v", got)
		}
		if len(got.PartyPositions) != 5 || len(got.Trail) != 5 {
			t.Fatal("formation does not have five slots")
		}
		for member := range got.PartyPositions {
			wantPosition := WorldPosition{X: uint16(160 + member*test.dx), Y: uint16(112 + member*test.dy)}
			wantTrail := WorldTrail{X: uint16(336 + member*test.dx), Y: uint16(328 + member*test.dy), Direction: test.direction}
			if got.PartyPositions[member] != wantPosition || got.Trail[member] != wantTrail {
				t.Fatalf("direction %d member %d formation mismatch", test.direction, member)
			}
		}
	}
	state := applyWorld(t, worldFixture(), 0, 0x46, 65535, 65535, 65535)
	if state.World.PartyX != -48 || state.World.PartyY != -24 || state.World.ViewportX != -208 || state.World.ViewportY != -136 || state.World.Trail[0].X != 65488 || state.World.Trail[0].Y != 65512 {
		t.Fatalf("packed coordinate wrapping = %+v", state.World)
	}
}

func TestWorldErrorsAreFullyTransactional(t *testing.T) {
	tests := []struct {
		name  string
		inst  Instruction
		alter func(*State)
	}{
		{"missing world", Instruction{Opcode: 0x4a}, func(s *State) { s.World = nil }},
		{"missing frame", Instruction{Opcode: 0x4a}, func(s *State) { s.Frames = nil }},
		{"missing self context", Instruction{Opcode: 0x12}, func(s *State) { s.Frames[0].EventID = 0 }},
		{"invalid self sentinel", Instruction{Opcode: 0x14}, func(s *State) { s.Frames[0].EventID = 65535 }},
		{"missing event", Instruction{Opcode: 0x13, Operands: [3]uint16{99, 10, 20}}, nil},
		{"special event addressing", Instruction{Opcode: 0x13, Operands: [3]uint16{0x9002, 10, 20}}, func(s *State) { s.World.Events[0x9002] = WorldEvent{} }},
		{"party slot beyond array", Instruction{Opcode: 0x15, Operands: [3]uint16{3, 2, 5}}, nil},
		{"missing party slot", Instruction{Opcode: 0x15, Operands: [3]uint16{3, 2, 1}}, func(s *State) { s.World.PartyFrames = s.World.PartyFrames[:1] }},
		{"missing scene", Instruction{Opcode: 0x59, Operands: [3]uint16{3}}, nil},
		{"scene beyond PAL bounds", Instruction{Opcode: 0x59, Operands: [3]uint16{301}}, func(s *State) { s.World.Scenes[301] = WorldScene{} }},
		{"missing rewrite scene", Instruction{Opcode: 0x6d, Operands: [3]uint16{3, 11, 22}}, nil},
		{"sprite resource reload", Instruction{Opcode: 0x65, Operands: [3]uint16{0, 100, 1}}, nil},
		{"player role beyond bounds", Instruction{Opcode: 0x65, Operands: [3]uint16{6, 100}}, nil},
		{"missing player sprite", Instruction{Opcode: 0x65, Operands: [3]uint16{5, 100}}, func(s *State) { delete(s.World.PlayerSprites, 5) }},
		{"missing supplied raw role", Instruction{Opcode: 0x65, Operands: [3]uint16{0, 100}}, func(s *State) { s.World.Roles = map[uint16][75]uint16{} }},
		{"invalid packed party offset", Instruction{Opcode: 0x46}, func(s *State) { s.World.PartyOffsetX = 32768 }},
		{"overflow PC before world write", Instruction{Opcode: 0x49, Operands: [3]uint16{1, 65535}}, func(s *State) { s.Frames[0].PC = 65535 }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			state := State{World: worldFixture(), Frames: []Frame{{PC: 10, EventID: 1}}}
			if test.alter != nil {
				test.alter(&state)
			}
			before := state
			before.Frames = slices.Clone(state.Frames)
			before.World = CloneWorld(state.World)
			originalWorld := state.World
			step := TraceStep{Effect: "unchanged", NextPC: 77}
			beforeStep := step
			handled, err := evaluateWorld(&state, &step, test.inst)
			if !handled || err == nil {
				t.Fatalf("result = %v,%v", handled, err)
			}
			if state.World != originalWorld || !reflect.DeepEqual(state, before) || !reflect.DeepEqual(step, beforeStep) {
				t.Fatal("failed instruction changed world, frames, or trace step")
			}
		})
	}
}

func TestCloneWorldDetachesAllMutableCollections(t *testing.T) {
	world := worldFixture()
	before := CloneWorld(world)
	cloned := CloneWorld(world)
	cloned.PartyRoles[0] = 5
	cloned.PartyFrames[0] = 50
	cloned.PartyPositions[0].X = 9
	cloned.Trail[0].Direction = 3
	cloned.Events[1] = WorldEvent{}
	cloned.Scenes[1] = WorldScene{}
	cloned.Inventory[60] = 100
	cloned.InventorySlots[0].Amount = 99
	cloned.PlayerSprites[0] = 300
	if !reflect.DeepEqual(world, before) {
		t.Fatal("clone shares a mutable collection")
	}
	if CloneWorld(nil) != nil {
		t.Fatal("nil clone changed representation")
	}
	empty := &WorldState{PartyFrames: []uint16{}, InventorySlots: []InventorySlot{}}
	if CloneWorld(empty).PartyFrames == nil || CloneWorld(empty).InventorySlots == nil {
		t.Fatal("clone changed nonnil empty slice")
	}
}

func TestWorldDoesNotAdvertiseUnimplementedOpcodes(t *testing.T) {
	for _, op := range []uint16{0x0000, 0x0011, 0x0044, 0x0075, 0xffff} {
		if worldOpcodeName(op) != "" {
			t.Fatalf("unimplemented opcode %04x advertised", op)
		}
		handled, err := evaluateWorld(nil, nil, Instruction{Opcode: op})
		if handled || err != nil {
			t.Fatalf("unimplemented opcode %04x handled: %v,%v", op, handled, err)
		}
	}
}
