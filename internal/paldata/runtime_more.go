package paldata

import "fmt"

// Format reference: SDLPal d4bf69b64d335cd680555af9df90e7ec4f3006a3,
// global.h (MAGIC, BATTLEFIELD, LEVELUPMAGIC_ALL, ENEMYPOS, GAMEDATA)
// and global.c PAL_ReadGlobalGameData (DATA chunks 4, 5, 6, 11, 13, 14).
const (
	MagicWords              = 16
	BattlefieldWords        = 1 + ElementCount
	PlayableRoleCount       = 5
	BattleSpriteCount       = 10
	MaxLevel                = 99
	MaxRuntimeMagics        = 65536
	MaxRuntimeBattlefields  = 65536
	MaxRuntimeLevelUpMagics = 65536
)

// Magic is a DATA.MKF chunk 4 record. Offsets are cast to SHORT at use sites;
// Speed and Sound are SHORT in the public structure. Specific is a union: a
// summon sprite WORD or LayerOffset SHORT. BaseDamage and EffectTimes retain
// their WORD bits because the engine chooses their signedness at each use.
type Magic struct {
	ID, Effect, Type                                         uint16
	XOffset, YOffset                                         int16
	Specific                                                 uint16
	LayerOffset, Speed                                       int16
	KeepEffect, FireDelay, EffectTimes, Shake, Wave, Unknown uint16
	CostMP, BaseDamage, Elemental                            uint16
	Sound                                                    int16
	Raw                                                      [MagicWords]uint16
}

type Battlefield struct {
	ID, ScreenWave uint16
	MagicEffect    [ElementCount]int16
	Raw            [BattlefieldWords]uint16
}

type LevelUpMagic struct {
	Level uint16
	// Magic is an object ID, whose object word zero selects the magic table.
	Magic uint16
}

type Position struct{ X, Y uint16 }

// Experience matches the separate save/runtime EXPERIENCE structure. These
// values are never attributes in Role.Raw.
type Experience struct{ Exp, Reserved, Level, Count uint16 }

type InitExp struct {
	Primary, Health, Magic, Attack, MagicPower, Defense, Dexterity, Flee [RoleCount]Experience
}

// InitialExperience applies PAL_LoadDefaultGame's zero-initialize-then-level
// rule for all eight experience tracks, including role slot five and level 0.
func (r *RuntimeTables) InitialExperience() InitExp {
	var out InitExp
	if r != nil {
		for i, role := range r.Roles {
			for _, track := range []*[RoleCount]Experience{&out.Primary, &out.Health, &out.Magic, &out.Attack, &out.MagicPower, &out.Defense, &out.Dexterity, &out.Flee} {
				track[i].Level = role.Level
			}
		}
	}
	return out
}

