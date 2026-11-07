package battle

import (
	"reflect"
	"testing"

	"github.com/HiChen85/pal--reverse--learn/internal/content"
)

func fixture() content.Pack {
	return content.Pack{
		Characters: []content.Character{
			{ID: "hero", Name: "归客", MaxHP: 100, MaxMP: 9, Attack: 20, Defense: 10, Speed: 10},
			{ID: "friend", Name: "山行者", MaxHP: 70, MaxMP: 4, Attack: 15, Defense: 6, Speed: 8},
		},
		Items:      []content.Item{{ID: "herb", Name: "草药", HealHP: 50}, {ID: "tea", Name: "清茶", RestoreMP: 20}},
		Encounters: []content.Encounter{{ID: "fog", Enemies: []content.Character{{ID: "shade", Name: "雾影", MaxHP: 100, Attack: 25, Defense: 10, Speed: 5}}}},
	}
}

func mustNew(t *testing.T, p content.Pack, party []string, inventory map[string]int) *Battle {
	t.Helper()
	b, err := New(p, party, "fog", inventory)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func do(t *testing.T, b *Battle, kind Kind, target, item string) {
	t.Helper()
	if err := b.Act(Command{ActorID: b.Snapshot().Turn, Kind: kind, TargetID: target, ItemID: item}); err != nil {
		t.Fatal(err)
	}
}

func TestOrderAutomaticEnemiesAndRounds(t *testing.T) {
	p := fixture()
	p.Encounters[0].Enemies = append(p.Encounters[0].Enemies, content.Character{ID: "fast", Name: "疾影", MaxHP: 20, Attack: 6, Speed: 15})
	b := mustNew(t, p, []string{"hero", "friend"}, nil)
	s := b.Snapshot()
	if s.Turn != "party:hero" || s.Party[0].HP != 99 || s.Round != 1 || len(s.Log) != 1 {
		t.Fatalf("fast enemy must act before hero: %+v", s)
	}
	do(t, b, Attack, s.Enemies[1].ID, "") // kill fast enemy
	s = b.Snapshot()
	if s.Turn != "party:friend" || s.Enemies[1].HP != 0 || s.Round != 1 {
		t.Fatalf("unexpected next actor: %+v", s)
	}
	do(t, b, Attack, s.Enemies[0].ID, "")
	s = b.Snapshot()
	if s.Turn != "party:hero" || s.Round != 2 || s.Party[0].HP != 79 {
		t.Fatalf("slow enemy must act before next round: %+v", s)
	}
}

func TestInvalidActionsAreTransactional(t *testing.T) {
	b := mustNew(t, fixture(), []string{"hero", "friend"}, map[string]int{"herb": 1})
	commands := []Command{
		{ActorID: "party:friend", Kind: Attack, TargetID: "enemy:0:shade"},
		{ActorID: "party:hero", Kind: Attack, TargetID: "party:friend"},
		{ActorID: "party:hero", Kind: Attack, TargetID: "missing"},
		{ActorID: "party:hero", Kind: Kind("escape")},
		{ActorID: "party:hero", Kind: UseItem, TargetID: "enemy:0:shade", ItemID: "herb"},
		{ActorID: "party:hero", Kind: UseItem, TargetID: "party:hero", ItemID: "herb"}, // full HP
		{ActorID: "party:hero", Kind: UseItem, TargetID: "party:hero", ItemID: "unknown"},
		{ActorID: "party:hero", Kind: Defend, TargetID: "party:hero"},
		{ActorID: "party:hero", Kind: Skill, TargetID: "enemy:0:shade", ItemID: "herb"},
	}
	for _, command := range commands {
		before := b.Snapshot()
		if err := b.Act(command); err == nil {
			t.Errorf("accepted invalid command: %+v", command)
		}
		if !reflect.DeepEqual(before, b.Snapshot()) {
			t.Fatalf("invalid command changed state: %+v", command)
		}
	}
}

func TestHealingInventoryAndMPCost(t *testing.T) {
	b := mustNew(t, fixture(), []string{"hero"}, map[string]int{"herb": 1, "tea": 1})
	do(t, b, Skill, "enemy:0:shade", "")
	if s := b.Snapshot(); s.Party[0].MP != 6 || s.Party[0].HP != 80 || s.Enemies[0].HP != 65 {
		t.Fatalf("skill formula or MP cost: %+v", s)
	}
	do(t, b, UseItem, "party:hero", "herb")
	if s := b.Snapshot(); s.Party[0].HP != 80 || s.Inventory["herb"] != 0 || s.Log[2].Amount != 20 {
		t.Fatalf("heal should clamp at max before enemy turn: %+v", s)
	}
	do(t, b, UseItem, "party:hero", "tea")
	if b.Snapshot().Party[0].MP != 9 || b.Snapshot().Inventory["tea"] != 0 {
		t.Fatal("MP restoration did not clamp")
	}
	before := b.Snapshot()
	if err := b.Act(Command{ActorID: before.Turn, Kind: UseItem, TargetID: "party:hero", ItemID: "tea"}); err == nil || !reflect.DeepEqual(before, b.Snapshot()) {
		t.Fatal("exhausted inventory should be a transactional failure")
	}
}

func TestDefendDurationAndInsufficientMP(t *testing.T) {
	p := fixture()
	p.Characters[0].MaxMP = 2
	b := mustNew(t, p, []string{"hero"}, nil)
	do(t, b, Defend, "", "")
	if b.Snapshot().Party[0].HP != 90 || b.Snapshot().Party[0].Defending {
		t.Fatal("defend should halve damage then expire at next actor turn")
	}
	before := b.Snapshot()
	if err := b.Act(Command{ActorID: before.Turn, Kind: Skill, TargetID: before.Enemies[0].ID}); err == nil || !reflect.DeepEqual(before, b.Snapshot()) {
		t.Fatal("insufficient MP should not mutate")
	}
	do(t, b, Attack, "enemy:0:shade", "")
	if b.Snapshot().Party[0].HP != 70 {
		t.Fatal("defend must not persist across own turn")
	}
}

func TestVictoryDefeatAndDeadActors(t *testing.T) {
	p := fixture()
	p.Encounters[0].Enemies[0].MaxHP = 10
	b := mustNew(t, p, []string{"hero"}, nil)
	do(t, b, Attack, "enemy:0:shade", "")
	s := b.Snapshot()
	if s.Outcome != Victory || s.Turn != "" || len(s.Log) != 1 || s.Party[0].HP != 100 {
		t.Fatalf("victory must prevent retaliation: %+v", s)
	}
	if err := b.Act(Command{ActorID: "party:hero", Kind: Defend}); err == nil || !reflect.DeepEqual(s, b.Snapshot()) {
		t.Fatal("finished battle must reject action")
	}
	p = fixture()
	p.Characters[0].MaxHP = 1
	p.Encounters[0].Enemies[0].Speed = 20
	b = mustNew(t, p, []string{"hero", "friend"}, nil)
	if b.Snapshot().Turn != "party:friend" || b.Snapshot().Party[0].HP != 0 {
		t.Fatal("dead party member must be skipped")
	}
	before := b.Snapshot()
	if err := b.Act(Command{ActorID: before.Turn, Kind: UseItem, TargetID: "party:hero", ItemID: "herb"}); err == nil || !reflect.DeepEqual(before, b.Snapshot()) {
		t.Fatal("dead party target must be rejected")
	}
	p.Characters[1].MaxHP = 1
	b = mustNew(t, p, []string{"hero", "friend"}, nil)
	do(t, b, Attack, "enemy:0:shade", "")
	if b.Snapshot().Outcome != Defeat || b.Snapshot().Turn != "" {
		t.Fatal("last dead party member must end combat")
	}
}

func TestSnapshotsInputIsolationAndReproducibility(t *testing.T) {
	p := fixture()
	inventory := map[string]int{"herb": 1}
	b := mustNew(t, p, []string{"hero"}, inventory)
	a := mustNew(t, p, []string{"hero"}, inventory)
	inventory["herb"] = 99
	p.Characters[0].Attack = 99999
	s := b.Snapshot()
	s.Party[0].HP = 0
	s.Enemies[0].HP = 0
	s.Inventory["herb"] = 0
	for i := 0; i < 3; i++ {
		do(t, a, Attack, "enemy:0:shade", "")
		do(t, b, Attack, "enemy:0:shade", "")
	}
	if !reflect.DeepEqual(a.Snapshot(), b.Snapshot()) || b.Snapshot().Inventory["herb"] != 1 {
		t.Fatal("same inputs must reproduce and caller data must not alias")
	}
	s = b.Snapshot()
	s.Log[0].Text = "corrupt"
	if b.Snapshot().Log[0].Text == "corrupt" {
		t.Fatal("log snapshot aliases battle")
	}
}

func TestRejectInvalidSetup(t *testing.T) {
	for _, party := range [][]string{nil, {"missing"}, {"hero", "hero"}} {
		if _, err := New(fixture(), party, "fog", nil); err == nil {
			t.Errorf("accepted invalid party: %v", party)
		}
	}
	p := fixture()
	p.Encounters[0].Enemies[0].MaxHP = 0
	if _, err := New(p, []string{"hero"}, "fog", nil); err == nil {
		t.Error("accepted enemy with zero max HP")
	}
	if _, err := New(fixture(), []string{"hero"}, "fog", map[string]int{"unknown": 1}); err == nil {
		t.Error("accepted unknown item")
	}
}
