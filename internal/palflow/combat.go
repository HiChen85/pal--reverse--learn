package palflow

import (
	"fmt"
	"github.com/HiChen85/pal--reverse--learn/internal/palcombat"
	"maps"
)

// CombatSource computes an actual battle. The legacy explicit outcome input is
// retained only for declared standalone control-flow traces, where it is marked
// as a stub; game sessions select a numeric battle policy explicitly.
type CombatSource interface {
	RunBattle(world *WorldState, team uint16, boss bool, inputs []Input, options Options) (CombatExecution, error)
}

type CombatExecution struct {
	Context        *CombatContext     `json:"context,omitempty"`
	World          *WorldState        `json:"world,omitempty"`
	Result         int                `json:"story_branch_result"`
	RawResult      uint16             `json:"raw_result"`
	Reports        []palcombat.Report `json:"rounds"`
	Triggers       []Report           `json:"triggers,omitempty"`
	InputsConsumed int                `json:"inputs_consumed"`
	Work           int                `json:"work"`
}

type CombatContext struct {
	LastEventID   uint16            `json:"last_event_id"`
	ScriptSuccess bool              `json:"script_success"`
	IdleFrames    map[uint16]uint16 `json:"idle_frames"`
}
type ContextCombatSource interface {
	RunBattleWithContext(world *WorldState, team uint16, boss bool, inputs []Input, options Options, context CombatContext) (CombatExecution, error)
}

func (r *Runner) evaluateCombat(s *State, step *TraceStep, inst Instruction, inputs []Input) (bool, error) {
	if inst.Opcode != 7 || s.World == nil || s.World.BattleMode != "auto_attack" {
		return false, nil
	}
	source, ok := r.source.(CombatSource)
	if !ok {
		return true, fmt.Errorf("numeric combat source is missing")
	}
	remaining := r.options.MaxSteps - s.ExecutedSteps - s.PlayerSubsteps - s.WorldTicks - s.AutoSubsteps - s.CombatWork - 1
	if remaining < 1 {
		return true, fmt.Errorf("no shared instruction budget remains for battle")
	}
	options := r.options
	options.MaxSteps = remaining
	options.MaxCallDepth -= len(s.Frames)
	if options.MaxCallDepth < 1 {
		return true, fmt.Errorf("no trigger call depth remains for battle callbacks")
	}
	options.MaxTextBytes -= s.TextBytes
	if options.MaxTextBytes < 1 {
		return true, fmt.Errorf("no shared dialogue budget remains for battle")
	}
	frameIndex := len(s.Frames) - 1
	frame := s.Frames[frameIndex]
	next, err := fallthroughPC(frame.PC)
	if err != nil {
		return true, err
	}
	var execution CombatExecution
	if contextual, ok := r.source.(ContextCombatSource); ok {
		execution, err = contextual.RunBattleWithContext(CloneWorld(s.World), inst.Operands[0], inst.Operands[2] == 0, inputs[s.InputIndex:], options, CombatContext{LastEventID: s.LastEventID, ScriptSuccess: s.ScriptSuccess, IdleFrames: maps.Clone(s.IdleFrames)})
	} else {
		execution, err = source.RunBattle(CloneWorld(s.World), inst.Operands[0], inst.Operands[2] == 0, inputs[s.InputIndex:], options)
	}
	if err != nil {
		return true, err
	}
	if execution.World == nil || execution.Work < 1 || execution.Work > remaining || execution.InputsConsumed < 0 || execution.InputsConsumed > len(inputs)-s.InputIndex {
		return true, fmt.Errorf("battle source returned an invalid execution proposal")
	}
	if execution.RawResult == 1 && inst.Operands[1] == 0 {
		return true, fmt.Errorf("numeric battle ended in unhandled defeat: original game-over boundary reached; action was not committed")
	}
	text := 0
	for _, report := range execution.Triggers {
		text += report.TextBytes
	}
	if text > options.MaxTextBytes {
		return true, textLimitError{}
	}
	s.World = CloneWorld(execution.World)
	s.World.AutoBattle = false
	s.CombatWork += execution.Work
	s.InputIndex += execution.InputsConsumed
	s.TextBytes += text
	if execution.Context != nil {
		s.LastEventID = execution.Context.LastEventID
		s.ScriptSuccess = execution.Context.ScriptSuccess
		s.IdleFrames = maps.Clone(execution.Context.IdleFrames)
	}
	if execution.RawResult == 1 && inst.Operands[1] != 0 {
		next = inst.Operands[1]
	}
	if execution.RawResult == 0xffff && inst.Operands[2] != 0 {
		next = inst.Operands[2]
	}
	s.Frames[frameIndex].PC = next
	execution.World = nil // The committed result is in State.World exactly once.
	step.Combat = &execution
	step.Effect = "execute imported numeric combat and route its actual raw result"
	return true, nil
}