func (r *RuntimeTables) decodeBattleTables(chunks [15][]byte) error {
	magics, err := recordCount(chunks[4], MagicWords*2, MaxRuntimeMagics, "DATA chunk4 magics")
	if err != nil {
		return err
	}
	fields, err := recordCount(chunks[5], BattlefieldWords*2, MaxRuntimeBattlefields, "DATA chunk5 battlefields")
	if err != nil {
		return err
	}
	learning, err := recordCount(chunks[6], PlayableRoleCount*4, MaxRuntimeLevelUpMagics, "DATA chunk6 level-up magics")
	if err != nil {
		return err
	}
	for _, fixed := range []struct {
		chunk, bytes int
		name         string
	}{
		{11, BattleSpriteCount * 2 * 2, "battle effect index"},
		{13, EnemyTeamSize * EnemyTeamSize * 4, "enemy positions"},
		{14, (MaxLevel + 1) * 2, "level-up experience"},
	} {
		if len(chunks[fixed.chunk]) != fixed.bytes {
			return fmt.Errorf("DATA chunk%d %s: need exactly %d bytes, got %d", fixed.chunk, fixed.name, fixed.bytes, len(chunks[fixed.chunk]))
		}
	}
	r.Magics = make([]Magic, magics)
	for i := range r.Magics {
		var raw [MagicWords]uint16
		for j := range raw {
			raw[j] = word(chunks[4], i*MagicWords+j)
		}
		r.Magics[i] = Magic{ID: uint16(i), Effect: raw[0], Type: raw[1], XOffset: int16(raw[2]), YOffset: int16(raw[3]), Specific: raw[4], LayerOffset: int16(raw[4]), Speed: int16(raw[5]), KeepEffect: raw[6], FireDelay: raw[7], EffectTimes: raw[8], Shake: raw[9], Wave: raw[10], Unknown: raw[11], CostMP: raw[12], BaseDamage: raw[13], Elemental: raw[14], Sound: int16(raw[15]), Raw: raw}
	}
	r.Battlefields = make([]Battlefield, fields)
	for i := range r.Battlefields {
		x := Battlefield{ID: uint16(i)}
		for j := range x.Raw {
			x.Raw[j] = word(chunks[5], i*BattlefieldWords+j)
		}
		x.ScreenWave = x.Raw[0]
		for j := range x.MagicEffect {
			x.MagicEffect[j] = int16(x.Raw[j+1])
		}
		r.Battlefields[i] = x
	}
	r.LevelUpMagics = make([][PlayableRoleCount]LevelUpMagic, learning)
	for i := range r.LevelUpMagics {
		for j := range r.LevelUpMagics[i] {
			base := (i*PlayableRoleCount + j) * 2
			r.LevelUpMagics[i][j] = LevelUpMagic{Level: word(chunks[6], base), Magic: word(chunks[6], base+1)}
		}
	}
	for i := range r.BattleEffectIndex {
		for j := range r.BattleEffectIndex[i] {
			r.BattleEffectIndex[i][j] = word(chunks[11], i*2+j)
		}
	}
	for i := range r.EnemyPositions {
		for j := range r.EnemyPositions[i] {
			base := (i*EnemyTeamSize + j) * 2
			r.EnemyPositions[i][j] = Position{X: word(chunks[13], base), Y: word(chunks[13], base+1)}
		}
	}
	for i := range r.LevelUpExp {
		r.LevelUpExp[i] = word(chunks[14], i)
	}
	r.HasBattleTables = true
	return nil
}

// These views require a caller to know the object's union member. They do not
// classify arbitrary objects from their numeric contents or flags.
type MagicObject struct {
	Magic, Reserved1, OnSuccess, OnUse, Description, Reserved2, Flags uint16
}
type EnemyObject struct{ Enemy, ResistanceToSorcery, OnTurnStart, OnBattleEnd, OnReady uint16 }
type ItemObject struct{ Bitmap, Price, OnUse, OnEquip, OnThrow, Description, Flags uint16 }
type PoisonObject struct{ Level, Color, PlayerScript, Reserved, EnemyScript uint16 }
type PlayerObject struct {
	Reserved               [2]uint16
	OnFriendDeath, OnDying uint16
}

func (g *Game) objectRecord(id uint16) ([7]uint16, error) {
	if g == nil || int(id) >= len(g.Objects) {
		return [7]uint16{}, fmt.Errorf("paldata object %d is outside imported table", id)
	}
	return g.Objects[id], nil
}
func (g *Game) MagicObject(id uint16) (MagicObject, error) {
	x, err := g.objectRecord(id)
	return MagicObject{Magic: x[0], Reserved1: x[1], OnSuccess: x[2], OnUse: x[3], Description: x[4], Reserved2: x[5], Flags: x[6]}, err
}
func (g *Game) EnemyObject(id uint16) (EnemyObject, error) {
	x, err := g.objectRecord(id)
	return EnemyObject{Enemy: x[0], ResistanceToSorcery: x[1], OnTurnStart: x[2], OnBattleEnd: x[3], OnReady: x[4]}, err
}
func (g *Game) ItemObject(id uint16) (ItemObject, error) {
	x, err := g.objectRecord(id)
	return ItemObject{Bitmap: x[0], Price: x[1], OnUse: x[2], OnEquip: x[3], OnThrow: x[4], Description: x[5], Flags: x[6]}, err
}
func (g *Game) PoisonObject(id uint16) (PoisonObject, error) {
	x, err := g.objectRecord(id)
	return PoisonObject{Level: x[0], Color: x[1], PlayerScript: x[2], Reserved: x[3], EnemyScript: x[4]}, err
}
func (g *Game) PlayerObject(id uint16) (PlayerObject, error) {
	x, err := g.objectRecord(id)
	return PlayerObject{Reserved: [2]uint16{x[0], x[1]}, OnFriendDeath: x[2], OnDying: x[3]}, err
}

