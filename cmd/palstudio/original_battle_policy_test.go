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
	"github.com/HiChen85/pal--reverse--learn/internal/pal"
	"github.com/HiChen85/pal--reverse--learn/internal/palgame"
	"github.com/egoist/mygo/ui"
)

func originalNumericBattleFixture(t *testing.T) string {
	t.Helper()
	dir := originalMapRuntimeFixture(t)
	path := filepath.Join(dir, "DATA.MKF")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	archive, err := pal.Open(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	chunks := make([][]byte, archive.Count())
	for i := range chunks {
		chunks[i], err = archive.ReadChunk(i)
		if err != nil {
			t.Fatal(err)
		}
	}
	binary.LittleEndian.PutUint16(chunks[1][22:], 350)
	binary.LittleEndian.PutUint16(chunks[1][24:], 2)
	binary.LittleEndian.PutUint16(chunks[1][26:], 3)
	binary.LittleEndian.PutUint16(chunks[1][64:], 2)
	copy(chunks[2], originalWords(2, 0xffff, 0xffff, 0xffff, 0xffff))
	for role := 0; role < 6; role++ {
		for attr, value := range map[int]uint16{7: 1000, 8: 20, 9: 1000, 10: 20, 17: 50, 20: 100} {
			binary.LittleEndian.PutUint16(chunks[3][(attr*6+role)*2:], value)
		}
	}
	for i := 0; i < 100; i++ {
		binary.LittleEndian.PutUint16(chunks[14][i*2:], 1000)
	}
	if err := os.WriteFile(path, originalMKF(chunks...), 0600); err != nil {
		t.Fatal(err)
	}
	rewriteOriginalSSS(t, dir, func(c [][]byte) {
		c[4] = originalWords(0, 0, 0, 0, 7, 0, 0, 0, 0x1f, 1, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0)
	})
	return dir
}

func TestOriginalNativeFastBattleChoiceResetReportAndRestore(t *testing.T) {
	a, err := newWorkbench(content.Demo())
	if err != nil {
		t.Fatal(err)
	}
	a.tab = 5
	a.original = newOriginalData()
	o := a.original
	o.directory = originalNumericBattleFixture(t)
	o.exportPath = filepath.Join(t.TempDir(), "battle-report.json")
	o.savePath = filepath.Join(t.TempDir(), "battle-session.json")
	tt := ui.NewTester(a.view, 1400, 6000)
	click := func(label string) {
		t.Helper()
		if err := tt.Click(label); err != nil || a.failed {
			t.Fatal("native battle action failed", label, err, a.message)
		}
	}
	click("读取原版数据")
	before := o.session.Snapshot()
	if o.fastVerify || before.World.BattleMode != "auto_attack" {
		t.Fatal("fast verification became the default")
	}
	click(originalFastBattleChoice)
	if !o.fastVerify || !reflect.DeepEqual(before, o.session.Snapshot()) || !tt.HasText("当前会话战斗模式：auto_attack") || !tt.HasText("下次重置战斗模式：fast_verify") || !tt.HasText("当前会话沿用原模式") {
		t.Fatal("pending battle choice silently changed the active session or lost its explanation")
	}
	click("重置到选定场景")
	if o.session.Snapshot().World.BattleMode != "fast_verify" || !tt.HasText("当前会话战斗模式：fast_verify") || tt.HasText("当前会话沿用原模式") {
		t.Fatal("explicit reset did not install the selected policy")
	}
	click("逻辑会话")
	click("执行场景 OnEnter")
	click("执行事件触发脚本")
	record := o.history[len(o.history)-1]
	if !record.SessionCommitted || record.Report.BattleOutcomesStubbed || record.Report.Trace[0].Combat == nil || record.Report.Trace[0].Combat.Policy != "fast_verify" || record.Report.Trace[0].Combat.RawResult != 3 || len(record.Report.Trace[0].Combat.Reports) != 1 || o.session.Snapshot().World.Cash != 3 || o.session.Snapshot().World.Inventory[1] != 1 {
		t.Fatal("UI failed to run and report a real simplified numeric battle")
	}
	click("导出本次记录")
	data, err := os.ReadFile(o.exportPath)
	if err != nil {
		t.Fatal(err)
	}
	var exported originalEvidence
	if err := json.Unmarshal(data, &exported); err != nil {
		t.Fatal(err)
	}
	combat := exported.Invocation.Report.Trace[0].Combat
	if combat == nil || combat.Policy != "fast_verify" || !reflect.DeepEqual(combat.Simplifications, palgame.BattlePolicyNotes("fast_verify")) || exported.Invocation.Request.World.BattleMode != "fast_verify" {
		t.Fatal("export omitted the actual numerical policy or simplification scope")
	}
	click("保存会话 JSON")
	saved := o.session.Snapshot()
	click(originalFastBattleChoice)
	if o.fastVerify || o.session.Snapshot().World.BattleMode != "fast_verify" || !tt.HasText("下次重置战斗模式：auto_attack") || !tt.HasText("当前会话沿用原模式") {
		t.Fatal("disabling the choice modified a running fast session")
	}
	click("重置到选定场景")
	if o.session.Snapshot().World.BattleMode != "auto_attack" {
		t.Fatal("explicit normal reset failed")
	}
	click("读取会话 JSON")
	if !reflect.DeepEqual(saved, o.session.Snapshot()) || !o.fastVerify || !tt.HasText("当前会话战斗模式：fast_verify") || !tt.HasText("下次重置战斗模式：fast_verify") || tt.HasText("当前会话沿用原模式") {
		t.Fatal("restoring a fast session lost its policy or left misleading next-reset controls")
	}
}
