package palgame

import (
	"errors"
	"fmt"
	"maps"

	"github.com/HiChen85/pal--reverse--learn/internal/palasset"
	"github.com/HiChen85/pal--reverse--learn/internal/paldata"
	"github.com/HiChen85/pal--reverse--learn/internal/palflow"
)

// SessionOptions selects a declared logical checkpoint. AssumeSceneEntered
// permits event tests without executing OnEnter; that is an explicit assumption,
// not evidence that the checkpoint is reachable in the original game.
type SessionOptions struct {
	RandomMode         string
	BattleMode         string
	BattleSeed         uint64
	Assets             *palasset.SpriteCatalog
	Maps               *palasset.MapCatalog
	RedactText         bool
	Scene              uint16
	AssumeSceneEntered bool
	MaxSteps           int
}
type SessionSnapshot struct {
	ScriptSuccess   bool                `json:"script_success"`
	World           *palflow.WorldState `json:"world"`
	IdleFrames      map[uint16]uint16   `json:"idle_frames"`
	LastEventID     uint16              `json:"last_event_id"`
	NeedsSceneEnter bool                `json:"needs_scene_enter"`
	Actions         int                 `json:"actions"`
}
type Session struct {
	runtime        *paldata.RuntimeTables
	maps           *palasset.MapCatalog
	lastInvocation *Invocation
	initialization Initialization
	assets         *palasset.SpriteCatalog
	redactText     bool
	game           *paldata.Game
	current        SessionSnapshot
	maxSteps       int
}

type Invocation struct {
	Request palflow.Request `json:"request"`
	Options palflow.Options `json:"options"`
}

func (s *Session) LastInvocation() *Invocation {
	if s.lastInvocation == nil {
		return nil
	}
	out := *s.lastInvocation
	out.Request.World = palflow.CloneWorld(out.Request.World)
	out.Request.IdleFrames = maps.Clone(out.Request.IdleFrames)
	out.Request.Inputs = append([]palflow.Input(nil), out.Request.Inputs...)
	return &out
}

func (s *Session) source() Source {
	return Source{Runtime: s.runtime, Game: s.game, Assets: s.assets, Maps: s.maps, RedactText: s.redactText}
}

func (s *Session) runnerOptions() palflow.Options {
	return palflow.Options{MaxSteps: s.maxSteps, MaxCallDepth: 64, MaxTextBytes: 1 << 20}
}

func NewSession(g *paldata.Game, r *paldata.RuntimeTables, options SessionOptions) (*Session, error) {
	scene := options.Scene
	if scene == 0 {
		scene = 1
	}
	initial, err := InitialState(g, r, scene)
	if err != nil {
		return nil, err
	}
	limit := options.MaxSteps
	if limit == 0 {
		limit = 10000
	}
	if limit < 1 || limit > palflow.MaxAllowedSteps {
		return nil, errors.New("session step limit out of range")
	}
	world := palflow.CloneWorld(initial.World)
	world.RandomMode = options.RandomMode
	if world.RandomMode == "" {
		world.RandomMode = "seeded"
	}
	if world.RandomMode != "seeded" && world.RandomMode != "explicit" {
		return nil, errors.New("unsupported random input policy")
	}
	world.BattleMode = options.BattleMode
	if world.BattleMode == "" {
		world.BattleMode = "auto_attack"
	}
	if world.BattleMode != "auto_attack" && world.BattleMode != "disabled" {
		return nil, errors.New("unsupported battle policy")
	}
	world.BattleRandomState = options.BattleSeed
	if world.BattleRandomState == 0 {
		world.BattleRandomState = 1
	}

	if options.AssumeSceneEntered && options.Maps != nil {
		world, err = (Source{Game: g, Assets: options.Assets, Maps: options.Maps}).LoadResources(world, true, true)
		if err != nil {
			return nil, err
		}
	}
	return &Session{runtime: r, maps: options.Maps, initialization: initial, game: g, assets: options.Assets, redactText: options.RedactText, maxSteps: limit, current: SessionSnapshot{World: world, LastEventID: initial.LastEventID, ScriptSuccess: initial.ScriptSuccess, IdleFrames: InitialIdleFrames(g), NeedsSceneEnter: !options.AssumeSceneEntered}}, nil
}

