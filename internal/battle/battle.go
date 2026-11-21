// Package battle implements an independently designed, deterministic combat
// system for story extensions. Its formulas are not PAL1 compatibility formulas.
package battle

import (
	"fmt"
	"sort"

	"github.com/HiChen85/pal--reverse--learn/internal/content"
)

type Kind string

const (
	Attack  Kind = "attack"
	Defend  Kind = "defend"
	Skill   Kind = "skill"
	UseItem Kind = "item"
)

type Outcome string

const (
	Ongoing   Outcome = "ongoing"
	Victory   Outcome = "victory"
	Defeat    Outcome = "defeat"
	SkillCost         = 3
)

type Command struct {
	ActorID  string `json:"actor_id"`
	Kind     Kind   `json:"kind"`
	TargetID string `json:"target_id,omitempty"`
	ItemID   string `json:"item_id,omitempty"`
}

type Unit struct {
	ID          string `json:"id"`
	CharacterID string `json:"character_id"`
	Name        string `json:"name"`
	HP          int    `json:"hp"`
	MP          int    `json:"mp"`
	MaxHP       int    `json:"max_hp"`
	MaxMP       int    `json:"max_mp"`
	Attack      int    `json:"attack"`
	Defense     int    `json:"defense"`
	Speed       int    `json:"speed"`
	Defending   bool   `json:"defending"`
}

type Event struct {
	Round    int    `json:"round"`
	ActorID  string `json:"actor_id"`
	TargetID string `json:"target_id,omitempty"`
	Text     string `json:"text"`
	Kind     Kind   `json:"kind"`
	Amount   int    `json:"amount"`
}

type State struct {
	Round     int            `json:"round"`
	Outcome   Outcome        `json:"outcome"`
	Turn      string         `json:"turn,omitempty"`
	Party     []Unit         `json:"party"`
	Enemies   []Unit         `json:"enemies"`
	Inventory map[string]int `json:"inventory"`
	Log       []Event        `json:"log"`
}

type fighter struct {
	unit  Unit
	party bool
}

// Battle owns all mutable combat data. It is intended for one owner, such as a
// desktop event loop; callers must serialize access if using multiple goroutines.
type Battle struct {
	units     []fighter
	order     []int
	cursor    int
	round     int
	outcome   Outcome
	inventory map[string]int
	items     map[string]content.Item
	log       []Event
}

// New creates fresh combatants and copies the inventory. Enemy turns before the
// first party turn execute immediately. Combat order uses descending speed,
// retaining input order on ties (party before enemies). No random source is used.
func New(p content.Pack, partyIDs []string, encounterID string, inventory map[string]int) (*Battle, error) {
	if len(partyIDs) == 0 || len(partyIDs) > 32 {
		return nil, fmt.Errorf("party must contain 1..32 characters")
	}
	b := &Battle{round: 1, outcome: Ongoing, inventory: make(map[string]int), items: make(map[string]content.Item)}
	characters := make(map[string]content.Character)
	for _, c := range p.Characters {
		if _, ok := characters[c.ID]; ok {
			return nil, fmt.Errorf("duplicate character %q", c.ID)
		}
		characters[c.ID] = c
	}
	seen := make(map[string]bool)
	for _, id := range partyIDs {
		c, ok := characters[id]
		if !ok || seen[id] {
			return nil, fmt.Errorf("unknown or duplicate party character %q", id)
		}
		if err := validCharacter(c); err != nil {
			return nil, err
		}
		seen[id] = true
		b.units = append(b.units, fighter{unit: newUnit("party:"+id, c), party: true})
	}
	found := false
	for _, encounter := range p.Encounters {
		if encounter.ID != encounterID {
			continue
		}
		if found {
			return nil, fmt.Errorf("duplicate encounter %q", encounterID)
		}
		found = true
		if len(encounter.Enemies) == 0 || len(encounter.Enemies) > 64 {
			return nil, fmt.Errorf("encounter must contain 1..64 enemies")
		}
		for i, c := range encounter.Enemies {
			if err := validCharacter(c); err != nil {
				return nil, err
			}
			b.units = append(b.units, fighter{unit: newUnit(fmt.Sprintf("enemy:%d:%s", i, c.ID), c)})
		}
	}
	if !found {
		return nil, fmt.Errorf("unknown encounter %q", encounterID)
	}
	for _, item := range p.Items {
		if item.ID == "" || item.HealHP < 0 || item.HealHP > 1_000_000 || item.RestoreMP < 0 || item.RestoreMP > 1_000_000 {
			return nil, fmt.Errorf("invalid item %q", item.ID)
		}
		if _, ok := b.items[item.ID]; ok {
			return nil, fmt.Errorf("duplicate item %q", item.ID)
		}
		b.items[item.ID] = item
	}
	for id, n := range inventory {
		if _, ok := b.items[id]; !ok || n < 0 || n > 1_000_000 {
			return nil, fmt.Errorf("invalid inventory entry %q", id)
		}
		b.inventory[id] = n
	}
	for i := range b.units {
		b.order = append(b.order, i)
	}
	sort.SliceStable(b.order, func(i, j int) bool { return b.units[b.order[i]].unit.Speed > b.units[b.order[j]].unit.Speed })
	b.prepareTurn()
	return b, nil
}

