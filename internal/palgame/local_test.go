package palgame

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/HiChen85/pal--reverse--learn/internal/palasset"
	"github.com/HiChen85/pal--reverse--learn/internal/paldata"
)

// This explicit checkpoint test is optional: game assets are never embedded in
// the repository. It verifies stateful pickup scripts, not main-story progress
// or the spatial reachability of the supplied checkpoint.
func TestLocalPickupCheckpoint(t *testing.T) {
	dir := os.Getenv("PAL_GAME_DATA_DIR")
	if dir == "" {
		t.Skip("set PAL_GAME_DATA_DIR to a matching local authorized dataset")
	}
	g, err := paldata.Load(dir, paldata.Options{Encoding: "big5", Layout: "dos"})
	if err != nil {
		t.Fatal(err)
	}
	r, err := paldata.LoadRuntime(dir)
	if err != nil {
		t.Fatal(err)
	}
	s, err := NewSession(g, r, SessionOptions{Scene: 1, AssumeSceneEntered: true})
	if err != nil {
		t.Fatal(err)
	}
	for i, id := range []uint16{5, 6, 7} {
		report, err := s.Trigger(id, nil)
		if err != nil {
			t.Fatal(err)
		}
		if !report.Completed || !reflect.DeepEqual(report.DialogueIDs, []uint16{uint16(1025 + i)}) {
			t.Fatalf("unexpected event %d message IDs %v", id, report.DialogueIDs)
		}
		before := s.Snapshot()
		if _, err = s.Trigger(id, nil); err == nil {
			t.Fatalf("event %d repeated after hidden", id)
		}
		if !reflect.DeepEqual(before, s.Snapshot()) {
			t.Fatal("rejected repeat changed checkpoint")
		}
	}
	for _, item := range []uint16{99, 201, 236} {
		if s.Snapshot().World.Inventory[item] != 1 {
			t.Fatalf("wrong pickup amount %d", item)
		}
	}
}

// This regression starts with the imported new-game defaults and completes the
// opening before any pickups. It verifies this logical sequence only, including
// script entry persistence; it does not establish spatial interaction reachability
// or compare a complete playthrough with the original executable.
func TestLocalOpeningAndPickupSequence(t *testing.T) {
	dir := os.Getenv("PAL_GAME_DATA_DIR")
	if dir == "" {
		t.Skip("set PAL_GAME_DATA_DIR to the documented local DOS sample")
	}
	g, err := paldata.Load(dir, paldata.Options{Encoding: "big5", Layout: "dos"})
	if err != nil {
		t.Fatal(err)
	}
	r, err := paldata.LoadRuntime(dir)
	if err != nil {
		t.Fatal(err)
	}
	assets, err := palasset.OpenMGO(filepath.Join(dir, "mgo.mkf"), 8<<20)
	if err != nil {
		t.Fatal("opening regression requires the documented MGO sample: ", err)
	}
	maps, err := palasset.OpenMaps(filepath.Join(dir, "map.mkf"), filepath.Join(dir, "gop.mkf"))
	if err != nil {
		t.Fatal(err)
	}
	s, err := NewSession(g, r, SessionOptions{Assets: assets, Maps: maps, RedactText: true})
	if err != nil {
		t.Fatal(err)
	}
	report, err := s.Enter(nil)
	if err != nil {
		t.Fatal(err)
	}
	after := s.Snapshot()
	if !report.Completed || len(report.DialogueIDs) != 67 || report.State.ReturnedEntry != 8145 || after.World.Scenes[1].OnEnter != 8145 || after.NeedsSceneEnter || after.Actions != 1 {
		t.Fatalf("opening regression mismatch: completed=%v messages=%d return=%d", report.Completed, len(report.DialogueIDs), report.State.ReturnedEntry)
	}
	if after.World.PartyX != 1344 || after.World.PartyY != 288 || after.World.Events[11].State != 0 || after.World.Events[11].AutoScript != 4458 || after.World.Events[11].X != 1152 || after.World.Events[11].Y != 384 {
		t.Fatal("opening movement/auto state mismatch")
	}
	for i, id := range []uint16{5, 6, 7} {
		report, err = s.Trigger(id, nil)
		if err != nil || !report.Completed || !reflect.DeepEqual(report.DialogueIDs, []uint16{uint16(1025 + i)}) {
			t.Fatalf("opening then event%d: %v", id, err)
		}
	}
	before := s.Snapshot()
	if _, err = s.Trigger(5, nil); err == nil || !reflect.DeepEqual(before, s.Snapshot()) {
		t.Fatal("repeat pickup accepted or changed state")
	}
	for _, item := range []uint16{99, 201, 236} {
		if s.Snapshot().World.Inventory[item] != 1 {
			t.Fatalf("wrong pickup item%d", item)
		}
	}
	// Departure and the following resource preparation continue the same world.
	report, err = s.Trigger(1, nil)
	if err != nil || !report.Completed || s.Snapshot().World.Scene != 3 || !s.Snapshot().World.SceneLoadPending || !s.Snapshot().NeedsSceneEnter {
		t.Fatalf("departure failed: %v", err)
	}
	if _, err = s.Trigger(63, nil); err == nil {
		t.Fatal("event bypassed pending scene entry")
	}
	if report, err = s.Enter(nil); err != nil || !report.Completed {
		t.Fatalf("next scene entry: %v", err)
	}
	next := s.Snapshot()
	if next.NeedsSceneEnter || next.World.SceneLoadPending || next.World.SceneResources == nil || next.World.SceneResources.SceneID != 3 || next.World.SceneResources.MapID != 10 {
		t.Fatal("next scene resources were not loaded")
	}
	for _, item := range []uint16{99, 201, 236} {
		if next.World.Inventory[item] != 1 {
			t.Fatal("scene transition discarded inventory")
		}
	}
	if s.LastInvocation() == nil || s.LastInvocation().Request.World.SceneResources.MapID != 10 {
		t.Fatal("export request differs from prepared invocation")
	}
}
