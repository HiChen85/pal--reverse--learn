package main

import (
	"github.com/HiChen85/pal--reverse--learn/internal/palflow"
	"github.com/HiChen85/pal--reverse--learn/internal/palgame"
)

// Compact reports retain executed battle results separately from supplied
// branch outcomes. Nested waits, auto calls and party walks are visited too.
func journeyBattles(report *palflow.Report, control *palgame.ControlReport) []journeyBattleSummary {
	var found []journeyBattleSummary
	var trace func(palflow.TraceStep)
	var auto func(palflow.AutoStep)
	var reports func(palflow.Report)
	reports = func(r palflow.Report) {
		for _, s := range r.Trace {
			trace(s)
		}
	}
	auto = func(s palflow.AutoStep) {
		for _, child := range s.TriggerSteps {
			trace(child)
		}
		if s.Instruction != nil {
			trace(*s.Instruction)
		}
	}
	trace = func(s palflow.TraceStep) {
		for _, tick := range s.WorldUpdates {
			for _, child := range tick.TriggerSteps {
				trace(child)
			}
			for _, child := range tick.AutoSteps {
				auto(child)
			}
		}
		if s.Combat != nil {
			mode := "numeric"
			if s.Combat.Policy == "fast_verify" {
				mode = "fast_verify"
			}
			sweeps := 0
			// Each executed sweep records one critical draw, even under
			// Bravery. Script randomness uses separate reason strings.
			for _, round := range s.Combat.Reports {
				for _, draw := range round.Draws {
					if draw.Reason == "attack_all.critical" {
						sweeps++
					}
				}
			}
			found = append(found, journeyBattleSummary{Mode: mode, EnemyTeam: s.Operands[0], RawResult: s.Combat.RawResult, Rounds: len(s.Combat.Reports), Work: s.Combat.Work, AllTargetSweeps: sweeps})
			for _, callback := range s.Combat.Triggers {
				reports(callback)
			}
		} else if s.Input != nil && s.Input.Input.Kind == palflow.InputBattle {
			raw := uint16(3)
			if s.Input.Input.Value == palflow.BattleLost {
				raw = 1
			} else if s.Input.Input.Value == palflow.BattleFled {
				raw = 0xffff
			}
			found = append(found, journeyBattleSummary{Mode: "supplied", EnemyTeam: s.Operands[0], RawResult: raw})
		}
		for _, child := range s.Children {
			trace(child)
		}
		for _, child := range s.AutoSteps {
			auto(child)
		}
		if s.PartyWalk != nil {
			for _, movement := range s.PartyWalk.Steps {
				for _, child := range movement.AutoSteps {
					auto(child)
				}
			}
		}
	}
	if report != nil {
		reports(*report)
	}
	if control != nil {
		search := -1
		if control.SearchEventID != 0 && len(control.Triggers) > 0 {
			search = len(control.Triggers) - 1
		}
		for i, r := range control.Triggers {
			if i != search {
				reports(r)
			}
		}
		for _, s := range control.AutoSteps {
			auto(s)
		}
		if search >= 0 {
			reports(control.Triggers[search])
		}
	}
	return found
}
