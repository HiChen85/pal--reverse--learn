package palflow

import (
	"fmt"
	"maps"
)

// CameraPosition retains signed PAL_X/PAL_Y coordinates after each packed
// 16-bit write. Moving the camera compensates screen positions, not the trail.
type CameraPosition struct {
	ViewportX    int `json:"viewport_x"`
	ViewportY    int `json:"viewport_y"`
	PartyOffsetX int `json:"party_offset_x"`
	PartyOffsetY int `json:"party_offset_y"`
}

type CameraFrame struct {
	Sequence int            `json:"sequence"`
	Before   CameraPosition `json:"before"`
	Moved    CameraPosition `json:"moved"`
	After    CameraPosition `json:"after"`
}

type CameraRecord struct {
	Mode          string        `json:"mode"`
	DeltaX        int           `json:"delta_x"`
	DeltaY        int           `json:"delta_y"`
	DisplayFrames int           `json:"display_frames"`
	DelayedFrames int           `json:"delayed_frames"`
	ScreenUpdates int           `json:"screen_updates"`
	Frames        []CameraFrame `json:"frames"`
}

// PaletteAvailabilitySource distinguishes an unavailable reference palette
// from a malformed asset (error). Unknown availability must block a scene fade:
// the reference skips every game update when palette lookup returns NULL.
type PaletteAvailabilitySource interface {
	PaletteAvailable(id uint16, night bool) (bool, error)
}

// SceneFadeRecord describes a checked current-palette lookup.
// Brightness contains the actual 0..63 multiplier, with denominator 64; the
// reference does not add an extra fully bright/black frame after its loop.
type SceneFadeRecord struct {
	SignedStep               int16   `json:"signed_step"`
	EffectiveStep            int     `json:"effective_step"`
	Palette                  uint16  `json:"palette"`
	Night                    bool    `json:"night"`
	PaletteLookup            string  `json:"palette_lookup"`
	ClearKeysEachFrame       bool    `json:"clear_keys_each_frame"`
	ClearDirectionsEachFrame bool    `json:"clear_directions_each_frame"`
	FrameDelayMilliseconds   uint32  `json:"frame_delay_milliseconds"`
	Brightness               []uint8 `json:"brightness"`
	ScreenUpdates            int     `json:"screen_updates"`
}

func cameraOpcodeName(op uint16) string {
	switch op {
	case 0x7f:
		return "move viewport with compensated party positions and bounded auto updates"
	case 0x93:
		return "fade current scene palette with bounded auto updates"
	}
	return ""
}

