package palflow

import (
	"fmt"
	"maps"
)

// PaletteTransitionRecord describes PAL_PaletteFade's requested palette and
// loop. Palette/Night identify the initial lookup, not immutable RGB colors:
// PAL_GetPalette uses a shared static color buffer that nested lookups can
// overwrite. This request model does not simulate that RGB aliasing.
// Coefficients are newpalette weights with denominator 31, not brightness.
type PaletteTransitionRecord struct {
	Palette                  uint16  `json:"palette"`
	Night                    bool    `json:"night"`
	PaletteLookup            string  `json:"palette_lookup"`
	ColorBufferModel         string  `json:"color_buffer_model"`
	UpdateScene              bool    `json:"update_scene"`
	Coefficients             []uint8 `json:"coefficients"`
	Denominator              uint8   `json:"denominator"`
	PaletteWrites            int     `json:"palette_writes"`
	ScreenUpdates            int     `json:"screen_updates"`
	ClearKeysEachFrame       bool    `json:"clear_keys_each_frame"`
	ClearDirectionsEachFrame bool    `json:"clear_directions_each_frame"`
	FrameDelayMilliseconds   uint32  `json:"frame_delay_milliseconds"`
}

func paletteTransitionOpcodeName(op uint16) string {
	if op == 0x80 {
		return "toggle day/night and fade palette with bounded optional scene updates"
	}
	return ""
}

// Behavior researched from script.c 0x0080 and palette.c PAL_PaletteFade at
// SDLPAL d4bf69b64d335cd680555af9df90e7ec4f3006a3. This models state and
// synchronous updates. Palette writes, input clearing and delays are requests.
func (r *Runner) evaluatePaletteTransition(state *State, step *TraceStep, inst Instruction, inputs []Input) (bool, error) {
	if inst.Opcode != 0x80 {
		return false, nil
	}
	if r == nil || r.options.MaxSteps < 1 || state == nil || state.World == nil || len(state.Frames) == 0 || step == nil {
		return true, fmt.Errorf("palette transition requires an explicit world, script frame, trace step and positive budget")
	}
	frameIndex := len(state.Frames) - 1
	frame := state.Frames[frameIndex]
	nextPC, err := fallthroughPC(frame.PC)
	if err != nil {
		return true, err
	}
	palettes, ok := r.source.(PaletteAvailabilitySource)
	if !ok {
		return true, fmt.Errorf("palette transition requires explicit palette availability")
	}
	palette, night := state.World.Palette, !state.World.NightPalette
	available, err := palettes.PaletteAvailable(palette, night)
	if err != nil {
		return true, fmt.Errorf("palette transition lookup: %w", err)
	}
	updateScene := inst.Operands[0] == 0
	ticks := 0
	if available && updateScene {
		ticks = 32
	}
	remaining := r.options.MaxSteps
	for _, used := range []int{state.ExecutedSteps, state.PlayerSubsteps, state.WorldTicks, state.AutoSubsteps, state.CombatWork, state.MediaWork, ticks, 1} {
		if used < 0 || used > remaining {
			return true, fmt.Errorf("palette transition exceeds the combined execution budget")
		}
		remaining -= used
	}
	working := *state
	working.World = CloneWorld(state.World)
	working.Frames = append([]Frame(nil), state.Frames...)
	working.IdleFrames = maps.Clone(state.IdleFrames)
	working.World.NightPalette = night
	record := &PaletteTransitionRecord{Palette: palette, Night: night, PaletteLookup: "unavailable", UpdateScene: updateScene, Denominator: 31, ColorBufferModel: "initial lookup identity and interpolation requests only; nested PAL_GetPalette lookups can overwrite the reference's shared static RGB buffer; colors and aliasing are not simulated"}
	evidence := *step
	evidence.WorldUpdates, evidence.AutoSteps = nil, nil
	if available {
		record.PaletteLookup = "available"
		record.FrameDelayMilliseconds = 25
		if updateScene {
			record.FrameDelayMilliseconds = 100
			record.ClearKeysEachFrame, record.ClearDirectionsEachFrame = true, true
		}
		for i := 0; i < 32; i++ {
			record.Coefficients = append(record.Coefficients, uint8(i))
			record.PaletteWrites++
			// VIDEO_SetPalette happens before GameUpdate and MakeScene. A
			// callback can replace the tint after this write, including at i=31.
			working.World.ScreenTint = ""
			if !updateScene {
				continue
			}
			update, err := r.cameraTick(&working, frame, i+1, ticks, inputs)
			if err != nil {
				return true, fmt.Errorf("palette transition at PC %04x, frame %d: %w", frame.PC, i+1, err)
			}
			count, err := r.cameraPresent(working.World)
			if err != nil {
				return true, fmt.Errorf("palette transition frame %d: %w", i+1, err)
			}
			update.ScreenUpdates = count
			record.ScreenUpdates += count
			evidence.WorldUpdates = append(evidence.WorldUpdates, update)
		}
	}
	working.Frames[frameIndex] = frame
	working.Frames[frameIndex].PC = nextPC
	evidence.PaletteTransition = record
	evidence.Ticks = uint16(ticks)
	evidence.Screen = &ScreenRecord{Kind: "palette_fade", Palette: palette, Night: night, Rendered: false, ScreenUpdates: record.ScreenUpdates}
	evidence.Effect = fmt.Sprintf("toggle night to %t; %s target palette, %d interpolation writes and %d trigger-disabled world updates; presentation and key clearing recorded without rendering", night, record.PaletteLookup, record.PaletteWrites, ticks)
	*state, *step = working, evidence
	return true, nil
}
