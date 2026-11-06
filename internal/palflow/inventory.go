package palflow

import "fmt"

// InventorySlot preserves retained zero-quantity entries, slot ordering and the
// amount reserved by battle actions. Item zero terminates the searchable prefix.
type InventorySlot struct {
	Item   uint16 `json:"item"`
	Amount uint16 `json:"amount"`
	InUse  uint16 `json:"in_use"`
}

const inventoryCapacity = 256

func inventoryOpcodeName(op uint16) string {
	switch op {
	case 0x001e:
		return "change cash by signed amount, or jump if insufficient"
	case 0x001f:
		return "add or remove inventory item by signed amount"
	case 0x0058:
		return "jump if inventory amount is below signed threshold"
	}
	return ""
}

// Semantics are checked against SDLPAL d4bf69b64d335cd680555af9df90e7ec4f3006a3,
// script.c (0x001e/0x001f/0x0058) and global.c inventory lookup/add functions.
// evaluateInventory models only the state transitions of the three listed
// instructions. It does not remove equipped items (0x0020), run battle item
// reservations or reproduce inventory-menu cursor effects.
func evaluateInventory(state *State, step *TraceStep, inst Instruction) (bool, error) {
	name := inventoryOpcodeName(inst.Opcode)
	if name == "" {
		return false, nil
	}
	if state == nil || len(state.Frames) == 0 {
		return true, fmt.Errorf("inventory instruction requires a current script frame")
	}
	if step == nil {
		return true, fmt.Errorf("inventory instruction requires a trace step")
	}
	if state.World == nil {
		return true, fmt.Errorf("inventory instruction requires an explicit world snapshot")
	}
	frameIndex := len(state.Frames) - 1
	frame := state.Frames[frameIndex]
	world := CloneWorld(state.World)
	a := inst.Operands
	var nextPC uint16
	var err error
	effect := name
	switch inst.Opcode {
	case 0x001e:
		delta := int32(int16(a[0]))
		if delta < 0 && world.Cash < uint32(-delta) {
			nextPC = a[1]
			effect += fmt.Sprintf("; cash unchanged, insufficient for %d; jump to %d", -delta, nextPC)
		} else {
			nextPC, err = fallthroughPC(frame.PC)
			if err != nil {
				return true, err
			}
			// The source DWORD arithmetic is modulo 2^32, including a positive wrap.
			world.Cash += uint32(delta)
			effect += fmt.Sprintf("; signed delta %d; cash now %d (uint32 arithmetic)", delta, world.Cash)
		}
	case 0x001f:
		nextPC, err = fallthroughPC(frame.PC)
		if err != nil {
			return true, err
		}
		if a[0] == 0 {
			effect += "; item zero ignored; inventory context not inspected"
			break
		}
		slots, _, err := checkedInventory(world, true)
		if err != nil {
			return true, err
		}
		quantity := int(int16(a[1]))
		if quantity == 0 {
			quantity = 1
		}
		index := 0
		for index < inventoryCapacity && slots[index].Item != 0 && slots[index].Item != a[0] {
			index++
		}
		found := index < inventoryCapacity && slots[index].Item == a[0]
		switch {
		case quantity > 0 && index == inventoryCapacity:
			effect += "; inventory full; add failed and opcode ignores the failure"
		case quantity > 0:
			amount := quantity
			if found {
				amount += int(slots[index].Amount)
			}
			slots[index].Item, slots[index].Amount = a[0], uint16(min(amount, 99))
			effect += fmt.Sprintf("; item %d quantity now %d; slot %d retained", a[0], slots[index].Amount, index)
		case found:
			amount := max(0, int(slots[index].Amount)+quantity)
			slots[index].Amount = uint16(amount)
			effect += fmt.Sprintf("; item %d quantity now %d; zero-quantity slots remain occupied", a[0], amount)
		default:
			effect += "; item not found; remove failed and opcode ignores the failure"
		}
		world.InventorySlots = slots
		world.Inventory = inventoryCounts(slots)
	case 0x0058:
		amount := 0
		if a[0] != 0 {
			_, counts, checkErr := checkedInventory(world, false)
			if checkErr != nil {
				return true, checkErr
			}
			amount = counts[a[0]]
		}
		threshold := int(int16(a[1]))
		if amount < threshold {
			nextPC = a[2]
			effect += fmt.Sprintf("; item %d amount %d < %d; jump to %d", a[0], amount, threshold, nextPC)
		} else {
			nextPC, err = fallthroughPC(frame.PC)
			if err != nil {
				return true, err
			}
			effect += fmt.Sprintf("; item %d amount %d >= %d; continue", a[0], amount, threshold)
		}
	}
	frames := append([]Frame(nil), state.Frames...)
	frames[frameIndex].PC = nextPC
	state.World, state.Frames, step.Effect = world, frames, effect
	return true, nil
}

// checkedInventory requires ordered slots for mutation. A nonnil empty slice is
// a known empty 256-slot inventory, not a zero-capacity inventory. For the 0x58
// read-only predicate, a map-only snapshot explicitly supplies complete aggregate
// counts; no slot order or insertion capacity is inferred from that map.
func checkedInventory(world *WorldState, requireSlots bool) ([]InventorySlot, map[uint16]int, error) {
	if world.Inventory != nil {
		if len(world.Inventory) > inventoryCapacity {
			return nil, nil, fmt.Errorf("inventory count map exceeds %d entries", inventoryCapacity)
		}
		for id, amount := range world.Inventory {
			if id == 0 || amount < 0 || amount > 99 {
				return nil, nil, fmt.Errorf("inventory count map requires nonzero item IDs and amounts in 0..99")
			}
		}
	}
	if world.InventorySlots == nil {
		if requireSlots {
			return nil, nil, fmt.Errorf("ordered inventory slots are required for mutation; a count map cannot establish retained-slot capacity")
		}
		if world.Inventory == nil {
			return nil, nil, fmt.Errorf("inventory predicate requires explicit inventory slots or complete count map")
		}
		return nil, world.Inventory, nil
	}
	if len(world.InventorySlots) > inventoryCapacity {
		return nil, nil, fmt.Errorf("inventory slot count exceeds %d", inventoryCapacity)
	}
	slots := make([]InventorySlot, inventoryCapacity)
	copy(slots, world.InventorySlots)
	for index, slot := range slots {
		if slot.Amount > 99 || slot.InUse > 99 {
			return nil, nil, fmt.Errorf("inventory slot %d amounts must be in 0..99", index)
		}
	}
	counts := inventoryCounts(slots)
	if world.Inventory != nil {
		if len(world.Inventory) != len(counts) {
			return nil, nil, fmt.Errorf("inventory count map conflicts with ordered slots (including retained zero-quantity entries)")
		}
		for id, amount := range counts {
			other, found := world.Inventory[id]
			if !found || other != amount {
				return nil, nil, fmt.Errorf("inventory count map conflicts with ordered slots for item %d", id)
			}
		}
	}
	return slots, counts, nil
}

// inventoryCounts mirrors PAL's first-match search: stop at item zero and use
// the first occurrence of an item ID, preserving zero-quantity occupied entries.
func inventoryCounts(slots []InventorySlot) map[uint16]int {
	counts := make(map[uint16]int)
	for _, slot := range slots {
		if slot.Item == 0 {
			break
		}
		if _, exists := counts[slot.Item]; !exists {
			counts[slot.Item] = int(slot.Amount)
		}
	}
	return counts
}
