package palflow

import (
	"fmt"
	"math"
	"slices"
)

// Player role operands are zero-based only when they explicitly name a role
// (0x23); the optional selectors in 0x19/0x30 and 0x55/0x56 are one-based.
// Party-wide operations use the ordered party list rather than a role prefix.
func playerTargets(world *WorldState, role uint16, all bool) ([]uint16, error) {
	roles := []uint16{role}
	if all {
		if len(world.PartyRoles) == 0 || len(world.PartyRoles) > 5 {
			return nil, fmt.Errorf("party-wide player instruction requires 1..5 explicit party roles")
		}
		roles = world.PartyRoles
	}
	for _, target := range roles {
		if _, err := playerRole(world, target); err != nil {
			return nil, err
		}
	}
	return roles, nil
}

func applyPlayerTransition(state *State, role uint16, inst Instruction) (*uint16, error) {
	if inst.Opcode == 0x20 {
		return removePlayerItems(state.World, inst)
	}
	if inst.Opcode == 0x74 {
		roles, err := playerTargets(state.World, role, true)
		if err != nil {
			return nil, err
		}
		for _, target := range roles {
			raw := state.World.Roles[target]
			if raw[9] < raw[7] {
				jump := inst.Operands[0]
				return &jump, nil
			}
		}
		return nil, nil
	}
	return nil, applyPlayer(state, role, inst)
}

func applyMorePlayer(state *State, role uint16, inst Instruction) error {
	world, a := state.World, inst.Operands
	switch inst.Opcode {
	case 0x19, 0x30:
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
		if inst.Opcode == 0x30 {
			world.EquipmentEffects[6][a[0]][role] = uint16(int32(raw[a[0]]) * int32(int16(a[1])) / 100)
		} else {
			raw[a[0]] += a[1] // Signed addition assigned to WORD preserves these bits.
			world.Roles[role] = raw
			if a[0] == 2 && world.PlayerSprites != nil {
				world.PlayerSprites[role] = raw[a[0]]
			}
		}
	case 0x1b, 0x1c, 0x1d:
		roles, err := playerTargets(world, role, a[0] != 0)
		if err != nil {
			return err
		}
		all := a[0] != 0
		if all && inst.Opcode == 0x1b {
			state.ScriptSuccess = false
		}
		for _, target := range roles {
			hp, mp := uint16(0), uint16(0)
			if inst.Opcode != 0x1c {
				hp = a[1]
			}
			if inst.Opcode != 0x1b {
				mp = a[1]
			}
			changed := increasePlayerHPMP(world, target, hp, mp)
			if all && inst.Opcode == 0x1b && changed {
				state.ScriptSuccess = true
			} else if !all && !changed {
				state.ScriptSuccess = false
			}
		}
	case 0x22:
		roles, err := playerTargets(world, role, a[0] != 0)
		if err != nil {
			return err
		}
		all := a[0] != 0
		if all {
			state.ScriptSuccess = false
		}
		for _, target := range roles {
			raw := world.Roles[target]
			if raw[9] != 0 {
				if !all {
					state.ScriptSuccess = false
				}
				continue
			}
			product := uint32(raw[7]) * uint32(a[1])
			if product > math.MaxInt32 {
				return fmt.Errorf("revive HP product %d overflows source signed int arithmetic", product)
			}
			raw[9] = uint16(product / 10)
			world.Roles[target] = raw
			if err := cureRevivedPlayerPoison(world, target); err != nil {
				return err
			}
			for status := range 9 {
				removePlayerStatus(world, target, status)
			}
			if all {
				state.ScriptSuccess = true
			}
		}
	case 0x23:
		role = a[0]
		if _, err := playerRole(world, role); err != nil {
			return err
		}
		if a[1] > 6 {
			return fmt.Errorf("unequip part %d is outside 0..6", a[1])
		}
		first, last := 0, 6
		if a[1] != 0 {
			first, last = int(a[1]-1), int(a[1])
		}
		for slot := first; slot < last; slot++ {
			raw := world.Roles[role]
			item := raw[11+slot]
			if a[1] != 0 && item == 0 {
				continue
			}
			if err := removeEquipmentEffects(world, role, slot); err != nil {
				return err
			}
			if item != 0 {
				slots, _, err := checkedInventory(world, true)
				if err != nil {
					return err
				}
				changeInventorySlots(slots, item, 1)
				world.InventorySlots, world.Inventory = slots, inventoryCounts(slots)
				raw[11+slot] = 0
				world.Roles[role] = raw
			}
		}
	case 0x2f:
		if _, err := playerRole(world, role); err != nil {
			return err
		}
		if a[0] >= 9 {
			return fmt.Errorf("player status %d is outside 0..8", a[0])
		}
		removePlayerStatus(world, role, int(a[0]))
	case 0x55, 0x56:
		if a[1] != 0 {
			role = a[1] - 1
		}
		raw, err := playerRole(world, role)
		if err != nil {
			return err
		}
		magic := raw[32:64]
		if inst.Opcode == 0x55 {
			if slices.Contains(magic, a[0]) {
				return nil
			}
			if index := slices.Index(magic, uint16(0)); index >= 0 {
				magic[index] = a[0]
			}
		} else if index := slices.Index(magic, a[0]); index >= 0 {
			magic[index] = 0
		}
		world.Roles[role] = raw
	case 0x57:
		raw, err := playerRole(world, role)
		if err != nil {
			return err
		}
		object, ok := world.Objects[a[0]]
		if !ok {
			return fmt.Errorf("magic object %d is absent from world snapshot", a[0])
		}
		if _, ok := world.MagicBaseDamage[object[0]]; !ok {
			return fmt.Errorf("magic table entry %d base damage is absent from world snapshot", object[0])
		}
		multiplier := a[1]
		if multiplier == 0 {
			multiplier = 8
		}
		world.MagicBaseDamage[object[0]] = raw[10] * multiplier
		raw[10] = 0
		world.Roles[role] = raw
	case 0x90:
		if a[2] > 4 {
			return fmt.Errorf("object script selector %d is outside normalized raw fields 2..6", a[2])
		}
		object, ok := world.Objects[a[0]]
		if !ok {
			return fmt.Errorf("object %d is absent from world snapshot", a[0])
		}
		object[2+a[2]] = a[1]
		world.Objects[a[0]] = object
	default:
		return fmt.Errorf("player opcode %04x is not supported by linear equipment interpreter", inst.Opcode)
	}
	return nil
}

