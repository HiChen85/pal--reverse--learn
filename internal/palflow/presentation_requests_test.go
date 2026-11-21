package palflow

import (
	"reflect"
	"strings"
	"testing"
)

// Expectations follow the independent call sites in pinned SDLPAL script.c
// PAL_PartyWalkTo/PAL_RunTriggerScript, play.c PAL_GameUpdate/PAL_StartFrame,
// text.c PAL_StartDialogWithOffset/PAL_EndDialog and video.c VIDEO_FadeScreen.
func TestPartyWalkPresentsOncePerMovementAndRollsBackMissingPalette(t *testing.T) {
	for _, available := range []bool{false, true} {
		s := partyWalkFixture()
		s.World.ScreenWave, s.World.WaveProgression, s.World.ScreenShakeTime = 10, 2, 100
		s.World.NeedToFadeIn = true
		before := detachPartyState(s)
		r := &Runner{source: &cameraPaletteSource{available: available}, options: Options{MaxSteps: 100}}
		step := TraceStep{}
		_, err := r.evaluatePartyWalk(&s, &step, ins(0x70, 1, 1), nil)
		if !available {
			if err == nil || !strings.Contains(err.Error(), "palette") || !reflect.DeepEqual(s, before) || step.PartyWalk != nil {
				t.Fatal("failed redraw committed movement or prior world update", err)
			}
			continue
		}
		if err != nil || len(step.PartyWalk.Steps) != 3 || s.World.ScreenWave != 16 || s.World.WavePhase != 3 || s.World.ScreenShakeTime != 96 || s.World.NeedToFadeIn || s.World.FrameNum != 103 {
			t.Fatalf("walk doubled/omitted scene presentation: %+v %v", s.World, err)
		}
		for i, movement := range step.PartyWalk.Steps {
			want := 1
			if i == 0 {
				want++
			}
			if movement.ScreenUpdates != want {
				t.Fatalf("movement %d requests=%d, want %d", i, movement.ScreenUpdates, want)
			}
		}
	}
}

func TestTriggerWaitPresentsTouchBeforeTriggerThenOuterFrame(t *testing.T) {
	s := partyWalkFixture()
	s.World.ScreenWave, s.World.WaveProgression, s.World.ScreenShakeTime = 10, 2, 100
	s.World.NeedToFadeIn = true
	s.World.Scenes[2] = WorldScene{EventStart: 1}
	s.World.Events[1] = WorldEvent{X: 23, Y: 13, State: 1, SpriteFrames: 1, TriggerMode: 4}
	r := &Runner{source: &cameraPaletteSource{available: true}, options: Options{MaxSteps: 100, MaxCallDepth: 32}}
	step := TraceStep{}
	_, err := r.evaluateTicks(&s, &step, ins(9, 2, 1), nil)
	if err != nil || step.Screen.ScreenUpdates != 5 || s.World.ScreenWave != 18 || s.World.WavePhase != 4 || s.World.ScreenShakeTime != 95 || s.World.NeedToFadeIn || s.World.FrameNum != 102 || len(step.WorldUpdates) != 2 {
		t.Fatalf("touch/wait draw ordering differs: %+v %+v %v", s.World, step, err)
	}
	if step.WorldUpdates[0].ScreenUpdates != 3 || step.WorldUpdates[1].ScreenUpdates != 2 {
		t.Fatal("missing per-update touch and outer redraw evidence", step.WorldUpdates)
	}
}

func TestRedrawBranchesKeepTheirDistinctPersistentEffects(t *testing.T) {
	for _, branch := range []string{"overworld", "battle", "rng"} {
		t.Run(branch, func(t *testing.T) {
			s := partyWalkFixture()
			s.World.ScreenWave, s.World.WaveProgression, s.World.ScreenShakeTime = 9, 2, 10
			s.World.NeedToFadeIn = true
			s.World.InBattle = branch == "battle"
			s.DialogPlayingRNG = branch == "rng"
			r := &Runner{source: &cameraPaletteSource{available: true}}
			step := TraceStep{}
			if err := r.evaluate(&s, &step, ins(5, 0, 2, 1), nil); err != nil {
				t.Fatal(err)
			}
			switch branch {
			case "overworld":
				if s.World.ScreenWave != 11 || s.World.WavePhase != 1 || s.World.ScreenShakeTime != 8 || s.World.NeedToFadeIn || s.World.PartyStepFrame != 2 || step.Screen.ScreenUpdates != 2 || step.Screen.OverworldDelayMilliseconds != 120 {
					t.Fatal("overworld redraw omitted scene/standing effects", s.World, step.Screen)
				}
			case "battle":
				if s.World.ScreenWave != 11 || s.World.WavePhase != 1 || s.World.ScreenShakeTime != 9 || !s.World.NeedToFadeIn || s.World.PartyStepFrame != 0 || step.Screen.ScreenUpdates != 1 || step.Screen.OverworldDelayMilliseconds != 0 {
					t.Fatal("battle redraw applied overworld fade or gestures", s.World, step.Screen)
				}
			case "rng":
				if s.World.ScreenWave != 9 || s.World.WavePhase != 0 || s.World.ScreenShakeTime != 10 || !s.World.NeedToFadeIn || s.World.PartyStepFrame != 0 || step.Screen.ScreenUpdates != 0 || step.Screen.OverworldDelayMilliseconds != 0 {
					t.Fatal("RNG restore incorrectly composed/displayed a scene", s.World, step.Screen)
				}
			}
		})
	}
}

