package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/HiChen85/pal--reverse--learn/internal/content"
	"github.com/HiChen85/pal--reverse--learn/internal/palflow"
	"github.com/HiChen85/pal--reverse--learn/internal/palgame"
	"github.com/egoist/mygo/ui"
)

func originalWords(v ...uint16) []byte {
	b := make([]byte, len(v)*2)
	for i, n := range v {
		binary.LittleEndian.PutUint16(b[i*2:], n)
	}
	return b
}
func originalOffsets(v ...uint32) []byte {
	b := make([]byte, len(v)*4)
	for i, n := range v {
		binary.LittleEndian.PutUint32(b[i*4:], n)
	}
	return b
}
func originalMKF(chunks ...[]byte) []byte {
	head := make([]byte, 4*(len(chunks)+1))
	offset := len(head)
	for i, b := range chunks {
		binary.LittleEndian.PutUint32(head[i*4:], uint32(offset))
		offset += len(b)
	}
	binary.LittleEndian.PutUint32(head[len(chunks)*4:], uint32(offset))
	for _, b := range chunks {
		head = append(head, b...)
	}
	return head
}
func originalFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	messages := []byte("Synthetic dialogue one.Synthetic dialogue two.")
	events := append(originalWords(0, 1, 2, 0, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0), originalWords(0, 3, 4, 0, 3, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0)...)
	scenes := originalWords(1, 1, 0, 0, 2, 0, 0, 2)
	scripts := originalWords(0, 0, 0, 0, 0xffff, 0, 0, 0, 0xfffe, 0, 0, 0, 0x000a, 5, 0, 0, 0xffff, 1, 0, 0, 0, 0, 0, 0)
	files := map[string][]byte{"SSS.MKF": originalMKF(events, scenes, originalWords(0, 0, 0, 0, 0, 0), originalOffsets(0, 23, uint32(len(messages))), scripts), "DATA.MKF": originalMKF(make([][]byte, 15)...), "M.MSG": messages, "WORD.DAT": []byte("Test      ")}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}
