package palgame

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/HiChen85/pal--reverse--learn/internal/palcombat"
	"github.com/HiChen85/pal--reverse--learn/internal/paldata"
	"github.com/HiChen85/pal--reverse--learn/internal/palflow"
)

const SaveVersion = 1
const MaxSaveBytes = 16 << 20
const saveFormat = "pal-studio-session"

// SaveSource binds imported tables and all supplied resource catalogs. PAT
// participates in palette-dependent script timing as well as rendering. Table
// hashes also detect an in-memory table edit after an import's file hashes were
// recorded. Names are canonical, so moving unchanged local resources is safe.
type SaveSource struct {
	Encoding            string              `json:"encoding"`
	Layout              string              `json:"layout"`
	Files               [4]paldata.FileInfo `json:"files"`
	Runtime             paldata.FileInfo    `json:"runtime"`
	MGO                 paldata.FileInfo    `json:"mgo"`
	MAP                 paldata.FileInfo    `json:"map"`
	GOP                 paldata.FileInfo    `json:"gop"`
	PAT                 paldata.FileInfo    `json:"pat"`
	RNG                 paldata.FileInfo    `json:"rng"`
	FBP                 paldata.FileInfo    `json:"fbp"`
	GameTablesSHA256    string              `json:"game_tables_sha256"`
	RuntimeTablesSHA256 string              `json:"runtime_tables_sha256"`
}

type savePayload struct {
	Source            SaveSource        `json:"source"`
	Options           palflow.Options   `json:"options"`
	RedactText        bool              `json:"redact_text"`
	Snapshot          SessionSnapshot   `json:"snapshot"`
	Initialization    Initialization    `json:"initialization"`
	InitialCheckpoint SessionCheckpoint `json:"initial_checkpoint"`
	NilCollections    []string          `json:"nil_collections"`
}

// SessionCheckpoint retains the caller's initial diagnostic declaration. It
// records an assumption, never proof of original-game spatial reachability.
type SessionCheckpoint struct {
	Scene              uint16 `json:"scene"`
	AssumeSceneEntered bool   `json:"assume_scene_entered"`
}

func (s *Session) InitialCheckpoint() SessionCheckpoint {
	return SessionCheckpoint{Scene: s.initialization.World.Scene, AssumeSceneEntered: s.initialAssumeEntered}
}

type sessionSave struct {
	Format        string      `json:"format"`
	Version       int         `json:"version"`
	PayloadSHA256 string      `json:"payload_sha256"`
	Payload       savePayload `json:"payload"`
}

