package palflow

import "fmt"

// PartyWalkStep records one leader movement followed by one trigger-disabled
// world update. Viewport coordinates are signed pixels; TrailHead retains the
// original WORD coordinate bits from before this movement.
type PartyWalkStep struct {
	Sequence         int        `json:"sequence"`
	BeforeViewportX  int        `json:"before_viewport_x"`
	BeforeViewportY  int        `json:"before_viewport_y"`
	AfterViewportX   int        `json:"after_viewport_x"`
	AfterViewportY   int        `json:"after_viewport_y"`
	Direction        uint16     `json:"direction"`
	WalkingFrame     uint16     `json:"walking_frame"`
	StepFrame        uint16     `json:"step_frame"`
	TrailHead        WorldTrail `json:"trail_head"`
	BeforeWorldFrame uint32     `json:"before_world_frame"`
	AfterWorldFrame  uint32     `json:"after_world_frame"`
	AutoSteps        []AutoStep `json:"auto_steps,omitempty"`
	ScreenUpdates    int        `json:"screen_updates"`
}

type PartyWalkRecord struct {
	RidingEvent      uint16          `json:"riding_event,omitempty"`
	Speed            int             `json:"speed"`
	TargetMapX       int             `json:"target_map_x"`
	TargetMapY       int             `json:"target_map_y"`
	InitialStepFrame uint16          `json:"initial_step_frame"`
	FinalStepFrame   uint16          `json:"final_step_frame"`
	StandingFrame    uint16          `json:"standing_frame"`
	Steps            []PartyWalkStep `json:"steps"`
}

func partyWalkOpcodeName(op uint16) string {
	switch op {
	case 0x70:
		return "walk party at speed 2 with bounded auto updates"
	case 0x7a:
		return "walk party at speed 4 with bounded auto updates"
	case 0x7b:
		return "walk party at speed 8 with bounded auto updates"
	case 0x3f, 0x44, 0x97:
		return "ride invoking event with bounded auto updates and preserved party gestures"
	}
	return ""
}

