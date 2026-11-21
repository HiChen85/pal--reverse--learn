package palcombat

import (
	"reflect"
	"testing"

	"github.com/HiChen85/pal--reverse--learn/internal/paldata"
)

func TestInitialRuntimeSlotsCompactFFFFButPreserveZeroLayout(t *testing.T) {
	c := fixture()
	c.Tables.EnemyTeams[0] = [5]uint16{0xffff, 10, 0, 10, 0xffff}
	s := create(t, c).Snapshot()
	if s.MaxEnemySlot != 2 || len(s.Enemies) != 2 || s.Enemies[0].Slot != 0 || s.Enemies[1].Slot != 2 {
		t.Fatalf("initial runtime slots %+v", s.Enemies)
	}
}

func TestReadyTopologyPreservesContinuationAndDoesNotQueueNewSlots(t *testing.T) {
	for _, failAfterTopology := range []bool{false, true} {
		c := fixture()
		c.Game.Objects[10][4] = 1
		calls := 0
		cb := &effectCallbacks{run: func(q ScriptRequest, s *Snapshot) (ScriptResult, error) {
			calls++
			if q.Invoker != 0 || q.Target.Index != 0 || q.Hook != "enemy ready" {
				t.Fatalf("wrong callback context %+v", q)
			}
			copy := clone(*s)
			copy.Enemies = append(copy.Enemies, EnemyState{Slot: 1, ObjectID: 10, Enemy: c.Tables.Enemies[0], HP: 150, Ready: 1})
			return ScriptResult{Success: true, NextEntry: 2, EnemyTopology: &EnemyTopology{Enemies: copy.Enemies, MaxSlot: 1}}, nil
		}}
		c.Callbacks = cb
		values := []int{500000, 500000, 0, 0, 0, 0, 9}
		if failAfterTopology {
			values = values[:2]
		}
		tape := &RecordedRandom{Values: values}
		c.Random = tape
		e := create(t, c)
		before := e.Snapshot()
		r, err := e.Submit(command(Defend))
		if calls != 1 {
			t.Fatalf("new slot acted in existing queue: calls %d", calls)
		}
		if failAfterTopology {
			if err == nil || r.Committed || !reflect.DeepEqual(before, e.Snapshot()) || cb.rollbacks != 1 || cb.commits != 0 || tape.Index != 2 || len(r.Draws) != 3 {
				t.Fatalf("failed topology leaked %+v %v", r, err)
			}
			continue
		}
		s := e.Snapshot()
		if err != nil || !r.Committed || len(s.Enemies) != 2 || s.Enemies[0].Ready != 2 || s.Enemies[1].Ready != 1 || cb.commits != 1 || len(r.Draws) != 7 {
			t.Fatalf("continuation/queue mismatch %+v %v", s, err)
		}
	}
}

func TestTurnStartVisitsAppendedEnemiesInDynamicOriginalSlotOrder(t *testing.T) {
	c := fixture()
	c.HidingTime = 2
	c.Game.Objects[10][2] = 1
	c.Random = &RecordedRandom{Values: []int{0, 0, 0, 0}}
	var slots []uint16
	cb := &effectCallbacks{run: func(q ScriptRequest, s *Snapshot) (ScriptResult, error) {
		slots = append(slots, q.Invoker)
		if q.Invoker != 0 {
			if q.Entry != 2 || s.Enemies[q.Target.Index].Slot != int(q.Invoker) {
				t.Fatalf("new enemy callback context %+v", q)
			}
			return ScriptResult{Success: true, NextEntry: 3}, nil
		}
		copy := clone(*s)
		// Deliberately return records in a different order from fixed slots.
		for _, slot := range []int{3, 1} {
			copy.Enemies = append(copy.Enemies, EnemyState{Slot: slot, ObjectID: 10, Enemy: c.Tables.Enemies[0], HP: 150, TurnStart: 2})
		}
		return ScriptResult{Success: true, NextEntry: 4, EnemyTopology: &EnemyTopology{Enemies: copy.Enemies, MaxSlot: 3}}, nil
	}}
	c.Callbacks = cb
	e := create(t, c)
	if _, err := e.Submit(command(Defend)); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(slots, []uint16{0, 1, 3}) || e.state.Enemies[0].TurnStart != 4 || e.state.Enemies[1].TurnStart != 3 || e.state.Enemies[2].TurnStart != 3 {
		t.Fatalf("dynamic turn start order %v state%+v", slots, e.state.Enemies)
	}
}

