package palflow

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

type conditionSceneSource struct {
	tables
	calls     []followerLoad
	fail      bool
	returnNil bool
	loaded    *WorldState
}

func (s *conditionSceneSource) LoadResources(world *WorldState, scene, players bool) (*WorldState, error) {
	s.calls = append(s.calls, followerLoad{Scene: scene, Players: players})
	w := CloneWorld(world)
	w.SceneResources = &SceneResourceState{SceneID: w.Scene, MapID: w.Scenes[w.Scene].Map}
	w.SceneLoadPending = false
	w.PartyOffsetX, w.PartyOffsetY = 160, 112
	w.SceneChanged = !w.SceneChanged // The instruction preserves its entering flag.
	if s.fail {
		return w, fmt.Errorf("unreadable replacement scene")
	}
	if s.returnNil {
		return nil, nil
	}
	s.loaded = w
	return w, nil
}

func TestConditionCurrentSceneMapReloadUsesIndependentFlags(t *testing.T) {
	for _, entering := range []bool{false, true} {
		w := conditionWorld()
		w.SceneChanged, w.SceneLoadPending = entering, true
		w.PlayerResources = []SpriteResource{{ID: 77}}
		before := CloneWorld(w)
		state := State{World: w, LastEventID: 45, ScriptSuccess: false, Frames: []Frame{{PC: 10, EventID: 45}}}
		source := &conditionSceneSource{}
		r, _ := New(source, Options{})
		step := TraceStep{}
		handled, err := r.evaluateConditions(&state, &step, ins(0x99, 0xffff, 66), nil)
		if !handled || err != nil || !reflect.DeepEqual(source.calls, []followerLoad{{Scene: true, Players: false}}) || state.World.Scenes[3].Map != 66 || state.World.SceneResources.MapID != 66 || state.World.SceneLoadPending || state.World.SceneChanged != entering || state.ScriptSuccess || state.LastEventID != 45 || state.Frames[0].PC != 11 {
			t.Fatalf("replacement scene flags/context differ: state=%+v calls=%+v err=%v", state, source.calls, err)
		}
		if !reflect.DeepEqual(w, before) || !reflect.DeepEqual(state.World.PlayerResources, before.PlayerResources) || state.World.Scenes[3].OnEnter != before.Scenes[3].OnEnter || len(step.Children) != 0 {
			t.Fatal("replacement mutated request, reloaded party or invoked OnEnter")
		}
		source.loaded.SceneResources.MapID = 99
		if state.World.SceneResources.MapID != 66 {
			t.Fatal("replacement source output aliases the committed world")
		}
	}
}

func TestConditionMapReloadFailuresAndExplicitSceneWritesAreAtomic(t *testing.T) {
	for _, tc := range []struct {
		name      string
		fail, nil bool
		pc        uint16
	}{{"decode", true, false, 10}, {"nil result", false, true, 10}, {"fallthrough overflow", false, false, 65535}} {
		t.Run(tc.name, func(t *testing.T) {
			state := State{World: conditionWorld(), Frames: []Frame{{PC: tc.pc}}}
			before := detachPartyState(state)
			step := TraceStep{Effect: "untouched"}
			source := &conditionSceneSource{fail: tc.fail, returnNil: tc.nil}
			r, _ := New(source, Options{})
			_, err := r.evaluateConditions(&state, &step, ins(0x99, 0xffff, 66), nil)
			if err == nil || !reflect.DeepEqual(state, before) || step.Effect != "untouched" {
				t.Fatalf("failed map replacement committed a prefix: state=%+v step=%+v err=%v", state, step, err)
			}
			if tc.pc == 65535 && len(source.calls) != 0 {
				t.Fatal("map resources were loaded after an invalid fallthrough")
			}
		})
	}
	state := State{World: conditionWorld(), Frames: []Frame{{PC: 10}}}
	source := &conditionSceneSource{fail: true}
	r, _ := New(source, Options{})
	_, err := r.evaluateConditions(&state, &TraceStep{}, ins(0x99, 3, 66), nil)
	if err != nil || len(source.calls) != 0 || state.World.Scenes[3].Map != 66 || state.World.SceneLoadPending {
		t.Fatal("explicit current scene ID must update the table without reloading", err)
	}
}

