package palflow

import (
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"
)

func TestPaletteTransitionExactLoopOrderAndPresentation(t *testing.T) {
	s := cameraFixture()
	s.World.ScreenWave, s.World.WaveProgression, s.World.ScreenShakeTime = 10, 3, 60
	source := &cameraPaletteSource{available: true}
	r := &Runner{source: source, options: Options{MaxSteps: 33}}
	step := TraceStep{}
	handled, err := r.evaluateInterpreted(&s, &step, ins(0x80, 0), nil)
	if !handled || err != nil {
		t.Fatal(err)
	}
	p := step.PaletteTransition
	if p == nil || p.Palette != 2 || p.Night || p.PaletteLookup != "available" || !p.UpdateScene || p.Denominator != 31 || p.PaletteWrites != 32 || len(p.Coefficients) != 32 || p.ScreenUpdates != 33 || !p.ClearKeysEachFrame || !p.ClearDirectionsEachFrame || p.FrameDelayMilliseconds != 100 {
		t.Fatalf("wrong transition record: %+v", p)
	}
	for i, c := range p.Coefficients {
		if int(c) != i {
			t.Fatal("interpolation endpoints differ from i/31", p.Coefficients)
		}
	}
	if len(source.queries) != 2 || source.queries[0] != [2]uint16{2, 0} || source.queries[1] != [2]uint16{2, 0} {
		t.Fatal("target/new-night and implicit fade palette lookup order", source.queries)
	}
	if s.World.NightPalette || s.World.NeedToFadeIn || s.World.ScreenTint != "" || s.World.ScreenWave != 106 || s.World.WavePhase != 0 || s.World.ScreenShakeTime != 27 || s.World.FrameNum != 132 || s.WorldTicks != 32 || s.Frames[0].PC != 0x1235 || len(step.WorldUpdates) != 32 || step.Ticks != 32 {
		t.Fatalf("wrong scene side effects: state=%+v step=%+v", s, step)
	}
	for i, update := range step.WorldUpdates {
		wantScreens := 1
		if i == 0 {
			wantScreens = 2
		}
		if update.Tick != uint16(i+1) || update.BeforeFrame != 100+uint32(i) || update.AfterFrame != 101+uint32(i) || update.ScreenUpdates != wantScreens {
			t.Fatal("missing ordered actual tick evidence", update)
		}
	}
}

func TestPaletteTransitionNoSceneUpdatesStillChecksPaletteAndWritesAllSteps(t *testing.T) {
	for _, operand := range []uint16{1, 2, 65535} {
		s := cameraFixture()
		s.World.Scenes, s.World.Events = nil, nil
		s.World.ScreenWave, s.World.WaveProgression, s.World.ScreenShakeTime = 10, 3, 9
		r := &Runner{source: &cameraPaletteSource{available: true}, options: Options{MaxSteps: 1}}
		step := TraceStep{}
		_, err := r.evaluateInterpreted(&s, &step, ins(0x80, operand), nil)
		if err != nil {
			t.Fatal(err)
		}
		p := step.PaletteTransition
		if s.World.NightPalette || !s.World.NeedToFadeIn || s.World.ScreenTint != "" || s.World.ScreenWave != 10 || s.World.ScreenShakeTime != 9 || s.WorldTicks != 0 || s.World.FrameNum != 100 || p.UpdateScene || p.PaletteWrites != 32 || p.ScreenUpdates != 0 || p.ClearKeysEachFrame || p.ClearDirectionsEachFrame || p.FrameDelayMilliseconds != 25 || len(step.WorldUpdates) != 0 {
			t.Fatal("no-update interpolation advanced world or omitted writes", s, step)
		}
	}
}

func TestPaletteTransitionUnavailablePaletteKeepsOuterToggleAndNoOtherWork(t *testing.T) {
	for _, operand := range []uint16{0, 1} {
		s := cameraFixture()
		s.World.Scenes, s.World.Events = nil, nil
		before := detachPartyState(s)
		r := &Runner{source: &cameraPaletteSource{available: false}, options: Options{MaxSteps: 1}}
		step := TraceStep{}
		_, err := r.evaluateInterpreted(&s, &step, ins(0x80, operand), nil)
		before.World.NightPalette = false
		before.Frames[0].PC++
		p := step.PaletteTransition
		if err != nil || !reflect.DeepEqual(s, before) || p.PaletteLookup != "unavailable" || p.PaletteWrites != 0 || len(p.Coefficients) != 0 || p.ScreenUpdates != 0 || p.ClearKeysEachFrame || step.Ticks != 0 {
			t.Fatal("NULL palette must return after the opcode's toggle", err, s, step)
		}
	}
}

