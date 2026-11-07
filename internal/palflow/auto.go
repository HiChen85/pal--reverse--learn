package palflow

import "fmt"

// AutoStep is one autoscript instruction inside a trigger-disabled frame wait.
// Work includes the instruction and, when it changes position, one movement.
type AutoStep struct {
	Instruction  *TraceStep     `json:"instruction,omitempty"`
	TriggerSteps []TraceStep    `json:"trigger_steps,omitempty"`
	Tick         uint16         `json:"tick"`
	FrameNum     uint32         `json:"frame_num"`
	EventID      uint16         `json:"event_id"`
	PC           uint16         `json:"pc"`
	Opcode       uint16         `json:"opcode"`
	Operands     [3]uint16      `json:"operands"`
	NextPC       uint16         `json:"next_pc"`
	Effect       string         `json:"effect"`
	Work         uint8          `json:"work"`
	Input        *InputUse      `json:"input,omitempty"`
	Before       *WorldPosition `json:"before,omitempty"`
	After        *WorldPosition `json:"after,omitempty"`
}

// The scheduler works on a detached State. Its caller commits it only after the
// complete wait succeeds, including every random input and budget check.
type autoScheduler struct {
	runner    *Runner
	state     *State
	inputs    []Input
	remaining int
	reserved  int
	steps     []AutoStep
}

func (a *autoScheduler) spend(work int, event, pc uint16) error {
	if work < 0 || work > a.remaining {
		return fmt.Errorf("auto script event %d PC %04x exceeds the combined execution budget", event, pc)
	}
	a.remaining -= work
	a.state.AutoSubsteps += work
	return nil
}

// syncRemaining includes work performed inside interpreted operations and
// inline trigger calls, as well as the outer wait instruction's reservation.
func (a *autoScheduler) syncRemaining(event, pc uint16) error {
	remaining := a.runner.options.MaxSteps
	for _, used := range []int{a.state.ExecutedSteps, a.state.PlayerSubsteps, a.state.WorldTicks, a.state.AutoSubsteps, a.state.CombatWork, a.state.MediaWork, a.reserved} {
		if used < 0 || used > remaining {
			return fmt.Errorf("auto script event %d PC %04x exceeds the combined execution budget", event, pc)
		}
		remaining -= used
	}
	a.remaining = remaining
	return nil
}

