package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/HiChen85/pal--reverse--learn/internal/palasset"
	"github.com/HiChen85/pal--reverse--learn/internal/paldata"
	"github.com/HiChen85/pal--reverse--learn/internal/palflow"
)

func journeyRawYJ1(plain []byte) []byte {
	out := make([]byte, 16)
	copy(out, "YJ_1")
	binary.LittleEndian.PutUint32(out[4:], uint32(len(plain)))
	binary.LittleEndian.PutUint16(out[12:], uint16((len(plain)+0x3fff)/0x4000))
	for len(plain) > 0 {
		n := min(len(plain), 0x4000)
		header := [4]byte{}
		binary.LittleEndian.PutUint16(header[:], uint16(n))
		out = append(out, header[:]...)
		out = append(out, plain[:n]...)
		plain = plain[n:]
	}
	binary.LittleEndian.PutUint32(out[8:], uint32(len(out)))
	return out
}

func journeySprite(frames int) []byte {
	words := frames + 1
	out := make([]byte, words*2+frames*6)
	for i := 0; i < words; i++ {
		binary.LittleEndian.PutUint16(out[i*2:], uint16(words+i*3))
	}
	for i := 0; i < frames; i++ {
		copy(out[words*2+i*6:], []byte{1, 0, 1, 0, 1, 7})
	}
	return out
}

// The fixture contains generated tables, uncompressed YJ1 blocks and tiny RLE
// sprites. It exercises the real import/catalog/session path without game data.
func journeyFixture(t *testing.T) (string, journeyRoute) {
	return journeyFixtureWithAuto(t, false)
}

func journeyFixtureWithAuto(t *testing.T, auto bool) (string, journeyRoute) {
	t.Helper()
	dir := t.TempDir()
	code := [][4]uint16{
		{}, {0x46, 5, 7, 0}, {},
		{0x1f, 1, 1, 0}, {0xffff, 0, 0, 0}, {0x49, 0xffff, 0, 0}, {},
		{0x59, 2, 0, 0}, {},
		{0x46, 10, 10, 0}, {},
		{0x1f, 2, 1, 0}, {0x7fff, 0, 0, 0}, {},
		{4, 16, 0, 0}, {}, {0xffff, 1, 0, 0}, {},
	}
	scripts := make([]byte, len(code)*8)
	for i, row := range code {
		for j, value := range row {
			binary.LittleEndian.PutUint16(scripts[i*8+j*2:], value)
		}
	}
	events := make([]byte, 3*32)
	for i, values := range [][16]uint16{
		{1: 144, 2: 120, 4: 3, 6: 1, 7: 1, 8: 1, 9: 3},
		{1: 400, 2: 400, 4: 7, 6: 1, 7: 1},
		{1: 480, 2: 480, 4: 11, 6: 1, 7: 1},
	} {
		if auto && i == 1 {
			values[5] = 14
		}
		for j, value := range values {
			binary.LittleEndian.PutUint16(events[i*32+j*2:], value)
		}
	}
	scenes := make([]byte, 3*8)
	for i, row := range [][4]uint16{{1, 1, 0, 0}, {2, 9, 0, 2}, {1, 0, 0, 3}} {
		for j, value := range row {
			binary.LittleEndian.PutUint16(scenes[i*8+j*2:], value)
		}
	}
	message := []byte("private synthetic dialogue")
	offsets := make([]byte, 12)
	binary.LittleEndian.PutUint32(offsets[4:], uint32(len(message)))
	message = append(message, []byte("private automatic dialogue")...)
	binary.LittleEndian.PutUint32(offsets[8:], uint32(len(message)))
	chunks := make([][]byte, 15)
	chunks[0] = make([]byte, paldata.StoreSize*2)
	chunks[1] = make([]byte, paldata.EnemyWords*2)
	chunks[2] = make([]byte, paldata.EnemyTeamSize*2)
	chunks[3] = make([]byte, paldata.RoleWords*paldata.RoleCount*2)
	chunks[4] = make([]byte, paldata.MagicWords*2)
	chunks[5] = make([]byte, paldata.BattlefieldWords*2)
	chunks[6] = make([]byte, paldata.PlayableRoleCount*4)
	chunks[11] = make([]byte, 40)
	chunks[13] = make([]byte, 100)
	chunks[14] = make([]byte, 200)
	mapData := journeyRawYJ1(make([]byte, palasset.MapDecodedBytes))
	files := map[string][]byte{
		"SSS.MKF":  mkf(events, scenes, make([]byte, 3*14), offsets, scripts),
		"DATA.MKF": mkf(chunks...), "M.MSG": message, "WORD.DAT": []byte("Hero      "),
		"MGO.MKF": mkf(journeyRawYJ1(journeySprite(12)), journeyRawYJ1(journeySprite(12))),
		"MAP.MKF": mkf(nil, mapData, mapData), "GOP.MKF": mkf(nil, journeySprite(1), journeySprite(1)),
	}
	spec := journeyRoute{Version: 1, Name: "synthetic-continuous", Encoding: "gbk", Layout: "windows", FilesSHA256: map[string]string{}, Actions: []journeyAction{{Type: "enter", Inputs: []palflow.Input{}}}}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(data)
		spec.FilesSHA256[name] = fmt.Sprintf("%x", sum)
	}
	return dir, spec
}

