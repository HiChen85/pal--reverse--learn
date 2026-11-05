// Package palflow inventories and traces a deliberately partial PAL trigger
// script model. It does not implement world updates, auto scripts, rendering,
// battle simulation, or main-story/side-quest verification.
package palflow

import "fmt"

type Instruction struct {
	Opcode   uint16    `json:"opcode"`
	Operands [3]uint16 `json:"operands"`
}

// Source provides the original table's direct indexes, including index zero.
// Trace never executes instruction zero: a zero script address means return.
type Source interface {
	Instruction(pc uint16) (Instruction, error)
	Message(id uint16) (string, error)
}
type Options struct {
	MaxTextBytes int `json:"max_text_bytes"`
	MaxSteps     int `json:"max_steps"`
	MaxCallDepth int `json:"max_call_depth"`
}
type Request struct {
	Entry       uint16            `json:"entry"`
	EventID     uint16            `json:"event_id"`
	LastEventID uint16            `json:"last_event_id"`
	IdleFrames  map[uint16]uint16 `json:"idle_frames"`
	Inputs      []Input           `json:"inputs"`
}

const (
	InputRandom  = "random"
	InputConfirm = "confirm"
	InputBattle  = "battle"
	BattleWin    = 0
	BattleLost   = 1
	BattleFled   = 2
)

type Input struct {
	Kind  string `json:"kind"`
	Value int    `json:"value"`
}
type InputUse struct {
	Index   int    `json:"index"`
	Input   Input  `json:"input"`
	Meaning string `json:"meaning"`
}
type Frame struct {
	PC        uint16 `json:"pc"`
	Entry     uint16 `json:"entry"`
	NextEntry uint16 `json:"next_entry"`
	EventID   uint16 `json:"event_id"`
}
type State struct {
	Frames        []Frame           `json:"frames"`
	IdleFrames    map[uint16]uint16 `json:"idle_frames"`
	LastEventID   uint16            `json:"last_event_id"`
	Finished      bool              `json:"finished"`
	ReturnedEntry uint16            `json:"returned_entry"`
	InputIndex    int               `json:"input_index"`
	ExecutedSteps int               `json:"executed_steps"`
}
type DisplayedMessage struct {
	ID   uint16 `json:"id"`
	Text string `json:"text"`
}
type DialogRecord struct {
	Position string `json:"position"`
	Color    uint8  `json:"color"`
	Portrait uint16 `json:"portrait"`
	RNG      bool   `json:"rng"`
	Rendered bool   `json:"rendered"`
}
type TraceStep struct {
	Sequence      int               `json:"sequence"`
	PC            uint16            `json:"pc"`
	Opcode        uint16            `json:"opcode"`
	Operands      [3]uint16         `json:"operands"`
	EventID       uint16            `json:"event_id"`
	Depth         int               `json:"depth"`
	Effect        string            `json:"effect"`
	NextPC        uint16            `json:"next_pc"`
	Input         *InputUse         `json:"input,omitempty"`
	Message       *DisplayedMessage `json:"message,omitempty"`
	Dialog        *DialogRecord     `json:"dialog,omitempty"`
	ReturnedEntry *uint16           `json:"returned_entry,omitempty"`
}
type Blocker struct {
	Kind        string       `json:"kind"`
	Reason      string       `json:"reason"`
	PC          uint16       `json:"pc"`
	Opcode      uint16       `json:"opcode"`
	Instruction *Instruction `json:"instruction,omitempty"`
	EventID     uint16       `json:"event_id"`
	Depth       int          `json:"depth"`
}

func (b *Blocker) Error() string {
	return fmt.Sprintf("PAL trigger blocked at %04x: %s", b.PC, b.Reason)
}

type Report struct {
	TextBytes             int         `json:"text_bytes"`
	Completed             bool        `json:"completed"` // Only the requested partial-model invocation stopped.
	DialogueIDs           []uint16    `json:"dialogue_ids"`
	Assumptions           []string    `json:"assumptions"`
	Status                string      `json:"status"`
	Coverage              string      `json:"coverage"`
	Trace                 []TraceStep `json:"trace"`
	State                 State       `json:"state"`
	Blocker               *Blocker    `json:"blocker,omitempty"`
	InputsConsumed        int         `json:"inputs_consumed"`
	UnusedInputs          []Input     `json:"unused_inputs,omitempty"`
	BattleOutcomesStubbed bool        `json:"battle_outcomes_stubbed"`
	Limitations           []string    `json:"limitations"`
}
type OpcodeCount struct {
	Opcode    uint16 `json:"opcode"`
	Hex       string `json:"hex"`
	Name      string `json:"name"`
	Count     int    `json:"count"`
	Supported bool   `json:"supported"`
}
type InventoryReport struct {
	Instructions        int           `json:"instructions"`
	ModeledInstructions int           `json:"modeled_instructions"`
	Opcodes             []OpcodeCount `json:"opcodes"`
}
type Edge struct {
	From uint16 `json:"from"`
	To   uint16 `json:"to"`
	Kind string `json:"kind"`
}
type StaticBlocker struct {
	PC     uint16 `json:"pc"`
	Opcode uint16 `json:"opcode"`
	Reason string `json:"reason"`
}
type AuditReport struct {
	Coverage    string          `json:"coverage"`
	Roots       []uint16        `json:"roots"`
	Reachable   []uint16        `json:"reachable"`
	Edges       []Edge          `json:"edges"`
	Opcodes     []OpcodeCount   `json:"opcodes"`
	Blockers    []StaticBlocker `json:"blockers"`
	Limitations []string        `json:"limitations"`
}
