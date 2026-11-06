package paldata

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/HiChen85/pal--reverse--learn/internal/pal"
)

const (
	RoleCount         = 6
	RoleWords         = 75
	EnemyWords        = 35
	EquipmentSlots    = 6
	MagicSlots        = 32
	ElementCount      = 5
	EnemyTeamSize     = 5
	StoreSize         = 9
	MaxRuntimeEnemies = 65536
	MaxRuntimeTeams   = 65536
	MaxRuntimeStores  = 1024
)

// Role is one column of DATA.MKF chunk 3's 75-by-6 array of WORDs.
// ID is the zero-based role slot, not an object code or word-table index.
// Raw retains every attribute, including the engine's unknown fields.
type Role struct {
	ID                                                                                      uint16
	Avatar, BattleSprite, Sprite, NameIndex, AttackAll                                      uint16
	Level, MaxHP, MaxMP, HP, MP                                                             uint16
	Equipment                                                                               [EquipmentSlots]uint16
	Attack, Magic, Defense, Dexterity, FleeRate, PoisonResistance                           uint16
	ElementalResistance                                                                     [ElementCount]uint16
	CoveredBy                                                                               uint16
	Magics                                                                                  [MagicSlots]uint16
	WalkFrames, CooperativeMagic                                                            uint16
	DeathSound, AttackSound, WeaponSound, CriticalSound, MagicSound, CoverSound, DyingSound uint16
	Raw                                                                                     [RoleWords]uint16
}

// Enemy preserves a 35-WORD DATA.MKF chunk 1 record. Only the five sounds are
// signed SHORTs in the referenced public structure; other fields remain WORDs.
type Enemy struct {
	ID                                                                              uint16
	IdleFrames, MagicFrames, AttackFrames, IdleAnimSpeed, ActWaitFrames, YPosOffset uint16
	AttackSound, ActionSound, MagicSound, DeathSound, CallSound                     int16
	HP, Exp, Cash, Level, Magic, MagicRate                                          uint16
	AttackEquivItem, AttackEquivItemRate, StealItem, StealCount                     uint16
	Attack, MagicStrength, Defense, Dexterity, FleeRate, PoisonResistance           uint16
	ElementalResistance                                                             [ElementCount]uint16
	PhysicalResistance, DualMove, CollectValue                                      uint16
	Raw                                                                             [EnemyWords]uint16
}

type RuntimeTables struct {
	Roles   [RoleCount]Role
	Enemies []Enemy
	// EnemyTeams contains object IDs, not indexes into Enemies. Both 0 and
	// 0xffff are empty slots. Resolve an occupied slot through Game.Objects[id][0].
	EnemyTeams [][EnemyTeamSize]uint16
	Stores     [][StoreSize]uint16
	Source     FileInfo
}

// LoadRuntime imports only one unambiguously named, regular DATA.MKF file.
// These four table layouts are shared by the requested DOS/Windows import
// profiles. This function neither guesses a release nor decodes other resources.
func LoadRuntime(dir string) (*RuntimeTables, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	name := ""
	for _, entry := range entries {
		if !strings.EqualFold(entry.Name(), "DATA.MKF") {
			continue
		}
		if name != "" {
			return nil, fmt.Errorf("paldata runtime: ambiguous DATA.MKF: %q and %q", name, entry.Name())
		}
		info, err := entry.Info()
		if err != nil {
			return nil, err
		}
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("paldata runtime: DATA.MKF must be a regular file; symlinks are not accepted")
		}
		name = entry.Name()
	}
	if name == "" {
		return nil, fmt.Errorf("paldata runtime: missing DATA.MKF")
	}
	data, err := readBounded(filepath.Join(dir, name), MaxArchiveBytes)
	if err != nil {
		return nil, fmt.Errorf("paldata runtime DATA.MKF: %w", err)
	}
	archive, err := pal.Open(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("paldata runtime DATA.MKF: %w", err)
	}
	if archive.Count() < 4 || archive.Count() > 4096 {
		return nil, fmt.Errorf("paldata runtime DATA.MKF: chunk count %d outside 4..4096", archive.Count())
	}
	var chunks [4][]byte
	for i := range chunks {
		chunks[i], err = archive.ReadChunk(i)
		if err != nil {
			return nil, fmt.Errorf("paldata runtime DATA.MKF chunk %d: %w", i, err)
		}
	}
	tables, err := decodeRuntime(chunks)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(data)
	tables.Source = FileInfo{Name: name, SHA256: fmt.Sprintf("%x", sum), Size: int64(len(data))}
	return tables, nil
}

