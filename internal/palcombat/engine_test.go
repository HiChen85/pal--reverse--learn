package palcombat

import (
	"errors"
	"reflect"
	"testing"

	"github.com/HiChen85/pal--reverse--learn/internal/paldata"
)

func fixture() Config {
	t := &paldata.RuntimeTables{HasBattleTables: true, Enemies: []paldata.Enemy{{HP: 150, Exp: 5, Cash: 3, Attack: 10, Defense: 10, PhysicalResistance: 2}}, EnemyTeams: [][5]uint16{{10}}, Magics: []paldata.Magic{{Type: 0, CostMP: 2, BaseDamage: 10, Elemental: 1}}, Battlefields: []paldata.Battlefield{{MagicEffect: [5]int16{2}}}}
	for i := range t.LevelUpExp {
		t.LevelUpExp[i] = 1000
	}
	g := &paldata.Game{Objects: make([][7]uint16, 32), Scripts: make([]paldata.Instruction, 8)}
	g.Objects[20] = [7]uint16{6: magicInBattle | magicToEnemy}
	p := Player{Role: paldata.Role{ID: 1, NameIndex: 1, Level: 1, HP: 100, MaxHP: 100, MP: 20, MaxMP: 20, Attack: 50, Magic: 40, Defense: 20, Dexterity: 100, FleeRate: 999, Magics: [32]uint16{20}}}
	return Config{Tables: t, Game: g, Party: []Player{p}, Cash: 100, Random: &RecordedRandom{}, Inventory: map[uint16]uint16{}}
}

func create(t *testing.T, c Config) *Engine {
	t.Helper()
	e, err := New(c)
	if err != nil {
		t.Fatal(err)
	}
	return e
}
func command(k Kind) []Action {
	a := Action{Actor: 1, Kind: k}
	if k == Attack {
		a.Target = Target{Side: Enemy}
	}
	return []Action{a}
}

// Numeric oracle derived independently from the PAL_CLASSIC rules in SDLPal
// d4bf69b64d335cd680555af9df90e7ec4f3006a3. No original game records are embedded.
func TestAttackRoundGolden(t *testing.T) {
	c := fixture()
	tape := &RecordedRandom{Values: []int{500000, 500000, 0, 1, 0, 0, 0, 0, 0, 0, 9}}
	c.Random = tape
	e := create(t, c)
	r, err := e.Submit(command(Attack))
	if err != nil {
		t.Fatal(err)
	}
	s := e.Snapshot()
	if !r.Committed || r.Outcome != Ongoing || s.Round != 2 || s.Party[0].Role.HP != 70 || s.Enemies[0].HP != 126 || s.Party[0].Counters != (Counters{Attack: 1, Health: 2}) {
		t.Fatalf("unexpected round: %+v", s)
	}
	want := []Draw{{"initiative.enemy.0.0", 1000001, 500000, ""}, {"initiative.party.1", 1000001, 500000, ""}, {"attack.damage_bonus", 2, 0, ""}, {"attack.critical", 6, 1, ""}, {"attack.variance", 1000001, 0, ""}, {"attack.health_experience", 2, 0, ""}, {"enemy.target", 1, 0, ""}, {"enemy.auto_defend", 17, 0, ""}, {"enemy.attack_bonus", 3, 0, ""}, {"enemy.damage_bonus", 2, 0, ""}, {"enemy.attack_item_choice", 10, 9, ""}}
	if !reflect.DeepEqual(r.Draws, want) || tape.Index != len(want) {
		t.Fatalf("random transcript differs: %+v", r.Draws)
	}
	if s.Party[0].Role.Raw[9] != 70 {
		t.Fatal("canonical role column was not updated")
	}
}

