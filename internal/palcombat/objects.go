package palcombat

import (
	"fmt"

	"github.com/HiChen85/pal--reverse--learn/internal/paldata"
)

const (
	itemUsable    = 1
	itemConsuming = 8
	applyAll      = 16
	magicInBattle = 2
	magicToEnemy  = 8
)

func (e *Engine) magicOffensive(id uint16) bool {
	return int(id) < len(e.objects) && e.objectAt(id)[6]&magicToEnemy != 0
}

func (e *Engine) magicSpec(id uint16) (paldata.Magic, error) {
	if id == 0 || int(id) >= len(e.objects) {
		return paldata.Magic{}, fmt.Errorf("palcombat: magic object %d outside imported table", id)
	}
	if !e.tables.HasBattleTables {
		return paldata.Magic{}, &UnsupportedError{Feature: "missing magic/battlefield tables", ObjectID: id}
	}
	n := e.objectAt(id)[0]
	if int(n) >= len(e.tables.Magics) {
		return paldata.Magic{}, fmt.Errorf("palcombat: object %d magic index %d outside imported table", id, n)
	}
	m := e.tables.Magics[n]
	if damage, ok := e.currentSnapshot().MagicBaseDamage[n]; ok {
		m.BaseDamage = damage
		m.Raw[13] = damage
	}
	if m.Type > 5 {
		return m, &UnsupportedError{Feature: "trance, summon, or unknown magic type", ObjectID: id}
	}
	if int16(m.BaseDamage) < 0 {
		return m, &UnsupportedError{Feature: "negative magic base damage", ObjectID: id}
	}
	if m.BaseDamage > 0 && int(e.battlefield) >= len(e.tables.Battlefields) {
		return m, fmt.Errorf("palcombat: battlefield %d outside imported table", e.battlefield)
	}
	return m, nil
}

func (e *Engine) validateObjectAction(p Player, a Action) error {
	if a.ObjectID == 0 || int(a.ObjectID) >= len(e.objects) {
		return fmt.Errorf("palcombat: action object %d outside table", a.ObjectID)
	}
	o := e.objectAt(a.ObjectID)
	all := o[6]&applyAll != 0
	if all {
		if a.Target != (Target{Side: All}) {
			return fmt.Errorf("palcombat: object %d requires all-target selection", a.ObjectID)
		}
	} else if a.Target.Side != Party && a.Target.Side != Enemy {
		return fmt.Errorf("palcombat: object %d requires a target side", a.ObjectID)
	}
	if a.Kind == UseItem {
		if o[6]&itemUsable == 0 || e.state.Inventory[a.ObjectID] == 0 {
			return fmt.Errorf("palcombat: item %d is unavailable or unusable", a.ObjectID)
		}
		if !all && (a.Target.Side != Party || a.Target.Index < 0 || a.Target.Index >= len(e.state.Party)) {
			return fmt.Errorf("palcombat: item requires a party target")
		}
		return e.checkScript(e.scriptEntry(e.state, a.ObjectID, 2), "item use", a.ObjectID)
	}
	known := false
	for _, id := range p.Role.Magics {
		known = known || id == a.ObjectID
	}
	if !known {
		return fmt.Errorf("palcombat: role %d has not learned magic %d", p.Role.ID, a.ObjectID)
	}
	if o[6]&magicInBattle == 0 {
		return fmt.Errorf("palcombat: magic %d cannot be used in battle", a.ObjectID)
	}
	if p.Status[Silence] > 0 {
		return fmt.Errorf("palcombat: role %d is silenced", p.Role.ID)
	}
	m, err := e.magicSpec(a.ObjectID)
	if err != nil {
		return err
	}
	if p.Role.MP < m.CostMP {
		return fmt.Errorf("palcombat: magic %d needs %d MP", a.ObjectID, m.CostMP)
	}
	toEnemy := o[6]&magicToEnemy != 0
	if toEnemy != (m.Type < 4) {
		return &UnsupportedError{Feature: "inconsistent magic type/target flags", ObjectID: a.ObjectID}
	}
	if !all {
		if toEnemy {
			if err = e.livingEnemy(a.Target); err != nil {
				return err
			}
		} else if a.Target.Side != Party || a.Target.Index < 0 || a.Target.Index >= len(e.state.Party) {
			return fmt.Errorf("palcombat: defensive magic requires party target")
		}
	}
	if err = e.checkScript(e.scriptEntry(e.state, a.ObjectID, 3), "magic use", a.ObjectID); err != nil {
		return err
	}
	return e.checkScript(e.scriptEntry(e.state, a.ObjectID, 2), "magic success", a.ObjectID)
}

