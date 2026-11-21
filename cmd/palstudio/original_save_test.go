package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/HiChen85/pal--reverse--learn/internal/content"
	"github.com/HiChen85/pal--reverse--learn/internal/palgame"
	"github.com/egoist/mygo/ui"
)

func TestOriginalNativeSaveAndExplicitRestoreClicks(t *testing.T) {
	a, err := newWorkbench(content.Demo())
	if err != nil {
		t.Fatal(err)
	}
	a.tab = 5
	a.original = newOriginalData()
	o := a.original
	o.directory = originalMapRuntimeFixture(t)
	o.savePath = filepath.Join(t.TempDir(), "session.json")
	tt := ui.NewTester(a.view, 1400, 5000)
	for _, label := range []string{"读取原版数据", "逻辑会话", "执行场景 OnEnter", "向东 ↘", "保存会话 JSON"} {
		if err := tt.Click(label); err != nil || a.failed {
			t.Fatal("native action failed", label, err, a.message)
		}
	}
	saved := o.session.Snapshot()
	active := o.session
	viewport := o.viewport
	data, err := os.ReadFile(o.savePath)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) == 0 || bytes.Contains(data, []byte(": null")) {
		t.Fatal("UI did not create strict local JSON")
	}
	if o.history[len(o.history)-1].ActionIntent.Kind != "save_session" || o.history[len(o.history)-1].SessionCommitted || o.session != active || o.viewport != viewport {
		t.Fatal("save replaced session or omitted save intent")
	}
	if err := tt.Click("向南 ↙"); err != nil || a.failed {
		t.Fatal(err, a.message)
	}
	if reflect.DeepEqual(saved, o.session.Snapshot()) {
		t.Fatal("native move failed to change session")
	}
	if err := tt.Click("读取会话 JSON"); err != nil || a.failed {
		t.Fatal(err, a.message)
	}
	if o.session == active || !reflect.DeepEqual(saved, o.session.Snapshot()) || o.viewport == nil || o.viewport == viewport || !o.useSession {
		t.Fatal("explicit load did not restore saved state and refresh viewport", o.viewportError)
	}
	load := o.history[len(o.history)-1]
	if load.Invoked || load.Request != nil || load.Options != nil || !load.SessionCommitted || load.ActionIntent.Path != o.savePath {
		t.Fatal("load intent was lost or turned into a script invocation")
	}
	before := o.session
	state := before.Snapshot()
	if err := os.WriteFile(o.savePath, []byte(`{"version":1,"version":1}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("读取会话 JSON"); err != nil {
		t.Fatal(err)
	}
	if !a.failed || o.session != before || !reflect.DeepEqual(state, o.session.Snapshot()) {
		t.Fatal("rejected UI load changed active session")
	}
}

func TestOriginalSessionPathRejectsResourceReportAndLinkAliases(t *testing.T) {
	o := newOriginalData()
	o.directory = originalMapRuntimeFixture(t)
	if err := o.load(); err != nil {
		t.Fatal(err)
	}
	o.exportPath = filepath.Join(t.TempDir(), "report.json")
	if err := os.WriteFile(o.exportPath, []byte("report-input"), 0600); err != nil {
		t.Fatal(err)
	}
	protected := []string{o.exportPath}
	for _, file := range o.game.Files {
		protected = append(protected, filepath.Join(o.loadedDirectory, file.Name))
	}
	for _, file := range o.mapIdentities {
		protected = append(protected, filepath.Join(o.loadedDirectory, file.Name))
	}
	protected = append(protected, filepath.Join(o.loadedDirectory, o.mgoIdentity.Name), filepath.Join(o.loadedDirectory, o.paletteIdentity.Name))
	before := o.session.Snapshot()
	for _, source := range protected {
		original, err := os.ReadFile(source)
		if err != nil {
			t.Fatal(err)
		}
		for _, symbolic := range []bool{false, true} {
			alias := filepath.Join(t.TempDir(), "alias.json")
			link := os.Link
			if symbolic {
				link = os.Symlink
			}
			if err := link(source, alias); err != nil {
				t.Fatal(err)
			}
			o.savePath = alias
			if err := o.runSessionFile("save_session"); err == nil {
				t.Fatal("protected resource/report alias accepted", source)
			}
			if err := o.runSessionFile("load_session"); err == nil {
				t.Fatal("protected resource/report input alias accepted", source)
			}
		}
		after, err := os.ReadFile(source)
		if err != nil || !bytes.Equal(original, after) {
			t.Fatal("protected input was changed", source, err)
		}
	}
	o.savePath = o.exportPath
	if err := o.runSessionFile("save_session"); err == nil {
		t.Fatal("report path accepted as save output")
	}
	parent := t.TempDir()
	aliasDir := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(parent, aliasDir); err != nil {
		t.Fatal(err)
	}
	o.savePath = filepath.Join(aliasDir, "save.json")
	if err := o.runSessionFile("save_session"); err == nil {
		t.Fatal("symlink directory alias accepted")
	}
	if !reflect.DeepEqual(before, o.session.Snapshot()) {
		t.Fatal("rejected session file actions changed active world")
	}
}

func TestOriginalSessionAtomicWritePreservesOldFileOnFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "save.json")
	before := []byte("old-complete-save")
	if err := os.WriteFile(path, before, 0600); err != nil {
		t.Fatal(err)
	}
	rejected := errors.New("path changed before commit")
	if err := writeOriginalSessionAtomic(path, []byte("new-complete-save"), func() error { return rejected }); !errors.Is(err, rejected) {
		t.Fatal("atomic recheck error lost", err)
	}
	got, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(got, before) {
		t.Fatal("failed write replaced previous save", err)
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil || len(entries) != 1 {
		t.Fatal("temporary save leaked", err)
	}
	if err := writeOriginalSessionAtomic(path, []byte("new-complete-save"), func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	got, err = os.ReadFile(path)
	if err != nil || string(got) != "new-complete-save" {
		t.Fatal("atomic replacement did not complete", err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("session file permissions are not private", err)
	}
}

func TestOriginalLoadRetainsInitialDiagnosticDeclaration(t *testing.T) {
	o := newOriginalData()
	o.directory = originalMapRuntimeFixture(t)
	if err := o.load(); err != nil {
		t.Fatal(err)
	}
	o.assumeEntered = true
	if err := o.resetSession(); err != nil {
		t.Fatal(err)
	}
	o.savePath = filepath.Join(t.TempDir(), "declared-checkpoint.json")
	if err := o.runSessionFile("save_session"); err != nil {
		t.Fatal(err)
	}
	o.assumeEntered = false
	if err := o.resetSession(); err != nil {
		t.Fatal(err)
	}
	if err := o.runSessionFile("load_session"); err != nil {
		t.Fatal(err)
	}
	if !o.checkpoint.AssumeSceneEntered || !o.assumeEntered || !o.session.InitialCheckpoint().AssumeSceneEntered {
		t.Fatal("restore erased the caller's diagnostic assumption")
	}
}

func TestOriginalSaveRestoresWithBoundAnimationCatalogs(t *testing.T) {
	o := newOriginalData()
	o.directory = originalMapRuntimeFixture(t)
	for _, name := range []string{"rng.mkf", "fbp.mkf"} {
		if err := os.WriteFile(filepath.Join(o.directory, name), originalMKF([]byte{}), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := o.load(); err != nil || o.rng == nil || o.fbp == nil {
		t.Fatal("optional animation imports failed", err, o.mediaError)
	}
	o.savePath = filepath.Join(t.TempDir(), "media-session.json")
	if err := o.runSessionFile("save_session"); err != nil {
		t.Fatal(err)
	}
	before := o.session.Snapshot()
	if err := o.runSessionFile("load_session"); err != nil || !reflect.DeepEqual(before, o.session.Snapshot()) {
		t.Fatal("UI restore omitted optional resource providers", err)
	}
	data, err := os.ReadFile(o.savePath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := palgame.RestoreSession(o.game, o.runtime, palgame.SessionOptions{Assets: o.assets, Maps: o.maps, Palettes: o.palettes}, data); err == nil {
		t.Fatal("media-bound save accepted missing animation catalogs")
	}
	// A newly discovered malformed source still receives output protection.
	bad := filepath.Join(o.loadedDirectory, "RNG.MKF")
	if err := os.WriteFile(bad, []byte("invalid optional archive"), 0600); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(t.TempDir(), "bad-resource.json")
	if err := os.Link(bad, alias); err != nil {
		t.Fatal(err)
	}
	o.savePath = alias
	if err := o.runSessionFile("save_session"); err == nil {
		t.Fatal("malformed source alias accepted as save destination")
	}
}
