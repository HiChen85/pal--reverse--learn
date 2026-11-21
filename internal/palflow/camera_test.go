package palflow

import (
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"
)

func cameraAutoFixture(code autoSource) (State, *Runner) {
	s := cameraFixture()
	s.World.Scenes[2] = WorldScene{EventStart: 2}
	s.World.Events = map[uint16]WorldEvent{1: {}, 2: {State: 1, AutoScript: 1}}
	source := &cameraPaletteSource{autoSource: code, available: true, messages: map[uint16]string{5: "abcd"}}
	return s, &Runner{source: source, options: Options{MaxSteps: 100000, MaxCallDepth: 32, MaxTextBytes: 1024}}
}

func TestCameraAndFadeSharedNestedBudgetTextInputAndRollback(t *testing.T) {
	for _, inst := range []Instruction{ins(0x7f, 2, 65535, 3), ins(0x93, 22)} {
		for _, budget := range []int{35, 36} {
			s, r := cameraAutoFixture(autoSource{1: ins(4, 10), 10: ins(6, 100, 14), 11: ins(0xffff, 5), 12: ins(0x49, 0xffff, 65535), 13: ins(1)})
			s.ExecutedSteps, s.PlayerSubsteps, s.WorldTicks, s.AutoSubsteps, s.CombatWork = 2, 3, 4, 5, 6
			s.MediaWork = 7
			r.options.MaxSteps = budget
			before := detachPartyState(s)
			step := TraceStep{Effect: "untouched"}
			_, err := r.evaluateInterpreted(&s, &step, inst, []Input{{Kind: InputRandom, Value: 1}})
			if budget == 35 {
				if err == nil || !strings.Contains(err.Error(), "budget") || !reflect.DeepEqual(s, before) || !reflect.DeepEqual(step, TraceStep{Effect: "untouched"}) {
					t.Fatalf("failed camera/fade leaked nested prefix: state=%+v step=%+v err=%v", s, step, err)
				}
				continue
			}
			if err != nil || s.InputIndex != 1 || s.TextBytes != 4 || s.ExecutedSteps != 6 || s.PlayerSubsteps != 3 || s.WorldTicks != 7 || s.AutoSubsteps != 6 || s.CombatWork != 6 || s.MediaWork != 7 || s.World.Events[2].State != -1 || s.World.Events[2].AutoScript != 2 || s.Frames[0].PC != 0x1235 || s.Frames[0].NextEntry != 0x1231 || len(step.WorldUpdates) != 3 {
				t.Fatalf("camera/fade shared work or callback frame mismatch: state=%+v step=%+v err=%v", s, step, err)
			}
			for i, update := range step.WorldUpdates {
				if update.Tick != uint16(i+1) || update.BeforeFrame != uint32(100+i) || update.AfterFrame != uint32(101+i) {
					t.Fatal("lost ordered update evidence", step.WorldUpdates)
				}
			}
			calls := step.WorldUpdates[0].AutoSteps
			if len(calls) != 1 || len(calls[0].TriggerSteps) != 4 || calls[0].TriggerSteps[0].Input.Index != 0 || calls[0].TriggerSteps[1].Message.Text != "abcd" || calls[0].EventID != 2 || calls[0].Tick != 1 || len(step.WorldUpdates[1].AutoSteps) != 0 {
				t.Fatal("nested callback/input/dialogue evidence missing", step.WorldUpdates)
			}
			report := Report{}
			collectStepEvidence(&report, step)
			if !reflect.DeepEqual(report.DialogueIDs, []uint16{5}) {
				t.Fatal("world-update dialogue collection missing", report.DialogueIDs)
			}
		}
	}
	s, r := cameraAutoFixture(autoSource{1: ins(4, 10), 10: ins(0xffff, 5), 11: ins(1)})
	r.options.MaxTextBytes = 3
	before := detachPartyState(s)
	_, err := r.evaluateCamera(&s, &TraceStep{}, ins(0x93, 32), nil)
	if err == nil || !strings.Contains(err.Error(), "text byte") || !reflect.DeepEqual(s, before) {
		t.Fatal("scene fade failed text budget leaked prefix", err)
	}
}