func (e *Engine) validateEnemyMagic(x EnemyState) error {
	m, err := e.magicSpec(x.Enemy.Magic)
	if err != nil {
		return err
	}
	if m.Type > 3 {
		return &UnsupportedError{Feature: "enemy defensive magic", ObjectID: x.Enemy.Magic}
	}
	if err = e.checkScript(e.scriptEntry(e.state, x.Enemy.Magic, 3), "enemy magic use", x.Enemy.Magic); err != nil {
		return err
	}
	return e.checkScript(e.scriptEntry(e.state, x.Enemy.Magic, 2), "enemy magic success", x.Enemy.Magic)
}

func (e *Engine) objectAction(s *Snapshot, actor int, a Action, r *Report) error {
	defer func() {
		if s.HidingTime < 0 {
			s.HidingTime = -s.HidingTime
		}
	}()
	o := e.objectAt(a.ObjectID)
	if a.Kind == UseItem {
		if _, err := e.runObjectScript(s, a.ObjectID, 2, "item use", a.Target, r); err != nil {
			return err
		}
		if o[6]&itemConsuming != 0 {
			if s.Inventory[a.ObjectID] == 0 {
				return fmt.Errorf("palcombat: item reservation exhausted")
			}
			s.Inventory[a.ObjectID]--
		}
		r.Events = append(r.Events, Event{Kind: string(UseItem), Actor: Party, ActorIndex: actor, ObjectID: a.ObjectID})
		return nil
	}
	m, err := e.magicSpec(a.ObjectID)
	if err != nil {
		return err
	}
	p := &s.Party[actor]
	if p.Role.MP < m.CostMP || p.Status[Silence] > 0 {
		return &UnsupportedError{Feature: "magic availability changed during round", ObjectID: a.ObjectID}
	}
	p.Role.MP -= m.CostMP
	r.Events = append(r.Events, Event{Kind: "mp_cost", Actor: Party, ActorIndex: actor, ObjectID: a.ObjectID, MP: -int(m.CostMP)})
	ok, err := e.runObjectScript(s, a.ObjectID, 3, "magic use", Target{Side: Party, Index: actor}, r)
	if err != nil {
		return err
	}
	if ok {
		target := a.Target
		if a.Target.Side == Enemy && s.Enemies[a.Target.Index].HP <= 0 {
			for j, x := range s.Enemies {
				if x.HP > 0 {
					target.Index = j
					break
				}
			}
		}
		if _, err = e.runObjectScript(s, a.ObjectID, 2, "magic success", target, r); err != nil {
			return err
		}
		m, err = e.magicSpec(a.ObjectID)
		if err != nil {
			return err
		}
		if m.BaseDamage > 0 && o[6]&magicToEnemy != 0 {
			targets := []int{target.Index}
			if a.Target.Side == All {
				targets = nil
				for j, x := range s.Enemies {
					if x.HP > 0 {
						targets = append(targets, j)
					}
				}
			}
			for _, j := range targets {
				x := s.Enemies[j]
				damage, err := e.magicDamage(effective(p.Player, 18, p.Role.Magic), enemyDefense(x), x.Enemy.ElementalResistance, x.Enemy.PoisonResistance, 1, m, r)
				if err != nil {
					return err
				}
				e.damageEnemy(s, j, max(1, damage), actor, Magic, a.ObjectID, false, r)
			}
		}
	}
	v, err := e.draw(2, "magic.magic_experience", r)
	if err != nil {
		return err
	}
	p.Counters.Magic += uint32(v + 2)
	p.Counters.MagicPower++
	return nil
}

