package palflow

import (
	"reflect"
	"testing"
)

func chaseState() State {
	return State{World: &WorldState{ViewportX: 36, ViewportY: 36, PartyOffsetX: 160, PartyOffsetY: 112, ChaseRange: 1, Events: map[uint16]WorldEvent{1: {X: 100, Y: 100, SpriteFrames: 3}}}, Frames: []Frame{{PC: 1, EventID: 1}}}
}

// Goldens from an extracted PAL_MonsterChasePlayer + PAL_NPCWalkOneStep C
// harness, SDLPAL d4bf69b64d335cd680555af9df90e7ec4f3006a3. The harness uses
// explicit collision probes; no executable or game resources are required.
func TestMonsterChaseCollisionGoldens(t *testing.T) {
	for _, tc := range []struct {
		blocked int
		x, y    uint16
	}{{0, 108, 104}, {1, 96, 96}, {3, 112, 100}} {
		s := chaseState()
		before := CloneWorld(s.World)
		source := &partyObstacleSource{blocked: map[int]bool{tc.blocked: true}}
		r, _ := New(source, Options{})
		step := TraceStep{}
		handled, err := r.evaluateMovement(&s, &step, ins(0x4c), nil)
		e := s.World.Events[1]
		if !handled || err != nil || e.X != tc.x || e.Y != tc.y || e.Direction != 3 || e.Frame != 1 || s.Frames[0].PC != 2 || len(source.queries) != 5 || !reflect.DeepEqual(before, chaseState().World) {
			t.Fatal("chase collision golden mismatch", tc, e, err)
		}
		for i, q := range source.queries {
			if q.Range || (i == 0 && (!q.Events || q.Self != 1)) || (i > 0 && (q.Events || q.Self != 0)) {
				t.Fatal("chase collision flags differ from reference", q)
			}
		}
	}
}

func TestMonsterChaseRangeRepelFloatingAndAxisRandom(t *testing.T) {
	for _, mode := range []string{"far", "repel", "floating", "axis"} {
		s := chaseState()
		source := &partyObstacleSource{}
		r, _ := New(source, Options{})
		inst := ins(0x4c)
		var inputs []Input
		switch mode {
		case "far":
			s.World.ViewportX = 1000
		case "repel":
			s.World.ChaseRange = 0
			s.World.FrameNum = 1
			s.World.Events[1] = WorldEvent{X: 100, Y: 100, Direction: 3, SpriteFrames: 3}
		case "floating":
			inst.Operands = [3]uint16{0, 0, 1}
		case "axis":
			s.World.ViewportX = -60
			s.World.ViewportY = -12
			inputs = []Input{{Kind: InputRandom, Value: 0}, {Kind: InputRandom, Value: 2}}
		}
		step := TraceStep{}
		if _, err := r.evaluateMovement(&s, &step, inst, inputs); err != nil {
			t.Fatal(err)
		}
		e := s.World.Events[1]
		if mode == "axis" {
			if s.InputIndex != 2 || len(step.RandomInputs) != 2 || e.Direction != 2 || e.X != 108 || e.Y != 96 {
				t.Fatal("axis ties did not consume source range/tail results", e)
			}
		} else if s.InputIndex != 0 {
			t.Fatal("unexpected RNG consumption")
		}
		if mode == "floating" && (len(source.queries) != 0 || e.X != 108 || e.Y != 104) {
			t.Fatal("floating chase inspected map", e)
		}
		if (mode == "far" || mode == "repel") && (e.X != 100 || e.Y != 100 || e.Frame != 1 || len(source.queries) != 0) {
			t.Fatal("stationary chase failed to advance animation", e)
		}
		if mode == "repel" && e.Direction != 0 {
			t.Fatal("repel did not rotate on odd frame")
		}
	}
}

func TestMonsterChaseFailureIsAtomicIncludingSeedAndInput(t *testing.T) {
	for _, failure := range []string{"collision", "missingrandom", "pc"} {
		s := chaseState()
		source := &partyObstacleSource{failAt: 3}
		r, _ := New(source, Options{})
		if failure == "missingrandom" {
			s.World.ViewportX = -60
		} else {
			s.World.RandomMode = "seeded"
			s.World.ViewportX = -60
		}
		if failure == "pc" {
			s.Frames[0].PC = 0xffff
		}
		before := detachPartyState(s)
		step := TraceStep{Effect: "unchanged"}
		if _, err := r.evaluateMovement(&s, &step, ins(0x4c), nil); err == nil || !reflect.DeepEqual(s, before) || step.Effect != "unchanged" {
			t.Fatal("chase failure changed state", failure, err)
		}
	}
}
