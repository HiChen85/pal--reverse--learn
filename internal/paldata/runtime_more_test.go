package paldata

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func runtimeFullFixture() [15][]byte {
	var chunks [15][]byte
	base := runtimeFixture()
	copy(chunks[:], base[:])
	chunks[4] = make([]byte, MagicWords*2)
	chunks[5] = make([]byte, BattlefieldWords*2)
	chunks[6] = make([]byte, PlayableRoleCount*4)
	chunks[11] = make([]byte, BattleSpriteCount*2*2)
	chunks[13] = make([]byte, EnemyTeamSize*EnemyTeamSize*4)
	chunks[14] = make([]byte, (MaxLevel+1)*2)
	return chunks
}

func decodeFullFixture(t *testing.T, c [15][]byte) *RuntimeTables {
	t.Helper()
	r, err := decodeRuntime([4][]byte{c[0], c[1], c[2], c[3]})
	if err == nil {
		err = r.decodeBattleTables(c)
	}
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestRuntimeBattleTablesSyntheticGolden(t *testing.T) {
	c := runtimeFullFixture()
	// Literal synthetic record exercises both signed SHORTs and WORDs whose
	// meaning can become signed at individual engine use sites.
	c[4] = u16(101, 9, 0xfffe, 0xfffd, 0xfffc, 0xfffb, 106, 107, 0xfff8, 109, 110, 111, 112, 0xfffe, 6, 0xffff,
		201, 4, 202, 203, 204, 205, 206, 207, 208, 209, 210, 211, 212, 213, 1, 215)
	c[5] = u16(10, 0xffff, 0xfffe, 3, 4, 0x8000, 20, 1, 2, 3, 4, 5)
	c[6] = u16(1, 11, 2, 12, 3, 13, 4, 14, 5, 15, 6, 16, 7, 17, 8, 18, 9, 19, 10, 20)
	for _, chunk := range []int{11, 13, 14} {
		for j := 0; j < len(c[chunk])/2; j++ {
			binary.LittleEndian.PutUint16(c[chunk][j*2:], uint16(chunk*100+j))
		}
	}
	r := decodeFullFixture(t, c)
	want := Magic{ID: 0, Effect: 101, Type: 9, XOffset: -2, YOffset: -3, Specific: 0xfffc, LayerOffset: -4, Speed: -5, KeepEffect: 106, FireDelay: 107, EffectTimes: 0xfff8, Shake: 109, Wave: 110, Unknown: 111, CostMP: 112, BaseDamage: 0xfffe, Elemental: 6, Sound: -1,
		Raw: [16]uint16{101, 9, 0xfffe, 0xfffd, 0xfffc, 0xfffb, 106, 107, 0xfff8, 109, 110, 111, 112, 0xfffe, 6, 0xffff}}
	if !r.HasBattleTables || r.Magics[0] != want || r.Magics[1].ID != 1 || r.Magics[1].CostMP != 212 || r.Magics[1].Raw[15] != 215 {
		t.Fatal("magic width, signedness, union or record stride differs from synthetic golden")
	}
	if r.Battlefields[0] != (Battlefield{ID: 0, ScreenWave: 10, MagicEffect: [5]int16{-1, -2, 3, 4, -32768}, Raw: [6]uint16{10, 0xffff, 0xfffe, 3, 4, 0x8000}}) || r.Battlefields[1].ID != 1 || r.Battlefields[1].MagicEffect[4] != 5 {
		t.Fatal("battlefield width, signedness or stride")
	}
	if r.LevelUpMagics[0][4] != (LevelUpMagic{5, 15}) || r.LevelUpMagics[1][0] != (LevelUpMagic{6, 16}) || r.LevelUpMagics[1][4] != (LevelUpMagic{10, 20}) {
		t.Fatal("five playable role columns or level-up record stride")
	}
	if r.BattleEffectIndex[0] != [2]uint16{1100, 1101} || r.BattleEffectIndex[9] != [2]uint16{1118, 1119} || r.EnemyPositions[0][4] != (Position{1308, 1309}) || r.EnemyPositions[4][0] != (Position{1340, 1341}) || r.EnemyPositions[4][4] != (Position{1348, 1349}) || r.LevelUpExp[0] != 1400 || r.LevelUpExp[99] != 1499 {
		t.Fatal("fixed-array layout or enemy-slot/team-size axis transposed")
	}
	c[4][0] = 0
	c[5][0] = 0
	c[6][0] = 0
	if r.Magics[0].Raw[0] != 101 || r.Battlefields[0].Raw[0] != 10 || r.LevelUpMagics[0][0].Level != 1 {
		t.Fatal("decoded tables alias input bytes")
	}
}

func TestRuntimeMoreMalformedTables(t *testing.T) {
	for _, chunk := range []int{4, 5, 6, 11, 13, 14} {
		for _, delta := range []int{-2, -1, 1, 2} {
			c := runtimeFullFixture()
			if delta < 0 {
				c[chunk] = c[chunk][:len(c[chunk])+delta]
			} else {
				c[chunk] = append(c[chunk], make([]byte, delta)...)
			}
			if err := (&RuntimeTables{}).decodeBattleTables(c); err == nil {
				t.Fatalf("accepted chunk %d delta %d", chunk, delta)
			}
		}
		c := runtimeFullFixture()
		c[chunk] = nil
		if err := (&RuntimeTables{}).decodeBattleTables(c); err == nil {
			t.Fatalf("accepted absent chunk %d", chunk)
		}
	}
	for _, tc := range []struct{ chunk, count, width int }{{4, MaxRuntimeMagics, MagicWords * 2}, {5, MaxRuntimeBattlefields, BattlefieldWords * 2}, {6, MaxRuntimeLevelUpMagics, PlayableRoleCount * 4}} {
		c := runtimeFullFixture()
		c[tc.chunk] = make([]byte, (tc.count+1)*tc.width)
		if err := (&RuntimeTables{}).decodeBattleTables(c); err == nil {
			t.Fatalf("accepted excessive chunk %d", tc.chunk)
		}
	}
	dir := t.TempDir()
	c := runtimeFullFixture()
	if err := os.WriteFile(filepath.Join(dir, "DATA.MKF"), mkf(c[:14]...), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadRuntime(dir); err == nil || !strings.Contains(err.Error(), "15..4096") {
		t.Fatalf("accepted incomplete runtime archive: %v", err)
	}
}

func TestInitialExperienceIsSeparateAndUsesEveryRoleLevel(t *testing.T) {
	r := decodeFullFixture(t, runtimeFullFixture())
	for i := range r.Roles {
		r.Roles[i].Level = uint16(i * 3)
		for j := range r.Roles[i].Raw {
			r.Roles[i].Raw[j] = 60000 // no unknown attribute is an EXP source
		}
	}
	x := r.InitialExperience()
	for _, track := range [][RoleCount]Experience{x.Primary, x.Health, x.Magic, x.Attack, x.MagicPower, x.Defense, x.Dexterity, x.Flee} {
		for i, value := range track {
			if value != (Experience{Level: uint16(i * 3)}) {
				t.Fatalf("role %d: experience was not zeroed then assigned its level", i)
			}
		}
	}
	if !reflect.DeepEqual(x.Primary, x.Flee) {
		t.Fatal("experience tracks differ")
	}
	x.Primary[1].Exp = 7
	if x.Health[1].Exp != 0 || r.InitialExperience().Primary[1].Exp != 0 {
		t.Fatal("initial experience tracks alias")
	}
}

func TestExplicitObjectUnionViewsUseNormalizedWords(t *testing.T) {
	g := &Game{Objects: [][7]uint16{{10, 11, 12, 13, 14, 15, 16}}}
	m, err := g.MagicObject(0)
	if err != nil || m != (MagicObject{10, 11, 12, 13, 14, 15, 16}) {
		t.Fatal("magic union offsets")
	}
	e, err := g.EnemyObject(0)
	if err != nil || e != (EnemyObject{10, 11, 12, 13, 14}) {
		t.Fatal("enemy union offsets")
	}
	item, err := g.ItemObject(0)
	if err != nil || item != (ItemObject{10, 11, 12, 13, 14, 15, 16}) {
		t.Fatal("item union offsets")
	}
	p, err := g.PoisonObject(0)
	if err != nil || p != (PoisonObject{10, 11, 12, 13, 14}) {
		t.Fatal("poison union offsets")
	}
	player, err := g.PlayerObject(0)
	if err != nil || player != (PlayerObject{[2]uint16{10, 11}, 12, 13}) {
		t.Fatal("player union offsets")
	}
	if _, err := g.MagicObject(1); err == nil {
		t.Fatal("accepted missing object")
	}
	var nilGame *Game
	if _, err := nilGame.EnemyObject(0); err == nil {
		t.Fatal("accepted nil game")
	}
	// DOS readObjects has already moved flags from raw word five to word six.
	g = &Game{Layout: "dos", ObjectWords: 6}
	if err := readObjects(g, u16(10, 11, 12, 13, 14, 16)); err != nil {
		t.Fatal(err)
	}
	m, err = g.MagicObject(0)
	if err != nil || m.Magic != 10 || m.OnSuccess != 12 || m.OnUse != 13 || m.Flags != 16 || m.Reserved2 != 0 {
		t.Fatal("DOS normalized object offsets")
	}
}

func TestRuntimeMagicReferencesAreValidatedOnlyForKnownKinds(t *testing.T) {
	fresh := func() (*RuntimeTables, *Game) {
		r := decodeFullFixture(t, runtimeFullFixture())
		g := &Game{Words: []string{"zero"}, Scripts: make([]Instruction, 2), Objects: make([][7]uint16, 5)}
		r.Roles[0].Magics[0] = 1
		r.LevelUpMagics[0][4] = LevelUpMagic{Level: 17, Magic: 2}
		g.Objects[1] = [7]uint16{0, 0, 1, 1}
		g.Objects[2] = [7]uint16{0, 0, 1, 1}
		g.Objects[3][0] = 65535 // arbitrary unused union member is not magic
		r.Stores[0][0] = 3      // item bitmap is not a magic index
		r.Enemies[0].Magic = 0xffff
		return r, g
	}
	r, g := fresh()
	if err := r.ValidateReferences(g); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		change func(*RuntimeTables, *Game)
	}{
		{"role magic table", func(_ *RuntimeTables, g *Game) { g.Objects[1][0] = 1 }},
		{"learning object", func(r *RuntimeTables, _ *Game) { r.LevelUpMagics[0][4].Magic = 5 }},
		{"learning magic table", func(_ *RuntimeTables, g *Game) { g.Objects[2][0] = 1 }},
		{"on success script", func(_ *RuntimeTables, g *Game) { g.Objects[2][2] = 2 }},
		{"on use script", func(_ *RuntimeTables, g *Game) { g.Objects[1][3] = 2 }},
		{"magic description", func(_ *RuntimeTables, g *Game) { g.Objects[1][4] = 2 }},
		{"enemy magic", func(r *RuntimeTables, _ *Game) { r.Enemies[0].Magic = 3 }},
		{"role level", func(r *RuntimeTables, _ *Game) { r.Roles[0].Level = 100 }},
		{"role battle sprite", func(r *RuntimeTables, _ *Game) { r.Roles[0].BattleSprite = 10 }},
		{"player callback", func(_ *RuntimeTables, g *Game) { g.Objects[0][2] = 2 }},
		{"item callback", func(_ *RuntimeTables, g *Game) { g.Objects[3][3] = 2 }},
		{"item description", func(_ *RuntimeTables, g *Game) { g.Objects[3][5] = 2 }},
		{"enemy callback", func(r *RuntimeTables, g *Game) { r.EnemyTeams[0][0] = 4; g.Objects[4][4] = 2 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, g := fresh()
			tc.change(r, g)
			if err := r.ValidateReferences(g); err == nil {
				t.Fatal("accepted out-of-range known-kind reference")
			}
		})
	}
	// A DOS magic's word four is reserved, even though the normalized union
	// leaves it in the Windows description position. Do not turn it into PC.
	r, g = fresh()
	g.Layout = "dos"
	g.Objects[1][4] = 65535
	if err := r.ValidateReferences(g); err != nil {
		t.Fatal(err)
	}
}
