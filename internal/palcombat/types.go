// Package palcombat implements a bounded, independently written PAL1 classic
// combat model. It uses imported numeric tables and explicit player commands.
// Unsupported scripts and rules fail visibly; they never imply a victory.
package palcombat

import (
	"fmt"

	"github.com/HiChen85/pal--reverse--learn/internal/paldata"
)

type Kind string

const (
	Attack  Kind = "attack"
	Defend  Kind = "defend"
	Flee    Kind = "flee"
	Magic   Kind = "magic"
	UseItem Kind = "item"
	Pass    Kind = "pass"
)

type Outcome string

const (
	Ongoing    Outcome = "ongoing"
	Victory    Outcome = "victory"
	Defeat     Outcome = "defeat"
	Escaped    Outcome = "escaped"
	Terminated Outcome = "terminated"
)

// These indexes are the PAL_CLASSIC status layout. Index 1 is paralysis;
// modern SDLPal's slow status has a different rule and is not accepted here.
const (
	Confused = iota
	Paralyzed
	Sleep
	Silence
	Puppet
	Bravery
	Protect
	Haste
	DualAttack
	StatusCount
)

// RandomSource must return a value in [0,n). The caller supplies and owns it.
// Invalid commands are rejected before it is called. A source error rolls back
// combat state, but values already obtained from an external source stay used;
// the returned Report retains those draws for inspection/replay.
type RandomSource interface{ Intn(n int) (int, error) }

// RecordedRandom is an explicit finite input tape, with no hidden RNG fallback.
// Exhaustion is an error. Values are range checked rather than reduced modulo n.
type RecordedRandom struct {
	Values []int
	Index  int
}

func (r *RecordedRandom) Intn(n int) (int, error) {
	if r == nil || r.Index < 0 || r.Index >= len(r.Values) {
		return 0, fmt.Errorf("palcombat: random tape exhausted")
	}
	v := r.Values[r.Index]
	if v < 0 || v >= n {
		return 0, fmt.Errorf("palcombat: random tape value %d at %d outside [0,%d)", v, r.Index, n)
	}
	r.Index++
	return v, nil
}

type Draw struct {
	Reason string `json:"reason"`
	N      int    `json:"n"`
	Value  int    `json:"value"`
	Error  string `json:"error,omitempty"`
}

// Callbacks binds the imported script VM to a combat round. Begin and Run must
// stage changes privately. Commit must atomically publish them, or publish
// nothing on error. A failed round always calls Rollback, including after a
// random-source failure; callback code must never commit the world from Run.
// Run may edit individual snapshot values, but must not replace its fighter
// slices or change engine-owned outcome, round, rewards or script continuations.
type Callbacks interface {
	Begin(Snapshot) (ScriptTransaction, error)
}
type ScriptTransaction interface {
	Run(ScriptRequest, *Snapshot, RandomSource) (ScriptResult, error)
	Commit(Snapshot) error
	Rollback()
}
type ScriptRequest struct {
	Hook            string
	ObjectID, Entry uint16
	// Invoker is a role ID for party scripts, an original team slot for
	// enemy scripts, and 0xffff for an all-target item/offensive magic.
	Invoker    uint16
	Target     Target
	PoisonSlot int
}
type ScriptResult struct {
	NextEntry uint16
	Success   bool
	Trace     []ScriptTrace
	// Terminal outcomes are explicit VM results. Ongoing is a no-op; pause
	// and pre-battle raw results are VM phase state and must not end combat.
	ForcedOutcome *Outcome
	// ObjectChanges implements object words 2..6 (0x0090). MagicBaseDamage
	// uses magic table indexes (0x0057), not magic object IDs.
	ObjectChanges   map[uint16][7]uint16
	MagicBaseDamage map[uint16]uint16
}

type Poison struct{ ID, Script uint16 }

// Effects contains all six equipment slots and the seventh temporary slot,
// with the same 75 attribute indexes as Role.Raw. A non-nil pointer certifies
// that the caller has actually evaluated the equipped objects' scripts.
type Effects [7][paldata.RoleWords]uint16

type Player struct {
	Role             paldata.Role        `json:"role"`
	EquipmentEffects *Effects            `json:"equipment_effects,omitempty"`
	Status           [StatusCount]uint16 `json:"status"`
	Poisons          [16]Poison          `json:"poisons"`
	Experience       uint32              `json:"experience"`
	// Counters are reset on battle entry, as in the classic engine.
	Counters Counters `json:"counters"`
	// Nil preserves primary experience and reports omitted hidden growth.
	// Supply all seven saved tracks for the full numeric victory calculation.
	HiddenExperience *HiddenExperience `json:"hidden_experience,omitempty"`
}

type HiddenExperience struct{ Health, Magic, Attack, MagicPower, Defense, Dexterity, Flee paldata.Experience }

