package palcombat

import (
	"fmt"

	"github.com/HiChen85/pal--reverse--learn/internal/paldata"
)

func New(c Config) (*Engine, error) {
	if c.Tables == nil || c.Game == nil || c.Random == nil {
		return nil, fmt.Errorf("palcombat: explicit tables, game, and random source are required")
	}
	if len(c.Party) < 1 || len(c.Party) > 3 {
		return nil, fmt.Errorf("palcombat: classic party must have 1..3 members")
	}
	if int(c.TeamID) >= len(c.Tables.EnemyTeams) {
		return nil, fmt.Errorf("palcombat: enemy team %d is outside imported table", c.TeamID)
	}
	e := &Engine{tables: *c.Tables, objects: append([][7]uint16(nil), c.Game.Objects...), random: c.Random, boss: c.Boss, battlefield: c.Battlefield, callbacks: c.Callbacks}
	e.scripts = append([]paldata.Instruction(nil), c.Game.Scripts...)
	e.tables.Enemies = append([]paldata.Enemy(nil), c.Tables.Enemies...)
	e.tables.EnemyTeams = append([][paldata.EnemyTeamSize]uint16(nil), c.Tables.EnemyTeams...)
	e.tables.Stores = append([][paldata.StoreSize]uint16(nil), c.Tables.Stores...)
	e.tables.Magics = append([]paldata.Magic(nil), c.Tables.Magics...)
	e.tables.Battlefields = append([]paldata.Battlefield(nil), c.Tables.Battlefields...)
	e.tables.LevelUpMagics = append([][paldata.PlayableRoleCount]paldata.LevelUpMagic(nil), c.Tables.LevelUpMagics...)
	e.state = Snapshot{Round: 1, Outcome: Ongoing, TeamID: c.TeamID, Cash: c.Cash, Inventory: cloneMap(c.Inventory), ScriptEntries: map[uint32]uint16{}}
	if c.HidingTime < -65535 || c.HidingTime > 65535 {
		return nil, fmt.Errorf("palcombat: hiding time outside source operand range")
	}
	e.state.HidingTime = c.HidingTime
	seen := map[uint16]bool{}
	for _, p := range c.Party {
		r := p.Role
		if r.ID >= paldata.RoleCount || seen[r.ID] {
			return nil, fmt.Errorf("palcombat: duplicate or invalid role %d", r.ID)
		}
		seen[r.ID] = true
		if r.MaxHP == 0 || r.HP > r.MaxHP || r.MP > r.MaxMP {
			return nil, fmt.Errorf("palcombat: invalid HP/MP for role %d", r.ID)
		}
		if r.Level > paldata.MaxLevel || p.Experience > 65535 {
			return nil, fmt.Errorf("palcombat: invalid level/experience for role %d", r.ID)
		}
		if int(r.NameIndex) >= len(e.objects) {
			return nil, fmt.Errorf("palcombat: role %d name object %d outside table", r.ID, r.NameIndex)
		}
		if p.EquipmentEffects == nil {
			for _, id := range r.Equipment {
				if id != 0 {
					return nil, &UnsupportedError{Feature: "unevaluated equipment effects", ObjectID: id}
				}
			}
		}
		p = copyPlayer(p)
		p.Counters = Counters{}
		if h := p.HiddenExperience; h != nil {
			for _, track := range []*paldata.Experience{&h.Health, &h.Magic, &h.Attack, &h.MagicPower, &h.Defense, &h.Dexterity, &h.Flee} {
				track.Count = 0
			}
		}
		// The classic battle entry revives a role brought in with zero HP to 1.
		if p.Role.HP == 0 {
			p.Role.HP = 1
			p.Status[Puppet] = 0
		}
		x := PlayerState{Player: p}
		syncRole(&x)
		e.state.Party = append(e.state.Party, x)
	}
	for id, n := range e.state.Inventory {
		if id == 0 || int(id) >= len(e.objects) || n > 99 {
			return nil, fmt.Errorf("palcombat: invalid inventory object %d amount %d", id, n)
		}
	}
	for slot, id := range c.Tables.EnemyTeams[c.TeamID] {
		if id == 0 || id == 0xffff {
			continue
		}
		if int(id) >= len(e.objects) {
			return nil, fmt.Errorf("palcombat: enemy object %d outside table", id)
		}
		n := e.objectAt(id)[0]
		if int(n) >= len(e.tables.Enemies) {
			return nil, fmt.Errorf("palcombat: enemy object %d table index %d outside imported enemies", id, n)
		}
		x := e.tables.Enemies[n]
		if x.HP == 0 {
			return nil, &UnsupportedError{Feature: "zero imported enemy HP", ObjectID: id}
		}
		o := e.objectAt(id)
		e.state.Enemies = append(e.state.Enemies, EnemyState{Slot: slot, ObjectID: id, Enemy: x, HP: int(x.HP), TurnStart: o[2], BattleEnd: o[3], Ready: o[4]})
	}
	if len(e.state.Enemies) == 0 {
		return nil, fmt.Errorf("palcombat: imported enemy team is empty")
	}
	return e, nil
}