// run follows PAL_RunAutoScript's distinction between same-tick jumps and
// instructions that return their replacement entry for the next world tick.
func (a *autoScheduler) run(eventID, tick uint16) error {
	if a.runner.source == nil {
		return fmt.Errorf("auto script event %d requires an instruction source", eventID)
	}
	pc := a.state.World.Events[eventID].AutoScript
	for {
		world := a.state.World
		if err := a.spend(1, eventID, pc); err != nil {
			return err
		}
		inst, err := a.runner.source.Instruction(pc)
		if err != nil {
			return fmt.Errorf("auto script event %d PC %04x: %w", eventID, pc, err)
		}
		step := AutoStep{Tick: tick, FrameNum: world.FrameNum, EventID: eventID, PC: pc, Opcode: inst.Opcode, Operands: inst.Operands, NextPC: pc, Work: 1}
		self := world.Events[eventID]
		x := inst.Operands
		jump := false
		advance := func() error {
			next, err := fallthroughPC(pc)
			step.NextPC = next
			return err
		}
		switch inst.Opcode {
		case 0:
			step.Effect = "retain the current auto entry and stop this tick"
		case 1:
			err = advance()
			step.Effect = "replace auto entry with the following instruction; stop this tick"
		case 2, 3:
			take := true
			if x[1] != 0 {
				self.AutoIdleFrame++
				take = self.AutoIdleFrame < x[1]
				if !take {
					self.AutoIdleFrame = 0
				}
				world.Events[eventID] = self
			}
			if take {
				step.NextPC = x[0]
				jump = inst.Opcode == 3
				step.Effect = "replace auto entry with operand 0"
				if jump {
					step.Effect += "; continue within this tick"
				}
			} else {
				err = advance()
				step.Effect = "idle threshold reached; reset auto counter and advance for the next tick"
			}
		case 4:
			context := x[1]
			if context == 0 {
				context = eventID
			}
			step.TriggerSteps, err = a.runner.runTriggerInline(a.state, x[0], context, a.inputs, a.reserved)
			if err == nil {
				err = a.syncRemaining(eventID, pc)
			}
			if err == nil {
				err = advance()
			}
			step.Effect = "run bounded trigger subroutine and advance auto entry for the next tick"
		case 6:
			step.Input, err = consume(a.state, a.inputs, InputRandom)
			if err != nil {
				break
			}
			if step.Input.Input.Value >= int(x[0]) {
				step.Effect = "random value reached threshold; preserve auto entry when jump target is zero"
				if x[1] != 0 {
					step.NextPC, jump = x[1], true
					step.Effect = "random value reached threshold; jump and continue within this tick"
				}
			} else {
				err = advance()
				step.Effect = "random value below threshold; advance for the next tick"
			}
		case 9:
			self.AutoIdleFrame++
			if self.AutoIdleFrame >= x[0] {
				self.AutoIdleFrame = 0
				err = advance()
				step.Effect = "auto wait threshold reached; reset counter and advance"
			} else {
				step.Effect = "increment auto idle counter and retain entry"
			}
			world.Events[eventID] = self
		case 0x49:
			step.Effect = "operand 0 zero leaves event state unchanged"
			if x[0] != 0 {
				targetID := x[0]
				if targetID == 0xffff {
					targetID = eventID
				}
				if targetID >= 0x9000 {
					return fmt.Errorf("auto script event %d PC %04x: event ID %04x requires unsupported special event addressing", eventID, pc, targetID)
				}
				target, ok := world.Events[targetID]
				if !ok {
					return fmt.Errorf("auto script event %d PC %04x: target event %d is absent from the world snapshot", eventID, pc, targetID)
				}
				target.State = int16(x[1])
				world.Events[targetID] = target
				step.Effect = fmt.Sprintf("set event %d state to %d", targetID, target.State)
			}
			err = advance()
		case 0xffff:
			if world.AutoMessageMode != "dos" {
				return fmt.Errorf("auto script event %d PC %04x opcode ffff requires explicit dos auto-message mode; mode %q is unsupported", eventID, pc, world.AutoMessageMode)
			}
			err = advance()
			step.Effect = "DOS auto message instruction advances without displaying text"
		case 0xa7:
			err = advance()
			step.Effect = "auto opcode 00a7 advances without invoking trigger or interpreted behavior"
		default:
			child := TraceStep{PC: pc, Opcode: inst.Opcode, Operands: x, EventID: eventID, Depth: len(a.state.Frames)}
			outerFrames := append([]Frame(nil), a.state.Frames...)
			if len(outerFrames) == 0 {
				return fmt.Errorf("auto script event %d PC %04x requires an enclosing script frame", eventID, pc)
			}
			frameIndex := len(outerFrames) - 1
			a.state.Frames = append([]Frame(nil), outerFrames...)
			a.state.Frames[frameIndex] = Frame{PC: pc, Entry: pc, NextEntry: pc, EventID: eventID}
			// The auto instruction was already charged by spend. Interpreted
			// handlers reserve one current instruction themselves; compensate
			// for that duplicate while retaining all enclosing reservations.
			interpreted := *a.runner
			interpreted.options.MaxSteps -= a.reserved - 1
			handled, interpretErr := interpreted.evaluateInterpreted(a.state, &child, inst, a.inputs)
			if interpretErr != nil {
				err = interpretErr
			} else if !handled {
				return fmt.Errorf("auto script event %d PC %04x opcode %04x is unsupported by PAL_InterpretInstruction", eventID, pc, inst.Opcode)
			} else if len(a.state.Frames) != len(outerFrames) {
				err = fmt.Errorf("interpreted auto instruction changed trigger frame depth")
			} else {
				step.NextPC = a.state.Frames[frameIndex].PC
				child.NextPC = step.NextPC
				step.Effect, step.Input = child.Effect, child.Input
				step.Instruction = &child
				if movementOpcodeName(inst.Opcode) != "" {
					before := WorldPosition{X: self.X, Y: self.Y}
					afterEvent := a.state.World.Events[eventID]
					after := WorldPosition{X: afterEvent.X, Y: afterEvent.Y}
					step.Before, step.After = &before, &after
					if before != after {
						err = a.spend(1, eventID, pc)
						step.Work++
					}
				}
			}
			a.state.Frames = outerFrames
			if err == nil {
				err = a.syncRemaining(eventID, pc)
			}
		}
		if err != nil {
			return fmt.Errorf("auto script event %d PC %04x: %w", eventID, pc, err)
		}
		world = a.state.World
		self = world.Events[eventID]
		self.AutoScript = step.NextPC
		world.Events[eventID] = self
		a.steps = append(a.steps, step)
		if !jump {
			return nil
		}
		pc = step.NextPC
	}
}

// walkAutoNPC reproduces the coordinate and frame arithmetic of opcode 0010.
// It has no collision checks, route finding, trigger processing, or rendering.
func walkAutoNPC(event *WorldEvent, target [3]uint16) bool {
	return walkNPCTo(event, target, 3)
}

func absAutoOffset(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
