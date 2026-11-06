// Package palflow inventories and executes a bounded PAL script model with
// world updates, auto scripts, collision sources and a numeric-combat bridge.
// Unsupported behavior stops execution; audiovisual requests are recorded.
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
	World       *WorldState       `json:"world,omitempty"`
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
	InputKey     = "key"
	InputShop    = "shop"
	BattleWin    = 0
	BattleLost   = 1
	BattleFled   = 2
)

type Input struct {
	Kind  string `json:"kind"`
	Value int    `json:"value"`
}
type InputUse struct {
	Generated bool   `json:"generated,omitempty"`
	Index     int    `json:"index"`
	Input     Input  `json:"input"`
	Meaning   string `json:"meaning"`
}
type Frame struct {
	PC        uint16 `json:"pc"`
	Entry     uint16 `json:"entry"`
	NextEntry uint16 `json:"next_entry"`
	EventID   uint16 `json:"event_id"`
}
type State struct {
	LastInlineReturn uint16             `json:"last_inline_return"`
	CombatWork       int                `json:"combat_work"`
	Random           ScriptRandomSource `json:"-"`
	TextBytes        int                `json:"text_bytes"`
	AutoSubsteps     int                `json:"auto_substeps"`
	WorldTicks       int                `json:"world_ticks"`
	CurEquipPart     int                `json:"cur_equip_part"`
	ScriptSuccess    bool               `json:"script_success"`
	PlayerSubsteps   int                `json:"player_substeps"`
	World            *WorldState        `json:"world,omitempty"`
	Music            uint16             `json:"music"`
	MusicKnown       bool               `json:"music_known"`
	BattleMusic      uint16             `json:"battle_music"`
	BattleMusicKnown bool               `json:"battle_music_known"`
	Frames           []Frame            `json:"frames"`
	IdleFrames       map[uint16]uint16  `json:"idle_frames"`
	LastEventID      uint16             `json:"last_event_id"`
	Finished         bool               `json:"finished"`
	ReturnedEntry    uint16             `json:"returned_entry"`
	InputIndex       int                `json:"input_index"`
	ExecutedSteps    int                `json:"executed_steps"`
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
type AudioRecord struct {
	Kind              string  `json:"kind"`
	ID                uint16  `json:"id"`
	Loop              bool    `json:"loop"`
	FadeSeconds       float64 `json:"fade_seconds"`
	PlaybackRequested bool    `json:"playback_requested"`
	Executed          bool    `json:"executed"`
}
type ScreenRecord struct {
	DelayMilliseconds          uint32 `json:"delay_milliseconds,omitempty"`
	Delay                      uint16 `json:"delay,omitempty"`
	Palette                    uint16 `json:"palette,omitempty"`
	Night                      bool   `json:"night"`
	Kind                       string `json:"kind"`
	ClearDialogRequested       bool   `json:"clear_dialog_requested"`
	OverworldDelayMilliseconds uint32 `json:"overworld_delay_milliseconds,omitempty"`
	Rendered                   bool   `json:"rendered"`
}
type TraceStep struct {
	Shop           *ShopRecord         `json:"shop,omitempty"`
	WorldUpdates   []WorldUpdateRecord `json:"world_updates,omitempty"`
	Combat         *CombatExecution    `json:"combat,omitempty"`
	RandomInputs   []InputUse          `json:"random_inputs,omitempty"`
	Children       []TraceStep         `json:"children,omitempty"`
	PartyWalk      *PartyWalkRecord    `json:"party_walk,omitempty"`
	AutoSteps      []AutoStep          `json:"auto_steps,omitempty"`
	Resources      []SpriteResource    `json:"resources,omitempty"`
	Ticks          uint16              `json:"ticks,omitempty"`
	PlayerSubsteps []PlayerSubstep     `json:"player_substeps,omitempty"`
	Audio          *AudioRecord        `json:"audio,omitempty"`
	Screen         *ScreenRecord       `json:"screen,omitempty"`
	Sequence       int                 `json:"sequence"`
	PC             uint16              `json:"pc"`
	Opcode         uint16              `json:"opcode"`
	Operands       [3]uint16           `json:"operands"`
	EventID        uint16              `json:"event_id"`
	Depth          int                 `json:"depth"`
	Effect         string              `json:"effect"`
	NextPC         uint16              `json:"next_pc"`
	Input          *InputUse           `json:"input,omitempty"`
	Message        *DisplayedMessage   `json:"message,omitempty"`
	Dialog         *DialogRecord       `json:"dialog,omitempty"`
	ReturnedEntry  *uint16             `json:"returned_entry,omitempty"`
}
type Blocker struct {
	Cause       *Blocker     `json:"cause,omitempty"`
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
	ModeledCount       int      `json:"modeled_count"`
	OperandConstraints []string `json:"operand_constraints,omitempty"`
	Opcode             uint16   `json:"opcode"`
	Hex                string   `json:"hex"`
	Name               string   `json:"name"`
	Count              int      `json:"count"`
	Supported          bool     `json:"supported"`
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