func journeyRouteFile(t *testing.T, spec journeyRoute) string {
	t.Helper()
	data, err := json.Marshal(spec)
	if err != nil {
		t.Fatal(err)
	}
	return journeyRouteBytes(t, data)
}

func journeyRouteBytes(t *testing.T, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "journey.json")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func journeyUint(value uint16) *uint16 { return &value }

func TestJourneyContinuousSpatialPickupMoveSceneEnterAndRollback(t *testing.T) {
	dir, spec := journeyFixture(t)
	messages := []uint16{0}
	spec.Actions = append(spec.Actions,
		journeyAction{Type: "interact", Inputs: []palflow.Input{}, ExpectedMessages: &messages, ExpectedInventory: map[uint16]int{1: 1}},
		journeyAction{Type: "move", Direction: journeyUint(3), Inputs: []palflow.Input{}},
		journeyAction{Type: "trigger", Event: journeyUint(2), Inputs: []palflow.Input{}, ExpectedScene: journeyUint(2)},
		journeyAction{Type: "enter", Inputs: []palflow.Input{}, ExpectedScene: journeyUint(2), ExpectedInventory: map[uint16]int{1: 1}},
		journeyAction{Type: "trigger", Event: journeyUint(3), Inputs: []palflow.Input{}},
		journeyAction{Type: "move", Direction: journeyUint(3), Inputs: []palflow.Input{}},
	)
	var out, log bytes.Buffer
	err := runJourney([]string{"-data", dir, "-route", journeyRouteFile(t, spec)}, &out, &log)
	if err == nil {
		t.Fatal("unsupported instruction did not stop journey")
	}
	var report journeyReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatal(err, log.String())
	}
	if report.Completed || report.FullGameVerified || report.Dataset.ReleaseIdentified || len(report.Actions) != 6 || report.Final.Actions != 5 {
		t.Fatalf("incorrect coverage or stop: completed=%v actions=%d committed=%d error=%s", report.Completed, len(report.Actions), report.Final.Actions, report.Error)
	}
	if report.Final.World.Scene != 2 || report.Final.NeedsSceneEnter || report.Final.World.SceneResources.MapID != 2 || report.Final.World.Inventory[1] != 1 || report.Final.World.Inventory[2] != 0 {
		t.Fatal("continuous state or failure rollback lost")
	}
	if report.Actions[1].Control.SearchEventID != 1 || !report.Actions[2].Control.Moved || report.Actions[2].Control.After.X != 176 {
		t.Fatal("spatial controls were not executed")
	}
	failed := report.Actions[5]
	if failed.Committed || failed.Trace.Blocker == nil || failed.Trace.Blocker.Opcode != 0x7fff || failed.BeforeSHA256 != failed.AfterSHA256 || len(failed.Delta) != 0 {
		t.Fatal("failed action committed a prefix or lost failure evidence")
	}
	if report.Actions[0].Invocation == nil || report.Actions[0].Invocation.Request.Entry != 1 || report.Actions[0].Invocation.WorldSHA256 == "" {
		t.Fatal("actual prepared invocation unavailable")
	}
	if report.Actions[4].Invocation.Request.Entry != 9 || report.Actions[4].BeforeSHA256 != report.Actions[3].AfterSHA256 {
		t.Fatal("scene entry reset the session")
	}
	for i, action := range report.Actions {
		if action.Trace != nil && action.Trace.State.World != nil {
			t.Fatalf("duplicate trace world in %d", i)
		}
		if action.Control != nil && action.Control.State.World != nil {
			t.Fatalf("duplicate control world in %d", i)
		}
		if action.Invocation != nil && action.Invocation.Request.World != nil {
			t.Fatalf("duplicate invocation world in %d", i)
		}
	}
	if bytes.Contains(out.Bytes(), []byte("private synthetic dialogue")) || bytes.Count(out.Bytes(), []byte(`"equipment_effects":`)) != 1 {
		t.Fatal("original dialogue or repeated full worlds leaked")
	}
}

