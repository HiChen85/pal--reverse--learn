package palflow

import (
	"fmt"
	"reflect"
	"testing"
)

type mediaSource struct {
	autoSource
	profile    string
	palette    bool
	paletteErr error
	archiveErr error
	rng        func(uint16, uint32) (RNGFrameResource, error)
	fbp        func(uint16) (FBPResource, error)
	cd         func(int16) (CDPlaybackResult, error)
}

func (s mediaSource) MediaProfile() (string, error)               { return s.profile, nil }
func (s mediaSource) RNGArchiveIdentity() (string, error)         { return "archive", s.archiveErr }
func (s mediaSource) PaletteAvailable(uint16, bool) (bool, error) { return s.palette, s.paletteErr }
func (s mediaSource) RNGFrameResource(movie uint16, frame uint32) (RNGFrameResource, error) {
	if s.rng == nil {
		return RNGFrameResource{}, fmt.Errorf("RNG unknown")
	}
	return s.rng(movie, frame)
}
func (s mediaSource) FBPResource(id uint16) (FBPResource, error) {
	if s.fbp == nil {
		return FBPResource{}, fmt.Errorf("FBP unknown")
	}
	return s.fbp(id)
}
func (s mediaSource) CDPlayback(track int16) (CDPlaybackResult, error) {
	if s.cd == nil {
		return CDPlaybackResult{}, fmt.Errorf("CD unknown")
	}
	return s.cd(track)
}
func mediaState() State {
	return State{World: &WorldState{FrameNum: 99, CurrentRNG: 3, CurrentRNGKnown: true}, Frames: []Frame{{PC: 1}}, InputIndex: 2, ScriptSuccess: true}
}
func mediaRunner(source Source, limit int) *Runner {
	return &Runner{source: source, options: Options{MaxSteps: limit}}
}
func applyMedia(t *testing.T, r *Runner, s *State, inst Instruction) TraceStep {
	t.Helper()
	step := TraceStep{PC: s.Frames[0].PC}
	ok, err := r.evaluateMedia(s, &step, inst)
	if !ok || err != nil {
		t.Fatalf("media %04x: %v %v", inst.Opcode, ok, err)
	}
	return step
}
func playableMedia(movie uint16, frame uint32) (RNGFrameResource, error) {
	status := "playable"
	if frame >= 6 {
		status = "missing_frame"
	}
	return RNGFrameResource{Movie: movie, Frame: frame, FrameCount: 6, Status: status, ArchiveSHA256: "archive", SHA256: "decoded", CompressedSize: 21, DecodedSize: 1}, nil
}

func TestRNGPlaybackInclusiveBoundsAndPersistentFlags(t *testing.T) {
	for _, tc := range []struct {
		name              string
		start, end, speed uint16
		frames, work      int
		stop              bool
	}{{"inclusive", 2, 4, 0, 3, 3, false}, {"until missing", 3, 0, 20, 3, 4, true}, {"empty equality", 4, 3, 1, 0, 0, false}, {"start after endpoint", 4, 2, 1, 2, 3, true}, {"missing first", 8, 0, 1, 0, 1, true}} {
		t.Run(tc.name, func(t *testing.T) {
			s := mediaState()
			s.World.NeedToFadeIn = true
			s.World.ScreenShakeTime = 10
			source := mediaSource{palette: true, rng: playableMedia}
			if tc.frames == 0 {
				source.paletteErr = fmt.Errorf("palette must not be read before the first successful frame")
			}
			step := applyMedia(t, mediaRunner(source, 100), &s, ins(0x37, tc.start, tc.end, tc.speed))
			p := step.Media.RNG
			if len(p.Frames) != tc.frames || s.MediaWork != tc.work || (p.Stop != nil) != tc.stop || p.FadeInRequested != (tc.frames > 0) || s.World.NeedToFadeIn != (tc.frames == 0) {
				t.Fatalf("playback %+v state %+v", p, s)
			}
			if s.World.FrameNum != 99 || s.WorldTicks != 0 || s.InputIndex != 2 || s.World.ScreenShakeTime != uint16(10-tc.frames) {
				t.Fatal("RNG advanced world, input or wrong screen count")
			}
			if tc.speed == 0 && (p.Speed != 16 || p.FrameDelayMilliseconds != 62.5) {
				t.Fatalf("default speed %+v", p)
			}
		})
	}
}

