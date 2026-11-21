package palgame

import (
	"reflect"
	"testing"

	"github.com/HiChen85/pal--reverse--learn/internal/paldata"
	"github.com/HiChen85/pal--reverse--learn/internal/palflow"
)

func itemSession(t *testing.T) *Session {
	t.Helper()
	g, r := fixture()
	g.Objects[1] = [7]uint16{2: 1, 6: 9}
	g.Scripts = []paldata.Instruction{{}, {Opcode: 0x1b, Operands: [3]uint16{0, 10}}, {Opcode: 1}, {}}
	s, err := NewSession(g, r, SessionOptions{AssumeSceneEntered: true})
	if err != nil {
		t.Fatal(err)
	}
	w := s.current.World
	raw := w.Roles[0]
	raw[7], raw[9] = 100, 50
	w.Roles[0] = raw
	w.Inventory[1] = 2
	w.InventorySlots[0] = palflow.InventorySlot{Item: 1, Amount: 2}
	return s
}

func TestUseItemCommitsEffectConsumptionAndReturnedPointer(t *testing.T) {
	s := itemSession(t)
	before := s.Snapshot()
	r, err := s.UseItem(1, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	after := s.Snapshot()
	if !r.Completed || !after.ScriptSuccess || after.World.Roles[0][9] != 60 || after.World.Inventory[1] != 1 || after.World.InventorySlots[0].Amount != 1 || after.World.Objects[1][2] != 3 || after.Actions != 1 || after.World.FrameNum != before.World.FrameNum {
		t.Fatal("item action lost HP, ordered inventory, returned entry or menu frame semantics")
	}
	if s.LastInvocation().Request.World.Inventory[1] != 2 || r.State.World.Inventory[1] != 2 {
		t.Fatal("script evidence aliases the post-menu consumption")
	}
	// A full-HP callback reports failure; a consuming item then stays available.
	s = itemSession(t)
	raw := s.current.World.Roles[0]
	raw[9] = 100
	s.current.World.Roles[0] = raw
	r, err = s.UseItem(1, 0, nil)
	if err != nil || r.State.ScriptSuccess || s.current.World.Inventory[1] != 2 || s.current.World.Objects[1][2] != 3 {
		t.Fatal("failed item callback consumed item or failed to persist entry")
	}
}

func TestUseItemFailureRollsBackAndValidatesSelection(t *testing.T) {
	for _, kind := range []string{"unsupported", "unused", "missing", "role", "scene", "flag"} {
		t.Run(kind, func(t *testing.T) {
			s := itemSession(t)
			role := uint16(0)
			var inputs []palflow.Input
			switch kind {
			case "unsupported":
				s.game.Scripts[2].Opcode = 0x7fff
			case "unused":
				inputs = []palflow.Input{{Kind: palflow.InputKey, Value: 1}}
			case "missing":
				s.current.World.Inventory[1] = 0
				s.current.World.InventorySlots[0].Amount = 0
			case "role":
				role = 4
			case "scene":
				s.current.NeedsSceneEnter = true
			case "flag":
				object := s.current.World.Objects[1]
				object[6] = 8
				s.current.World.Objects[1] = object
			}
			before := s.Snapshot()
			if _, err := s.UseItem(1, role, inputs); err == nil || !reflect.DeepEqual(before, s.Snapshot()) {
				t.Fatal("invalid or incomplete item action committed")
			}
		})
	}
}

func TestUseItemEquippedAndApplyToAll(t *testing.T) {
	s := itemSession(t)
	w := s.current.World
	w.Inventory[1] = 0
	w.InventorySlots[0].Amount = 0
	raw := w.Roles[0]
	raw[11] = 1
	w.Roles[0] = raw
	if _, err := s.UseItem(1, 0, nil); err != nil || s.current.World.Roles[0][11] != 1 {
		t.Fatal("usable equipment was removed or unavailable")
	}
	s = itemSession(t)
	obj := s.current.World.Objects[1]
	obj[6] = 25
	s.current.World.Objects[1] = obj
	s.game.Scripts[1].Operands[0] = 1
	if _, err := s.UseItem(1, 0, nil); err == nil {
		t.Fatal("ambiguous all-party target accepted")
	}
	if _, err := s.UseItem(1, 0xffff, nil); err != nil || s.current.World.Roles[0][9] != 60 {
		t.Fatal("all-party callback failed", err)
	}
}
