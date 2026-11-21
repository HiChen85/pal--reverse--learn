package palgame

import (
	"reflect"
	"testing"

	"github.com/HiChen85/pal--reverse--learn/internal/palcombat"
	"github.com/HiChen85/pal--reverse--learn/internal/paldata"
	"github.com/HiChen85/pal--reverse--learn/internal/palflow"
)

func TestZeroEntryBattleCallbacksRestoreContextAtExactWorkLimit(t *testing.T) {
	source, w := numericCombatFixture(t, false)
	// The trailing explicit zero extends the source's fixed slot range, even
	// though it adds no enemy record. Both pre- and post-battle passes visit it.
	source.Runtime.EnemyTeams[0] = [5]uint16{10, 0, 0xffff, 0xffff, 0xffff}
	before := palflow.CloneWorld(w)
	context := palflow.CombatContext{LastEventID: 9, ScriptSuccess: false, IdleFrames: map[uint16]uint16{3: 4}}
	result, err := source.RunBattleWithContext(w, 0, false, nil, palflow.Options{MaxSteps: 1}, context)
	if err != nil {
		t.Fatal(err)
	}
	if result.RawResult != 3 || result.Context == nil || result.Context.LastEventID != 1 || !result.Context.ScriptSuccess || !reflect.DeepEqual(result.Context.IdleFrames, context.IdleFrames) || result.Work != 1 || len(result.Triggers) != 0 || !result.DialogEnded || !reflect.DeepEqual(w, before) {
		t.Fatalf("zero-entry context/work mismatch: %+v", result)
	}
	if context.LastEventID != 9 || context.ScriptSuccess || context.IdleFrames[3] != 4 {
		t.Fatal("context passed by the caller was mutated")
	}
}

func TestZeroPostBattleEntryResetsPreviousHookFailure(t *testing.T) {
	source, w := numericCombatFixture(t, false)
	source.Runtime.EnemyTeams[0] = [5]uint16{10, 0, 0xffff, 0xffff, 0xffff}
	source.Game.Objects[10][3], w.Objects[10] = 14, [7]uint16{3: 14}
	// Teleporting during battle fails its script predicate and jumps to stop.
	source.Game.Scripts[14] = paldata.Instruction{Opcode: 0x38, Operands: [3]uint16{15}}
	// Slot zero's end hook returns ScriptSuccess=false. Slot one's zero entry
	// then starts and ends a real invocation, resetting it to true.
	result, err := source.RunBattle(w, 0, false, nil, palflow.Options{MaxSteps: 3})
	if err != nil {
		t.Fatal(err)
	}
	if result.Context == nil || result.Context.LastEventID != 1 || !result.Context.ScriptSuccess || result.Work != 3 || len(result.Triggers) != 1 || result.Triggers[0].State.ScriptSuccess || result.Triggers[0].State.LastEventID != 0 || result.World.Cash != 3 || result.World.Experience[0][0].Exp != 2 {
		t.Fatalf("zero end hook did not reset a preceding failed hook: %+v", result)
	}
}

func TestZeroEntryBattleContextDoesNotEscapeLaterFailedHook(t *testing.T) {
	source, w := numericCombatFixture(t, false)
	source.Runtime.EnemyTeams[0] = [5]uint16{10, 0, 0xffff, 0xffff, 0xffff}
	source.Game.Objects[10][3], w.Objects[10] = 14, [7]uint16{3: 14}
	source.Game.Scripts[14] = paldata.Instruction{Opcode: 0xdead}
	before := palflow.CloneWorld(w)
	runner, err := palflow.New(source, palflow.Options{MaxSteps: 100})
	if err != nil {
		t.Fatal(err)
	}
	report, err := runner.Trace(palflow.Request{Entry: 1, EventID: 7, World: w})
	if err == nil || report.Completed || report.State.LastEventID != 7 || report.State.ExecutedSteps != 0 || report.State.CombatWork != 0 || report.InputsConsumed != 0 || !reflect.DeepEqual(report.State.World, before) || !reflect.DeepEqual(w, before) {
		t.Fatalf("failed battle published earlier zero-entry context: %+v error=%v", report, err)
	}
}

func TestZeroEntryCallbackCanFinishAtExactDialogueBudget(t *testing.T) {
	source, w := numericCombatFixture(t, false)
	w.InBattle = true
	party, err := combatParty(w)
	if err != nil {
		t.Fatal(err)
	}
	random := &combatRandom{state: 1}
	callbacks := &combatCallbacks{source: source, world: w, random: random, options: palflow.Options{MaxSteps: 1, MaxCallDepth: 32, MaxTextBytes: 1}, work: 1, textBytes: 1, context: palflow.CombatContext{LastEventID: 7, ScriptSuccess: false}}
	engine, err := palcombat.New(palcombat.Config{Tables: source.Runtime, Game: source.Game, Party: party, TeamID: 0, Random: random, Callbacks: callbacks, Inventory: map[uint16]uint16{}})
	if err != nil {
		t.Fatal(err)
	}
	txRaw, err := callbacks.Begin(engine.Snapshot())
	if err != nil {
		t.Fatal(err)
	}
	tx := txRaw.(*combatTransaction)
	snapshot := engine.Snapshot()
	result, err := tx.Run(palcombat.ScriptRequest{Entry: 0, Invoker: 0}, &snapshot, random)
	if err != nil || !result.Success || result.NextEntry != 0 || callbacks.work != 1 || tx.textBytes != 1 || tx.context.LastEventID != 0 || !tx.context.ScriptSuccess || len(callbacks.triggers) != 0 {
		t.Fatalf("instruction-free callback was charged at an exhausted budget: %+v error=%v", result, err)
	}
	tx.Rollback()
	if callbacks.context.LastEventID != 7 || callbacks.context.ScriptSuccess {
		t.Fatal("zero-entry proposal changed uncommitted callback context")
	}
}
