package palflow

import "fmt"

// conditionsOpcodeName covers state predicates and configuration checked against
// SDLPAL d4bf69b64d335cd680555af9df90e7ec4f3006a3 script.c, plus the poison
// lookup functions in global.c and RandomLong's degenerate range in util.c.
func conditionsOpcodeName(op uint16) string {
	switch op {
	case 0x38:
		return "call scene teleport script, or fail and jump"
	case 0x52:
		return "negate invoking event state and set vanish time"
	case 0x53:
		return "select day palette flag"
	case 0x54:
		return "select night palette flag"
	case 0x5d:
		return "jump if invoking player role lacks specified poison"
	case 0x61:
		return "jump if invoking player role lacks ordinary poison"
	case 0x6f:
		return "synchronize invoking event state when target state matches"
	case 0x71:
		return "set screen wave and signed progression"
	case 0x79:
		return "jump if a party role has specified name"
	case 0x81:
		return "jump and fail unless party faces active scene event in range"
	case 0x83:
		return "jump and fail unless scene event is in invoking event range"
	case 0x84:
		return "place item event in front of party using explicit collision source"
	case 0x86:
		return "jump if party equipped item count is below threshold"
	case 0x8b:
		return "set palette ID and record conditional palette request"
	case 0x94:
		return "jump if target event state equals signed value"
	case 0x95:
		return "jump if current scene equals specified scene"
	case 0x99:
		return "change scene map; current-scene sentinel reloads scene resources"
	case 0x9a:
		return "set signed state for inclusive event range"
	case 0xa2:
		return "skip following instructions using explicit bounded random result"
	}
	return ""
}