func TestJourneyCompactPreservesHashesMessagesWorkAndFailureEvidence(t *testing.T) {
	dir, spec := journeyFixture(t)
	spec.Actions = append(spec.Actions, journeyAction{Type: "interact", Inputs: []palflow.Input{}}, journeyAction{Type: "trigger", Event: journeyUint(2), Inputs: []palflow.Input{}}, journeyAction{Type: "enter", Inputs: []palflow.Input{}}, journeyAction{Type: "trigger", Event: journeyUint(3), Inputs: []palflow.Input{}})
	path := journeyRouteFile(t, spec)
	var full, compact journeyReport
	lengths := []int{}
	for _, mode := range []string{"full", "compact"} {
		args := []string{"-data", dir, "-route", path}
		if mode == "compact" {
			args = append(args, "-compact")
		}
		var out bytes.Buffer
		if err := runJourney(args, &out, &bytes.Buffer{}); err == nil {
			t.Fatal("late unsupported opcode was skipped")
		}
		lengths = append(lengths, out.Len())
		result := &full
		if mode == "compact" {
			result = &compact
		}
		if err := json.Unmarshal(out.Bytes(), result); err != nil {
			t.Fatal(err)
		}
	}
	if compact.TraceDetail != "compact" || full.TraceDetail != "full" || journeyDigest(compact.Final) != journeyDigest(full.Final) || lengths[1] >= lengths[0] || len(compact.Actions) != len(full.Actions) {
		t.Fatal("compact mode changed actual execution or failed to compact")
	}
	for i, c := range compact.Actions {
		f := full.Actions[i]
		if c.BeforeSHA256 != f.BeforeSHA256 || c.AfterSHA256 != f.AfterSHA256 || c.Work != f.Work || journeyDigest(c.MessageIDs) != journeyDigest(f.MessageIDs) {
			t.Fatal("compact action lost execution evidence", i)
		}
		if i == len(compact.Actions)-1 {
			if c.Trace == nil || c.Trace.Blocker == nil || c.Trace.Blocker.Opcode != 0x7fff || c.Invocation == nil || c.Committed {
				t.Fatal("compact mode omitted failed trace")
			}
		} else if c.Trace != nil || c.Control != nil || c.Invocation != nil || c.Delta != nil {
			t.Fatal("successful compact action retained full detail")
		}
	}
	if journeyDigest(compact.Actions[1].MessageIDs) != journeyDigest([]uint16{0}) {
		t.Fatal("actual dialogue references omitted")
	}
}

func TestJourneyExpectationFailureReportsActualCommit(t *testing.T) {
	dir, spec := journeyFixture(t)
	spec.Actions[0].ExpectedScene = journeyUint(2)
	spec.Actions = append(spec.Actions, journeyAction{Type: "interact", Inputs: []palflow.Input{}})
	var out bytes.Buffer
	if err := runJourney([]string{"-data", dir, "-route", journeyRouteFile(t, spec)}, &out, &bytes.Buffer{}); err == nil {
		t.Fatal("incorrect expectation accepted")
	}
	var report journeyReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if len(report.Actions) != 1 || !report.Actions[0].Committed || report.Actions[0].ExpectationsMatched || report.Final.Actions != 1 || report.Final.NeedsSceneEnter {
		t.Fatal("QA failure misrepresented actual committed action")
	}
}

