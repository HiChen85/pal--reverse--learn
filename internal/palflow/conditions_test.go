package palflow

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func conditionWorld() *WorldState {
	w := &WorldState{
		Scene: 3, Direction: 0, ViewportX: 100, ViewportY: 100,
		PartyOffsetX: 160, PartyOffsetY: 112, PartyRoles: []uint16{0, 2},
		Events: map[uint16]WorldEvent{
			45: {X: 244, Y: 220, State: 1},
			46: {X: 244, Y: 220, State: 1},
			63: {State: 0}, 64: {State: 2},
		},
		Scenes: map[uint16]WorldScene{3: {Map: 10, EventStart: 44}, 4: {EventStart: 76}},
		Roles:  map[uint16][75]uint16{0: {3: 7, 11: 12, 12: 12}, 2: {3: 8, 15: 12}},
		Objects: map[uint16][7]uint16{
			10: {0: 0}, 11: {0: 98}, 12: {0: 99}, 13: {0: 100},
		},
	}
	return w
}

func conditionEvaluate(t *testing.T, w *WorldState, event uint16, inst Instruction, inputs ...Input) (State, TraceStep) {
	t.Helper()
	r, err := New(tables{}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	state := State{World: w, ScriptSuccess: true, Frames: []Frame{{PC: 10, EventID: event}}}
	step := TraceStep{}
	handled, err := r.evaluateConditions(&state, &step, inst, inputs)
	if !handled || err != nil {
		t.Fatalf("condition %04x: handled=%t err=%v", inst.Opcode, handled, err)
	}
	if step.Effect == "" {
		t.Fatal("condition omitted trace evidence")
	}
	return state, step
}

// These constants are independently derived from script.c's signed casts and
// comparisons. They cover both sides of each exact branch boundary.
func TestConditionsSourceGoldenPredicates(t *testing.T) {
	for _, tc := range []struct {
		name string
		inst Instruction
		edit func(*WorldState)
		pc   uint16
	}{
		{"state signed match", ins(0x94, 45, 65535, 20), func(w *WorldState) { e := w.Events[45]; e.State = -1; w.Events[45] = e }, 20},
		{"state signed mismatch", ins(0x94, 45, 65535, 20), nil, 11},
		{"state zero operand invokes self", ins(0x94, 0, 1, 20), nil, 20},
		{"state sentinel invokes self", ins(0x94, 65535, 1, 20), nil, 20},
		{"scene equal", ins(0x95, 3, 20), nil, 20},
		{"scene different", ins(0x95, 6, 20), nil, 11},
		{"name uses raw attribute", ins(0x79, 8, 20), nil, 20},
		{"name is not role ID", ins(0x79, 2, 20), nil, 11},
		{"equipment count equal", ins(0x86, 12, 3, 20), nil, 11},
		{"equipment count below", ins(0x86, 12, 4, 20), nil, 20},
		{"equipment zero threshold means one", ins(0x86, 99, 0, 20), nil, 20},
		{"equipment empty slots count", ins(0x86, 0, 9, 20), nil, 11},
		{"equipment empty slots count boundary", ins(0x86, 0, 10, 20), nil, 20},
		{"poison kind absent", ins(0x5d, 10, 20), nil, 20},
		{"poison zero matches empty slot", ins(0x5d, 0, 20), nil, 11},
		{"ordinary poison absent", ins(0x61, 20), nil, 20},
		{"ordinary poison level zero included", ins(0x61, 20), func(w *WorldState) { w.PlayerPoisons[0][0].ID = 10 }, 11},
		{"ordinary poison level98 included", ins(0x61, 20), func(w *WorldState) { w.PlayerPoisons[0][0].ID = 11 }, 11},
		{"equipment poison level99 ignored", ins(0x61, 20), func(w *WorldState) { w.PlayerPoisons[0][0].ID = 12 }, 20},
		{"poison above99 ignored", ins(0x61, 20), func(w *WorldState) { w.PlayerPoisons[0][0].ID = 13 }, 20},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := conditionWorld()
			if tc.edit != nil {
				tc.edit(w)
			}
			event := uint16(45)
			if tc.inst.Opcode == 0x5d || tc.inst.Opcode == 0x61 {
				event = 0
			}
			before := CloneWorld(w)
			state, _ := conditionEvaluate(t, w, event, tc.inst)
			if state.Frames[0].PC != tc.pc || !state.ScriptSuccess {
				t.Fatalf("PC/success = %d/%t", state.Frames[0].PC, state.ScriptSuccess)
			}
			if !reflect.DeepEqual(w, before) {
				t.Fatal("predicate mutated caller world")
			}
		})
	}
}