func (e *Engine) Snapshot() Snapshot { return clone(e.state) }

func canAct(p PlayerState) bool {
	return p.Role.HP > 0 && p.Status[Sleep] == 0 && p.Status[Paralyzed] == 0
}

// Submit executes one classic round after validating the entire command batch.
// Each conscious living member needs exactly one command. Disabled or dead
// members need none. Commands cannot manufacture rewards or battle outcomes.
func (e *Engine) Submit(actions []Action) (Report, error) {
	r := Report{Round: e.state.Round, Outcome: e.state.Outcome}
	if e.state.Outcome != Ongoing {
		return r, fmt.Errorf("palcombat: battle ended with %s", e.state.Outcome)
	}
	commands, err := e.validate(actions)
	if err != nil {
		return r, err
	}
	e.randomFailure = nil
	s := clone(e.state)
	e.active = &s
	defer func() { e.active = nil }()
	if e.callbacks != nil {
		tx, beginErr := e.callbacks.Begin(clone(s))
		if beginErr != nil {
			return r, beginErr
		}
		if tx == nil {
			return r, fmt.Errorf("palcombat: callbacks returned a nil transaction")
		}
		e.transaction = tx
		defer func() {
			if !r.Committed {
				tx.Rollback()
			}
			e.transaction = nil
		}()
	}
	for i := range s.Party {
		s.Party[i].Defending = false
	}
	if !s.Started {
		if err = e.turnStart(&s, &r); err != nil {
			return r, err
		}
		s.Started = true
		e.collect(&s, &r)
	}
	var queue []queueEntry
	if s.Outcome == Ongoing {
		queue, err = e.queue(s, commands, &r)
		if err != nil {
			return r, err
		}
	}
	for _, q := range queue {
		if s.Outcome != Ongoing {
			break
		}
		if q.side == Party {
			if !canAct(s.Party[q.index]) {
				continue
			}
			err = e.playerAction(&s, q.index, commands[s.Party[q.index].Role.ID], &r)
		} else if s.Enemies[q.index].HP > 0 && s.HidingTime == 0 {
			x := &s.Enemies[q.index]
			_, err = e.runScript(&s, ScriptRequest{Hook: "enemy ready", ObjectID: x.ObjectID, Entry: x.Ready, Invoker: uint16(x.Slot), Target: Target{Side: Enemy, Index: q.index}}, &r, &x.Ready)
			if err == nil {
				e.collect(&s, &r)
			}
			if err == nil && s.Outcome == Ongoing && s.Enemies[q.index].HP > 0 {
				err = e.enemyAction(&s, q.index, &r)
			}
		}
		if err != nil {
			return r, err
		}
		e.collect(&s, &r)
		if s.Outcome != Ongoing {
			break
		}
	}
	if s.Outcome == Ongoing {
		if err = e.endTurn(&s, &r); err != nil {
			return r, err
		}
		s.Round++
	}

	if s.Outcome == Victory {
		if err = e.applyRewards(&s, &r); err != nil {
			return r, err
		}
	}
	if s.Outcome != Ongoing {
		e.finish(&s)
	}
	for i := range s.Party {
		syncRole(&s.Party[i])
	}
	if err = e.validateSnapshot(s); err != nil {
		return r, err
	}
	if e.randomFailure != nil {
		return r, e.randomFailure
	}
	if e.transaction != nil {
		if err = e.transaction.Commit(clone(s)); err != nil {
			return r, err
		}
	}
	e.state = s
	r.Outcome = s.Outcome
	r.Committed = true
	r.Rewards = s.Earned
	r.Rewards.ByRole = cloneMap(s.Earned.ByRole)
	switch s.Outcome {
	case Victory:
		v := 0
		r.StoryResult = &v
	case Defeat:
		v := 1
		r.StoryResult = &v
	case Terminated:
		v := 0
		r.StoryResult = &v
	case Escaped:
		v := 2
		r.StoryResult = &v
	}
	return r, nil
}

