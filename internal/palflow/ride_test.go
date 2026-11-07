package palflow

import (
	"reflect"
	"testing"
)

func TestRideMovesEventAndViewportPreservesGesturesAndStoresNewTrail(t *testing.T) {
	for _, tc := range []struct {
		op           uint16
		steps, speed int
	}{{0x3f, 3, 2}, {0x44, 2, 4}, {0x97, 1, 8}} {
		s := partyWalkFixture()
		s.World.Events[2] = WorldEvent{X: 100, Y: 200, Frame: 7, Direction: 2}
		before := CloneWorld(s.World)
		r, _ := New(autoSource{}, Options{MaxSteps: 100})
		step := TraceStep{}
		if ok, err := r.evaluatePartyWalk(&s, &step, ins(tc.op, 1, 1), nil); !ok || err != nil {
			t.Fatal(err)
		}
		w := s.World
		e := w.Events[2]
		if w.ViewportX != 32 || w.ViewportY != 16 || e.X != 109 || e.Y != 203 || e.Frame != 7 || e.Direction != 2 || w.FrameNum != uint32(100+tc.steps) || !reflect.DeepEqual(w.PartyFrames, before.PartyFrames) || !reflect.DeepEqual(w.PartyPositions, before.PartyPositions) || w.PartyStepFrame != before.PartyStepFrame {
			t.Fatal("ride changed gestures or relative event offset", tc, e)
		}
		if step.PartyWalk.RidingEvent != 2 || step.PartyWalk.Speed != tc.speed || len(step.PartyWalk.Steps) != tc.steps || w.Trail[0] != (WorldTrail{X: 32, Y: 16, Direction: 3}) || s.Frames[0].PC != 0x1235 {
			t.Fatal("ride evidence/trail mismatch", tc)
		}
		if tc.op == 0x3f && (w.Trail[1].X != 31 || w.Trail[1].Y != 16 || w.Trail[2].X != 27 || w.Trail[2].Y != 15) {
			t.Fatal("ride stored old rather than new positions")
		}
	}
}

func TestRideAtTargetDoesNotChangeStandingPhaseAndBudgetFailureRollsBack(t *testing.T) {
	s := partyWalkFixture()
	s.World.ViewportX, s.World.ViewportY = 32, 16
	s.World.Events[2] = WorldEvent{X: 100, Y: 200}
	before := CloneWorld(s.World)
	r, _ := New(autoSource{}, Options{MaxSteps: 100})
	step := TraceStep{}
	if _, err := r.evaluatePartyWalk(&s, &step, ins(0x44, 1, 1), nil); err != nil || !reflect.DeepEqual(s.World, before) {
		t.Fatal("empty ride changed party gestures or world", err)
	}
	s = partyWalkFixture()
	s.World.Events[2] = WorldEvent{X: 100, Y: 200}
	saved := detachPartyState(s)
	r, _ = New(autoSource{}, Options{MaxSteps: 2})
	if _, err := r.evaluatePartyWalk(&s, &step, ins(0x3f, 1, 1), nil); err == nil || !reflect.DeepEqual(s, saved) {
		t.Fatal("partial ride survived budget failure", err)
	}
}

func TestOffsetAndAnimateEventDoesNotAdvanceWorld(t *testing.T) {
	s := State{World: &WorldState{Events: map[uint16]WorldEvent{2: {X: 1, Y: 0, Frame: 3, SpriteFrames: 3}}, FrameNum: 100}, Frames: []Frame{{PC: 1, EventID: 2}}}
	step := TraceStep{}
	if _, err := (*Runner)(nil).evaluateWorldExtra(&s, &step, ins(0x6c, 0, 0xffff, 0xffff), nil); err != nil {
		t.Fatal(err)
	}
	e := s.World.Events[2]
	if e.X != 0 || e.Y != 65535 || e.Frame != 0 || s.World.FrameNum != 100 {
		t.Fatal("event relative WORD movement/gesture mismatch", e)
	}
}
