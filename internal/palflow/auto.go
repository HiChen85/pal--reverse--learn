package palflow

import "fmt"

// AutoStep is one autoscript instruction inside a trigger-disabled frame wait.
// Work includes the instruction and, when it changes position, one movement.
type AutoStep struct {
	Tick     uint16         `json:"tick"`
	FrameNum uint32         `json:"frame_num"`
	EventID  uint16         `json:"event_id"`
	PC       uint16         `json:"pc"`
	Opcode   uint16         `json:"opcode"`
	Operands [3]uint16      `json:"operands"`
	NextPC   uint16         `json:"next_pc"`
	Effect   string         `json:"effect"`
	Work     uint8          `json:"work"`
	Input    *InputUse      `json:"input,omitempty"`
	Before   *WorldPosition `json:"before,omitempty"`
	After    *WorldPosition `json:"after,omitempty"`
}

// The scheduler works on a detached State. Its caller commits it only after the
// complete wait succeeds, including every random input and budget check.
type autoScheduler struct {
	runner    *Runner
	state     *State
	inputs    []Input
	remaining int
	steps     []AutoStep
}

func (a *autoScheduler) spend(work int, event, pc uint16) error {
	if work > a.remaining {
		return fmt.Errorf("auto script event %d PC %04x exceeds the combined execution budget", event, pc)
	}
	a.remaining -= work
	a.state.AutoSubsteps += work
	return nil
}

// run follows PAL_RunAutoScript's distinction between same-tick jumps and
// instructions that return their replacement entry for the next world tick.
func (a *autoScheduler) run(eventID, tick uint16) error {
	if a.runner.source == nil {
		return fmt.Errorf("auto script event %d requires an instruction source", eventID)
	}
	world := a.state.World
	pc := world.Events[eventID].AutoScript
	for {
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
			return fmt.Errorf("auto script event %d PC %04x opcode 0004 requires unsupported trigger subroutine scheduling", eventID, pc)
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
		case 0x10:
			before := WorldPosition{X: self.X, Y: self.Y}
			arrived := walkAutoNPC(&self, x)
			after := WorldPosition{X: self.X, Y: self.Y}
			if before != after {
				if err = a.spend(1, eventID, pc); err != nil {
					break
				}
				step.Work++
			}
			step.Before, step.After = &before, &after
			world.Events[eventID] = self
			if arrived {
				err = advance()
				step.Effect = "NPC reached tile target; reset gesture and advance"
			} else {
				step.Effect = "NPC walked toward tile target at speed 3; retain entry"
			}
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
		default:
			return fmt.Errorf("auto script event %d PC %04x opcode %04x is unsupported", eventID, pc, inst.Opcode)
		}
		if err != nil {
			return fmt.Errorf("auto script event %d PC %04x: %w", eventID, pc, err)
		}
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
	x, y := int(target[0])*32+int(target[2])*16, int(target[1])*16+int(target[2])*8
	dx, dy := x-int(event.X), y-int(event.Y)
	if dy < 0 {
		event.Direction = 2
		if dx < 0 {
			event.Direction = 1
		}
	} else {
		event.Direction = 3
		if dx < 0 {
			event.Direction = 0
		}
	}
	if absAutoOffset(dx) < 6 || absAutoOffset(dy) < 6 {
		event.X, event.Y = uint16(x), uint16(y)
	} else {
		stepX, stepY := uint16(6), uint16(3)
		if event.Direction == 0 || event.Direction == 1 {
			stepX = 65530
		}
		if event.Direction == 1 || event.Direction == 2 {
			stepY = 65533
		}
		event.X, event.Y = event.X+stepX, event.Y+stepY
		frames := event.SpriteFrames
		if frames == 3 {
			frames = 4
		}
		if frames == 0 {
			frames = event.SpriteFramesAuto
		}
		if frames != 0 {
			event.Frame++
			event.Frame %= frames
		}
	}
	arrived := int(event.X) == x && int(event.Y) == y
	if arrived {
		event.Frame = 0
	}
	return arrived
}

func absAutoOffset(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
