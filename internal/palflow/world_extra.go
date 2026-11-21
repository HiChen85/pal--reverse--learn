package palflow

import "fmt"

// These effects were checked against SDLPAL script.c and scene.c at
// d4bf69b64d335cd680555af9df90e7ec4f3006a3. Gesture and resource operations use
// the existing bounded model; none of these instructions advances game time.
func worldExtraOpcodeName(op uint16) string {
	switch op {
	case 0x62:
		return "pause enemy chasing for specified update cycles"
	case 0x63:
		return "increase enemy chase range for specified update cycles"
	case 0x6c:
		return "offset event position and advance gesture without a world update"
	case 0x6e:
		return "offset party viewport, save trail and update walking gestures"
	case 0x78:
		return "reserved instruction with no effect in the reference interpreter"
	case 0x7d:
		return "offset event position without advancing its gesture"
	case 0x7e:
		return "set signed event layer"
	case 0x87:
		return "advance event gesture without moving"
	case 0x98:
		return "set party followers and reload resources for each assigned follower"
	case 0xa1:
		return "overlap party positions, rebuild trail and restore standing gestures"
	}
	return ""
}

func (r *Runner) evaluateWorldExtra(state *State, step *TraceStep, inst Instruction, _ []Input) (bool, error) {
	name := worldExtraOpcodeName(inst.Opcode)
	if name == "" {
		return false, nil
	}
	if state == nil || len(state.Frames) == 0 || step == nil {
		return true, fmt.Errorf("world instruction requires state, current script frame and trace step")
	}
	if state.World == nil && inst.Opcode != 0x78 {
		return true, fmt.Errorf("world instruction requires an explicit world snapshot")
	}
	index := len(state.Frames) - 1
	nextPC, err := fallthroughPC(state.Frames[index].PC)
	if err != nil {
		return true, err
	}
	working := *state
	working.World = CloneWorld(state.World)
	working.Frames = append([]Frame(nil), state.Frames...)
	w := working.World
	evidence := *step
	evidence.Effect = name
	a := inst.Operands
	var source Source
	if r != nil {
		source = r.source
	}
	switch inst.Opcode {
	case 0x62, 0x63:
		w.ChaseCycles, w.ChaseRange = a[0], 0
		if inst.Opcode == 0x63 {
			w.ChaseRange = 3
		}
	case 0x6e:
		if len(w.Trail) < 5 {
			return true, fmt.Errorf("party offset requires all five trail entries")
		}
		copy(w.Trail[1:5], w.Trail[:4])
		w.Trail[0] = WorldTrail{
			X:         uint16(int16(w.ViewportX)) + uint16(int16(w.PartyOffsetX)),
			Y:         uint16(int16(w.ViewportY)) + uint16(int16(w.PartyOffsetY)),
			Direction: w.Direction,
		}
		w.ViewportX = int(int16(uint16(w.ViewportX) + a[0]))
		w.ViewportY = int(int16(uint16(w.ViewportY) + a[1]))
		w.Layer = int16(a[2] * 8)
		if a[0] != 0 || a[1] != 0 {
			if !signedPartyCoordinate(w.PartyOffsetX) || !signedPartyCoordinate(w.PartyOffsetY) {
				return true, fmt.Errorf("walking gestures require signed 16-bit party offsets")
			}
			if err := updatePartyGestures(source, w, true); err != nil {
				return true, fmt.Errorf("party offset walking gesture: %w", err)
			}
		}
		w.PartyX = int(int16(w.ViewportX + int(int16(w.PartyOffsetX))))
		w.PartyY = int(int16(w.ViewportY + int(int16(w.PartyOffsetY))))
	case 0x78:
		// The reference interpreter intentionally has an empty case.
	case 0x6c, 0x7d, 0x7e, 0x87:
		id := a[0]
		if id == 0 || id == 0xffff {
			id = state.Frames[index].EventID
		}
		if id == 0 || id == 0xffff || id >= 0x9000 {
			return true, fmt.Errorf("event instruction requires a concrete event without special addressing")
		}
		event, ok := w.Events[id]
		if !ok {
			return true, fmt.Errorf("event %d is absent from the world snapshot", id)
		}
		switch inst.Opcode {
		case 0x6c, 0x7d:
			event.X += a[1]
			event.Y += a[2]
			if inst.Opcode == 0x6c {
				walkNPCOneStep(&event, 0)
			}
		case 0x7e:
			event.Layer = int16(a[1])
		case 0x87:
			walkNPCOneStep(&event, 0)
		}
		w.Events[id] = event
	case 0x98:
		if a[0] == 0 && a[1] == 0 {
			w.FollowerSprites = nil
			break
		}
		loader, ok := source.(SceneResourceSource)
		if !ok {
			return true, fmt.Errorf("setting followers requires a local scene/player resource source")
		}
		party := len(w.PartyRoles)
		if party < 1 || party > 5 || len(w.FollowerSprites) > 2 || len(w.PartyPositions) < party || len(w.PartyFrames) < party {
			return true, fmt.Errorf("setting followers requires a valid explicit party/follower snapshot")
		}
		if a[0] == 0 && (len(w.FollowerSprites) < 1 || len(w.PartyPositions) <= party || len(w.PartyFrames) <= party) {
			return true, fmt.Errorf("second follower assignment requires the preserved first follower slot")
		}
		evidence.Resources = append([]SpriteResource(nil), step.Resources...)
		for i := 0; i < 2; i++ {
			if a[i] == 0 {
				continue
			}
			count := i + 1
			if party+count > 5 || len(w.Trail) <= 3+i {
				return true, fmt.Errorf("follower %d requires a free party slot and trail entry %d", count, 3+i)
			}
			followers := make([]uint16, count)
			copy(followers, w.FollowerSprites)
			followers[i] = a[i]
			w.FollowerSprites = followers
			if len(w.PartyPositions) < party+count {
				w.PartyPositions = append(w.PartyPositions, make([]WorldPosition, party+count-len(w.PartyPositions))...)
			}
			if len(w.PartyFrames) < party+count {
				w.PartyFrames = append(w.PartyFrames, make([]uint16, party+count-len(w.PartyFrames))...)
			}
			loaded, err := loader.LoadResources(w, w.SceneLoadPending, true)
			if err != nil {
				return true, fmt.Errorf("follower %d resource load: %w", count, err)
			}
			if loaded == nil || !sameParty(w, loaded) || len(loaded.PartyPositions) < party+count || len(loaded.PartyFrames) < party+count || len(loaded.Trail) <= 3+i {
				return true, fmt.Errorf("follower %d resource load returned an incomplete or changed party snapshot", count)
			}
			w = CloneWorld(loaded)
			working.World = w
			trail := w.Trail[3+i]
			w.PartyPositions[party+i] = WorldPosition{
				X: trail.X - uint16(int16(w.ViewportX)),
				Y: trail.Y - uint16(int16(w.ViewportY)),
			}
			w.PartyFrames[party+i] = trail.Direction * 3
			evidence.Resources = append(evidence.Resources, w.PlayerResources...)
		}
	case 0xa1:
		party := len(w.PartyRoles)
		if party < 1 || len(w.PartyPositions) < party || len(w.Trail) < 5 {
			return true, fmt.Errorf("party overlap requires explicit party positions and all five trail entries")
		}
		leader := w.PartyPositions[0]
		head := WorldTrail{
			X:         leader.X + uint16(int16(w.ViewportX)),
			Y:         leader.Y + uint16(int16(w.ViewportY)),
			Direction: w.Direction,
		}
		for i := 0; i < 5; i++ {
			w.Trail[i] = head
		}
		for i := 1; i < party; i++ {
			w.PartyPositions[i] = WorldPosition{X: leader.X, Y: leader.Y - 1}
		}
		if err := updatePartyGestures(source, w, false); err != nil {
			return true, fmt.Errorf("party overlap standing gesture: %w", err)
		}
		w.PartyX, w.PartyY = int(int16(head.X)), int(int16(head.Y))
	}
	working.Frames[index].PC = nextPC
	*state, *step = working, evidence
	return true, nil
}
