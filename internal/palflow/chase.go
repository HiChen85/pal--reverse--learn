package palflow

import "fmt"

// The chase instruction uses its invoking event, not operand 0's interpreted
// event selector. Collision calls deliberately disable party-offset range
// clipping, matching PAL_CheckObstacle (unlike keyboard party movement).
func (r *Runner) evaluateChase(state *State, step *TraceStep, inst Instruction, inputs []Input) error {
	if r == nil || state == nil || state.World == nil || len(state.Frames) == 0 || step == nil {
		return fmt.Errorf("monster chase requires runner, frame and world")
	}
	i := len(state.Frames) - 1
	frame := state.Frames[i]
	event, ok := state.World.Events[frame.EventID]
	if !ok || frame.EventID == 0 || frame.EventID >= 0x9000 {
		return fmt.Errorf("monster chase requires a concrete invoking event")
	}
	next, err := fallthroughPC(frame.PC)
	if err != nil {
		return err
	}
	working := *state
	working.World = CloneWorld(state.World)
	w := working.World
	speed, distance := int(inst.Operands[1]), int(inst.Operands[0])
	if speed == 0 {
		speed = 4
	}
	if distance == 0 {
		distance = 8
	}
	movement := 0
	var draws []InputUse
	if w.ChaseRange == 0 {
		if w.FrameNum&1 != 0 {
			event.Direction = (event.Direction + 1) % 4
		}
	} else {
		dx, dy := w.ViewportX+w.PartyOffsetX-int(event.X), w.ViewportY+w.PartyOffsetY-int(event.Y)
		// Ties consume random values before the range check in the original.
		for axis, offset := range []*int{&dx, &dy} {
			if *offset != 0 {
				continue
			}
			value, draw, err := consumeRandom(&working, inputs, 0, 1, fmt.Sprintf("monster chase axis %d", axis))
			if err != nil {
				return err
			}
			if draw != nil {
				draws = append(draws, *draw)
			}
			*offset = 1
			if value != 0 {
				*offset = -1
			}
		}
		if absAutoOffset(dx)+absAutoOffset(dy)*2 < distance*32*int(w.ChaseRange) {
			event.Direction = partyWalkDirection(dx, dy)
			if inst.Operands[2] != 0 {
				movement = speed
			} else {
				collision, ok := r.source.(ObstacleSource)
				if !ok {
					return fmt.Errorf("monster chase requires loaded collision resources")
				}
				point := WorldPosition{X: event.X + uint16(signChase(dx)*16), Y: event.Y + uint16(signChase(dy)*8)}
				blocked, err := collision.IsObstacle(w, point, frame.EventID, true, false)
				if err != nil {
					return err
				}
				x, y, h := diamondTile(int(event.X), int(event.Y))
				anchor := WorldPosition{X: uint16(x*32 + h*16), Y: uint16(y*16 + h*8)}
				if blocked {
					event.X, event.Y = anchor.X, anchor.Y
				} else {
					movement = speed
				}
				// Probe the four small diamond offsets in order. A blocked
				// probe resets the working position; the next probe starts there.
				for _, delta := range [][2]int{{-4, 2}, {-4, -2}, {4, -2}, {4, 2}} {
					event.X += uint16(delta[0])
					event.Y += uint16(delta[1])
					blocked, err = collision.IsObstacle(w, WorldPosition{X: event.X, Y: event.Y}, 0, false, false)
					if err != nil {
						return err
					}
					if blocked {
						event.X, event.Y = anchor.X, anchor.Y
					}
				}
			}
		}
	}
	walkNPCOneStep(&event, movement)
	w.Events[frame.EventID] = event
	working.Frames = append([]Frame(nil), state.Frames...)
	working.Frames[i].PC = next
	*state = working
	step.RandomInputs = draws
	step.Effect = fmt.Sprintf("monster chase event %d: range %d × %d; movement speed %d; collision range clipping disabled", frame.EventID, distance, w.ChaseRange, movement)
	return nil
}

func signChase(n int) int {
	if n < 0 {
		return -1
	}
	return 1
}