// evaluatePartyWalk models PAL_PartyWalkTo and PAL_UpdatePartyGestures. The
// leader branch has no collision query; other party members query the source.
// Every tick runs the existing bounded PAL_GameUpdate(FALSE) subset. The whole
// instruction, including consumed inputs, rolls back if any tick is unsupported.
func (r *Runner) evaluatePartyWalk(state *State, step *TraceStep, inst Instruction, inputs []Input) (bool, error) {
	if partyWalkOpcodeName(inst.Opcode) == "" {
		return false, nil
	}
	if state == nil || len(state.Frames) == 0 || state.World == nil || step == nil {
		return true, fmt.Errorf("party walk requires a script frame, world snapshot and trace step")
	}
	if r == nil || r.options.MaxSteps < 1 {
		return true, fmt.Errorf("party walk requires a positive execution budget")
	}
	remaining := r.options.MaxSteps
	for _, used := range []int{state.ExecutedSteps, state.PlayerSubsteps, state.WorldTicks, state.AutoSubsteps, state.CombatWork, state.MediaWork, 1} {
		if used < 0 || used > remaining {
			return true, fmt.Errorf("party walk exceeds the combined execution budget")
		}
		remaining -= used
	}
	w := state.World
	riding := inst.Opcode == 0x3f || inst.Opcode == 0x44 || inst.Opcode == 0x97
	if w.InBattle || w.SceneLoadPending || w.SceneChanged {
		return true, fmt.Errorf("party walk requires an entered overworld scene without pending scene resources")
	}
	if riding {
		id := state.Frames[len(state.Frames)-1].EventID
		if _, exists := w.Events[id]; !exists || id == 0 || id >= 0x9000 || len(w.Trail) < 5 {
			return true, fmt.Errorf("party ride requires a concrete invoking event and five trail entries")
		}
	} else {
		if err := validatePartyGestures(w, true); err != nil {
			return true, fmt.Errorf("party walk: %w", err)
		}
	}
	for _, coordinate := range []int{w.PartyOffsetX, w.PartyOffsetY, w.ViewportX, w.ViewportY} {
		if !signedPartyCoordinate(coordinate) {
			return true, fmt.Errorf("party walk viewport and party offsets must be signed 16-bit coordinates")
		}
	}
	frameIndex := len(state.Frames) - 1
	parentFrame := state.Frames[frameIndex]
	nextPC, err := fallthroughPC(parentFrame.PC)
	if err != nil {
		return true, err
	}
	a := inst.Operands
	targetX, targetY := int(a[0])*32+int(a[2])*16, int(a[1])*16+int(a[2])*8
	targetViewportX, targetViewportY := targetX-w.PartyOffsetX, targetY-w.PartyOffsetY
	if !signedPartyCoordinate(targetViewportX) || !signedPartyCoordinate(targetViewportY) {
		return true, fmt.Errorf("party walk target viewport exceeds signed 16-bit range; refusing a wrapping/nonterminating path")
	}
	working := *state
	working.World = CloneWorld(w)
	working.Frames = append([]Frame(nil), state.Frames...)
	w = working.World
	speed := 2
	if inst.Opcode == 0x7a || inst.Opcode == 0x44 {
		speed = 4
	} else if inst.Opcode == 0x7b || inst.Opcode == 0x97 {
		speed = 8
	}
	record := &PartyWalkRecord{Speed: speed, TargetMapX: targetX, TargetMapY: targetY, InitialStepFrame: w.PartyStepFrame, Steps: []PartyWalkStep{}}
	if riding {
		record.RidingEvent = parentFrame.EventID
	}
	for w.ViewportX != targetViewportX || w.ViewportY != targetViewportY {
		dx, dy := targetViewportX-w.ViewportX, targetViewportY-w.ViewportY
		evidence := PartyWalkStep{Sequence: len(record.Steps) + 1, BeforeViewportX: w.ViewportX, BeforeViewportY: w.ViewportY, BeforeWorldFrame: w.FrameNum}
		copy(w.Trail[1:5], w.Trail[:4])
		w.Trail[0] = WorldTrail{X: uint16(w.ViewportX + w.PartyOffsetX), Y: uint16(w.ViewportY + w.PartyOffsetY), Direction: w.Direction}
		w.Direction = partyWalkDirection(dx, dy)
		mx, my := partyWalkDelta(dx, speed*2), partyWalkDelta(dy, speed)
		w.ViewportX += mx
		w.ViewportY += my
		if riding {
			w.Trail[0] = WorldTrail{X: uint16(w.ViewportX + w.PartyOffsetX), Y: uint16(w.ViewportY + w.PartyOffsetY), Direction: w.Direction}
			event := w.Events[record.RidingEvent]
			event.X += uint16(mx)
			event.Y += uint16(my)
			w.Events[record.RidingEvent] = event
		} else {
			if err := updatePartyGestures(r.source, w, true); err != nil {
				return true, fmt.Errorf("party walk movement %d: %w", evidence.Sequence, err)
			}
		}
		w.PartyX, w.PartyY = int(int16(w.ViewportX+w.PartyOffsetX)), int(int16(w.ViewportY+w.PartyOffsetY))
		evidence.AfterViewportX, evidence.AfterViewportY = w.ViewportX, w.ViewportY
		evidence.Direction, evidence.StepFrame = w.Direction, w.PartyStepFrame
		if len(w.PartyFrames) > 0 {
			evidence.WalkingFrame = w.PartyFrames[0]
		}
		evidence.TrailHead = w.Trail[0]
		tick := TraceStep{}
		if _, err := r.evaluateTicksCore(&working, &tick, Instruction{Opcode: 9, Operands: [3]uint16{1}}, inputs, false); err != nil {
			return true, fmt.Errorf("party walk movement %d: %w", evidence.Sequence, err)
		}
		// The synthetic wait is an internal update, not an instruction in the
		// parent script. Its next PC must never leak into the caller's frame.
		working.Frames[frameIndex] = parentFrame
		w = working.World
		if w.SceneChanged || w.SceneLoadPending || !sameParty(w, state.World) {
			return true, fmt.Errorf("party walk movement %d changed scene or party; continuation is not modeled", evidence.Sequence)
		}
		if evidence.ScreenUpdates, err = r.cameraPresent(w); err != nil {
			return true, fmt.Errorf("party walk movement %d redraw: %w", evidence.Sequence, err)
		}
		evidence.AfterWorldFrame = w.FrameNum
		evidence.AutoSteps = tick.AutoSteps
		record.Steps = append(record.Steps, evidence)
	}
	// PAL_UpdatePartyGestures(FALSE) runs even when the party was at the target.
	if !riding {
		if err := updatePartyGestures(r.source, w, false); err != nil {
			return true, fmt.Errorf("party walk standing gesture: %w", err)
		}
	}
	record.FinalStepFrame = w.PartyStepFrame
	if len(w.PartyFrames) > 0 {
		record.StandingFrame = w.PartyFrames[0]
	}
	working.Frames[frameIndex].PC = nextPC
	*state = working
	step.PartyWalk = record
	step.Ticks = uint16(len(record.Steps))
	step.Effect = fmt.Sprintf("walk party %d movements at speed %d, run bounded trigger-disabled updates, and restore standing gestures; no rendering", len(record.Steps), speed)
	if riding {
		step.Effect = fmt.Sprintf("ride event %d for %d movements at speed %d, preserve party gestures and run bounded trigger-disabled updates; no rendering", record.RidingEvent, len(record.Steps), speed)
	}
	return true, nil
}

