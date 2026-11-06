package palflow

import "fmt"

func ticksOpcodeName(op uint16) string {
	if op == 0x0009 {
		return "wait frames with bounded auto scripts, without triggers or party gestures"
	}
	return ""
}

// evaluateTicks models a bounded subset of PAL_GameUpdate(FALSE), preserving
// event order and PAL_RunAutoScript's one-instruction-per-tick rules.
// Rendering, elapsed wall time, and dialog clearing are recorded, not performed.
func (r *Runner) evaluateTicks(state *State, step *TraceStep, inst Instruction, inputs []Input) (bool, error) {
	if inst.Opcode != 0x0009 {
		return false, nil
	}
	if state == nil || len(state.Frames) == 0 {
		return true, fmt.Errorf("frame wait requires a current script frame")
	}
	if step == nil {
		return true, fmt.Errorf("frame wait requires a trace step")
	}
	if inst.Operands[1] != 0 {
		return true, fmt.Errorf("frame wait with trigger updates requested by operand 1 is not modeled")
	}
	if inst.Operands[2] != 0 {
		return true, fmt.Errorf("frame wait with party gestures requested by operand 2 is not modeled")
	}
	if state.World == nil {
		return true, fmt.Errorf("frame wait requires an explicit world snapshot")
	}
	if r == nil || r.options.MaxSteps < 1 {
		return true, fmt.Errorf("frame wait requires a positive execution budget")
	}
	frameIndex := len(state.Frames) - 1
	frame := state.Frames[frameIndex]
	nextPC, err := fallthroughPC(frame.PC)
	if err != nil {
		return true, err
	}
	world := state.World
	if world.Scene == 0 || world.Scene >= 300 {
		return true, fmt.Errorf("frame wait scene %d requires a current and next scene boundary within 1..300", world.Scene)
	}
	scene, ok := world.Scenes[world.Scene]
	if !ok {
		return true, fmt.Errorf("frame wait current scene %d is absent from the world snapshot", world.Scene)
	}
	nextScene, ok := world.Scenes[world.Scene+1]
	if !ok {
		return true, fmt.Errorf("frame wait next scene %d boundary is absent from the world snapshot", world.Scene+1)
	}
	if nextScene.EventStart < scene.EventStart {
		return true, fmt.Errorf("frame wait scene %d has descending event boundaries %d..%d", world.Scene, scene.EventStart, nextScene.EventStart)
	}
	var candidates []uint16
	for id := int(scene.EventStart) + 1; id <= int(nextScene.EventStart); id++ {
		event, ok := world.Events[uint16(id)]
		if !ok {
			return true, fmt.Errorf("frame wait scene %d event %d is absent from the world snapshot", world.Scene, id)
		}
		// Supported auto instructions cannot give another event a new script.
		// Keeping inactive candidates preserves later activation in event order.
		if event.AutoScript != 0 {
			candidates = append(candidates, uint16(id))
		}
	}
	ticks := inst.Operands[0]
	if ticks == 0 {
		ticks = 1
	}
	if state.ExecutedSteps < 0 || state.PlayerSubsteps < 0 || state.WorldTicks < 0 || state.AutoSubsteps < 0 {
		return true, fmt.Errorf("frame wait execution counters must be nonnegative")
	}
	// Subtract each component rather than adding caller-supplied counters, so
	// malformed extreme counters cannot overflow the combined budget check.
	remaining := r.options.MaxSteps
	for _, used := range []int{state.ExecutedSteps, state.PlayerSubsteps, state.WorldTicks, state.AutoSubsteps, int(ticks), 1} {
		if used > remaining {
			return true, fmt.Errorf("frame wait of %d ticks exceeds the combined instruction, player substep, and world tick budget of %d", ticks, r.options.MaxSteps)
		}
		remaining -= used
	}
	working := *state
	working.World = CloneWorld(world)
	scheduler := autoScheduler{runner: r, state: &working, inputs: inputs, remaining: remaining}
	for tick := 1; tick <= int(ticks); tick++ {
		active, interrupted := false, false
		for _, eventID := range candidates {
			event := working.World.Events[eventID]
			if event.State <= 0 || event.VanishTime != 0 || event.AutoScript == 0 {
				continue
			}
			active = true
			if err := scheduler.run(eventID, uint16(tick)); err != nil {
				return true, fmt.Errorf("frame wait at PC %04x, tick %d: %w", frame.PC, tick, err)
			}
			// PAL_GameUpdate checks fEnteringScene after each auto invocation,
			// including when that flag was already set before this update.
			if working.World.SceneChanged {
				interrupted = true
				break
			}
		}
		if !active {
			// No supported instruction can activate anything without an active
			// auto script, so the remaining counter-only updates are equivalent.
			advanceWorldCounters(working.World, uint16(int(ticks)-tick+1))
			break
		}
		if !interrupted {
			advanceWorldCounters(working.World, 1)
		}
	}
	frames := append([]Frame(nil), state.Frames...)
	frames[frameIndex].PC = nextPC
	state.World = working.World
	state.Frames = frames
	state.WorldTicks += int(ticks)
	state.AutoSubsteps = working.AutoSubsteps
	state.InputIndex = working.InputIndex
	step.Ticks = ticks
	step.AutoSteps = scheduler.steps
	step.Screen = &ScreenRecord{Kind: "frame_wait", ClearDialogRequested: true, Rendered: false}
	step.Effect = fmt.Sprintf("request %d world ticks with %d bounded auto instructions, without triggers or party gestures; request dialog clear and redraw", ticks, len(scheduler.steps))
	return true, nil
}

func advanceWorldCounters(world *WorldState, ticks uint16) {
	if world.ChaseCycles > 0 && ticks >= world.ChaseCycles {
		world.ChaseRange = 1
	}
	world.ChaseCycles -= ticks
	world.FrameNum += uint32(ticks)
}
