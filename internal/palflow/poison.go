package palflow

import (
	"fmt"
	"slices"

	"github.com/HiChen85/pal--reverse--learn/internal/palcombat"
)

func poisonOpcodeName(op uint16) string {
	switch op {
	case 0x28:
		return "apply enemy poison with immediate bounded poison callback"
	case 0x29:
		return "apply player poison with equipment resistance and callback"
	case 0x2a:
		return "cure one matching enemy poison"
	case 0x2b:
		return "cure player poison by kind"
	case 0x2c:
		return "cure player poisons by level"
	case 0x5e:
		return "jump when enemy lacks a specified poison"
	case 0x5f:
		return "set invoking player's HP to zero"
	case 0x8a:
		return "enable automatic action for the next battle"
	}
	return ""
}

func (r *Runner) evaluatePoison(state *State, step *TraceStep, inst Instruction, inputs []Input) (bool, error) {
	name := poisonOpcodeName(inst.Opcode)
	if name == "" {
		return false, nil
	}
	if state == nil || step == nil || state.World == nil || len(state.Frames) == 0 {
		return true, fmt.Errorf("poison instruction needs a world and current frame")
	}
	working := *state
	working.World = CloneWorld(state.World)
	working.Frames = append([]Frame(nil), state.Frames...)
	evidence := *step
	evidence.Children = slices.Clone(step.Children)
	evidence.RandomInputs = slices.Clone(step.RandomInputs)
	index := len(working.Frames) - 1
	frame := working.Frames[index]
	a := inst.Operands
	next, err := fallthroughPC(frame.PC)
	if err != nil {
		return true, err
	}
	w := working.World
	switch inst.Opcode {
	case 0x8a:
		w.AutoBattle = true
	case 0x5f:
		raw, err := playerRole(w, frame.EventID)
		if err != nil {
			return true, err
		}
		raw[9] = 0
		w.Roles[frame.EventID] = raw
	case 0x28, 0x2a, 0x5e:
		if err := battleSnapshot(w); err != nil {
			return true, err
		}
		all := inst.Opcode != 0x5e && a[0] != 0
		first := int(frame.EventID)
		if all {
			first = 0
		}
		for slot := first; ; slot++ {
			if all {
				if slot > topologyExtent(working.World.Battle) {
					break
				}
				// All-target poison/cure checks the current slot's object at
				// visitation time. Earlier callbacks may fill or extend slots.
				current := topologyEnemy(working.World.Battle, slot)
				if current == nil || !occupiedEnemy(*current) {
					continue
				}
			} else if slot != first {
				break
			}
			x, err := battleEnemy(working.World, uint16(slot))
			if err != nil {
				return true, err
			}
			if inst.Opcode == 0x5e {
				found := false
				for _, poison := range x.Poisons {
					found = found || poison.ID == a[0]
				}
				if !found {
					next = a[1]
				}
				continue
			}
			if inst.Opcode == 0x2a {
				for i, poison := range x.Poisons {
					if poison.ID == a[1] {
						x.Poisons[i] = palcombat.Poison{}
						break
					}
				}
				continue
			}
			id := battleObjectID(*x)
			object, ok := working.World.Objects[id]
			if !ok {
				return true, fmt.Errorf("enemy object %d missing", id)
			}
			value, err := battleRandom(&working, &evidence, inputs, 0, 9)
			if err != nil {
				return true, err
			}
			if value < int(object[1]) {
				continue
			}
			duplicate := false
			for _, poison := range x.Poisons {
				duplicate = duplicate || poison.ID == a[1]
			}
			if duplicate {
				continue
			}
			for i, poison := range x.Poisons {
				if poison.ID != 0 {
					continue
				}
				definition, ok := working.World.Objects[a[1]]
				if !ok {
					return true, fmt.Errorf("poison object %d missing", a[1])
				}
				x.Poisons[i].ID = a[1]
				children, err := r.runTriggerInline(&working, definition[4], frame.EventID, inputs, 1)
				if err != nil {
					return true, err
				}
				evidence.Children = append(evidence.Children, children...)
				x, err = battleEnemy(working.World, uint16(slot))
				if err != nil {
					return true, err
				}
				x.Poisons[i].Script = working.LastInlineReturn
				break
			}
		}
	case 0x29, 0x2b, 0x2c:
		roles := []uint16{frame.EventID}
		if a[0] != 0 {
			roles = slices.Clone(w.PartyRoles)
		}
		for _, role := range roles {
			raw, err := playerRole(working.World, role)
			if err != nil {
				return true, err
			}
			if inst.Opcode == 0x29 {
				resistance := raw[22]
				for slot := range 7 {
					resistance += working.World.EquipmentEffects[slot][22][role]
				}
				resistance = min(resistance, 100)
				value, err := battleRandom(&working, &evidence, inputs, 1, 100)
				if err != nil {
					return true, err
				}
				if value <= int(resistance) {
					continue
				}
			}
			member := slices.Index(working.World.PartyRoles, role)
			if member < 0 {
				continue
			}
			if member >= 5 {
				return true, fmt.Errorf("party poison slot %d outside 0..4", member)
			}
			if inst.Opcode == 0x2b || inst.Opcode == 0x2c {
				for i := range 16 {
					poison := working.World.PlayerPoisons[i][member]
					clear := poison.ID == a[1]
					if inst.Opcode == 0x2c {
						object, ok := working.World.Objects[poison.ID]
						if !ok {
							return true, fmt.Errorf("poison object %d missing", poison.ID)
						}
						clear = object[0] <= a[1]
					}
					if clear {
						working.World.PlayerPoisons[i][member] = WorldPoison{}
					}
				}
				continue
			}
			for i := range 16 {
				poison := working.World.PlayerPoisons[i][member]
				if poison.ID == 0 {
					definition, ok := working.World.Objects[a[1]]
					if !ok {
						return true, fmt.Errorf("poison object %d missing", a[1])
					}
					working.World.PlayerPoisons[i][member].ID = a[1]
					children, err := r.runTriggerInline(&working, definition[2], role, inputs, 1)
					if err != nil {
						return true, err
					}
					evidence.Children = append(evidence.Children, children...)
					working.World.PlayerPoisons[i][member].Script = working.LastInlineReturn
					break
				}
				if poison.ID == a[1] {
					break
				}
			}
		}
	}
	working.Frames[index].PC = next
	evidence.Effect = name
	*state, *step = working, evidence
	return true, nil
}