func (e *Engine) validate(actions []Action) (map[uint16]Action, error) {
	commands := make(map[uint16]Action, len(actions))
	reserved := map[uint16]uint16{}
	party := map[uint16]PlayerState{}
	for _, p := range e.state.Party {
		party[p.Role.ID] = p
	}
	for _, a := range actions {
		p, ok := party[a.Actor]
		if !ok || !canAct(p) {
			return nil, fmt.Errorf("palcombat: role %d cannot act", a.Actor)
		}
		if _, ok = commands[a.Actor]; ok {
			return nil, fmt.Errorf("palcombat: duplicate command for role %d", a.Actor)
		}
		switch a.Kind {
		case Attack:
			if a.ObjectID != 0 {
				return nil, fmt.Errorf("palcombat: physical attack takes no object")
			}
			all := p.Role.AttackAll != 0
			if p.EquipmentEffects != nil {
				for _, slot := range p.EquipmentEffects {
					all = all || slot[4] != 0
				}
			}
			if all {
				return nil, &UnsupportedError{Feature: "attack-all equipment/role"}
			}
			if err := e.livingEnemy(a.Target); err != nil {
				return nil, err
			}
		case Defend, Flee, Pass:
			if a.ObjectID != 0 || a.Target != (Target{}) {
				return nil, fmt.Errorf("palcombat: %s takes no target or object", a.Kind)
			}
		case Magic, UseItem:
			if err := e.validateObjectAction(p.Player, a); err != nil {
				return nil, err
			}
			if a.Kind == UseItem && e.objectAt(a.ObjectID)[6]&itemConsuming != 0 {
				reserved[a.ObjectID]++
				if reserved[a.ObjectID] > e.state.Inventory[a.ObjectID] {
					return nil, fmt.Errorf("palcombat: insufficient item %d for command batch", a.ObjectID)
				}
			}
		default:
			return nil, fmt.Errorf("palcombat: unknown command %q", a.Kind)
		}
		commands[a.Actor] = a
	}
	for _, p := range e.state.Party {
		if canAct(p) {
			if _, ok := commands[p.Role.ID]; !ok {
				return nil, fmt.Errorf("palcombat: missing command for role %d", p.Role.ID)
			}
		} else {
			commands[p.Role.ID] = Action{Actor: p.Role.ID, Kind: Attack, Target: Target{Side: Enemy, Index: retarget(e.state, 0)}}
		}
		if p.Status[Confused] > 0 || p.Status[Puppet] > 0 {
			return nil, &UnsupportedError{Feature: "confusion/puppet status"}
		}
	}
	for _, x := range e.state.Enemies {
		if x.HP <= 0 {
			continue
		}
		for _, hook := range []struct {
			name  string
			entry uint16
		}{{"enemy turn-start script", x.TurnStart}, {"enemy battle-end script", x.BattleEnd}, {"enemy ready script", x.Ready}} {
			if err := e.checkScript(hook.entry, hook.name, x.ObjectID); err != nil {
				return nil, err
			}
		}
		if x.Status[Confused] > 0 || x.Status[Puppet] > 0 {
			return nil, &UnsupportedError{Feature: "enemy confusion/puppet status", ObjectID: x.ObjectID}
		}
		if x.Enemy.AttackEquivItemRate > 0 {
			id := x.Enemy.AttackEquivItem
			if int(id) >= len(e.objects) {
				return nil, fmt.Errorf("palcombat: enemy attack item %d outside table", id)
			}
			if err := e.checkScript(e.scriptEntry(e.state, id, 2), "enemy attack item", id); err != nil {
				return nil, err
			}
		}
		if x.Enemy.Magic != 0 && x.Enemy.Magic != 0xffff && x.Enemy.MagicRate > 0 {
			if err := e.validateEnemyMagic(x); err != nil {
				return nil, err
			}
		}
	}
	return commands, nil
}