func saveHash(v any) (string, error) {
	h := sha256.New()
	if err := json.NewEncoder(h).Encode(v); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
func validSaveDigest(value string) bool {
	b, err := hex.DecodeString(value)
	return err == nil && len(b) == 32 && value == strings.ToLower(value)
}

func (s *Session) saveSource() (SaveSource, error) {
	var out SaveSource
	if s == nil || s.game == nil || s.runtime == nil {
		return out, errors.New("save requires imported game and runtime tables")
	}
	out.Encoding, out.Layout = s.game.Encoding, s.game.Layout
	if (out.Encoding != "big5" && out.Encoding != "gbk") || (out.Layout != "dos" && out.Layout != "windows") {
		return out, errors.New("save requires explicit imported encoding and layout")
	}
	names := []string{"SSS.MKF", "DATA.MKF", "M.MSG", "WORD.DAT"}
	for i, name := range names {
		found := false
		for _, f := range s.game.Files {
			if strings.EqualFold(f.Name, name) {
				if found || !validSaveDigest(f.SHA256) || f.Size < 0 {
					return out, fmt.Errorf("invalid imported %s identity", name)
				}
				found = true
				f.Name = name
				out.Files[i] = f
			}
		}
		if !found {
			return out, fmt.Errorf("save requires imported %s identity", name)
		}
	}
	out.Runtime = s.runtime.Source
	out.Runtime.Name = "DATA.MKF"
	if out.Runtime != out.Files[1] {
		return out, errors.New("runtime DATA.MKF differs from imported game")
	}
	out.MGO = paldata.FileInfo{Name: "MGO.MKF"}
	if s.assets != nil {
		out.MGO.SHA256 = s.assets.SourceSHA256()
		out.MGO.Size = s.assets.SourceSize()
	}
	out.MAP = paldata.FileInfo{Name: "MAP.MKF"}
	out.GOP = paldata.FileInfo{Name: "GOP.MKF"}
	if s.maps != nil {
		out.MAP.SHA256 = s.maps.MAPSourceSHA256()
		out.MAP.Size = s.maps.MAPSourceSize()
		out.GOP.SHA256 = s.maps.GOPSourceSHA256()
		out.GOP.Size = s.maps.GOPSourceSize()
	}
	out.PAT = paldata.FileInfo{Name: "PAT.MKF"}
	if s.palettes != nil {
		out.PAT.SHA256, out.PAT.Size = s.palettes.SourceSHA256(), s.palettes.SourceSize()
	}
	out.RNG = paldata.FileInfo{Name: "RNG.MKF"}
	if s.rng != nil {
		out.RNG.SHA256, out.RNG.Size = s.rng.SourceSHA256(), s.rng.SourceSize()
	}
	out.FBP = paldata.FileInfo{Name: "FBP.MKF"}
	if s.fbp != nil {
		out.FBP.SHA256, out.FBP.Size = s.fbp.SourceSHA256(), s.fbp.SourceSize()
	}
	// Files are already bound above and contain import-time spelling only.
	g := *s.game
	g.Files = nil
	var err error
	out.GameTablesSHA256, err = saveHash(g)
	if err != nil {
		return out, err
	}
	r := *s.runtime
	r.Source = out.Runtime
	out.RuntimeTablesSHA256, err = saveHash(r)
	return out, err
}

// MarshalSave exports a modern local JSON checkpoint, never an original RPG
// save or evidence that the checkpoint is reachable. No pending VM invocation
// is serialized: only completed Session actions can be resumed.
func (s *Session) MarshalSave() ([]byte, error) {
	source, err := s.saveSource()
	if err != nil {
		return nil, err
	}
	if err := s.validateSavedSnapshot(s.current); err != nil {
		return nil, err
	}
	if s.maxSteps < 1 || s.maxSteps > palflow.MaxAllowedSteps || s.initialization.World == nil {
		return nil, errors.New("save requires complete initialization and supported execution limits")
	}
	p := savePayload{Source: source, Options: s.runnerOptions(), RedactText: s.redactText, Snapshot: s.Snapshot(), Initialization: s.Initialization(), InitialCheckpoint: s.InitialCheckpoint(), NilCollections: []string{}}
	nils := []string{}
	saveJSONValue(reflect.ValueOf(p), "", &nils)
	sort.Strings(nils)
	p.NilCollections = nils
	ignored := []string{}
	value := saveJSONValue(reflect.ValueOf(p), "", &ignored)
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(encoded)
	out := map[string]any{"format": saveFormat, "version": SaveVersion, "payload_sha256": fmt.Sprintf("%x", sum), "payload": value}
	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return nil, err
	}
	if len(data) >= MaxSaveBytes {
		return nil, errors.New("session save exceeds byte limit")
	}
	return append(data, '\n'), nil
}

// RestoreSession validates into a new detached Session. Callers replace their
// active session only after this succeeds and an explicit load action occurred.
// Resource catalogs come from options; scene, policies, seed and budget come
// exclusively from the save. LoadResources is intentionally never called here:
// loading it would reset saved offsets, resource flags and event frame counts.
func RestoreSession(g *paldata.Game, r *paldata.RuntimeTables, options SessionOptions, data []byte) (*Session, error) {
	var saved sessionSave
	tree, err := readSaveJSON(data, &saved)
	if err != nil {
		return nil, err
	}
	if saved.Format != saveFormat || saved.Version != SaveVersion {
		return nil, errors.New("unsupported session save format or version")
	}
	canonical, err := json.Marshal(tree["payload"])
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(canonical)
	if !validSaveDigest(saved.PayloadSHA256) || saved.PayloadSHA256 != fmt.Sprintf("%x", sum) {
		return nil, errors.New("session save payload checksum mismatch")
	}
	p := &saved.Payload
	wanted := map[string]bool{}
	for _, path := range p.NilCollections {
		if wanted[path] || path == "/nil_collections" {
			return nil, errors.New("invalid duplicate nil collection path")
		}
		wanted[path] = true
	}
	if err := restoreSaveNils(reflect.ValueOf(p).Elem(), "", wanted); err != nil {
		return nil, err
	}
	if len(wanted) != 0 {
		return nil, errors.New("unknown nil collection path")
	}
	s := &Session{game: g, runtime: r, assets: options.Assets, maps: options.Maps, palettes: options.Palettes, rng: options.RNG, fbp: options.FBP, redactText: p.RedactText, maxSteps: p.Options.MaxSteps}
	source, err := s.saveSource()
	if err != nil {
		return nil, err
	}
	if source != p.Source {
		return nil, errors.New("session save resources, tables, encoding or layout differ from current import")
	}
	if r == nil {
		return nil, errors.New("runtime is required")
	}
	if err := r.ValidateReferences(g); err != nil {
		return nil, err
	}
	if p.Options.MaxSteps < 1 || p.Options.MaxSteps > palflow.MaxAllowedSteps || p.Options != s.runnerOptions() {
		return nil, errors.New("unsupported saved execution limits")
	}
	if err := s.validateSavedSnapshot(p.Snapshot); err != nil {
		return nil, err
	}
	if p.Initialization.World == nil {
		return nil, errors.New("save initialization world is required")
	}
	if p.InitialCheckpoint.Scene != p.Initialization.World.Scene {
		return nil, errors.New("saved initial checkpoint differs from initialization scene")
	}
	initial, err := InitialState(g, r, p.Initialization.World.Scene)
	if err != nil {
		return nil, err
	}
	if !reflect.DeepEqual(initial, p.Initialization) {
		return nil, errors.New("save initialization differs from imported new-game initialization")
	}
	s.current = p.Snapshot
	s.initialization = p.Initialization
	s.initialAssumeEntered = p.InitialCheckpoint.AssumeSceneEntered
	return s, nil
}