// The source first assigns the sum to WORD, then interprets that wrapped value
// as SHORT for the lower bound, and finally applies the unsigned maximum.
func increasePlayerHPMP(world *WorldState, role, hp, mp uint16) bool {
	raw := world.Roles[role]
	if raw[9] == 0 {
		return false
	}
	beforeHP, beforeMP := raw[9], raw[10]
	for _, change := range []struct {
		current, maximum int
		delta            uint16
	}{{9, 7, hp}, {10, 8, mp}} {
		value := raw[change.current] + change.delta
		if int16(value) < 0 {
			value = 0
		} else if value > raw[change.maximum] {
			value = raw[change.maximum]
		}
		raw[change.current] = value
	}
	world.Roles[role] = raw
	return raw[9] != beforeHP || raw[10] != beforeMP
}

func removePlayerStatus(world *WorldState, role uint16, status int) {
	if world.PlayerStatus[role][status] <= 999 {
		world.PlayerStatus[role][status] = 0
	}
}

func cureRevivedPlayerPoison(world *WorldState, role uint16) error {
	partyIndex := slices.Index(world.PartyRoles, role)
	if partyIndex < 0 {
		return nil
	}
	if partyIndex >= 5 {
		return fmt.Errorf("poison party index %d is outside 0..4", partyIndex)
	}
	for index := range 16 {
		poison := world.PlayerPoisons[index][partyIndex]
		if poison == (WorldPoison{}) {
			continue // Both branches leave a fully empty poison slot unchanged.
		}
		object, ok := world.Objects[poison.ID]
		if !ok {
			return fmt.Errorf("poison object %d is absent from snapshot", poison.ID)
		}
		if object[0] <= 3 {
			world.PlayerPoisons[index][partyIndex] = WorldPoison{}
		}
	}
	return nil
}

func removePlayerItems(world *WorldState, inst Instruction) (*uint16, error) {
	a := inst.Operands
	quantity := int(a[1])
	if quantity == 0 {
		quantity = 1
	}
	if a[2] != 0 {
		count := 0
		if a[0] != 0 {
			_, counts, err := checkedInventory(world, false)
			if err != nil {
				return nil, err
			}
			count = counts[a[0]]
			roles, err := playerTargets(world, 0, true)
			if err != nil {
				return nil, err
			}
			for _, role := range roles {
				raw := world.Roles[role]
				for slot := range 6 {
					if raw[11+slot] == a[0] {
						count++
					}
				}
			}
		}
		if quantity > count {
			jump := a[2]
			return &jump, nil
		}
	}
	result := 0
	if a[0] != 0 {
		slots, _, err := checkedInventory(world, true)
		if err != nil {
			return nil, err
		}
		result = changeInventorySlots(slots, a[0], -quantity)
		world.InventorySlots, world.Inventory = slots, inventoryCounts(slots)
	}
	if result > 0 {
		return nil, nil
	}
	if result < 0 {
		quantity = -result
	}
	roles, err := playerTargets(world, 0, true)
	if err != nil {
		return nil, err
	}
	for _, role := range roles {
		for slot := range 6 {
			raw := world.Roles[role]
			if raw[11+slot] != a[0] {
				continue
			}
			if err := removeEquipmentEffects(world, role, slot); err != nil {
				return nil, err
			}
			raw[11+slot] = 0
			world.Roles[role] = raw
			quantity--
			if quantity == 0 {
				return nil, nil
			}
		}
	}
	return nil, nil
}