func TestConditionPoisonUsesFirstMatchingPartyIndex(t *testing.T) {
	w := conditionWorld()
	w.PartyRoles = []uint16{2, 0, 0}
	w.PlayerPoisons[0][0].ID = 10
	w.PlayerPoisons[0][2].ID = 10
	state, _ := conditionEvaluate(t, w, 0, ins(0x5d, 10, 20))
	if state.Frames[0].PC != 20 {
		t.Fatal("poison lookup must use first matching party index")
	}
	w.PlayerPoisons[0][1].ID = 10
	state, _ = conditionEvaluate(t, w, 0, ins(0x5d, 10, 20))
	if state.Frames[0].PC != 11 {
		t.Fatal("poison lookup used role index instead of party index")
	}
	state, _ = conditionEvaluate(t, w, 1, ins(0x5d, 0, 20))
	if state.Frames[0].PC != 20 {
		t.Fatal("role absent from party must not match empty poison slots")
	}
}

func TestConditionsFacingAndZoneSourceGoldens(t *testing.T) {
	for _, tc := range []struct {
		name  string
		op    uint16
		x, y  uint16
		state int16
		pc    uint16
	}{
		{"facing origin", 0x81, 244, 220, 1, 11},
		{"facing just inside", 0x81, 259, 220, 1, 11},
		{"facing horizontal boundary", 0x81, 260, 220, 1, 20},
		{"facing vertical boundary", 0x81, 244, 228, 1, 20},
		{"facing hidden event", 0x81, 244, 220, 0, 20},
		{"zone just inside", 0x83, 259, 220, 0, 11},
		{"zone boundary", 0x83, 260, 220, 1, 20},
		{"zone vertical boundary", 0x83, 244, 228, 1, 20},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := conditionWorld()
			w.Events[46] = WorldEvent{X: tc.x, Y: tc.y, State: tc.state}
			state, _ := conditionEvaluate(t, w, 45, ins(tc.op, 46, 0, 20))
			if state.Frames[0].PC != tc.pc || state.ScriptSuccess != (tc.pc == 11) {
				t.Fatalf("PC/success = %d/%t", state.Frames[0].PC, state.ScriptSuccess)
			}
		})
	}
	// The source adds an offset to the event, not to the party, and uses all
	// four isometric directions. The cached PartyX/Y fields are irrelevant.
	for dir, pos := range []WorldPosition{{X: 244, Y: 220}, {X: 244, Y: 204}, {X: 276, Y: 204}, {X: 276, Y: 220}} {
		w := conditionWorld()
		w.Direction, w.PartyX, w.PartyY = uint16(dir), -9999, -9999
		w.Events[46] = WorldEvent{X: pos.X, Y: pos.Y, State: 1}
		state, _ := conditionEvaluate(t, w, 45, ins(0x81, 46, 1, 20))
		if state.Frames[0].PC != 11 || state.World.Events[46].TriggerMode != 6 {
			t.Fatalf("direction %d did not face event or set touch mode", dir)
		}
	}
	for _, op := range []uint16{0x81, 0x83, 0x84} {
		for _, id := range []uint16{0, 44, 77, 65535} {
			w := conditionWorld()
			w.Events = nil
			state, _ := conditionEvaluate(t, w, 0, ins(op, id, 3, 20))
			if state.Frames[0].PC != 20 || state.ScriptSuccess {
				t.Fatalf("%04x did not reject raw event %d outside scene before dereferencing context", op, id)
			}
		}
	}
}