func TestDefendAndFleeGolden(t *testing.T) {
	for _, tc := range []struct {
		name    string
		kind    Kind
		boss    bool
		values  []int
		hp      uint16
		out     Outcome
		counter Counters
	}{
		{"defend", Defend, false, []int{500000, 500000, 0, 0, 0, 0, 9}, 86, Ongoing, Counters{Defense: 2}},
		{"escape", Flee, false, []int{500000, 500000, 0}, 100, Escaped, Counters{}},
		{"boss", Flee, true, []int{500000, 500000, 0, 0, 0, 0, 0, 9}, 70, Ongoing, Counters{Flee: 2}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := fixture()
			c.Boss = tc.boss
			c.Random = &RecordedRandom{Values: tc.values}
			e := create(t, c)
			r, err := e.Submit(command(tc.kind))
			if err != nil {
				t.Fatal(err)
			}
			s := e.Snapshot()
			if s.Party[0].Role.HP != tc.hp || s.Outcome != tc.out || s.Party[0].Counters != tc.counter || !r.Committed || s.Earned.Applied {
				t.Fatalf("unexpected outcome %+v", s)
			}
		})
	}
}

func TestRandomFailureRollsBackEveryBoundary(t *testing.T) {
	values := []int{500000, 500000, 0, 1, 0, 0, 0, 0, 0, 0, 9}
	for n := range values {
		t.Run(string(rune('a'+n)), func(t *testing.T) {
			c := fixture()
			tape := &RecordedRandom{Values: append([]int(nil), values[:n]...)}
			c.Random = tape
			e := create(t, c)
			before := e.Snapshot()
			r, err := e.Submit(command(Attack))
			if err == nil || r.Committed || !reflect.DeepEqual(e.Snapshot(), before) {
				t.Fatal("failed draw committed combat state")
			}
			if tape.Index != n || len(r.Draws) != n+1 || r.Draws[n].Error == "" {
				t.Fatalf("consumed random transcript was lost: %+v", r.Draws)
			}
		})
	}
}

func TestValidationConsumesNoRandom(t *testing.T) {
	for _, a := range [][]Action{nil, {{Actor: 1, Kind: Attack, Target: Target{Side: Enemy, Index: 3}}}, {{Actor: 1, Kind: Kind("victory")}}, {{Actor: 1, Kind: Pass}, {Actor: 1, Kind: Pass}}} {
		c := fixture()
		tape := &RecordedRandom{}
		c.Random = tape
		e := create(t, c)
		before := e.Snapshot()
		r, err := e.Submit(a)
		if err == nil || len(r.Draws) > 0 || tape.Index != 0 || !reflect.DeepEqual(e.Snapshot(), before) {
			t.Fatal("invalid action changed state or consumed random")
		}
	}
}

func TestVictoryGrowthGolden(t *testing.T) {
	c := fixture()
	c.Tables.Enemies[0].HP = 1
	c.Tables.Enemies[0].Exp = 12
	c.Tables.LevelUpExp[1] = 10
	c.Tables.LevelUpMagics = [][paldata.PlayableRoleCount]paldata.LevelUpMagic{{1: {Level: 2, Magic: 21}}}
	h := &HiddenExperience{}
	for _, x := range []*paldata.Experience{&h.Health, &h.Magic, &h.Attack, &h.MagicPower, &h.Defense, &h.Dexterity, &h.Flee} {
		x.Level = 1
	}
	c.Party[0].HiddenExperience = h
	c.Random = &RecordedRandom{Values: []int{500000, 500000, 0, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0}}
	e := create(t, c)
	r, err := e.Submit(command(Attack))
	if err != nil {
		t.Fatal(err)
	}
	s := e.Snapshot()
	p := s.Party[0]
	if s.Outcome != Victory || !s.Earned.Applied || s.Cash != 103 || s.Earned.Experience != 12 || s.Earned.ByRole[1] != 12 || p.Experience != 2 || p.Role.Level != 2 || p.Role.HP != 111 || p.Role.MaxHP != 111 || p.Role.MP != 28 || p.Role.Attack != 54 || p.Role.Magic != 44 || p.Role.Defense != 22 || p.Role.Dexterity != 102 || p.Role.FleeRate != 999 || p.Role.Magics[1] != 21 || p.HiddenExperience.Health.Exp != 6 || p.HiddenExperience.Health.Level != 2 || p.HiddenExperience.Attack.Exp != 8 || len(r.Limitations) != 0 || r.StoryResult == nil || *r.StoryResult != 0 {
		t.Fatalf("growth golden differs: %+v, %+v", s, r)
	}
	before := e.Snapshot()
	if _, err = e.Submit(command(Attack)); err == nil || !reflect.DeepEqual(before, e.Snapshot()) {
		t.Fatal("ended battle applied rewards twice")
	}
}