// evaluateConditions never commits a partial predicate/configuration change.
// Teleport runs a bounded synchronous trigger call. Its child evidence and
// shared instruction, input and text budgets also apply to auto invocations.
func (r *Runner) evaluateConditions(state *State, step *TraceStep, inst Instruction, inputs []Input) (bool, error) {
	name := conditionsOpcodeName(inst.Opcode)
	if name == "" {
		return false, nil
	}
	if state == nil || len(state.Frames) == 0 || step == nil {
		return true, fmt.Errorf("condition instruction requires state, current script frame and trace step")
	}
	if state.World == nil && inst.Opcode != 0xa2 {
		return true, fmt.Errorf("condition instruction requires an explicit world snapshot")
	}
	working := *state
	working.World = CloneWorld(state.World)
	working.Frames = append([]Frame(nil), state.Frames...)
	w := working.World
	index := len(working.Frames) - 1
	frame := working.Frames[index]
	a := inst.Operands
	evidence := *step
	evidence.Effect = name
	var nextPC uint16
	var err error
	branch := func(take bool, target uint16) error {
		if take {
			nextPC = target
			evidence.Effect += fmt.Sprintf("; jump to %d", target)
			return nil
		}
		nextPC, err = fallthroughPC(frame.PC)
		evidence.Effect += "; advance"
		return err
	}
	lookupEvent := func(id uint16) (uint16, WorldEvent, error) {
		if id == 0 || id == 0xffff {
			id = frame.EventID
		}
		if id == 0 || id == 0xffff {
			return 0, WorldEvent{}, fmt.Errorf("event condition requires a concrete invoking event context")
		}
		if id >= 0x9000 {
			return 0, WorldEvent{}, fmt.Errorf("event ID %04x requires unsupported special event addressing", id)
		}
		event, ok := w.Events[id]
		if !ok {
			return 0, WorldEvent{}, fmt.Errorf("event %d is absent from the world snapshot", id)
		}
		return id, event, nil
	}
	lookupScene := func(id uint16) (WorldScene, error) {
		if id == 0 || id > 300 {
			return WorldScene{}, fmt.Errorf("scene ID %d is outside 1..300", id)
		}
		scene, ok := w.Scenes[id]
		if !ok {
			return WorldScene{}, fmt.Errorf("scene %d is absent from the world snapshot", id)
		}
		return scene, nil
	}
	inScene := func(id uint16) (bool, error) {
		if w.Scene == 0 || w.Scene >= 300 {
			return false, fmt.Errorf("event membership requires current and next scene boundaries within 1..300")
		}
		scene, err := lookupScene(w.Scene)
		if err != nil {
			return false, err
		}
		next, err := lookupScene(w.Scene + 1)
		if err != nil {
			return false, err
		}
		if next.EventStart < scene.EventStart {
			return false, fmt.Errorf("scene %d has descending event boundaries %d..%d", w.Scene, scene.EventStart, next.EventStart)
		}
		return id > scene.EventStart && id <= next.EventStart, nil
	}
	switch inst.Opcode {
	case 0x38:
		success := false
		var scene WorldScene
		if !w.InBattle {
			scene, err = lookupScene(w.Scene)
			if err != nil {
				return true, err
			}
			success = scene.OnTeleport != 0
		}
		if !success {
			working.ScriptSuccess = false
			err = branch(true, a[0])
			break
		}
		err = branch(false, 0)
		if err != nil {
			return true, err
		}
		if err := r.sourceRequired(); err != nil {
			return true, fmt.Errorf("scene teleport: %w", err)
		}
		evidence.Children, err = r.runTriggerInline(&working, scene.OnTeleport, 0xffff, inputs, 1)
		if err != nil {
			return true, fmt.Errorf("scene teleport: %w", err)
		}
		evidence.Effect += fmt.Sprintf("; call scene teleport entry %d using last event context; replacement entry ignored", scene.OnTeleport)
	case 0x52:
		id, event, lookupErr := lookupEvent(frame.EventID)
		if lookupErr != nil {
			return true, lookupErr
		}
		event.State = -event.State // SHORT assignment retains two's-complement wrap.
		duration := a[0]
		if duration == 0 {
			duration = 800
		}
		event.VanishTime = int16(duration)
		w.Events[id] = event
		err = branch(false, 0)
	case 0x53, 0x54:
		w.NightPalette = inst.Opcode == 0x54
		err = branch(false, 0)
	case 0x5d, 0x61:
		if err = conditionParty(w); err != nil {
			return true, err
		}
		if frame.EventID >= 6 {
			return true, fmt.Errorf("poison predicate player role %d is outside 0..5", frame.EventID)
		}
		partyIndex := -1
		for i, role := range w.PartyRoles {
			if role == frame.EventID {
				partyIndex = i
				break
			}
		}
		poisoned := false
		if partyIndex >= 0 {
			for _, row := range w.PlayerPoisons {
				poison := row[partyIndex].ID
				if inst.Opcode == 0x5d {
					// PAL_IsPlayerPoisonedByKind also compares empty (zero) slots.
					if poison == a[0] {
						poisoned = true
						break
					}
				} else if poison != 0 {
					object, ok := w.Objects[poison]
					if !ok {
						return true, fmt.Errorf("poison object %d is absent from the world snapshot", poison)
					}
					if object[0] < 99 {
						poisoned = true
						break
					}
				}
			}
		}
		target := a[1]
		if inst.Opcode == 0x61 {
			target = a[0]
		}
		err = branch(!poisoned, target)
	case 0x6f:
		_, target, lookupErr := lookupEvent(a[0])
		if lookupErr != nil {
			return true, lookupErr
		}
		if target.State == int16(a[1]) {
			id, invoking, lookupErr := lookupEvent(frame.EventID)
			if lookupErr != nil {
				return true, lookupErr
			}
			invoking.State = int16(a[1])
			w.Events[id] = invoking
		}
		err = branch(false, 0)
	case 0x71:
		w.ScreenWave, w.WaveProgression = a[0], int16(a[1])
		err = branch(false, 0)
	case 0x79, 0x86:
		if err = conditionParty(w); err != nil {
			return true, err
		}
		count := 0
		for _, role := range w.PartyRoles {
			raw, lookupErr := playerRole(w, role)
			if lookupErr != nil {
				return true, lookupErr
			}
			if inst.Opcode == 0x79 {
				if raw[3] == a[0] {
					count = 1
					break
				}
			} else {
				for slot := 11; slot < 17; slot++ {
					if raw[slot] == a[0] {
						count++
					}
				}
			}
		}
		if inst.Opcode == 0x79 {
			err = branch(count != 0, a[1])
		} else {
			threshold := max(1, int(a[1]))
			// Source mutates a zero operand to one. The immutable Source table is
			// preserved; repeated execution has the same effective threshold.
			err = branch(count < threshold, a[2])
			evidence.Effect += fmt.Sprintf("; equipped count %d, effective threshold %d", count, threshold)
		}
	case 0x81, 0x83, 0x84:
		inside, lookupErr := inScene(a[0])
		if lookupErr != nil {
			return true, lookupErr
		}
		failed := !inside
		if inside {
			if inst.Opcode == 0x84 {
				if w.Direction > 3 {
					return true, fmt.Errorf("item placement requires party direction in 0..3")
				}
				if r == nil {
					return true, fmt.Errorf("item placement requires an explicit collision source")
				}
				collision, ok := r.source.(ObstacleSource)
				if !ok {
					return true, fmt.Errorf("item placement requires an explicit collision source")
				}
				x := int(int16(w.ViewportX)) + int(int16(w.PartyOffsetX))
				y := int(int16(w.ViewportY)) + int(int16(w.PartyOffsetY))
				if w.Direction == 1 || w.Direction == 0 {
					x -= 16
				} else {
					x += 16
				}
				if w.Direction == 1 || w.Direction == 2 {
					y -= 8
				} else {
					y += 8
				}
				position := WorldPosition{X: uint16(x), Y: uint16(y)}
				failed, err = collision.IsObstacle(w, position, 0, false, false)
				if err != nil {
					return true, fmt.Errorf("item placement collision query: %w", err)
				}
				if !failed {
					id, target, lookupErr := lookupEvent(a[0])
					if lookupErr != nil {
						return true, lookupErr
					}
					target.X, target.Y, target.State = position.X, position.Y, int16(a[1])
					w.Events[id] = target
				}
				if failed {
					working.ScriptSuccess = false
				}
				err = branch(failed, a[2])
				break
			}
			id, target, lookupErr := lookupEvent(a[0])
			if lookupErr != nil {
				return true, lookupErr
			}
			x, y := int(target.X), int(target.Y)
			if inst.Opcode == 0x81 {
				if w.Direction > 3 {
					return true, fmt.Errorf("facing predicate requires party direction in 0..3")
				}
				if w.Direction == 1 || w.Direction == 0 { // west or south
					x += 16
				} else {
					x -= 16
				}
				if w.Direction == 1 || w.Direction == 2 { // west or north
					y += 8
				} else {
					y -= 8
				}
				x -= int(int16(w.ViewportX)) + int(int16(w.PartyOffsetX))
				y -= int(int16(w.ViewportY)) + int(int16(w.PartyOffsetY))
				failed = conditionAbs(x)+conditionAbs(y*2) >= int(a[1])*32+16 || target.State <= 0
				if !failed && a[1] > 0 {
					target.TriggerMode = 5 + a[1]
					w.Events[id] = target
				}
			} else {
				_, invoking, lookupErr := lookupEvent(frame.EventID)
				if lookupErr != nil {
					return true, lookupErr
				}
				x, y = int(invoking.X)-x, int(invoking.Y)-y
				failed = conditionAbs(x)+conditionAbs(y*2) >= int(a[1])*32+16
			}
		}
		if failed {
			working.ScriptSuccess = false
		}
		err = branch(failed, a[2])
	case 0x8b:
		w.Palette = a[0]
		if !w.NeedToFadeIn {
			evidence.Screen = &ScreenRecord{Kind: "set_day_palette"}
			evidence.Effect += "; request day palette application without changing night flag; rendering not executed"
		}
		err = branch(false, 0)
	case 0x94:
		_, event, lookupErr := lookupEvent(a[0])
		if lookupErr != nil {
			return true, lookupErr
		}
		err = branch(event.State == int16(a[1]), a[2])
	case 0x95:
		err = branch(w.Scene == a[0], a[1])
	case 0x99:
		id := a[0]
		if id == 0xffff {
			id = w.Scene
		}
		scene, lookupErr := lookupScene(id)
		if lookupErr != nil {
			return true, lookupErr
		}
		scene.Map = a[1]
		w.Scenes[id] = scene
		err = branch(false, 0)
		if err != nil {
			return true, err
		}
		if a[0] == 0xffff {
			if r == nil {
				return true, fmt.Errorf("current-scene map replacement requires a scene resource source")
			}
			loader, ok := r.source.(SceneResourceSource)
			if !ok {
				return true, fmt.Errorf("current-scene map replacement requires a scene resource source")
			}
			entering := w.SceneChanged
			loaded, loadErr := loader.LoadResources(w, true, false)
			if loadErr != nil {
				return true, fmt.Errorf("current-scene map replacement: %w", loadErr)
			}
			if loaded == nil {
				return true, fmt.Errorf("current-scene map replacement returned no world snapshot")
			}
			working.World = CloneWorld(loaded)
			working.World.SceneChanged = entering
			evidence.Effect += "; reload scene resources without entering scene or reloading party sprites"
		}
	case 0x9a:
		for id := int(a[0]); id <= int(a[1]); id++ {
			if id == 0 {
				return true, fmt.Errorf("event range cannot include event zero")
			}
			event, ok := w.Events[uint16(id)]
			if !ok {
				return true, fmt.Errorf("event %d is absent from the world snapshot", id)
			}
			event.State = int16(a[2])
			w.Events[uint16(id)] = event
		}
		err = branch(false, 0)
	case 0xa2:
		skip := 0
		if a[0] > 1 {
			var use *InputUse
			skip, use, err = consumeRandom(&working, inputs, 0, int(a[0])-1, "script instruction skip")
			if err != nil {
				return true, fmt.Errorf("reference random result: %w", err)
			}
			evidence.Input = use
		} else {
			evidence.Effect += "; degenerate RandomLong returns zero without drawing random input"
		}

		if int(frame.PC)+skip >= 0xffff {
			return true, fmt.Errorf("random skip program counter would overflow 16-bit address space")
		}
		nextPC = frame.PC + uint16(skip) + 1
		evidence.Effect += fmt.Sprintf("; skip %d and continue at %d", skip, nextPC)
	}
	if err != nil {
		return true, err
	}
	working.Frames[index].PC = nextPC
	*state, *step = working, evidence
	return true, nil
}

func referenceRandomSkipMaximum(bound uint16) int {
	if bound <= 1 {
		return 0
	}
	const maximumRandomValue = 1<<31 - 1
	return maximumRandomValue / (maximumRandomValue / int(bound))
}

func conditionParty(w *WorldState) error {
	if len(w.PartyRoles) == 0 || len(w.PartyRoles) > 5 {
		return fmt.Errorf("party condition requires one to five explicit party roles")
	}
	for _, role := range w.PartyRoles {
		if role >= 6 {
			return fmt.Errorf("party condition role %d is outside 0..5", role)
		}
	}
	return nil
}

func conditionAbs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