func sameParty(a, b *WorldState) bool {
	if len(a.PartyRoles) != len(b.PartyRoles) || len(a.FollowerSprites) != len(b.FollowerSprites) {
		return false
	}
	for i := range a.PartyRoles {
		if a.PartyRoles[i] != b.PartyRoles[i] {
			return false
		}
	}
	for i := range a.FollowerSprites {
		if a.FollowerSprites[i] != b.FollowerSprites[i] {
			return false
		}
	}
	return true
}

func validatePartyGestures(world *WorldState, walking bool) error {
	if world == nil {
		return fmt.Errorf("party gestures require an explicit world snapshot")
	}
	party, followers := len(world.PartyRoles), len(world.FollowerSprites)
	if party == 0 || party+followers > 5 || followers > 2 {
		return fmt.Errorf("party gestures require 1..5 total members and at most two followers")
	}
	if world.PartyStepFrame > 3 || world.Direction > 3 {
		return fmt.Errorf("party gestures require step phase and direction in 0..3")
	}
	if len(world.PartyFrames) < party+followers || (walking && len(world.PartyPositions) < party+followers) {
		return fmt.Errorf("party gestures require position/gesture state for every party member and follower")
	}
	trailLength := 0
	if party > 1 {
		trailLength = 3
	}
	if followers > 0 {
		trailLength = 3 + followers
	}
	if walking && trailLength < 5 {
		trailLength = 5
	}
	if len(world.Trail) < trailLength {
		return fmt.Errorf("party gestures require %d trail entries", trailLength)
	}
	for i := 0; i < trailLength; i++ {
		if world.Trail[i].Direction > 3 {
			return fmt.Errorf("party gestures trail %d direction is outside 0..3", i)
		}
	}
	for _, role := range world.PartyRoles {
		if _, err := playerRole(world, role); err != nil {
			return err
		}
	}
	return nil
}