func TestOriginalDataNativeReadTraceAndEvidence(t *testing.T) {
	a, err := newWorkbench(content.Demo())
	if err != nil {
		t.Fatal(err)
	}
	a.tab = 5
	a.original = newOriginalData()
	a.original.directory = originalFixture(t)
	tt := ui.NewTester(a.view, 1200, 1500)
	if err := tt.Click("读取原版数据"); err != nil {
		t.Fatal(err)
	}
	if a.failed {
		t.Fatal(a.message)
	}
	o := a.original
	if o.game == nil || o.game.Layout != "dos" || len(o.game.Scenes) != 2 {
		t.Fatal("explicit DOS import failed")
	}
	start, end, err := o.eventRange()
	if err != nil || start != 0 || end != 2 {
		t.Fatal("scene range is not the next EventStart bound", start, end, err)
	}
	if err := tt.Click("执行场景 OnEnter"); err != nil {
		t.Fatal(err)
	}
	if !a.failed || len(o.history) != 1 {
		t.Fatal("unsupported opcode was silently completed")
	}
	report := o.history[0].Report
	if report.Blocker == nil || report.Blocker.PC != 2 || report.Blocker.Opcode != 0xfffe {
		t.Fatal("missing exact blocker evidence")
	}
	if len(report.Trace) != 1 || report.Trace[0].Message == nil || !strings.Contains(report.Trace[0].Message.Text, "Synthetic dialogue one") {
		t.Fatal("direct decoded original-table message missing")
	}
	if _, ok := tt.Find("Synthetic dialogue one."); !ok {
		t.Fatal("decoded dialogue missing from native view")
	}
	// Select a real event trigger and explicitly supply confirmation. A missing
	// input must block; it must never select yes or an external win implicitly.
	o.event = "2"
	o.inputsJSON = "[]"
	if err := o.run("event_trigger"); err == nil {
		t.Fatal("missing confirmation accepted")
	}
	if len(o.history[len(o.history)-1].Report.Trace) != 0 {
		t.Fatal("blocked input committed an instruction")
	}
	o.inputsJSON = `[{"kind":"confirm","value":1}]`
	if err := o.run("event_trigger"); err != nil {
		t.Fatal(err)
	}
	done := o.history[len(o.history)-1]
	if !done.Report.Completed || done.Report.InputsConsumed != 1 || len(done.Report.DialogueIDs) != 1 {
		t.Fatal("explicit confirmation path failed")
	}
	o.exportPath = filepath.Join(t.TempDir(), "test.paltrace.json")
	if err := o.export(); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(o.exportPath)
	if err != nil {
		t.Fatal(err)
	}
	var evidence originalEvidence
	if err := json.Unmarshal(b, &evidence); err != nil {
		t.Fatal(err)
	}
	if len(evidence.Files) != 4 || evidence.Invocation.Request.Inputs[0].Kind != palflow.InputConfirm || !evidence.Invocation.Report.State.Finished {
		t.Fatal("export lost input/state/evidence")
	}
	o.scene = "2"
	if _, _, err := o.eventRange(); err == nil {
		t.Fatal("inferred last-scene range without successor")
	}
}
func TestOriginalReadAndInputErrorsPreserveEvidence(t *testing.T) {
	o := newOriginalData()
	o.directory = originalFixture(t)
	if err := o.load(); err != nil {
		t.Fatal(err)
	}
	old := o.game
	o.directory = filepath.Join(t.TempDir(), "missing")
	if err := o.load(); err == nil {
		t.Fatal("missing source accepted")
	}
	if o.game != old {
		t.Fatal("failed import destroyed prior evidence")
	}
	if err := o.run("on_enter"); err == nil {
		t.Fatal("changed source options allowed stale run")
	}
	for _, input := range []string{`[{"kind":"confirm","value":2}]`, `[{"kind":"random","value":-1}]`, `[{"kind":"battle","value":3}]`, `[{"kind":"skip","value":1}]`, `[] []`, `[{"kind":"confirm","value":null}]`, `[{"kind":"confirm"}]`, `[{"kind":"confirm","value":1,"value":0}]`, `[{"kind":"confirm","value":1,"secret":true}]`} {
		if _, err := decodeOriginalInputs(input); err == nil {
			t.Errorf("accepted %s", input)
		}
	}
	if _, err := (palgame.Source{}).Instruction(1); err == nil {
		t.Fatal("nil instruction source accepted")
	}
	if _, err := (palgame.Source{Game: old}).Message(65535); err == nil {
		t.Fatal("out of bounds message accepted")
	}
}

func TestOriginalExportCannotOverwriteInputOrAlias(t *testing.T) {
	o := newOriginalData()
	o.directory = originalFixture(t)
	if err := o.load(); err != nil {
		t.Fatal(err)
	}
	_ = o.run("on_enter")
	source := filepath.Join(o.directory, "SSS.MKF")
	before, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	paths := []string{source, filepath.Join(t.TempDir(), "report.txt")}
	alias := filepath.Join(t.TempDir(), "alias.paltrace.json")
	if err := os.Link(source, alias); err != nil {
		t.Fatal(err)
	}
	paths = append(paths, alias)
	symbolic := filepath.Join(t.TempDir(), "symbolic.json")
	if err := os.Symlink(source, symbolic); err != nil {
		t.Fatal(err)
	}
	paths = append(paths, symbolic)
	for _, path := range paths {
		o.exportPath = path
		if err := o.export(); err == nil {
			t.Fatalf("accepted protected export %s", path)
		}
		after, err := os.ReadFile(source)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(before, after) {
			t.Fatalf("source bytes changed after rejected export to %s", path)
		}
	}
}
func TestOriginalUnusedInputsDoNotCommitReplacementEntry(t *testing.T) {
	o := newOriginalData()
	o.directory = originalFixture(t)
	if err := o.load(); err != nil {
		t.Fatal(err)
	}
	// opcode1 stops and proposes replacing this entry with the next PC.
	o.game.Scripts[1].Opcode = 1
	o.inputsJSON = `[{"kind":"confirm","value":1}]`
	original := o.enterPC[0]
	err := o.run("on_enter")
	if err == nil || !strings.Contains(err.Error(), "未消费") {
		t.Fatal("unused input was not surfaced")
	}
	if o.enterPC[0] != original {
		t.Fatal("unused input trace committed replacement entry")
	}
	report := o.history[len(o.history)-1].Report
	if !report.Completed || len(report.UnusedInputs) != 1 || report.State.ReturnedEntry != 2 {
		t.Fatal("diagnostic report did not retain completed proposal and unused input")
	}
}