func TestCameraNestedInsideAutoTriggerAndRecursiveAutoIsBounded(t *testing.T) {
	for _, budget := range []int{9, 10} {
		s, r := cameraAutoFixture(autoSource{1: ins(4, 10), 10: ins(0x49, 0xffff, 65535), 11: ins(0x7f, 3, 2, 2), 12: ins(1)})
		r.options.MaxSteps = budget
		before := detachPartyState(s)
		step := TraceStep{}
		_, err := r.evaluateCamera(&s, &step, ins(0x7f, 1, 65535, 3), nil)
		if budget == 9 {
			if err == nil || !reflect.DeepEqual(s, before) || !reflect.DeepEqual(step, TraceStep{}) {
				t.Fatal("nested camera exceeded budget without rollback", err)
			}
		} else if err != nil || s.WorldTicks != 5 || s.ExecutedSteps != 3 || s.AutoSubsteps != 1 || s.Frames[0].PC != 0x1235 || len(step.WorldUpdates[0].AutoSteps[0].TriggerSteps[1].WorldUpdates) != 2 {
			t.Fatalf("nested camera/frame evidence mismatch: state=%+v step=%+v err=%v", s, step, err)
		}
	}
	s, r := cameraAutoFixture(autoSource{1: ins(0x7f, 1, 1, 1)})
	r.options.MaxSteps = 20
	before := detachPartyState(s)
	_, err := r.evaluateCamera(&s, &TraceStep{}, ins(0x93, 64), nil)
	if err == nil || !strings.Contains(err.Error(), "budget") || !reflect.DeepEqual(s, before) {
		t.Fatal("recursive auto camera was unbounded or committed prefix", err)
	}
}

type cameraObservingSource struct {
	*cameraPaletteSource
	needDuringUpdate []bool
	positions        []CameraPosition
}

func (s *cameraObservingSource) LoadResources(w *WorldState, scene, players bool) (*WorldState, error) {
	s.needDuringUpdate = append(s.needDuringUpdate, w.NeedToFadeIn)
	s.positions = append(s.positions, cameraPosition(w))
	return CloneWorld(w), nil
}

func TestCameraUpdateOrderAndSceneFadeInitialFlagAndCapturedPalette(t *testing.T) {
	s, r := cameraAutoFixture(autoSource{1: ins(0x65, 0, 12, 1), 2: ins(0x50, 1), 3: ins(0x8b, 9)})
	source := &cameraObservingSource{cameraPaletteSource: r.source.(*cameraPaletteSource)}
	r.source = source
	step := TraceStep{}
	_, err := r.evaluateCamera(&s, &step, ins(0x93, 22), nil)
	if err != nil || !reflect.DeepEqual(source.needDuringUpdate, []bool{false}) || s.World.NeedToFadeIn || !s.World.NightPalette || s.World.Palette != 9 || !step.SceneFade.Night || step.SceneFade.Palette != 2 || step.SceneFade.ScreenUpdates != 4 {
		t.Fatalf("fade flag/captured palette or implicit fade order mismatch: palette=%d need=%t fade=%+v observed=%v err=%v", s.World.Palette, s.World.NeedToFadeIn, step.SceneFade, source.needDuringUpdate, err)
	}
	s, r = cameraAutoFixture(autoSource{1: ins(0x65, 0, 12, 1), 2: ins(0x65, 0, 12, 1)})
	source = &cameraObservingSource{cameraPaletteSource: r.source.(*cameraPaletteSource)}
	r.source = source
	step = TraceStep{}
	_, err = r.evaluateCamera(&s, &step, ins(0x7f, 3, 65534, 2), nil)
	if err != nil || len(source.positions) != 2 || source.positions[0].ViewportX != 32763 || source.positions[0].PartyOffsetX != -32763 || source.positions[1].ViewportX != 32766 || source.positions[1].PartyOffsetX != -32766 || step.WorldUpdates[1].AutoSteps[0].Tick != 2 || step.Camera.ScreenUpdates != 3 {
		t.Fatal("camera did not move before each auto update", source.positions, step, err)
	}
}