// Initialization returns detached bootstrap evidence, including mutable callback
// return-address pointers, without exposing the active session or initial world.
func (s *Session) Initialization() Initialization {
	out := s.initialization
	out.World = palflow.CloneWorld(out.World)
	out.Evidence = append([]palflow.PlayerSubstep(nil), out.Evidence...)
	for i := range out.Evidence {
		if out.Evidence[i].ReturnedEntry != nil {
			value := *out.Evidence[i].ReturnedEntry
			out.Evidence[i].ReturnedEntry = &value
		}
	}
	return out
}
func (s *Session) Snapshot() SessionSnapshot {
	out := s.current
	out.World = palflow.CloneWorld(out.World)
	out.IdleFrames = maps.Clone(out.IdleFrames)
	return out
}
func (s *Session) Enter(inputs []palflow.Input) (palflow.Report, error) {
	s.lastInvocation = nil
	if !s.current.NeedsSceneEnter {
		return palflow.Report{}, errors.New("current scene has already entered; reset or select an explicit checkpoint to replay")
	}
	world := palflow.CloneWorld(s.current.World)
	if world.SceneLoadPending || (s.maps != nil && world.SceneResources == nil) {
		var err error
		world, err = s.source().LoadResources(world, true, world.SceneResources == nil)
		if err != nil {
			return palflow.Report{}, err
		}
	}
	scene := s.current.World.Scene
	record, ok := s.current.World.Scenes[scene]
	if !ok {
		return palflow.Report{}, errors.New("current scene missing")
	}
	return s.invokePrepared(record.OnEnter, 0xffff, scene, 0, inputs, world)
}

// Trigger checks scene membership and active state. Position/direction/search
// distance and automatic touch detection are deliberately not inferred; the
// caller explicitly requests this logical interaction and the report says so.
func (s *Session) Trigger(eventID uint16, inputs []palflow.Input) (palflow.Report, error) {
	s.lastInvocation = nil
	if s.current.NeedsSceneEnter {
		return palflow.Report{}, errors.New("scene OnEnter has not completed")
	}
	start, end, err := s.eventRange(s.current.World.Scene)
	if err != nil {
		return palflow.Report{}, err
	}
	if eventID <= start || eventID > end {
		return palflow.Report{}, fmt.Errorf("event %d is outside the current scene", eventID)
	}
	event, ok := s.current.World.Events[eventID]
	if !ok {
		return palflow.Report{}, errors.New("event missing")
	}
	if event.State <= 0 || event.VanishTime != 0 {
		return palflow.Report{}, errors.New("event is hidden, inactive or temporarily vanished")
	}
	if event.TriggerMode == 0 {
		return palflow.Report{}, errors.New("event trigger is disabled")
	}
	return s.invoke(event.Trigger, eventID, s.current.World.Scene, eventID, inputs)
}
func (s *Session) eventRange(scene uint16) (uint16, uint16, error) {
	current, ok := s.current.World.Scenes[scene]
	if !ok {
		return 0, 0, errors.New("scene missing")
	}
	next, ok := s.current.World.Scenes[scene+1]
	if !ok || current.EventStart > next.EventStart || int(next.EventStart) > len(s.game.Events) {
		return 0, 0, errors.New("scene has no valid following event boundary")
	}
	return current.EventStart, next.EventStart, nil
}
func (s *Session) invoke(entry, eventID, scene, triggerID uint16, inputs []palflow.Input) (palflow.Report, error) {
	return s.invokePrepared(entry, eventID, scene, triggerID, inputs, palflow.CloneWorld(s.current.World))
}

func (s *Session) invokePrepared(entry, eventID, scene, triggerID uint16, inputs []palflow.Input, world *palflow.WorldState) (palflow.Report, error) {
	if triggerID == 0 {
		world.SceneChanged = false
	}
	req := palflow.Request{Entry: entry, EventID: eventID, LastEventID: s.current.LastEventID, IdleFrames: maps.Clone(s.current.IdleFrames), World: world, Inputs: append([]palflow.Input(nil), inputs...)}
	options := s.runnerOptions()
	runner, err := palflow.New(s.source(), options)
	if err != nil {
		return palflow.Report{}, err
	}
	s.lastInvocation = &Invocation{Request: req, Options: options}
	report, err := runner.Trace(req)
	report.Assumptions = append(report.Assumptions, "Logical interaction checkpoint only: spatial reachability, direction, collision and automatic world scheduling are not verified.")
	if err != nil {
		return report, err
	}
	if len(report.UnusedInputs) > 0 {
		return report, errors.New("unused inputs; session action was not committed")
	}
	if !report.Completed || report.State.World == nil {
		return report, errors.New("script did not complete with a world snapshot")
	}
	next := palflow.CloneWorld(report.State.World)
	if triggerID == 0 {
		r, ok := next.Scenes[scene]
		if !ok {
			return report, errors.New("invoking scene disappeared")
		}
		r.OnEnter = report.State.ReturnedEntry
		next.Scenes[scene] = r
	} else {
		e, ok := next.Events[triggerID]
		if !ok {
			return report, errors.New("invoking event disappeared")
		}
		e.Trigger = report.State.ReturnedEntry
		next.Events[triggerID] = e
	}
	// A failed or partial trace never updates the active checkpoint. The completed
	// proposal is committed together with its counters and returned entry points.
	s.current = SessionSnapshot{ScriptSuccess: report.State.ScriptSuccess, World: next, IdleFrames: maps.Clone(report.State.IdleFrames), LastEventID: report.State.LastEventID, NeedsSceneEnter: next.SceneChanged, Actions: s.current.Actions + 1}
	return report, nil
}
