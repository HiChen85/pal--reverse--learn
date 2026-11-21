package palflow

import (
	"reflect"
	"testing"
)

type shopSource struct {
	autoSource
	stock []uint16
}

func (s shopSource) StoreItems(uint16) ([]uint16, error) {
	return append([]uint16(nil), s.stock...), nil
}
func shopState() State {
	return State{World: &WorldState{Cash: 100, InventorySlots: make([]InventorySlot, 256), Inventory: map[uint16]int{}, Objects: map[uint16][7]uint16{1: {1: 7, 6: 32}, 2: {1: 200, 6: 32}}}, Frames: []Frame{{PC: 1, EventID: 3}}, ScriptSuccess: true}
}
func choice(id int) Input     { return Input{Kind: InputShop, Value: id} }
func confirm(value int) Input { return Input{Kind: InputConfirm, Value: value} }

func TestBuyMenuAffordabilityConfirmationAndNoWorldTicks(t *testing.T) {
	s := shopState()
	r, _ := New(shopSource{stock: []uint16{1, 2}}, Options{MaxSteps: 20})
	step := TraceStep{}
	inputs := []Input{choice(2), choice(1), confirm(0), choice(1), confirm(1), choice(0)}
	if _, err := r.evaluateShop(&s, &step, ins(0x26, 0), inputs); err != nil {
		t.Fatal(err)
	}
	if s.InputIndex != len(inputs) || s.World.Cash != 93 || s.World.Inventory[1] != 1 || s.PlayerSubsteps != 4 || s.World.FrameNum != 0 || !s.ScriptSuccess || s.Frames[0].PC != 2 || !step.Shop.Closed || len(step.Shop.Selections) != 4 || step.Shop.Selections[0].Confirmation != nil {
		t.Fatal("buy menu consumed incorrect choices/confirmations or time")
	}
}

func TestBuyMenuRetainsSourceFullAndCappedInventoryCharges(t *testing.T) {
	for _, full := range []bool{false, true} {
		s := shopState()
		if full {
			for i := range s.World.InventorySlots {
				id := uint16(i + 3)
				s.World.InventorySlots[i] = InventorySlot{Item: id, Amount: 1}
				s.World.Inventory[id] = 1
			}
		} else {
			s.World.InventorySlots[0] = InventorySlot{Item: 1, Amount: 99}
			s.World.Inventory[1] = 99
		}
		r, _ := New(shopSource{stock: []uint16{1}}, Options{})
		step := TraceStep{}
		if _, err := r.evaluateShop(&s, &step, ins(0x26), []Input{choice(1), confirm(1), choice(0)}); err != nil {
			t.Fatal(err)
		}
		want := 99
		if full {
			want = 0
		}
		if s.World.Cash != 93 || s.World.Inventory[1] != want {
			t.Fatal("source full/capped purchase semantics changed")
		}
	}
}

func TestSellMenuCompactsRetainedZerosPreservesReservationsAndRoundsHalfPrice(t *testing.T) {
	s := shopState()
	s.World.InventorySlots[0] = InventorySlot{Item: 2}
	s.World.InventorySlots[1] = InventorySlot{Item: 1, Amount: 2, InUse: 1}
	s.World.Inventory[1] = 2
	s.World.Inventory[2] = 0
	r, _ := New(shopSource{}, Options{})
	step := TraceStep{}
	if _, err := r.evaluateShop(&s, &step, ins(0x27), []Input{choice(1), confirm(1), choice(0)}); err != nil {
		t.Fatal(err)
	}
	if s.World.Cash != 103 || s.World.Inventory[1] != 1 || s.World.InventorySlots[0] != (InventorySlot{Item: 1, Amount: 1, InUse: 1}) || s.World.InventorySlots[1] != (InventorySlot{}) {
		t.Fatal("sell menu compaction, half-price or reservation mismatch")
	}
	if _, ok := s.World.Inventory[2]; ok {
		t.Fatal("zero stack survived menu compaction")
	}
}

func TestSellMenuCashWrapAndFinalCompaction(t *testing.T) {
	s := shopState()
	s.World.Cash = 0xffffffff
	s.World.Inventory[1] = 1
	s.World.InventorySlots[0] = InventorySlot{Item: 1, Amount: 1}
	r, _ := New(shopSource{}, Options{})
	step := TraceStep{}
	if _, err := r.evaluateShop(&s, &step, ins(0x27), []Input{choice(1), confirm(1), choice(0)}); err != nil {
		t.Fatal(err)
	}
	if s.World.Cash != 2 || s.World.InventorySlots[0] != (InventorySlot{}) || len(s.World.Inventory) != 0 {
		t.Fatal("sale DWORD wrap or next-menu compaction mismatch")
	}
}

func TestSellMenuCannotSelectFullyReservedStack(t *testing.T) {
	s := shopState()
	s.World.Inventory[1] = 1
	s.World.InventorySlots[0] = InventorySlot{Item: 1, Amount: 1, InUse: 1}
	before := detachPartyState(s)
	r, _ := New(shopSource{}, Options{})
	if _, err := r.evaluateShop(&s, &TraceStep{}, ins(0x27), []Input{choice(1), confirm(1), choice(0)}); err == nil || !reflect.DeepEqual(s, before) {
		t.Fatal("reserved inventory was sold", err)
	}
}

func TestShopLateFailuresRollbackEarlierTradesAndInputCursor(t *testing.T) {
	for _, tc := range []struct {
		name   string
		inputs []Input
		budget int
		op     uint16
	}{{"missing close", []Input{choice(1), confirm(1)}, 20, 0x26}, {"wrong stock", []Input{choice(1), confirm(1), choice(3)}, 20, 0x26}, {"budget", []Input{choice(1), confirm(1), choice(0)}, 2, 0x26}, {"unsellable", []Input{choice(1), confirm(1), choice(2)}, 20, 0x27}, {"missing confirm", []Input{choice(1)}, 20, 0x26}} {
		t.Run(tc.name, func(t *testing.T) {
			s := shopState()
			if tc.op == 0x27 {
				s.World.Inventory[1] = 2
				s.World.InventorySlots[0] = InventorySlot{Item: 1, Amount: 2}
			}
			before := detachPartyState(s)
			r, _ := New(shopSource{stock: []uint16{1}}, Options{MaxSteps: tc.budget})
			step := TraceStep{Effect: "preserved"}
			if _, err := r.evaluateShop(&s, &step, ins(tc.op), tc.inputs); err == nil || !reflect.DeepEqual(s, before) || step.Effect != "preserved" {
				t.Fatal("late menu failure committed", err)
			}
		})
	}
}
