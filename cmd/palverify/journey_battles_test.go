package main

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/HiChen85/pal--reverse--learn/internal/palcombat"
	"github.com/HiChen85/pal--reverse--learn/internal/palflow"
	"github.com/HiChen85/pal--reverse--learn/internal/palgame"
)

func TestJourneyBattleSummariesOrderAndDistinguishSuppliedOutcomes(t *testing.T) {
	touch := palflow.TraceStep{Opcode: 7, Operands: [3]uint16{1}, Combat: &palflow.CombatExecution{RawResult: 3, Reports: make([]palcombat.Report, 2), Work: 132}}
	touch.Combat.Reports[0].Draws = []palcombat.Draw{{Reason: "attack_all.critical"}, {Reason: "attack_all.critical"}, {Reason: "attack.critical"}}
	auto := palflow.TraceStep{Opcode: 7, Operands: [3]uint16{2}, Combat: &palflow.CombatExecution{RawResult: 0, Work: 130}}
	auto.Combat.Reports = []palcombat.Report{{Draws: []palcombat.Draw{{Reason: "script.attack_all.critical"}}}}
	search := palflow.TraceStep{Opcode: 7, Operands: [3]uint16{3}, Input: &palflow.InputUse{Input: palflow.Input{Kind: palflow.InputBattle, Value: palflow.BattleFled}}}
	control := palgame.ControlReport{SearchEventID: 7, Triggers: []palflow.Report{{Trace: []palflow.TraceStep{touch}}, {Trace: []palflow.TraceStep{search}}}, AutoSteps: []palflow.AutoStep{{Instruction: &auto}}}
	found := journeyBattles(nil, &control)
	if len(found) != 3 || found[0].EnemyTeam != 1 || found[0].Rounds != 2 || found[0].Work != 132 || found[0].Mode != "numeric" || found[1].EnemyTeam != 2 || found[1].RawResult != 0 || found[2].EnemyTeam != 3 || found[2].Mode != "supplied" || found[2].RawResult != 0xffff {
		t.Fatal("battle summary lost source, nesting or control execution order", found)
	}
	if found[0].AllTargetSweeps != 2 || found[1].AllTargetSweeps != 0 || found[2].AllTargetSweeps != 0 {
		t.Fatal("all-target evidence was inferred, lost or counted across battles", found)
	}
	legacy, err := json.Marshal(found[2])
	if err != nil || bytes.Contains(legacy, []byte("all_target_sweeps")) {
		t.Fatal("non-all battle changed its compact JSON shape", string(legacy), err)
	}
}

func TestJourneyBattleSummariesMarkFastVerification(t *testing.T) {
	r := palflow.Report{Trace: []palflow.TraceStep{{Opcode: 7, Operands: [3]uint16{15}, Combat: &palflow.CombatExecution{Policy: "fast_verify", RawResult: 3, Reports: make([]palcombat.Report, 1), Work: 131}}}}
	found := journeyBattles(&r, nil)
	if len(found) != 1 || found[0].Mode != "fast_verify" || found[0].EnemyTeam != 15 || found[0].RawResult != 3 || found[0].Rounds != 1 || found[0].Work != 131 {
		t.Fatal("compact report hid the simplified numeric policy", found)
	}
}