func (r *RuntimeTables) validateBattleReferences(g *Game) error {
	refs := func(where string, scripts ...uint16) error {
		for _, id := range scripts {
			if id != 0 {
				if err := scriptRef(g, id, where); err != nil {
					return err
				}
			}
		}
		return nil
	}
	itemRef := func(id uint16, where string) error {
		if id == 0 {
			return nil
		}
		object, err := g.ItemObject(id)
		if err != nil {
			return fmt.Errorf("%s: %w", where, err)
		}
		return refs(where+" callbacks", object.OnUse, object.OnEquip, object.OnThrow, object.Description)
	}
	// Only objects reached through documented magic-valued fields are magic
	// objects. Item bitmaps and enemy indexes can share their numeric range.
	magicRef := func(id uint16, where string) error {
		if id == 0 {
			return nil
		}
		object, err := g.MagicObject(id)
		if err != nil {
			return fmt.Errorf("%s: %w", where, err)
		}
		if int(object.Magic) >= len(r.Magics) {
			return fmt.Errorf("%s: object %d magic index %d outside table (%d)", where, id, object.Magic, len(r.Magics))
		}
		for _, script := range []struct {
			id   uint16
			name string
		}{{object.OnSuccess, "on success"}, {object.OnUse, "on use"}} {
			if script.id != 0 {
				if err := scriptRef(g, script.id, where+" "+script.name); err != nil {
					return err
				}
			}
		}
		if g.Layout != "dos" && object.Description != 0 {
			if err := scriptRef(g, object.Description, where+" description"); err != nil {
				return err
			}
		}
		return nil
	}
	for i, role := range r.Roles {
		if role.Level > MaxLevel {
			return fmt.Errorf("role %d: level %d outside experience table 0..%d", i, role.Level, MaxLevel)
		}
		if role.BattleSprite >= BattleSpriteCount {
			return fmt.Errorf("role %d: battle sprite %d outside battle effect index table (%d)", i, role.BattleSprite, BattleSpriteCount)
		}
		// The combat engine uses the role's name object for the player
		// callbacks; its word-table label is validated separately.
		player, err := g.PlayerObject(role.NameIndex)
		if err != nil {
			return fmt.Errorf("role %d name object: %w", i, err)
		}
		if err := refs(fmt.Sprintf("role %d player callbacks", i), player.OnFriendDeath, player.OnDying); err != nil {
			return err
		}
		for j, id := range role.Equipment {
			if err := itemRef(id, fmt.Sprintf("role %d equipment %d", i, j)); err != nil {
				return err
			}
		}
		for j, id := range role.Magics {
			if err := magicRef(id, fmt.Sprintf("role %d magic %d", i, j)); err != nil {
				return err
			}
		}
		if err := magicRef(role.CooperativeMagic, fmt.Sprintf("role %d cooperative magic", i)); err != nil {
			return err
		}
	}
	for i, enemy := range r.Enemies {
		if err := itemRef(enemy.AttackEquivItem, fmt.Sprintf("enemy %d attack item", i)); err != nil {
			return err
		}
		if err := itemRef(enemy.StealItem, fmt.Sprintf("enemy %d steal item", i)); err != nil {
			return err
		}
		if enemy.Magic != 0xffff {
			if err := magicRef(enemy.Magic, fmt.Sprintf("enemy %d magic", i)); err != nil {
				return err
			}
		}
	}
	for i, team := range r.EnemyTeams {
		for j, id := range team {
			if id == 0 || id == 0xffff {
				continue
			}
			object, err := g.EnemyObject(id)
			if err != nil {
				return fmt.Errorf("team %d slot %d: %w", i, j, err)
			}
			if err := refs(fmt.Sprintf("team %d slot %d enemy callbacks", i, j), object.OnTurnStart, object.OnBattleEnd, object.OnReady); err != nil {
				return err
			}
		}
	}
	for i, store := range r.Stores {
		for j, id := range store {
			if err := itemRef(id, fmt.Sprintf("store %d slot %d", i, j)); err != nil {
				return err
			}
		}
	}
	for i, row := range r.LevelUpMagics {
		for j, entry := range row {
			if err := magicRef(entry.Magic, fmt.Sprintf("level-up magic row %d role %d", i, j)); err != nil {
				return err
			}
		}
	}
	return nil
}
