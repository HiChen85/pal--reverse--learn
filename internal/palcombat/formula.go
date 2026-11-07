package palcombat

// BaseDamage is the observed PAL1 piecewise physical/magical base formula.
// Integer tenths represent the 1.6, 0.6, and +0.5 terms exactly. The signed
// 16-bit result is intentional: the imported engine uses SHORT at this boundary.
func BaseDamage(attack, defense uint16) int16 {
	a, d := int(attack), int(defense)
	n := 0
	if a > d {
		n = (20*a - 16*d + 5) / 10
	} else if 5*a > 3*d {
		n = (10*a - 6*d + 5) / 10
	}
	return int16(n)
}

// PhysicalDamage applies the target's divisor. Zero means no division, rather
// than immunity. Callers apply the attack's random bonuses and minimum damage.
func PhysicalDamage(attack, defense, resistance uint16) int {
	n := int(BaseDamage(attack, defense))
	if resistance != 0 {
		n /= int(resistance)
	}
	return n
}

func effective(p Player, attribute int, base uint16) uint16 {
	n := base
	if p.EquipmentEffects != nil {
		for _, slot := range p.EquipmentEffects {
			n += slot[attribute]
		}
	}
	return n
}

func enemyDefense(e EnemyState) uint16 { return e.Enemy.Defense + (e.Enemy.Level+6)*4 }
func enemyAttack(e EnemyState) uint16 {
	n := int(int16(e.Enemy.Attack)) + (int(e.Enemy.Level)+6)*6
	return uint16(max(0, n))
}
func enemyDexterity(e EnemyState) int {
	return int(int16(e.Enemy.Dexterity)) + (int(e.Enemy.Level)+6)*3
}
func dying(p PlayerState) bool { return int(p.Role.HP) < min(100, int(p.Role.MaxHP)/5) }

func copyPlayer(p Player) Player {
	if p.EquipmentEffects != nil {
		x := *p.EquipmentEffects
		p.EquipmentEffects = &x
	}
	if p.HiddenExperience != nil {
		x := *p.HiddenExperience
		p.HiddenExperience = &x
	}
	return p
}

// CloneSnapshot detaches every mutable collection and pointed-to player value.
// It does not grant mutation access to an Engine; use Engine.Snapshot to obtain
// the current state and callbacks to stage changes within a validated round.
func CloneSnapshot(s Snapshot) Snapshot { return clone(s) }

func clone(s Snapshot) Snapshot {
	s.Party = append([]PlayerState(nil), s.Party...)
	for i := range s.Party {
		s.Party[i].Player = copyPlayer(s.Party[i].Player)
	}
	s.Enemies = append([]EnemyState(nil), s.Enemies...)
	s.Inventory = cloneMap(s.Inventory)
	s.Earned.ByRole = cloneMap(s.Earned.ByRole)
	s.ScriptEntries = cloneMap(s.ScriptEntries)
	s.ObjectChanges = cloneMap(s.ObjectChanges)
	s.MagicBaseDamage = cloneMap(s.MagicBaseDamage)
	return s
}
func cloneMap[K comparable, V any](src map[K]V) map[K]V {
	if src == nil {
		return nil
	}
	dst := make(map[K]V, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

// syncRole keeps the preserved column coherent after combat mutations so a
// caller can write HP/MP and level changes back into its canonical world state.
func syncRole(p *PlayerState) {
	r := &p.Role
	r.Raw[6], r.Raw[7], r.Raw[8], r.Raw[9], r.Raw[10] = r.Level, r.MaxHP, r.MaxMP, r.HP, r.MP
	r.Raw[17], r.Raw[18], r.Raw[19], r.Raw[20], r.Raw[21] = r.Attack, r.Magic, r.Defense, r.Dexterity, r.FleeRate
	copy(r.Raw[32:64], r.Magics[:])
}
