package palgame

import (
	"reflect"
	"testing"

	"github.com/HiChen85/pal--reverse--learn/internal/paldata"
	"github.com/HiChen85/pal--reverse--learn/internal/palflow"
)

const (
	testEquipItem = 70
	testOldEquip  = 71
	testEquipRole = 4
)

func equipmentSession(t *testing.T) *Session {
	t.Helper()
	g, runtime := fixture()
	g.Objects = make([][7]uint16, testOldEquip+1)
	g.Objects[testEquipItem] = [7]uint16{3: 1, 6: 2 | 1<<(6+testEquipRole)}
	g.Scripts = []paldata.Instruction{{},
		{Opcode: 0x18, Operands: [3]uint16{0xe, testEquipItem}},
		{Opcode: 0x1a, Operands: [3]uint16{17, 23}},
		{Opcode: 1}, {},
	}
	s, err := NewSession(g, runtime, SessionOptions{AssumeSceneEntered: true})
	if err != nil {
		t.Fatal(err)
	}
	w := s.current.World
	w.PartyRoles = []uint16{0, testEquipRole}
	w.Inventory = map[uint16]int{65: 0, testEquipItem: 1}
	w.InventorySlots[0] = palflow.InventorySlot{Item: 65}
	w.InventorySlots[1] = palflow.InventorySlot{Item: testEquipItem, Amount: 1}
	raw := w.Roles[testEquipRole]
	raw[14] = testOldEquip
	w.Roles[testEquipRole] = raw
	w.EquipmentEffects[3][18][testEquipRole] = 99
	return s
}

func TestEquipItemCommitsScriptSwapEffectsAndReturnedEntry(t *testing.T) {
	s := equipmentSession(t)
	before := s.Snapshot()
	if got := before.EquipmentChoices(); !reflect.DeepEqual(got, map[uint16][]uint16{testEquipItem: {testEquipRole}}) {
		t.Fatalf("eligible equipment choices = %v", got)
	}
	report, err := s.EquipItem(testEquipItem, testEquipRole, nil)
	if err != nil || !report.Completed {
		t.Fatal("equipment callback failed", err)
	}
	after := s.Snapshot()
	w := after.World
	if after.Actions != 1 || w.FrameNum != before.World.FrameNum || after.LastEventID != testEquipRole || w.Roles[testEquipRole][14] != testEquipItem || w.Roles[0][14] != 0 {
		t.Fatal("swap changed the wrong role, context, or frame")
	}
	if w.EquipmentEffects[3][17][testEquipRole] != 23 || w.EquipmentEffects[3][18][testEquipRole] != 0 || w.LastUnequippedItem != testOldEquip || w.Objects[testEquipItem][3] != 4 {
		t.Fatal("swap lost equipment effects, previous item, or returned entry")
	}
	if w.Inventory[testEquipItem] != 0 || w.Inventory[testOldEquip] != 1 || w.InventorySlots[0] != (palflow.InventorySlot{Item: testOldEquip, Amount: 1}) {
		t.Fatal("swap and menu compaction did not commit together")
	}
	invocation := s.LastInvocation()
	if invocation == nil || invocation.Request.Entry != 1 || invocation.Request.EventID != testEquipRole || invocation.Request.World.LastUnequippedItem != testEquipItem || invocation.Request.World.InventorySlots[0].Item != testEquipItem || report.State.World.Objects[testEquipItem][3] != 1 {
		t.Fatal("prepared invocation and callback evidence must preserve their own state")
	}
	if _, err := s.EquipItem(testEquipItem, testEquipRole, nil); err == nil || s.LastInvocation() != nil || !reflect.DeepEqual(after, s.Snapshot()) {
		t.Fatal("equipped copy was selectable without stock or retained a stale invocation")
	}
}

func TestEquipItemMenuValidationAndAtomicRollback(t *testing.T) {
	for _, kind := range []string{"pending entry", "pending load", "scene change", "battle", "incompatible role", "absent role", "all roles", "flag", "reserved", "missing slots", "unsupported", "unused"} {
		t.Run(kind, func(t *testing.T) {
			s := equipmentSession(t)
			w := s.current.World
			role := uint16(testEquipRole)
			var inputs []palflow.Input
			switch kind {
			case "pending entry":
				s.current.NeedsSceneEnter = true
			case "pending load":
				w.SceneLoadPending = true
			case "scene change":
				w.SceneChanged = true
			case "battle":
				w.InBattle = true
			case "incompatible role":
				role = 0
			case "absent role":
				w.PartyRoles = []uint16{0}
			case "all roles":
				role = 0xffff
			case "flag":
				object := w.Objects[testEquipItem]
				object[6] ^= 2
				w.Objects[testEquipItem] = object
			case "reserved":
				w.InventorySlots[1].InUse = 1
			case "missing slots":
				w.InventorySlots = nil
			case "unsupported":
				s.game.Scripts[3].Opcode = 0x7fff
			case "unused":
				inputs = []palflow.Input{{Kind: palflow.InputKey, Value: 1}}
			}
			before := s.Snapshot()
			report, err := s.EquipItem(testEquipItem, role, inputs)
			if err == nil || !reflect.DeepEqual(before, s.Snapshot()) {
				t.Fatal("invalid or incomplete equipment action committed")
			}
			invoked := kind == "unsupported" || kind == "unused"
			if invoked != (s.LastInvocation() != nil) {
				t.Fatal("incorrect invocation evidence")
			}
			if invoked && (report.State.World == nil || report.State.World.Roles[testEquipRole][14] != testEquipItem) {
				t.Fatal("rejected action lost proposed swap evidence")
			}
			if !invoked {
				for _, choice := range before.EquipmentChoices()[testEquipItem] {
					if choice == role {
						t.Fatal("menu exposed an invalid selection")
					}
				}
			}
		})
	}
}

func TestEquipItemCommitsCompletedScriptFailureAndLeavesStockToScript(t *testing.T) {
	s := equipmentSession(t)
	// A healing instruction on a role with zero health finishes with a false
	// script result. No swap opcode runs in this callback.
	s.game.Scripts[1] = paldata.Instruction{Opcode: 0x1b, Operands: [3]uint16{0, 5}}
	s.game.Scripts[2] = paldata.Instruction{Opcode: 1}
	report, err := s.EquipItem(testEquipItem, testEquipRole, nil)
	after := s.Snapshot()
	if err != nil || !report.Completed || after.Actions != 1 || after.ScriptSuccess || after.World.Objects[testEquipItem][3] != 3 || after.World.LastUnequippedItem != testEquipItem {
		t.Fatal("completed callback did not commit its script result and entry", err)
	}
	if after.World.Inventory[testEquipItem] != 1 || after.World.Roles[testEquipRole][14] != testOldEquip {
		t.Fatal("menu consumed or moved the selected item without a swap opcode")
	}
}
