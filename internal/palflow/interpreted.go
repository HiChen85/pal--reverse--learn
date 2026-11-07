package palflow

import (
	"errors"
	"fmt"
	"maps"
)

// evaluateInterpreted is the shared PAL_InterpretInstruction subset. Trigger
// control flow, trigger-only waits/dialogue and auto control flow stay in their
// respective dispatchers, so one auto instruction cannot accidentally run a
// trigger-only operation with different semantics.
func (r *Runner) evaluateInterpreted(s *State, step *TraceStep, inst Instruction, inputs []Input) (bool, error) {
	if ok, err := r.evaluatePaletteTransition(s, step, inst, inputs); ok {
		return true, err
	}
	if ok, err := r.evaluateMedia(s, step, inst); ok {
		return true, err
	}
	if ok, err := r.evaluateCamera(s, step, inst, inputs); ok {
		return true, err
	}
	if ok, err := r.evaluateShop(s, step, inst, inputs); ok {
		return true, err
	}
	if ok, err := r.evaluatePoison(s, step, inst, inputs); ok {
		return true, err
	}
	if ok, err := r.evaluateBattleOps(s, step, inst, inputs); ok {
		return true, err
	}
	if ok, err := r.evaluateWorldExtra(s, step, inst, inputs); ok {
		return true, err
	}
	if ok, err := r.evaluateMovement(s, step, inst, inputs); ok {
		return true, err
	}
	if ok, err := r.evaluatePartyWalk(s, step, inst, inputs); ok {
		return true, err
	}
	if ok, err := r.evaluateConditions(s, step, inst, inputs); ok {
		return true, err
	}
	if ok, err := r.evaluatePlayer(s, step, inst); ok {
		return true, err
	}
	if ok, err := evaluateInventory(s, step, inst); ok {
		return true, err
	}
	if ok, err := r.evaluateResources(s, step, inst); ok {
		return true, err
	}
	if ok, err := r.evaluatePresentation(s, step, inst, inputs); ok {
		return true, err
	}
	return evaluateWorld(s, step, inst)
}

type textLimitError struct{}

func (textLimitError) Error() string { return "maximum cumulative dialogue text byte count reached" }

func (r *Runner) evaluateAndCharge(s *State, step *TraceStep, inst Instruction, inputs []Input) error {
	if err := r.evaluate(s, step, inst, inputs); err != nil {
		return err
	}
	if step.Message != nil {
		if len(step.Message.Text) > r.options.MaxTextBytes-s.TextBytes {
			return textLimitError{}
		}
		s.TextBytes += len(step.Message.Text)
	}
	s.ExecutedSteps++
	return nil
}

// runTriggerInline is a synchronous trigger invocation from an interpreted or
// auto instruction. reserved accounts for enclosing work not yet charged (for
// example the outer wait instruction). Nested calls share all counters, random
// input and dialogue budget. The complete call is atomic on failure.
func (r *Runner) runTriggerInline(state *State, entry, eventID uint16, inputs []Input, reserved int) ([]TraceStep, error) {
	if r == nil || state == nil || reserved < 0 || reserved > r.options.MaxSteps {
		return nil, errors.New("inline trigger requires state and a valid reserved budget")
	}
	limit := *r
	limit.options.MaxSteps -= reserved
	if len(state.Frames) >= r.options.MaxCallDepth {
		return nil, errors.New("maximum trigger-call depth reached")
	}
	if eventID == 0xffff {
		eventID = state.LastEventID
	}
	if eventID == 0xffff {
		return nil, errors.New("last event context cannot be the reuse sentinel")
	}
	w := *state
	w.World = CloneWorld(state.World)
	w.IdleFrames = maps.Clone(state.IdleFrames)
	if w.IdleFrames == nil {
		w.IdleFrames = map[uint16]uint16{}
	}
	w.Frames = append([]Frame(nil), state.Frames...)
	base := len(w.Frames)
	w.Frames = append(w.Frames, Frame{PC: entry, Entry: entry, NextEntry: entry, EventID: eventID})
	w.LastEventID, w.ScriptSuccess, w.Finished = eventID, true, false
	var trace []TraceStep
	returned := entry
	for len(w.Frames) > base {
		frame := w.Frames[len(w.Frames)-1]
		if frame.PC == 0 {
			if len(w.Frames) == base+1 {
				returned = frame.NextEntry
			}
			stopFrame(&w, nil)
			continue
		}
		fail := func(kind, reason string, inst *Instruction) error {
			b := &Blocker{Kind: kind, Reason: reason, PC: frame.PC, EventID: frame.EventID, Depth: len(w.Frames), Instruction: inst}
			if inst != nil {
				b.Opcode = inst.Opcode
			}
			return b
		}
		if w.ExecutedSteps+w.PlayerSubsteps+w.WorldTicks+w.AutoSubsteps+w.CombatWork+w.MediaWork >= limit.options.MaxSteps {
			return nil, fail("step_limit", "inline trigger exhausted the shared instruction budget", nil)
		}
		inst, err := r.source.Instruction(frame.PC)
		if err != nil {
			return nil, fail("source_bounds", err.Error(), nil)
		}
		step := TraceStep{Sequence: w.ExecutedSteps + 1, PC: frame.PC, Opcode: inst.Opcode, Operands: inst.Operands, EventID: frame.EventID, Depth: len(w.Frames)}
		if err = limit.evaluateAndCharge(&w, &step, inst, inputs); err != nil {
			kind := "instruction_blocked"
			var textErr textLimitError
			if errors.As(err, &textErr) {
				kind = "text_limit"
			}
			return nil, fail(kind, err.Error(), &inst)
		}
		if len(w.Frames) == base && step.ReturnedEntry != nil {
			returned = *step.ReturnedEntry
		}
		for len(w.Frames) > base && w.Frames[len(w.Frames)-1].PC == 0 {
			if len(w.Frames) == base+1 {
				returned = w.Frames[len(w.Frames)-1].NextEntry
			}
			stopFrame(&w, nil)
		}
		if len(w.Frames) > base {
			step.NextPC = w.Frames[len(w.Frames)-1].PC
		}
		trace = append(trace, step)
	}
	w.Finished, w.ReturnedEntry = state.Finished, state.ReturnedEntry
	// A trigger invocation resets the global equipment part even for entry zero.
	w.CurEquipPart = -1
	w.DialogPlayingRNG = false
	w.LastInlineReturn = returned
	*state = w
	return trace, nil
}

func (r *Runner) sourceRequired() error {
	if r == nil || r.source == nil {
		return fmt.Errorf("instruction source is required")
	}
	return nil
}
