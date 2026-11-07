package palcombat

import (
	"fmt"
	"maps"
)

func (e *Engine) scriptEntry(s Snapshot, object uint16, word int) uint16 {
	if v, ok := s.ScriptEntries[uint32(object)*8+uint32(word)]; ok {
		return v
	}
	return e.objectAt(object)[word]
}

func (e *Engine) checkScript(entry uint16, hook string, object uint16) error {
	if entry == 0 {
		return nil
	}
	if int(entry) >= len(e.scripts) {
		return fmt.Errorf("palcombat: %s entry %d outside imported scripts", hook, entry)
	}
	if e.callbacks == nil {
		return &UnsupportedError{Feature: hook, ObjectID: object, Entry: entry}
	}
	return nil
}

type scriptRandom struct {
	e      *Engine
	report *Report
	reason string
}

func (s scriptRandom) Intn(n int) (int, error) { return s.e.draw(n, s.reason, s.report) }

func (e *Engine) runScript(s *Snapshot, request ScriptRequest, r *Report, next *uint16) (bool, error) {
	if request.Entry == 0 && e.transaction == nil {
		return true, nil
	}
	if err := e.checkScript(request.Entry, request.Hook, request.ObjectID); err != nil {
		return false, err
	}
	if e.transaction == nil {
		return false, fmt.Errorf("palcombat: no active script transaction")
	}
	party, enemies := &s.Party[0], &s.Enemies[0]
	// Capture an enemy continuation by index before a typed topology result can
	// append and move the backing array. Local/object continuation pointers are
	// independent of that array and retain their existing path.
	nextEnemy, nextWord := -1, 0
	for i := range s.Enemies {
		for word, p := range []*uint16{&s.Enemies[i].TurnStart, &s.Enemies[i].BattleEnd, &s.Enemies[i].Ready} {
			if next != nil && next == p {
				nextEnemy, nextWord = i, word
			}
		}
	}
	outcome, round, started, team := s.Outcome, s.Round, s.Started, s.TeamID
	maxEnemySlot := s.MaxEnemySlot
	rewards := s.Earned
	rewards.ByRole = cloneMap(rewards.ByRole)
	entries := cloneMap(s.ScriptEntries)
	objects := cloneMap(s.ObjectChanges)
	magics := cloneMap(s.MagicBaseDamage)
	ownedEnemies := make([]struct {
		rewarded         bool
		turn, end, ready uint16
		object           uint16
		slot             int
	}, len(s.Enemies))
	for i, x := range s.Enemies {
		ownedEnemies[i] = struct {
			rewarded         bool
			turn, end, ready uint16
			object           uint16
			slot             int
		}{x.Rewarded, x.TurnStart, x.BattleEnd, x.Ready, x.ObjectID, x.Slot}
	}
	result, err := e.transaction.Run(request, s, scriptRandom{e, r, "script." + request.Hook})
	r.Scripts = append(r.Scripts, result.Trace...)
	if err != nil {
		return false, err
	}
	if e.randomFailure != nil {
		return false, e.randomFailure
	}
	if len(s.Party) == 0 || len(s.Enemies) == 0 || &s.Party[0] != party || &s.Enemies[0] != enemies {
		return false, &UnsupportedError{Feature: "script replaces fighter slices", ObjectID: request.ObjectID, Entry: request.Entry}
	}
	if len(s.Enemies) != len(ownedEnemies) {
		return false, &UnsupportedError{Feature: "script changes enemy topology", ObjectID: request.ObjectID, Entry: request.Entry}
	}
	for i, x := range s.Enemies {
		old := ownedEnemies[i]
		if x.Rewarded != old.rewarded || x.TurnStart != old.turn || x.BattleEnd != old.end || x.Ready != old.ready || x.ObjectID != old.object || x.Slot != old.slot {
			return false, &UnsupportedError{Feature: "script modifies engine-owned enemy fields", ObjectID: request.ObjectID, Entry: request.Entry}
		}
	}
	if s.Outcome != outcome || s.Round != round || s.Started != started || s.TeamID != team || s.MaxEnemySlot != maxEnemySlot || s.Earned.Experience != rewards.Experience || s.Earned.Cash != rewards.Cash || s.Earned.Applied != rewards.Applied || !maps.Equal(s.Earned.ByRole, rewards.ByRole) || !maps.Equal(s.ScriptEntries, entries) || !maps.Equal(s.ObjectChanges, objects) || !maps.Equal(s.MagicBaseDamage, magics) {
		return false, &UnsupportedError{Feature: "script modifies engine-owned round fields", ObjectID: request.ObjectID, Entry: request.Entry}
	}
	if result.NextEntry != 0 && int(result.NextEntry) >= len(e.scripts) {
		return false, fmt.Errorf("palcombat: script continuation %d outside imported table", result.NextEntry)
	}
	if err = e.validateSnapshot(*s); err != nil {
		return false, err
	}
	if err = e.applyScriptResult(s, result); err != nil {
		return false, err
	}
	if err = e.validateSnapshot(*s); err != nil {
		return false, err
	}
	if next != nil {
		if nextEnemy >= 0 {
			x := &s.Enemies[nextEnemy]
			*[]*uint16{&x.TurnStart, &x.BattleEnd, &x.Ready}[nextWord] = result.NextEntry
		} else {
			*next = result.NextEntry
		}
	}
	return result.Success, nil
}

