package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/HiChen85/pal--reverse--learn/internal/palflow"
)

const (
	questGate uint16 = iota + 1
	questRelay
	questTokens
	questPermitDesk
)

const (
	questTokenItem  uint16 = 7
	questBadgeItem  uint16 = 11
	questPermitItem uint16 = 14
)

const (
	questGateEntry     uint16 = 3
	questRelayEntry    uint16 = 13
	questRelayReturned uint16 = 21
	questTokensEntry   uint16 = 27
	questPermitEntry   uint16 = 31
	questArrivalEntry  uint16 = 35
)

const (
	questGateLocked uint16 = iota
	questRelayPrompt
	questRelayDeclined
	questRelayMissing
	questTokensFound
	questRelayRepaired
	questRelayGreeting
	questPermitIssued
	questGateByPermit
	questGateByRelay
	questArrived
)

func sideQuestCampaignFixture(t *testing.T) (string, journeyRoute) {
	t.Helper()
	// Reuse the generated maps, sprites, and runtime tables for this scenario.
	dir, spec := journeyFixture(t)
	code := [][4]uint16{
		1: {0x46, 12, 11, 0}, // Scene 1 starts at the gate.
		{},
		questGateEntry: {0x94, questRelay, 2, 7},
		{0x58, questPermitItem, 1, 11},
		{0xffff, questGateByPermit},
		{3, 8},
		7: {0xffff, questGateByRelay},
		8: {0x49, questGate, 0},
		{0x59, 2},
		{},
		11: {0xffff, questGateLocked},
		{},
		questRelayEntry: {0xffff, questRelayPrompt},
		{0x0a, 25},
		{0x58, questTokenItem, 1, 23},
		{0x1f, questTokenItem, 0xffff},
		{0x1f, questBadgeItem, 1},
		{0x49, questRelay, 2},
		{0xffff, questRelayRepaired},
		{1},                // The next interaction starts at the follow-up dialogue.
		questRelayReturned: {0xffff, questRelayGreeting},
		{},
		23: {0xffff, questRelayMissing},
		{},
		25: {0xffff, questRelayDeclined},
		{},
		questTokensEntry: {0x1f, questTokenItem, 2},
		{0x49, questTokens, 0},
		{0xffff, questTokensFound},
		{},
		questPermitEntry: {0x1f, questPermitItem, 1},
		{0x49, questPermitDesk, 0},
		{0xffff, questPermitIssued},
		{},
		questArrivalEntry: {0x46, 17, 15, 0},
		{0xffff, questArrived},
		{},
	}
	scripts := make([]byte, len(code)*8)
	for i, row := range code {
		for j, value := range row {
			binary.LittleEndian.PutUint16(scripts[i*8+j*2:], value)
		}
	}
	positions := [][3]uint16{
		{384, 176, questGateEntry},
		{368, 168, questRelayEntry},
		{352, 160, questTokensEntry},
		{368, 184, questPermitEntry},
	}
	events := make([]byte, len(positions)*32)
	for i, pos := range positions {
		row := [16]uint16{1: pos[0], 2: pos[1], 4: pos[2], 6: 1, 7: 1}
		for j, value := range row {
			binary.LittleEndian.PutUint16(events[i*32+j*2:], value)
		}
	}
	boundary := uint16(len(positions))
	scenes := make([]byte, 3*8)
	for i, row := range [][4]uint16{{1, 1, 0, 0}, {2, questArrivalEntry, 0, boundary}, {0, 0, 0, boundary}} {
		for j, value := range row {
			binary.LittleEndian.PutUint16(scenes[i*8+j*2:], value)
		}
	}
	lines := []string{
		questGateLocked:    "The signal gate is locked.",
		questRelayPrompt:   "Spend a coil to fix the relay?",
		questRelayDeclined: "The relay can wait.",
		questRelayMissing:  "You need a coil first.",
		questTokensFound:   "The bin holds two coils.",
		questRelayRepaired: "Signal restored. Take this badge.",
		questRelayGreeting: "The relay is still online.",
		questPermitIssued:  "Your survey permit is ready.",
		questGateByPermit:  "Survey permit accepted.",
		questGateByRelay:   "The relay signal opens the gate.",
		questArrived:       "You reach the survey platform.",
	}
	var messages []byte
	offsets := make([]byte, (len(lines)+1)*4)
	for i, line := range lines {
		messages = append(messages, line...)
		binary.LittleEndian.PutUint32(offsets[(i+1)*4:], uint32(len(messages)))
	}
	var words bytes.Buffer
	names := map[uint16]string{0: "Surveyor", questTokenItem: "Coil", questBadgeItem: "Badge", questPermitItem: "Permit"}
	for id := uint16(0); id <= questPermitItem; id++ {
		fmt.Fprintf(&words, "%-10s", names[id])
	}
	for name, data := range map[string][]byte{
		"SSS.MKF":  mkf(events, scenes, make([]byte, (questPermitItem+1)*14), offsets, scripts),
		"M.MSG":    messages,
		"WORD.DAT": words.Bytes(),
	} {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
			t.Fatal(err)
		}
		spec.FilesSHA256[name] = journeyBytesDigest(data)
	}
	return dir, spec
}

