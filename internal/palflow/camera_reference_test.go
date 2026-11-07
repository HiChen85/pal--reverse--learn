package palflow

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"slices"
	"testing"
)

type cameraPaletteSource struct {
	autoSource
	available bool
	lookupErr error
	queries   [][2]uint16
	messages  map[uint16]string
}

func (s *cameraPaletteSource) PaletteAvailable(id uint16, night bool) (bool, error) {
	n := uint16(0)
	if night {
		n = 1
	}
	s.queries = append(s.queries, [2]uint16{id, n})
	return s.available, s.lookupErr
}

func (s *cameraPaletteSource) Message(id uint16) (string, error) {
	if msg, ok := s.messages[id]; ok {
		return msg, nil
	}
	return "", fmt.Errorf("message %d absent", id)
}

func cameraFixture() State {
	return State{CurEquipPart: -1, ScriptSuccess: true, IdleFrames: map[uint16]uint16{1: 7}, LastEventID: 1,
		Frames: []Frame{{PC: 0x1234, Entry: 0x1230, NextEntry: 0x1231, EventID: 1}},
		World: &WorldState{
			Scene: 1, Scenes: map[uint16]WorldScene{1: {}, 2: {}},
			ViewportX: 32760, ViewportY: -32765, PartyOffsetX: -32760, PartyOffsetY: 32760,
			PartyRoles: []uint16{0, 1}, FollowerSprites: []uint16{40},
			PartyPositions: []WorldPosition{{X: 32767, Y: 32768}, {X: 65535, Y: 5}, {X: 100, Y: 200}, {X: 300, Y: 400}, {X: 500, Y: 600}},
			PartyFrames:    []uint16{7, 8, 9, 10, 11}, PartyStepFrame: 2, Direction: 3,
			PartyX: 700, PartyY: 800, Trail: []WorldTrail{{X: 30, Y: 40, Direction: 1}, {X: 50, Y: 60, Direction: 2}},
			FrameNum: 100, ChaseCycles: 10, ChaseRange: 4,
			Palette: 2, NightPalette: true, NeedToFadeIn: true, ScreenTint: "red",
			PlayerSprites: map[uint16]uint16{0: 12, 1: 13},
		},
	}
}

// These expected values come from compiled trusted SDLPal C, with synthetic
// party/palette data. The reference source and its build harness stay in /tmp;
// the repository retains only independently derived numeric results.
func TestCameraAndSceneFadeReferenceGolden(t *testing.T) {
	data, err := os.ReadFile("testdata/camera_reference.json")
	if err != nil {
		t.Fatal(err)
	}
	var golden struct {
		Reference struct{ Commit string }
		Camera    []struct {
			Name                      string
			Operands                  [3]uint16
			Viewport, Offset          [2]int
			Positions                 []WorldPosition
			Updates, Displays, Delays int
		}
		Fade []struct {
			Operand          uint16
			Available        bool
			Updates, Clears  int
			NeedFade         bool `json:"need_fade"`
			NeedDuringUpdate bool `json:"need_during_update"`
			Brightness       []uint8
		}
	}
	if err := json.Unmarshal(data, &golden); err != nil {
		t.Fatal(err)
	}
	if golden.Reference.Commit != "d4bf69b64d335cd680555af9df90e7ec4f3006a3" || len(golden.Camera) != 11 || len(golden.Fade) != 36 {
		t.Fatal("missing pinned reference golden cases")
	}
	for _, tc := range golden.Camera {
		t.Run(tc.Name, func(t *testing.T) {
			s := cameraFixture()
			before := detachPartyState(s)
			r := &Runner{source: &cameraPaletteSource{available: true}, options: Options{MaxSteps: 100000}}
			step := TraceStep{}
			handled, err := r.evaluateInterpreted(&s, &step, Instruction{Opcode: 0x7f, Operands: tc.Operands}, nil)
			w := s.World
			if !handled || err != nil || [2]int{w.ViewportX, w.ViewportY} != tc.Viewport || [2]int{w.PartyOffsetX, w.PartyOffsetY} != tc.Offset || !reflect.DeepEqual(w.PartyPositions, tc.Positions) {
				t.Fatalf("reference camera %s mismatch: state=%+v positions=%v step=%+v err=%v", tc.Name, w, w.PartyPositions, step, err)
			}
			if step.Camera == nil || step.Camera.DisplayFrames != tc.Displays || step.Camera.DelayedFrames != tc.Delays || int(step.Ticks) != tc.Updates || s.WorldTicks != tc.Updates || len(step.WorldUpdates) != tc.Updates || w.FrameNum != 100+uint32(tc.Updates) || s.Frames[0].PC != 0x1235 || s.Frames[0].NextEntry != 0x1231 {
				t.Fatalf("reference camera update/display counters mismatch: state=%+v step=%+v", s, step)
			}
			if !reflect.DeepEqual(w.Trail, before.World.Trail) || !reflect.DeepEqual(w.PartyFrames, before.World.PartyFrames) || w.PartyStepFrame != before.World.PartyStepFrame || w.Direction != before.World.Direction || w.PartyX != before.World.PartyX || w.PartyY != before.World.PartyY {
				t.Fatal("camera changed trail, pose or cached map position")
			}
			if !reflect.DeepEqual(before, cameraFixture()) {
				t.Fatal("camera mutated the caller's original snapshot")
			}
		})
	}
	for _, tc := range golden.Fade {
		t.Run(fmt.Sprintf("fade_%04x_available_%t", tc.Operand, tc.Available), func(t *testing.T) {
			s := cameraFixture()
			before := CloneWorld(s.World)
			source := &cameraPaletteSource{available: tc.Available}
			r := &Runner{source: source, options: Options{MaxSteps: 100000}}
			step := TraceStep{}
			_, err := r.evaluateInterpreted(&s, &step, ins(0x93, tc.Operand), nil)
			if err != nil || step.SceneFade == nil || !slices.Equal(step.SceneFade.Brightness, tc.Brightness) || s.WorldTicks != tc.Updates || int(step.Ticks) != tc.Updates || s.World.NeedToFadeIn != tc.NeedFade || len(step.WorldUpdates) != tc.Updates || s.World.FrameNum != 100+uint32(tc.Updates) {
				t.Fatalf("reference fade mismatch: state=%+v fade=%+v err=%v", s, step.SceneFade, err)
			}
			if tc.NeedDuringUpdate || step.SceneFade.ClearKeysEachFrame != (tc.Clears > 0) || step.SceneFade.ClearDirectionsEachFrame != (tc.Clears > 0) || step.SceneFade.ScreenUpdates != tc.Updates || len(source.queries) != 1 || source.queries[0] != [2]uint16{2, 1} {
				t.Fatalf("reference fade key/palette evidence mismatch: %+v, queries=%v", step.SceneFade, source.queries)
			}
			if !reflect.DeepEqual(before.PartyPositions, s.World.PartyPositions) || !reflect.DeepEqual(before.Trail, s.World.Trail) || !reflect.DeepEqual(before.PartyFrames, s.World.PartyFrames) || (tc.Updates == 0 && s.World.ScreenTint != "red") || (tc.Updates > 0 && s.World.ScreenTint != "") {
				t.Fatal("scene fade changed positions/gestures or mishandled palette application")
			}
		})
	}
}