func (e *Engine) livingEnemy(t Target) error {
	if t.Side != Enemy || t.Index < 0 || t.Index >= len(e.state.Enemies) || e.state.Enemies[t.Index].HP <= 0 {
		return fmt.Errorf("palcombat: target must be a living imported enemy")
	}
	return nil
}

type queueEntry struct {
	side         Side
	index, speed int
}

func (e *Engine) queue(s Snapshot, commands map[uint16]Action, r *Report) ([]queueEntry, error) {
	q := make([]queueEntry, 0, len(s.Enemies)*2+len(s.Party))
	for i, x := range s.Enemies {
		if x.HP <= 0 {
			continue
		}
		n := 1
		if x.Enemy.DualMove != 0 {
			n = 2
		}
		for k := 0; k < n; k++ {
			speed, err := e.initiative(uint16(enemyDexterity(x)), fmt.Sprintf("initiative.enemy.%d.%d", i, k), r)
			if err != nil {
				return nil, err
			}
			q = append(q, queueEntry{Enemy, i, speed})
		}
	}
	for i, p := range s.Party {
		if !canAct(p) {
			q = append(q, queueEntry{Party, i, 0})
			continue
		}
		speed := effective(p.Player, 20, p.Role.Dexterity)
		if p.Status[Haste] > 0 {
			speed *= 3
		}
		speed = min(speed, 999)
		switch commands[p.Role.ID].Kind {
		case Defend:
			speed *= 5
		case Flee:
			speed /= 2
		case UseItem:
			speed *= 3
		case Magic:
			if !e.magicOffensive(commands[p.Role.ID].ObjectID) {
				speed *= 3
			}
		}
		if dying(p) {
			speed /= 2
		}
		v, err := e.initiative(speed, fmt.Sprintf("initiative.party.%d", p.Role.ID), r)
		if err != nil {
			return nil, err
		}
		q = append(q, queueEntry{Party, i, v})
	}
	// PAL_CLASSIC's non-adjacent exchange order can reverse equal-speed
	// fighters when a faster later fighter moves forward.
	for i := range q {
		for j := i + 1; j < len(q); j++ {
			if q[j].speed > q[i].speed {
				q[i], q[j] = q[j], q[i]
			}
		}
	}
	return q, nil
}

func (e *Engine) draw(n int, reason string, r *Report) (int, error) {
	if e.randomFailure != nil {
		return 0, e.randomFailure
	}
	if n <= 0 {
		e.randomFailure = fmt.Errorf("palcombat: invalid random bound %d", n)
		return 0, e.randomFailure
	}
	v, err := e.random.Intn(n)
	if err != nil {
		r.Draws = append(r.Draws, Draw{Reason: reason, N: n, Value: v, Error: err.Error()})
		e.randomFailure = fmt.Errorf("palcombat random %s: %w", reason, err)
		return 0, e.randomFailure
	}
	if v < 0 || v >= n {
		r.Draws = append(r.Draws, Draw{Reason: reason, N: n, Value: v, Error: "outside requested range"})
		e.randomFailure = fmt.Errorf("palcombat random %s: value %d outside [0,%d)", reason, v, n)
		return 0, e.randomFailure
	}
	r.Draws = append(r.Draws, Draw{Reason: reason, N: n, Value: v})
	return v, nil
}