func TestCampaignOptionalSideQuestAndMainGate(t *testing.T) {
	type checkpoint struct {
		tokens, badges, permits int
		gate, relay, bin, desk  int16
	}
	type step struct {
		action                  journeyAction
		searchEvent, startEntry uint16
	}
	type segment struct {
		name       string
		relayEntry uint16
		steps      []step
	}
	expect := func(s step, scene uint16, state checkpoint, messages ...uint16) step {
		ids := append([]uint16{}, messages...)
		s.action.ExpectedMessages = &ids
		s.action.ExpectedScene = journeyUint(scene)
		s.action.ExpectedInventory = map[uint16]int{questTokenItem: state.tokens, questBadgeItem: state.badges, questPermitItem: state.permits}
		s.action.ExpectedEventStates = map[uint16]int16{questGate: state.gate, questRelay: state.relay, questTokens: state.bin, questPermitDesk: state.desk}
		if s.action.Inputs == nil {
			s.action.Inputs = []palflow.Input{}
		}
		return s
	}
	enter := step{action: journeyAction{Type: "enter"}}
	move := func(direction uint16) step {
		return step{action: journeyAction{Type: "move", Direction: journeyUint(direction)}}
	}
	interact := func(id, entry uint16, input ...palflow.Input) step {
		return step{action: journeyAction{Type: "interact", Inputs: input}, searchEvent: id, startEntry: entry}
	}
	no, yes := palflow.Input{Kind: palflow.InputConfirm, Value: 0}, palflow.Input{Kind: palflow.InputConfirm, Value: 1}
	const south, west, north, east = 0, 1, 2, 3
	start := checkpoint{gate: 1, relay: 1, bin: 1, desk: 1}
	collected := start
	collected.tokens, collected.bin = 2, 0
	repaired := collected
	repaired.tokens, repaired.badges, repaired.relay = 1, 1, 2
	openedByRelay := repaired
	openedByRelay.gate = 0
	permitted := start
	permitted.permits, permitted.desk = 1, 0
	openedByPermit := permitted
	openedByPermit.gate = 0
	intro := segment{"check-gate", questRelayEntry, []step{
		expect(enter, 1, start),
		expect(interact(questGate, questGateEntry), 1, start, questGateLocked),
	}}
	for _, tc := range []struct {
		name     string
		segments []segment
	}{
		{"repair-relay", []segment{
			intro,
			{"decline-and-check-stock", questRelayEntry, []step{
				expect(move(west), 1, start),
				expect(interact(questRelay, questRelayEntry, no), 1, start, questRelayPrompt, questRelayDeclined),
				expect(interact(questRelay, questRelayEntry, yes), 1, start, questRelayPrompt, questRelayMissing),
			}},
			{"collect-and-repair", questRelayReturned, []step{
				expect(move(west), 1, start),
				expect(interact(questTokens, questTokensEntry), 1, collected, questTokensFound),
				expect(interact(0, 0), 1, collected),
				expect(move(east), 1, collected),
				expect(interact(questRelay, questRelayEntry, no), 1, collected, questRelayPrompt, questRelayDeclined),
				expect(interact(questRelay, questRelayEntry, yes), 1, repaired, questRelayPrompt, questRelayRepaired),
			}},
			{"revisit-and-cross", questRelayReturned, []step{
				expect(interact(questRelay, questRelayReturned), 1, repaired, questRelayGreeting),
				expect(move(east), 1, repaired),
				expect(interact(questGate, questGateEntry), 2, openedByRelay, questGateByRelay),
				expect(enter, 2, openedByRelay, questArrived),
			}},
		}},
		{"skip-relay", []segment{
			intro,
			{"collect-permit", questRelayEntry, []step{
				expect(move(south), 1, start),
				expect(interact(questPermitDesk, questPermitEntry), 1, permitted, questPermitIssued),
				expect(move(north), 1, permitted),
			}},
			{"cross-with-permit", questRelayEntry, []step{
				expect(interact(questGate, questGateEntry), 2, openedByPermit, questGateByPermit),
				expect(enter, 2, openedByPermit, questArrived),
			}},
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir, base := sideQuestCampaignFixture(t)
			routes := make([]string, 0, len(tc.segments))
			for _, segment := range tc.segments {
				route := base
				route.Name = segment.name
				route.Actions = make([]journeyAction, 0, len(segment.steps))
				for _, step := range segment.steps {
					route.Actions = append(route.Actions, step.action)
				}
				routes = append(routes, journeyRouteFile(t, route))
			}
			manifest := campaignManifestFile(t, routes...)
			dest := filepath.Join(t.TempDir(), "reports")
			var output bytes.Buffer
			if err := run([]string{"campaign", "-data", dir, "-manifest", manifest, "-out-dir", dest, "-compact=false"}, &output, &bytes.Buffer{}); err != nil {
				t.Fatal(err)
			}
			var index campaignReport
			if err := json.Unmarshal(output.Bytes(), &index); err != nil {
				t.Fatal(err)
			}
			if !index.Completed || index.FullGameVerified || len(index.Segments) != len(tc.segments) || index.InitialSHA256 == "" {
				t.Fatalf("unexpected campaign summary: %+v", index)
			}
			previous, actions := index.InitialSHA256, 0
			for i, summary := range index.Segments {
				want := tc.segments[i]
				report := campaignReadReport(t, filepath.Join(dest, summary.Report))
				if !report.Completed || summary.Status != "completed" || summary.InitialSHA256 != previous || report.InitialSHA256 != previous || len(report.Actions) != len(want.steps) {
					t.Fatalf("%s: incomplete or disconnected campaign segment", want.name)
				}
				for j, got := range report.Actions {
					step := want.steps[j]
					if got.BeforeSHA256 != previous || got.AfterSHA256 == "" || !got.Committed || !got.ExpectationsMatched || !slices.Equal(got.MessageIDs, *step.action.ExpectedMessages) {
						t.Fatalf("%s action %d: unexpected messages %v, commitment, or hash chain", want.name, j+1, got.MessageIDs)
					}
					control := got.Control
					switch step.action.Type {
					case "move":
						if control == nil || !control.Moved || len(control.TriggeredEvents) != 0 {
							t.Fatalf("%s action %d: expected an unobstructed move", want.name, j+1)
						}
					case "interact":
						if control == nil || control.SearchEventID != step.searchEvent || control.InputsConsumed != len(step.action.Inputs) {
							t.Fatalf("%s action %d: expected search event %d and %d consumed inputs", want.name, j+1, step.searchEvent, len(step.action.Inputs))
						}
						if step.searchEvent == 0 {
							if len(control.TriggeredEvents) != 0 {
								t.Fatalf("%s action %d: spent pickup triggered again", want.name, j+1)
							}
						} else if !slices.Equal(control.TriggeredEvents, []uint16{step.searchEvent}) || len(control.Triggers) != 1 || len(control.Triggers[0].Trace) == 0 || control.Triggers[0].Trace[0].PC != step.startEntry {
							t.Fatalf("%s action %d: expected event %d to start at script %d", want.name, j+1, step.searchEvent, step.startEntry)
						}
					}
					previous = got.AfterSHA256
					actions++
				}
				if summary.FinalSHA256 != previous || journeyDigest(report.Final) != previous || report.Final.Actions != actions {
					t.Fatalf("%s: final snapshot or committed action count broke the chain", want.name)
				}
				w := report.Final.World
				last := want.steps[len(want.steps)-1].action
				if w == nil || w.Scene != *last.ExpectedScene {
					t.Fatalf("%s: final snapshot did not reach scene %d", want.name, *last.ExpectedScene)
				}
				for id, count := range last.ExpectedInventory {
					if w.Inventory[id] != count {
						t.Fatalf("%s: item %d count = %d, want %d", want.name, id, w.Inventory[id], count)
					}
				}
				for id, state := range last.ExpectedEventStates {
					if event, ok := w.Events[id]; !ok || event.State != state {
						t.Fatalf("%s: event %d state = %d, want %d (present: %v)", want.name, id, event.State, state, ok)
					}
				}
				if w.Events[questRelay].Trigger != want.relayEntry {
					t.Fatalf("%s: relay script entry = %d, want %d", want.name, w.Events[questRelay].Trigger, want.relayEntry)
				}
				if i == len(tc.segments)-1 && (report.Final.NeedsSceneEnter || w.SceneResources == nil || w.SceneResources.SceneID != 2 || w.SceneResources.MapID != 2) {
					t.Fatal("destination scene did not finish loading")
				}
			}
			if index.FinalSHA256 != previous || index.ActionsCommitted != actions || index.ActionsAttempted != actions || index.ActionsPlanned != actions {
				t.Fatal("campaign summary lost the final snapshot or action counts")
			}
		})
	}
}