// evaluateCamera implements the state changes and synchronous game updates of
// 007F and 0093. Display, palette writes, key clearing and wall time remain
// requests. Every update uses the existing trigger-disabled scheduler, and the
// entire instruction commits only after all its nested work succeeds.
func (r *Runner) evaluateCamera(state *State, step *TraceStep, inst Instruction, inputs []Input) (bool, error) {
	if cameraOpcodeName(inst.Opcode) == "" {
		return false, nil
	}
	if state == nil || len(state.Frames) == 0 || state.World == nil || step == nil {
		return true, fmt.Errorf("camera/scene fade requires a script frame, explicit world snapshot and trace step")
	}
	if r == nil || r.options.MaxSteps < 1 {
		return true, fmt.Errorf("camera/scene fade requires a positive execution budget")
	}
	frameIndex := len(state.Frames) - 1
	frame := state.Frames[frameIndex]
	nextPC, err := fallthroughPC(frame.PC)
	if err != nil {
		return true, err
	}
	a := inst.Operands
	ticks := 0
	var fade *SceneFadeRecord
	if inst.Opcode == 0x93 {
		palettes, ok := r.source.(PaletteAvailabilitySource)
		if !ok {
			return true, fmt.Errorf("scene fade requires explicit palette availability")
		}
		available, err := palettes.PaletteAvailable(state.World.Palette, state.World.NightPalette)
		if err != nil {
			return true, fmt.Errorf("scene fade palette lookup: %w", err)
		}
		stride := int(int16(a[0]))
		if stride == 0 {
			stride = 1
		}
		fade = &SceneFadeRecord{SignedStep: int16(a[0]), EffectiveStep: stride, Palette: state.World.Palette, Night: state.World.NightPalette, PaletteLookup: "unavailable", FrameDelayMilliseconds: 100}
		if available {
			fade.PaletteLookup = "available"
			fade.ClearKeysEachFrame, fade.ClearDirectionsEachFrame = true, true
			level := 0
			if stride < 0 {
				level = 63
			}
			for ; level >= 0 && level < 64; level += stride {
				fade.Brightness = append(fade.Brightness, uint8(level))
			}
		}
		ticks = len(fade.Brightness)
	} else if (a[0] != 0 || a[1] != 0) && a[2] != 0xffff {
		ticks = max(1, int(int16(a[2])))
	}
	// Reserve every outer update before nested autos/trigger calls can consume
	// its budget. Subtraction avoids overflow from caller-supplied counters.
	remaining := r.options.MaxSteps
	for _, used := range []int{state.ExecutedSteps, state.PlayerSubsteps, state.WorldTicks, state.AutoSubsteps, state.CombatWork, state.MediaWork, ticks, 1} {
		if used < 0 || used > remaining {
			return true, fmt.Errorf("camera/scene fade exceeds the combined execution budget")
		}
		remaining -= used
	}
	working := *state
	working.World = CloneWorld(state.World)
	working.Frames = append([]Frame(nil), state.Frames...)
	working.IdleFrames = maps.Clone(state.IdleFrames)
	evidence := *step
	evidence.WorldUpdates = nil
	evidence.AutoSteps = nil
	evidence.Camera, evidence.SceneFade = nil, nil
	if fade != nil {
		// PAL_SceneFade clears this before its first update; the opcode writes
		// its final sign-based value after the entire fade returns.
		if ticks > 0 {
			working.World.NeedToFadeIn = false
		}
		for n := 1; n <= ticks; n++ {
			update, err := r.cameraTick(&working, frame, n, ticks, inputs)
			if err != nil {
				return true, fmt.Errorf("scene fade at PC %04x, frame %d: %w", frame.PC, n, err)
			}
			evidence.WorldUpdates = append(evidence.WorldUpdates, update)
			updates, err := r.cameraPresent(working.World)
			if err != nil {
				return true, fmt.Errorf("scene fade frame %d: %w", n, err)
			}
			fade.ScreenUpdates += updates
			// Each palette write supersedes a previously requested color tint.
			working.World.ScreenTint = ""
		}
		working.World.NeedToFadeIn = fade.SignedStep < 0
		evidence.SceneFade = fade
		evidence.Screen = &ScreenRecord{Kind: "scene_fade", Palette: fade.Palette, Night: fade.Night, Rendered: false}
		evidence.Effect = fmt.Sprintf("scene fade with signed step %d and %s palette requests %d trigger-disabled world updates; record key clearing and palette writes without rendering", fade.SignedStep, fade.PaletteLookup, ticks)
	} else {
		record := &CameraRecord{Mode: "relative", DeltaX: int(int16(a[0])), DeltaY: int(int16(a[1])), DisplayFrames: ticks, DelayedFrames: ticks}
		frames := ticks
		if a[0] == 0 && a[1] == 0 {
			record.Mode, record.DisplayFrames, frames = "restore", 1, 1
			if a[2] == 0xffff {
				record.DisplayFrames = 0
			}
		} else if a[2] == 0xffff {
			record.Mode, record.DisplayFrames, frames = "absolute", 1, 1
			record.DelayedFrames = 1
		}
		for n := 1; n <= frames; n++ {
			w := working.World
			count, err := cameraPartyCount(w)
			if err != nil {
				return true, fmt.Errorf("viewport frame %d: %w", n, err)
			}
			entry := CameraFrame{Sequence: n, Before: cameraPosition(w)}
			dx, dy := record.DeltaX, record.DeltaY
			switch record.Mode {
			case "restore":
				dx, dy = int(int16(w.PartyPositions[0].X))-160, int(int16(w.PartyPositions[0].Y))-112
				w.ViewportX, w.ViewportY = int(int16(w.ViewportX+dx)), int(int16(w.ViewportY+dy))
				w.PartyOffsetX, w.PartyOffsetY = 160, 112
				record.DeltaX, record.DeltaY = dx, dy
			case "absolute":
				x, y := int(int16(int(a[0])*32-160)), int(int16(int(a[1])*16-112))
				dx, dy = x-w.ViewportX, y-w.ViewportY
				w.ViewportX, w.ViewportY = x, y
				record.DeltaX, record.DeltaY = dx, dy
			default:
				w.ViewportX, w.ViewportY = int(int16(w.ViewportX+dx)), int(int16(w.ViewportY+dy))
				w.PartyOffsetX, w.PartyOffsetY = int(int16(w.PartyOffsetX-dx)), int(int16(w.PartyOffsetY-dy))
			}
			for member := 0; member < count; member++ {
				w.PartyPositions[member].X -= uint16(dx)
				w.PartyPositions[member].Y -= uint16(dy)
			}
			entry.Moved = cameraPosition(w)
			if ticks > 0 {
				update, err := r.cameraTick(&working, frame, n, ticks, inputs)
				if err != nil {
					return true, fmt.Errorf("viewport at PC %04x, frame %d: %w", frame.PC, n, err)
				}
				evidence.WorldUpdates = append(evidence.WorldUpdates, update)
			}
			if record.DisplayFrames > 0 {
				updates, err := r.cameraPresent(working.World)
				if err != nil {
					return true, fmt.Errorf("viewport frame %d: %w", n, err)
				}
				record.ScreenUpdates += updates
			}
			entry.After = cameraPosition(working.World)
			record.Frames = append(record.Frames, entry)
		}
		evidence.Camera = record
		evidence.Screen = &ScreenRecord{Kind: "viewport", Rendered: false}
		evidence.Effect = fmt.Sprintf("%s viewport with compensated party/follower screen positions, %d trigger-disabled world updates and %d display requests; preserve trail and party gestures", record.Mode, ticks, record.DisplayFrames)
	}
	working.Frames[frameIndex] = frame
	working.Frames[frameIndex].PC = nextPC
	evidence.Ticks = uint16(ticks)
	*state, *step = working, evidence
	return true, nil
}

