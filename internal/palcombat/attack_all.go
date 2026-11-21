package palcombat

import "fmt"

// PAL_PlayerCanAttackAll reads the seven equipment-effect slots, including the
// temporary battle slot. The preserved base role column is not consulted.
func canAttackAll(p Player) bool {
	if p.EquipmentEffects != nil {
		for _, slot := range p.EquipmentEffects {
			if slot[4] != 0 {
				return true
			}
		}
	}
	return false
}

func (e *Engine) validatePhysicalTarget(p Player, target Target) error {
	// With all-target equipment a previously selected (now defeated) slot is
	// irrelevant: the reference promotes to -1 before checking that target.
	promoted := canAttackAll(p) && target.Side == Enemy && target.Index >= 0 && target.Index < len(e.state.Enemies)
	if promoted || target.Side == All && target.Index == 0 || target.Side == Enemy && target.Index == -1 {
		if retarget(e.state, -1) < 0 {
			return fmt.Errorf("palcombat: physical attack has no living enemy")
		}
		return nil
	}
	return e.livingEnemy(target)
}

// Independently modeled from PAL_CLASSIC fight.c's Attack/all branch and
// global.c PAL_PlayerCanAttackAll, SDLPAL d4bf69b64d335cd680555af9df90e7ec4f3006a3.
// Ordinary selected attacks are promoted here, as by the reference action
// validator. One critical draw applies to each complete sweep; unlike a single
// attack there is no damage bonus, role-zero doubling, or variance draw.
func (e *Engine) attackAll(s *Snapshot, actor int, r *Report) error {
	p := &s.Party[actor]
	sweeps := 1
	if p.Status[DualAttack] > 0 {
		sweeps = 2
	}
	for sweep := 0; sweep < sweeps; sweep++ {
		critical, err := e.draw(6, "attack_all.critical", r)
		if err != nil {
			return err
		}
		crit := critical == 0 || p.Status[Bravery] > 0
		division := float32(1)
		for _, slot := range [...]int{2, 1, 0, 4, 3} {
			if slot > s.MaxEnemySlot {
				continue
			}
			index := enemyIndexAtSlot(s, slot)
			if index < 0 || !enemyPresent(s.Enemies[index]) {
				continue
			}
			x := &s.Enemies[index]
			damage := float32(PhysicalDamage(effective(p.Player, 17, p.Role.Attack), enemyDefense(*x), x.Enemy.PhysicalResistance))
			if crit {
				damage *= 3
			}
			damage /= division
			if damage <= 0 {
				damage = 1
			}
			// The reference subtracts FLOAT from a WORD health field. For a
			// surviving enemy the remaining health truncates toward zero, so
			// fractional damage cannot first be truncated to an integer.
			// Negative FLOAT-to-unsigned conversion is not portable C. The
			// compatibility model explicitly truncates first and wraps the
			// WORD, matching the retained native-reference observations. Keep
			// that word across dual sweeps; signed health is checked only when
			// the complete action reaches fighter collection.
			before := x.HP
			x.HP = int(uint16(int64(float32(before) - damage)))
			amount := before - max(0, int(float32(before)-damage))
			// Preserve the existing clamped event amount for display. The raw
			// WORD result above is authoritative for later hits and callbacks.
			r.Events = append(r.Events, Event{Kind: string(Attack), Actor: Party, ActorIndex: actor, Target: Enemy, TargetIndex: index, HP: -amount, Critical: crit})
			division *= 2
		}
	}
	p.Counters.Attack++
	health, err := e.draw(2, "attack.health_experience", r)
	if err != nil {
		return err
	}
	p.Counters.Health += uint32(health + 2)
	return nil
}