func (e *Engine) magicDamage(strength, defense uint16, resist [5]uint16, poison, multiplier uint16, m paldata.Magic, r *Report) (int, error) {
	v, err := e.draw(fractionSteps+1, "magic.strength_variance", r)
	if err != nil {
		return 0, err
	}
	strength = uint16(int64(strength)*(10*fractionSteps+int64(v))/fractionSteps) / 10
	damage := int16(int(BaseDamage(strength, defense))/4 + int(m.BaseDamage))
	if m.Elemental != 0 {
		resistance := poison
		if m.Elemental <= 5 {
			resistance = resist[m.Elemental-1]
		}
		damage = int16(int64(damage) * (int64(10*multiplier) - int64(resistance)) / int64(multiplier))
		damage /= 5
		if m.Elemental <= 5 {
			damage = int16(int(damage) * (10 + int(e.tables.Battlefields[e.battlefield].MagicEffect[m.Elemental-1])))
			damage /= 10
		}
	}
	return int(damage), nil
}

func (e *Engine) enemyMagic(s *Snapshot, actor, target int, r *Report) error {
	x := s.Enemies[actor]
	m, err := e.magicSpec(x.Enemy.Magic)
	if err != nil {
		return err
	}
	targets := []int{target}
	if m.Type != 0 {
		targets = nil
		for j, p := range s.Party {
			if p.Role.HP > 0 {
				targets = append(targets, j)
			}
		}
	}
	guards := map[int]bool{}
	guardTargets := targets
	if m.Type != 0 {
		guardTargets = nil
		for j := range s.Party {
			guardTargets = append(guardTargets, j)
		}
	}
	for _, j := range guardTargets {
		p := s.Party[j]
		if p.Status[Sleep] == 0 && p.Status[Paralyzed] == 0 && p.Status[Confused] == 0 {
			v, err := e.draw(3, "enemy.magic_auto_defend", r)
			if err != nil {
				return err
			}
			guards[j] = v == 0 && p.Role.HP > 0
		}
	}
	ok, err := e.runObjectScript(s, x.Enemy.Magic, 3, "enemy magic use", Target{Side: Party, Index: target}, r)
	if err != nil {
		return err
	}
	if ok {
		if _, err = e.runObjectScript(s, x.Enemy.Magic, 2, "enemy magic success", Target{Side: Party, Index: target}, r); err != nil {
			return err
		}
	}
	if m.BaseDamage == 0 {
		return nil
	}
	if m.Type != 0 {
		targets = nil
		for j, p := range s.Party {
			if p.Role.HP > 0 {
				targets = append(targets, j)
			}
		}
	}
	strength := uint16(max(0, int(int16(x.Enemy.MagicStrength))+(int(x.Enemy.Level)+6)*6))
	for _, j := range targets {
		p := s.Party[j]
		var resistance [5]uint16
		for k := range resistance {
			resistance[k] = 100 + min(uint16(100), effective(p.Player, 23+k, p.Role.ElementalResistance[k]))
		}
		poison := 100 + min(uint16(100), effective(p.Player, 22, p.Role.PoisonResistance))
		damage, err := e.magicDamage(strength, effective(p.Player, 19, p.Role.Defense), resistance, poison, 20, m, r)
		if err != nil {
			return err
		}
		div := 1
		if p.Defending {
			div *= 2
		}
		if p.Status[Protect] > 0 {
			div *= 2
		}
		if guards[j] {
			div++
		}
		damage /= div
		if damage < 0 {
			return &UnsupportedError{Feature: "negative enemy magic damage", ObjectID: x.Enemy.Magic}
		}
		if err = e.damagePlayer(s, j, damage, actor, Magic, x.Enemy.Magic, r); err != nil {
			return err
		}
	}
	return nil
}
