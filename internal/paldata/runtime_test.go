package paldata

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func runtimeFixture() [4][]byte {
	var c [4][]byte
	c[0] = make([]byte, StoreSize*2)
	c[1] = make([]byte, EnemyWords*2)
	c[2] = make([]byte, EnemyTeamSize*2)
	c[3] = make([]byte, RoleWords*RoleCount*2)
	return c
}

func TestRuntimeRoleSoATransposePreservesUnknownFields(t *testing.T) {
	chunks := runtimeFixture()
	for attr := 0; attr < RoleWords; attr++ {
		for role := 0; role < RoleCount; role++ {
			binary.LittleEndian.PutUint16(chunks[3][(attr*RoleCount+role)*2:], uint16(attr*100+role))
		}
	}
	r, err := decodeRuntime(chunks)
	if err != nil {
		t.Fatal(err)
	}
	for i, role := range r.Roles {
		for a, v := range role.Raw {
			if want := uint16(a*100 + i); v != want {
				t.Fatalf("role %d attr %d=%d, want %d", i, a, v, want)
			}
		}
		if role.ID != uint16(i) || role.Avatar != uint16(i) || role.BattleSprite != uint16(100+i) || role.Sprite != uint16(200+i) || role.NameIndex != uint16(300+i) {
			t.Fatal("identity field offsets")
		}
		if role.Level != uint16(600+i) || role.MaxHP != uint16(700+i) || role.MaxMP != uint16(800+i) || role.HP != uint16(900+i) || role.MP != uint16(1000+i) {
			t.Fatal("health offsets")
		}
		if role.Attack != uint16(1700+i) || role.Magic != uint16(1800+i) || role.Defense != uint16(1900+i) || role.Dexterity != uint16(2000+i) || role.FleeRate != uint16(2100+i) {
			t.Fatal("combat offsets")
		}
		if role.Equipment[0] != uint16(1100+i) || role.Equipment[5] != uint16(1600+i) || role.ElementalResistance[4] != uint16(2700+i) {
			t.Fatal("equipment/resistance offsets")
		}
		if role.Magics[0] != uint16(3200+i) || role.Magics[31] != uint16(6300+i) || role.CoveredBy != uint16(3100+i) || role.WalkFrames != uint16(6400+i) || role.CooperativeMagic != uint16(6500+i) || role.DyingSound != uint16(7400+i) {
			t.Fatal("trailing offsets")
		}
	}
	chunks[3][0] = 99
	if r.Roles[0].Raw[0] != 0 {
		t.Fatal("decoded role aliases source")
	}
}

func TestRuntimeEnemyRecordWidthsAndSignedSounds(t *testing.T) {
	c := runtimeFixture()
	c[1] = make([]byte, 2*EnemyWords*2)
	for i := 0; i < 2; i++ {
		for j := 0; j < EnemyWords; j++ {
			binary.LittleEndian.PutUint16(c[1][(i*EnemyWords+j)*2:], uint16(i*100+j))
		}
	}
	for j := 6; j <= 10; j++ {
		binary.LittleEndian.PutUint16(c[1][j*2:], uint16(65535-(j-6)))
	}
	c[0] = u16(1, 2, 3, 4, 5, 6, 7, 8, 9)
	c[2] = u16(1, 0xffff, 0, 2, 3)
	r, err := decodeRuntime(c)
	if err != nil {
		t.Fatal(err)
	}
	e := r.Enemies[0]
	if e.AttackSound != -1 || e.ActionSound != -2 || e.MagicSound != -3 || e.DeathSound != -4 || e.CallSound != -5 {
		t.Fatalf("SHORT sounds: %+v", e)
	}
	if e.HP != 11 || e.Exp != 12 || e.Cash != 13 || e.Level != 14 || e.Magic != 15 || e.Attack != 21 || e.MagicStrength != 22 || e.Defense != 23 || e.Dexterity != 24 || e.ElementalResistance[4] != 31 || e.CollectValue != 34 {
		t.Fatal("enemy field offsets")
	}
	if r.Enemies[1].ID != 1 || r.Enemies[1].HP != 111 || r.Enemies[1].Raw[34] != 134 {
		t.Fatal("enemy record stride")
	}
	if r.Stores[0] != [9]uint16{1, 2, 3, 4, 5, 6, 7, 8, 9} || r.EnemyTeams[0] != [5]uint16{1, 0xffff, 0, 2, 3} {
		t.Fatal("store/team dimensions")
	}
}

func TestRuntimeMalformedTables(t *testing.T) {
	for i := 0; i < 4; i++ {
		for _, delta := range []int{-1, 1} {
			c := runtimeFixture()
			if delta < 0 {
				c[i] = c[i][:len(c[i])-1]
			} else {
				c[i] = append(c[i], 0)
			}
			if _, err := decodeRuntime(c); err == nil {
				t.Fatalf("accepted chunk %d with delta %d", i, delta)
			}
		}
		c := runtimeFixture()
		c[i] = nil
		if _, err := decodeRuntime(c); err == nil {
			t.Fatalf("accepted empty table %d", i)
		}
	}
	for i, limit := range []int{MaxRuntimeStores * StoreSize * 2, MaxRuntimeEnemies * EnemyWords * 2, MaxRuntimeTeams * EnemyTeamSize * 2} {
		c := runtimeFixture()
		c[i] = make([]byte, limit+[]int{StoreSize, EnemyWords, EnemyTeamSize}[i]*2)
		if _, err := decodeRuntime(c); err == nil {
			t.Fatalf("accepted excessive table %d", i)
		}
	}
}