func validCharacter(c content.Character) error {
	if c.ID == "" || c.Name == "" || c.MaxHP <= 0 || c.MaxHP > 1_000_000 || c.MaxMP < 0 || c.MaxMP > 1_000_000 || c.Attack < 0 || c.Attack > 1_000_000 || c.Defense < 0 || c.Defense > 1_000_000 || c.Speed < 0 || c.Speed > 1_000_000 {
		return fmt.Errorf("invalid combat stats for %q", c.ID)
	}
	return nil
}

func newUnit(id string, c content.Character) Unit {
	return Unit{ID: id, CharacterID: c.ID, Name: c.Name, HP: c.MaxHP, MP: c.MaxMP, MaxHP: c.MaxHP, MaxMP: c.MaxMP, Attack: c.Attack, Defense: c.Defense, Speed: c.Speed}
}

// Act validates every condition before changing any state. On success, enemy
// turns run automatically until a living party member can act or combat ends.
// Attack and Skill target an enemy; UseItem targets a living party member.
// Defend needs no target. Defending halves incoming damage until the next turn.
func (b *Battle) Act(cmd Command) error {
	if b.outcome != Ongoing {
		return fmt.Errorf("battle has ended: %s", b.outcome)
	}
	actor := &b.units[b.order[b.cursor]]
	if !actor.party || actor.unit.ID != cmd.ActorID || actor.unit.HP <= 0 {
		return fmt.Errorf("actor %q cannot act now", cmd.ActorID)
	}
	targetIndex := -1
	for i := range b.units {
		if b.units[i].unit.ID == cmd.TargetID {
			targetIndex = i
			break
		}
	}
	var item content.Item
	switch cmd.Kind {
	case Attack, Skill:
		if targetIndex < 0 || b.units[targetIndex].party || b.units[targetIndex].unit.HP <= 0 {
			return fmt.Errorf("choose a living enemy target")
		}
		if cmd.ItemID != "" {
			return fmt.Errorf("attack does not use an item")
		}
		if cmd.Kind == Skill && actor.unit.MP < SkillCost {
			return fmt.Errorf("skill requires %d MP", SkillCost)
		}
	case Defend:
		if cmd.TargetID != "" || cmd.ItemID != "" {
			return fmt.Errorf("defend takes no target or item")
		}
	case UseItem:
		var ok bool
		item, ok = b.items[cmd.ItemID]
		if !ok || b.inventory[cmd.ItemID] <= 0 {
			return fmt.Errorf("item %q unavailable", cmd.ItemID)
		}
		if targetIndex < 0 || !b.units[targetIndex].party || b.units[targetIndex].unit.HP <= 0 {
			return fmt.Errorf("choose a living party target")
		}
		target := b.units[targetIndex].unit
		if (item.HealHP == 0 || target.HP == target.MaxHP) && (item.RestoreMP == 0 || target.MP == target.MaxMP) {
			return fmt.Errorf("item would have no effect")
		}
	default:
		return fmt.Errorf("unknown action %q", cmd.Kind)
	}

	actorID := actor.unit.ID
	switch cmd.Kind {
	case Attack, Skill:
		power := actor.unit.Attack
		if cmd.Kind == Skill {
			actor.unit.MP -= SkillCost
			power *= 2
		}
		target := &b.units[targetIndex].unit
		damage := hit(target, power)
		b.log = append(b.log, Event{Round: b.round, ActorID: actorID, TargetID: target.ID, Kind: cmd.Kind, Amount: damage, Text: fmt.Sprintf("%s 对 %s 造成 %d 伤害", actor.unit.Name, target.Name, damage)})
	case Defend:
		actor.unit.Defending = true
		b.log = append(b.log, Event{Round: b.round, ActorID: actorID, Kind: Defend, Text: actor.unit.Name + " 采取防御"})
	case UseItem:
		target := &b.units[targetIndex].unit
		oldHP, oldMP := target.HP, target.MP
		target.HP = min(target.MaxHP, target.HP+item.HealHP)
		target.MP = min(target.MaxMP, target.MP+item.RestoreMP)
		b.inventory[item.ID]--
		b.log = append(b.log, Event{Round: b.round, ActorID: actorID, TargetID: target.ID, Kind: UseItem, Amount: target.HP - oldHP, Text: fmt.Sprintf("%s 使用 %s：%s 恢复 %d HP / %d MP", actor.unit.Name, item.Name, target.Name, target.HP-oldHP, target.MP-oldMP)})
	}
	if !b.finish() {
		b.advance()
		b.prepareTurn()
	}
	return nil
}

