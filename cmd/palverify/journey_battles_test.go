package main

import (
	"github.com/HiChen85/pal--reverse--learn/internal/palcombat"
	"github.com/HiChen85/pal--reverse--learn/internal/palflow"
	"github.com/HiChen85/pal--reverse--learn/internal/palgame"
	"testing"
)

func TestJourneyBattleSummariesOrderAndDistinguishSuppliedOutcomes(t *testing.T) {
	touch := palflow.TraceStep{Opcode: 7, Operands: [3]uint16{1}, Combat: &palflow.CombatExecution{RawResult: 3, Reports: make([]palcombat.Report, 2), Work: 132}}
	auto := palflow.TraceStep{Opcode: 7, Operands: [3]uint16{2}, Combat: &palflow.CombatExecution{RawResult: 0, Work: 130}}
	search := palflow.TraceStep{Opcode: 7, Operands: [3]uint16{3}, Input: &palflow.InputUse{Input: palflow.Input{Kind: palflow.InputBattle, Value: palflow.BattleFled}}}
	control := palgame.ControlReport{SearchEventID: 7, Triggers: []palflow.Report{{Trace: []palflow.TraceStep{touch}}, {Trace: []palflow.TraceStep{search}}}, AutoSteps: []palflow.AutoStep{{Instruction: &auto}}}
	found := journeyBattles(nil, &control)
	if len(found) != 3 || found[0].EnemyTeam != 1 || found[0].Rounds != 2 || found[0].Work != 132 || found[0].Mode != "numeric" || found[1].EnemyTeam != 2 || found[1].RawResult != 0 || found[2].EnemyTeam != 3 || found[2].Mode != "supplied" || found[2].RawResult != 0xffff {
		t.Fatal("battle summary lost source, nesting or control execution order", found)
	}
}