func TestConditionAutoTeleportRunsSynchronousChildWithSharedBudgets(t *testing.T) {
	for _, limit := range []int{6, 7} {
		state, r := autoFixture(autoSource{1: ins(0x38, 50), 10: ins(6, 100, 14), 11: ins(0x49, 0xffff, 0xffff), 12: ins(0x81, 2, 0, 13), 13: ins(1), 14: ins(0)})
		scene := state.World.Scenes[1]
		scene.OnTeleport = 10
		state.World.Scenes[1] = scene
		state.LastEventID, state.CurEquipPart, state.ScriptSuccess = 1, 3, true
		before := detachPartyState(state)
		r.options.MaxSteps = limit
		step := TraceStep{}
		_, err := r.evaluateTicks(&state, &step, ins(9, 1), []Input{{Kind: InputRandom, Value: 1}})
		if limit == 6 {
			if err == nil || !strings.Contains(err.Error(), "budget") || !reflect.DeepEqual(state, before) || !reflect.DeepEqual(step, TraceStep{}) {
				t.Fatalf("failed auto teleport committed child/input state: state=%+v step=%+v err=%v", state, step, err)
			}
			continue
		}
		if err != nil || len(step.AutoSteps) != 1 || step.AutoSteps[0].Instruction == nil {
			t.Fatalf("auto teleport hit frame-depth guard or lost evidence: step=%+v err=%v", step, err)
		}
		children := step.AutoSteps[0].Instruction.Children
		if len(children) != 4 || children[0].EventID != 1 || children[0].PC != 10 || children[3].PC != 13 || children[0].Input == nil || children[0].Input.Index != 0 || state.InputIndex != 1 || state.World.Events[1].State != -1 || state.World.Events[2].AutoScript != 2 || state.ScriptSuccess || state.LastEventID != 1 || state.CurEquipPart != -1 || state.ExecutedSteps != 4 || state.AutoSubsteps != 1 || state.WorldTicks != 1 || state.World.Scenes[1].OnTeleport != 10 || state.Frames[0].PC != 0x1235 {
			t.Fatalf("auto teleport context/returned entry/counters differ: state=%+v children=%+v", state, children)
		}
	}
}

func TestConditionTeleportSharesTextBudgetAndRollsBackWholeCall(t *testing.T) {
	source := tables{scripts: []Instruction{{}, ins(0x38), ins(0), ins(0xffff, 5), ins(0x49, 45, 65535), ins(0)}, messages: map[uint16]string{5: "abcd"}}
	w := conditionWorld()
	scene := w.Scenes[3]
	scene.OnTeleport = 3
	w.Scenes[3] = scene
	for _, limit := range []int{3, 4} {
		r, _ := New(source, Options{MaxTextBytes: limit})
		state := State{World: w, Frames: []Frame{{PC: 1}}, LastEventID: 45, CurEquipPart: 3}
		before := detachPartyState(state)
		step := TraceStep{}
		_, err := r.evaluateConditions(&state, &step, ins(0x38), nil)
		if limit == 3 {
			if err == nil || !reflect.DeepEqual(state, before) || !reflect.DeepEqual(step, TraceStep{}) {
				t.Fatal("teleport text failure committed a child prefix", err)
			}
		} else if err != nil || state.TextBytes != 4 || state.World.Events[45].State != -1 || state.CurEquipPart != -1 || len(step.Children) != 3 || step.Children[0].Message.ID != 5 || len(state.Frames) != 1 || state.Frames[0].PC != 2 {
			t.Fatalf("teleport text was lost, charged twice or leaked frames: state=%+v step=%+v err=%v", state, step, err)
		}
	}
}