func TestMediaFailureIsAtomic(t *testing.T) {
	for _, tc := range []struct {
		name   string
		source Source
		limit  int
		mutate func(*State)
		inst   Instruction
	}{
		{"unknown movie", mediaSource{palette: true, rng: playableMedia}, 100, func(s *State) { s.World.CurrentRNGKnown = false }, ins(0x37, 0, 3)},
		{"no RNG", autoSource{}, 100, nil, ins(0x37, 0, 3)},
		{"empty range still opens archive", mediaSource{archiveErr: fmt.Errorf("archive missing")}, 100, nil, ins(0x37, 4, 3)},
		{"late resource error", mediaSource{palette: true, rng: func(movie uint16, frame uint32) (RNGFrameResource, error) {
			if frame == 2 {
				return RNGFrameResource{}, fmt.Errorf("unsafe frame")
			}
			return playableMedia(movie, frame)
		}}, 100, nil, ins(0x37, 0, 3)},
		{"unknown palette", mediaSource{rng: playableMedia}, 100, func(s *State) { s.World.NeedToFadeIn = true }, ins(0x37, 0, 3)},
		{"RNG budget", mediaSource{palette: true, rng: playableMedia}, 3, nil, ins(0x37, 0, 3)},
		{"unknown device", mediaSource{}, 100, nil, ins(0xa3, 1, 2)},
		{"unknown FBP profile", mediaSource{}, 100, nil, ins(0x76, 1, 2)},
		{"no scene identity", mediaSource{}, 100, nil, ins(0x9b)},
		{"missing color palette", mediaSource{}, 100, nil, ins(0x8c, 4, 1)},
		{"backup counter overflow", mediaSource{}, 100, func(s *State) { s.World.ScreenBackupRequests = ^uint64(0) }, ins(0xa6)},
		{"scene palette unavailable", mediaSource{}, 100, func(s *State) {
			s.World.NeedToFadeIn = true
			s.World.SceneResources = &SceneResourceState{MapID: 1, MapSHA256: "map", GOPSHA256: "gop", MAPArchiveSHA256: "maps", GOPArchiveSHA256: "gops"}
		}, ins(0x9b)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := mediaState()
			if tc.mutate != nil {
				tc.mutate(&s)
			}
			before := s
			before.World = CloneWorld(s.World)
			before.Frames = append([]Frame(nil), s.Frames...)
			step := TraceStep{Effect: "unchanged"}
			original := step
			ok, err := mediaRunner(tc.source, tc.limit).evaluateMedia(&s, &step, tc.inst)
			if !ok || err == nil || !reflect.DeepEqual(s, before) || !reflect.DeepEqual(step, original) {
				t.Fatalf("failure committed state/evidence: %v %v %+v", ok, err, s)
			}
		})
	}
}

func TestScenePresentationWavePhaseAndCounterSaturation(t *testing.T) {
	for _, tc := range []struct {
		wave         uint16
		progress     int16
		phase        uint8
		wantWave     uint16
		wantProgress int16
		wantPhase    uint8
	}{{1, -2, 7, 0, 0, 7}, {255, 1, 11, 0, 0, 11}, {2, -1, 31, 1, -1, 0}, {10, 2, 2, 12, 2, 3}} {
		w := &WorldState{ScreenWave: tc.wave, WaveProgression: tc.progress, WavePhase: tc.phase, NeedToFadeIn: true, ScreenTint: "red", ScreenShakeTime: 2}
		if extra := applyScenePresentation(w); extra != 1 {
			t.Fatalf("fade screen updates=%d", extra)
		}
		if w.ScreenWave != tc.wantWave || w.WaveProgression != tc.wantProgress || w.WavePhase != tc.wantPhase || w.NeedToFadeIn || w.ScreenTint != "" {
			t.Fatalf("wave/fade state %+v", w)
		}
		advanceScreenRequests(w, 3)
		if w.ScreenShakeTime != 0 {
			t.Fatal("shake counter underflow")
		}
	}
}

