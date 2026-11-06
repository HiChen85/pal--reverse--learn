package palflow

import (
	"fmt"
	"slices"
)

// PlayerSubstep preserves the exact bounded OnEquip instruction evidence. A
// party change is committed only after every invoked equipment script succeeds.
type PlayerSubstep struct {
	PC            uint16    `json:"pc"`
	Opcode        uint16    `json:"opcode"`
	Operands      [3]uint16 `json:"operands"`
	Role          uint16    `json:"role"`
	Item          uint16    `json:"item"`
	Slot          int       `json:"slot"`
	Effect        string    `json:"effect"`
	ReturnedEntry *uint16   `json:"returned_entry,omitempty"`
}

func playerOpcodeName(op uint16) string {
	switch op {
	case 0x17:
		return "set equipment extra attribute"
	case 0x18:
		return "equip item and exchange ordered inventory entries"
	case 0x19:
		return "change raw player attribute by signed amount"
	case 0x1a:
		return "set raw player/equipment attribute"
	case 0x1b:
		return "change living player HP by signed amount"
	case 0x1c:
		return "change living player MP by signed amount"
	case 0x1d:
		return "change living player HP and MP by signed amount"
	case 0x20:
		return "remove inventory then party equipment, or jump if insufficient"
	case 0x22:
		return "revive dead player and cure removable status and low-level poison"
	case 0x23:
		return "unequip specified role and return items to inventory"
	case 0x2d:
		return "set explicitly modeled player status"
	case 0x2f:
		return "remove player status unless supplied by equipment"
	case 0x30:
		return "set temporary player attribute by signed base percentage"
	case 0x55:
		return "add magic to first empty player magic slot"
	case 0x56:
		return "remove first matching player magic without compacting"
	case 0x57:
		return "set magic base damage from player MP and consume all MP"
	case 0x74:
		return "jump if any party member has less than maximum HP"
	case 0x75:
		return "set party and rebuild bounded linear equipment effects"
	case 0x90:
		return "set normalized object raw script field"
	}
	return ""
}

func (r *Runner) evaluatePlayer(state *State, step *TraceStep, inst Instruction) (bool, error) {
	if playerOpcodeName(inst.Opcode) == "" {
		return false, nil
	}
	if state == nil || len(state.Frames) == 0 || step == nil {
		return true, fmt.Errorf("player instruction requires state, frame and trace step")
	}
	if state.World == nil {
		return true, fmt.Errorf("player instruction requires an explicit world snapshot")
	}
	frame := state.Frames[len(state.Frames)-1]
	var err error
	var jump *uint16
	working := *state
	working.World = CloneWorld(state.World)
	var evidence []PlayerSubstep
	if inst.Opcode == 0x75 {
		evidence, err = r.setPartyAndEquip(&working, inst)
	} else {
		jump, err = applyPlayerTransition(&working, frame.EventID, inst)
	}
	if err != nil {
		return true, err
	}
	var nextPC uint16
	if jump != nil {
		nextPC = *jump
	} else {
		nextPC, err = fallthroughPC(frame.PC)
		if err != nil {
			return true, err
		}
	}
	state.World = working.World
	state.CurEquipPart = working.CurEquipPart
	state.ScriptSuccess = working.ScriptSuccess
	state.LastEventID = working.LastEventID
	state.PlayerSubsteps = working.PlayerSubsteps
	state.Frames[len(state.Frames)-1].PC = nextPC
	step.PlayerSubsteps = evidence
	step.Effect = playerOpcodeName(inst.Opcode)
	if inst.Opcode == 0x75 {
		step.Resources = append([]SpriteResource(nil), working.World.PlayerResources...)
		step.Effect += "; current party/follower MGO sprites decoded; no rendering or combat simulated"
	}
	return true, nil
}