const fractionSteps = 1_000_000

func (e *Engine) initiative(speed uint16, reason string, r *Report) (int, error) {
	v, err := e.draw(fractionSteps+1, reason, r)
	if err != nil {
		return 0, err
	}
	return int(int16(uint16(int64(speed) * (9*fractionSteps + 2*int64(v)) / (10 * fractionSteps)))), nil
}

func (e *Engine) playerAction(s *Snapshot, i int, a Action, r *Report) error {
	p := &s.Party[i]
	if p.Status[Confused] > 0 || p.Status[Puppet] > 0 {
		return &UnsupportedError{Feature: "confusion/puppet status"}
	}
	if a.Kind == Magic {
		m, err := e.magicSpec(a.ObjectID)
		if err != nil {
			return err
		}
		known := false
		for _, id := range p.Role.Magics {
			known = known || id == a.ObjectID
		}
		if !known || p.Status[Silence] > 0 || p.Role.MP < m.CostMP {
			if e.magicOffensive(a.ObjectID) {
				a.Kind = Attack
				a.Target = Target{Side: Enemy, Index: retarget(*s, a.Target.Index)}
			} else {
				a.Kind = Defend
				a.Target = Target{}
			}
			a.ObjectID = 0
			r.Events = append(r.Events, Event{Kind: "action_fallback", Actor: Party, ActorIndex: i})
		}
	}
	if a.Kind == UseItem && s.Inventory[a.ObjectID] == 0 {
		a.Kind = Defend
		a.Target = Target{}
		a.ObjectID = 0
		r.Events = append(r.Events, Event{Kind: "action_fallback", Actor: Party, ActorIndex: i})
	}
	switch a.Kind {
	case Attack:
		all := p.Role.AttackAll != 0
		if p.EquipmentEffects != nil {
			for _, slot := range p.EquipmentEffects {
				all = all || slot[4] != 0
			}
		}
		if all {
			return &UnsupportedError{Feature: "attack-all equipment/role"}
		}
		target := a.Target.Index
		if target < 0 || target >= len(s.Enemies) || s.Enemies[target].HP <= 0 {
			target = retarget(*s, target)
			if target < 0 {
				return nil
			}
		}
		n := 1
		if p.Status[DualAttack] > 0 {
			n = 2
		}
		for k := 0; k < n; k++ {
			bonus, err := e.draw(2, "attack.damage_bonus", r)
			if err != nil {
				return err
			}
			damage := int16(PhysicalDamage(effective(p.Player, 17, p.Role.Attack), enemyDefense(s.Enemies[target]), s.Enemies[target].Enemy.PhysicalResistance) + 1 + bonus)
			critical, err := e.draw(6, "attack.critical", r)
			if err != nil {
				return err
			}
			crit := critical == 0 || p.Status[Bravery] > 0
			if crit {
				damage *= 3
			}
			if p.Role.ID == 0 {
				v, err := e.draw(12, "attack.role0_bonus", r)
				if err != nil {
					return err
				}
				if v == 0 {
					damage *= 2
					crit = true
				}
			}
			v, err := e.draw(fractionSteps+1, "attack.variance", r)
			if err != nil {
				return err
			}
			damage = max(1, int16(int64(damage)*(8*fractionSteps+int64(v))/(8*fractionSteps)))
			e.damageEnemy(s, target, int(damage), i, Attack, 0, crit, r)
		}
		p.Counters.Attack++
		v, err := e.draw(2, "attack.health_experience", r)
		if err != nil {
			return err
		}
		p.Counters.Health += uint32(v + 2)
	case Defend:
		p.Defending = true
		p.Counters.Defense += 2
		r.Events = append(r.Events, Event{Kind: string(Defend), Actor: Party, ActorIndex: i})
	case Pass:
		r.Events = append(r.Events, Event{Kind: string(Pass), Actor: Party, ActorIndex: i})
	case Flee:
		var total uint16
		for _, x := range s.Enemies {
			if x.HP > 0 {
				total += x.Enemy.Dexterity + (x.Enemy.Level+6)*4
			}
		}
		if int16(total) < 0 {
			total = 0
		}
		v, err := e.draw(int(total)+1, "flee.check", r)
		if err != nil {
			return err
		}
		ok := !e.boss && int(effective(p.Player, 21, p.Role.FleeRate)) >= v
		if ok {
			s.Outcome = Escaped
			r.Events = append(r.Events, Event{Kind: "escaped", Actor: Party, ActorIndex: i})
		} else {
			p.Counters.Flee += 2
			r.Events = append(r.Events, Event{Kind: "flee_failed", Actor: Party, ActorIndex: i})
		}
	case Magic, UseItem:
		return e.objectAction(s, i, a, r)
	}
	return nil
}

