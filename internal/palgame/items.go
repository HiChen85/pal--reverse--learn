package palgame

import (
	"errors"
	"fmt"
	"maps"

	"github.com/HiChen85/pal--reverse--learn/internal/palflow"
)

// UseItem executes one selection in PAL_GameUseItem. target is a role ID,
// rather than a party index; apply-to-all items require 0xffff. Opening the
// menu does not advance world frames. Scripts and consumption commit together.
func (s *Session) UseItem(item, target uint16, inputs []palflow.Input) (palflow.Report, error) {
	if s == nil {
		return palflow.Report{}, errors.New("item use requires an active session")
	}
	s.lastInvocation = nil
	if s.current.World == nil || s.current.NeedsSceneEnter || s.current.World.SceneLoadPending || s.current.World.SceneChanged || s.current.World.InBattle {
		return palflow.Report{}, errors.New("item use requires an entered overworld scene")
	}
	w := s.current.World
	object, exists := w.Objects[item]
	if item == 0 || !exists || object[6]&1 == 0 {
		return palflow.Report{}, fmt.Errorf("item %d is not usable", item)
	}
	available := w.Inventory[item] > 0
	for _, role := range w.PartyRoles {
		raw := w.Roles[role]
		for _, equipped := range raw[11:17] {
			available = available || equipped == item
		}
	}
	if !available {
		return palflow.Report{}, errors.New("usable item is absent from inventory and current party equipment")
	}
	if object[6]&16 != 0 {
		if target != 0xffff {
			return palflow.Report{}, errors.New("apply-to-all item requires target 65535")
		}
	} else {
		member := false
		for _, role := range w.PartyRoles {
			member = member || role == target
		}
		if !member {
			return palflow.Report{}, errors.New("item target must be a current party role")
		}
	}
	req := palflow.Request{Entry: object[2], EventID: target, LastEventID: s.current.LastEventID, IdleFrames: maps.Clone(s.current.IdleFrames), World: palflow.CloneWorld(w), Inputs: append([]palflow.Input(nil), inputs...)}
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
		return report, errors.New("item script incomplete or inputs unused; action was not committed")
	}
	next := palflow.CloneWorld(report.State.World)
	object, exists = next.Objects[item]
	if !exists {
		return report, errors.New("item script removed its object record")
	}
	object[2] = report.State.ReturnedEntry
	next.Objects[item] = object
	// Read flags after the callback, matching the mutable object table in C.
	if object[6]&8 != 0 && report.State.ScriptSuccess {
		next, _, err = palflow.ChangeInventory(next, item, -1)
		if err != nil {
			return report, err
		}
	}
	s.current = SessionSnapshot{ScriptSuccess: report.State.ScriptSuccess, World: next, IdleFrames: maps.Clone(report.State.IdleFrames), LastEventID: report.State.LastEventID, NeedsSceneEnter: next.SceneChanged, Actions: s.current.Actions + 1}
	return report, nil
}
