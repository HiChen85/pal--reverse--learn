package palflow

import (
	"fmt"

	"github.com/HiChen85/pal--reverse--learn/internal/palcombat"
	"github.com/HiChen85/pal--reverse--learn/internal/paldata"
)

// Defeated records retain their identity for evidence and reward accounting.
// Rewarded is the numeric engine's equivalent of the source's cleared object ID.
func battleObjectID(x palcombat.EnemyState) uint16 {
	if x.Rewarded {
		return 0
	}
	return x.ObjectID
}

func occupiedEnemy(x palcombat.EnemyState) bool { return battleObjectID(x) != 0 }

func topologyEnemy(s *palcombat.Snapshot, slot int) *palcombat.EnemyState {
	for i := range s.Enemies {
		if s.Enemies[i].Slot == slot {
			return &s.Enemies[i]
		}
	}
	return nil
}

func putTopologyEnemy(s *palcombat.Snapshot, x palcombat.EnemyState) {
	if old := topologyEnemy(s, x.Slot); old != nil {
		*old = x
	} else {
		s.Enemies = append(s.Enemies, x)
	}
}

func topologyExtent(s *palcombat.Snapshot) int {
	n := s.MaxEnemySlot
	for _, x := range s.Enemies {
		if occupiedEnemy(x) {
			n = max(n, x.Slot)
		}
	}
	return n
}

// This is a numeric behavior model of the pinned script predicates. It never
// advances a game frame or introduces random draws for animation delays.
func (r *Runner) battleTopology(w *WorldState, invoker uint16, inst Instruction) (uint16, error) {
	if !w.InBattle {
		return 0, fmt.Errorf("enemy topology instruction requires an active battle")
	}
	if err := battleSnapshot(w); err != nil {
		return 0, err
	}
	s, a := w.Battle, inst.Operands
	source := topologyEnemy(s, int(invoker))
	if source == nil {
		return 0, fmt.Errorf("enemy topology invoker slot %d is absent", invoker)
	}
	if !occupiedEnemy(*source) {
		// Battle-end callbacks can run after the source clears this slot's
		// object ID. Explicit summons from that retained record are not yet
		// modeled; refuse the whole instruction instead of reviving its
		// historical evidence identity. Tracked as PAL-BATTLE-002.
		return 0, &palcombat.UnsupportedError{Feature: "enemy topology from a cleared invoking slot", ObjectID: source.ObjectID}
	}
	// Copy before any append: division may allocate a new backing array.
	base := *source
	blocked := w.BattleHidingTime > 0 || base.Status[palcombat.Sleep] != 0 || base.Status[palcombat.Paralyzed] != 0 || base.Status[palcombat.Confused] != 0
	switch inst.Opcode {
	case 0x9c:
		currentSlot := int(invoker)
		if a[0] != 0 && a[0] != 0xffff {
			currentSlot = int(a[0])
		}
		if currentSlot >= paldata.EnemyTeamSize {
			return 0, fmt.Errorf("division current enemy slot %d exceeds classic capacity", currentSlot)
		}
		current := topologyEnemy(s, currentSlot)
		count := 0
		for _, x := range s.Enemies {
			if occupiedEnemy(x) {
				count++
			}
		}
		// The operand also selects wCurEventObjectID in the source's common
		// prelude. Keep that alias, including failure on an implicit empty slot.
		if count != 1 || current == nil || current.HP <= 1 {
			return a[1], nil
		}
		n := int(a[0])
		if n == 0 {
			n = 1
		}
		hp := (base.HP + n) / (n + 1)
		remaining := n
		for slot := 0; slot < paldata.EnemyTeamSize && remaining > 0; slot++ {
			old := topologyEnemy(s, slot)
			if old != nil && occupiedEnemy(*old) {
				continue
			}
			x := palcombat.EnemyState{Slot: slot, ObjectID: base.ObjectID, Enemy: base.Enemy, TurnStart: base.TurnStart, BattleEnd: base.BattleEnd, Ready: base.Ready}
			setBattleEnemyHP(&x, uint16(hp))
			putTopologyEnemy(s, x)
			remaining--
		}
		setBattleEnemyHP(topologyEnemy(s, currentSlot), uint16(hp))
		s.MaxEnemySlot = 0
		for _, x := range s.Enemies {
			if occupiedEnemy(x) {
				s.MaxEnemySlot = max(s.MaxEnemySlot, x.Slot)
			}
		}
	case 0x9e:
		n := int(int16(a[1]))
		if n <= 0 {
			n = 1
		}
		extent, available := topologyExtent(s), 0
		for slot := 0; slot <= extent; slot++ {
			old := topologyEnemy(s, slot)
			if old == nil || !occupiedEnemy(*old) {
				available++
			}
		}
		if blocked || available < n {
			return a[2], nil
		}
		id := a[0]
		if id == 0 || id == 0xffff {
			id = base.ObjectID
		}
		enemy, object, err := r.topologyBaseline(w, id)
		if err != nil {
			return 0, err
		}
		for slot := 0; slot <= extent && n > 0; slot++ {
			old := topologyEnemy(s, slot)
			if old != nil && occupiedEnemy(*old) {
				continue
			}
			putTopologyEnemy(s, palcombat.EnemyState{Slot: slot, ObjectID: id, Enemy: enemy, HP: int(enemy.HP), TurnStart: object[2], BattleEnd: object[3], Ready: object[4]})
			n--
		}
	case 0x9f:
		if blocked {
			return 0, nil
		}
		enemy, _, err := r.topologyBaseline(w, a[0])
		if err != nil {
			return 0, err
		}
		source.ObjectID, source.Enemy = a[0], enemy
		setBattleEnemyHP(source, uint16(base.HP))
	}
	return 0, nil
}

func (r *Runner) topologyBaseline(w *WorldState, id uint16) (paldata.Enemy, [7]uint16, error) {
	object, ok := w.Objects[id]
	if id == 0 || id == 0xffff || !ok {
		return paldata.Enemy{}, object, fmt.Errorf("topology enemy object %d is absent or invalid", id)
	}
	tables, err := r.battleTables()
	if err != nil {
		return paldata.Enemy{}, object, err
	}
	if int(object[0]) >= len(tables.Enemies) {
		return paldata.Enemy{}, object, fmt.Errorf("topology enemy table index %d is outside imported table", object[0])
	}
	return tables.Enemies[object[0]], object, nil
}
