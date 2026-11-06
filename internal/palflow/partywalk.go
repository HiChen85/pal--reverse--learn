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
}

type PartyWalkRecord struct {
	Speed            int             `json:"speed"`
	TargetMapX       int             `json:"target_map_x"`
	TargetMapY       int             `json:"target_map_y"`
	InitialStepFrame uint16          `json:"initial_step_frame"`
	FinalStepFrame   uint16          `json:"final_step_frame"`
	StandingFrame    uint16          `json:"standing_frame"`
	Steps            []PartyWalkStep `json:"steps"`
}

func partyWalkOpcodeName(op uint16) string {
	if op == 0x70 {
		return "walk one-member party at speed 2 with bounded auto updates"
	}
	return ""
}

// evaluatePartyWalk models PAL_PartyWalkTo at speed 2 only for one party member
// without followers. The leader branch has no collision query. Other members
// would require PAL_UpdatePartyGestures' map-obstacle checks and are refused.
// Every tick runs the existing bounded PAL_GameUpdate(FALSE) subset. The whole
// instruction, including consumed inputs, rolls back if any tick is unsupported.
func (r *Runner) evaluatePartyWalk(state *State, step *TraceStep, inst Instruction, inputs []Input) (bool, error) {
	if inst.Opcode != 0x70 {
		return false, nil
	}
	if state == nil || len(state.Frames) == 0 || state.World == nil || step == nil {
		return true, fmt.Errorf("party walk requires a script frame, world snapshot and trace step")
	}
	if r == nil || r.options.MaxSteps < 1 {
		return true, fmt.Errorf("party walk requires a positive execution budget")
	}
	remaining := r.options.MaxSteps
	for _, used := range []int{state.ExecutedSteps, state.PlayerSubsteps, state.WorldTicks, state.AutoSubsteps, 1} {
		if used < 0 || used > remaining {
			return true, fmt.Errorf("party walk exceeds the combined execution budget")
		}
		remaining -= used
	}
	w := state.World
	if len(w.PartyRoles) != 1 || len(w.FollowerSprites) != 0 {
		return true, fmt.Errorf("party walk supports exactly one party member without followers; other members require collision checks")
	}
	if w.InBattle || w.SceneLoadPending || w.SceneChanged {
		return true, fmt.Errorf("party walk requires an entered overworld scene without pending scene resources")
	}
	if w.PartyStepFrame > 3 || w.Direction > 3 {
		return true, fmt.Errorf("party walk requires step phase and direction in 0..3")
	}
	if len(w.Trail) < 5 || len(w.PartyPositions) < 1 || len(w.PartyFrames) < 1 {
		return true, fmt.Errorf("party walk requires five trail entries and leader position/gesture state")
	}
	for _, coordinate := range []int{w.PartyOffsetX, w.PartyOffsetY, w.ViewportX, w.ViewportY} {
		if !signedPartyCoordinate(coordinate) {
			return true, fmt.Errorf("party walk viewport and party offsets must be signed 16-bit coordinates")
		}
	}
	raw, err := playerRole(w, w.PartyRoles[0])
	if err != nil {
		return true, err
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
	record := &PartyWalkRecord{Speed: 2, TargetMapX: targetX, TargetMapY: targetY, InitialStepFrame: w.PartyStepFrame, Steps: []PartyWalkStep{}}
	for w.ViewportX != targetViewportX || w.ViewportY != targetViewportY {
		dx, dy := targetViewportX-w.ViewportX, targetViewportY-w.ViewportY
		evidence := PartyWalkStep{Sequence: len(record.Steps) + 1, BeforeViewportX: w.ViewportX, BeforeViewportY: w.ViewportY, BeforeWorldFrame: w.FrameNum}
		copy(w.Trail[1:5], w.Trail[:4])
		w.Trail[0] = WorldTrail{X: uint16(w.ViewportX + w.PartyOffsetX), Y: uint16(w.ViewportY + w.PartyOffsetY), Direction: w.Direction}
		w.Direction = partyWalkDirection(dx, dy)
		w.ViewportX += partyWalkDelta(dx, 4)
		w.ViewportY += partyWalkDelta(dy, 2)
		w.PartyStepFrame = (w.PartyStepFrame + 1) % 4
		leaderFrame := uint16(0)
		if w.PartyStepFrame&1 != 0 {
			leaderFrame = (w.PartyStepFrame + 1) / 2
		}
		w.PartyFrames[0] = w.Direction*3 + leaderFrame
		if raw[64] == 4 {
			w.PartyFrames[0] = w.Direction*4 + w.PartyStepFrame
		}
		w.PartyPositions[0] = WorldPosition{X: uint16(w.PartyOffsetX), Y: uint16(w.PartyOffsetY)}
		w.PartyX, w.PartyY = int(int16(w.ViewportX+w.PartyOffsetX)), int(int16(w.ViewportY+w.PartyOffsetY))
		evidence.AfterViewportX, evidence.AfterViewportY = w.ViewportX, w.ViewportY
		evidence.Direction, evidence.WalkingFrame, evidence.StepFrame = w.Direction, w.PartyFrames[0], w.PartyStepFrame
		evidence.TrailHead = w.Trail[0]
		tick := TraceStep{}
		if _, err := r.evaluateTicks(&working, &tick, Instruction{Opcode: 9, Operands: [3]uint16{1}}, inputs); err != nil {
			return true, fmt.Errorf("party walk movement %d: %w", evidence.Sequence, err)
		}
		// The synthetic wait is an internal update, not an instruction in the
		// parent script. Its next PC must never leak into the caller's frame.
		working.Frames[frameIndex] = parentFrame
		w = working.World
		if w.SceneChanged || w.SceneLoadPending || len(w.PartyRoles) != 1 || len(w.FollowerSprites) != 0 || w.PartyRoles[0] != state.World.PartyRoles[0] {
			return true, fmt.Errorf("party walk movement %d changed scene or party; continuation is not modeled", evidence.Sequence)
		}
		evidence.AfterWorldFrame = w.FrameNum
		evidence.AutoSteps = tick.AutoSteps
		record.Steps = append(record.Steps, evidence)
	}
	// PAL_UpdatePartyGestures(FALSE) runs even when the party was at the target.
	standingFrames := raw[64]
	if standingFrames == 0 {
		standingFrames = 3
	}
	w.PartyFrames[0] = w.Direction * standingFrames
	w.PartyStepFrame = (w.PartyStepFrame & 2) ^ 2
	record.StandingFrame, record.FinalStepFrame = w.PartyFrames[0], w.PartyStepFrame
	working.Frames[frameIndex].PC = nextPC
	*state = working
	step.PartyWalk = record
	step.Ticks = uint16(len(record.Steps))
	step.Effect = fmt.Sprintf("walk single-member party %d movements at speed 2, run bounded trigger-disabled updates, and restore standing gesture; no rendering", len(record.Steps))
	return true, nil
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