func playerRole(world *WorldState, role uint16) ([75]uint16, error) {
	if role >= 6 {
		return [75]uint16{}, fmt.Errorf("player role %d is outside 0..5", role)
	}
	raw, ok := world.Roles[role]
	if !ok {
		return raw, fmt.Errorf("player role %d is absent from world snapshot", role)
	}
	return raw, nil
}
func effectSlot(operand uint16, includeExtra bool) (int, error) {
	maximum := uint16(0x10)
	if includeExtra {
		maximum = 0x11
	}
	if operand < 0xb || operand > maximum {
		return 0, fmt.Errorf("equipment operand %04x is outside 0x000b..0x%04x", operand, maximum)
	}
	return int(operand - 0xb), nil
}
func applyPlayer(state *State, role uint16, inst Instruction) error {
	world := state.World
	a := inst.Operands
	switch inst.Opcode {
	case 0x17:
		if _, err := playerRole(world, role); err != nil {
			return err
		}
		slot, err := effectSlot(a[0], true)
		if err != nil {
			return err
		}
		if a[1] >= 75 {
			return fmt.Errorf("equipment attribute %d is outside 0..74", a[1])
		}
		world.EquipmentEffects[slot][a[1]][role] = a[2]
	case 0x18:
		raw, err := playerRole(world, role)
		if err != nil {
			return err
		}
		slot, err := effectSlot(a[0], false)
		if err != nil {
			return err
		}
		if err := removeEquipmentEffects(world, role, slot); err != nil {
			return err
		}
		state.CurEquipPart = slot
		if old := raw[11+slot]; old != a[1] {
			slots, _, err := checkedInventory(world, true)
			if err != nil {
				return fmt.Errorf("equipment swap: %w", err)
			}
			index, found := inventoryIndex(slots, a[1])
			_, oldFound := inventoryIndex(slots, old)
			if found && index < inventoryCapacity && slots[index].Amount == 1 && old != 0 && !oldFound {
				slots[index].Item = old
			} else {
				changeInventorySlots(slots, a[1], -1)
				if old != 0 {
					changeInventorySlots(slots, old, 1)
				}
			}
			raw[11+slot] = a[1]
			world.Roles[role] = raw
			world.InventorySlots, world.Inventory = slots, inventoryCounts(slots)
			world.LastUnequippedItem = old
		}
	case 0x1a:
		if a[2] != 0 {
			role = a[2] - 1
		}
		raw, err := playerRole(world, role)
		if err != nil {
			return err
		}
		if a[0] >= 75 {
			return fmt.Errorf("player attribute %d is outside 0..74", a[0])
		}
		if state.CurEquipPart != -1 {
			if state.CurEquipPart < 0 || state.CurEquipPart >= 7 {
				return fmt.Errorf("current equipment part %d is outside -1..6", state.CurEquipPart)
			}
			world.EquipmentEffects[state.CurEquipPart][a[0]][role] = a[1]
		} else {
			raw[a[0]] = a[1]
			world.Roles[role] = raw
			// Attribute 2 is the canonical scene sprite. Keep an explicitly
			// supplied mirror coherent while preserving an omitted partial cache.
			if a[0] == 2 && world.PlayerSprites != nil {
				world.PlayerSprites[role] = a[1]
			}
		}
	case 0x2d:
		raw, err := playerRole(world, role)
		if err != nil {
			return err
		}
		status, rounds := a[0], a[1]
		if status >= 9 {
			return fmt.Errorf("player status %d is outside 0..8", status)
		}
		mode := world.PlayerStatusMode
		if mode != "" && mode != "classic" && mode != "modern" {
			return fmt.Errorf("unknown player status mode %q", mode)
		}
		if (status == 1 || status == 7) && mode == "" {
			return fmt.Errorf("status %d requires explicit classic/modern status mode", status)
		}
		// This upstream modern-only conflict path returns without a BOOL value.
		// Do not invent a ScriptSuccess result for that undefined return value.
		if mode == "modern" && ((status == 1 && world.PlayerStatus[role][7] > 0) || (status == 7 && world.PlayerStatus[role][1] > 0)) {
			return fmt.Errorf("modern slow/haste conflict has an undefined BOOL return in inspected source; not modeled")
		}
		existing := world.PlayerStatus[role][status]
		switch status {
		case 0, 1, 2, 3:
			if existing == 0 {
				world.PlayerStatus[role][status] = rounds
			}
		case 4:
			if raw[9] == 0 {
				if existing < rounds {
					world.PlayerStatus[role][status] = rounds
				}
			} else {
				state.ScriptSuccess = false
			}
		case 5, 6, 7, 8:
			if raw[9] != 0 && existing < rounds {
				world.PlayerStatus[role][status] = rounds
			}
		}
	default:
		return applyMorePlayer(state, role, inst)
	}
	return nil
}
func removeEquipmentEffects(world *WorldState, role uint16, slot int) error {
	for attr := range 75 {
		world.EquipmentEffects[slot][attr][role] = 0
	}
	if slot == 3 {
		world.PlayerStatus[role][8] = 0
	}
	if slot != 5 {
		return nil
	}
	partyIndex := slices.Index(world.PartyRoles, role)
	if partyIndex < 0 {
		return nil
	}
	if partyIndex >= 5 {
		return fmt.Errorf("poison party index %d is outside 0..4", partyIndex)
	}
	var kept []WorldPoison
	for index := range 16 {
		poison := world.PlayerPoisons[index][partyIndex]
		if poison.ID == 0 {
			break
		}
		object, ok := world.Objects[poison.ID]
		if !ok {
			return fmt.Errorf("poison object %d is absent from snapshot", poison.ID)
		}
		if object[0] < 99 {
			kept = append(kept, poison)
		}
	}
	for index := range 16 {
		world.PlayerPoisons[index][partyIndex] = WorldPoison{}
		if index < len(kept) {
			world.PlayerPoisons[index][partyIndex] = kept[index]
		}
	}
	return nil
}