func TestJourneyEventStateAssertionsPreserveActualCommit(t *testing.T) {
	for _, want := range []int16{0, 1} {
		dir, spec := journeyFixture(t)
		spec.Actions = append(spec.Actions, journeyAction{Type: "interact", Inputs: []palflow.Input{}, ExpectedEventStates: map[uint16]int16{1: want}})
		var out bytes.Buffer
		err := runJourney([]string{"-data", dir, "-compact", "-route", journeyRouteFile(t, spec)}, &out, &bytes.Buffer{})
		var r journeyReport
		if e := json.Unmarshal(out.Bytes(), &r); e != nil {
			t.Fatal(e)
		}
		if (err == nil) != (want == 0) || !r.Actions[1].Committed || r.Final.World.Events[1].State != 0 {
			t.Fatal("event-state assertion rewrote or misreported committed pickup", err)
		}
		if want == 1 && r.Actions[1].Control == nil {
			t.Fatal("failed event assertion lost full diagnostic")
		}
	}
	_, spec := journeyFixture(t)
	spec.Actions[0].ExpectedEventStates = map[uint16]int16{1: 0}
	data, _ := json.Marshal(spec)
	data = bytes.Replace(data, []byte(`"expected_event_states":{"1":0}`), []byte(`"expected_event_states":{"01":0}`), 1)
	if _, _, err := loadJourneyRoute(journeyRouteBytes(t, data)); err == nil {
		t.Fatal("noncanonical event ID accepted")
	}
}

func TestJourneyControlMessagesIncludeAutoBeforeManualSearch(t *testing.T) {
	dir, spec := journeyFixtureWithAuto(t, true)
	messages := []uint16{1, 0}
	spec.Actions = append(spec.Actions, journeyAction{Type: "interact", Inputs: []palflow.Input{}, ExpectedMessages: &messages, ExpectedInventory: map[uint16]int{1: 1}})
	var out bytes.Buffer
	if err := runJourney([]string{"-data", dir, "-route", journeyRouteFile(t, spec)}, &out, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	var report journeyReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	control := report.Actions[1].Control
	if !report.Completed || control.SearchEventID != 1 || len(control.AutoSteps) != 1 || len(control.AutoSteps[0].TriggerSteps) != 2 || !report.Actions[1].ExpectationsMatched {
		t.Fatal("auto/search messages lost or reordered")
	}
}

func TestJourneyStrictRouteFormat(t *testing.T) {
	_, spec := journeyFixture(t)
	base, _ := json.Marshal(spec)
	cases := map[string][]byte{
		"duplicate":        bytes.Replace(base, []byte(`"version":1`), []byte(`"version":1,"version":1`), 1),
		"case alias":       bytes.Replace(base, []byte(`"version":1`), []byte(`"Version":1`), 1),
		"null":             bytes.Replace(base, []byte(`"inputs":[]`), []byte(`"inputs":null`), 1),
		"null expected":    bytes.Replace(base, []byte(`"inputs":[]`), []byte(`"inputs":[],"expected_messages":null`), 1),
		"unknown":          bytes.Replace(base, []byte(`"type":"enter"`), []byte(`"type":"enter","scene":2`), 1),
		"duplicate nested": bytes.Replace(base, []byte(`"inputs":[]`), []byte(`"inputs":[{"kind":"confirm","value":1,"value":0}]`), 1),
		"missing inputs":   bytes.Replace(base, []byte(`,"inputs":[]`), nil, 1),
		"bad utf8":         append(base, 0xff),
		"trailing":         append(append([]byte{}, base...), []byte(" {}")...),
		"too large":        bytes.Repeat([]byte(" "), journeyRouteLimit+1),
	}
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			if _, _, err := loadJourneyRoute(journeyRouteBytes(t, data)); err == nil {
				t.Fatal("invalid route accepted")
			}
		})
	}
	for name, change := range map[string]func(*journeyRoute){
		"empty":        func(r *journeyRoute) { r.Actions = []journeyAction{} },
		"many":         func(r *journeyRoute) { r.Actions = make([]journeyAction, journeyMaxActions+1) },
		"missing hash": func(r *journeyRoute) { delete(r.FilesSHA256, "GOP.MKF") },
		"bypass enter": func(r *journeyRoute) { r.Actions[0].Type = "interact" },
		"direction": func(r *journeyRoute) {
			r.Actions = append(r.Actions, journeyAction{Type: "move", Direction: journeyUint(4), Inputs: []palflow.Input{}})
		},
		"extra event": func(r *journeyRoute) { r.Actions[0].Event = journeyUint(1) },
	} {
		t.Run(name, func(t *testing.T) {
			var copy journeyRoute
			_ = json.Unmarshal(base, &copy)
			change(&copy)
			if _, _, err := loadJourneyRoute(journeyRouteFile(t, copy)); err == nil {
				t.Fatal("invalid route accepted")
			}
		})
	}
}

