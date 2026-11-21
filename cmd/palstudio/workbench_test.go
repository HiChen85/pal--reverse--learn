package main

import (
	"github.com/HiChen85/pal--reverse--learn/internal/battle"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/HiChen85/pal--reverse--learn/internal/content"
	"github.com/egoist/mygo/ui"
)

func TestNativeEditingAndSave(t *testing.T) {
	a, err := newWorkbench(content.Demo())
	if err != nil {
		t.Fatal(err)
	}
	tt := ui.NewTester(a.view, 1200, 1000)
	if err := tt.Click("新增节点"); err != nil {
		t.Fatal(err)
	}
	if !a.dirty || len(a.draft.Nodes) != 8 {
		t.Fatal("node was not appended to editable draft")
	}
	if err := tt.Click("校验并应用"); err != nil {
		t.Fatal(err)
	}
	if a.failed || a.dirty {
		t.Fatalf("apply failed: %s", a.message)
	}
	if err := tt.Click("人物"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("新增人物"); err != nil {
		t.Fatal(err)
	}
	if len(a.draft.Characters) != 3 {
		t.Fatal("character not appended")
	}
	if err := tt.Click("物品"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("新增物品"); err != nil {
		t.Fatal(err)
	}
	if len(a.draft.Items) != 3 {
		t.Fatal("item not appended")
	}
	a.packPath = filepath.Join(t.TempDir(), "edited.json")
	if err := tt.Click("保存内容包"); err != nil {
		t.Fatal(err)
	}
	if a.failed {
		t.Fatal(a.message)
	}
	p, err := content.Load(a.packPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Characters) != 3 || len(p.Items) != 3 || len(p.Nodes) != 8 {
		t.Fatal("saved pack lost draft edits")
	}
}

func TestNativePreviewAndProgress(t *testing.T) {
	a, err := newWorkbench(content.Demo())
	if err != nil {
		t.Fatal(err)
	}
	a.tab = 3
	tt := ui.NewTester(a.view, 1200, 1000)
	if err := tt.Click("继续剧情"); err != nil {
		t.Fatal(err)
	}
	if a.service.Current().Node.ID != "crossroads" {
		t.Fatal("continue did not reach choice")
	}
	if err := tt.Click("拜访守井人"); err != nil {
		t.Fatal(err)
	}
	if a.service.Current().Node.ID != "well" {
		t.Fatal("visible choice not forwarded")
	}
	a.progressPath = filepath.Join(t.TempDir(), "progress.json")
	if err := tt.Click("保存进度"); err != nil {
		t.Fatal(err)
	}
	if a.failed {
		t.Fatal(a.message)
	}
	if err := tt.Click("继续剧情"); err != nil {
		t.Fatal(err)
	}
	if a.service.Current().Battle == nil {
		t.Fatal("battle preview not shown")
	}
	if err := tt.Click("攻击"); err != nil {
		t.Fatal(err)
	}
	if a.failed {
		t.Fatal(a.message)
	}
	if err := tt.Click("读取进度"); err != nil {
		t.Fatal(err)
	}
	if a.failed || a.service.Current().Node.ID != "well" {
		t.Fatal("progress did not restore choice", a.message)
	}
}

func TestJSONEditsRequireExplicitImport(t *testing.T) {
	a, err := newWorkbench(content.Demo())
	if err != nil {
		t.Fatal(err)
	}
	a.jsonDraft = "{}"
	a.jsonDirty = true
	a.dirty = true
	if err := a.apply(); err == nil {
		t.Fatal("unimported JSON would have been silently discarded")
	}
	original := a.draft.Title
	if err := a.applyJSON(); err == nil {
		t.Fatal("invalid pack accepted")
	}
	if a.draft.Title != original {
		t.Fatal("invalid JSON changed draft")
	}
}

func TestSaveUneditedPackPreservesBattle(t *testing.T) {
	a, err := newWorkbench(content.Demo())
	if err != nil {
		t.Fatal(err)
	}
	if err := a.service.Start("mountain"); err != nil {
		t.Fatal(err)
	}
	before := a.service.Current().Battle
	if err := a.service.Act(battle.Command{ActorID: before.Turn, Kind: battle.Attack, TargetID: before.Enemies[0].ID}); err != nil {
		t.Fatal(err)
	}
	before = a.service.Current().Battle
	a.packPath = filepath.Join(t.TempDir(), "pack.json")
	if err := a.savePack(); err != nil {
		t.Fatal(err)
	}
	after := a.service.Current().Battle
	if after == nil || !reflect.DeepEqual(before, after) {
		t.Fatal("saving unchanged pack reset the active battle")
	}
}

func TestNativeCharacterNumberBindingAndSwitch(t *testing.T) {
	a, err := newWorkbench(content.Demo())
	if err != nil {
		t.Fatal(err)
	}
	a.tab = 1
	tt := ui.NewTester(a.view, 1200, 1100)
	r, ok := tt.Find("生命上限数值")
	if !ok {
		t.Fatal("HP input absent")
	}
	tt.ClickAt(r.X+20, r.Y+r.H/2)
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type("275")
	if a.draft.Characters[0].MaxHP != 275 || !a.dirty {
		t.Fatal("number input did not write draft")
	}
	if err := tt.Click("新增人物"); err != nil {
		t.Fatal(err)
	}
	if a.characterID == a.draft.Characters[0].ID {
		t.Fatal("new character was not selected")
	}
	r, ok = tt.Find("生命上限数值")
	if !ok {
		t.Fatal("new character HP input absent")
	}
	tt.ClickAt(r.X+20, r.Y+r.H/2)
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type("320")
	if a.draft.Characters[2].MaxHP != 320 || a.draft.Characters[0].MaxHP != 275 {
		t.Fatal("selection reused old numeric binding")
	}
}

// Selection must update stable game IDs across event callbacks and frames, even
// when names are only presentation labels and the item count changes its label.
func TestNativeBattleNamedSelection(t *testing.T) {
	p := content.Demo()
	second := p.Encounters[0].Enemies[0]
	second.ID = "second-mist"
	second.Name = "第二守影"
	second.MaxHP = 250
	p.Encounters[0].Enemies = append(p.Encounters[0].Enemies, second)
	a, err := newWorkbench(p)
	if err != nil {
		t.Fatal(err)
	}
	a.tab = 3
	if err := a.service.Start("mountain"); err != nil {
		t.Fatal(err)
	}
	tt := ui.NewTester(a.view, 1200, 1200)
	choose := func(trigger, option string) {
		t.Helper()
		if err := tt.Click(trigger); err != nil {
			t.Fatal(err)
		}
		if err := tt.Click(option); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
	}
	choose("敌方目标选择", "第二守影")
	before := a.service.Current().Battle
	if a.enemyTarget != before.Enemies[1].ID {
		t.Fatalf("second enemy label selected stable ID %q, want %q", a.enemyTarget, before.Enemies[1].ID)
	}
	if err := tt.Click("攻击"); err != nil {
		t.Fatal(err)
	}
	after := a.service.Current().Battle
	if a.failed || after.Enemies[1].HP >= before.Enemies[1].HP || after.Enemies[0].HP != before.Enemies[0].HP {
		t.Fatal("attack did not damage selected second enemy", a.message)
	}
	choose("友方目标选择", "苏青")
	if a.allyTarget != after.Party[1].ID {
		t.Fatalf("second ally ID %q, want %q", a.allyTarget, after.Party[1].ID)
	}
	choose("战斗物品选择", "听泉露 ×3")
	if a.battleItem != "spring" {
		t.Fatalf("second item stable ID %q", a.battleItem)
	}
	// The first actor was Su Qing. Spend MP with Lin Yue, then use the selected
	// restoring item on Su Qing after she spends MP on her next turn.
	if err := tt.Click("技能 · 3 灵力"); err != nil {
		t.Fatal(err)
	}
	if a.failed {
		t.Fatal(a.message)
	}
	if err := tt.Click("技能 · 3 灵力"); err != nil {
		t.Fatal(err)
	}
	if a.failed {
		t.Fatal(a.message)
	}
	if err := tt.Click("使用物品"); err != nil {
		t.Fatal(err)
	}
	if a.failed {
		t.Fatal(a.message)
	}
	end := a.service.Current().Battle
	if end.Inventory["spring"] != 2 || a.battleItem != "spring" {
		t.Fatalf("count changed selection: count=%d selected=%q", end.Inventory["spring"], a.battleItem)
	}
	if _, ok := tt.Find("听泉露 ×2"); !ok {
		t.Fatal("selected item label did not reflect changed count")
	}
}