// Build a tiny original-format DATA archive with the real role/objects layout;
// all names and dialogue are synthetic and no external resource is needed.
func originalRuntimeFixture(t *testing.T) string {
	t.Helper()
	dir := originalFixture(t)
	chunks := make([][]byte, 15)
	chunks[0] = make([]byte, 18)
	chunks[1] = make([]byte, 70)
	chunks[2] = make([]byte, 10)
	chunks[3] = make([]byte, 75*6*2)
	chunks[4] = make([]byte, 32)
	chunks[5] = make([]byte, 12)
	chunks[6] = make([]byte, 20)
	chunks[11] = make([]byte, 40)
	chunks[13] = make([]byte, 100)
	chunks[14] = make([]byte, 200)
	if err := os.WriteFile(filepath.Join(dir, "DATA.MKF"), originalMKF(chunks...), 0600); err != nil {
		t.Fatal(err)
	}
	events := append(originalWords(0, 1, 2, 0, 1, 0, 1, 1, 0, 0, 0, 0, 0, 0, 0, 0), originalWords(0, 3, 4, 0, 5, 0, 1, 1, 0, 0, 0, 0, 0, 0, 0, 0)...)
	scenes := originalWords(1, 0, 0, 0, 2, 0, 0, 2)
	objects := originalWords(0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0)
	scripts := originalWords(0, 0, 0, 0, 0x1f, 1, 1, 0, 0xffff, 0, 0, 0, 0x49, 0xffff, 0, 0, 0, 0, 0, 0, 0x1f, 2, 1, 0, 0xffff, 1, 0, 0, 0x49, 0xffff, 0, 0, 0, 0, 0, 0)
	msg := []byte("Synthetic dialogue one.Synthetic dialogue two.")
	if err := os.WriteFile(filepath.Join(dir, "SSS.MKF"), originalMKF(events, scenes, objects, originalOffsets(0, 23, uint32(len(msg))), scripts), 0600); err != nil {
		t.Fatal(err)
	}
	return dir
}
func TestNativeOriginalLogicalSessionPersistsTwoActions(t *testing.T) {
	a, err := newWorkbench(content.Demo())
	if err != nil {
		t.Fatal(err)
	}
	a.tab = 5
	a.original = newOriginalData()
	a.original.directory = originalRuntimeFixture(t)
	tt := ui.NewTester(a.view, 1200, 2000)
	if err := tt.Click("读取原版数据"); err != nil {
		t.Fatal(err)
	}
	o := a.original
	if a.failed || o.runtime == nil || o.session == nil {
		t.Fatal("runtime-backed session was not constructed", a.message, o.runtimeError)
	}
	if err := tt.Click("逻辑会话"); err != nil {
		t.Fatal(err)
	}
	if !o.useSession {
		t.Fatal("session mode did not enable")
	}
	if err := tt.Click("执行事件触发脚本"); err != nil {
		t.Fatal(err)
	}
	if !a.failed || !o.session.Snapshot().NeedsSceneEnter {
		t.Fatal("pending OnEnter was bypassed")
	}
	if err := tt.Click("执行场景 OnEnter"); err != nil {
		t.Fatal(err)
	}
	if a.failed {
		t.Fatal(a.message)
	}
	if err := tt.Click("执行事件触发脚本"); err != nil {
		t.Fatal(err)
	}
	if a.failed {
		t.Fatal(a.message)
	}
	first := o.session.Snapshot()
	if first.World.Inventory[1] != 1 || first.World.Events[1].State != 0 {
		t.Fatal("first pickup state not committed")
	}
	if err := tt.Click("原版事件编号"); err != nil {
		t.Fatal(err)
	}
	tt.Key(0, ui.KeyDown)
	tt.Key(0, ui.KeyEnter)
	if o.event != "2" {
		t.Fatal("second event selection failed", o.event)
	}
	if err := tt.Click("执行事件触发脚本"); err != nil {
		t.Fatal(err)
	}
	if a.failed {
		t.Fatal(a.message)
	}
	after := o.session.Snapshot()
	if after.World.Inventory[1] != 1 || after.World.Inventory[2] != 1 || after.World.Events[2].State != 0 || after.Actions != 3 {
		t.Fatal("two completed actions did not retain inventory/events", after.Actions)
	}
	o.event = "1"
	if err := o.run("event_trigger"); err == nil {
		t.Fatal("hidden event repeated")
	}
	if !reflect.DeepEqual(after, o.session.Snapshot()) {
		t.Fatal("rejected hidden event changed session")
	}
}
func TestOriginalSessionCheckpointAndExcessInputsAreExplicit(t *testing.T) {
	o := newOriginalData()
	o.directory = originalRuntimeFixture(t)
	if err := o.load(); err != nil {
		t.Fatal(err)
	}
	o.assumeEntered = true
	if err := o.resetSession(); err != nil {
		t.Fatal(err)
	}
	o.useSession = true
	o.event = "1"
	before := o.session.Snapshot()
	o.inputsJSON = `[{"kind":"confirm","value":1}]`
	if err := o.run("event_trigger"); err == nil {
		t.Fatal("extraneous inputs committed logical action")
	}
	if !reflect.DeepEqual(before, o.session.Snapshot()) {
		t.Fatal("inexact replay changed active session")
	}
	record := o.history[len(o.history)-1]
	if record.Checkpoint == nil || !record.Checkpoint.AssumeSceneEntered || record.SessionCommitted || record.SessionBefore == nil || record.SessionAfter == nil || record.Request.World == nil || len(record.Report.UnusedInputs) != 1 {
		t.Fatal("checkpoint/report/request evidence incomplete")
	}
	o.exportPath = filepath.Join(t.TempDir(), "session.paltrace.json")
	if err := o.export(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(o.exportPath)
	if err != nil {
		t.Fatal(err)
	}
	var evidence originalEvidence
	if err := json.Unmarshal(data, &evidence); err != nil {
		t.Fatal(err)
	}
	if evidence.Invocation.Mode != "session" || evidence.Invocation.SessionBefore.World.Inventory[1] != 0 || evidence.Invocation.SessionAfter.World.Inventory[1] != 0 {
		t.Fatal("export lost before/after checkpoint distinction")
	}
	// The scene selector is only a reset target; changing it cannot teleport the
	// active logical session or alter its boundaries.
	o.scene = "2"
	start, end, err := o.eventRange()
	if err != nil || start != 0 || end != 2 || o.session.Snapshot().World.Scene != 1 {
		t.Fatal("scene selector silently moved session")
	}
	if err := o.resetSession(); err == nil {
		t.Fatal("last sentinel scene accepted as logical checkpoint")
	}
}
func TestOriginalDiagnosticAttachesWorldOnlyWithRuntime(t *testing.T) {
	for _, runtimeAvailable := range []bool{false, true} {
		o := newOriginalData()
		if runtimeAvailable {
			o.directory = originalRuntimeFixture(t)
		} else {
			o.directory = originalFixture(t)
		}
		if err := o.load(); err != nil {
			t.Fatal(err)
		}
		if o.useSession {
			t.Fatal("load silently opted into session mode")
		}
		o.event = "1"
		_ = o.run("event_trigger")
		request := o.history[len(o.history)-1].Request
		if (request.World != nil) != runtimeAvailable {
			t.Fatal("diagnostic world availability was invented or omitted")
		}
	}
}