func cameraPosition(w *WorldState) CameraPosition {
	return CameraPosition{ViewportX: w.ViewportX, ViewportY: w.ViewportY, PartyOffsetX: w.PartyOffsetX, PartyOffsetY: w.PartyOffsetY}
}

func (r *Runner) cameraPresent(w *WorldState) (int, error) {
	palettes, _ := r.source.(PaletteAvailabilitySource)
	return ApplyScenePresentation(w, palettes)
}

// ApplyScenePresentation performs the persistent changes for one MakeScene
// followed by a full-screen display request. It validates a pending implicit
// fade before mutating world, advances wave/fade/shake state, and returns the
// number of screen requests including the extra request made by that fade.
// Pixels and wall time are not produced. A nil palette source is permitted
// only when no fade-in is pending.
func ApplyScenePresentation(w *WorldState, palettes PaletteAvailabilitySource) (int, error) {
	if w == nil {
		return 0, fmt.Errorf("scene presentation requires an explicit world snapshot")
	}
	if w.NeedToFadeIn {
		if palettes == nil {
			return 0, fmt.Errorf("scene redraw's pending fade-in requires explicit palette availability")
		}
		available, err := palettes.PaletteAvailable(w.Palette, w.NightPalette)
		if err != nil {
			return 0, fmt.Errorf("scene redraw palette lookup: %w", err)
		}
		if !available {
			return 0, fmt.Errorf("scene redraw's pending fade-in palette %d is unavailable", w.Palette)
		}
	}
	updates := 1 + applyScenePresentation(w)
	advanceScreenRequests(w, updates)
	return updates, nil
}

func cameraPartyCount(w *WorldState) (int, error) {
	count := len(w.PartyRoles) + len(w.FollowerSprites)
	if len(w.PartyRoles) < 1 || len(w.PartyRoles) > 5 || len(w.FollowerSprites) > 2 || count > 5 || len(w.PartyPositions) < count {
		return 0, fmt.Errorf("viewport requires complete party/follower screen positions for 1..5 members")
	}
	for _, coordinate := range []int{w.ViewportX, w.ViewportY, w.PartyOffsetX, w.PartyOffsetY} {
		if !signedPartyCoordinate(coordinate) {
			return 0, fmt.Errorf("viewport and party offsets must be signed 16-bit coordinates")
		}
	}
	return count, nil
}

func (r *Runner) cameraTick(state *State, frame Frame, sequence, total int, inputs []Input) (WorldUpdateRecord, error) {
	before := state.World.FrameNum
	limit := *r
	// evaluateTicks reserves its current tick and instruction. Lowering the
	// available total also reserves camera/fade frames still to come.
	limit.options.MaxSteps -= total - sequence
	tick := TraceStep{}
	if _, err := limit.evaluateTicksCore(state, &tick, Instruction{Opcode: 9, Operands: [3]uint16{1}}, inputs, false); err != nil {
		return WorldUpdateRecord{}, err
	}
	state.Frames[len(state.Frames)-1] = frame
	for i := range tick.AutoSteps {
		tick.AutoSteps[i].Tick = uint16(sequence)
	}
	return WorldUpdateRecord{Tick: uint16(sequence), BeforeFrame: before, AfterFrame: state.World.FrameNum, AutoSteps: tick.AutoSteps}, nil
}
