package palflow

import "fmt"

func ticksOpcodeName(op uint16) string {
	if op == 0x0009 {
		return "wait frames with bounded auto scripts, optional triggers and standing gestures"
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
	if _, err := sceneEventIDs(world, "frame wait"); err != nil {
		return true, err
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
	for _, used := range []int{state.ExecutedSteps, state.PlayerSubsteps, state.WorldTicks, state.AutoSubsteps, state.CombatWork, int(ticks), 1} {
		if used < 0 || used > remaining {
			return true, fmt.Errorf("frame wait of %d ticks exceeds the combined instruction, player substep, and world tick budget of %d", ticks, r.options.MaxSteps)
		}
		remaining -= used
	}
	working := *state
	working.World = CloneWorld(world)
	working.Frames = append([]Frame(nil), state.Frames...)
	// Account for all requested ticks before running any inline trigger. Their
	// shared budget must include future outer ticks as well as the current one.
	working.WorldTicks += int(ticks)
	scheduler := autoScheduler{runner: r, state: &working, inputs: inputs, remaining: remaining, reserved: 1}
	var updates []WorldUpdateRecord
	for tick := 1; tick <= int(ticks); tick++ {
		if inst.Operands[2] != 0 {
			if err := updatePartyGestures(r.source, working.World, false); err != nil {
				return true, fmt.Errorf("frame wait party gestures at tick %d: %w", tick, err)
			}
		}
		if inst.Operands[1] != 0 {
			fromAuto, beforeFrame := len(scheduler.steps), working.World.FrameNum
			calls, err := r.updateTriggeredTick(&working, &scheduler, inputs, uint16(tick))
			if err != nil {
				return true, fmt.Errorf("trigger-enabled frame wait at PC %04x, tick %d: %w", frame.PC, tick, err)
			}
			updates = append(updates, WorldUpdateRecord{Tick: uint16(tick), BeforeFrame: beforeFrame, AfterFrame: working.World.FrameNum, TriggerSteps: calls, AutoSteps: append([]AutoStep(nil), scheduler.steps[fromAuto:]...)})
			continue
		}
		// Auto scripts may replace another event's entry, so candidates include
		// every current-scene event and are recomputed after scene transitions.
		candidates, err := sceneEventIDs(working.World, "frame wait")
		if err != nil {
			return true, err
		}
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
			// Standing gestures change the static step phase on every tick.
			// Only gesture-free waits may fold their remaining empty updates.
			if inst.Operands[2] == 0 {
				advanceWorldCounters(working.World, uint16(int(ticks)-tick+1))
				break
			}
			advanceWorldCounters(working.World, 1)
			continue
		}
		if !interrupted {
			advanceWorldCounters(working.World, 1)
		}
	}
	working.Frames[frameIndex] = frame
	working.Frames[frameIndex].PC = nextPC
	*state = working
	step.Ticks = ticks
	step.AutoSteps = scheduler.steps
	if inst.Operands[1] != 0 {
		step.AutoSteps = nil
		step.WorldUpdates = updates
	}
	step.Screen = &ScreenRecord{Kind: "frame_wait", ClearDialogRequested: true, Rendered: false}
	step.Effect = fmt.Sprintf("request %d world ticks with %d bounded auto instructions; standing gestures=%t, triggers enabled=%t; request dialog clear and redraw", ticks, len(scheduler.steps), inst.Operands[2] != 0, inst.Operands[1] != 0)
	return true, nil
}

func sceneEventIDs(world *WorldState, context string) ([]uint16, error) {
	if world == nil {
		return nil, fmt.Errorf("%s requires an explicit world snapshot", context)
	}
	if world.Scene == 0 || world.Scene >= 300 {
		return nil, fmt.Errorf("%s scene %d requires a current and next scene boundary within 1..300", context, world.Scene)
	}
	scene, ok := world.Scenes[world.Scene]
	if !ok {
		return nil, fmt.Errorf("%s current scene %d is absent from the world snapshot", context, world.Scene)
	}
	next, ok := world.Scenes[world.Scene+1]
	if !ok {
		return nil, fmt.Errorf("%s next scene %d boundary is absent from the world snapshot", context, world.Scene+1)
	}
	if next.EventStart < scene.EventStart {
		return nil, fmt.Errorf("%s scene %d has descending event boundaries %d..%d", context, world.Scene, scene.EventStart, next.EventStart)
	}
	ids := make([]uint16, 0, int(next.EventStart)-int(scene.EventStart))
	for id := int(scene.EventStart) + 1; id <= int(next.EventStart); id++ {
		if _, ok := world.Events[uint16(id)]; !ok {
			return nil, fmt.Errorf("%s scene %d event %d is absent from the world snapshot", context, world.Scene, id)
		}
		ids = append(ids, uint16(id))
	}
	return ids, nil
}

func advanceWorldCounters(world *WorldState, ticks uint16) {
	if world.ChaseCycles > 0 && ticks >= world.ChaseCycles {
		world.ChaseRange = 1
	}
	world.ChaseCycles -= ticks
	world.FrameNum += uint32(ticks)
}
