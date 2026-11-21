package palcombat

import (
	"errors"
	"reflect"
	"slices"
	"testing"
)

func TestPlayerMagicUsesSignedBaseAfterScripts(t *testing.T) {
	for _, tc := range []struct {
		name         string
		base, update uint16
		write        bool
		formulaHP    int
	}{
		{name: "zero", base: 0},
		{name: "signed minimum", base: 0x8000},
		{name: "negative becomes positive", base: 0xffff, update: 7, write: true, formulaHP: 13},
		{name: "positive becomes negative", base: 14, update: 0xffff, write: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := fixture()
			c.HidingTime = 1
			c.Tables.Magics[0].BaseDamage, c.Tables.Magics[0].Elemental = tc.base, 0
			if !tc.write {
				c.Tables.Battlefields = nil
			}
			c.Game.Objects[20][3], c.Game.Objects[20][2] = 1, 2
			var calls []string
			c.Callbacks = &effectCallbacks{run: func(q ScriptRequest, s *Snapshot) (ScriptResult, error) {
				calls = append(calls, q.Hook)
				if q.Actor != Party {
					t.Fatalf("unexpected player magic request %+v", q)
				}
				out := ScriptResult{NextEntry: q.Entry, Success: true}
				if q.Hook == "magic success" {
					s.Enemies[0].HP -= 3
					if tc.write {
						out.MagicBaseDamage = map[uint16]uint16{0: tc.update}
					}
				}
				return out, nil
			}}
			values := []int{0, 0, 1}
			wantDraws := []string{"magic.magic_experience"}
			if tc.formulaHP > 0 {
				values = []int{0, 0, 0, 1}
				wantDraws = append([]string{"magic.strength_variance"}, wantDraws...)
			}
			c.Random = &RecordedRandom{Values: values}
			e := create(t, c)
			r, err := e.Submit([]Action{{Actor: 1, Kind: Magic, ObjectID: 20, Target: Target{Side: Enemy}}})
			if err != nil || !r.Committed {
				t.Fatalf("magic round failed: %+v %v", r, err)
			}
			s := e.Snapshot()
			p := s.Party[0]
			if len(r.Draws) < 2 || s.Enemies[0].HP != 147-tc.formulaHP || p.Role.MP != 18 || p.Counters != (Counters{Magic: 3, MagicPower: 1}) ||
				!slices.Equal(calls, []string{"magic use", "magic success"}) || !slices.Equal(magicDrawReasons(r.Draws[2:]), wantDraws) {
				t.Fatalf("enemy HP %d, party MP %d, counters %+v, hooks %v, draws %+v", s.Enemies[0].HP, p.Role.MP, p.Counters, calls, r.Draws)
			}
		})
	}
}

func TestEnemyMagicUsesSignedBaseAfterScripts(t *testing.T) {
	for _, tc := range []struct {
		name         string
		base, update uint16
		useOK        bool
		formulaHP    uint16
		fail         bool
	}{
		{name: "negative stays negative", base: 0x8000, update: 0xffff, useOK: true},
		{name: "success makes positive base zero", base: 9, update: 0, useOK: true},
		{name: "failed use makes negative base positive", base: 0xffff, update: 9, formulaHP: 19},
		{name: "failed formula rolls back hook effects", base: 0xffff, update: 9, formulaHP: 19, fail: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := fixture()
			c.Tables.Magics[0].BaseDamage, c.Tables.Magics[0].Elemental = tc.base, 0
			c.Tables.Enemies[0].Magic, c.Tables.Enemies[0].MagicRate = 20, 10
			c.Game.Objects[20][3], c.Game.Objects[20][2] = 1, 2
			var calls []string
			cb := &effectCallbacks{run: func(q ScriptRequest, s *Snapshot) (ScriptResult, error) {
				calls = append(calls, q.Hook)
				if q.Actor != Enemy || q.Target != (Target{Side: Party}) || q.Invoker != 1 {
					t.Fatalf("unexpected enemy magic request %+v", q)
				}
				out := ScriptResult{NextEntry: q.Entry, Success: tc.useOK}
				if q.Hook == "enemy magic use" {
					s.Party[0].Role.HP -= 3
				}
				if !tc.useOK || q.Hook == "enemy magic success" {
					out.MagicBaseDamage = map[uint16]uint16{0: tc.update}
				}
				return out, nil
			}}
			c.Callbacks = cb
			values := []int{0, 0, 0, 0, 1}
			wantDraws := []string{"enemy.target", "enemy.magic_choice", "enemy.magic_auto_defend"}
			if tc.formulaHP > 0 {
				wantDraws = append(wantDraws, "magic.strength_variance")
				if !tc.fail {
					values = append(values, 0)
				}
			}
			c.Random = &RecordedRandom{Values: values}
			e := create(t, c)
			before := e.Snapshot()
			r, err := e.Submit(command(Pass))
			s := e.Snapshot()
			wantCalls := []string{"enemy magic use"}
			if tc.useOK {
				wantCalls = append(wantCalls, "enemy magic success")
			}
			if len(r.Draws) < 2 || !slices.Equal(magicDrawReasons(r.Draws[2:]), wantDraws) || !slices.Equal(calls, wantCalls) || e.actionSide != "" {
				t.Fatalf("hooks %v, draws %+v, action side %q, error %v", calls, r.Draws, e.actionSide, err)
			}
			if tc.fail {
				if err == nil || r.Committed || !reflect.DeepEqual(s, before) || cb.rollbacks != 1 {
					t.Fatalf("failed formula committed effects: %+v %v", r, err)
				}
				return
			}
			if err != nil || !r.Committed || s.Party[0].Role.HP != 97-tc.formulaHP || s.MagicBaseDamage[0] != tc.update {
				t.Fatalf("enemy magic HP %d, base %v, report %+v, error %v", s.Party[0].Role.HP, s.MagicBaseDamage, r, err)
			}
		})
	}
}

func TestLargestSignedMagicBaseRunsFormulaAndStillRejectsNegativeEnemyDamage(t *testing.T) {
	c := fixture()
	c.Tables.Magics[0].BaseDamage, c.Tables.Magics[0].Elemental = 0x7fff, 0
	c.Tables.Enemies[0].Magic, c.Tables.Enemies[0].MagicRate = 20, 10
	c.Random = &RecordedRandom{Values: []int{0, 0, 0, 0, 1, 0}}
	e := create(t, c)
	before := e.Snapshot()
	r, err := e.Submit(command(Pass))
	var unsupported *UnsupportedError
	if !errors.As(err, &unsupported) || unsupported.Feature != "negative enemy magic damage" || r.Committed || !reflect.DeepEqual(e.Snapshot(), before) {
		t.Fatalf("overflowed formula did not fail atomically: %+v %v", r, err)
	}
}

func magicDrawReasons(draws []Draw) []string {
	reasons := make([]string, 0, len(draws))
	for _, draw := range draws {
		reasons = append(reasons, draw.Reason)
	}
	return reasons
}