func TestReusedDefeatedSlotKeepsPendingActionAndRewardsEachIncarnationOnce(t *testing.T) {
	c := fixture()
	c.Tables.Enemies[0].HP = 1
	c.Tables.Enemies = append(c.Tables.Enemies, paldata.Enemy{HP: 150, Attack: 1, PhysicalResistance: 2})
	c.Game.Objects[11] = [7]uint16{0: 1, 4: 1}
	c.Tables.EnemyTeams[0] = [5]uint16{10, 11, 0xffff, 0xffff, 0xffff}
	reused := false
	cb := &effectCallbacks{run: func(q ScriptRequest, s *Snapshot) (ScriptResult, error) {
		if reused {
			return ScriptResult{NextEntry: q.Entry, Success: true}, nil
		}
		if q.Invoker != 1 || !s.Enemies[0].Rewarded || s.Earned.Experience != 5 || s.Earned.Cash != 3 {
			t.Fatalf("replacement happened before KO reward %+v", s)
		}
		reused = true
		copy := clone(*s)
		copy.Enemies[0] = EnemyState{Slot: 0, ObjectID: 10, Enemy: c.Tables.Enemies[0], HP: 1}
		return ScriptResult{NextEntry: 2, Success: true, EnemyTopology: &EnemyTopology{Enemies: copy.Enemies, MaxSlot: 1}}, nil
	}}
	c.Callbacks = cb
	// Enemy equal-speed exchange puts slot 1 before slot 0 after the faster
	// party member moves to the front. Slot 0 is killed, then reused before
	// its pre-existing queue entry executes.
	values := []int{500000, 500000, 500000, 0, 1, 0, 0, 0, 0, 0, 0, 9, 0, 0, 0, 0, 9}
	c.Random = &RecordedRandom{Values: values}
	e := create(t, c)
	r, err := e.Submit(command(Attack))
	if err != nil {
		t.Fatal(err)
	}
	attacks := 0
	for _, event := range r.Events {
		if event.Kind == "attack" && event.Actor == Enemy {
			attacks++
		}
	}
	if attacks != 2 || !reused || e.state.Enemies[0].Rewarded || e.state.Earned.Experience != 5 || e.state.Earned.Cash != 3 || e.state.Enemies[1].Ready != 2 {
		t.Fatalf("reused queue/reward %+v events%+v", e.state, r.Events)
	}
	// Kill the replacement while the other enemy keeps the fight ongoing.
	e.state.Enemies[0].HP = 0
	for round := 0; round < 2; round++ {
		e.random = &RecordedRandom{Values: []int{500000, 500000, 0, 0, 0, 0, 9}}
		if _, err := e.Submit(command(Defend)); err != nil {
			t.Fatal(err)
		}
		if e.state.Earned.Experience != 10 || e.state.Earned.Cash != 6 {
			t.Fatal("reused slot reward omitted or duplicated")
		}
	}
}

func TestTypedTopologyCannotBypassEngineOwnership(t *testing.T) {
	for _, mutate := range []func(*Snapshot){
		func(s *Snapshot) { s.Enemies[0].ObjectID = 11 },
		func(s *Snapshot) { s.MaxEnemySlot = 1 },
		func(s *Snapshot) { s.Enemies[0].Ready = 2 },
	} {
		c := fixture()
		c.Game.Objects[10][2] = 1
		cb := &effectCallbacks{run: func(_ ScriptRequest, s *Snapshot) (ScriptResult, error) {
			mutate(s)
			return ScriptResult{Success: true}, nil
		}}
		c.Callbacks = cb
		e := create(t, c)
		before := e.Snapshot()
		if r, err := e.Submit(command(Defend)); err == nil || r.Committed || !reflect.DeepEqual(before, e.Snapshot()) || cb.rollbacks != 1 || len(r.Draws) != 0 {
			t.Fatal("direct callback ownership mutation accepted")
		}
	}
}

