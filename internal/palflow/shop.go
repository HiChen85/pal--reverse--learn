package palflow

import "fmt"

type StoreSource interface {
	StoreItems(id uint16) ([]uint16, error)
}
type ShopRecord struct {
	Kind       string          `json:"kind"`
	Store      uint16          `json:"store"`
	Stock      []uint16        `json:"stock,omitempty"`
	Selections []ShopSelection `json:"selections"`
	Closed     bool            `json:"closed"`
}
type ShopSelection struct {
	Input           InputUse  `json:"input"`
	Confirmation    *InputUse `json:"confirmation,omitempty"`
	Price           uint16    `json:"price"`
	CashBefore      uint32    `json:"cash_before"`
	CashAfter       uint32    `json:"cash_after"`
	AmountBefore    int       `json:"amount_before"`
	AmountAfter     int       `json:"amount_after"`
	InventoryResult int       `json:"inventory_result"`
	Effect          string    `json:"effect"`
}

func shopOpcodeName(op uint16) string {
	if op == 0x26 {
		return "buy menu with explicit selections and confirmations"
	}
	if op == 0x27 {
		return "sell menu with explicit selections and confirmations"
	}
	return ""
}

// A selection is an item ID followed by a confirmation when the reference UI
// asks one. Zero closes the menu. The menu never advances the world; a missing
// selection, invalid item or exhausted shared budget rolls back all trades.
func (r *Runner) evaluateShop(s *State, step *TraceStep, inst Instruction, inputs []Input) (bool, error) {
	if shopOpcodeName(inst.Opcode) == "" {
		return false, nil
	}
	if r == nil || s == nil || s.World == nil || len(s.Frames) == 0 || step == nil {
		return true, fmt.Errorf("shop requires runner, frame and explicit inventory world")
	}
	i := len(s.Frames) - 1
	next, err := fallthroughPC(s.Frames[i].PC)
	if err != nil {
		return true, err
	}
	working := *s
	working.World = CloneWorld(s.World)
	record := &ShopRecord{Kind: "sell", Selections: []ShopSelection{}}
	if inst.Opcode == 0x26 {
		record.Kind, record.Store = "buy", inst.Operands[0]
		source, ok := r.source.(StoreSource)
		if !ok {
			return true, fmt.Errorf("buy menu requires imported store data")
		}
		stock, err := source.StoreItems(record.Store)
		if err != nil {
			return true, err
		}
		if len(stock) > 9 {
			return true, fmt.Errorf("store exceeds nine source item slots")
		}
		for _, id := range stock {
			if id == 0 {
				break
			}
			if _, ok := working.World.Objects[id]; !ok {
				return true, fmt.Errorf("store item %d missing", id)
			}
			record.Stock = append(record.Stock, id)
		}
	}
	for {
		remaining := r.options.MaxSteps
		for _, used := range []int{working.ExecutedSteps, working.PlayerSubsteps, working.WorldTicks, working.AutoSubsteps, working.CombatWork, working.MediaWork, 1} {
			if used < 0 || used > remaining {
				return true, fmt.Errorf("shop exceeds combined execution budget")
			}
			remaining -= used
		}
		if remaining < 1 {
			return true, fmt.Errorf("shop selection exceeds combined execution budget")
		}
		working.PlayerSubsteps++
		if record.Kind == "sell" {
			working.World, err = CompactInventory(working.World)
			if err != nil {
				return true, err
			}
		}
		use, err := consume(&working, inputs, InputShop)
		if err != nil {
			return true, err
		}
		item := uint16(use.Input.Value)
		selection := ShopSelection{Input: *use, CashBefore: working.World.Cash, CashAfter: working.World.Cash, AmountBefore: working.World.Inventory[item], AmountAfter: working.World.Inventory[item]}
		if item == 0 {
			selection.Effect = "close menu"
			record.Selections = append(record.Selections, selection)
			record.Closed = true
			break
		}
		object, exists := working.World.Objects[item]
		if !exists {
			return true, fmt.Errorf("shop selection object %d missing", item)
		}
		selection.Price = object[1]
		if record.Kind == "buy" {
			available := false
			for _, id := range record.Stock {
				available = available || id == item
			}
			if !available {
				return true, fmt.Errorf("item %d is absent from store %d", item, record.Store)
			}
			if uint32(selection.Price) > working.World.Cash {
				selection.Effect = "insufficient cash; confirmation not requested"
				record.Selections = append(record.Selections, selection)
				continue
			}
		} else {
			reserved := 0
			for _, slot := range working.World.InventorySlots {
				if slot.Item == item {
					reserved = int(int16(slot.InUse))
					break
				}
			}
			if object[6]&32 == 0 || selection.AmountBefore <= reserved {
				return true, fmt.Errorf("item %d is not a selectable sellable inventory stack", item)
			}
			selection.Price /= 2
		}
		selection.Confirmation, err = consume(&working, inputs, InputConfirm)
		if err != nil {
			return true, err
		}
		if selection.Confirmation.Input.Value == 0 {
			selection.Effect = "confirmation declined"
			record.Selections = append(record.Selections, selection)
			continue
		}
		quantity := 1
		if record.Kind == "sell" {
			quantity = -1
		}
		working.World, selection.InventoryResult, err = ChangeInventory(working.World, item, quantity)
		if err != nil {
			return true, err
		}
		if record.Kind == "buy" {
			working.World.Cash -= uint32(selection.Price)
			selection.Effect = "purchase: charge price even when add fails or stack is capped"
		} else if selection.InventoryResult != 0 {
			working.World.Cash += uint32(selection.Price)
			selection.Effect = "sale: half price with uint32 cash arithmetic"
		}
		selection.CashAfter, selection.AmountAfter = working.World.Cash, working.World.Inventory[item]
		record.Selections = append(record.Selections, selection)
	}
	working.Frames = append([]Frame(nil), s.Frames...)
	working.Frames[i].PC = next
	*s = working
	step.Shop = record
	step.Effect = shopOpcodeName(inst.Opcode) + "; explicit menu choices, no elapsed game frames"
	return true, nil
}
