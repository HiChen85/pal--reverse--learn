package palcombat

import (
	"fmt"

	"github.com/HiChen85/pal--reverse--learn/internal/paldata"
)

// A defeated record keeps its historical identity, HP word, status and script
// slots. Presence is separate: the source clears only its active object ID.
func enemyPresent(x EnemyState) bool { return x.ObjectID != 0 && !x.Rewarded }

func enemyIndexAtSlot(s *Snapshot, slot int) int {
	for i := range s.Enemies {
		if s.Enemies[i].Slot == slot {
			return i
		}
	}
	return -1
}

func enemyIndexes(s *Snapshot) []int {
	indexes := make([]int, 0, len(s.Enemies))
	for slot := 0; slot <= s.MaxEnemySlot; slot++ {
		if i := enemyIndexAtSlot(s, slot); i >= 0 {
			indexes = append(indexes, i)
		}
	}
	return indexes
}

// Applying a VM result preserves every existing slice index. A queued action
// therefore refers to its original slot even if that slot was killed and then
// summoned again, as in the classic fixed-slot action queue.
func (e *Engine) applyEnemyTopology(s *Snapshot, change *EnemyTopology) error {
	if change == nil {
		return nil
	}
	if change.MaxSlot < 0 || change.MaxSlot >= paldata.EnemyTeamSize || len(change.Enemies) > paldata.EnemyTeamSize {
		return fmt.Errorf("palcombat: invalid explicit enemy topology extent")
	}
	seen := [paldata.EnemyTeamSize]bool{}
	for _, x := range change.Enemies {
		if x.Slot < 0 || x.Slot >= paldata.EnemyTeamSize || seen[x.Slot] {
			return fmt.Errorf("palcombat: duplicate or invalid explicit enemy slot %d", x.Slot)
		}
		seen[x.Slot] = true
		oldIndex := enemyIndexAtSlot(s, x.Slot)
		if oldIndex >= 0 {
			old := s.Enemies[oldIndex]
			if !old.Rewarded && old.ObjectID != 0 {
				if x.Rewarded != old.Rewarded || x.TurnStart != old.TurnStart || x.BattleEnd != old.BattleEnd || x.Ready != old.Ready {
					return &UnsupportedError{Feature: "topology changes living enemy reward or continuations", ObjectID: x.ObjectID}
				}
			} else if x.Rewarded && x != old {
				return &UnsupportedError{Feature: "topology edits a rewarded enemy without replacing it", ObjectID: x.ObjectID}
			}
		} else if x.Rewarded {
			return &UnsupportedError{Feature: "topology creates a rewarded enemy", ObjectID: x.ObjectID}
		}
		if !x.Rewarded && x.ObjectID != 0 {
			if x.Slot > change.MaxSlot || int(x.ObjectID) >= len(e.objects) || int(e.objectAt(x.ObjectID)[0]) >= len(e.tables.Enemies) {
				return fmt.Errorf("palcombat: explicit enemy object %d or slot outside imported topology", x.ObjectID)
			}
			for _, entry := range []uint16{x.TurnStart, x.BattleEnd, x.Ready} {
				if entry != 0 && int(entry) >= len(e.scripts) {
					return fmt.Errorf("palcombat: explicit enemy continuation %d outside imported table", entry)
				}
			}
		}
	}
	for _, old := range s.Enemies {
		if !seen[old.Slot] {
			return &UnsupportedError{Feature: "topology removes an enemy record", ObjectID: old.ObjectID}
		}
	}
	for _, x := range change.Enemies {
		if i := enemyIndexAtSlot(s, x.Slot); i >= 0 {
			s.Enemies[i] = x
		} else {
			s.Enemies = append(s.Enemies, x)
		}
	}
	s.MaxEnemySlot = change.MaxSlot
	return nil
}