func TestRNGDialogFlagRequiresPortraitPersistsUntilNestedEndDialog(t *testing.T) {
	code := autoSource{1: ins(0x3c, 42, 0, 1), 2: ins(0x3b), 3: ins(5, 0, 1, 1), 4: ins(4, 10), 5: ins(5, 0, 1, 1), 6: ins(0), 10: ins(0)}
	runner, _ := New(&cameraPaletteSource{autoSource: code, available: true}, Options{})
	w := partyWalkFixture().World
	w.ScreenWave, w.WaveProgression, w.ScreenShakeTime = 10, 2, 10
	r, err := runner.Trace(Request{Entry: 1, World: w})
	if err != nil || !r.Completed || r.Trace[2].Screen.Kind != "redraw_rng_restore" || r.Trace[5].Screen.Kind != "redraw" || r.State.World.ScreenWave != 12 || r.State.World.ScreenShakeTime != 9 || r.State.World.ScreenBackupRequests != 1 || r.State.DialogPlayingRNG {
		t.Fatalf("RNG dialog lifetime differs: %+v %v", r, err)
	}
	code[1] = ins(0x3b, 0, 0, 1) // A center dialog has no portrait.
	r, err = runner.Trace(Request{Entry: 1, World: w})
	if err != nil || r.Trace[2].Screen.Kind != "redraw" || r.State.World.ScreenWave != 14 || r.State.World.ScreenShakeTime != 8 || r.State.World.ScreenBackupRequests != 0 {
		t.Fatal("portraitless RNG parameter enabled RNG backing restore", err)
	}
}

func TestFadeScreenUsesWORDSpeedWithoutWorldTicks(t *testing.T) {
	for _, tc := range []struct{ speed, delay uint16 }{{0, 10}, {2, 30}, {6553, 4}, {65535, 0}} {
		s := mediaState()
		s.World.SceneResources = &SceneResourceState{MapID: 1, MapSHA256: "map", GOPSHA256: "gop", MAPArchiveSHA256: "maps", GOPArchiveSHA256: "gops"}
		s.World.NeedToFadeIn, s.World.ScreenShakeTime = true, 100
		s.World.ScreenWave, s.World.WaveProgression = 10, 2
		r := mediaRunner(mediaSource{palette: true}, 100)
		step := TraceStep{}
		_, err := r.evaluatePresentation(&s, &step, ins(0x73, tc.speed), nil)
		if err != nil || step.Media.FrameDelayMilliseconds != tc.delay || step.Media.ScreenUpdates != 74 || step.Screen.ScreenUpdates != 74 || s.World.ScreenShakeTime != 26 || s.World.ScreenWave != 12 || s.World.WavePhase != 1 || s.World.NeedToFadeIn || s.World.FrameNum != 99 || s.WorldTicks != 0 || s.World.ScreenBackupRequests != 1 {
			t.Fatalf("fade speed %d: %+v %+v %v", tc.speed, step.Media, s.World, err)
		}
	}
	for _, mutation := range []func(*WorldState){func(w *WorldState) { w.NeedToFadeIn = true }, func(w *WorldState) { w.SceneResources = nil }, func(w *WorldState) { w.ScreenBackupRequests = ^uint64(0) }} {
		s := mediaState()
		s.World.SceneResources = &SceneResourceState{MapID: 1, MapSHA256: "map", GOPSHA256: "gop", MAPArchiveSHA256: "maps", GOPArchiveSHA256: "gops"}
		mutation(s.World)
		before := detachPartyState(s)
		step := TraceStep{}
		_, err := mediaRunner(mediaSource{}, 100).evaluatePresentation(&s, &step, ins(0x73), nil)
		if err == nil || !reflect.DeepEqual(s, before) || !reflect.DeepEqual(step, TraceStep{}) {
			t.Fatal("failed fade committed backup/wave/palette state", err)
		}
	}
}