func TestConditionsDoNotRestorePriorScriptFailure(t *testing.T) {
	r, _ := New(tables{}, Options{})
	state := State{World: conditionWorld(), ScriptSuccess: false, Frames: []Frame{{PC: 10, EventID: 45}}}
	_, err := r.evaluateConditions(&state, &TraceStep{}, ins(0x81, 46, 0, 20), nil)
	if err != nil || state.ScriptSuccess || state.Frames[0].PC != 11 {
		t.Fatal("successful facing condition must preserve earlier script failure")
	}
}

func TestConditionsActualTableOperandMetadata(t *testing.T) {
	// Original SSS.MKF SHA256 38252bae8dabaebe9c780f00e8e6829bce56f89d862ecf9d44cc7b135047ed5d.
	// Scene 3's event 64 auto path at 0x1396 synchronizes from event 63.
	// Scene 6's battle-monster trigger path at 0x9ccf uses default vanish time.
	r, _ := New(tables{}, Options{})
	state := State{World: conditionWorld(), Frames: []Frame{{PC: 0x1396, EventID: 64}}}
	_, err := r.evaluateConditions(&state, &TraceStep{}, ins(0x6f, 63, 0, 0), nil)
	if err != nil || state.World.Events[64].State != 0 || state.Frames[0].PC != 0x1397 {
		t.Fatalf("scene3 synchronization = %+v / %v", state, err)
	}
	state.World.Events[155] = WorldEvent{State: 1}
	state.Frames[0] = Frame{PC: 0x9ccf, EventID: 155}
	_, err = r.evaluateConditions(&state, &TraceStep{}, ins(0x52, 0, 0, 0), nil)
	if err != nil || state.World.Events[155].State != -1 || state.World.Events[155].VanishTime != 800 || state.Frames[0].PC != 0x9cd0 {
		t.Fatalf("scene6 default vanish = %+v / %v", state.World.Events[155], err)
	}
}

func TestConditionsConfigurationAndSignedWorldWrites(t *testing.T) {
	w := conditionWorld()
	state, _ := conditionEvaluate(t, w, 45, ins(0x52, 65535))
	if state.World.Events[45].State != -1 || state.World.Events[45].VanishTime != -1 || w.Events[45].State != 1 {
		t.Fatal("vanish signed assignment or world detachment is incorrect")
	}
	w.Events[45] = WorldEvent{State: -32768}
	state, _ = conditionEvaluate(t, w, 45, ins(0x52, 32768))
	if state.World.Events[45].State != -32768 || state.World.Events[45].VanishTime != -32768 {
		t.Fatal("SHORT minimum negation must retain original bits")
	}
	state, _ = conditionEvaluate(t, w, 0, ins(0x54))
	if !state.World.NightPalette {
		t.Fatal("night flag not set")
	}
	state, _ = conditionEvaluate(t, state.World, 0, ins(0x53))
	if state.World.NightPalette {
		t.Fatal("day flag not set")
	}
	state, _ = conditionEvaluate(t, w, 0, ins(0x71, 65535, 65534))
	if state.World.ScreenWave != 65535 || state.World.WaveProgression != -2 {
		t.Fatal("wave signed fields incorrect")
	}
	w.NightPalette = true
	state, screen := conditionEvaluate(t, w, 0, ins(0x8b, 7))
	if state.World.Palette != 7 || !state.World.NightPalette || screen.Screen == nil {
		t.Fatal("palette request incorrectly changed night flag")
	}
	w.NeedToFadeIn = true
	_, screen = conditionEvaluate(t, w, 0, ins(0x8b, 8))
	if screen.Screen != nil {
		t.Fatal("pending fade must suppress immediate palette application")
	}
	state, _ = conditionEvaluate(t, w, 0, ins(0x99, 3, 99))
	if state.World.Scenes[3].Map != 99 || state.World.SceneLoadPending || w.Scenes[3].Map != 10 {
		t.Fatal("explicit scene map change must not reload or mutate request")
	}
	state, _ = conditionEvaluate(t, w, 0, ins(0x9a, 45, 46, 65534))
	if state.World.Events[45].State != -2 || state.World.Events[46].State != -2 || w.Events[46].State != 1 {
		t.Fatal("inclusive event state range incorrect")
	}
	state, _ = conditionEvaluate(t, w, 0, ins(0x9a, 46, 45, 65534))
	if !reflect.DeepEqual(state.World, w) {
		t.Fatal("descending source event range should perform zero writes")
	}
}

