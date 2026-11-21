package palcombat

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"
)

func allAttackConfig() Config {
	c := fixture()
	c.Tables.EnemyTeams[0] = [5]uint16{10, 11, 12, 13, 14}
	c.Tables.Enemies[0].HP = 1000
	c.Tables.Enemies[0].Defense = 0
	c.Party[0].Role.ID = 0
	c.Party[0].EquipmentEffects = &Effects{}
	c.Party[0].EquipmentEffects[3][4] = 1
	return c
}

// Numeric goldens retain output of the fixed public C formula and actual
// all-target numeric loop, compiled outside the repository. No C source or
// proprietary game resources are shipped. Negative FLOAT-to-WORD results are
// native observations; Go defines its compatibility conversion explicitly.
func TestAttackAllNativeReference(t *testing.T) {
	data, err := os.ReadFile("testdata/attack_all_reference.json")
	if err != nil {
		t.Fatal(err)
	}
	var reference struct {
		Cases []struct {
			Attack     uint16 `json:"attack"`
			HP         int    `json:"hp"`
			Mask       int    `json:"mask"`
			MaxSlot    int    `json:"max_slot"`
			Sweeps     int    `json:"sweeps"`
			Critical   bool   `json:"critical"`
			ExpectedHP []int  `json:"expected_hp"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(data, &reference); err != nil {
		t.Fatal(err)
	}
	for _, tc := range reference.Cases {
		c := allAttackConfig()
		c.Party[0].Role.Attack = tc.Attack
		values := make([]int, tc.Sweeps+1)
		for i := 0; i < tc.Sweeps; i++ {
			if !tc.Critical {
				values[i] = 1
			}
		}
		c.Random = &RecordedRandom{Values: values}
		e := create(t, c)
		s := e.Snapshot()
		s.MaxEnemySlot = tc.MaxSlot
		if tc.Sweeps == 2 {
			s.Party[0].Status[DualAttack] = 1
		}
		for i := range s.Enemies {
			s.Enemies[i].HP = tc.HP
			s.Enemies[i].Rewarded = tc.Mask&(1<<s.Enemies[i].Slot) == 0
		}
		// The reference's fixed-slot visit order must survive slice reorder.
		s.Enemies[0], s.Enemies[4] = s.Enemies[4], s.Enemies[0]
		r := Report{}
		if err := e.playerAction(&s, 0, Action{Actor: 0, Kind: Attack, Target: Target{Side: Enemy}}, &r); err != nil {
			t.Fatal(err)
		}
		got := make([]int, 5)
		for _, x := range s.Enemies {
			got[x.Slot] = x.HP
		}
		if !reflect.DeepEqual(got, tc.ExpectedHP) {
			t.Fatalf("case %+v: got HP %v", tc, got)
		}
		var wantSlots []int
		for sweep := 0; sweep < tc.Sweeps; sweep++ {
			for _, slot := range []int{2, 1, 0, 4, 3} {
				if slot <= tc.MaxSlot && tc.Mask&(1<<slot) != 0 {
					wantSlots = append(wantSlots, slot)
				}
			}
		}
		var gotSlots []int
		for _, event := range r.Events {
			if event.Kind == string(Attack) {
				gotSlots = append(gotSlots, s.Enemies[event.TargetIndex].Slot)
			}
		}
		if !reflect.DeepEqual(gotSlots, wantSlots) || len(r.Draws) != tc.Sweeps+1 || s.Party[0].Counters != (Counters{Attack: 1, Health: 2}) {
			t.Fatal("slot/random/counter mismatch", gotSlots, wantSlots, r.Draws, s.Party[0].Counters)
		}
		for i, draw := range r.Draws {
			if i < tc.Sweeps && (draw.Reason != "attack_all.critical" || draw.N != 6) {
				t.Fatal("unexpected all-target randomness", draw)
			}
		}
	}
}

func TestAttackAllEligibilityAndTargetNormalization(t *testing.T) {
	for slot := range 7 {
		c := allAttackConfig()
		c.Party[0].EquipmentEffects = &Effects{}
		c.Party[0].EquipmentEffects[slot][4] = 1
		c.Tables.Enemies[0].HP = 1
		for _, target := range []Target{{Side: Enemy, Index: 0}, {Side: Enemy, Index: -1}, {Side: All}} {
			c.Random = &RecordedRandom{Values: make([]int, 30)}
			e := create(t, c)
			r, err := e.Submit([]Action{{Actor: 0, Kind: Attack, Target: target}})
			if err != nil || !r.Committed || r.Outcome != Victory || e.Snapshot().Earned.Experience != 25 || e.Snapshot().Cash != 115 {
				t.Fatal("all-target normalization/rewards", slot, target, r, err)
			}
			if _, err := e.Submit([]Action{{Actor: 0, Kind: Attack, Target: target}}); err == nil || e.Snapshot().Cash != 115 {
				t.Fatal("repeated all-target rewards")
			}
		}
	}
	// Base-role column is not part of PAL_PlayerCanAttackAll.
	c := allAttackConfig()
	c.Party[0].EquipmentEffects = &Effects{}
	c.Party[0].Role.AttackAll = 1
	c.Random = &RecordedRandom{Values: make([]int, 50)}
	e := create(t, c)
	r, err := e.Submit([]Action{{Actor: 0, Kind: Attack, Target: Target{Side: All}}})
	if err != nil || !r.Committed {
		t.Fatal(err)
	}
	for _, d := range r.Draws {
		if d.Reason == "attack_all.critical" {
			t.Fatal("base role column activated all attack")
		}
	}
}

func TestAttackAllBraveryStillDrawsAndMinimumDamage(t *testing.T) {
	c := allAttackConfig()
	c.Random = &RecordedRandom{Values: []int{5, 0}}
	e := create(t, c)
	s := e.Snapshot()
	s.Party[0].Status[Bravery] = 1
	r := Report{}
	if err := e.playerAction(&s, 0, Action{Kind: Attack, Target: Target{Side: Enemy}}, &r); err != nil {
		t.Fatal(err)
	}
	if s.Enemies[2].HP != 907 || s.Enemies[1].HP != 953 || len(r.Draws) != 2 || !r.Events[0].Critical {
		t.Fatal("bravery random/float damage mismatch", s.Enemies, r)
	}
}

func TestAttackAllPromotesDefeatedSelectedTargetBeforeLivingCheck(t *testing.T) {
	c := allAttackConfig()
	c.Tables.Enemies[0].HP = 1
	c.Random = &RecordedRandom{Values: make([]int, 30)}
	e := create(t, c)
	e.state.Enemies[0].Rewarded, e.state.Enemies[0].HP = true, 0
	r, err := e.Submit([]Action{{Actor: 0, Kind: Attack, Target: Target{Side: Enemy, Index: 0}}})
	if err != nil || !r.Committed || r.Outcome != Victory || e.Snapshot().Earned.Experience != 20 {
		t.Fatal("stale selected enemy blocked an equipment-promoted all attack", err, r)
	}
}

func TestAttackAllDrawFailuresRollbackEntireRound(t *testing.T) {
	// Five enemy initiative draws + one party draw, two sweep criticals and
	// one health-experience draw. Failing at any point cannot commit a prefix.
	values := []int{0, 0, 0, 0, 0, 0, 1, 1, 0}
	for limit := 0; limit < len(values); limit++ {
		c := allAttackConfig()
		c.Tables.Enemies[0].HP = 1
		c.Party[0].Status[DualAttack] = 1
		c.Random = &RecordedRandom{Values: append([]int(nil), values[:limit]...)}
		e := create(t, c)
		before := e.Snapshot()
		r, err := e.Submit([]Action{{Actor: 0, Kind: Attack, Target: Target{Side: Enemy}}})
		if err == nil || r.Committed || !reflect.DeepEqual(before, e.Snapshot()) {
			t.Fatal("failed all-target round committed prefix", limit, err)
		}
	}
}