func TestMagicRoundGolden(t *testing.T) {
	c := fixture()
	c.Tables.Enemies[0].ElementalResistance[0] = 5
	c.Random = &RecordedRandom{Values: []int{500000, 500000, 0, 0, 0, 0, 0, 0, 9}}
	e := create(t, c)
	r, err := e.Submit([]Action{{Actor: 1, Kind: Magic, ObjectID: 20, Target: Target{Side: Enemy}}})
	if err != nil {
		t.Fatal(err)
	}
	s := e.Snapshot()
	if s.Enemies[0].HP != 131 || s.Party[0].Role.MP != 18 || s.Party[0].Counters != (Counters{Magic: 2, MagicPower: 1}) || len(r.Draws) != 9 {
		t.Fatalf("magic golden differs: %+v %+v", s, r)
	}
}

type fakeCallbacks struct {
	world, commits, rollbacks int
	run                       func(ScriptRequest, *Snapshot, RandomSource) (ScriptResult, error)
	commitError               error
	committedSnapshot         Snapshot
}
type fakeTransaction struct {
	owner  *fakeCallbacks
	staged int
}

func (f *fakeCallbacks) Begin(Snapshot) (ScriptTransaction, error) {
	return &fakeTransaction{owner: f, staged: f.world}, nil
}
func (f *fakeTransaction) Run(q ScriptRequest, s *Snapshot, r RandomSource) (ScriptResult, error) {
	f.staged++
	if f.owner.run != nil {
		return f.owner.run(q, s, r)
	}
	return ScriptResult{Success: true, NextEntry: q.Entry}, nil
}
func (f *fakeTransaction) Commit(s Snapshot) error {
	if f.owner.commitError != nil {
		return f.owner.commitError
	}
	f.owner.world = f.staged
	f.owner.committedSnapshot = s
	f.owner.commits++
	return nil
}
func (f *fakeTransaction) Rollback() { f.owner.rollbacks++ }

func TestScriptTransactionRollbackAndContinuation(t *testing.T) {
	for _, tc := range []struct {
		name        string
		random      []int
		commitError error
		success     bool
	}{
		{"random_failure", nil, nil, false},
		{"commit_failure", []int{500000, 500000, 0, 0, 0, 0, 9}, errors.New("atomic commit failed"), false},
		{"commit", []int{500000, 500000, 0, 0, 0, 0, 9}, nil, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := fixture()
			c.Game.Objects[10][2] = 1
			f := &fakeCallbacks{commitError: tc.commitError, run: func(q ScriptRequest, s *Snapshot, r RandomSource) (ScriptResult, error) {
				s.Party[0].Role.MP = 10
				return ScriptResult{Success: true, NextEntry: 0, Trace: []ScriptTrace{{Entry: q.Entry, PC: q.Entry, Opcode: 0, Success: true}}}, nil
			}}
			c.Callbacks = f
			c.Random = &RecordedRandom{Values: tc.random}
			e := create(t, c)
			before := e.Snapshot()
			r, err := e.Submit(command(Defend))
			if tc.success {
				if err != nil || !r.Committed || f.world != 1 || f.commits != 1 || f.rollbacks != 0 || e.Snapshot().Enemies[0].TurnStart != 0 {
					t.Fatalf("commit failed: %+v %v %+v", r, err, f)
				}
			} else if err == nil || r.Committed || f.world != 0 || f.commits != 0 || f.rollbacks != 1 || !reflect.DeepEqual(e.Snapshot(), before) {
				t.Fatalf("transaction escaped rollback: %+v %v %+v", r, err, f)
			}
		})
	}
}

