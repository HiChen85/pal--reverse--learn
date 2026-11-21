package palgame

import (
	"reflect"
	"testing"

	"github.com/HiChen85/pal--reverse--learn/internal/paldata"
	"github.com/HiChen85/pal--reverse--learn/internal/palflow"
)

func TestPlanPathReturnsExecutableKeysWithoutWorldMutation(t *testing.T) {
	s := controlFixture(t, nil)
	before := s.Snapshot()
	goal := palflow.WorldPosition{X: 384, Y: 160}
	path, err := PlanPath(s.source(), before.World, goal, PathOptions{})
	if err != nil || len(path) != 4 || !reflect.DeepEqual(before, s.Snapshot()) {
		t.Fatal("path planning mutated world or lost shortest reachable path", path, err)
	}
	for _, key := range path {
		r, err := s.Move(key, nil)
		if err != nil || !r.Moved {
			t.Fatal("planned direction could not execute", err)
		}
	}
	if controlPosition(s.current.World) != goal {
		t.Fatal("direction path did not reach goal")
	}
}

func TestPlanPathAvoidsOtherTouchTriggersAndAllowsExplicitGoal(t *testing.T) {
	s := controlFixture(t, []paldata.Event{{X: 352, Y: 160, State: 1, TriggerMode: 4, Trigger: 1}})
	goal := palflow.WorldPosition{X: 384, Y: 160}
	path, err := PlanPath(s.source(), s.current.World, goal, PathOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range path {
		r, err := s.Move(key, nil)
		if err != nil || len(r.TriggeredEvents) != 0 || controlPosition(s.current.World) == (palflow.WorldPosition{X: 352, Y: 160}) {
			t.Fatal("path crossed an unrelated automatic trigger", err)
		}
	}
	s = controlFixture(t, []paldata.Event{{X: 352, Y: 160, State: 1, TriggerMode: 4, Trigger: 1}})
	path, err = PlanPath(s.source(), s.current.World, palflow.WorldPosition{X: 352, Y: 160}, PathOptions{AllowedTouchEvent: 1})
	if err != nil || len(path) != 2 {
		t.Fatal("explicit touch goal was not reachable", path, err)
	}
	for _, key := range path {
		if _, err := s.Move(key, nil); err != nil {
			t.Fatal(err)
		}
	}
	r, err := s.Interact(nil)
	if err != nil || !reflect.DeepEqual(r.TriggeredEvents, []uint16{1}) {
		t.Fatal("planned touch did not execute on following frame", err)
	}
}

func TestPlanPathRejectsStaleResourcesBlockedGoalsAndUnboundedSearch(t *testing.T) {
	s := controlFixture(t, []paldata.Event{{X: 352, Y: 160, State: 2}})
	goal := palflow.WorldPosition{X: 352, Y: 160}
	for _, options := range []PathOptions{{}, {MaxNodes: -1}, {MaxNodes: 65537}, {AllowedTouchEvent: 1}, {MaxNodes: 1}} {
		if _, err := PlanPath(s.source(), s.current.World, goal, options); err == nil {
			t.Fatal("invalid or blocked goal accepted", options)
		}
	}
	w := palflow.CloneWorld(s.current.World)
	w.SceneResources.MAPArchiveSHA256 = "other"
	if _, err := PlanPath(s.source(), w, palflow.WorldPosition{X: 336, Y: 168}, PathOptions{}); err == nil {
		t.Fatal("stale map identity accepted")
	}
}