type conditionCollisionSource struct {
	tables
	blocked   bool
	fail      bool
	positions *[]WorldPosition
}

func (s conditionCollisionSource) IsObstacle(_ *WorldState, position WorldPosition, self uint16, events, checkRange bool) (bool, error) {
	if self != 0 || events || checkRange {
		return false, fmt.Errorf("item placement collision flags differ from source")
	}
	*s.positions = append(*s.positions, position)
	if s.fail {
		return false, fmt.Errorf("synthetic unreadable map")
	}
	return s.blocked, nil
}

func TestConditionItemPlacementUsesMapWithoutEventCollision(t *testing.T) {
	for dir, pos := range []WorldPosition{{X: 244, Y: 220}, {X: 244, Y: 204}, {X: 276, Y: 204}, {X: 276, Y: 220}} {
		for _, blocked := range []bool{false, true} {
			w := conditionWorld()
			w.Direction = uint16(dir)
			var positions []WorldPosition
			r, _ := New(conditionCollisionSource{positions: &positions, blocked: blocked}, Options{})
			state := State{World: w, ScriptSuccess: true, Frames: []Frame{{PC: 10}}}
			_, err := r.evaluateConditions(&state, &TraceStep{}, ins(0x84, 46, 65535, 20), nil)
			if err != nil || len(positions) != 1 || positions[0] != pos {
				t.Fatalf("placement dir%d obstacle=%t query=%+v err=%v", dir, blocked, positions, err)
			}
			if blocked {
				if state.Frames[0].PC != 20 || state.ScriptSuccess || state.World.Events[46] != w.Events[46] {
					t.Fatal("blocked placement changed event or did not branch")
				}
			} else if state.Frames[0].PC != 11 || state.World.Events[46].X != pos.X || state.World.Events[46].Y != pos.Y || state.World.Events[46].State != -1 {
				t.Fatal("clear placement did not store position and signed state")
			}
		}
	}
}

func TestConditionExplicitRandomSkipsAndDegenerateRanges(t *testing.T) {
	for _, tc := range []struct {
		bound    uint16
		value    int
		pc       uint16
		consumed int
	}{{0, 99, 11, 0}, {1, 99, 11, 0}, {2, 0, 11, 1}, {2, 1, 12, 1}, {65535, 65534, 65535, 1}} {
		w := conditionWorld()
		r, _ := New(tables{}, Options{})
		state := State{World: w, Frames: []Frame{{PC: 10}}}
		if tc.bound == 65535 {
			state.Frames[0].PC = 0
		}
		step := TraceStep{}
		_, err := r.evaluateConditions(&state, &step, ins(0xa2, tc.bound), []Input{{Kind: InputRandom, Value: tc.value}})
		if err != nil || state.Frames[0].PC != tc.pc || state.InputIndex != tc.consumed {
			t.Fatalf("bound%d result%d: pc%d consumed%d err%v", tc.bound, tc.value, state.Frames[0].PC, state.InputIndex, err)
		}
		if (step.Input != nil) != (tc.consumed == 1) {
			t.Fatal("random consumption evidence incorrect")
		}
	}
	state, _ := conditionEvaluate(t, nil, 0, ins(0xa2, 1))
	if state.World != nil || state.Frames[0].PC != 11 {
		t.Fatal("pure deterministic random skip should not require a world")
	}
}

