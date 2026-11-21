package palcombat

import (
	"os"
	"testing"

	"github.com/HiChen85/pal--reverse--learn/internal/paldata"
)

// Only source identities and numeric expectations are retained. This test
// reads user-owned resources locally; no resources, dialogue or art are fixtures.
func TestLocalTeamZeroGolden(t *testing.T) {
	dir := os.Getenv("PALCOMBAT_DATA_DIR")
	if dir == "" {
		t.Skip("set PALCOMBAT_DATA_DIR for authorized local numerical regression")
	}
	tables, err := paldata.LoadRuntime(dir)
	if err != nil {
		t.Fatal(err)
	}
	g, err := paldata.Load(dir, paldata.Options{Encoding: "big5", Layout: "dos"})
	if err != nil {
		t.Fatal(err)
	}
	if tables.Source.SHA256 != "d9e82696a42d93f9623c63714a68a12645c7d650776eaa8510ec27b7c35e819a" {
		t.Skip("local DATA identity has no numerical oracle")
	}
	matched := false
	for _, f := range g.Files {
		if f.SHA256 == "38252bae8dabaebe9c780f00e8e6829bce56f89d862ecf9d44cc7b135047ed5d" {
			matched = true
		}
	}
	if !matched {
		t.Fatal("known DATA oracle requires its paired SSS identity")
	}
	role := tables.Roles[0]
	// A controlled unequipped combatant avoids claiming that equipment scripts
	// were evaluated. All remaining attributes come from the local numeric import.
	role.Equipment = [paldata.EquipmentSlots]uint16{}
	h := &HiddenExperience{}
	for _, x := range []*paldata.Experience{&h.Health, &h.Magic, &h.Attack, &h.MagicPower, &h.Defense, &h.Dexterity, &h.Flee} {
		x.Level = role.Level
	}
	tape := &RecordedRandom{}
	e := create(t, Config{Tables: tables, Game: g, TeamID: 0, Party: []Player{{Role: role, HiddenExperience: h}}, Random: tape})
	if len(e.Snapshot().Enemies) != 2 || e.Snapshot().Enemies[0].ObjectID != 398 || e.Snapshot().Enemies[1].ObjectID != 398 {
		t.Fatal("team identity differs")
	}
	for round, want := range []struct {
		target, hp0, hp1 int
		playerHP         uint16
		draws            int
		outcome          Outcome
	}{
		{0, 9, 28, 132, 18, Ongoing},
		{0, 0, 28, 123, 13, Ongoing},
		{1, 0, 9, 114, 12, Ongoing},
		{1, 0, 0, 132, 7, Victory},
	} {
		before := e.Snapshot()
		values := []int{}
		for _, x := range before.Enemies {
			if x.HP > 0 {
				values = append(values, 500000)
			}
		}
		values = append(values, 500000)
		values = append(values, 0, 1, 1, 0, 0)
		for i, x := range before.Enemies {
			if x.HP <= 0 || i == want.target && x.HP <= 19 {
				continue
			}
			values = append(values, 0, 0, 0, 0, 9)
		}
		tape.Values = values
		tape.Index = 0
		r, err := e.Submit([]Action{{Actor: 0, Kind: Attack, Target: Target{Side: Enemy, Index: want.target}}})
		if err != nil {
			t.Fatalf("round %d: %v", round+1, err)
		}
		s := e.Snapshot()
		if !r.Committed || s.Outcome != want.outcome || s.Enemies[0].HP != want.hp0 || s.Enemies[1].HP != want.hp1 || s.Party[0].Role.HP != want.playerHP || len(r.Draws) != want.draws || tape.Index != len(values) {
			t.Fatalf("round %d numerical oracle differs: hp=(%d,%d) party=%d draws=%d outcome=%s", round+1, s.Enemies[0].HP, s.Enemies[1].HP, s.Party[0].Role.HP, len(r.Draws), s.Outcome)
		}
	}
	s := e.Snapshot()
	if s.Cash != 2 || s.Earned.Cash != 2 || s.Earned.Experience != 2 || s.Party[0].Experience != 2 || s.Party[0].HiddenExperience.Health.Exp != 2 || !s.Earned.Applied {
		t.Fatal("numeric reward oracle differs")
	}
}