func (r *Runner) setPartyAndEquip(state *State, inst Instruction) ([]PlayerSubstep, error) {
	world := state.World
	party := make([]uint16, 0, 3)
	for _, argument := range inst.Operands {
		if argument != 0 {
			role := argument - 1
			if _, err := playerRole(world, role); err != nil {
				return nil, err
			}
			party = append(party, role)
		}
	}
	if len(party) == 0 {
		if _, err := playerRole(world, 0); err != nil {
			return nil, err
		}
		party = append(party, 0)
	}
	// PAL stores followers in the same five raw slots as party roles. Changing
	// the party length shifts which stale slot a follower reload reads. Our
	// detached follower list cannot reconstruct that overlap safely.
	if len(world.FollowerSprites) > 0 && len(party) != len(world.PartyRoles) {
		return nil, fmt.Errorf("party length change with followers requires the original overlapping raw party slots")
	}
	world.PartyRoles = party
	if err := r.loadPartySprites(world); err != nil {
		return nil, err
	}
	world.PlayerPoisons = [16][5]WorldPoison{}
	return r.rebuildEquipment(state, 1)
}

// InitializeEquipment models PAL_UpdateEquipments before the first scene runs.
// It neither changes the party nor reloads sprites or globally clears poisons.
// The detached result includes the equipment callbacks' final LastEventID,
// ScriptSuccess and returned entry points. No world or callback prefix is
// committed on failure. Its budget counts only actual callback instructions.
func (r *Runner) InitializeEquipment(world *WorldState) (State, []PlayerSubstep, error) {
	if world == nil {
		return State{}, nil, fmt.Errorf("equipment initialization requires an explicit world snapshot")
	}
	state := State{World: CloneWorld(world), CurEquipPart: -1, ScriptSuccess: true}
	evidence, err := r.rebuildEquipment(&state, 0)
	if err != nil {
		return State{}, nil, err
	}
	state.Finished = true
	return state, evidence, nil
}

// rebuildEquipment implements the common PAL_UpdateEquipments pass. Caller
// decides whether a surrounding party opcode reserves one shared budget unit.
func (r *Runner) rebuildEquipment(state *State, reserveOuter int) ([]PlayerSubstep, error) {
	world := state.World
	// PAL_UpdateEquipments visits every role, including roles outside the party.
	for role := uint16(0); role < 6; role++ {
		if _, err := playerRole(world, role); err != nil {
			return nil, err
		}
	}
	remaining := r.options.MaxSteps - state.ExecutedSteps - state.PlayerSubsteps - state.WorldTicks - state.AutoSubsteps - state.CombatWork - reserveOuter
	if remaining < 0 {
		return nil, fmt.Errorf("no shared instruction budget remains for equipment rebuild")
	}
	world.EquipmentEffects = [7][75][6]uint16{}
	var evidence []PlayerSubstep
	for role := uint16(0); role < 6; role++ {
		for slot := range 6 {
			raw := world.Roles[role]
			item := raw[11+slot]
			if item == 0 {
				continue
			}
			object, ok := world.Objects[item]
			if !ok {
				return nil, fmt.Errorf("equipped item object %d for role %d slot %d is absent", item, role, slot)
			}
			pc, entry := object[3], object[3]
			state.LastEventID = role
			state.ScriptSuccess = true
			for pc != 0 {
				if len(evidence) >= remaining {
					return nil, fmt.Errorf("shared instruction budget exceeded by OnEquip role %d item %d at PC %04x", role, item, pc)
				}
				instruction, err := r.source.Instruction(pc)
				if err != nil {
					return nil, fmt.Errorf("OnEquip role %d item %d PC %04x: %w", role, item, pc, err)
				}
				child := PlayerSubstep{PC: pc, Opcode: instruction.Opcode, Operands: instruction.Operands, Role: role, Item: item, Slot: slot}
				switch instruction.Opcode {
				case 0:
					child.Effect = "stop and preserve OnEquip entry"
				case 1:
					entry, err = fallthroughPC(pc)
					if err != nil {
						return nil, fmt.Errorf("OnEquip role %d item %d PC %04x: %w", role, item, pc, err)
					}
					child.Effect = "stop and replace OnEquip entry with following instruction"
				case 0x17, 0x18, 0x19, 0x1a, 0x1b, 0x1c, 0x1d,
					0x22, 0x23, 0x2d, 0x2f, 0x30, 0x55, 0x56, 0x57, 0x90:
					if err = applyPlayer(state, role, instruction); err != nil {
						return nil, fmt.Errorf("OnEquip role %d item %d PC %04x opcode %04x operands %04x: %w", role, item, pc, instruction.Opcode, instruction.Operands, err)
					}
					child.Effect = playerOpcodeName(instruction.Opcode)
				default:
					return nil, fmt.Errorf("OnEquip role %d item %d PC %04x opcode %04x operands %04x is outside bounded linear equipment subset", role, item, pc, instruction.Opcode, instruction.Operands)
				}
				if instruction.Opcode == 0 || instruction.Opcode == 1 {
					returned := entry
					child.ReturnedEntry = &returned
					evidence = append(evidence, child)
					break
				}
				evidence = append(evidence, child)
				pc, err = fallthroughPC(pc)
				if err != nil {
					return nil, fmt.Errorf("OnEquip role %d item %d PC overflow: %w", role, item, err)
				}
			}
			state.CurEquipPart = -1 // PAL_RunTriggerScript resets this on every return.
			object = world.Objects[item]
			object[3] = entry
			world.Objects[item] = object
		}
	}
	state.PlayerSubsteps += len(evidence)
	return evidence, nil
}
