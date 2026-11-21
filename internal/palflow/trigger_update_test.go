package palflow

import (
	"reflect"
	"strings"
	"testing"
)

func TestTriggerEnabledWaitPreservesTouchAutoOrderAndReturnedEntries(t *testing.T) {
	s := partyWalkFixture()
	w := s.World
	w.Scenes[2] = WorldScene{EventStart: 4}
	w.Events = map[uint16]WorldEvent{1: {State: 1, VanishTime: -1, AutoScript: 20}, 2: {State: -2, X: 500, Y: 500, Frame: 7, AutoScript: 21}, 3: {State: 1, X: 23, Y: 13, TriggerMode: 4, Trigger: 10}, 4: {State: 1, AutoScript: 30}}
	source := tables{scripts: make([]Instruction, 42), messages: map[uint16]string{0: "touch first", 1: "touch resumed", 2: "auto call"}}
	source.scripts[10], source.scripts[11], source.scripts[12], source.scripts[13] = ins(0xffff, 0), ins(1), ins(0xffff, 1), ins(0)
	source.scripts[20], source.scripts[21] = ins(0x7d, 1, 1, 0), ins(0)
	source.scripts[30], source.scripts[31], source.scripts[40], source.scripts[41] = ins(4, 40), ins(3, 30), ins(0xffff, 2), ins(0)
	r, _ := New(source, Options{MaxSteps: 100, MaxTextBytes: 1000})
	step := TraceStep{}
	if _, err := r.evaluateTicks(&s, &step, ins(9, 2, 1), nil); err != nil {
		t.Fatal(err)
	}
	report := Report{}
	collectStepEvidence(&report, step)
	if !reflect.DeepEqual(report.DialogueIDs, []uint16{0, 2, 1, 2}) || s.World.Events[3].Trigger != 12 || s.World.Events[1].VanishTime != 0 || s.World.Events[1].X != 1 || s.World.Events[2].State != 2 || s.World.Events[2].Frame != 0 || s.World.FrameNum != 102 || s.Frames[0].PC != 0x1235 || len(step.WorldUpdates) != 2 || len(step.AutoSteps) != 0 {
		t.Fatal("trigger/auto order, vanish restoration or callback entry mismatch", report.DialogueIDs)
	}
}

func TestTriggerEnabledWaitRollsBackNestedFailureAndBoundsRecursion(t *testing.T) {
	for _, recursive := range []bool{false, true} {
		s := partyWalkFixture()
		s.World.Scenes[2] = WorldScene{EventStart: 1}
		s.World.Events[1] = WorldEvent{State: 1, X: 23, Y: 13, TriggerMode: 4, Trigger: 10}
		code := autoSource{10: ins(0x1e, 1), 11: ins(0x7fff)}
		if recursive {
			code[10] = ins(9, 1, 1)
		}
		r, _ := New(code, Options{MaxSteps: 100, MaxCallDepth: 4})
		before := detachPartyState(s)
		step := TraceStep{Effect: "preserved"}
		_, err := r.evaluateTicks(&s, &step, ins(9, 1, 1), nil)
		if err == nil || !reflect.DeepEqual(s, before) || step.Effect != "preserved" {
			t.Fatal("nested triggered wait failure committed", err)
		}
		if recursive && !strings.Contains(err.Error(), "depth") {
			t.Fatal("recursive wait did not respect trigger call depth", err)
		}
	}
}

func TestTriggerEnabledWaitDisplacementQueriesRangeAndDoesNotChangeTrail(t *testing.T) {
	s := partyWalkFixture()
	s.World.Scenes[2] = WorldScene{EventStart: 1}
	s.World.Events[1] = WorldEvent{State: 2, X: 23, Y: 13, Sprite: 1, Direction: 3}
	source := &partyObstacleSource{blocked: map[int]bool{1: true}}
	r, _ := New(source, Options{MaxSteps: 100})
	before := append([]WorldTrail(nil), s.World.Trail...)
	if _, err := r.evaluateTicks(&s, &TraceStep{}, ins(9, 1, 1), nil); err != nil {
		t.Fatal(err)
	}
	if len(source.queries) != 2 || !source.queries[0].Range || !source.queries[0].Events || s.World.ViewportX != 7 || s.World.ViewportY != 5 || !reflect.DeepEqual(before, s.World.Trail) {
		t.Fatal("NPC displacement did not preserve source collision/trail behavior", source.queries)
	}
}
