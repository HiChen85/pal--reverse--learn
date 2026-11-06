// Package palgame adapts imported PAL tables into explicitly declared headless
// snapshots. It does not establish that a snapshot is reachable in the original
// game, advance frames, or certify the identity of a Windows release.
package palgame

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"

	"github.com/HiChen85/pal--reverse--learn/internal/palasset"
	"github.com/HiChen85/pal--reverse--learn/internal/paldata"
	"github.com/HiChen85/pal--reverse--learn/internal/palflow"
)

// Initialization preserves the new-game equipment pass and its invocation
// context separately from later scene/event actions. It is not route completion.
type Initialization struct {
	World         *palflow.WorldState     `json:"world"`
	LastEventID   uint16                  `json:"last_event_id"`
	ScriptSuccess bool                    `json:"script_success"`
	Evidence      []palflow.PlayerSubstep `json:"evidence"`
	Steps         int                     `json:"steps"`
}

// InitialSnapshot is the world-only convenience wrapper around InitialState.
func InitialSnapshot(g *paldata.Game, r *paldata.RuntimeTables, scene uint16) (*palflow.WorldState, error) {
	initial, err := InitialState(g, r, scene)
	if err != nil {
		return nil, err
	}
	return initial.World, nil
}

// InitialState constructs the reference engine's new-game defaults and runs its
// bounded equipment callbacks before the first scene is entered. A scene other
// than one is a caller-declared checkpoint, not a verified route.
func InitialState(g *paldata.Game, r *paldata.RuntimeTables, scene uint16) (Initialization, error) {
	if g == nil || r == nil {
		return Initialization{}, errors.New("imported game and runtime tables are required")
	}
	if scene == 0 || int(scene) >= len(g.Scenes) {
		return Initialization{}, errors.New("scene requires a valid record and following event boundary")
	}
	found := false
	for _, f := range g.Files {
		if strings.EqualFold(f.Name, "DATA.MKF") {
			found = true
			if f.SHA256 == "" || f.SHA256 != r.Source.SHA256 {
				return Initialization{}, errors.New("runtime DATA.MKF digest differs from imported game")
			}
		}
	}
	if !found {
		return Initialization{}, errors.New("imported game has no DATA.MKF identity")
	}
	if err := r.ValidateReferences(g); err != nil {
		return Initialization{}, err
	}
	w := &palflow.WorldState{Scene: scene, ChaseRange: 1, PartyRoles: []uint16{0}, PartyFrames: make([]uint16, 5), PartyOffsetX: 160, PartyOffsetY: 112, PartyX: 160, PartyY: 112, PartyPositions: make([]palflow.WorldPosition, 5), Trail: make([]palflow.WorldTrail, 5), Events: map[uint16]palflow.WorldEvent{}, Scenes: map[uint16]palflow.WorldScene{}, Inventory: map[uint16]int{}, InventorySlots: make([]palflow.InventorySlot, 256), PlayerSprites: map[uint16]uint16{}}
	for i, e := range g.Events {
		w.Events[uint16(i+1)] = palflow.WorldEvent{Trigger: e.Trigger, AutoScript: e.AutoScript, State: e.State, VanishTime: e.VanishTime, Layer: e.Layer, X: e.X, Y: e.Y, Direction: e.Direction, Frame: e.CurrentFrame, TriggerMode: e.TriggerMode, SpriteFrames: e.SpriteFrames, SpriteFramesAuto: e.SpriteFramesAuto, AutoIdleFrame: e.AutoIdleFrame}
	}
	for i, s := range g.Scenes {
		w.Scenes[uint16(i+1)] = palflow.WorldScene{Map: s.Map, OnEnter: s.OnEnter, OnTeleport: s.OnTeleport, EventStart: s.EventStart}
	}
	w.Roles = map[uint16][75]uint16{}
	w.Objects = map[uint16][7]uint16{}
	w.PlayerStatusMode = "classic"
	if g.Layout == "dos" {
		w.AutoMessageMode = "dos"
	}
	for i, object := range g.Objects {
		w.Objects[uint16(i)] = object
	}
	for _, role := range r.Roles {
		w.PlayerSprites[role.ID] = role.Sprite
	}
	for _, role := range r.Roles {
		w.Roles[role.ID] = role.Raw
	}
	runner, err := palflow.New(Source{Game: g}, palflow.Options{})
	if err != nil {
		return Initialization{}, err
	}
	state, evidence, err := runner.InitializeEquipment(w)
	if err != nil {
		return Initialization{}, fmt.Errorf("new-game equipment initialization: %w", err)
	}
	return Initialization{World: state.World, LastEventID: state.LastEventID, ScriptSuccess: state.ScriptSuccess, Evidence: evidence, Steps: state.PlayerSubsteps}, nil
}
func InitialIdleFrames(g *paldata.Game) map[uint16]uint16 {
	out := map[uint16]uint16{}
	if g != nil {
		for i, e := range g.Events {
			out[uint16(i+1)] = e.ScriptIdleFrame
		}
	}
	return out
}

// Source reads direct indexes. RedactText keeps evidence local without copying
// the game's dialogue into reports intended for sharing.
type Source struct {
	Game       *paldata.Game
	RedactText bool
	Assets     *palasset.SpriteCatalog
}

func (s Source) SpriteResource(id uint16) (palflow.SpriteResource, error) {
	if s.Assets == nil {
		return palflow.SpriteResource{}, errors.New("local MGO catalog is required for sprite loading")
	}
	info, err := s.Assets.Sprite(id)
	if err != nil {
		return palflow.SpriteResource{}, err
	}
	return palflow.SpriteResource{ID: id, ArchiveSHA256: s.Assets.SourceSHA256(), SHA256: info.SHA256, DecodedSize: info.DecodedSize, FrameCount: info.FrameCount, UnsafeFrames: len(info.UnsafeFrames)}, nil
}

func (s Source) Instruction(pc uint16) (palflow.Instruction, error) {
	if s.Game == nil || int(pc) >= len(s.Game.Scripts) {
		return palflow.Instruction{}, fmt.Errorf("script %04x outside imported table", pc)
	}
	i := s.Game.Scripts[pc]
	return palflow.Instruction{Opcode: i.Opcode, Operands: i.Operands}, nil
}
func (s Source) Message(id uint16) (string, error) {
	if s.Game == nil || int(id) >= len(s.Game.Messages) {
		return "", fmt.Errorf("message %d outside imported table", id)
	}
	if !s.RedactText {
		return s.Game.Messages[id], nil
	}
	if int(id) >= len(s.Game.RawMessages) {
		return "", errors.New("raw message unavailable for digest-only evidence")
	}
	sum := sha256.Sum256(s.Game.RawMessages[id])
	return fmt.Sprintf("[message:%d sha256:%x]", id, sum), nil
}
