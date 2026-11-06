package palflow

import "fmt"

func presentationOpcodeName(op uint16) string {
	switch op {
	case 0x43:
		return "set music and request playback"
	case 0x45:
		return "set battle music"
	case 0x47:
		return "request sound effect"
	case 0x4d:
		return "wait for explicit key acknowledgement"
	case 0x4f:
		return "request red fade"
	case 0x50:
		return "fade out and request later fade in"
	case 0x51:
		return "fade in current palette"
	case 0x73:
		return "request scene fade"
	case 0x77:
		return "stop music and clear current music configuration"
	case 0x85:
		return "request a wall-clock delay without world updates"
	}
	return ""
}

func (r *Runner) evaluatePresentation(s *State, step *TraceStep, inst Instruction, inputs []Input) (bool, error) {
	if presentationOpcodeName(inst.Opcode) == "" {
		return false, nil
	}
	if s == nil || len(s.Frames) == 0 || step == nil {
		return true, fmt.Errorf("presentation instruction needs state, frame and step")
	}
	i := len(s.Frames) - 1
	next, err := fallthroughPC(s.Frames[i].PC)
	if err != nil {
		return true, err
	}
	w := CloneWorld(s.World)
	a := inst.Operands
	evidence := *step
	switch inst.Opcode {
	case 0x43:
		s.Music, s.MusicKnown = a[0], true
		if w != nil {
			w.Music, w.MusicKnown = a[0], true
		}
		audio := &AudioRecord{Kind: "music", ID: a[0], Loop: a[1] != 1, PlaybackRequested: true}
		if a[1] == 3 && a[0] != 9 {
			audio.FadeSeconds = 3
		}
		evidence.Audio = audio
		evidence.Screen = &ScreenRecord{Kind: "clear_dialog", ClearDialogRequested: true}
	case 0x45:
		s.BattleMusic, s.BattleMusicKnown = a[0], true
		if w != nil {
			w.BattleMusic, w.BattleMusicKnown = a[0], true
		}
		evidence.Audio = &AudioRecord{Kind: "battle_music_config", ID: a[0]}
		evidence.Screen = &ScreenRecord{Kind: "clear_dialog", ClearDialogRequested: true}
	case 0x47:
		evidence.Audio = &AudioRecord{Kind: "sound", ID: a[0], PlaybackRequested: true}
		evidence.Screen = &ScreenRecord{Kind: "clear_dialog", ClearDialogRequested: true}
	case 0x4d:
		use, err := consume(s, inputs, InputKey)
		if err != nil {
			return true, err
		}
		evidence.Input = use
		evidence.Screen = &ScreenRecord{Kind: "wait_key"}
	case 0x4f, 0x50, 0x51:
		if w == nil {
			return true, fmt.Errorf("fade needs an explicit world palette and fade state")
		}
		delay := a[0]
		switch inst.Opcode {
		case 0x4f:
			w.ScreenTint = "red"
			evidence.Screen = &ScreenRecord{Kind: "fade_red"}
		case 0x50:
			if delay == 0 {
				delay = 1
			}
			w.NeedToFadeIn = true
			evidence.Screen = &ScreenRecord{Kind: "fade_out", Delay: delay}
		case 0x51:
			if int16(delay) <= 0 {
				delay = 1
			}
			w.NeedToFadeIn = false
			w.ScreenTint = ""
			evidence.Screen = &ScreenRecord{Kind: "fade_in", Delay: delay, Palette: w.Palette, Night: w.NightPalette}
		}
	case 0x73:
		evidence.Screen = &ScreenRecord{Kind: "fade_scene", Delay: a[0]}
	case 0x77:
		s.Music, s.MusicKnown = 0, true
		if w != nil {
			w.Music, w.MusicKnown = 0, true
		}
		fade := float64(a[0]) * 3
		if a[0] == 0 {
			fade = 2
		}
		evidence.Audio = &AudioRecord{Kind: "music", ID: 0, Loop: false, FadeSeconds: fade, PlaybackRequested: true}
		evidence.Screen = &ScreenRecord{Kind: "clear_dialog", ClearDialogRequested: true}
	case 0x85:
		evidence.Screen = &ScreenRecord{Kind: "delay", DelayMilliseconds: uint32(a[0]) * 80}
	}
	frames := append([]Frame(nil), s.Frames...)
	frames[i].PC = next
	s.World, s.Frames = w, frames
	evidence.Effect = presentationOpcodeName(inst.Opcode) + "; presentation request recorded without advancing world time"
	*step = evidence
	return true, nil
}
