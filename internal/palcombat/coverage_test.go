package palcombat

import (
	"reflect"
	"testing"
)

func TestRecoveredMemberGetsClassicZeroSpeedAttack(t *testing.T) {
	c := fixture()
	c.Party[0].Status[Sleep] = 1
	c.Game.Objects[10][4] = 1
	c.Callbacks = &fakeCallbacks{run: func(q ScriptRequest, s *Snapshot, r RandomSource) (ScriptResult, error) {
		s.Party[0].Status[Sleep] = 0
		return ScriptResult{Success: true, NextEntry: 0}, nil
	}}
	c.Random = &RecordedRandom{Values: []int{500000, 0, 0, 0, 0, 9, 0, 1, 0, 0}}
	e := create(t, c)
	r, err := e.Submit(nil)
	if err != nil {
		t.Fatal(err)
	}
	s := e.Snapshot()
	if s.Party[0].Role.HP != 70 || s.Enemies[0].HP != 126 || len(r.Draws) != 10 || s.Party[0].Counters.Attack != 1 {
		t.Fatalf("same-round recovery differs %+v %+v", s, r)
	}
}

func TestEnemyMagicDefendGolden(t *testing.T) {
	c := fixture()
	c.Tables.Enemies[0].Magic = 20
	c.Tables.Enemies[0].MagicRate = 10
	c.Random = &RecordedRandom{Values: []int{500000, 500000, 0, 0, 0, 0}}
	e := create(t, c)
	r, err := e.Submit(command(Defend))
	if err != nil {
		t.Fatal(err)
	}
	s := e.Snapshot()
	if s.Party[0].Role.HP != 92 || s.Party[0].Role.MP != 20 || len(r.Draws) != 6 {
		t.Fatalf("enemy magic guard divisor differs %+v %+v", s, r)
	}
}

func TestSilencedEnemyStillConsumesMagicChoice(t *testing.T) {
	c := fixture()
	c.Tables.Enemies[0].Magic = 20
	c.Tables.Enemies[0].MagicRate = 10
	c.Random = &RecordedRandom{Values: []int{500000, 500000, 0, 0, 10}}
	e := create(t, c)
	e.state.Enemies[0].Status[Silence] = 1
	r, err := e.Submit(command(Pass))
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Draws) != 5 || r.Draws[3].Reason != "enemy.magic_choice" || e.Snapshot().Party[0].Role.HP != 100 {
		t.Fatalf("short-circuit random order differs %+v", r)
	}
}

func TestDeadMemberDoesNotEarnExperience(t *testing.T) {
	c := fixture()
	c.Tables.Enemies[0].HP = 1
	c.Tables.Enemies[0].Dexterity = 100
	c.Party[0].Role.HP = 1
	p := c.Party[0]
	p.Role.ID = 2
	p.Role.NameIndex = 2
	p.Role.HP = 100
	c.Party = append(c.Party, p)
	c.Random = &RecordedRandom{Values: []int{500000, 500000, 500000, 0, 0, 0, 0, 9, 0, 1, 0, 0}}
	e := create(t, c)
	r, err := e.Submit([]Action{{Actor: 1, Kind: Pass}, {Actor: 2, Kind: Attack, Target: Target{Side: Enemy}}})
	if err != nil {
		t.Fatal(err)
	}
	s := e.Snapshot()
	if s.Outcome != Victory || s.Party[0].Experience != 0 || s.Party[1].Experience != 5 || s.Party[0].Role.HP != 50 || len(r.Rewards.ByRole) != 1 || r.Rewards.ByRole[2] != 5 {
		t.Fatalf("living-member experience/half-recovery differs %+v %+v", s, r)
	}
}

func TestInitiativeExchangeTieGolden(t *testing.T) {
	c := fixture()
	c.Tables.EnemyTeams[0][1] = 10
	c.Random = &RecordedRandom{Values: []int{500000, 500000, 500000}}
	e := create(t, c)
	r := Report{}
	q, err := e.queue(e.Snapshot(), map[uint16]Action{1: {Actor: 1, Kind: Pass}}, &r)
	if err != nil {
		t.Fatal(err)
	}
	want := []queueEntry{{Party, 0, 100}, {Enemy, 1, 18}, {Enemy, 0, 18}}
	if !reflect.DeepEqual(q, want) {
		t.Fatalf("classic equal-speed exchange differs: %+v", q)
	}
}