func (e *Engine) runObjectScript(s *Snapshot, object uint16, word int, hook string, target Target, r *Report) (bool, error) {
	invoker := uint16(0xffff)
	switch target.Side {
	case Party:
		if target.Index < 0 || target.Index >= len(s.Party) {
			return false, fmt.Errorf("palcombat: script party target outside snapshot")
		}
		invoker = s.Party[target.Index].Role.ID
	case Enemy:
		if target.Index < 0 || target.Index >= len(s.Enemies) {
			return false, fmt.Errorf("palcombat: script enemy target outside snapshot")
		}
		invoker = uint16(s.Enemies[target.Index].Slot)
	case All:
		if hook == "magic success" && !e.magicOffensive(object) {
			invoker = 0
		}
	default:
		return false, fmt.Errorf("palcombat: script target side %q is unsupported", target.Side)
	}
	entry := e.scriptEntry(*s, object, word)
	next := entry
	ok, err := e.runScript(s, ScriptRequest{Hook: hook, ObjectID: object, Entry: entry, Invoker: invoker, Target: target}, r, &next)
	if err == nil && entry != 0 {
		if s.ScriptEntries == nil {
			s.ScriptEntries = map[uint32]uint16{}
		}
		s.ScriptEntries[uint32(object)*8+uint32(word)] = next
	}
	return ok, err
}

func (e *Engine) turnStart(s *Snapshot, r *Report) error {
	for slot := 0; slot <= s.MaxEnemySlot; slot++ {
		i := enemyIndexAtSlot(s, slot)
		if i < 0 {
			continue
		}
		x := &s.Enemies[i]
		if !enemyPresent(*x) || x.HP <= 0 {
			continue
		}
		if _, err := e.runScript(s, ScriptRequest{Hook: "enemy turn start", ObjectID: x.ObjectID, Entry: x.TurnStart, Invoker: uint16(x.Slot), Target: Target{Side: Enemy, Index: i}}, r, &x.TurnStart); err != nil {
			return err
		}
		if s.Outcome != Ongoing {
			break
		}
	}
	return nil
}

// PAL_BattleMain's initial pass invokes every fixed slot, including empty
// slots and zero entries. Later classic turn-start passes filter empty slots.
// Zero-entry callbacks still update trigger context in a connected script VM.
func (e *Engine) preBattle(s *Snapshot, r *Report) error {
	for slot := 0; slot <= s.MaxEnemySlot; slot++ {
		i := enemyIndexAtSlot(s, slot)
		if i < 0 {
			if _, err := e.runScript(s, ScriptRequest{Hook: "enemy turn start", Invoker: uint16(slot), Target: Target{Side: Enemy, Index: -1}}, r, nil); err != nil {
				return err
			}
		} else {
			x := &s.Enemies[i]
			if _, err := e.runScript(s, ScriptRequest{Hook: "enemy turn start", ObjectID: x.ObjectID, Entry: x.TurnStart, Invoker: uint16(slot), Target: Target{Side: Enemy, Index: i}}, r, &x.TurnStart); err != nil {
				return err
			}
		}
		if s.Outcome != Ongoing {
			break
		}
	}
	return nil
}

// Party identities stay fixed. Enemy identities change only through the typed
// result path; runScript separately rejects direct identity/slice mutations.
func (e *Engine) validateSnapshot(s Snapshot) error {
	if len(s.Party) != len(e.state.Party) || len(s.Enemies) < 1 || len(s.Enemies) > 5 || s.TeamID != e.state.TeamID || s.MaxEnemySlot < 0 || s.MaxEnemySlot >= 5 {
		return &UnsupportedError{Feature: "script changes fighter topology"}
	}
	for i, p := range s.Party {
		if p.Role.ID != e.state.Party[i].Role.ID || p.Role.HP > p.Role.MaxHP || p.Role.MP > p.Role.MaxMP || p.Role.MaxHP == 0 || p.Role.Level > 99 || int(p.Role.NameIndex) >= len(e.objects) {
			return fmt.Errorf("palcombat: invalid player snapshot at slot %d", i)
		}
	}
	seen := [5]bool{}
	for _, x := range s.Enemies {
		if x.Slot < 0 || x.Slot >= 5 || seen[x.Slot] || int(x.ObjectID) >= len(e.objects) || x.HP < 0 || x.HP > 65535 || (!x.Rewarded && x.ObjectID != 0 && x.Slot > s.MaxEnemySlot) {
			return &UnsupportedError{Feature: "script changes enemy identity or signed HP", ObjectID: x.ObjectID}
		}
		seen[x.Slot] = true
	}
	for id, n := range s.Inventory {
		if id == 0 || int(id) >= len(e.objects) || n > 99 {
			return fmt.Errorf("palcombat: invalid script inventory object %d amount %d", id, n)
		}
	}
	return nil
}

func (e *Engine) finish(s *Snapshot) {
	for i := range s.Party {
		p := &s.Party[i]
		p.Defending = false
		for j, n := range p.Status {
			if n <= 999 {
				p.Status[j] = 0
			}
		}
		for slot, poison := range p.Poisons {
			if int(poison.ID) < len(e.objects) && e.objectAt(poison.ID)[0] <= 3 {
				p.Poisons[slot] = Poison{}
			}
		}
		if p.EquipmentEffects != nil {
			p.EquipmentEffects[6] = [75]uint16{}
		}
	}
}