func TestJourneyItemSelectionAndRandomRanges(t *testing.T) {
	_, spec := journeyFixture(t)
	spec.Actions = append(spec.Actions, journeyAction{Type: "use_item", Item: journeyUint(1), TargetRole: journeyUint(0), Inputs: []palflow.Input{{Kind: palflow.InputRandom, Value: 0}, {Kind: palflow.InputRandom, Value: 2147483647}, {Kind: palflow.InputShop, Value: 0}, {Kind: palflow.InputShop, Value: 65535}}})
	if _, _, err := loadJourneyRoute(journeyRouteFile(t, spec)); err != nil {
		t.Fatal("usable-item action or opcode-specific random range rejected", err)
	}
	for _, change := range []func(*journeyAction){func(a *journeyAction) { a.Item = nil }, func(a *journeyAction) { a.TargetRole = nil }, func(a *journeyAction) { a.Direction = journeyUint(1) }, func(a *journeyAction) { a.Type = "interact" }} {
		copy := spec
		copy.Actions = append([]journeyAction(nil), spec.Actions...)
		change(&copy.Actions[1])
		if _, _, err := loadJourneyRoute(journeyRouteFile(t, copy)); err == nil {
			t.Fatal("invalid item action accepted")
		}
	}
}

// Numeric route only; original assets and dialogue stay outside the repository.
// This verifies a continuous spatial route, not a full game or original runtime.
func TestLocalJourneyOpeningWineAndVillage(t *testing.T) {
	dir := os.Getenv("PAL_GAME_DATA_DIR")
	if dir == "" {
		t.Skip("set PAL_GAME_DATA_DIR to the hash-matched local sample")
	}
	var output bytes.Buffer
	err := runJourney([]string{"-data", dir, "-route", "../../testdata/routes/sample-opening-wine-village.json"}, &output, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	var report journeyReport
	if err := json.Unmarshal(output.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if !report.Completed || report.FullGameVerified || len(report.Actions) != 546 || report.Final.World.Scene != 4 || report.Final.World.Cash != 550 || report.Final.World.Inventory[272] != 0 || len(report.InitialIdleFrames) != 5332 {
		t.Fatal("sample spatial route regression mismatch")
	}
	for _, a := range report.Actions {
		if !a.Committed || !a.ExpectationsMatched || a.Action.Type == "trigger" {
			t.Fatal("spatial route skipped or failed an action")
		}
		if a.Control != nil && (a.Control.State.World != nil || a.Control.State.IdleFrames != nil) {
			t.Fatal("duplicate full state escaped trace compaction")
		}
	}
	t.Logf("actions=%d final scene=%d position=(%d,%d) cash=%d reportBytes=%d", len(report.Actions), report.Final.World.Scene, report.Final.World.PartyX, report.Final.World.PartyY, report.Final.World.Cash, output.Len())
}

func TestLocalJourneyIslandLandfall(t *testing.T) {
	dir := os.Getenv("PAL_GAME_DATA_DIR")
	if dir == "" {
		t.Skip("set PAL_GAME_DATA_DIR to the hash-matched local sample")
	}
	var out bytes.Buffer
	if err := runJourney([]string{"-data", dir, "-compact", "-route", "../../testdata/routes/sample-opening-island-landfall.json"}, &out, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	var r journeyReport
	if err := json.Unmarshal(out.Bytes(), &r); err != nil {
		t.Fatal(err)
	}
	if !r.Completed || r.FullGameVerified || r.TraceDetail != "compact" || len(r.Actions) != 1084 || r.Final.World.Scene != 15 || r.Final.World.PartyX != 752 || r.Final.World.PartyY != 808 || r.Final.World.Inventory[279] != 1 || r.Final.World.Inventory[272] != 0 {
		t.Fatal("island landfall route mismatch")
	}
	messages := 0
	for _, a := range r.Actions {
		messages += len(a.MessageIDs)
		if !a.Committed || !a.ExpectationsMatched || a.Action.Type == "trigger" {
			t.Fatal("island route bypassed spatial control or failed")
		}
	}
	if messages != 372 {
		t.Fatal("island dialogue count mismatch", messages)
	}
	t.Logf("actions=%d messages=%d final scene=%d bytes=%d", len(r.Actions), messages, r.Final.World.Scene, out.Len())
}

func TestJourneyInputIdentityAndAliases(t *testing.T) {
	for _, kind := range []string{"ambiguous", "symlink", "missing"} {
		t.Run(kind, func(t *testing.T) {
			dir, _ := journeyFixture(t)
			path := filepath.Join(dir, "MGO.MKF")
			switch kind {
			case "ambiguous":
				if err := os.WriteFile(filepath.Join(dir, "mgo.mkf"), []byte("extra"), 0600); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				if err := os.Rename(path, filepath.Join(dir, "original")); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink("original", path); err != nil {
					t.Fatal(err)
				}
			case "missing":
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := journeyFiles(dir); err == nil {
				t.Fatal("invalid resource accepted")
			}
		})
	}
	dir, spec := journeyFixture(t)
	routePath := journeyRouteFile(t, spec)
	for _, kind := range []string{"same", "hardlink", "symlink", "symlink directory", "input hardlink"} {
		t.Run(kind, func(t *testing.T) {
			dest := routePath
			switch kind {
			case "hardlink", "input hardlink":
				dest = filepath.Join(t.TempDir(), "report.json")
				source := routePath
				if kind == "input hardlink" {
					source = filepath.Join(dir, "MGO.MKF")
				}
				if err := os.Link(source, dest); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				dest = filepath.Join(t.TempDir(), "report.json")
				if err := os.Symlink(routePath, dest); err != nil {
					t.Fatal(err)
				}
			case "symlink directory":
				parent := filepath.Join(t.TempDir(), "alias")
				if err := os.Symlink(filepath.Dir(routePath), parent); err != nil {
					t.Fatal(err)
				}
				dest = filepath.Join(parent, filepath.Base(routePath))
			}
			if err := runJourney([]string{"-data", dir, "-route", routePath, "-out", dest}, &bytes.Buffer{}, &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), "覆盖") {
				t.Fatal("output alias was not rejected", err)
			}
		})
	}
	spec.FilesSHA256["MAP.MKF"] = strings.Repeat("0", 64)
	var out bytes.Buffer
	if err := runJourney([]string{"-data", dir, "-route", journeyRouteFile(t, spec)}, &out, &bytes.Buffer{}); err == nil || out.Len() != 0 {
		t.Fatal("mismatched locked resource accepted")
	}
}

func TestJourneyInitRouteAndAtomicOutput(t *testing.T) {
	dir, _ := journeyFixture(t)
	path := filepath.Join(t.TempDir(), "starter.json")
	if err := runJourney([]string{"-data", dir, "-encoding", "gbk", "-init-route", path}, &bytes.Buffer{}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	spec, _, err := loadJourneyRoute(path)
	if err != nil || len(spec.FilesSHA256) != 7 || len(spec.Actions) != 1 || spec.Encoding != "gbk" {
		t.Fatal("bad starter route", err)
	}
	reportPath := filepath.Join(t.TempDir(), "report.json")
	if err := runJourney([]string{"-data", dir, "-route", path, "-out", reportPath}, &bytes.Buffer{}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(reportPath)
	if err != nil {
		t.Fatal(err)
	}
	var report journeyReport
	if err := json.Unmarshal(data, &report); err != nil || !report.Completed || report.Final.Actions != 1 {
		t.Fatal("starter route execution failed", err)
	}
	var out bytes.Buffer
	if err := runJourney([]string{"-data", dir, "-route", path, "-layout", "dos"}, &out, &bytes.Buffer{}); err == nil {
		t.Fatal("route layout silently overridden")
	}
	if err := journeyWriteReport("", journeyReport{Name: strings.Repeat("x", journeyReportLimit)}, &out); err == nil || out.Len() != 0 {
		t.Fatal("oversized report was emitted")
	}
}