func TestEnemyTargetRetriesDeadSlots(t *testing.T) {
	c := fixture()
	p := c.Party[0]
	p.Role.ID = 2
	c.Party = append(c.Party, p)
	p.Role.ID = 3
	c.Party = append(c.Party, p)
	c.Random = &RecordedRandom{Values: []int{0, 2, 10}}
	e := create(t, c)
	s := e.Snapshot()
	s.Party[0].Role.HP = 0
	r := Report{}
	if err := e.enemyAction(&s, 0, &r); err != nil {
		t.Fatal(err)
	}
	if len(r.Draws) != 3 || r.Draws[0].N != 3 || r.Draws[1].N != 3 || len(r.Events) != 1 || r.Events[0].Kind != "evaded" || r.Events[0].ActorIndex != 2 {
		t.Fatalf("dead-target rejection differs %+v", r)
	}
}

type invalidRandom struct{}

func (invalidRandom) Intn(n int) (int, error) { return n, nil }
func TestInvalidRandomValueIsRetainedAndRejected(t *testing.T) {
	c := fixture()
	c.Random = invalidRandom{}
	e := create(t, c)
	before := e.Snapshot()
	r, err := e.Submit(command(Pass))
	if err == nil || r.Committed || len(r.Draws) != 1 || r.Draws[0].Value != 1000001 || r.Draws[0].Error == "" || !reflect.DeepEqual(e.Snapshot(), before) {
		t.Fatal("out-of-range external draw was lost or committed")
	}
}

func TestVictoryRequiresImportedRewardTables(t *testing.T) {
	c := fixture()
	c.Tables.HasBattleTables = false
	c.Tables.Enemies[0].HP = 1
	c.Random = &RecordedRandom{Values: []int{500000, 500000, 0, 1, 0, 0}}
	e := create(t, c)
	before := e.Snapshot()
	r, err := e.Submit(command(Attack))
	if err == nil || r.Committed || !reflect.DeepEqual(before, e.Snapshot()) {
		t.Fatal("missing reward data produced a synthetic victory")
	}
}

func TestCallbackCannotAssignOutcomeOrSuppressRewards(t *testing.T) {
	for _, mutate := range []func(*Snapshot){func(s *Snapshot) { s.Outcome = Victory }, func(s *Snapshot) { s.Enemies[0].Rewarded = true }} {
		c := fixture()
		c.Game.Objects[10][2] = 1
		f := &fakeCallbacks{run: func(q ScriptRequest, s *Snapshot, r RandomSource) (ScriptResult, error) {
			mutate(s)
			return ScriptResult{Success: true}, nil
		}}
		c.Callbacks = f
		e := create(t, c)
		before := e.Snapshot()
		report, err := e.Submit(command(Pass))
		if err == nil || report.Committed || f.commits != 0 || f.rollbacks != 1 || !reflect.DeepEqual(before, e.Snapshot()) {
			t.Fatal("callback modified engine-owned outcome or reward state")
		}
	}
}

func TestCallbackCommitReceivesIsolatedFinalSnapshot(t *testing.T) {
	c := fixture()
	f := &fakeCallbacks{}
	c.Callbacks = f
	c.Random = &RecordedRandom{Values: []int{500000, 500000, 0, 0, 0, 0, 9}}
	e := create(t, c)
	report, err := e.Submit(command(Defend))
	if err != nil {
		t.Fatal(err)
	}
	if !report.Committed || f.committedSnapshot.Party[0].Role.HP != 86 || f.committedSnapshot.Round != 2 {
		t.Fatal("callback did not receive finalized combat state")
	}
	f.committedSnapshot.Party[0].Role.HP = 1
	f.committedSnapshot.Enemies[0].HP = 1
	if e.Snapshot().Party[0].Role.HP != 86 || e.Snapshot().Enemies[0].HP != 150 {
		t.Fatal("commit snapshot aliases committed engine state")
	}
}
