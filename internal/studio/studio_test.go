package studio

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/HiChen85/pal--reverse--learn/internal/battle"
	"github.com/HiChen85/pal--reverse--learn/internal/content"
)

func demo(t *testing.T) *Studio {
	t.Helper()
	s, err := New(content.Demo())
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func ok(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
func TestProgressRestoresMidBattleAndRejectsInvalidReplay(t *testing.T) {
	s := demo(t)
	ok(t, s.Start("well"))
	ok(t, s.Continue())
	b := s.Current().Battle
	ok(t, s.Act(battle.Command{ActorID: b.Turn, Kind: battle.Attack, TargetID: b.Enemies[0].ID}))
	file := filepath.Join(t.TempDir(), "progress.json")
	ok(t, s.SaveProgress(file))
	restored := demo(t)
	ok(t, restored.LoadProgress(file))
	if !reflect.DeepEqual(s.Current(), restored.Current()) {
		t.Fatal("restore changed midbattle state")
	}
	var data progress
	raw, err := os.ReadFile(file)
	ok(t, err)
	ok(t, json.Unmarshal(raw, &data))
	data.Steps = append(data.Steps, step{Kind: "choose", Choice: new(int)})
	raw, err = json.Marshal(data)
	ok(t, err)
	ok(t, os.WriteFile(file, raw, 0600))
	before := restored.Current()
	if restored.LoadProgress(file) == nil {
		t.Fatal("invalid replay accepted")
	}
	if !reflect.DeepEqual(before, restored.Current()) {
		t.Fatal("partial replay replaced active session")
	}
}
func TestPackEditingIsTransactionalAndInvalidatesProgress(t *testing.T) {
	s := demo(t)
	ok(t, s.Start("well"))
	before := s.Current()
	p := s.Pack()
	p.Nodes[0].Next = "missing"
	if s.ReplacePack(p) == nil {
		t.Fatal("bad pack accepted")
	}
	if !reflect.DeepEqual(before, s.Current()) {
		t.Fatal("invalid edit changed preview")
	}
	p = s.Pack()
	p.Nodes[0].Text = "edited"
	if s.Pack().Nodes[0].Text == "edited" {
		t.Fatal("pack aliases workspace")
	}
	file := filepath.Join(t.TempDir(), "progress.json")
	ok(t, s.SaveProgress(file))
	ok(t, s.ReplacePack(p))
	if s.Current().Started {
		t.Fatal("edited pack retained preview")
	}
	if s.LoadProgress(file) == nil {
		t.Fatal("edited pack accepted stale progress")
	}
}
func TestProgressStrictAndSaveRules(t *testing.T) {
	s := demo(t)
	dir := t.TempDir()
	file := filepath.Join(dir, "progress.json")
	sentinel := []byte("existing save")
	ok(t, os.WriteFile(file, sentinel, 0600))
	if s.SaveProgress(file) == nil {
		t.Fatal("unstarted save accepted")
	}
	preserved, err := os.ReadFile(file)
	ok(t, err)
	if string(preserved) != string(sentinel) {
		t.Fatal("failed save changed existing file")
	}
	ok(t, s.Start(""))
	ok(t, s.SaveProgress(file))
	temps, err := filepath.Glob(filepath.Join(dir, ".progress-*.tmp"))
	ok(t, err)
	if len(temps) != 0 {
		t.Fatalf("temporary files were not cleaned up after save: %v", temps)
	}
	raw, err := os.ReadFile(file)
	ok(t, err)
	var p map[string]any
	ok(t, json.Unmarshal(raw, &p))
	p["unexpected"] = true
	bad, err := json.Marshal(p)
	ok(t, err)
	ok(t, os.WriteFile(file, bad, 0600))
	if s.LoadProgress(file) == nil {
		t.Fatal("unknown field accepted")
	}
	ok(t, os.WriteFile(file, append(raw, []byte(" {}")...), 0600))
	if s.LoadProgress(file) == nil {
		t.Fatal("trailing document accepted")
	}
}
func TestSaveOpenAndExtensionReferencesBase(t *testing.T) {
	s := demo(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "pack.json")
	ok(t, s.SavePack(path))
	p := s.Pack()
	p.Title = "changed"
	ok(t, s.ReplacePack(p))
	ok(t, s.LoadPack(path))
	if s.Pack().Title == "changed" {
		t.Fatal("open did not restore saved pack")
	}
	extension := content.Pack{SchemaVersion: 1, ID: "chapter2", Title: "second", Start: "new-start", Nodes: []content.Node{{ID: "new-start", Speaker: "lin-yue", Text: "new chapter", Next: "ending"}}}
	path = filepath.Join(dir, "extension.json")
	ok(t, content.SaveExtension(path, extension, s.Pack()))
	ok(t, s.MergePack(path))
	ok(t, s.Start("new-start"))
	if s.Current().Node.Speaker != "lin-yue" {
		t.Fatal("extension base ref lost")
	}
	before := s.Pack()
	if s.MergePack(path) == nil {
		t.Fatal("duplicate extension accepted")
	}
	if !reflect.DeepEqual(before, s.Pack()) {
		t.Fatal("failed merge changed workspace")
	}
}