func (s *Session) validateSavedSnapshot(snapshot SessionSnapshot) error {
	w := snapshot.World
	if w == nil {
		return errors.New("complete save world is required")
	}
	if snapshot.Actions < 0 || snapshot.Actions > 1_000_000_000 {
		return errors.New("save action count out of range")
	}
	if w.RandomMode != "seeded" && w.RandomMode != "explicit" {
		return errors.New("unsupported saved random policy")
	}
	if w.BattleMode != "auto_attack" && w.BattleMode != "fast_verify" && w.BattleMode != "disabled" {
		return errors.New("unsupported saved battle policy")
	}
	if w.InBattle || w.Battle != nil && w.Battle.Outcome == palcombat.Ongoing {
		return errors.New("in-progress battle saves are unsupported")
	}
	if w.Battle != nil {
		switch w.Battle.Outcome {
		case palcombat.Victory, palcombat.Defeat, palcombat.Escaped, palcombat.Terminated:
		default:
			return errors.New("unknown saved battle outcome")
		}
		if len(w.Battle.Party) > 5 || len(w.Battle.Enemies) > 5 {
			return errors.New("saved battle summary exceeds party limits")
		}
		if len(w.Battle.Party) == 0 || w.Battle.Round < 1 || w.Battle.MaxEnemySlot < 0 || w.Battle.MaxEnemySlot >= 5 {
			return errors.New("saved terminal battle summary is incomplete")
		}
	}
	if w.PlayerStatusMode != "classic" || w.AutoMessageMode != "" && w.AutoMessageMode != "dos" {
		return errors.New("unsupported saved world policy")
	}
	if w.Scene == 0 || int(w.Scene) >= len(s.game.Scenes) {
		return errors.New("saved current scene has no following boundary")
	}
	if (w.SceneChanged || w.SceneLoadPending) && !snapshot.NeedsSceneEnter {
		return errors.New("pending scene state requires scene entry")
	}
	if w.Events == nil || len(w.Events) != len(s.game.Events) || w.Scenes == nil || len(w.Scenes) != len(s.game.Scenes) || w.Objects == nil || len(w.Objects) != len(s.game.Objects) || w.Roles == nil || len(w.Roles) != 6 || w.PlayerSprites == nil || len(w.PlayerSprites) != 6 || w.MagicBaseDamage == nil || len(w.MagicBaseDamage) != len(s.runtime.Magics) || snapshot.IdleFrames == nil || len(snapshot.IdleFrames) != len(s.game.Events) {
		return errors.New("save world tables are incomplete")
	}
	validPC := func(pc uint16) bool { return pc == 0 || int(pc) < len(s.game.Scripts) }
	for i := range s.game.Events {
		id := uint16(i + 1)
		e, ok := w.Events[id]
		_, idle := snapshot.IdleFrames[id]
		if !ok || !idle || !validPC(e.Trigger) || !validPC(e.AutoScript) {
			return fmt.Errorf("saved event %d missing or has invalid continuation", id)
		}
	}
	for i, original := range s.game.Scenes {
		sc, ok := w.Scenes[uint16(i+1)]
		if !ok || sc.EventStart != original.EventStart || !validPC(sc.OnEnter) || !validPC(sc.OnTeleport) {
			return fmt.Errorf("saved scene %d missing or invalid", i+1)
		}
	}
	for i := range s.game.Objects {
		if _, ok := w.Objects[uint16(i)]; !ok {
			return errors.New("saved object table has missing IDs")
		}
	}
	for i := range 6 {
		raw, ok := w.Roles[uint16(i)]
		sprite, found := w.PlayerSprites[uint16(i)]
		if !ok || !found || sprite != raw[2] {
			return errors.New("saved role or sprite cache is incomplete or inconsistent")
		}
	}
	for _, magic := range s.runtime.Magics {
		if _, ok := w.MagicBaseDamage[magic.ID]; !ok {
			return errors.New("saved magic table has missing IDs")
		}
	}
	// Trigger context is shared with role IDs (0..5) and battle slot IDs
	// (0..4); a completed battle can retain one without a matching map event.
	if snapshot.LastEventID >= paldata.RoleCount && snapshot.LastEventID != 0xffff {
		if _, ok := w.Events[snapshot.LastEventID]; !ok {
			return errors.New("saved last event is absent")
		}
	}
	if len(w.PartyRoles) < 1 || len(w.PartyRoles)+len(w.FollowerSprites) > 5 || len(w.PartyFrames) != 5 || len(w.PartyPositions) != 5 || len(w.Trail) != 5 || w.Direction > 3 {
		return errors.New("saved party state is incomplete or out of range")
	}
	seenRoles := map[uint16]bool{}
	for _, id := range w.PartyRoles {
		if id >= 6 || seenRoles[id] {
			return errors.New("saved party role is invalid or repeated")
		}
		seenRoles[id] = true
	}
	for _, coord := range []int{w.PartyX, w.PartyY, w.PartyOffsetX, w.PartyOffsetY, w.ViewportX, w.ViewportY} {
		if coord < -32768 || coord > 32767 {
			return errors.New("saved coordinates must fit signed 16-bit PAL positions")
		}
	}
	if w.Inventory == nil || len(w.InventorySlots) != 256 {
		return errors.New("complete saved ordered inventory is required")
	}
	if _, err := palflow.CompactInventory(w); err != nil {
		return fmt.Errorf("saved inventory: %w", err)
	}
	for id := range w.Inventory {
		if _, ok := w.Objects[id]; !ok {
			return errors.New("saved inventory item is absent from object table")
		}
	}
	for _, slot := range w.InventorySlots {
		if slot.InUse != 0 {
			return errors.New("overworld save cannot contain pending battle item reservations")
		}
		if slot.Item != 0 {
			if _, ok := w.Objects[slot.Item]; !ok {
				return errors.New("saved inventory slot item is absent from object table")
			}
		}
	}
	if len(w.PlayerResources) > 5 {
		return errors.New("saved player resource count exceeds party capacity")
	}
	if s.maps != nil && w.SceneResources == nil && !snapshot.NeedsSceneEnter {
		return errors.New("entered saved scene is missing loaded map resources")
	}
	for _, resource := range w.PlayerResources {
		if err := s.validateSavedSprite(resource); err != nil {
			return err
		}
	}
	if loaded := w.SceneResources; loaded != nil {
		if s.maps == nil || loaded.SceneID == 0 || int(loaded.SceneID) >= len(s.game.Scenes) || !w.SceneLoadPending && loaded.SceneID != w.Scene {
			return errors.New("saved loaded scene context is invalid")
		}
		m, err := s.maps.Map(loaded.MapID)
		if err != nil {
			return err
		}
		info := m.Info()
		if loaded.MAPArchiveSHA256 != s.maps.MAPSourceSHA256() || loaded.GOPArchiveSHA256 != s.maps.GOPSourceSHA256() || loaded.MapSHA256 != info.SHA256 || loaded.GOPSHA256 != info.GOPSHA256 {
			return errors.New("saved loaded map identity differs from local catalogs")
		}
		start, end := w.Scenes[loaded.SceneID].EventStart, w.Scenes[loaded.SceneID+1].EventStart
		if loaded.EventSprites == nil {
			return errors.New("saved loaded event sprite table is missing")
		}
		if len(w.PlayerResources) != len(w.PartyRoles)+len(w.FollowerSprites) {
			return errors.New("saved loaded player resource table is incomplete")
		}
		expectedSprites := 0
		for id := int(start) + 1; id <= int(end); id++ {
			e := w.Events[uint16(id)]
			if e.Sprite == 0 {
				continue
			}
			expectedSprites++
			resource, ok := loaded.EventSprites[uint16(id)]
			if !ok || resource.ID != e.Sprite || resource.FrameCount != e.SpriteFramesAuto {
				return errors.New("saved loaded event sprite table is incomplete or inconsistent")
			}
		}
		if len(loaded.EventSprites) != expectedSprites {
			return errors.New("saved loaded event sprite table has extra entries")
		}
		for id, resource := range loaded.EventSprites {
			if id <= start || id > end {
				return errors.New("saved loaded sprite belongs to another scene")
			}
			if err := s.validateSavedSprite(resource); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Session) validateSavedSprite(resource palflow.SpriteResource) error {
	actual, err := s.source().SpriteResource(resource.ID)
	if err != nil {
		return err
	}
	if actual != resource {
		return errors.New("saved sprite identity differs from local MGO catalog")
	}
	return nil
}