func TestCameraPresentationWaveShakeAndNoDisplayRestore(t *testing.T) {
	for _, inst := range []Instruction{ins(0x7f, 0, 0, 65535), ins(0x7f, 0, 0, 0), ins(0x7f, 2, 3, 65535), ins(0x7f, 1, 1, 3), ins(0x93, 22)} {
		s := cameraFixture()
		s.World.ScreenWave, s.World.WaveProgression, s.World.ScreenShakeTime = 10, 3, 9
		r := &Runner{source: &cameraPaletteSource{available: true}, options: Options{MaxSteps: 100}}
		step := TraceStep{}
		_, err := r.evaluateCamera(&s, &step, inst, nil)
		if err != nil {
			t.Fatal(err)
		}
		frames, screens := 0, 0
		if step.Camera != nil {
			frames, screens = step.Camera.DisplayFrames, step.Camera.ScreenUpdates
		} else {
			frames, screens = len(step.SceneFade.Brightness), step.SceneFade.ScreenUpdates
		}
		if s.World.ScreenWave != uint16(10+3*frames) || s.World.WavePhase != uint8(frames) || s.World.ScreenShakeTime != uint16(9-screens) || (frames == 0 && !s.World.NeedToFadeIn) || (frames > 0 && s.World.NeedToFadeIn) {
			t.Fatalf("camera/fade presentation counters mismatch: world=%+v step=%+v", s.World, step)
		}
	}
}

func TestCameraRecomputesSceneAutoCandidatesAndPreservesInterruptedFrameCounters(t *testing.T) {
	s, r := cameraAutoFixture(autoSource{1: ins(0x59, 2), 2: ins(0x49, 3, 65535)})
	s.World.Scenes[3] = WorldScene{EventStart: 3}
	s.World.Events[3] = WorldEvent{State: 1, AutoScript: 2}
	step := TraceStep{}
	_, err := r.evaluateCamera(&s, &step, ins(0x7f, 1, 1, 3), nil)
	if err != nil || s.World.Scene != 2 || !s.World.SceneChanged || !s.World.SceneLoadPending || s.WorldTicks != 3 || s.AutoSubsteps != 2 || s.World.FrameNum != 101 || s.World.Events[3].State != -1 || len(step.WorldUpdates) != 3 {
		t.Fatal("camera continuation did not use the new scene's candidates", err, step.WorldUpdates)
	}
	for i, update := range step.WorldUpdates {
		wantAfter := uint32(100)
		if i == 2 {
			wantAfter = 101
		}
		if update.BeforeFrame != 100 || update.AfterFrame != wantAfter {
			t.Fatal("interrupted update advanced frame counters", step.WorldUpdates)
		}
	}
	if step.WorldUpdates[0].AutoSteps[0].EventID != 2 || step.WorldUpdates[1].AutoSteps[0].EventID != 3 || len(step.WorldUpdates[2].AutoSteps) != 0 {
		t.Fatal("camera retained stale scene candidates", step.WorldUpdates)
	}
}

func TestSceneFadeUnavailablePaletteSkipsSceneValidationAndAllPresentation(t *testing.T) {
	s := cameraFixture()
	s.World.Scenes, s.World.Events = nil, nil
	s.World.ScreenWave, s.World.WaveProgression, s.World.ScreenShakeTime = 10, 3, 9
	before := detachPartyState(s)
	r := &Runner{source: &cameraPaletteSource{available: false}, options: Options{MaxSteps: 1}}
	step := TraceStep{}
	_, err := r.evaluateCamera(&s, &step, ins(0x93, 3), nil)
	before.World.NeedToFadeIn = false
	before.Frames[0].PC++
	if err != nil || !reflect.DeepEqual(s, before) || step.Ticks != 0 || step.SceneFade.PaletteLookup != "unavailable" || step.SceneFade.ScreenUpdates != 0 || step.SceneFade.ClearKeysEachFrame {
		t.Fatal("unavailable palette did not return before scene/key/palette processing", err, step)
	}
}