type Counters struct{ Health, Magic, Attack, MagicPower, Defense, Dexterity, Flee uint32 }

type PlayerState struct {
	Player
	Defending bool `json:"defending"`
}

type EnemyState struct {
	Slot                        int                 `json:"slot"`
	ObjectID                    uint16              `json:"object_id"`
	Enemy                       paldata.Enemy       `json:"enemy"`
	HP                          int                 `json:"hp"`
	Status                      [StatusCount]uint16 `json:"status"`
	Poisons                     [16]Poison          `json:"poisons"`
	Rewarded                    bool                `json:"rewarded"`
	TurnStart, BattleEnd, Ready uint16
}

type Snapshot struct {
	Round           int                  `json:"round"`
	Outcome         Outcome              `json:"outcome"`
	TeamID          uint16               `json:"team_id"`
	Party           []PlayerState        `json:"party"`
	Enemies         []EnemyState         `json:"enemies"`
	Inventory       map[uint16]uint16    `json:"inventory"`
	Cash            uint32               `json:"cash"`
	Earned          Rewards              `json:"earned"`
	Started         bool                 `json:"started"`
	HidingTime      int                  `json:"hiding_time"`
	ObjectChanges   map[uint16][7]uint16 `json:"object_changes,omitempty"`
	MagicBaseDamage map[uint16]uint16    `json:"magic_base_damage,omitempty"`
	// Object script continuations, indexed by objectID*8 + object word.
	ScriptEntries map[uint32]uint16 `json:"script_entries,omitempty"`
}

// Action.Actor is a role slot (0..5). A target index is accompanied by a side;
// TargetAll represents the object's apply-to-all flag and has Index zero.
type Action struct {
	Actor    uint16 `json:"actor"`
	Kind     Kind   `json:"kind"`
	Target   Target `json:"target"`
	ObjectID uint16 `json:"object_id,omitempty"`
}

type Side string

const (
	Party Side = "party"
	Enemy Side = "enemy"
	All   Side = "all"
)

type Target struct {
	Side  Side `json:"side,omitempty"`
	Index int  `json:"index,omitempty"`
}

type Event struct {
	Kind        string `json:"kind"`
	Actor       Side   `json:"actor_side,omitempty"`
	ActorIndex  int    `json:"actor_index,omitempty"`
	Target      Side   `json:"target_side,omitempty"`
	TargetIndex int    `json:"target_index,omitempty"`
	ObjectID    uint16 `json:"object_id,omitempty"`
	HP          int    `json:"hp,omitempty"`
	MP          int    `json:"mp,omitempty"`
	Critical    bool   `json:"critical,omitempty"`
}

type Rewards struct {
	Experience uint32            `json:"experience"`
	Cash       uint32            `json:"cash"`
	ByRole     map[uint16]uint32 `json:"by_role,omitempty"`
	Applied    bool              `json:"applied"`
}

type Report struct {
	Round     int     `json:"round"`
	Outcome   Outcome `json:"outcome"`
	Committed bool    `json:"committed"`
	Events    []Event `json:"events,omitempty"`
	Draws     []Draw  `json:"draws,omitempty"`
	Rewards   Rewards `json:"rewards"`
	// StoryResult is a PAL trigger input: 0 victory, 1 defeat, 2 escaped.
	// A nil value means combat has not ended. No story script is called here.
	StoryResult *int          `json:"story_result,omitempty"`
	Limitations []string      `json:"limitations,omitempty"`
	Scripts     []ScriptTrace `json:"scripts,omitempty"`
}

type ScriptTrace struct {
	Entry, PC, Opcode uint16
	Operands          [3]uint16
	Success           bool
}

type Config struct {
	Tables      *paldata.RuntimeTables
	Game        *paldata.Game
	Party       []Player
	TeamID      uint16
	Battlefield uint16
	Boss        bool
	Cash        uint32
	Inventory   map[uint16]uint16
	Random      RandomSource
	Callbacks   Callbacks
	HidingTime  int
}

// UnsupportedError is a coverage boundary, not a successful script execution.
// ObjectID and Entry identify the exact missing imported behavior when known.
type UnsupportedError struct {
	Feature         string
	ObjectID, Entry uint16
}

func (e *UnsupportedError) Error() string {
	return fmt.Sprintf("palcombat: unsupported %s (object %04x, script %04x)", e.Feature, e.ObjectID, e.Entry)
}

// Engine has one owner; callers must serialize access. All imported tables,
// party data, and snapshots are copied so they cannot mutate an active fight.
type Engine struct {
	active        *Snapshot
	state         Snapshot
	objects       [][7]uint16
	scripts       []paldata.Instruction
	tables        paldata.RuntimeTables
	random        RandomSource
	boss          bool
	battlefield   uint16
	callbacks     Callbacks
	transaction   ScriptTransaction
	randomFailure error
}