func retarget(s Snapshot, begin int) int {
	if len(s.Enemies) == 0 {
		return -1
	}
	if begin < 0 || begin >= len(s.Enemies) {
		begin = 0
	}
	for n := 0; n < len(s.Enemies); n++ {
		i := (begin + n) % len(s.Enemies)
		if s.Enemies[i].HP > 0 {
			return i
		}
	}
	return -1
}

func (e *Engine) enemyAction(s *Snapshot, i int, r *Report) error {
	before := make([]uint16, len(s.Party))
	for j, p := range s.Party {
		before[j] = p.Role.HP
	}
	if err := e.enemyPerform(s, i, r); err != nil {
		return err
	}
	return e.checkPlayers(s, before, r)
}

func (e *Engine) enemyPerform(s *Snapshot, i int, r *Report) error {
	x := s.Enemies[i]
	if x.Status[Confused] > 0 || x.Status[Puppet] > 0 {
		return &UnsupportedError{Feature: "enemy confusion/puppet status", ObjectID: x.ObjectID}
	}
	target := -1
	for attempts := 0; attempts < 65536; attempts++ {
		v, err := e.draw(len(s.Party), "enemy.target", r)
		if err != nil {
			return err
		}
		if s.Party[v].Role.HP > 0 {
			target = v
			break
		}
	}
	if target < 0 {
		return fmt.Errorf("palcombat: random target rejection limit reached")
	}
	if x.Status[Sleep] > 0 || x.Status[Paralyzed] > 0 {
		r.Events = append(r.Events, Event{Kind: "disabled", Actor: Enemy, ActorIndex: i})
		return nil
	}
	var v int
	var err error
	if x.Enemy.Magic != 0 {
		v, err = e.draw(10, "enemy.magic_choice", r)
		if err != nil {
			return err
		}
		if v < int(x.Enemy.MagicRate) && x.Status[Silence] == 0 {
			if x.Enemy.Magic == 0xffff {
				r.Events = append(r.Events, Event{Kind: "enemy_magic_noop", Actor: Enemy, ActorIndex: i})
				return nil
			}
			return e.enemyMagic(s, i, target, r)
		}
	}
	p := &s.Party[target]
	v, err = e.draw(17, "enemy.auto_defend", r)
	if err != nil {
		return err
	}
	evaded := v >= 10
	if evaded && (dying(*p) || p.Status[Sleep] > 0 || p.Status[Paralyzed] > 0) {
		for j, cover := range s.Party {
			if cover.Role.ID == p.Role.CoveredBy && j != target && cover.Role.HP > 0 && !dying(cover) && canAct(cover) && cover.Status[Confused] == 0 {
				r.Events = append(r.Events, Event{Kind: "covered", Actor: Party, ActorIndex: j, Target: Party, TargetIndex: target})
				return nil
			}
		}
	}
	if p.Status[Sleep] > 0 || p.Status[Paralyzed] > 0 {
		evaded = false
	}
	if evaded {
		r.Events = append(r.Events, Event{Kind: "evaded", Actor: Party, ActorIndex: target})
		return nil
	}
	strength, err := e.draw(3, "enemy.attack_bonus", r)
	if err != nil {
		return err
	}
	defense := effective(p.Player, 19, p.Role.Defense)
	if p.Defending {
		defense *= 2
	}
	damage := int16(PhysicalDamage(enemyAttack(x)+uint16(strength), defense, 2))
	v, err = e.draw(2, "enemy.damage_bonus", r)
	if err != nil {
		return err
	}
	damage += int16(v)
	if p.Status[Protect] > 0 {
		damage /= 2
	}
	damage = max(1, damage)
	if err = e.damagePlayer(s, target, int(damage), i, Attack, 0, r); err != nil {
		return err
	}
	v, err = e.draw(10, "enemy.attack_item_choice", r)
	if err != nil {
		return err
	}
	if int(x.Enemy.AttackEquivItemRate) >= v+1 {
		v, err = e.draw(100, "enemy.attack_item_resistance", r)
		if err != nil {
			return err
		}
		if int(min(uint16(100), effective(p.Player, 22, p.Role.PoisonResistance))) < v+1 {
			if _, err = e.runObjectScript(s, x.Enemy.AttackEquivItem, 2, "enemy attack item", Target{Side: Party, Index: target}, r); err != nil {
				return err
			}
		}
	}
	return nil
}