func TestScriptCannotSwallowRandomError(t *testing.T) {
	c := fixture()
	c.Game.Objects[10][2] = 1
	f := &fakeCallbacks{run: func(q ScriptRequest, s *Snapshot, r RandomSource) (ScriptResult, error) {
		_, _ = r.Intn(2)
		return ScriptResult{Success: true}, nil
	}}
	c.Callbacks = f
	e := create(t, c)
	before := e.Snapshot()
	r, err := e.Submit(command(Pass))
	if err == nil || r.Committed || f.commits != 0 || f.rollbacks != 1 || len(r.Draws) != 1 || r.Draws[0].Error == "" || !reflect.DeepEqual(before, e.Snapshot()) {
		t.Fatal("callback swallowed failed random draw")
	}
}

func TestNonzeroScriptNeedsCallback(t *testing.T) {
	c := fixture()
	c.Game.Objects[10][2] = 1
	e := create(t, c)
	r, err := e.Submit(command(Pass))
	var unsupported *UnsupportedError
	if !errors.As(err, &unsupported) || unsupported.Entry != 1 || r.Committed || len(r.Draws) != 0 {
		t.Fatalf("script silently skipped: %+v %v", r, err)
	}
}

func TestAllTargetItemScriptAndReservations(t *testing.T) {
	c := fixture()
	c.Game.Objects[21] = [7]uint16{2: 1, 6: itemUsable | itemConsuming | applyAll}
	c.Inventory[21] = 1
	c.Random = &RecordedRandom{Values: []int{500000, 500000, 0, 0, 0, 0, 9}}
	f := &fakeCallbacks{run: func(q ScriptRequest, s *Snapshot, r RandomSource) (ScriptResult, error) {
		if q.Invoker != 0xffff || q.Target.Side != All {
			t.Fatalf("all-target invoker differs %+v", q)
		}
		s.Party[0].Role.HP = 80
		return ScriptResult{Success: false, NextEntry: 2}, nil
	}}
	c.Callbacks = f
	e := create(t, c)
	r, err := e.Submit([]Action{{Actor: 1, Kind: UseItem, ObjectID: 21, Target: Target{Side: All}}})
	if err != nil {
		t.Fatal(err)
	}
	s := e.Snapshot()
	if !r.Committed || s.Inventory[21] != 0 || s.ScriptEntries[21*8+2] != 2 || s.Party[0].Role.HP != 50 {
		t.Fatalf("item consumption/continuation differs: %+v", s)
	}
	c = fixture()
	p := c.Party[0]
	p.Role.ID = 2
	c.Party = append(c.Party, p)
	c.Game.Objects[21] = [7]uint16{6: itemUsable | itemConsuming}
	c.Inventory[21] = 1
	e = create(t, c)
	a := []Action{{Actor: 1, Kind: UseItem, ObjectID: 21, Target: Target{Side: Party}}, {Actor: 2, Kind: UseItem, ObjectID: 21, Target: Target{Side: Party}}}
	r, err = e.Submit(a)
	if err == nil || len(r.Draws) != 0 {
		t.Fatal("overreserved item consumed random")
	}
}

func TestSnapshotAndInputsAreIsolated(t *testing.T) {
	c := fixture()
	c.Party[0].EquipmentEffects = &Effects{}
	c.Party[0].HiddenExperience = &HiddenExperience{}
	e := create(t, c)
	before := e.Snapshot()
	c.Game.Objects[10][0] = 9
	c.Tables.Enemies[0].HP = 1
	c.Party[0].EquipmentEffects[0][17] = 99
	c.Party[0].HiddenExperience.Health.Exp = 99
	s := e.Snapshot()
	s.Party[0].Role.HP = 1
	s.Party[0].EquipmentEffects[0][17] = 99
	s.Party[0].HiddenExperience.Health.Exp = 99
	s.Enemies[0].HP = 1
	s.ScriptEntries[10] = 1
	s.Inventory[21] = 99
	if !reflect.DeepEqual(before, e.Snapshot()) {
		t.Fatal("mutable inputs or snapshots alias combat state")
	}
}
