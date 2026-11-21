package palgame

import (
	"errors"
	"fmt"
	"maps"
	"slices"

	"github.com/HiChen85/pal--reverse--learn/internal/paldata"
	"github.com/HiChen85/pal--reverse--learn/internal/palflow"
)

// EquipItem runs the selected inventory item's OnEquip callback for one
// current party role. The callback performs the swap and applies any effects.
func (s *Session) EquipItem(item, role uint16, inputs []palflow.Input) (palflow.Report, error) {
	if s == nil {
		return palflow.Report{}, errors.New("equipping an item requires an active session")
	}
	s.lastInvocation = nil
	if !s.current.equipmentMenuOpen() {
		return palflow.Report{}, errors.New("equipping an item requires an entered overworld scene")
	}
	w := s.current.World
	object, exists := w.Objects[item]
	if item == 0 || !exists || object[6]&2 == 0 {
		return palflow.Report{}, fmt.Errorf("item %d is not equippable", item)
	}
	if !slices.Contains(w.PartyRoles, role) || !equipmentRoleAllowed(w, object, role) {
		return palflow.Report{}, fmt.Errorf("item %d requires an eligible current party role, got %d", item, role)
	}
	prepared, err := palflow.CompactInventory(w)
	if err != nil {
		return palflow.Report{}, err
	}
	if !equipmentStock(prepared.InventorySlots)[item] {
		return palflow.Report{}, fmt.Errorf("item %d has no unreserved inventory stock", item)
	}
	prepared.LastUnequippedItem = item
	req := palflow.Request{Entry: object[3], EventID: role, LastEventID: s.current.LastEventID, IdleFrames: maps.Clone(s.current.IdleFrames), World: prepared, Inputs: append([]palflow.Input(nil), inputs...)}
	options := s.runnerOptions()
	runner, err := palflow.New(s.source(), options)
	if err != nil {
		return palflow.Report{}, err
	}
	s.lastInvocation = &Invocation{Request: req, Options: options}
	report, err := runner.Trace(req)
	if err != nil {
		return report, err
	}
	if !report.Completed || report.State.World == nil || len(report.UnusedInputs) != 0 {
		return report, errors.New("equipment script incomplete or inputs unused; action was not committed")
	}
	next := palflow.CloneWorld(report.State.World)
	object, exists = next.Objects[item]
	if !exists {
		return report, errors.New("equipment script removed its object record")
	}
	object[3] = report.State.ReturnedEntry
	next.Objects[item] = object
	s.current = SessionSnapshot{ScriptSuccess: report.State.ScriptSuccess, World: next, IdleFrames: maps.Clone(report.State.IdleFrames), LastEventID: report.State.LastEventID, NeedsSceneEnter: next.SceneChanged, Actions: s.current.Actions + 1}
	return report, nil
}

// EquipmentChoices returns inventory item IDs and their eligible current party
// roles, in party order. Choices are available only in an entered overworld.
func (s SessionSnapshot) EquipmentChoices() map[uint16][]uint16 {
	if !s.equipmentMenuOpen() {
		return nil
	}
	w := s.World
	choices := make(map[uint16][]uint16)
	for item, available := range equipmentStock(w.InventorySlots) {
		object := w.Objects[item]
		if !available || object[6]&2 == 0 {
			continue
		}
		for _, role := range w.PartyRoles {
			if equipmentRoleAllowed(w, object, role) {
				choices[item] = append(choices[item], role)
			}
		}
	}
	return choices
}

func (s SessionSnapshot) equipmentMenuOpen() bool {
	w := s.World
	return w != nil && !s.NeedsSceneEnter && !w.SceneLoadPending && !w.SceneChanged && !w.InBattle
}

func equipmentRoleAllowed(w *palflow.WorldState, object [7]uint16, role uint16) bool {
	_, exists := w.Roles[role]
	return exists && role < paldata.RoleCount && object[6]&(1<<(6+role)) != 0
}

func equipmentStock(slots []palflow.InventorySlot) map[uint16]bool {
	stock := make(map[uint16]bool)
	for _, slot := range slots {
		// The menu compacts away zero amounts before looking up an item.
		if slot.Amount == 0 {
			continue
		}
		if slot.Item == 0 {
			break
		}
		if _, seen := stock[slot.Item]; !seen {
			stock[slot.Item] = slot.Amount > slot.InUse
		}
	}
	return stock
}
