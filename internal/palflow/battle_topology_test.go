package palflow

import (
	"errors"
	"reflect"
	"testing"

	"github.com/HiChen85/pal--reverse--learn/internal/palcombat"
	"github.com/HiChen85/pal--reverse--learn/internal/paldata"
)

func TestClearedBattleEndInvokerTopologyIsExplicitlyUnsupportedAndAtomic(t *testing.T) {
	// The reference runs battle-end hooks without filtering cleared slots.
	// An explicit 9E could fill slot zero here; until that case is modeled,
	// it must remain a visible failure with no partial instruction changes.
	w := divisionWorld(0, 0)
	w.Battle.Enemies[0].Rewarded = true
	w.Battle.Outcome = palcombat.Victory
	w.Battle.MaxEnemySlot = 1
	r, err := New(battleTablesSource{runtime: battleRuntime()}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	s := State{World: w, Frames: []Frame{{PC: 10, EventID: 0}}, InputIndex: 2, ScriptSuccess: true}
	before, frames := CloneWorld(w), append([]Frame(nil), s.Frames...)
	step := TraceStep{}
	handled, err := r.evaluateBattleOps(&s, &step, ins(0x9e, 11, 1, 20), nil)
	var unsupported *palcombat.UnsupportedError
	if !handled || !errors.As(err, &unsupported) || unsupported.Feature != "enemy topology from a cleared invoking slot" {
		t.Fatalf("cleared-slot compatibility boundary was hidden: handled=%t error=%v", handled, err)
	}
	if s.World != w || !reflect.DeepEqual(s.World, before) || !reflect.DeepEqual(s.Frames, frames) || s.InputIndex != 2 || !s.ScriptSuccess || !reflect.DeepEqual(step, TraceStep{}) {
		t.Fatal("unsupported cleared-slot summon published partial state or evidence")
	}
}

func TestMoneyMagicUsesObjectMappingAndSourceIntegerGoldens(t *testing.T) {
	for _, tc := range []struct {
		cash, left uint32
		damage     uint16
	}{
		{0, 0, 0}, {1, 0, 0}, {4, 0, 1}, {4999, 0, 1999}, {5000, 0, 2000}, {5001, 1, 2000}, {0xffffffff, 0xffffec77, 2000},
	} {
		runtime := battleRuntime()
		runtime.Magics = make([]paldata.Magic, 2)
		r, _ := New(battleTablesSource{runtime: runtime}, Options{})
		w := battleWorld()
		w.Cash, w.InBattle = tc.cash, false // 0x88 also works outside combat.
		s := State{World: w, Frames: []Frame{{PC: 10}}}
		step := TraceStep{}
		_, err := r.evaluateBattleOps(&s, &step, ins(0x88, 11), nil)
		if err != nil || s.World.Cash != tc.left || s.World.MagicBaseDamage[1] != tc.damage || s.InputIndex != 0 || w.Cash != tc.cash || runtime.Magics[1].BaseDamage != 0 {
			t.Fatalf("cash %d: world%+v err%v", tc.cash, s.World, err)
		}
	}
}

func divisionWorld(slot, hp int) *WorldState {
	w := battleWorld()
	w.Battle.Enemies = w.Battle.Enemies[:1]
	x := &w.Battle.Enemies[0]
	x.Slot, x.TurnStart, x.BattleEnd, x.Ready = slot, 5, 6, 7
	x.Status[palcombat.Protect] = 4
	x.Poisons[0] = palcombat.Poison{ID: 20, Script: 8}
	setBattleEnemyHP(x, uint16(hp))
	w.Battle.MaxEnemySlot = slot
	return w
}

func TestDivisionGoldensPreserveSourceButResetClones(t *testing.T) {
	for _, tc := range []struct {
		slot, hp      int
		count         uint16
		enemies, each int
	}{
		{0, 101, 0, 2, 51}, {1, 101, 1, 2, 51}, {2, 100, 2, 3, 34}, {4, 101, 4, 5, 21},
		// FFFF uses the invoker alias and a requested denominator of 65536,
		// even though only four new slots can actually be allocated.
		{0, 101, 0xffff, 5, 1},
	} {
		w := divisionWorld(tc.slot, tc.hp)
		s, _ := battleEvaluate(t, w, uint16(tc.slot), ins(0x9c, tc.count, 20))
		if len(s.World.Battle.Enemies) != tc.enemies || s.Frames[0].PC != 11 || s.InputIndex != 0 {
			t.Fatalf("division result %+v", s)
		}
		for _, x := range s.World.Battle.Enemies {
			if x.HP != tc.each || x.TurnStart != 5 || x.BattleEnd != 6 || x.Ready != 7 || x.Enemy.HP != uint16(tc.each) || x.Enemy.Raw[11] != uint16(tc.each) {
				t.Fatalf("division field mismatch %+v", x)
			}
			if x.Slot != tc.slot && (x.Status != [palcombat.StatusCount]uint16{} || x.Poisons != [16]palcombat.Poison{}) {
				t.Fatal("clone inherited poison or status")
			}
			if x.Slot == tc.slot && (x.Status[palcombat.Protect] != 4 || x.Poisons[0].ID != 20) {
				t.Fatal("division cleared original status/poison")
			}
		}
	}
}

func TestDivisionFailureBranchesAndUnsafeAliasIsAtomic(t *testing.T) {
	for _, tc := range []struct {
		count, branch, pc uint16
		hp                int
		extra             bool
	}{
		{0, 20, 20, 1, false}, {0, 0, 11, 1, false}, {1, 20, 20, 101, false}, {0, 20, 20, 101, true},
	} {
		w := divisionWorld(0, tc.hp)
		if tc.extra {
			w.Battle.Enemies = append(w.Battle.Enemies, palcombat.EnemyState{Slot: 1, ObjectID: 11, HP: 1})
		}
		s, _ := battleEvaluate(t, w, 0, ins(0x9c, tc.count, tc.branch))
		if s.Frames[0].PC != tc.pc || !reflect.DeepEqual(w.Battle, s.World.Battle) {
			t.Fatal("division failure changed state or wrong branch")
		}
	}
	r, _ := New(battleTablesSource{runtime: battleRuntime()}, Options{})
	w := divisionWorld(0, 101)
	s := State{World: w, Frames: []Frame{{PC: 10}}}
	step := TraceStep{}
	if _, err := r.evaluateBattleOps(&s, &step, ins(0x9c, 5, 20), nil); err == nil || s.World != w || s.Frames[0].PC != 10 {
		t.Fatal("out-of-capacity alias was not rejected atomically")
	}
}

func TestSummonUsesExtentBaselineAndClearsReusedSlot(t *testing.T) {
	w := divisionWorld(0, 30)
	w.Battle.MaxEnemySlot = 2
	w.Battle.Enemies = append(w.Battle.Enemies, palcombat.EnemyState{Slot: 1, ObjectID: 10, HP: 0, Rewarded: true, Ready: 99, Status: [palcombat.StatusCount]uint16{3}, Poisons: [16]palcombat.Poison{{ID: 20}}})
	w.Objects[11] = [7]uint16{0: 1, 2: 7, 3: 8, 4: 9}
	s, _ := battleEvaluate(t, w, 0, ins(0x9e, 11, 2, 20))
	if len(s.World.Battle.Enemies) != 3 || s.World.Battle.MaxEnemySlot != 2 {
		t.Fatal("summon changed slot extent")
	}
	for _, x := range s.World.Battle.Enemies {
		if x.Slot == 0 {
			continue
		}
		if x.ObjectID != 11 || x.HP != 60 || x.TurnStart != 7 || x.BattleEnd != 8 || x.Ready != 9 || x.Rewarded || x.Status != [palcombat.StatusCount]uint16{} || x.Poisons != [16]palcombat.Poison{} {
			t.Fatalf("summon inheritance %+v", x)
		}
	}
	for _, count := range []uint16{0, 0x8000, 0xffff} {
		s, _ := battleEvaluate(t, w, 0, ins(0x9e, 0xffff, count, 20))
		if len(s.World.Battle.Enemies) != 2 || s.World.Battle.Enemies[1].HP != 100 || s.World.Battle.Enemies[1].ObjectID != 10 {
			t.Fatal("default self-summon did not import fresh baseline")
		}
	}
	w.Battle.MaxEnemySlot = 0
	w.Battle.Enemies = w.Battle.Enemies[:1]
	s, _ = battleEvaluate(t, w, 0, ins(0x9e, 11, 1, 20))
	if s.Frames[0].PC != 20 || len(s.World.Battle.Enemies) != 1 {
		t.Fatal("summon expanded beyond original extent")
	}
}

func TestTransformPreservesRuntimeFieldsAndBlockedOperationsDoNotValidateTarget(t *testing.T) {
	w := divisionWorld(0, 133)
	s, _ := battleEvaluate(t, w, 0, ins(0x9f, 11))
	x := s.World.Battle.Enemies[0]
	if x.ObjectID != 11 || x.Enemy.ID != 1 || x.HP != 133 || x.Enemy.HP != 133 || x.TurnStart != 5 || x.BattleEnd != 6 || x.Ready != 7 || x.Status != w.Battle.Enemies[0].Status || x.Poisons != w.Battle.Enemies[0].Poisons {
		t.Fatalf("transform inheritance %+v", x)
	}
	for _, status := range []int{-1, palcombat.Sleep, palcombat.Paralyzed, palcombat.Confused} {
		w := divisionWorld(0, 100)
		w.Battle.MaxEnemySlot = 1
		if status < 0 {
			w.BattleHidingTime = 1
		} else {
			w.Battle.Enemies[0].Status[status] = 2
		}
		for _, inst := range []Instruction{ins(0x9e, 999, 1, 20), ins(0x9f, 999)} {
			s, _ := battleEvaluate(t, w, 0, inst)
			if !reflect.DeepEqual(w.Battle, s.World.Battle) {
				t.Fatal("blocked operation mutated enemy state")
			}
		}
	}
}

func TestDefeatedRecordUsesClearedObjectForStatusBaselineAndKindComparison(t *testing.T) {
	w := battleWorld()
	w.Objects[0] = [7]uint16{0: 1, 1: 0}
	x := &w.Battle.Enemies[0] // slot 3, historical object 10/resistance 5
	x.Rewarded = true
	setBattleEnemyHP(x, 40)
	s, _ := battleEvaluate(t, w, 3, ins(0x2e, palcombat.Sleep, 3, 20), Input{Kind: InputRandom, Value: 1})
	if s.Frames[0].PC != 11 || s.World.Battle.Enemies[0].Status[palcombat.Sleep] != 3 || s.World.Battle.Enemies[0].ObjectID != 10 {
		t.Fatal("single-slot status used a defeated record's historical resistance")
	}
	s, _ = battleEvaluate(t, w, 3, ins(0x64, 50, 20))
	if s.Frames[0].PC != 20 {
		t.Fatal("defeated baseline must use object zero's 60 HP, not historical object's 100 HP")
	}
	// Absent runtime slots 0 and 2 also have object zero in the fixed array.
	s, _ = battleEvaluate(t, w, 3, ins(0x91, 20))
	if s.Frames[0].PC != 20 {
		t.Fatal("empty-slot kind comparison omitted implicit zero slots")
	}
	w = battleWorld()
	w.Battle.Enemies[1].ObjectID, w.Battle.Enemies[1].Rewarded = 10, true
	s, _ = battleEvaluate(t, w, 3, ins(0x91, 20))
	if s.Frames[0].PC != 11 {
		t.Fatal("earlier defeated record counted as an active enemy of its historical kind")
	}
}
