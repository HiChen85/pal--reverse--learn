package palflow

import (
	"reflect"
	"strings"
	"testing"
)

func moveNPC(t *testing.T, state *State, inst Instruction) TraceStep {
	t.Helper()
	step := TraceStep{}
	if handled, err := (*Runner)(nil).evaluateMovement(state, &step, inst, nil); !handled || err != nil {
		t.Fatalf("NPC movement: handled=%t err=%v", handled, err)
	}
	return step
}

func TestNPCSingleStepsDirectionWrapAndAnimation(t *testing.T) {
	for _, test := range []struct {
		op, x, y uint16
	}{{0xb, 65534, 3}, {0xc, 65534, 65535}, {0xd, 6, 65535}, {0xe, 6, 3}} {
		state := State{World: &WorldState{Events: map[uint16]WorldEvent{2: {X: 2, Y: 1, Frame: 3, SpriteFrames: 3}}}, Frames: []Frame{{PC: 1, EventID: 2}}}
		original := CloneWorld(state.World)
		moveNPC(t, &state, ins(test.op))
		event := state.World.Events[2]
		if event.X != test.x || event.Y != test.y || event.Direction != test.op-0xb || event.Frame != 0 || state.Frames[0].PC != 2 {
			t.Fatalf("opcode %04x movement=%+v frame=%+v", test.op, event, state.Frames[0])
		}
		if original.Events[2].X != 2 || original.Events[2].Frame != 3 {
			t.Fatal("single step changed its input snapshot")
		}
	}
}

func TestNPCDirectionGestureSentinelsAndUnusualDirection(t *testing.T) {
	state := State{World: &WorldState{Events: map[uint16]WorldEvent{2: {Direction: 2, Frame: 7}}}, Frames: []Frame{{PC: 1, EventID: 2}}}
	moveNPC(t, &state, ins(0xf, 0xffff, 4))
	if state.World.Events[2].Direction != 2 || state.World.Events[2].Frame != 4 {
		t.Fatal("direction sentinel did not preserve direction")
	}
	moveNPC(t, &state, ins(0xf, 8, 0xffff))
	if state.World.Events[2].Direction != 8 || state.World.Events[2].Frame != 4 {
		t.Fatal("gesture sentinel or raw direction assignment differed from source")
	}
}

func TestNPCWalkToSpeedsAndFrameParity(t *testing.T) {
	for _, test := range []struct {
		op    uint16
		frame uint32
		x, y  uint16
	}{{0x10, 100, 16, 13}, {0x11, 101, 14, 12}, {0x11, 100, 10, 10}, {0x7c, 101, 18, 14}, {0x7c, 100, 10, 10}, {0x82, 100, 26, 18}} {
		state := State{World: &WorldState{FrameNum: test.frame, Events: map[uint16]WorldEvent{2: {X: 10, Y: 10, SpriteFrames: 4}}}, Frames: []Frame{{PC: 1, EventID: 2}}}
		moveNPC(t, &state, ins(test.op, 10, 10))
		event := state.World.Events[2]
		if event.X != test.x || event.Y != test.y || state.Frames[0].PC != 1 || state.World.FrameNum != test.frame {
			t.Fatalf("opcode=%04x frame=%d event=%+v PC=%d", test.op, test.frame, event, state.Frames[0].PC)
		}
	}
	state := State{World: &WorldState{Events: map[uint16]WorldEvent{1: {X: 29, Y: 200, Frame: 9}}}, Frames: []Frame{{PC: 1, EventID: 1}}}
	moveNPC(t, &state, ins(0x11, 1, 1))
	if state.Frames[0].PC != 2 || state.World.Events[1].X != 32 || state.World.Events[1].Y != 16 || state.World.Events[1].Frame != 0 {
		t.Fatal("close X axis did not snap both coordinates and advance")
	}
}

func TestNPCMovementFailureIsAtomic(t *testing.T) {
	for _, test := range []struct {
		id   uint16
		pc   uint16
		want string
	}{{0, 1, "concrete invoking event"}, {0xffff, 1, "concrete invoking event"}, {0x9001, 1, "special addressing"}, {3, 1, "absent"}, {2, 0xffff, "program counter"}} {
		state := State{World: &WorldState{Events: map[uint16]WorldEvent{2: {X: 32, Y: 16, Frame: 7}}}, Frames: []Frame{{PC: test.pc, EventID: test.id}}}
		before := detachPartyState(state)
		step := TraceStep{Effect: "preserved"}
		handled, err := (*Runner)(nil).evaluateMovement(&state, &step, ins(0x10, 1, 1), nil)
		if !handled || err == nil || !strings.Contains(err.Error(), test.want) || !reflect.DeepEqual(state, before) || step.Effect != "preserved" {
			t.Fatalf("NPC failure=%v state=%+v step=%+v", err, state, step)
		}
	}
}

func TestObstacleDiamondTileAndSignedRange(t *testing.T) {
	for _, test := range []struct{ px, py, x, y, h int }{
		{0, 0, 0, 0, 0}, {16, 0, 0, 0, 1}, {31, 0, 1, 0, 0}, {0, 15, 0, 1, 0}, {16, 8, 0, 0, 1}, {31, 15, 1, 1, 0}, {-1, -1, 0, 0, 0},
	} {
		x, y, h := ObstacleTile(WorldPosition{X: uint16(test.px), Y: uint16(test.py)})
		if x != test.x || y != test.y || h != test.h {
			t.Fatalf("pixel=(%d,%d) tile=(%d,%d,%d)", test.px, test.py, x, y, h)
		}
	}
	w := &WorldState{PartyOffsetX: 160, PartyOffsetY: 112}
	for _, test := range []struct {
		x, y int
		want bool
	}{{160, 112, false}, {159, 112, true}, {160, 111, true}, {1855, 112, false}, {1856, 112, true}, {160, 32640, true}, {-1, 112, true}} {
		blocked, err := ObstacleOutOfRange(w, WorldPosition{X: uint16(test.x), Y: uint16(test.y)})
		if err != nil || blocked != test.want {
			t.Fatalf("range (%d,%d)=%t,%v", test.x, test.y, blocked, err)
		}
	}
}

func TestEventObstaclesRespectSceneSelfStateAndExactDistance(t *testing.T) {
	w := &WorldState{Scene: 1, Scenes: map[uint16]WorldScene{1: {}, 2: {EventStart: 3}}, Events: map[uint16]WorldEvent{
		1: {State: 2, X: 100, Y: 100, VanishTime: -15},
		2: {State: 1, X: 130, Y: 100}, 3: {State: -2, X: 140, Y: 100}, 4: {State: 2, X: 150, Y: 100},
	}}
	for _, test := range []struct {
		x, y, self uint16
		want       bool
	}{{100, 100, 0, true}, {100, 100, 1, false}, {115, 100, 0, true}, {116, 100, 0, false}, {100, 107, 0, true}, {100, 108, 0, false}, {130, 100, 0, false}, {150, 100, 0, false}} {
		blocked, err := CheckEventObstacle(w, WorldPosition{X: test.x, Y: test.y}, test.self)
		if err != nil || blocked != test.want {
			t.Fatalf("event obstacle (%d,%d) self=%d=%t,%v", test.x, test.y, test.self, blocked, err)
		}
	}
	delete(w.Events, 3)
	if _, err := CheckEventObstacle(w, WorldPosition{X: 100, Y: 100}, 0); err == nil {
		t.Fatal("incomplete event snapshot was treated as clear or blocked")
	}
}
