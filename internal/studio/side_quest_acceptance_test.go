package studio

import (
	"maps"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"github.com/HiChen85/pal--reverse--learn/internal/battle"
	"github.com/HiChen85/pal--reverse--learn/internal/content"
)

func TestOptionalQuestRejoinsMainStory(t *testing.T) {
	pack := content.Pack{
		SchemaVersion: content.SchemaVersion, ID: "signal-yard", Title: "The Signal Yard", Start: "orders",
		Characters: []content.Character{{ID: "ada", Name: "Ada", MaxHP: 20, Attack: 8, Speed: 2}},
		Items:      []content.Item{{ID: "tonic", Name: "Tonic", HealHP: 4}},
		Encounters: []content.Encounter{
			{ID: "shed", Enemies: []content.Character{{ID: "shed-thorn", Name: "Shed thorn", MaxHP: 6, Attack: 4, Speed: 3}}},
			{ID: "bridge", Enemies: []content.Character{{ID: "bridge-thorn", Name: "Bridge thorn", MaxHP: 6, Attack: 4, Speed: 1}}},
		},
		Nodes: []content.Node{
			{ID: "orders", Text: "Light the crossing signal before the cart arrives.", SetFlags: map[string]bool{"has-orders": true}, Next: "yard"},
			{ID: "yard", Text: "The bridge is ahead. The workshop has lost its tool roll.", Choices: []content.Choice{
				{Text: "Cross the bridge", Next: "crossing", RequireFlags: map[string]bool{"has-orders": true}},
				{Text: "Read the yard notice", Next: "notice"},
				{Text: "Look for the tool roll", Next: "toolshed", RequireFlags: map[string]bool{"tools-returned": false}},
			}},
			{ID: "notice", Text: "Keep the bridge clear for the cart.", Next: "yard"},
			{ID: "toolshed", Text: "A thorn has trapped the tool roll under the shed door.", Encounter: "shed", Next: "return-tools"},
			{ID: "return-tools", Text: "The workshop has its tools back.", SetFlags: map[string]bool{"tools-returned": true}, Next: "yard"},
			{ID: "crossing", Text: "A thorn blocks the crossing signal.", Encounter: "bridge", Next: "signal-lit"},
			{ID: "signal-lit", Text: "The crossing signal is lit. The cart can pass.", SetFlags: map[string]bool{"signal-lit": true}},
		},
	}
	for _, route := range []struct {
		name     string
		complete bool
		tonics   int
	}{
		{name: "skip the workshop", tonics: 3},
		{name: "return the tools", complete: true, tonics: 2},
	} {
		t.Run(route.name, func(t *testing.T) {
			s, err := New(pack)
			ok(t, err)
			ok(t, s.Start(""))
			ok(t, s.Continue())
			flags := map[string]bool{"has-orders": true}
			choices := []string{"crossing", "notice", "toolshed"}
			assertQuestScene(t, s, "yard", flags, choices...)

			if route.complete {
				ok(t, s.Choose(2))
				assertQuestScene(t, s, "toolshed", flags)
				fight := assertQuestBattle(t, s, "shed-thorn", battle.Ongoing, 3)
				ok(t, s.Act(battle.Command{ActorID: fight.Turn, Kind: battle.UseItem, TargetID: fight.Turn, ItemID: "tonic"}))
				fight = assertQuestBattle(t, s, "shed-thorn", battle.Ongoing, 2)
				if err := s.Continue(); err == nil {
					t.Fatal("continued past the unfinished optional encounter")
				}
				ok(t, s.Act(battle.Command{ActorID: fight.Turn, Kind: battle.Attack, TargetID: fight.Enemies[0].ID}))
				assertQuestBattle(t, s, "shed-thorn", battle.Victory, 2)
				ok(t, s.Continue())
				flags["tools-returned"] = true
				assertQuestScene(t, s, "return-tools", flags)
				ok(t, s.Continue())
				choices = []string{"crossing", "notice"}
				assertQuestScene(t, s, "yard", flags, choices...)

				ok(t, s.Choose(1))
				assertQuestScene(t, s, "notice", flags)
				ok(t, s.Continue())
				assertQuestScene(t, s, "yard", flags, choices...)
				before := s.Current()
				if err := s.Choose(2); err == nil || !reflect.DeepEqual(before, s.Current()) {
					t.Fatalf("selecting the completed quest again returned %v at node %q", err, s.Current().Node.ID)
				}
			}

			checkpoint := filepath.Join(t.TempDir(), "yard-progress.json")
			ok(t, s.SaveProgress(checkpoint))
			restored, err := New(pack)
			ok(t, err)
			ok(t, restored.LoadProgress(checkpoint))
			if !reflect.DeepEqual(s.Current(), restored.Current()) {
				t.Fatalf("restored hub differs from the saved hub\n got: %+v\nwant: %+v", restored.Current(), s.Current())
			}
			assertQuestScene(t, restored, "yard", flags, choices...)

			ok(t, s.Choose(0))
			ok(t, restored.Choose(0))
			if !reflect.DeepEqual(s.Current(), restored.Current()) {
				t.Fatalf("restored route entered a different main encounter\n got: %+v\nwant: %+v", restored.Current(), s.Current())
			}
			assertQuestScene(t, restored, "crossing", flags)
			fight := assertQuestBattle(t, restored, "bridge-thorn", battle.Ongoing, route.tonics)
			ok(t, restored.Act(battle.Command{ActorID: fight.Turn, Kind: battle.Attack, TargetID: fight.Enemies[0].ID}))
			assertQuestBattle(t, restored, "bridge-thorn", battle.Victory, route.tonics)
			ok(t, restored.Continue())
			flags["signal-lit"] = true
			assertQuestScene(t, restored, "signal-lit", flags)
			ending := restored.Current()
			if !ending.Ended || ending.Battle != nil {
				t.Fatalf("main ending is not terminal: %+v", ending)
			}
			if err := restored.Continue(); err == nil || !reflect.DeepEqual(ending, restored.Current()) {
				t.Fatalf("continuing past the ending returned %v at node %q", err, restored.Current().Node.ID)
			}
		})
	}
}

func assertQuestScene(t *testing.T, s *Studio, node string, flags map[string]bool, choices ...string) {
	t.Helper()
	view := s.Current()
	var targets []string
	for _, choice := range view.Choices {
		targets = append(targets, choice.Next)
	}
	if !view.Started || view.Node.ID != node || !maps.Equal(view.Flags, flags) || !slices.Equal(targets, choices) {
		t.Fatalf("story reached %q with flags %v and choices %v (started = %v), want %q with flags %v and choices %v",
			view.Node.ID, view.Flags, targets, view.Started, node, flags, choices)
	}
}

func assertQuestBattle(t *testing.T, s *Studio, enemy string, outcome battle.Outcome, tonics int) *battle.State {
	t.Helper()
	state := s.Current().Battle
	if state == nil || state.Outcome != outcome || len(state.Enemies) != 1 || state.Enemies[0].CharacterID != enemy ||
		!maps.Equal(state.Inventory, map[string]int{"tonic": tonics}) {
		t.Fatalf("encounter state = %+v, want %q with outcome %q and %d tonics", state, enemy, outcome, tonics)
	}
	return state
}