func TestShakeSelectionAndBackupRequests(t *testing.T) {
	s := mediaState()
	r := mediaRunner(mediaSource{}, 100)
	step := applyMedia(t, r, &s, ins(0x35, 7, 0))
	if s.World.ScreenShakeTime != 7 || s.World.ScreenShakeLevel != 4 || step.Media.ScreenUpdates != 0 {
		t.Fatal("shake configuration differs")
	}
	advanceScreenRequests(s.World, 2)
	if s.World.ScreenShakeTime != 5 {
		t.Fatal("shake did not consume displays")
	}
	step = applyMedia(t, r, &s, ins(0x35, 0, 9))
	if s.World.ScreenShakeTime != 0 || s.World.ScreenShakeLevel != 9 || step.Media.ScreenUpdates != 1 {
		t.Fatal("stop shake must request immediate update")
	}
	applyMedia(t, r, &s, ins(0x36, 0))
	if !s.World.CurrentRNGKnown || s.World.CurrentRNG != 0 {
		t.Fatal("movie zero selection lost")
	}
	step = applyMedia(t, r, &s, ins(0xa6))
	if s.World.ScreenBackupRequests != 1 || !step.Media.BackupRequested || step.Media.ScreenUpdates != 0 {
		t.Fatal("backup falsely displayed")
	}
}

func TestFBPProfileFadeWrapAndEffectReset(t *testing.T) {
	for _, tc := range []struct {
		profile string
		fade    uint16
		updates int
		delay   uint16
	}{{"dos", 2, 97, 30}, {"dos", 65535, 97, 0}, {"dos", 0, 1, 0}, {"windows", 2, 1, 0}} {
		s := mediaState()
		s.World.EndingEffectSprite = 7
		s.World.NeedToFadeIn = true
		r := mediaRunner(mediaSource{profile: tc.profile, fbp: func(id uint16) (FBPResource, error) {
			return FBPResource{ID: id, Status: "black_missing_picture", ArchiveSHA256: "source"}, nil
		}}, 100)
		step := applyMedia(t, r, &s, ins(0x76, 49, tc.fade))
		f := step.Media.FBP
		if step.Media.ScreenUpdates != tc.updates || f.FrameDelayMilliseconds != tc.delay || !f.BlackScreen || !s.World.NeedToFadeIn {
			t.Fatalf("FBP %+v", step.Media)
		}
		if tc.profile == "dos" && (s.World.EndingEffectSprite != 0 || !f.FinalBlitSkipped) {
			t.Fatal("DOS effect reset/hack lost")
		}
		if tc.profile == "windows" && s.World.EndingEffectSprite != 7 {
			t.Fatal("Windows branch reset DOS effect")
		}
	}
}

func TestColorFadeAndSceneComposition(t *testing.T) {
	s := mediaState()
	s.World.NeedToFadeIn = true
	s.World.Palette = 4
	s.World.NightPalette = true
	r := mediaRunner(mediaSource{palette: true}, 100)
	step := applyMedia(t, r, &s, ins(0x8c, 0x123, 0, 0))
	if s.World.NeedToFadeIn || s.World.ScreenTint != "palette_color:35" || step.Media.ColorFade.Color != 35 || step.Media.ColorFade.PaletteWrites != 65 || step.Media.ColorFade.FrameDelayMilliseconds != 10 {
		t.Fatalf("color fade %+v", step.Media)
	}
	applyMedia(t, r, &s, ins(0x8c, 1, 2, 1))
	if s.World.ScreenTint != "" {
		t.Fatal("fade from color did not restore palette")
	}
	s.World.SceneResources = &SceneResourceState{MapID: 1, MapSHA256: "map", GOPSHA256: "gop", MAPArchiveSHA256: "maps", GOPArchiveSHA256: "gops"}
	s.World.ScreenWave = 255
	s.World.WaveProgression = 1
	s.World.NeedToFadeIn = true
	s.World.ScreenShakeTime = 100
	step = applyMedia(t, r, &s, ins(0x9b, 123, 456, 789))
	if step.Media.FadeSpeed != 2 || step.Media.ScreenUpdates != 74 || !step.Media.BackupRequested || !step.Media.SceneCompositionRequested || s.World.ScreenWave != 0 || s.World.WaveProgression != 0 || s.World.NeedToFadeIn || s.World.ScreenShakeTime != 26 || s.World.FrameNum != 99 {
		t.Fatalf("scene composition %+v %+v", step.Media, s.World)
	}
}