func TestConditionsTeleportCallsUseLastEventAndNormalBudget(t *testing.T) {
	// The explicit call changes static last-event context to 2. Teleport from
	// caller event 1 must then reuse event 2, which changes event 2's state.
	src := tables{scripts: []Instruction{{}, ins(4, 5, 2), ins(0x38, 7), ins(0), {}, ins(0), {}, ins(0), ins(0x49, 65535, 65534), ins(1)}}
	w := conditionWorld()
	scene := w.Scenes[3]
	scene.OnTeleport = 8
	w.Scenes[3] = scene
	w.Events[2] = WorldEvent{State: 1}
	report := run(t, src, Request{Entry: 1, EventID: 1, World: w})
	if !reflect.DeepEqual(pcList(report), []uint16{1, 5, 2, 3}) || report.State.World.Events[2].State != -2 || report.State.LastEventID != 2 || report.State.ReturnedEntry != 1 {
		t.Fatalf("teleport nested semantics: %+v", report)
	}
	children := report.Trace[2].Children
	if len(children) != 2 || children[0].PC != 8 || children[1].PC != 9 || children[0].EventID != 2 || children[0].Depth != 2 || report.State.ExecutedSteps != 6 {
		t.Fatal("teleport missing nested evidence or normal instruction charge")
	}
	r, _ := New(src, Options{MaxSteps: 3})
	limited, err := r.Trace(Request{Entry: 1, EventID: 1, World: w})
	if err == nil || limited.Blocker.Cause == nil || limited.Blocker.Cause.Kind != "step_limit" || limited.State.ExecutedSteps != 2 || limited.State.World.Events[2].State != 1 || len(limited.Trace) != 2 {
		t.Fatal("teleport child escaped combined budget or committed a partial synchronous call")
	}
	r, _ = New(src, Options{MaxCallDepth: 1})
	state := State{World: w, LastEventID: 2, Frames: []Frame{{PC: 2, EventID: 1}}}
	_, err = r.evaluateConditions(&state, &TraceStep{}, ins(0x38, 7), nil)
	if err == nil || state.Frames[0].PC != 2 || len(state.Frames) != 1 {
		t.Fatal("teleport call-depth failure did not remain atomic")
	}
	for _, battle := range []bool{false, true} {
		w := conditionWorld()
		w.InBattle = battle
		if battle {
			w.Scenes = nil
		}
		state, _ := conditionEvaluate(t, w, 0, ins(0x38, 20))
		if state.Frames[0].PC != 20 || state.ScriptSuccess {
			t.Fatal("failed teleport did not jump and mark failure")
		}
	}
}

func TestConditionReferenceRandomDivisionTailGoldens(t *testing.T) {
	// util.c's actual integer division differs from its documented ideal range.
	// With lrand() == INT_MAX, the divisors for these bounds are respectively
	// 1073741823, 715827882, 306783378, 21474836, 46272 and 32768.
	// The 32-bit LCG reaches INT_MAX from pre-update seed -1493846240.
	for _, tc := range []struct {
		bound uint16
		tail  int
	}{{2, 2}, {3, 3}, {7, 7}, {100, 100}, {46409, 46410}, {65535, 65535}} {
		r, _ := New(tables{}, Options{})
		state := State{Frames: []Frame{{PC: 10}}}
		before := state
		before.Frames = append([]Frame(nil), state.Frames...)
		step := TraceStep{}
		handled, err := r.evaluateConditions(&state, &step, ins(0xa2, tc.bound), []Input{{Kind: InputRandom, Value: tc.tail}})
		if tc.bound == 65535 {
			if !handled || err == nil || !strings.Contains(err.Error(), "program counter") || !reflect.DeepEqual(state, before) || !reflect.DeepEqual(step, TraceStep{}) {
				t.Fatalf("reachable tail must fail atomically on PC overflow, not input range: state=%+v step=%+v err=%v", state, step, err)
			}
			continue
		}
		if !handled || err != nil || state.Frames[0].PC != uint16(11+tc.tail) || state.InputIndex != 1 || step.Input == nil || step.Input.Input.Value != tc.tail {
			t.Fatalf("bound %d reference tail %d: state=%+v step=%+v err=%v", tc.bound, tc.tail, state, step, err)
		}
		before, beforeStep := state, step
		before.Frames = append([]Frame(nil), state.Frames...)
		_, err = r.evaluateConditions(&state, &step, ins(0xa2, tc.bound), []Input{{Kind: InputRandom, Value: 0}, {Kind: InputRandom, Value: tc.tail + 1}})
		if err == nil || !strings.Contains(err.Error(), "reference random result") || !reflect.DeepEqual(state, before) || !reflect.DeepEqual(step, beforeStep) {
			t.Fatalf("bound %d accepted an unreachable tail or changed state/evidence: state=%+v step=%+v err=%v", tc.bound, state, step, err)
		}
	}
}

