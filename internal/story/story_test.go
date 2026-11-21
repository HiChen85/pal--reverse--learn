package story

import (
	"reflect"
	"testing"

	"github.com/HiChen85/pal--reverse--learn/internal/battle"
	"github.com/HiChen85/pal--reverse--learn/internal/content"
)

func newDemo(t *testing.T) *Session {
	t.Helper()
	s, err := New(content.Demo())
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
func win(t *testing.T, s *Session) {
	t.Helper()
	for range 100 {
		v := s.View()
		if v.Battle == nil {
			t.Fatal("missing battle")
		}
		b := v.Battle
		if b.Outcome == battle.Victory {
			return
		}
		if b.Outcome != battle.Ongoing {
			t.Fatal("unexpected loss")
		}
		var target string
		for _, u := range b.Enemies {
			if u.HP > 0 {
				target = u.ID
				break
			}
		}
		must(t, s.Act(battle.Command{ActorID: b.Turn, Kind: battle.Attack, TargetID: target}))
	}
	t.Fatal("battle did not end")
}
func TestFlagsBattleGateAndEnding(t *testing.T) {
	s := newDemo(t)
	must(t, s.Start(""))
	must(t, s.Continue())
	must(t, s.Choose(0))
	if !s.View().Flags["knows-spring"] {
		t.Fatal("flag not applied on entry")
	}
	must(t, s.Continue())
	before := s.View()
	if s.Continue() == nil || s.Choose(0) == nil {
		t.Fatal("battle was bypassed")
	}
	if !reflect.DeepEqual(before, s.View()) {
		t.Fatal("failed transition changed session")
	}
	win(t, s)
	must(t, s.Continue())
	if len(s.View().Choices) != 2 {
		t.Fatal("expected unlocked branch")
	}
	must(t, s.Choose(0))
	must(t, s.Continue())
	if !s.View().Ended || s.View().Node.ID != "ending" {
		t.Fatal("did not finish")
	}
	if s.Continue() == nil {
		t.Fatal("ended story continued")
	}
}
func TestHiddenChoicesAndViewCopies(t *testing.T) {
	s := newDemo(t)
	must(t, s.Start("bridge"))
	v := s.View()
	if len(v.Choices) != 1 || v.Choices[0].Next != "ending" {
		t.Fatal("hidden branch exposed")
	}
	v.Flags["knows-spring"] = true
	v.Node.Choices[0].RequireFlags["knows-spring"] = false
	v.Choices[0].Next = "spring-cave"
	if len(s.View().Choices) != 1 || s.View().Flags["knows-spring"] {
		t.Fatal("view aliases session")
	}
	before := s.View()
	if s.Start("missing") == nil {
		t.Fatal("unknown start accepted")
	}
	if !reflect.DeepEqual(before, s.View()) {
		t.Fatal("bad start modified state")
	}
	must(t, s.Choose(0))
	if !s.View().Ended {
		t.Fatal("visible index did not map to ending")
	}
}
func TestInventoryCarriesToNextEncounter(t *testing.T) {
	p := content.Demo()
	p.Characters[1].Speed = 0
	for i := range p.Nodes {
		if p.Nodes[i].ID == "mountain" {
			p.Nodes[i].Next = "second"
		}
	}
	p.Nodes = append(p.Nodes, content.Node{ID: "second", Encounter: "mist-guardian", Next: "ending"})
	s, err := New(p)
	must(t, err)
	must(t, s.Start("mountain"))
	b := s.View().Battle
	must(t, s.Act(battle.Command{ActorID: b.Turn, Kind: battle.Defend}))
	b = s.View().Battle
	must(t, s.Act(battle.Command{ActorID: b.Turn, Kind: battle.UseItem, TargetID: b.Party[0].ID, ItemID: "herb"}))
	win(t, s)
	must(t, s.Continue())
	b = s.View().Battle
	if b.Inventory["herb"] != 2 {
		t.Fatalf("inventory reset: %v", b.Inventory)
	}
	if b.Party[0].HP != b.Party[0].MaxHP {
		t.Fatal("preview encounter should start healed")
	}
	b.Inventory["herb"] = 100
	if s.View().Battle.Inventory["herb"] != 2 {
		t.Fatal("battle snapshot aliases session")
	}
}
func TestDefeatRequiresRetry(t *testing.T) {
	p := content.Demo()
	for i := range p.Characters {
		p.Characters[i].MaxHP = 1
		p.Characters[i].Speed = 0
	}
	p.Encounters[0].Enemies[0].Speed = 100
	p.Encounters[0].Enemies[0].Attack = 1000
	s, err := New(p)
	must(t, err)
	must(t, s.Start("mountain"))
	for range 10 {
		b := s.View().Battle
		if b.Outcome == battle.Defeat {
			break
		}
		must(t, s.Act(battle.Command{ActorID: b.Turn, Kind: battle.Defend}))
	}
	if s.View().Battle.Outcome != battle.Defeat {
		t.Fatal("expected defeat")
	}
	if s.Continue() == nil {
		t.Fatal("defeat advanced")
	}
	must(t, s.RetryBattle())
	if s.View().Battle.Inventory["herb"] != 3 {
		t.Fatal("retry lost initial inventory")
	}
}

func TestFirstThreeCharactersFormPreviewParty(t *testing.T) {
	p := content.Demo()
	third := p.Characters[0]
	third.ID = "third"
	third.Name = "Third"
	fourth := p.Characters[0]
	fourth.ID = "fourth"
	fourth.Name = "Fourth"
	p.Characters = append(p.Characters, third, fourth)
	s, err := New(p)
	must(t, err)
	must(t, s.Start("mountain"))
	party := s.View().Battle.Party
	if len(party) != 3 || party[0].CharacterID != "lin-yue" || party[1].CharacterID != "su-qing" || party[2].CharacterID != "third" {
		t.Fatalf("unexpected party: %+v", party)
	}
}
