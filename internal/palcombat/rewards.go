package palcombat

import (
	"fmt"

	"github.com/HiChen85/pal--reverse--learn/internal/paldata"
)

func (e *Engine) threshold(level uint16) (uint32, error) {
	if !e.tables.HasBattleTables {
		return 0, &UnsupportedError{Feature: "victory without imported level-up tables"}
	}
	n := e.tables.LevelUpExp[min(level, paldata.MaxLevel)]
	if n == 0 {
		return 0, fmt.Errorf("palcombat: zero experience threshold for level %d", level)
	}
	return uint32(n), nil
}

func (e *Engine) applyRewards(s *Snapshot, r *Report) error {
	if s.Earned.Applied {
		return fmt.Errorf("palcombat: rewards already applied")
	}
	s.Cash += s.Earned.Cash
	s.Earned.ByRole = map[uint16]uint32{}
	for i := range s.Party {
		p := &s.Party[i]
		if p.Role.HP == 0 {
			continue
		}
		exp := p.Experience + s.Earned.Experience
		leveled := false
		for {
			n, err := e.threshold(p.Role.Level)
			if err != nil {
				return err
			}
			if exp < n {
				break
			}
			exp -= n
			if p.Role.Level < paldata.MaxLevel {
				if err = e.levelUp(p, r); err != nil {
					return err
				}
				leveled = true
			} else {
				exp %= n
				break
			}
		}
		p.Experience = uint32(uint16(exp))
		s.Earned.ByRole[p.Role.ID] = s.Earned.Experience
		if p.HiddenExperience == nil {
			r.Limitations = appendUnique(r.Limitations, "hidden experience growth omitted: saved hidden tracks were not supplied")
		} else if err := e.hiddenGrowth(p, s.Earned.Experience, r); err != nil {
			return err
		}
		if leveled {
			p.Role.HP = p.Role.MaxHP
			p.Role.MP = p.Role.MaxMP
		}
		if p.Role.ID < paldata.PlayableRoleCount {
			for _, row := range e.tables.LevelUpMagics {
				entry := row[p.Role.ID]
				if entry.Magic == 0 || entry.Level > p.Role.Level {
					continue
				}
				known := false
				for _, id := range p.Role.Magics {
					known = known || id == entry.Magic
				}
				if known {
					continue
				}
				for j, id := range p.Role.Magics {
					if id == 0 {
						p.Role.Magics[j] = entry.Magic
						r.Events = append(r.Events, Event{Kind: "learned_magic", Actor: Party, ActorIndex: i, ObjectID: entry.Magic})
						break
					}
				}
			}
		} else {
			r.Limitations = appendUnique(r.Limitations, "role five has no imported level-up magic column")
		}
	}
	for slot := 0; slot <= s.MaxEnemySlot; slot++ {
		i := enemyIndexAtSlot(s, slot)
		if i < 0 {
			if _, err := e.runScript(s, ScriptRequest{Hook: "enemy battle end", Invoker: uint16(slot), Target: Target{Side: Enemy, Index: -1}}, r, nil); err != nil {
				return err
			}
			continue
		}
		// Battle-end hooks are intentionally unfiltered: defeat clears the
		// active object ID but leaves this continuation in the original slot.
		x := &s.Enemies[i]
		if _, err := e.runScript(s, ScriptRequest{Hook: "enemy battle end", ObjectID: x.ObjectID, Entry: x.BattleEnd, Invoker: uint16(x.Slot), Target: Target{Side: Enemy, Index: i}}, r, nil); err != nil {
			return err
		}
	}
	for i := range s.Party {
		p := &s.Party[i]
		p.Role.HP += (p.Role.MaxHP - p.Role.HP) / 2
		p.Role.MP += (p.Role.MaxMP - p.Role.MP) / 2
	}
	s.Earned.Applied = true
	return nil
}

func (e *Engine) levelUp(p *PlayerState, r *Report) error {
	fields := []*uint16{&p.Role.MaxHP, &p.Role.MaxMP, &p.Role.Attack, &p.Role.Magic, &p.Role.Defense, &p.Role.Dexterity}
	bounds := []int{8, 6, 2, 2, 2, 2}
	base := []uint16{10, 8, 4, 4, 2, 2}
	for i, field := range fields {
		v, err := e.draw(bounds[i], fmt.Sprintf("level_up.%d.%d", p.Role.ID, i), r)
		if err != nil {
			return err
		}
		*field = min(uint16(*field+base[i]+uint16(v)), 999)
	}
	p.Role.FleeRate = min(uint16(p.Role.FleeRate+2), 999)
	p.Role.Level++
	r.Events = append(r.Events, Event{Kind: "level_up", Actor: Party, ActorIndex: roleIndex(e.state, p.Role.ID)})
	return nil
}

func roleIndex(s Snapshot, id uint16) int {
	for i, p := range s.Party {
		if p.Role.ID == id {
			return i
		}
	}
	return -1
}

func (e *Engine) hiddenGrowth(p *PlayerState, gained uint32, r *Report) error {
	h := p.HiddenExperience
	c := p.Counters
	tracks := []*paldata.Experience{&h.Health, &h.Magic, &h.Attack, &h.MagicPower, &h.Defense, &h.Dexterity, &h.Flee}
	counts := []uint32{c.Health, c.Magic, c.Attack, c.MagicPower, c.Defense, c.Dexterity, c.Flee}
	stats := []*uint16{&p.Role.MaxHP, &p.Role.MaxMP, &p.Role.Attack, &p.Role.Magic, &p.Role.Defense, &p.Role.Dexterity, &p.Role.FleeRate}
	var total uint32
	for _, n := range counts {
		total += n
	}
	if total == 0 {
		return nil
	}
	for i, track := range tracks {
		track.Count = uint16(counts[i])
		track.Level = min(track.Level, paldata.MaxLevel)
		exp := uint32(track.Exp) + (gained*counts[i]/total)*2
		for {
			n, err := e.threshold(track.Level)
			if err != nil {
				return err
			}
			if exp < n {
				break
			}
			exp -= n
			v, err := e.draw(2, fmt.Sprintf("hidden_experience.%d.%d", p.Role.ID, i), r)
			if err != nil {
				return err
			}
			*stats[i] += uint16(1 + v)
			if track.Level < paldata.MaxLevel {
				track.Level++
			}
		}
		track.Exp = uint16(exp)
	}
	return nil
}

func appendUnique(s []string, v string) []string {
	for _, x := range s {
		if x == v {
			return s
		}
	}
	return append(s, v)
}
