package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/HiChen85/pal--reverse--learn/internal/content"
	"github.com/egoist/mygo/ui"
)

func TestOriginalNativeEquipmentMenuCommitsAndExportsIntent(t *testing.T) {
	const helm, pin = 38, 39
	dir := originalMapRuntimeFixture(t)
	var equipEntry uint16
	rewriteOriginalSSS(t, dir, func(c [][]byte) {
		enter := uint16(len(c[4]) / 8)
		equipEntry = enter + 3
		binary.LittleEndian.PutUint16(c[1][2:], enter)
		c[2] = make([]byte, 12*(pin+1))
		copy(c[2][12*helm:], originalWords(0, 0, 0, equipEntry, 0, 2|1<<6))
		copy(c[2][12*pin:], originalWords(0, 0, 0, 0, 0, 2|1<<10))
		c[4] = append(c[4], originalWords(
			0x1f, helm, 2, 0,
			0x1f, pin, 1, 0,
			0, 0, 0, 0,
			0x18, 0xb, helm, 0,
			0x1a, 17, 31, 0,
			1, 0, 0, 0,
			0, 0, 0, 0,
		)...)
	})
	words := bytes.Repeat([]byte{' '}, 10*(pin+1))
	copy(words, "Tern")
	copy(words[10*helm:], "Slate Helm")
	copy(words[10*pin:], "Copper Pin")
	if err := os.WriteFile(filepath.Join(dir, "WORD.DAT"), words, 0600); err != nil {
		t.Fatal(err)
	}
	a, err := newWorkbench(content.Demo())
	if err != nil {
		t.Fatal(err)
	}
	a.tab, a.original = 5, newOriginalData()
	o := a.original
	o.directory = dir
	tt := ui.NewTester(a.view, 1400, 4000)
	for _, label := range []string{"读取原版数据", "逻辑会话"} {
		if err := tt.Click(label); err != nil || a.failed {
			t.Fatal(label, err, a.message)
		}
	}
	if err := tt.Click("装备"); err != nil || a.failed || len(o.history) != 0 {
		t.Fatal("pending scene must disable equipment", err, a.message)
	}
	if err := tt.Click("执行场景 OnEnter"); err != nil || a.failed {
		t.Fatal(err, a.message)
	}
	if o.equipItem != "38" || o.equipRole != "0" || !tt.HasText("当前装备 · Tern：无") || tt.HasText("Copper Pin") {
		t.Fatal("menu did not choose the only eligible stock and party role")
	}
	before := o.session.Snapshot()
	o.inputsJSON = `[{"kind":"key","value":1}]`
	if err := tt.Click("装备"); err != nil || !a.failed || !reflect.DeepEqual(before, o.session.Snapshot()) {
		t.Fatal("unused input failed to roll back the UI action", err, a.message)
	}
	o.inputsJSON = "[]"
	if err := tt.Click("装备"); err != nil || a.failed || !tt.HasText("当前装备 · Tern：Slate Helm") {
		t.Fatal("equipment view failed to update after commit", err, a.message)
	}
	r := o.history[len(o.history)-1]
	if !r.SessionCommitted || r.Kind != "equip_item" || r.ActionIntent.Item == nil || *r.ActionIntent.Item != helm || r.ActionIntent.TargetRole == nil || *r.ActionIntent.TargetRole != 0 || r.Request == nil || r.Request.Entry != equipEntry || r.Request.World.LastUnequippedItem != helm {
		t.Fatal("equipment evidence lost action intent or prepared invocation")
	}
	w := r.SessionAfter.World
	if w.Inventory[helm] != 1 || w.Roles[0][11] != helm || w.EquipmentEffects[0][17][0] != 31 || w.FrameNum != before.World.FrameNum {
		t.Fatal("UI action did not commit the script's stock and effects")
	}
	o.exportPath = filepath.Join(t.TempDir(), "equipment.json")
	if err := o.export(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(o.exportPath)
	if err != nil {
		t.Fatal(err)
	}
	var exported originalEvidence
	if err := json.Unmarshal(data, &exported); err != nil || !exported.Invocation.SessionCommitted || exported.Invocation.Kind != "equip_item" || exported.Invocation.SessionAfter.World.Roles[0][11] != helm {
		t.Fatal("export lost the committed equipment action", err)
	}
	history := len(o.history)
	o.directory += "-stale"
	tt.Frame()
	if err := tt.Click("装备"); err != nil || a.failed || len(o.history) != history || o.equipItem != "" || o.equipRole != "" {
		t.Fatal("changing source failed to clear and disable equipment selection", err)
	}
	o.directory = dir
	tt.Frame()
	if err := tt.Click("重置到选定场景"); err != nil || a.failed || o.equipItem != "" || o.equipRole != "" {
		t.Fatal("reset did not clear equipment selections", err, a.message)
	}
}