func TestCameraValidationAndLateFailuresAreAtomic(t *testing.T) {
	for _, tc := range []struct {
		name  string
		inst  Instruction
		alter func(*State, *Runner)
	}{
		{"unknown palette", ins(0x93, 2), func(s *State, r *Runner) { r.source = autoSource{} }},
		{"malformed palette", ins(0x93, 2), func(s *State, r *Runner) { r.source.(*cameraPaletteSource).lookupErr = errors.New("malformed PAT") }},
		{"missing screen positions", ins(0x7f, 2, 3, 65535), func(s *State, r *Runner) { s.World.PartyPositions = s.World.PartyPositions[:1] }},
		{"invalid viewport", ins(0x7f, 2, 3, 2), func(s *State, r *Runner) { s.World.ViewportX = 32768 }},
		{"invalid offset", ins(0x7f, 0, 0, 65535), func(s *State, r *Runner) { s.World.PartyOffsetY = -32769 }},
		{"PC overflow", ins(0x93, 2), func(s *State, r *Runner) { s.Frames[0].PC = 65535 }},
		{"counter overflow", ins(0x93, 2), func(s *State, r *Runner) { s.AutoSubsteps = math.MaxInt }},
		{"negative combat work", ins(0x7f, 1, 1, 2), func(s *State, r *Runner) { s.CombatWork = -1 }},
		{"negative media work", ins(0x7f, 1, 1, 2), func(s *State, r *Runner) { s.MediaWork = -1 }},
		{"late unsupported auto", ins(0x7f, 1, 1, 2), func(s *State, r *Runner) {
			r.source.(*cameraPaletteSource).autoSource[1] = ins(1)
			r.source.(*cameraPaletteSource).autoSource[2] = ins(0x7fff)
		}},
		{"implicit fade unavailable", ins(0x7f, 0, 0, 0), func(s *State, r *Runner) { r.source.(*cameraPaletteSource).available = false }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, r := cameraAutoFixture(autoSource{1: ins(1)})
			tc.alter(&s, r)
			before := detachPartyState(s)
			updates := make([]WorldUpdateRecord, 1, 5)
			updates[0].Tick = 77
			step := TraceStep{Effect: "untouched", WorldUpdates: updates}
			beforeStep := step
			_, err := r.evaluateCamera(&s, &step, tc.inst, nil)
			if err == nil || !reflect.DeepEqual(s, before) || !reflect.DeepEqual(step, beforeStep) || step.WorldUpdates[:cap(step.WorldUpdates)][1].Tick != 0 {
				t.Fatalf("failed camera/fade leaked state/evidence: err=%v state=%+v step=%+v", err, s, step)
			}
		})
	}
	if handled, err := (*Runner)(nil).evaluateCamera(nil, nil, ins(0x7fff), nil); handled || err != nil {
		t.Fatal("unrelated opcode handled", handled, err)
	}
}

func TestCameraAuditBlocksAutomaticWorkButAllowsPureViewportWrites(t *testing.T) {
	for _, tc := range []struct {
		inst    Instruction
		blocked bool
	}{
		{ins(0x7f, 0, 0, 0), false}, {ins(0x7f, 1, 2, 65535), false}, {ins(0x7f, 1, 2, 0), true}, {ins(0x93, 32767), true},
	} {
		if !supported(tc.inst.Opcode) {
			t.Fatal("camera opcode not registered")
		}
		report := Audit([]Instruction{{}, tc.inst, ins(0)}, 1)
		if (len(report.Blockers) > 0) != tc.blocked || (len(report.Reachable) == 1) != tc.blocked {
			t.Fatal("audit expanded automatic work as a no-op", tc, report)
		}
	}
}
