package palcombat

import (
	"reflect"
	"testing"

	"github.com/HiChen85/pal--reverse--learn/internal/paldata"
)

type effectCallbacks struct {
	run                func(ScriptRequest, *Snapshot) (ScriptResult, error)
	commits, rollbacks int
}
type effectTransaction struct{ owner *effectCallbacks }

func (c *effectCallbacks) Begin(Snapshot) (ScriptTransaction, error) {
	return &effectTransaction{c}, nil
}
func (t *effectTransaction) Run(q ScriptRequest, s *Snapshot, _ RandomSource) (ScriptResult, error) {
	return t.owner.run(q, s)
}
func (t *effectTransaction) Commit(Snapshot) error { t.owner.commits++; return nil }
func (t *effectTransaction) Rollback()             { t.owner.rollbacks++ }

func TestExplicitScriptOutcomesEndWithoutInventingVictory(t *testing.T) {
	for _, outcome := range []Outcome{Defeat, Terminated, Escaped} {
		t.Run(string(outcome), func(t *testing.T) {
			c := fixture()
			c.Game.Objects[10][2] = 1
			cb := &effectCallbacks{run: func(q ScriptRequest, s *Snapshot) (ScriptResult, error) {
				return ScriptResult{NextEntry: q.Entry, Success: true, ForcedOutcome: &outcome}, nil
			}}
			c.Callbacks = cb
			e := create(t, c)
			r, err := e.Submit(command(Defend))
			if err != nil || !r.Committed || r.Outcome != outcome || e.Snapshot().Earned.Applied || len(r.Draws) != 0 || cb.commits != 1 {
				t.Fatalf("forced ending: %+v %v", r, err)
			}
		})
	}
}

func TestPoisonOrderSelfCureAndFreshLaterSlot(t *testing.T) {
	c := fixture()
	c.Game.Scripts = make([]paldata.Instruction, 32)
	c.Party[0].Poisons[0] = Poison{ID: 12, Script: 6}
	var calls []string
	cb := &effectCallbacks{run: func(q ScriptRequest, s *Snapshot) (ScriptResult, error) {
		calls = append(calls, q.Hook)
		if q.Hook == "player poison" && q.PoisonSlot == 0 {
			s.Party[0].Poisons[0] = Poison{}
			s.Party[0].Poisons[1] = Poison{ID: 13, Script: 7}
			s.Party[0].Status[Protect] = 2
		}
		return ScriptResult{NextEntry: q.Entry + 1, Success: true}, nil
	}}
	c.Callbacks = cb
	c.Random = &RecordedRandom{Values: []int{500000, 500000, 0, 0, 0, 0, 9}}
	e := create(t, c)
	e.state.Enemies[0].Poisons[0] = Poison{ID: 14, Script: 8}
	if _, err := e.Submit(command(Defend)); err != nil {
		t.Fatal(err)
	}
	s := e.Snapshot()
	if !reflect.DeepEqual(calls, []string{"player poison", "player poison", "enemy poison"}) || s.Party[0].Poisons[0] != (Poison{Script: 7}) || s.Party[0].Poisons[1].Script != 8 || s.Enemies[0].Poisons[0].Script != 9 || s.Party[0].Status[Protect] != 1 {
		t.Fatalf("poison order/continuation: %+v calls%v", s, calls)
	}
}

func TestHidingSkipsEnemyActionsAndResumesTurnStartAtZero(t *testing.T) {
	c := fixture()
	c.HidingTime = 2
	c.Game.Objects[10][2] = 1
	cb := &effectCallbacks{run: func(q ScriptRequest, _ *Snapshot) (ScriptResult, error) {
		return ScriptResult{NextEntry: q.Entry, Success: true}, nil
	}}
	c.Callbacks = cb
	c.Random = &RecordedRandom{Values: []int{0, 0, 0, 0}}
	e := create(t, c)
	e.state.Started = true
	for round := 0; round < 2; round++ {
		r, err := e.Submit(command(Defend))
		if err != nil || len(r.Draws) != 2 || e.Snapshot().Party[0].Role.HP != 100 {
			t.Fatalf("hidden round%d: %+v %v", round, r, err)
		}
	}
	if e.Snapshot().HidingTime != 0 {
		t.Fatal("hiding countdown did not expire")
	}
}

func TestConfigurationChangesAreRoundTransactional(t *testing.T) {
	for _, fail := range []bool{false, true} {
		c := fixture()
		c.Game.Objects[10][2] = 1
		cb := &effectCallbacks{run: func(q ScriptRequest, _ *Snapshot) (ScriptResult, error) {
			o := c.Game.Objects[20]
			o[3] = 2
			return ScriptResult{NextEntry: q.Entry, Success: true, ObjectChanges: map[uint16][7]uint16{20: o}, MagicBaseDamage: map[uint16]uint16{0: 12}}, nil
		}}
		c.Callbacks = cb
		values := []int{500000, 500000, 0, 0, 0, 0, 9}
		if fail {
			values = nil
		}
		c.Random = &RecordedRandom{Values: values}
		e := create(t, c)
		before := e.Snapshot()
		r, err := e.Submit(command(Defend))
		m, merr := e.magicSpec(20)
		if merr != nil {
			t.Fatal(merr)
		}
		if fail {
			if err == nil || r.Committed || !reflect.DeepEqual(before, e.Snapshot()) || m.BaseDamage != 10 || e.scriptEntry(e.state, 20, 3) != 0 || cb.rollbacks != 1 {
				t.Fatal("failed round leaked configuration")
			}
		}
		if !fail {
			if err != nil || !r.Committed || m.BaseDamage != 12 || e.scriptEntry(e.state, 20, 3) != 2 || c.Tables.Magics[0].BaseDamage != 10 || c.Game.Objects[20][3] != 0 {
				t.Fatalf("successful configuration proposal: %+v %v", r, err)
			}
		}
	}
}