func TestRuntimeReferencesUseObjectIndirectionAndSentinels(t *testing.T) {
	fresh := func() (*RuntimeTables, *Game) {
		r, err := decodeRuntime(runtimeFixture())
		if err != nil {
			t.Fatal(err)
		}
		g := &Game{Words: []string{"zero", "one"}, Objects: make([][7]uint16, 4)}
		r.EnemyTeams[0] = [5]uint16{3, 0xffff, 0, 0, 0} // Object3 -> enemy0. Direct indexing would reject it.
		r.Enemies[0].Magic = 0xffff                     // A no-op spell in the public enemy AI.
		return r, g
	}
	r, g := fresh()
	if err := r.ValidateReferences(g); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name   string
		mutate func(*RuntimeTables, *Game)
	}{
		{"name", func(r *RuntimeTables, _ *Game) { r.Roles[2].NameIndex = 2 }},
		{"cover", func(r *RuntimeTables, _ *Game) { r.Roles[0].CoveredBy = 6 }},
		{"equipment", func(r *RuntimeTables, _ *Game) { r.Roles[0].Equipment[0] = 4 }},
		{"magic", func(r *RuntimeTables, _ *Game) { r.Roles[0].Magics[31] = 4 }},
		{"cooperative", func(r *RuntimeTables, _ *Game) { r.Roles[0].CooperativeMagic = 4 }},
		{"enemy magic", func(r *RuntimeTables, _ *Game) { r.Enemies[0].Magic = 4 }},
		{"enemy attack item", func(r *RuntimeTables, _ *Game) { r.Enemies[0].AttackEquivItem = 4 }},
		{"enemy steal item", func(r *RuntimeTables, _ *Game) { r.Enemies[0].StealItem = 4 }},
		{"team object", func(r *RuntimeTables, _ *Game) { r.EnemyTeams[0][0] = 4 }},
		{"team enemy", func(_ *RuntimeTables, g *Game) { g.Objects[3][0] = 1 }},
		{"store", func(r *RuntimeTables, _ *Game) { r.Stores[0][8] = 4 }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, g := fresh()
			tc.mutate(r, g)
			if err := r.ValidateReferences(g); err == nil {
				t.Fatal("accepted out-of-bounds reference")
			}
		})
	}
	if err := r.ValidateReferences(nil); err == nil {
		t.Fatal("nil game")
	}
	var absent *RuntimeTables
	if err := absent.ValidateReferences(g); err == nil {
		t.Fatal("nil tables")
	}
}

func TestLoadRuntimeOnlyNeedsBoundedRegularData(t *testing.T) {
	c := runtimeFixture()
	dir := t.TempDir()
	path := filepath.Join(dir, "dAtA.mKf")
	if err := os.WriteFile(path, mkf(c[:]...), 0600); err != nil {
		t.Fatal(err)
	}
	r, err := LoadRuntime(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Enemies) != 1 || len(r.EnemyTeams) != 1 || len(r.Stores) != 1 || r.Source.Name != "dAtA.mKf" || r.Source.Size != int64(len(mkf(c[:]...))) || len(r.Source.SHA256) != 64 {
		t.Fatal("runtime metadata")
	}
	if err := os.WriteFile(filepath.Join(dir, "DATA.MKF"), mkf(c[:]...), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadRuntime(dir); err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("ambiguous: %v", err)
	}
	if err := os.Remove(filepath.Join(dir, "DATA.MKF")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadRuntime(dir); err == nil {
		t.Fatal("missing")
	}
	if err := os.Symlink("outside", path); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadRuntime(dir); err == nil {
		t.Fatal("symlink")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadRuntime(dir); err == nil {
		t.Fatal("directory")
	}
}

// Optional local check prints counts only. Proprietary records and text are
// never included in source, fixtures, or test output.
func TestRuntimeLocalInput(t *testing.T) {
	dir := os.Getenv("PALDATA_RUNTIME_DIR")
	if dir == "" {
		t.Skip("set PALDATA_RUNTIME_DIR for authorized local metadata probe")
	}
	r, err := LoadRuntime(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("runtime table counts: roles=%d enemies=%d teams=%d stores=%d", len(r.Roles), len(r.Enemies), len(r.EnemyTeams), len(r.Stores))
	if cp := os.Getenv("PALDATA_RUNTIME_ENCODING"); cp != "" {
		layout := os.Getenv("PALDATA_RUNTIME_LAYOUT")
		if layout == "" {
			t.Fatal("PALDATA_RUNTIME_LAYOUT must be explicit for reference probe")
		}
		g, err := Load(dir, Options{Encoding: cp, Layout: layout})
		if err != nil {
			t.Fatal(err)
		}
		if err := r.ValidateReferences(g); err != nil {
			t.Fatal(err)
		}
		t.Log("runtime references validated against explicitly selected layout and encoding")
	}
}