func TestCDTrackNormalizationAndExplicitFallback(t *testing.T) {
	for _, resolution := range []CDPlaybackResult{{Basis: "explicit no-CD model"}, {Available: true, Basis: "available device; playback rejected"}, {Available: true, PlaySucceeded: true, Basis: "explicit successful device outcome"}} {
		s := mediaState()
		r := mediaRunner(mediaSource{cd: func(track int16) (CDPlaybackResult, error) {
			if track != -2 {
				t.Errorf("track=%d", track)
			}
			return resolution, nil
		}}, 100)
		step := applyMedia(t, r, &s, ins(0xa3, 65535, 8))
		fallback := !resolution.Available || !resolution.PlaySucceeded
		if step.Media.CD.FallbackRequested != fallback || !s.MusicKnown || s.Music != 8 || s.World.Music != 8 || !step.Audio.PlaybackRequested || step.Audio.Executed {
			t.Fatalf("CD resolution %+v %+v", step.Media, step.Audio)
		}
		if fallback && (step.Audio.ID != 8 || !step.Audio.Loop || step.Audio.Kind != "music") {
			t.Fatal("fallback music request wrong")
		}
	}
}

func TestMediaBudgetIncludesEarlierWorkAtExactBoundary(t *testing.T) {
	for _, limit := range []int{29, 30} {
		s := mediaState()
		s.ExecutedSteps, s.PlayerSubsteps, s.WorldTicks, s.AutoSubsteps, s.CombatWork, s.MediaWork = 2, 6, 3, 4, 5, 7
		before := s
		before.World = CloneWorld(s.World)
		before.Frames = append([]Frame(nil), s.Frames...)
		step := TraceStep{}
		_, err := mediaRunner(mediaSource{rng: playableMedia}, limit).evaluateMedia(&s, &step, ins(0x37, 0, 1))
		if limit == 29 {
			if err == nil || !reflect.DeepEqual(s, before) {
				t.Fatal("RNG validation exceeded shared budget or committed failure")
			}
		} else if err != nil || s.MediaWork != 9 {
			t.Fatalf("exact shared budget rejected: %v %+v", err, s)
		}
	}
}

func TestMediaSharedTriggerAndAutoDispatch(t *testing.T) {
	source := mediaSource{autoSource: autoSource{1: ins(0x36, 4), 2: ins(0x37, 0, 1, 0), 3: ins(0x35, 10, 0), 4: ins(0)}, rng: playableMedia, palette: true}
	r, err := New(source, Options{MaxSteps: 1000})
	if err != nil {
		t.Fatal(err)
	}
	report, err := r.Trace(Request{Entry: 1, World: &WorldState{FrameNum: 99}})
	if err != nil || !report.Completed || report.State.World.CurrentRNG != 4 || report.State.MediaWork != 2 || report.State.World.FrameNum != 99 || report.State.World.ScreenShakeTime != 10 || len(report.Trace) != 4 {
		t.Fatalf("trigger media %+v %v", report, err)
	}
	for _, step := range report.Trace[:3] {
		if step.Media == nil {
			t.Fatalf("media request missing from trigger trace %+v", step)
		}
	}
	state, autoRunner := autoFixture(source.autoSource)
	autoRunner.source = source
	step := waitAuto(t, autoRunner, &state, 3)
	if len(step.AutoSteps) != 3 || state.MediaWork != 2 || state.World.CurrentRNG != 4 || state.World.Events[2].AutoScript != 4 || state.World.FrameNum != 103 {
		t.Fatalf("auto media state %+v %+v", state, step)
	}
	for _, child := range step.AutoSteps {
		if child.Instruction == nil || child.Instruction.Media == nil {
			t.Fatalf("media request missing from auto trace %+v", child)
		}
	}
	for _, opcode := range []uint16{0x35, 0x36, 0x37, 0x76, 0x8c, 0x9b, 0xa3, 0xa6} {
		if !supported(opcode) {
			t.Errorf("media opcode %04x missing from modeled inventory", opcode)
		}
	}
}
