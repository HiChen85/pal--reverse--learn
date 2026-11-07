package palflow

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"testing"
)

// Only numeric results from the trusted public C reference are retained here.
// Its extracted code and synthetic build harness remain outside the repository.
func TestRelativePartyStepReferenceAndTriggerDispatch(t *testing.T) {
	data, err := os.ReadFile("testdata/relative_step_reference.json")
	if err != nil {
		t.Fatal(err)
	}
	var golden struct {
		Reference struct{ Commit string }
		Cases     []struct {
			Phase         uint16
			CollisionMask int `json:"collision_mask"`
			Party         int
			Operands      [3]uint16
			Viewport      [2]int
			Layer         int16
			StepFrame     uint16 `json:"step_frame"`
			Positions     []WorldPosition
			Frames        []uint16
			Trail         []WorldTrail
			Queries       []WorldPosition
		}
	}
	if err := json.Unmarshal(data, &golden); err != nil {
		t.Fatal(err)
	}
	if golden.Reference.Commit != "d4bf69b64d335cd680555af9df90e7ec4f3006a3" || len(golden.Cases) != 32 {
		t.Fatal("missing pinned reference cases")
	}
	for _, tc := range golden.Cases {
		t.Run(fmt.Sprintf("party%d_phase%d_collision%d", tc.Party, tc.Phase, tc.CollisionMask), func(t *testing.T) {
			w := relativePartyStepFixture(tc.Phase, tc.Party)
			before := CloneWorld(w)
			source := &partyObstacleSource{autoSource: autoSource{1: {Opcode: 0x6e, Operands: tc.Operands}, 2: ins(0)}, blocked: map[int]bool{1: tc.CollisionMask&1 != 0, 2: tc.CollisionMask&2 != 0}}
			r, err := New(source, Options{MaxSteps: 2})
			if err != nil {
				t.Fatal(err)
			}
			report, err := r.Trace(Request{Entry: 1, EventID: 1, World: w})
			if err != nil || !report.Completed {
				t.Fatalf("trigger dispatch failed: %v %+v", err, report.Blocker)
			}
			got := report.State.World
			if [2]int{got.ViewportX, got.ViewportY} != tc.Viewport || got.Layer != tc.Layer || got.PartyStepFrame != tc.StepFrame || !reflect.DeepEqual(got.PartyPositions, tc.Positions) || !reflect.DeepEqual(got.PartyFrames, tc.Frames) || !reflect.DeepEqual(got.Trail, tc.Trail) {
				t.Fatalf("reference relative step mismatch: %+v", got)
			}
			if got.PartyX != int(int16(tc.Viewport[0]+160)) || got.PartyY != int(int16(tc.Viewport[1]+112)) {
				t.Fatal("cached map position differs from packed signed coordinate")
			}
			if len(source.queries) != len(tc.Queries) {
				t.Fatalf("leader collision was queried or member collision omitted: %+v", source.queries)
			}
			for i, q := range source.queries {
				if q.Position != tc.Queries[i] || q.Self != 0 || !q.Events || !q.Range {
					t.Fatalf("formation collision %d differs: %+v expected %+v", i, q, tc.Queries[i])
				}
			}
			if report.State.ExecutedSteps != 2 || report.State.WorldTicks != 0 || report.State.AutoSubsteps != 0 || report.State.MediaWork != 0 || got.FrameNum != 100 || got.ChaseCycles != 10 || got.ScreenWave != 7 || got.WaveProgression != 2 || got.ScreenShakeTime != 5 || !got.NeedToFadeIn || report.Trace[0].Screen != nil {
				t.Fatal("single relative step advanced world time or presented a scene")
			}
			if !reflect.DeepEqual(w, before) {
				t.Fatal("relative step mutated the request's world")
			}
		})
	}
}

func relativePartyStepFixture(phase uint16, party int) *WorldState {
	w := multiPartyWalkFixture().World
	w.ViewportX, w.ViewportY, w.PartyOffsetX, w.PartyOffsetY = 32760, -32760, 160, 112
	w.Direction, w.PartyStepFrame = 3, phase
	w.PartyRoles = w.PartyRoles[:party]
	if party == 1 {
		w.FollowerSprites = nil
	}
	for i := range 5 {
		w.PartyPositions[i] = WorldPosition{X: uint16(111 + i*222), Y: uint16(-222 - i*222)}
		w.PartyFrames[i] = uint16(20 + i)
	}
	w.Roles[0], w.Roles[1], w.Roles[2] = [75]uint16{64: 3}, [75]uint16{64: 4}, [75]uint16{}
	w.Trail = []WorldTrail{{X: 10, Y: 20, Direction: 0}, {X: 65530, Y: 32770, Direction: 1}, {X: 30, Y: 40, Direction: 2}, {X: 50, Y: 60, Direction: 3}, {X: 70, Y: 80, Direction: 0}}
	w.ScreenWave, w.WaveProgression, w.ScreenShakeTime, w.NeedToFadeIn = 7, 2, 5, true
	return w
}

func TestRelativePartyStepLateCollisionFailureIsAtomic(t *testing.T) {
	s := multiPartyWalkFixture()
	s.World = relativePartyStepFixture(3, 3)
	before, original := detachPartyState(s), s.World
	source := &partyObstacleSource{failAt: 2}
	r := &Runner{source: source}
	step := TraceStep{Effect: "retain evidence"}
	beforeStep := step
	handled, err := r.evaluateInterpreted(&s, &step, ins(0x6e, 1, 65535, 65535), nil)
	if !handled || err == nil || len(source.queries) != 2 || !reflect.DeepEqual(s, before) || !reflect.DeepEqual(original, before.World) || !reflect.DeepEqual(step, beforeStep) {
		t.Fatalf("second member collision partially committed: handled=%v err=%v state=%+v", handled, err, s)
	}
}
