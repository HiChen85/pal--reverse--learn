package palflow

import "fmt"

// ObstacleSource supplies PAL_CheckObstacleWithRange against the current map.
// A missing map or event snapshot must be reported as an error, never as clear.
// Position contains PAL_POS's raw signed 16-bit coordinate bits.
type ObstacleSource interface {
	IsObstacle(world *WorldState, position WorldPosition, self uint16, checkEvents, checkRange bool) (bool, error)
}

// ObstacleTile reproduces PAL_CheckObstacleWithRange's diamond-to-tile mapping,
// including C's truncation toward zero for signed coordinates.
func ObstacleTile(position WorldPosition) (x, y, h int) {
	px, py := int(int16(position.X)), int(int16(position.Y))
	return diamondTile(px, py)
}

func diamondTile(px, py int) (x, y, h int) {
	x, y = px/32, py/16
	xr, yr := px%32, py%16
	if xr+yr*2 >= 16 {
		if xr+yr*2 >= 48 {
			x++
			y++
		} else if 32-xr+yr*2 < 16 {
			x++
		} else if 32-xr+yr*2 < 48 {
			h = 1
		} else {
			y++
		}
	}
	return
}

// ObstacleOutOfRange is the pre-tile range check used when fCheckRange is true.
// The original source's vertical upper bound is 2048, without division by 16.
func ObstacleOutOfRange(world *WorldState, position WorldPosition) (bool, error) {
	if world == nil {
		return false, fmt.Errorf("obstacle range check requires a world snapshot")
	}
	if !signedPartyCoordinate(world.PartyOffsetX) || !signedPartyCoordinate(world.PartyOffsetY) {
		return false, fmt.Errorf("obstacle range check requires signed 16-bit party offsets")
	}
	x, y := int(int16(position.X))/32, int(int16(position.Y))/16
	bx, by := world.PartyOffsetX/32, world.PartyOffsetY/16
	return x < bx || x >= 64-bx-1 || y < by || y >= 2048-by-1, nil
}

// CheckEventObstacle is the event half of PAL_CheckObstacleWithRange. Events
// with state >= 2 block even when vanished; the original does not test vanish.
func CheckEventObstacle(world *WorldState, position WorldPosition, self uint16) (bool, error) {
	ids, err := sceneEventIDs(world, "obstacle check")
	if err != nil {
		return false, err
	}
	x, y := int(int16(position.X)), int(int16(position.Y))
	for _, id := range ids {
		if id == self {
			continue
		}
		event := world.Events[id]
		if event.State >= 2 && absAutoOffset(int(event.X)-x)+absAutoOffset(int(event.Y)-y)*2 < 16 {
			return true, nil
		}
	}
	return false, nil
}

func movementOpcodeName(op uint16) string {
	switch op {
	case 0xb, 0xc, 0xd, 0xe:
		return "walk invoking NPC one step at speed 2"
	case 0xf:
		return "set invoking NPC direction and gesture"
	case 0x10:
		return "walk invoking NPC to tile at speed 3"
	case 0x11:
		return "walk invoking NPC to tile at speed 2 on alternating frames"
	case 0x7c:
		return "walk invoking NPC to tile at speed 4 on alternating frames"
	case 0x82:
		return "walk invoking NPC to tile at speed 8"
	case 0x4c:
		return "chase party with map collision, avoidance and rotating repel behavior"
	}
	return ""
}

// evaluateMovement is PAL_InterpretInstruction's NPC movement subset. A walk-to
// executes exactly one attempt; a retained PC is not an implicit time update.
func (r *Runner) evaluateMovement(state *State, step *TraceStep, inst Instruction, inputs []Input) (bool, error) {
	if movementOpcodeName(inst.Opcode) == "" {
		return false, nil
	}
	if inst.Opcode == 0x4c {
		return true, r.evaluateChase(state, step, inst, inputs)
	}
	if state == nil || len(state.Frames) == 0 || state.World == nil || step == nil {
		return true, fmt.Errorf("NPC movement requires a script frame, world snapshot and trace step")
	}
	index := len(state.Frames) - 1
	frame := state.Frames[index]
	id := frame.EventID
	if id == 0 || id == 0xffff || id >= 0x9000 {
		return true, fmt.Errorf("NPC movement requires a concrete invoking event without special addressing")
	}
	event, ok := state.World.Events[id]
	if !ok {
		return true, fmt.Errorf("NPC movement event %d is absent from the world snapshot", id)
	}
	advance := true
	effect := movementOpcodeName(inst.Opcode)
	switch inst.Opcode {
	case 0xb, 0xc, 0xd, 0xe:
		event.Direction = inst.Opcode - 0xb
		walkNPCOneStep(&event, 2)
	case 0xf:
		if inst.Operands[0] != 0xffff {
			event.Direction = inst.Operands[0]
		}
		if inst.Operands[1] != 0xffff {
			event.Frame = inst.Operands[1]
		}
	default:
		speed := 3
		if inst.Opcode == 0x11 || inst.Opcode == 0x7c {
			speed = 2
			if inst.Opcode == 0x7c {
				speed = 4
			}
			if (uint32(id)^state.World.FrameNum)&1 == 0 {
				advance = false
				effect += "; parity gate retains entry without moving"
				break
			}
		} else if inst.Opcode == 0x82 {
			speed = 8
		}
		advance = walkNPCTo(&event, inst.Operands, speed)
		if advance {
			effect += "; reached target and reset gesture"
		} else {
			effect += "; retain entry for the next attempt"
		}
	}
	if advance {
		next, err := fallthroughPC(frame.PC)
		if err != nil {
			return true, err
		}
		frame.PC = next
	}
	world := CloneWorld(state.World)
	world.Events[id] = event
	frames := append([]Frame(nil), state.Frames...)
	frames[index] = frame
	state.World, state.Frames = world, frames
	step.Effect = effect
	return true, nil
}

func walkNPCOneStep(event *WorldEvent, speed int) {
	dx, dy := speed*2, speed
	if event.Direction == 0 || event.Direction == 1 {
		dx = -dx
	}
	if event.Direction == 1 || event.Direction == 2 {
		dy = -dy
	}
	event.X += uint16(dx)
	event.Y += uint16(dy)
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

func walkNPCTo(event *WorldEvent, target [3]uint16, speed int) bool {
	x, y := int(target[0])*32+int(target[2])*16, int(target[1])*16+int(target[2])*8
	dx, dy := x-int(event.X), y-int(event.Y)
	event.Direction = partyWalkDirection(dx, dy)
	if absAutoOffset(dx) < speed*2 || absAutoOffset(dy) < speed*2 {
		event.X, event.Y = uint16(x), uint16(y)
	} else {
		walkNPCOneStep(event, speed)
	}
	arrived := int(event.X) == x && int(event.Y) == y
	if arrived {
		event.Frame = 0
	}
	return arrived
}
