package palflow

// Messages in synchronous interpreted/auto callbacks are execution evidence,
// too. They are already charged by the shared State.TextBytes counter; this
// traversal only builds the display sequence and marks external battle results.
func collectStepEvidence(report *Report, step TraceStep) {
	for _, tick := range step.WorldUpdates {
		for _, child := range tick.TriggerSteps {
			collectStepEvidence(report, child)
		}
		for _, auto := range tick.AutoSteps {
			collectAutoEvidence(report, auto)
		}
	}
	if step.Combat != nil {
		for _, trigger := range step.Combat.Triggers {
			report.DialogueIDs = append(report.DialogueIDs, trigger.DialogueIDs...)
		}
	}
	if step.Message != nil {
		report.DialogueIDs = append(report.DialogueIDs, step.Message.ID)
	}
	if step.Input != nil && step.Input.Input.Kind == InputBattle {
		if !report.BattleOutcomesStubbed {
			report.Assumptions = append(report.Assumptions, "Battle outcomes were supplied externally; combat was not executed for those outcomes.")
		}
		report.BattleOutcomesStubbed = true
	}
	for _, child := range step.Children {
		collectStepEvidence(report, child)
	}
	for _, auto := range step.AutoSteps {
		collectAutoEvidence(report, auto)
	}
	if step.PartyWalk != nil {
		for _, movement := range step.PartyWalk.Steps {
			for _, auto := range movement.AutoSteps {
				collectAutoEvidence(report, auto)
			}
		}
	}
}

func collectAutoEvidence(report *Report, auto AutoStep) {
	for _, child := range auto.TriggerSteps {
		collectStepEvidence(report, child)
	}
	if auto.Instruction != nil {
		collectStepEvidence(report, *auto.Instruction)
	}
}