func TestConditionsFailuresRollbackWorldFramesInputsAndEvidence(t *testing.T) {
	for _, tc := range []struct {
		name   string
		inst   Instruction
		pc     uint16
		inputs []Input
		edit   func(*WorldState)
	}{
		{"missing target", ins(0x94, 47, 0, 20), 10, nil, nil},
		{"special addressing", ins(0x94, 0x9000, 0, 20), 10, nil, nil},
		{"missing party", ins(0x86, 12, 1, 20), 10, nil, func(w *WorldState) { w.PartyRoles = nil }},
		{"missing role", ins(0x86, 12, 1, 20), 10, nil, func(w *WorldState) { delete(w.Roles, 2) }},
		{"missing poison object", ins(0x61, 20), 10, nil, func(w *WorldState) { w.PlayerPoisons[0][0].ID = 99 }},
		{"unknown membership boundary", ins(0x81, 46, 0, 20), 10, nil, func(w *WorldState) { delete(w.Scenes, 4) }},
		{"descending membership boundary", ins(0x83, 46, 0, 20), 10, nil, func(w *WorldState) { w.Scenes[4] = WorldScene{EventStart: 1} }},
		{"unknown placement map", ins(0x84, 46, 1, 20), 10, nil, nil},
		{"current map reload", ins(0x99, 65535, 7), 10, nil, nil},
		{"range partial update", ins(0x9a, 45, 47, 65535), 10, nil, nil},
		{"range event zero", ins(0x9a, 0, 2, 1), 10, nil, nil},
		{"fallthrough overflow", ins(0x52, 5), 65535, nil, nil},
		{"random absent", ins(0xa2, 10), 10, nil, nil},
		{"random wrong kind", ins(0xa2, 10), 10, []Input{{Kind: InputConfirm, Value: 1}}, nil},
		{"random negative", ins(0xa2, 10), 10, []Input{{Kind: InputRandom, Value: -1}}, nil},
		{"random upper bound", ins(0xa2, 10), 10, []Input{{Kind: InputRandom, Value: 11}}, nil},
		{"random overflow after consume", ins(0xa2, 3), 65534, []Input{{Kind: InputRandom, Value: 1}}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := conditionWorld()
			if tc.edit != nil {
				tc.edit(w)
			}
			r, _ := New(tables{}, Options{})
			event := uint16(45)
			if tc.inst.Opcode == 0x61 {
				event = 0
			}
			state := State{World: w, ScriptSuccess: true, Frames: []Frame{{PC: tc.pc, EventID: event}}, InputIndex: 0}
			before := state
			before.World = CloneWorld(w)
			before.Frames = append([]Frame(nil), state.Frames...)
			originalWorld := state.World
			originalFrame := &state.Frames[0]
			step := TraceStep{PC: 99, Effect: "unchanged"}
			beforeStep := step
			handled, err := r.evaluateConditions(&state, &step, tc.inst, tc.inputs)
			if !handled || err == nil {
				t.Fatalf("expected block, got %t/%v", handled, err)
			}
			if !reflect.DeepEqual(state, before) || state.World != originalWorld || &state.Frames[0] != originalFrame || !reflect.DeepEqual(step, beforeStep) {
				t.Fatal("failed condition committed state, input or evidence")
			}
		})
	}
}

func TestConditionsRegistryPreservesUnmodeledBattleAndEndingBlocks(t *testing.T) {
	for _, op := range []uint16{0x39, 0x3a, 0x3f, 0x44, 0x4c, 0x4d, 0x5b, 0x5e, 0x60, 0x64, 0x92, 0x96, 0x9f, 0xa0, 0xa4, 0xa5, 0xa6} {
		if conditionsOpcodeName(op) != "" {
			t.Fatalf("unmodeled opcode %04x was advertised", op)
		}
	}
	r, _ := New(tables{}, Options{})
	if handled, err := r.evaluateConditions(nil, nil, ins(0x39), nil); handled || err != nil {
		t.Fatal("condition helper swallowed battle instruction")
	}
	if handled, err := r.evaluateConditions(nil, nil, ins(0x94), nil); !handled || err == nil {
		t.Fatal("condition helper accepted absent context")
	}
}
