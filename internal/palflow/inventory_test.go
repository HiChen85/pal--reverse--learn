package palflow

import (
	"math"
	"reflect"
	"testing"
)

func inventoryState(world *WorldState) State {
	return State{World: world, Frames: []Frame{{PC: 10, Entry: 10, EventID: 1}}}
}
func inventoryRun(t *testing.T, state *State, op uint16, a, b, c uint16) TraceStep {
	t.Helper()
	step := TraceStep{}
	handled, err := evaluateInventory(state, &step, Instruction{Opcode: op, Operands: [3]uint16{a, b, c}})
	if !handled || err != nil {
		t.Fatalf("op %04x: handled=%v err=%v", op, handled, err)
	}
	return step
}
func TestCashSignedChangesAndInsufficientJump(t *testing.T) {
	cases := []struct {
		name     string
		cash     uint32
		amount   uint16
		pc       uint16
		wantCash uint32
		wantPC   uint16
	}{
		{"gain", 100, 20, 10, 120, 11},
		{"spend", 100, 65516, 10, 80, 11},
		{"exact spend", 20, 65516, 10, 0, 11},
		{"insufficient", 19, 65516, 10, 19, 7},
		{"signed min spend", 32768, 32768, 10, 0, 11},
		{"signed min insufficient", 32767, 32768, 10, 32767, 7},
		{"uint32 wrap", math.MaxUint32, 1, 10, 0, 11},
		{"jump does not need fallthrough", 0, 65535, 65535, 0, 7},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			source := &WorldState{Cash: tc.cash}
			state := inventoryState(source)
			state.Frames[0].PC = tc.pc
			inventoryRun(t, &state, 0x1e, tc.amount, 7, 0)
			if state.World.Cash != tc.wantCash || state.Frames[0].PC != tc.wantPC {
				t.Fatalf("cash/PC = %d/%d", state.World.Cash, state.Frames[0].PC)
			}
			if source.Cash != tc.cash || state.World == source {
				t.Fatal("source world mutated or aliased")
			}
		})
	}
}
func TestInventorySignedAddRemoveAndRetainedSlots(t *testing.T) {
	source := &WorldState{InventorySlots: []InventorySlot{}, Inventory: map[uint16]int{}}
	state := inventoryState(source)
	inventoryRun(t, &state, 0x1f, 2, 0, 0)
	if state.World.Inventory[2] != 1 || len(state.World.InventorySlots) != 256 {
		t.Fatal("zero quantity did not add one into known-empty full capacity")
	}
	inventoryRun(t, &state, 0x1f, 2, 32767, 0)
	if state.World.Inventory[2] != 99 {
		t.Fatal("quantity did not cap at 99")
	}
	inventoryRun(t, &state, 0x1f, 2, 65534, 0)
	if state.World.Inventory[2] != 97 {
		t.Fatal("signed removal failed to subtract two")
	}
	state.World.InventorySlots[0].InUse = 7
	inventoryRun(t, &state, 0x1f, 2, 32768, 0)
	slot := state.World.InventorySlots[0]
	if slot.Item != 2 || slot.Amount != 0 || slot.InUse != 7 {
		t.Fatalf("remove must retain item/inuse: %+v", slot)
	}
	inventoryRun(t, &state, 0x1f, 3, 1, 0)
	if state.World.InventorySlots[1].Item != 3 || state.World.Inventory[2] != 0 {
		t.Fatal("zero-quantity slot was incorrectly reused")
	}
	if len(source.InventorySlots) != 0 || len(source.Inventory) != 0 {
		t.Fatal("caller snapshot mutated")
	}
}
func TestInventoryFullAddAndMissingRemoveIgnored(t *testing.T) {
	slots := make([]InventorySlot, 256)
	for i := range slots {
		slots[i] = InventorySlot{Item: uint16(i + 1), Amount: 1}
	}
	state := inventoryState(&WorldState{InventorySlots: slots})
	inventoryRun(t, &state, 0x1f, 500, 1, 0)
	if state.Frames[0].PC != 11 || len(state.World.Inventory) != 256 {
		t.Fatal("full add must preserve inventory and advance")
	}
	inventoryRun(t, &state, 0x1f, 500, 65535, 0)
	if _, exists := state.World.Inventory[500]; exists {
		t.Fatal("missing remove created an item")
	}
	inventoryRun(t, &state, 0x1f, 1, 1, 0)
	if state.World.Inventory[1] != 2 {
		t.Fatal("full inventory must still add to an existing slot")
	}
}
func TestInventorySearchFirstMatchAndFirstEmpty(t *testing.T) {
	state := inventoryState(&WorldState{InventorySlots: []InventorySlot{{Item: 1, Amount: 2}, {Item: 1, Amount: 7}, {}, {Item: 9, Amount: 4}}, Inventory: map[uint16]int{1: 2}})
	inventoryRun(t, &state, 0x1f, 1, 1, 0)
	if state.World.InventorySlots[0].Amount != 3 || state.World.InventorySlots[1].Amount != 7 {
		t.Fatal("duplicate slot search did not use first match")
	}
	if _, exists := state.World.Inventory[9]; exists {
		t.Fatal("item after empty slot visible too early")
	}
	inventoryRun(t, &state, 0x1f, 3, 1, 0)
	if state.World.InventorySlots[2].Item != 3 || state.World.Inventory[9] != 4 {
		t.Fatal("insertion at first empty must expose known subsequent slots")
	}
}
func TestInventoryPredicateSignedThresholdAndSources(t *testing.T) {
	cases := []struct {
		name            string
		world           *WorldState
		item, threshold uint16
		wantPC          uint16
	}{
		{"aggregate less", &WorldState{Inventory: map[uint16]int{2: 7}}, 2, 8, 9},
		{"aggregate equal", &WorldState{Inventory: map[uint16]int{2: 7}}, 2, 7, 11},
		{"negative signed threshold", &WorldState{Inventory: map[uint16]int{2: 7}}, 2, 65535, 11},
		{"missing item", &WorldState{Inventory: map[uint16]int{}}, 2, 1, 9},
		{"knownempty slots", &WorldState{InventorySlots: []InventorySlot{}}, 2, 1, 9},
		{"zero item known without inventory", &WorldState{}, 0, 1, 9},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			state := inventoryState(tc.world)
			inventoryRun(t, &state, 0x58, tc.item, tc.threshold, 9)
			if state.Frames[0].PC != tc.wantPC {
				t.Fatalf("PC %d", state.Frames[0].PC)
			}
		})
	}
}
func TestInventoryZeroItemIgnoredWithoutInventory(t *testing.T) {
	state := inventoryState(&WorldState{})
	step := inventoryRun(t, &state, 0x1f, 0, 32768, 0)
	if state.Frames[0].PC != 11 || state.World.InventorySlots != nil || state.World.Inventory != nil || step.Effect == "" {
		t.Fatal("item zero should be explicit no-op")
	}
}
func TestInventoryErrorsRollbackAllState(t *testing.T) {
	cases := []struct {
		name  string
		world *WorldState
		op    uint16
		pc    uint16
	}{
		{"missing world", nil, 0x1e, 10},
		{"unknown slots for mutation", &WorldState{Inventory: map[uint16]int{1: 2}}, 0x1f, 10},
		{"unknown inventory for predicate", &WorldState{}, 0x58, 10},
		{"negative count", &WorldState{Inventory: map[uint16]int{1: -1}}, 0x58, 10},
		{"overflow count", &WorldState{Inventory: map[uint16]int{1: 100}}, 0x58, 10},
		{"slot count overflow", &WorldState{InventorySlots: make([]InventorySlot, 257)}, 0x1f, 10},
		{"slot amount overflow", &WorldState{InventorySlots: []InventorySlot{{Item: 1, Amount: 100}}}, 0x1f, 10},
		{"slot inuse overflow", &WorldState{InventorySlots: []InventorySlot{{Item: 1, Amount: 2, InUse: 100}}}, 0x1f, 10},
		{"conflicting mirror", &WorldState{InventorySlots: []InventorySlot{{Item: 1, Amount: 2}}, Inventory: map[uint16]int{1: 3}}, 0x1f, 10},
		{"mirror omits tombstone", &WorldState{InventorySlots: []InventorySlot{{Item: 1, Amount: 0}}, Inventory: map[uint16]int{}}, 0x58, 10},
		{"cash fallthrough overflow", &WorldState{Cash: 100}, 0x1e, 65535},
		{"item fallthrough overflow", &WorldState{InventorySlots: []InventorySlot{}}, 0x1f, 65535},
		{"predicate fallthrough overflow", &WorldState{Inventory: map[uint16]int{1: 9}}, 0x58, 65535},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			state := inventoryState(tc.world)
			state.Frames[0].PC = tc.pc
			originalWorld := state.World
			originalFrames := state.Frames
			before := state
			before.World = CloneWorld(state.World)
			before.Frames = append([]Frame(nil), state.Frames...)
			step := TraceStep{Effect: "unchanged", PC: 77}
			stepBefore := step
			handled, err := evaluateInventory(&state, &step, Instruction{Opcode: tc.op, Operands: [3]uint16{1, 1, 9}})
			if !handled || err == nil {
				t.Fatalf("expected blocking error, got %v/%v", handled, err)
			}
			if !reflect.DeepEqual(state, before) || state.World != originalWorld || &state.Frames[0] != &originalFrames[0] || !reflect.DeepEqual(step, stepBefore) {
				t.Fatal("failed instruction mutated input state or trace step")
			}
		})
	}
}
func TestInventoryUnsupportedAndContextErrors(t *testing.T) {
	if inventoryOpcodeName(0x20) != "" {
		t.Fatal("equipment fallback remove must remain unsupported")
	}
	if handled, err := evaluateInventory(nil, nil, Instruction{Opcode: 0x20}); handled || err != nil {
		t.Fatal("unsupported operation unexpectedly handled")
	}
	state := inventoryState(&WorldState{})
	if handled, err := evaluateInventory(&state, nil, Instruction{Opcode: 0x1e}); !handled || err == nil {
		t.Fatal("nil step accepted")
	}
	if handled, err := evaluateInventory(&State{}, &TraceStep{}, Instruction{Opcode: 0x1e}); !handled || err == nil {
		t.Fatal("missing frame accepted")
	}
}
