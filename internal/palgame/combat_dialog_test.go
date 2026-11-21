package palgame

import (
	"testing"

	"github.com/HiChen85/pal--reverse--learn/internal/paldata"
	"github.com/HiChen85/pal--reverse--learn/internal/palflow"
)

func TestNumericBattleZeroEntryPreBattleCallEndsOuterRNGDialog(t *testing.T) {
	source, w := numericCombatFixture(t, false)
	// No equipment or nonzero enemy hooks: palcombat intentionally has no VM
	// trace for these zero-entry calls. The pinned PAL_BattleMain still calls
	// RunTriggerScript(0) and its EndDialog before entering normal battle logic.
	result, err := source.RunBattle(w, 0, false, nil, palflow.Options{MaxSteps: 1000})
	if err != nil || !result.DialogEnded || len(result.Triggers) != 0 {
		t.Fatalf("zero-entry pre-battle EndDialog evidence missing: %+v %v", result, err)
	}
	source.Game.Scripts[1] = paldata.Instruction{Opcode: 0x3c, Operands: [3]uint16{42, 0, 1}}
	source.Game.Scripts[2] = paldata.Instruction{Opcode: 7}
	source.Game.Scripts[3] = paldata.Instruction{Opcode: 5}
	w.ScreenWave, w.WaveProgression, w.ScreenShakeTime = 10, 1, 10
	runner, _ := palflow.New(source, palflow.Options{MaxSteps: 1000})
	report, err := runner.Trace(palflow.Request{Entry: 1, World: w})
	if err != nil || !report.Completed || report.Trace[1].Combat == nil || !report.Trace[1].Combat.DialogEnded || report.Trace[2].Screen.Kind != "redraw" || report.State.DialogPlayingRNG || report.State.World.ScreenWave != 11 || report.State.World.ScreenShakeTime != 9 {
		t.Fatalf("battle left outer RNG dialog active: %+v %v", report, err)
	}

	// A rejected battle never reaches the pre-battle trigger boundary.
	source.Game.Scripts[2].Operands[0] = 99
	report, err = runner.Trace(palflow.Request{Entry: 1, World: w})
	if err == nil || report.Completed || report.Blocker.PC != 2 || !report.State.DialogPlayingRNG || report.State.World.ScreenWave != 10 || report.State.World.ScreenShakeTime != 10 {
		t.Fatal("failed battle cleared the committed outer dialog prefix", err)
	}
}