func TestOriginalSlotOrderControlsInitiativeRetargetAndAllTargetMagic(t *testing.T) {
	c := fixture()
	c.Game.Objects[20][6] |= applyAll
	c.Random = &RecordedRandom{Values: make([]int, 16)}
	e := create(t, c)
	s := e.Snapshot()
	s.MaxEnemySlot = 4
	base := s.Enemies[0]
	s.Enemies = nil
	for _, slot := range []int{3, 0, 1, 4} {
		x := base
		x.Slot = slot
		s.Enemies = append(s.Enemies, x)
	}
	report := Report{}
	if _, err := e.queue(s, map[uint16]Action{1: {Actor: 1, Kind: Defend}}, &report); err != nil {
		t.Fatal(err)
	}
	wantReasons := []string{"initiative.enemy.1.0", "initiative.enemy.2.0", "initiative.enemy.0.0", "initiative.enemy.3.0"}
	for i, want := range wantReasons {
		if report.Draws[i].Reason != want {
			t.Fatalf("initiative draw order %+v", report.Draws)
		}
	}
	dead := clone(s)
	dead.Enemies[0].HP = 0
	if got := retarget(dead, 0); got != 3 {
		t.Fatalf("target after original slot 3: index %d, want slot4/index3", got)
	}
	report = Report{}
	if err := e.objectAction(&s, 0, Action{Actor: 1, Kind: Magic, ObjectID: 20, Target: Target{Side: All}}, &report); err != nil {
		t.Fatal(err)
	}
	var targets []int
	for _, event := range report.Events {
		if event.Kind == "magic" && event.Target == Enemy {
			targets = append(targets, event.TargetIndex)
		}
	}
	if !reflect.DeepEqual(targets, []int{1, 2, 0, 3}) {
		t.Fatalf("all-target magic did not follow original slot order: %v", targets)
	}
}

func TestRetainedDeadPoisonAndStatusTickWithoutRestoringFighterPresence(t *testing.T) {
	c := fixture()
	c.Game.Scripts = make([]paldata.Instruction, 16)
	c.Tables.EnemyTeams[0] = [5]uint16{10, 11, 0xffff, 0xffff, 0xffff}
	c.Random = &RecordedRandom{Values: []int{0, 0, 0, 0, 0, 0, 9}}
	var calls []ScriptRequest
	cb := &effectCallbacks{run: func(q ScriptRequest, _ *Snapshot) (ScriptResult, error) {
		calls = append(calls, q)
		return ScriptResult{NextEntry: q.Entry + 1, Success: true}, nil
	}}
	c.Callbacks = cb
	e := create(t, c)
	// This record was defeated during an earlier round. The separate initial
	// pre-battle pass intentionally visits even cleared slots in the source.
	e.state.Started = true
	dead := &e.state.Enemies[0]
	dead.Rewarded, dead.HP, dead.TurnStart = true, 65535, 2
	dead.Poisons[0], dead.Status[Protect] = Poison{ID: 12, Script: 8}, 2
	e.state.Earned.Experience, e.state.Earned.Cash = 5, 3
	if _, err := e.Submit(command(Defend)); err != nil {
		t.Fatal(err)
	}
	s := e.Snapshot()
	if len(calls) != 1 || calls[0].Hook != "enemy poison" || calls[0].Invoker != 0 || s.Enemies[0].Poisons[0].Script != 9 || s.Enemies[0].Status[Protect] != 1 || s.Earned.Experience != 5 || s.Earned.Cash != 3 {
		t.Fatalf("retained dead-slot poison/status handling: calls%+v state%+v", calls, s.Enemies[0])
	}
	if got := retarget(s, 0); got != 1 {
		t.Fatal("positive retained HP made a cleared slot targetable")
	}
	if _, err := e.validate([]Action{{Actor: 1, Kind: Attack, Target: Target{Side: Enemy, Index: 0}}}); err == nil {
		t.Fatal("historical enemy with positive retained HP accepted as an active target")
	}
}

func TestBattleEndCallbacksRetainDeadSlotsAndRunInOriginalSlotOrder(t *testing.T) {
	c := fixture()
	c.Tables.EnemyTeams[0] = [5]uint16{10, 11, 0xffff, 0xffff, 0xffff}
	var calls []ScriptRequest
	cb := &effectCallbacks{run: func(q ScriptRequest, _ *Snapshot) (ScriptResult, error) {
		calls = append(calls, q)
		return ScriptResult{NextEntry: 3, Success: true}, nil
	}}
	c.Callbacks = cb
	e := create(t, c)
	s := e.Snapshot()
	s.Outcome, s.Earned.Experience, s.Earned.Cash = Victory, 5, 3
	for i := range s.Enemies {
		s.Enemies[i].HP, s.Enemies[i].Rewarded, s.Enemies[i].BattleEnd = 0, true, uint16(i+1)
	}
	e.transaction = &effectTransaction{owner: cb}
	defer func() { e.transaction = nil }()
	if err := e.applyRewards(&s, &Report{}); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 2 || calls[0].Invoker != 0 || calls[1].Invoker != 1 || calls[0].Entry != 1 || calls[1].Entry != 2 || calls[0].Hook != "enemy battle end" || calls[1].Hook != "enemy battle end" || s.Enemies[0].BattleEnd != 1 || s.Enemies[1].BattleEnd != 2 {
		t.Fatalf("dead battle-end script slots were filtered or rewritten: %+v", calls)
	}
}