func TestPaletteTransitionSharedNestedBudgetsAndAtomicFailures(t *testing.T) {
	for _, budget := range []int{64, 65} {
		s, r := cameraAutoFixture(autoSource{1: ins(4, 10), 10: ins(6, 100, 14), 11: ins(0xffff, 5), 12: ins(0x49, 0xffff, 65535), 13: ins(1)})
		s.ExecutedSteps, s.PlayerSubsteps, s.WorldTicks, s.AutoSubsteps, s.CombatWork, s.MediaWork = 2, 3, 4, 5, 6, 7
		r.options.MaxSteps = budget
		before := detachPartyState(s)
		step := TraceStep{Effect: "untouched"}
		_, err := r.evaluateInterpreted(&s, &step, ins(0x80, 0), []Input{{Kind: InputRandom, Value: 1}})
		if budget == 64 {
			if err == nil || !strings.Contains(err.Error(), "budget") || !reflect.DeepEqual(s, before) || !reflect.DeepEqual(step, TraceStep{Effect: "untouched"}) {
				t.Fatal("failed transition leaked callback prefix", err, s, step)
			}
			continue
		}
		if err != nil || s.InputIndex != 1 || s.TextBytes != 4 || s.ExecutedSteps != 6 || s.WorldTicks != 36 || s.AutoSubsteps != 6 || s.MediaWork != 7 || s.CombatWork != 6 || s.World.Events[2].State != -1 || s.World.Events[2].AutoScript != 2 || s.Frames[0].PC != 0x1235 || len(step.WorldUpdates) != 32 {
			t.Fatal("shared callback budget/evidence mismatch", err, s, step)
		}
		calls := step.WorldUpdates[0].AutoSteps
		if len(calls) != 1 || len(calls[0].TriggerSteps) != 4 || calls[0].TriggerSteps[0].Input.Index != 0 || calls[0].TriggerSteps[1].Message.Text != "abcd" {
			t.Fatal("nested input/message evidence missing", step)
		}
	}
	for _, tc := range []struct {
		name string
		edit func(*State, *Runner)
	}{
		{"unknown palette", func(s *State, r *Runner) { r.source = autoSource{} }},
		{"malformed palette", func(s *State, r *Runner) { r.source.(*cameraPaletteSource).lookupErr = errors.New("bad PAT") }},
		{"late unsupported auto", func(s *State, r *Runner) {
			r.source.(*cameraPaletteSource).autoSource = autoSource{1: ins(1), 2: ins(0x7fff)}
		}},
		{"negative media work", func(s *State, r *Runner) { s.MediaWork = -1 }},
		{"overflow combat work", func(s *State, r *Runner) { s.CombatWork = math.MaxInt }},
		{"PC overflow", func(s *State, r *Runner) { s.Frames[0].PC = 65535 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, r := cameraAutoFixture(autoSource{1: ins(1)})
			tc.edit(&s, r)
			before := detachPartyState(s)
			step := TraceStep{Effect: "untouched"}
			_, err := r.evaluateInterpreted(&s, &step, ins(0x80, 0), nil)
			if err == nil || !reflect.DeepEqual(s, before) || !reflect.DeepEqual(step, TraceStep{Effect: "untouched"}) {
				t.Fatal("transition validation/failure was not atomic", err)
			}
		})
	}
}

func TestPaletteTransitionRetainsInitialRequestIdentityAcrossAutoCallbacks(t *testing.T) {
	s, r := cameraAutoFixture(autoSource{1: ins(0x8b, 9), 2: ins(0x54), 3: ins(0)})
	step := TraceStep{}
	_, err := r.evaluateInterpreted(&s, &step, ins(0x80, 0), nil)
	if err != nil || s.World.Palette != 9 || !s.World.NightPalette || step.PaletteTransition.Palette != 2 || step.PaletteTransition.Night || s.World.NeedToFadeIn || !strings.Contains(step.PaletteTransition.ColorBufferModel, "shared static RGB buffer") {
		t.Fatal("transition omitted its initial request identity, RGB limit or callback writes", err, s, step)
	}
	s, r = cameraAutoFixture(autoSource{1: ins(4, 10), 10: ins(0x49, 0xffff, 65535), 11: ins(0x80, 1), 12: ins(1)})
	step = TraceStep{}
	_, err = r.evaluateInterpreted(&s, &step, ins(0x80, 0), nil)
	if err != nil || !s.World.NightPalette || len(step.WorldUpdates[0].AutoSteps[0].TriggerSteps) != 3 || step.WorldUpdates[0].AutoSteps[0].TriggerSteps[1].PaletteTransition.PaletteWrites != 32 {
		t.Fatal("nested palette transition did not use shared dispatcher", err, s, step)
	}
}

func TestPaletteTransitionTriggerAndAutoDispatch(t *testing.T) {
	source := &cameraPaletteSource{available: true, autoSource: autoSource{1: ins(0x80, 1), 2: ins(0)}}
	r, err := New(source, Options{MaxSteps: 100})
	if err != nil {
		t.Fatal(err)
	}
	report, err := r.Trace(Request{Entry: 1, World: cameraFixture().World})
	if err != nil || !report.Completed || len(report.Trace) < 1 || report.Trace[0].PaletteTransition == nil {
		t.Fatal("trigger dispatcher bypassed palette transition", err, report)
	}
	s, r2 := cameraAutoFixture(autoSource{1: ins(0x80, 1), 2: ins(0)})
	_, err = r2.evaluateTicks(&s, &TraceStep{}, ins(9, 1), nil)
	if err != nil || s.World.NightPalette || s.World.Events[2].AutoScript != 2 {
		t.Fatal("auto dispatcher bypassed palette transition", err, s)
	}
	if !supported(0x80) || operandRestriction(ins(0x80, 0)) == "" {
		t.Fatal("audit failed to distinguish runtime palette updates")
	}
}