// updatePartyGestures follows scene.c, including follower animation phases and
// the collision fallback to trail[1]. Callers must use a detached world.
func updatePartyGestures(source Source, world *WorldState, walking bool) error {
	if err := validatePartyGestures(world, walking); err != nil {
		return err
	}
	party := len(world.PartyRoles)
	if !walking {
		for i, role := range world.PartyRoles {
			frames := world.Roles[role][64]
			if frames == 0 {
				frames = 3
			}
			direction := world.Direction
			if i > 0 {
				direction = world.Trail[2].Direction
			}
			world.PartyFrames[i] = direction * frames
		}
		for i := range world.FollowerSprites {
			world.PartyFrames[party+i] = world.Trail[3+i].Direction * 3
		}
		world.PartyStepFrame = (world.PartyStepFrame & 2) ^ 2
		return nil
	}
	var obstacles ObstacleSource
	if party > 1 {
		var ok bool
		obstacles, ok = source.(ObstacleSource)
		if !ok {
			return fmt.Errorf("walking with multiple party members requires a map obstacle source")
		}
	}
	world.PartyStepFrame = (world.PartyStepFrame + 1) % 4
	leaderPhase, followerPhase := uint16(0), uint16(0)
	if world.PartyStepFrame&1 != 0 {
		leaderPhase = (world.PartyStepFrame + 1) / 2
		followerPhase = 3 - leaderPhase
	}
	world.PartyPositions[0] = WorldPosition{X: uint16(world.PartyOffsetX), Y: uint16(world.PartyOffsetY)}
	for i, role := range world.PartyRoles {
		direction := world.Direction
		if i > 0 {
			trail := world.Trail[1]
			x, y := int(trail.X)-world.ViewportX, int(trail.Y)-world.ViewportY
			if i == 2 {
				if trail.Direction == 1 || trail.Direction == 3 {
					x -= 16
				} else {
					x += 16
				}
				y += 8
			} else {
				if trail.Direction == 0 || trail.Direction == 1 {
					x += 16
				} else {
					x -= 16
				}
				if trail.Direction == 1 || trail.Direction == 2 {
					y += 8
				} else {
					y -= 8
				}
			}
			// rgParty.x/y are SHORT; assignment wraps before PAL_XY packs
			// the map coordinate used by the collision function.
			position := WorldPosition{X: uint16(x), Y: uint16(y)}
			mapPosition := WorldPosition{X: uint16(int(int16(position.X)) + world.ViewportX), Y: uint16(int(int16(position.Y)) + world.ViewportY)}
			blocked, err := obstacles.IsObstacle(world, mapPosition, 0, true, true)
			if err != nil {
				return fmt.Errorf("party member %d collision: %w", i, err)
			}
			if blocked {
				position = WorldPosition{X: uint16(int(trail.X) - world.ViewportX), Y: uint16(int(trail.Y) - world.ViewportY)}
			}
			world.PartyPositions[i] = position
			direction = world.Trail[2].Direction
		}
		world.PartyFrames[i] = direction*3 + leaderPhase
		if world.Roles[role][64] == 4 {
			world.PartyFrames[i] = direction*4 + world.PartyStepFrame
		}
	}
	for i := range world.FollowerSprites {
		trail := world.Trail[3+i]
		world.PartyPositions[party+i] = WorldPosition{X: uint16(int(trail.X) - world.ViewportX), Y: uint16(int(trail.Y) - world.ViewportY)}
		world.PartyFrames[party+i] = trail.Direction*3 + followerPhase
	}
	return nil
}

func signedPartyCoordinate(n int) bool { return n >= -32768 && n <= 32767 }
func partyWalkDelta(distance, maximum int) int {
	if distance < -maximum {
		return -maximum
	}
	if distance > maximum {
		return maximum
	}
	return distance
}
func partyWalkDirection(dx, dy int) uint16 {
	if dy < 0 {
		if dx < 0 {
			return 1
		}
		return 2
	}
	if dx < 0 {
		return 0
	}
	return 3
}
