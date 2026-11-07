package palcombat

import "fmt"

func (e *Engine) currentSnapshot() *Snapshot {
	if e.active != nil {
		return e.active
	}
	return &e.state
}
func (e *Engine) objectAt(id uint16) [7]uint16 {
	if x, ok := e.currentSnapshot().ObjectChanges[id]; ok {
		return x
	}
	return e.objects[id]
}

func (e *Engine) applyScriptResult(s *Snapshot, result ScriptResult) error {
	for id, object := range result.ObjectChanges {
		if int(id) >= len(e.objects) {
			return fmt.Errorf("palcombat: script changes missing object %d", id)
		}
		old := e.objectAt(id)
		if object[0] != old[0] || object[1] != old[1] {
			return &UnsupportedError{Feature: "script changes object identity words", ObjectID: id}
		}
		if s.ObjectChanges == nil {
			s.ObjectChanges = map[uint16][7]uint16{}
		}
		if s.ScriptEntries == nil {
			s.ScriptEntries = map[uint32]uint16{}
		}
		s.ObjectChanges[id] = object
		for word := 2; word <= 6; word++ {
			if object[word] != old[word] {
				s.ScriptEntries[uint32(id)*8+uint32(word)] = object[word]
			}
		}
	}
	for id, damage := range result.MagicBaseDamage {
		if int(id) >= len(e.tables.Magics) {
			return fmt.Errorf("palcombat: script changes missing magic %d", id)
		}
		if s.MagicBaseDamage == nil {
			s.MagicBaseDamage = map[uint16]uint16{}
		}
		s.MagicBaseDamage[id] = damage
	}
	if err := e.applyEnemyTopology(s, result.EnemyTopology); err != nil {
		return err
	}
	if result.ForcedOutcome != nil {
		switch *result.ForcedOutcome {
		case Ongoing: // A running-phase result does not manufacture an ending.
		case Victory, Defeat, Escaped, Terminated:
			s.Outcome = *result.ForcedOutcome
		default:
			return &UnsupportedError{Feature: "script requests unknown battle outcome"}
		}
	}
	return nil
}

func (e *Engine) endTurn(s *Snapshot, r *Report) error {
	for i := range s.Party {
		s.Party[i].Defending = false
		for slot := range s.Party[i].Poisons {
			poison := s.Party[i].Poisons[slot]
			if poison.ID == 0 {
				continue
			}
			next := poison.Script
			_, err := e.runScript(s, ScriptRequest{Hook: "player poison", ObjectID: poison.ID, Entry: poison.Script, Invoker: s.Party[i].Role.ID, Target: Target{Side: Party, Index: i}, PoisonSlot: slot}, r, &next)
			if err != nil {
				return err
			}
			// A self-cured poison still receives its returned continuation in
			// the original slot, including the valid ID=0/Script!=0 case.
			s.Party[i].Poisons[slot].Script = next
		}
		for k, v := range s.Party[i].Status {
			if v > 0 {
				s.Party[i].Status[k]--
			}
		}
	}
	for enemySlot := 0; enemySlot <= s.MaxEnemySlot; enemySlot++ {
		i := enemyIndexAtSlot(s, enemySlot)
		if i < 0 {
			continue
		}
		// Classic poison callbacks and status ticks inspect every fixed slot,
		// including retained fields after its active object ID was cleared.
		for slot := range s.Enemies[i].Poisons {
			poison := s.Enemies[i].Poisons[slot]
			if poison.ID == 0 {
				continue
			}
			next := poison.Script
			_, err := e.runScript(s, ScriptRequest{Hook: "enemy poison", ObjectID: poison.ID, Entry: poison.Script, Invoker: uint16(s.Enemies[i].Slot), Target: Target{Side: Enemy, Index: i}, PoisonSlot: slot}, r, &next)
			if err != nil {
				return err
			}
			s.Enemies[i].Poisons[slot].Script = next
		}
		for k, v := range s.Enemies[i].Status {
			if v > 0 {
				s.Enemies[i].Status[k]--
			}
		}
	}
	// Mark defeats before callbacks for the next round, but defer defeat of
	// the party: a turn-start callback may revive a player.
	for _, i := range enemyIndexes(s) {
		x := &s.Enemies[i]
		if enemyPresent(*x) && x.HP == 0 {
			x.Rewarded = true
			s.Earned.Experience += uint32(x.Enemy.Exp)
			s.Earned.Cash += uint32(x.Enemy.Cash)
			r.Events = append(r.Events, Event{Kind: "enemy_defeated", Actor: Enemy, ActorIndex: i})
		}
	}
	if s.HidingTime > 0 {
		s.HidingTime--
	}
	if s.Outcome == Ongoing && s.HidingTime == 0 {
		if err := e.turnStart(s, r); err != nil {
			return err
		}
	}
	e.collect(s, r)
	return nil
}
