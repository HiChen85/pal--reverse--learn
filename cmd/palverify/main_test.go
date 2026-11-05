package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/HiChen85/pal--reverse--learn/internal/paldata"
	"github.com/HiChen85/pal--reverse--learn/internal/palflow"
)

func mkf(chunks ...[]byte) []byte {
	out := make([]byte, (len(chunks)+1)*4)
	pos := len(out)
	for i, c := range chunks {
		binary.LittleEndian.PutUint32(out[i*4:], uint32(pos))
		out = append(out, c...)
		pos += len(c)
	}
	binary.LittleEndian.PutUint32(out[len(chunks)*4:], uint32(pos))
	return out
}
func fixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	code := [][4]uint16{{0, 0, 0, 0}, {0xffff, 0, 0, 0}, {0x000a, 5, 0, 0}, {0xffff, 1, 0, 0}, {0, 0, 0, 0}, {0xffff, 2, 0, 0}, {0, 0, 0, 0}}
	scripts := make([]byte, len(code)*8)
	for i, r := range code {
		for j, v := range r {
			binary.LittleEndian.PutUint16(scripts[i*8+j*2:], v)
		}
	}
	events := make([]byte, 32)
	binary.LittleEndian.PutUint16(events[8:], 1)
	scenes := make([]byte, 8)
	binary.LittleEndian.PutUint16(scenes[2:], 1)
	offsets := make([]byte, 16)
	for i, v := range []uint32{0, 5, 8, 10} {
		binary.LittleEndian.PutUint32(offsets[i*4:], v)
	}
	files := map[string][]byte{"SSS.MKF": mkf(events, scenes, make([]byte, 14), offsets, scripts), "DATA.MKF": mkf(make([][]byte, 15)...), "M.MSG": []byte("startyesno"), "WORD.DAT": []byte("Hero      ")}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}
func routeFor(t *testing.T, dir string, entry uint16, inputs []palflow.Input, expected []uint16) string {
	t.Helper()
	g, err := paldata.Load(dir, paldata.Options{Encoding: "gbk"})
	if err != nil {
		t.Fatal(err)
	}
	r := route{Version: 1, Name: "synthetic-confirm", FilesSHA256: map[string]string{}, Request: palflow.Request{Entry: entry, EventID: 1, Inputs: inputs}, ExpectedMessages: &expected}
	for _, f := range g.Files {
		r.FilesSHA256[strings.ToUpper(f.Name)] = f.SHA256
	}
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "route.json")
	if err = os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	return path
}
func TestInspectAndAuditAreNotFullGameVerification(t *testing.T) {
	dir := fixture(t)
	for _, mode := range []string{"inspect", "audit"} {
		var out, log bytes.Buffer
		if err := run([]string{mode, "-data", dir, "-encoding", "gbk"}, &out, &log); err != nil {
			t.Fatal(err, log.String())
		}
		var e envelope
		if err := json.Unmarshal(out.Bytes(), &e); err != nil {
			t.Fatal(err)
		}
		if e.FullGameVerified || e.Dataset.ReleaseIdentified || e.Dataset.Scripts != 7 || e.Dataset.Messages != 3 {
			t.Fatalf("invalid coverage claim: %+v", e)
		}
		if mode == "audit" && (e.Audit == nil || e.Inventory == nil) {
			t.Fatal("audit not generated")
		}
	}
}
func TestTraceChoiceAndTranscriptRedaction(t *testing.T) {
	dir := fixture(t)
	path := routeFor(t, dir, 1, []palflow.Input{{Kind: palflow.InputConfirm, Value: 0}}, []uint16{0, 2})
	for _, text := range []bool{false, true} {
		var out, log bytes.Buffer
		args := []string{"trace", "-data", dir, "-encoding", "gbk", "-route", path}
		if text {
			args = append(args, "-include-text")
		}
		if err := run(args, &out, &log); err != nil {
			t.Fatal(err, log.String(), out.String())
		}
		var e envelope
		if err := json.Unmarshal(out.Bytes(), &e); err != nil {
			t.Fatal(err)
		}
		if e.Trace == nil || !e.Trace.Completed || e.ExpectedMessagesMatched == nil || !*e.ExpectedMessagesMatched {
			t.Fatalf("trace missing: %+v", e)
		}
		if text && e.Trace.Trace[0].Message.Text != "start" {
			t.Fatal("included text changed")
		}
		if !text && (!strings.HasPrefix(e.Trace.Trace[0].Message.Text, "[message:0 sha256:") || strings.Contains(out.String(), `"text": "start"`)) {
			t.Fatal("dialogue leaked by default")
		}
		if e.FullGameVerified {
			t.Fatal("script stop reported as game verification")
		}
	}
}
func TestTraceRejectsMissingInputAndStaleFileHash(t *testing.T) {
	dir := fixture(t)
	path := routeFor(t, dir, 1, nil, []uint16{0})
	report := filepath.Join(t.TempDir(), "blocked.json")
	var out, log bytes.Buffer
	if err := run([]string{"trace", "-data", dir, "-encoding", "gbk", "-route", path, "-out", report}, &out, &log); err == nil {
		t.Fatal("missing decision accepted")
	}
	raw, err := os.ReadFile(report)
	if err != nil {
		t.Fatal(err)
	}
	var e envelope
	if err = json.Unmarshal(raw, &e); err != nil {
		t.Fatal(err)
	}
	if e.Trace == nil || e.Trace.Blocker == nil || e.Trace.Completed {
		t.Fatal("missing decision not recorded as blocker")
	}
	if err = os.WriteFile(filepath.Join(dir, "WORD.DAT"), []byte("Other     "), 0600); err != nil {
		t.Fatal(err)
	}
	if err = run([]string{"trace", "-data", dir, "-encoding", "gbk", "-route", path}, &out, &log); err == nil || !strings.Contains(err.Error(), "摘要") {
		t.Fatalf("stale hash accepted: %v", err)
	}
}
func TestTraceRejectsWrongExpectedMessagesAndUnusedInputs(t *testing.T) {
	dir := fixture(t)
	cases := []struct {
		inputs []palflow.Input
		want   []uint16
	}{{[]palflow.Input{{Kind: "confirm", Value: 1}}, []uint16{0, 2}}, {[]palflow.Input{{Kind: "confirm", Value: 1}, {Kind: "confirm", Value: 1}}, []uint16{0, 1}}}
	for _, c := range cases {
		path := routeFor(t, dir, 1, c.inputs, c.want)
		var out, log bytes.Buffer
		if err := run([]string{"trace", "-data", dir, "-encoding", "gbk", "-route", path}, &out, &log); err == nil {
			t.Fatal("mismatched replay accepted")
		}
	}
}
func TestReportCannotOverwriteResourcesOrRoute(t *testing.T) {
	dir := fixture(t)
	path := routeFor(t, dir, 1, nil, []uint16{0})
	var out, log bytes.Buffer
	for _, dest := range []string{filepath.Join(dir, "SSS.MKF"), path} {
		if err := run([]string{"trace", "-data", dir, "-encoding", "gbk", "-route", path, "-out", dest}, &out, &log); err == nil {
			t.Fatal("accepted unsafe report destination")
		}
	}
}
