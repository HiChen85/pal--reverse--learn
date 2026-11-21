package studio

import (
	"reflect"
	"testing"

	"github.com/HiChen85/pal--reverse--learn/internal/battle"
	"github.com/HiChen85/pal--reverse--learn/internal/content"
)

func TestMapPreviewStartsRiverRoutes(t *testing.T) {
	pack, err := content.Load("../../examples/chapter-two.json")
	ok(t, err)
	s, err := New(pack)
	ok(t, err)
	preview, err := s.NewMapPreview("river-bank")
	ok(t, err)

	spawn := content.MapPoint{X: 1, Y: 1}
	if got := preview.Position(); got != spawn {
		t.Fatalf("spawn position = %v, want %v", got, spawn)
	}
	if err := preview.Move(0, -1); err == nil || preview.Position() != spawn {
		t.Fatalf("move into wall returned %v at %v", err, preview.Position())
	}
	ok(t, preview.Move(1, 0))
	if node, found := preview.EventNode(); found {
		t.Fatalf("unexpected event %q on the path", node)
	}
	ok(t, preview.Move(1, 0))
	node, found := preview.EventNode()
	if !found || node != "river-arrival" {
		t.Fatalf("arrival event = %q, found = %v", node, found)
	}
	if s.Current().Started {
		t.Fatal("walking onto an event started the story")
	}
	ok(t, s.Start(node))
	if got := s.Current().Node.ID; got != node {
		t.Fatalf("started story at %q, want %q", got, node)
	}
	ok(t, s.Continue())
	ok(t, s.Choose(0))
	if got := s.Current(); got.Node.ID != "river-lampkeeper" || !got.Flags["knows-river-signal"] {
		t.Fatalf("lantern dialogue reached %q with flags %v", got.Node.ID, got.Flags)
	}
	ok(t, s.Continue())
	if got := s.Current().Choices; len(got) != 1 || got[0].Next != "river-sluice" {
		t.Fatalf("choices after the lantern dialogue = %v", got)
	}
	ok(t, s.Choose(0))
	winRiverEncounter(t, s)
	ok(t, s.Continue())
	if got := s.Current().Choices; len(got) != 2 || got[1].Next != "river-beacons" {
		t.Fatalf("lantern dialogue did not open the bridge option: %v", got)
	}
	ok(t, s.Choose(1))
	ok(t, s.Continue())
	if got := s.Current(); !got.Ended || got.Node.ID != "river-bridge-end" || !got.Flags["river-crossing-restored"] {
		t.Fatalf("bridge ending reached %q with flags %v (ended = %v)", got.Node.ID, got.Flags, got.Ended)
	}

	preview, err = s.NewMapPreview("river-bank")
	ok(t, err)
	for range 2 {
		ok(t, preview.Move(0, 1))
	}
	for range 5 {
		ok(t, preview.Move(1, 0))
	}
	node, found = preview.EventNode()
	if !found || node != "river-sluice" {
		t.Fatalf("sluice event = %q, found = %v", node, found)
	}
	if got := s.Current(); !got.Ended || got.Node.ID != "river-bridge-end" {
		t.Fatalf("walking onto the sluice event replaced the bridge ending with %q", got.Node.ID)
	}
	ok(t, s.Start(node))
	if got := s.Current(); got.Node.ID != node || len(got.Flags) != 0 {
		t.Fatalf("sluice event started at %q with flags %v", got.Node.ID, got.Flags)
	}
	winRiverEncounter(t, s)
	ok(t, s.Continue())
	if got := s.Current().Choices; len(got) != 1 || got[0].Next != "river-end" {
		t.Fatalf("choices after starting at the sluice event = %v", got)
	}
	ok(t, s.Choose(0))
	if got := s.Current(); !got.Ended || got.Node.ID != "river-end" || !got.Flags["river-names-filed"] {
		t.Fatalf("road ending reached %q with flags %v (ended = %v)", got.Node.ID, got.Flags, got.Ended)
	}
}

func winRiverEncounter(t *testing.T, s *Studio) {
	t.Helper()
	for range 3 {
		b := s.Current().Battle
		if b == nil || b.Outcome != battle.Ongoing || len(b.Enemies) != 1 || b.Enemies[0].CharacterID != "rope-moss" {
			t.Fatalf("unexpected river battle state: %+v", b)
		}
		ok(t, s.Act(battle.Command{ActorID: b.Turn, Kind: battle.Skill, TargetID: b.Enemies[0].ID}))
	}
	if b := s.Current().Battle; b == nil || b.Outcome != battle.Victory {
		t.Fatalf("river battle did not end in victory: %+v", b)
	}
}

func TestMapPreviewUsesAppliedPack(t *testing.T) {
	s := demo(t)
	draft := s.Pack()
	eventTile := content.MapPoint{X: 1}
	draft.Maps = []content.TileMap{{
		ID: "test-map", Name: "Test map", Rows: []string{"..#"},
		Events: []content.MapEvent{{Position: eventTile, Node: "ending"}},
	}}
	if _, err := s.NewMapPreview("test-map"); err == nil {
		t.Fatal("unapplied map was available for preview")
	}
	ok(t, s.ReplacePack(draft))
	ok(t, s.Start(""))
	before := s.Current()
	draft.Maps[0].Rows[0] = "###"
	if s.ReplacePack(draft) == nil {
		t.Fatal("invalid map edit was applied")
	}
	preview, err := s.NewMapPreview("test-map")
	ok(t, err)
	ok(t, preview.Move(1, 0))
	for _, step := range []content.MapPoint{{X: 1}, {Y: 1}, {X: 1, Y: 1}, {X: 2}, {}} {
		if err := preview.Move(step.X, step.Y); err == nil || preview.Position() != eventTile {
			t.Fatalf("invalid move %v returned %v at %v", step, err, preview.Position())
		}
	}
	if node, found := preview.EventNode(); !found || node != "ending" {
		t.Fatalf("event after invalid moves = %q, found = %v", node, found)
	}
	fresh, err := s.NewMapPreview("test-map")
	ok(t, err)
	if got := fresh.Position(); got != (content.MapPoint{}) {
		t.Fatalf("new preview position = %v, want spawn at (0, 0)", got)
	}
	if !reflect.DeepEqual(before, s.Current()) {
		t.Fatal("map preview or invalid map edit changed the story session")
	}
}