func (e *Engine) damageEnemy(s *Snapshot, target, damage, actor int, kind Kind, object uint16, critical bool, r *Report) {
	x := &s.Enemies[target]
	amount := min(max(0, x.HP), damage)
	x.HP -= amount
	r.Events = append(r.Events, Event{Kind: string(kind), Actor: Party, ActorIndex: actor, Target: Enemy, TargetIndex: target, ObjectID: object, HP: -amount, Critical: critical})
}

func (e *Engine) damagePlayer(s *Snapshot, target, damage, actor int, kind Kind, object uint16, r *Report) error {
	p := &s.Party[target]
	old := p.Role.HP
	amount := min(int(old), max(0, damage))
	p.Role.HP -= uint16(amount)
	r.Events = append(r.Events, Event{Kind: string(kind), Actor: Enemy, ActorIndex: actor, Target: Party, TargetIndex: target, ObjectID: object, HP: -amount})
	return nil
}

func (e *Engine) checkPlayers(s *Snapshot, before []uint16, r *Report) error {
	for i, p := range s.Party {
		if before[i] == 0 || p.Role.HP != 0 {
			continue
		}
		for j, cover := range s.Party {
			if cover.Role.ID == p.Role.CoveredBy && canAct(cover) && cover.Status[Confused] == 0 && e.scriptEntry(*s, cover.Role.NameIndex, 2) != 0 {
				_, err := e.runObjectScript(s, cover.Role.NameIndex, 2, "friend death", Target{Side: Party, Index: j}, r)
				return err
			}
		}
	}
	for i, p := range s.Party {
		if p.Role.HP == 0 || p.Role.HP >= before[i] || !dying(p) || int(before[i]) < int(p.Role.MaxHP)/5 || p.Status[Sleep] > 0 || p.Status[Confused] > 0 {
			continue
		}
		for _, cover := range s.Party {
			if cover.Role.ID == p.Role.CoveredBy && canAct(cover) && cover.Status[Confused] == 0 {
				_, err := e.runObjectScript(s, p.Role.NameIndex, 3, "player dying", Target{Side: Party, Index: i}, r)
				return err
			}
		}
	}
	return nil
}

func (e *Engine) collect(s *Snapshot, r *Report) {
	if s.Outcome != Ongoing {
		return
	}
	alive := false
	for i := range s.Enemies {
		x := &s.Enemies[i]
		if x.HP > 0 {
			alive = true
		} else if !x.Rewarded {
			x.Rewarded = true
			s.Earned.Experience += uint32(x.Enemy.Exp)
			s.Earned.Cash += uint32(x.Enemy.Cash)
			r.Events = append(r.Events, Event{Kind: "enemy_defeated", Actor: Enemy, ActorIndex: i})
		}
	}
	if s.Outcome == Escaped {
		return
	}
	if !alive {
		s.Outcome = Victory
		return
	}
	for _, p := range s.Party {
		if p.Role.HP > 0 {
			return
		}
	}
	s.Outcome = Defeat
}
