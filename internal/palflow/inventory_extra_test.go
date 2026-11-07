package palflow

import "testing"

func TestHalveCashUsesUnsignedFloorAndDetachedWorld(t *testing.T) {
	for _, amount := range []uint32{0, 1, 9, 0xffffffff} {
		world := &WorldState{Cash: amount}
		state := inventoryState(world)
		inventoryRun(t, &state, 0x8f, 0, 0, 0)
		if state.World.Cash != amount/2 || state.Frames[0].PC != 11 || world.Cash != amount {
			t.Fatal("halving cash must floor unsigned division and leave caller snapshot unchanged")
		}
	}
}

func TestInventoryMutationHelperReturnsPreciseRemovalShortage(t *testing.T) {
	slots := make([]InventorySlot, inventoryCapacity)
	slots[0] = InventorySlot{Item: 10, Amount: 2, InUse: 1}
	if result := changeInventorySlots(slots, 10, -5); result != -3 || slots[0] != (InventorySlot{Item: 10, Amount: 0, InUse: 1}) {
		t.Fatal("shortage must retain the zero-quantity item and reservation")
	}
	if result := changeInventorySlots(slots, 20, -5); result != 0 {
		t.Fatal("missing item is a no-op rather than a partial shortage")
	}
	if result := changeInventorySlots(slots, 10, 0); result != 1 || slots[0].Amount != 1 {
		t.Fatal("zero quantity must add one")
	}
}