// Damage is max(1, attack-defense/2), halved while defending (minimum 1),
// capped by current HP. Skills double attack before applying this formula.
func hit(target *Unit, power int) int {
	damage := max(1, power-target.Defense/2)
	if target.Defending {
		damage = max(1, damage/2)
	}
	damage = min(damage, target.HP)
	target.HP -= damage
	return damage
}

func (b *Battle) advance() {
	b.cursor++
	if b.cursor == len(b.order) {
		b.cursor = 0
		b.round++
	}
}

func (b *Battle) prepareTurn() {
	for b.outcome == Ongoing {
		actor := &b.units[b.order[b.cursor]]
		if actor.unit.HP <= 0 {
			b.advance()
			continue
		}
		actor.unit.Defending = false
		if actor.party {
			return
		}
		// Stable, deterministic enemy AI selects the first living party member.
		for i := range b.units {
			target := &b.units[i]
			if target.party && target.unit.HP > 0 {
				damage := hit(&target.unit, actor.unit.Attack)
				b.log = append(b.log, Event{Round: b.round, ActorID: actor.unit.ID, TargetID: target.unit.ID, Kind: Attack, Amount: damage, Text: fmt.Sprintf("%s 对 %s 造成 %d 伤害", actor.unit.Name, target.unit.Name, damage)})
				break
			}
		}
		if b.finish() {
			return
		}
		b.advance()
	}
}

func (b *Battle) finish() bool {
	party, enemies := false, false
	for _, u := range b.units {
		if u.unit.HP > 0 {
			if u.party {
				party = true
			} else {
				enemies = true
			}
		}
	}
	if !party {
		b.outcome = Defeat
	} else if !enemies {
		b.outcome = Victory
	}
	return b.outcome != Ongoing
}

// Snapshot returns independent copies, including the inventory and event log.
func (b *Battle) Snapshot() State {
	s := State{Round: b.round, Outcome: b.outcome, Inventory: make(map[string]int), Log: append([]Event(nil), b.log...)}
	if b.outcome == Ongoing {
		s.Turn = b.units[b.order[b.cursor]].unit.ID
	}
	for _, u := range b.units {
		if u.party {
			s.Party = append(s.Party, u.unit)
		} else {
			s.Enemies = append(s.Enemies, u.unit)
		}
	}
	for id, n := range b.inventory {
		s.Inventory[id] = n
	}
	return s
}