func decodeRuntime(chunks [4][]byte) (*RuntimeTables, error) {
	if len(chunks[3]) != RoleWords*RoleCount*2 {
		return nil, fmt.Errorf("DATA chunk3 roles: need exactly %d bytes, got %d", RoleWords*RoleCount*2, len(chunks[3]))
	}
	stores, err := recordCount(chunks[0], StoreSize*2, MaxRuntimeStores, "DATA chunk0 stores")
	if err != nil {
		return nil, err
	}
	enemies, err := recordCount(chunks[1], EnemyWords*2, MaxRuntimeEnemies, "DATA chunk1 enemies")
	if err != nil {
		return nil, err
	}
	teams, err := recordCount(chunks[2], EnemyTeamSize*2, MaxRuntimeTeams, "DATA chunk2 enemy teams")
	if err != nil {
		return nil, err
	}
	r := &RuntimeTables{Stores: make([][StoreSize]uint16, stores), Enemies: make([]Enemy, enemies), EnemyTeams: make([][EnemyTeamSize]uint16, teams)}
	for i := range r.Stores {
		for j := range r.Stores[i] {
			r.Stores[i][j] = word(chunks[0], i*StoreSize+j)
		}
	}
	for i := range r.EnemyTeams {
		for j := range r.EnemyTeams[i] {
			r.EnemyTeams[i][j] = word(chunks[2], i*EnemyTeamSize+j)
		}
	}
	for i := range r.Roles {
		raw := [RoleWords]uint16{}
		for a := range raw {
			raw[a] = word(chunks[3], a*RoleCount+i)
		}
		x := Role{ID: uint16(i), Raw: raw, Avatar: raw[0], BattleSprite: raw[1], Sprite: raw[2], NameIndex: raw[3], AttackAll: raw[4], Level: raw[6], MaxHP: raw[7], MaxMP: raw[8], HP: raw[9], MP: raw[10], Attack: raw[17], Magic: raw[18], Defense: raw[19], Dexterity: raw[20], FleeRate: raw[21], PoisonResistance: raw[22], CoveredBy: raw[31], WalkFrames: raw[64], CooperativeMagic: raw[65], DeathSound: raw[68], AttackSound: raw[69], WeaponSound: raw[70], CriticalSound: raw[71], MagicSound: raw[72], CoverSound: raw[73], DyingSound: raw[74]}
		copy(x.Equipment[:], raw[11:17])
		copy(x.ElementalResistance[:], raw[23:28])
		copy(x.Magics[:], raw[32:64])
		r.Roles[i] = x
	}
	for i := range r.Enemies {
		raw := [EnemyWords]uint16{}
		for j := range raw {
			raw[j] = word(chunks[1], i*EnemyWords+j)
		}
		x := Enemy{ID: uint16(i), Raw: raw, IdleFrames: raw[0], MagicFrames: raw[1], AttackFrames: raw[2], IdleAnimSpeed: raw[3], ActWaitFrames: raw[4], YPosOffset: raw[5], AttackSound: int16(raw[6]), ActionSound: int16(raw[7]), MagicSound: int16(raw[8]), DeathSound: int16(raw[9]), CallSound: int16(raw[10]), HP: raw[11], Exp: raw[12], Cash: raw[13], Level: raw[14], Magic: raw[15], MagicRate: raw[16], AttackEquivItem: raw[17], AttackEquivItemRate: raw[18], StealItem: raw[19], StealCount: raw[20], Attack: raw[21], MagicStrength: raw[22], Defense: raw[23], Dexterity: raw[24], FleeRate: raw[25], PoisonResistance: raw[26], PhysicalResistance: raw[32], DualMove: raw[33], CollectValue: raw[34]}
		copy(x.ElementalResistance[:], raw[27:32])
		r.Enemies[i] = x
	}
	return r, nil
}

// ValidateReferences checks only references whose interpretation is established
// by these structures. Game.Objects is a tagged-by-use union: arbitrary object
// records must not all be interpreted as enemies. No scripts are executed.
func (r *RuntimeTables) ValidateReferences(g *Game) error {
	if r == nil || g == nil {
		return fmt.Errorf("paldata runtime: nil tables or game")
	}
	objectRef := func(id uint16, where string) error {
		if id != 0 && int(id) >= len(g.Objects) {
			return fmt.Errorf("%s: object %d outside table (%d)", where, id, len(g.Objects))
		}
		return nil
	}
	for i, role := range r.Roles {
		if int(role.NameIndex) >= len(g.Words) {
			return fmt.Errorf("role %d: name index %d outside word table (%d)", i, role.NameIndex, len(g.Words))
		}
		if role.CoveredBy >= RoleCount {
			return fmt.Errorf("role %d: covered-by role %d outside 0..%d", i, role.CoveredBy, RoleCount-1)
		}
		for j, id := range role.Equipment {
			if err := objectRef(id, fmt.Sprintf("role %d equipment %d", i, j)); err != nil {
				return err
			}
		}
		for j, id := range role.Magics {
			if err := objectRef(id, fmt.Sprintf("role %d magic %d", i, j)); err != nil {
				return err
			}
		}
		if err := objectRef(role.CooperativeMagic, fmt.Sprintf("role %d cooperative magic", i)); err != nil {
			return err
		}
	}
	for i, enemy := range r.Enemies {
		for _, ref := range []struct {
			name string
			id   uint16
		}{{"magic", enemy.Magic}, {"attack item", enemy.AttackEquivItem}, {"steal item", enemy.StealItem}} {
			if ref.name == "magic" && ref.id == 0xffff {
				continue // public enemy AI uses 0xffff for a deliberate no-op spell
			}
			if err := objectRef(ref.id, fmt.Sprintf("enemy %d %s", i, ref.name)); err != nil {
				return err
			}
		}
	}
	for i, team := range r.EnemyTeams {
		for j, id := range team {
			if id == 0 || id == 0xffff {
				continue
			}
			if err := objectRef(id, fmt.Sprintf("team %d slot %d", i, j)); err != nil {
				return err
			}
			enemyID := g.Objects[id][0]
			if int(enemyID) >= len(r.Enemies) {
				return fmt.Errorf("team %d slot %d: object %d enemy index %d outside table (%d)", i, j, id, enemyID, len(r.Enemies))
			}
		}
	}
	for i, store := range r.Stores {
		for j, id := range store {
			if err := objectRef(id, fmt.Sprintf("store %d slot %d", i, j)); err != nil {
				return err
			}
		}
	}
	return nil
}
