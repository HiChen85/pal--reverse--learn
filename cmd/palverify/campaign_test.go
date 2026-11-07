package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/HiChen85/pal--reverse--learn/internal/palflow"
)

func campaignManifestFile(t *testing.T, segments ...string) string {
	t.Helper()
	data, err := json.Marshal(campaignManifest{Version: 1, Segments: segments})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "manifest.json")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func campaignReadReport(t *testing.T, path string) journeyReport {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var report journeyReport
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatal(err)
	}
	return report
}

func TestCampaignContinuousSegmentsMatchSingleJourney(t *testing.T) {
	for _, mode := range []string{"auto_attack", "fast_verify"} {
		t.Run(mode, func(t *testing.T) {
			dir, spec := journeyFixtureWithAuto(t, true)
			spec.Actions = append(spec.Actions,
				journeyAction{Type: "interact", Inputs: []palflow.Input{}},
				journeyAction{Type: "move", Direction: journeyUint(3), Inputs: []palflow.Input{}},
				journeyAction{Type: "trigger", Event: journeyUint(2), Inputs: []palflow.Input{}},
				journeyAction{Type: "enter", Inputs: []palflow.Input{}},
				journeyAction{Type: "move", Direction: journeyUint(3), Inputs: []palflow.Input{}},
			)
			var singleJSON bytes.Buffer
			if err := runJourney([]string{"-data", dir, "-compact", "-battle-mode", mode, "-route", journeyRouteFile(t, spec)}, &singleJSON, &bytes.Buffer{}); err != nil {
				t.Fatal(err)
			}
			var single journeyReport
			if err := json.Unmarshal(singleJSON.Bytes(), &single); err != nil {
				t.Fatal(err)
			}
			first, second := spec, spec
			first.Name, second.Name = "first", "second"
			first.Actions, second.Actions = spec.Actions[:3], spec.Actions[3:]
			firstPath, secondPath := journeyRouteFile(t, first), journeyRouteFile(t, second)
			if _, _, err := loadJourneyRoute(secondPath); err == nil {
				t.Fatal("ordinary journey accepted a continuation without new-game enter")
			}
			manifestDir := t.TempDir()
			manifest := filepath.Join(manifestDir, "manifest.json")
			// Relative segment paths resolve from the manifest, not the cwd.
			if err := os.WriteFile(filepath.Join(manifestDir, "first.json"), mustReadCampaignInput(t, firstPath), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(manifestDir, "second.json"), mustReadCampaignInput(t, secondPath), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(manifest, []byte(`{"version":1,"segments":["first.json","second.json"]}`), 0600); err != nil {
				t.Fatal(err)
			}
			dest := filepath.Join(t.TempDir(), "reports")
			var out bytes.Buffer
			if err := run([]string{"campaign", "-data", dir, "-manifest", manifest, "-out-dir", dest, "-battle-mode", mode}, &out, &bytes.Buffer{}); err != nil {
				t.Fatal(err)
			}
			var index campaignReport
			if err := json.Unmarshal(out.Bytes(), &index); err != nil {
				t.Fatal(err)
			}
			if !index.Completed || index.FullGameVerified || index.Dataset.ReleaseIdentified || index.ActionsPlanned != 6 || index.ActionsAttempted != 6 || index.ActionsCommitted != 6 || index.ExecutionPolicy.BattleMode != mode || len(index.Files) != 7 || !bytes.Equal(out.Bytes(), mustReadCampaignInput(t, filepath.Join(dest, "campaign.json"))) {
				t.Fatal("campaign summary lost actual completion, policy, sources or file identity", index)
			}
			if (len(index.ExecutionPolicy.BattleSimplifications) > 0) != (mode == "fast_verify") {
				t.Fatal("campaign hid verification-only simplifications")
			}
			var all []journeyActionReport
			previous := index.InitialSHA256
			var last journeyReport
			for i, entry := range index.Segments {
				path := filepath.Join(dest, entry.Report)
				part := campaignReadReport(t, path)
				if entry.Status != "completed" || entry.InitialSHA256 != previous || entry.InitialSHA256 != part.InitialSHA256 || entry.FinalSHA256 != journeyDigest(part.Final) || entry.RouteSHA256 != part.RouteSHA256 || entry.ReportSHA256 != journeyBytesDigest(mustReadCampaignInput(t, path)) || !reflect.DeepEqual(part.Files, index.Files) || part.FullGameVerified || !strings.Contains(part.Scope, "same live Session") || part.Kind != "continuous_session_campaign_segment" {
					t.Fatal("campaign hash chain or segment provenance failed", i, entry)
				}
				if i > 0 && part.Actions[0].BeforeSHA256 != previous {
					t.Fatal("continuation reset or replaced the active session")
				}
				all = append(all, part.Actions...)
				previous, last = entry.FinalSHA256, part
			}
			if !reflect.DeepEqual(single.Final, last.Final) || index.FinalSHA256 != journeyDigest(single.Final) || index.InitialSHA256 != single.InitialSHA256 {
				t.Fatal("segmented execution differs from the identical single journey")
			}
			for i, action := range single.Actions {
				got := all[i]
				if action.BeforeSHA256 != got.BeforeSHA256 || action.AfterSHA256 != got.AfterSHA256 || action.Work != got.Work || !reflect.DeepEqual(action.MessageIDs, got.MessageIDs) || !reflect.DeepEqual(action.Battles, got.Battles) {
					t.Fatal("segmentation changed action messages, work, battle summary or snapshot hashes", i)
				}
			}
			h := sha256.New()
			for _, action := range single.Actions {
				campaignHashAction(h, action)
			}
			if index.ActionSummarySHA256 != hex.EncodeToString(h.Sum(nil)) {
				t.Fatal("campaign action-summary hash depends on the segment boundaries")
			}
		})
	}
}

func mustReadCampaignInput(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestCampaignFailedContinuationPreservesPriorEvidence(t *testing.T) {
	for _, unexpectedEnter := range []bool{false, true} {
		dir, first := journeyFixture(t)
		first.Actions = append(first.Actions, journeyAction{Type: "interact", Inputs: []palflow.Input{}})
		second, third := first, first
		second.Name, third.Name = "second", "third"
		second.Actions = []journeyAction{
			{Type: "trigger", Event: journeyUint(2), Inputs: []palflow.Input{}},
			{Type: "enter", Inputs: []palflow.Input{}},
			{Type: "trigger", Event: journeyUint(3), Inputs: []palflow.Input{}},
		}
		if unexpectedEnter {
			second.Actions = []journeyAction{{Type: "enter", Inputs: []palflow.Input{}}}
		}
		third.Actions = []journeyAction{{Type: "move", Direction: journeyUint(3), Inputs: []palflow.Input{}}}
		manifest := campaignManifestFile(t, journeyRouteFile(t, first), journeyRouteFile(t, second), journeyRouteFile(t, third))
		dest := filepath.Join(t.TempDir(), "reports")
		var out bytes.Buffer
		if err := runCampaign([]string{"-data", dir, "-manifest", manifest, "-out-dir", dest}, &out, &bytes.Buffer{}); err == nil {
			t.Fatal("failed continuation was skipped")
		}
		var index campaignReport
		if err := json.Unmarshal(out.Bytes(), &index); err != nil {
			t.Fatal(err)
		}
		firstReport := campaignReadReport(t, filepath.Join(dest, campaignSegmentName(0)))
		failed := campaignReadReport(t, filepath.Join(dest, campaignSegmentName(1)))
		if index.Completed || index.FullGameVerified || index.Error == "" || index.Segments[0].Status != "completed" || index.Segments[1].Status != "failed" || index.Segments[2].Status != "not_run" || failed.Completed || !firstReport.Completed || failed.InitialSHA256 != journeyDigest(firstReport.Final) || failed.Final.World.Inventory[1] != 1 || index.FinalSHA256 != journeyDigest(failed.Final) {
			t.Fatal("failure lost prior state, evidence or chain identity")
		}
		if _, err := os.Stat(filepath.Join(dest, campaignSegmentName(2))); !os.IsNotExist(err) {
			t.Fatal("a later segment was executed or published after failure", err)
		}
		if unexpectedEnter {
			if len(failed.Actions) != 1 || failed.Actions[0].Committed || failed.Actions[0].BeforeSHA256 != failed.Actions[0].AfterSHA256 || !reflect.DeepEqual(firstReport.Final, failed.Final) || index.ActionsCommitted != 2 {
				t.Fatal("unexpected enter silently reset the live session")
			}
		} else if len(failed.Actions) != 3 || failed.Actions[2].Trace == nil || failed.Actions[2].Trace.Blocker == nil || failed.Actions[2].Trace.Blocker.Opcode != 0x7fff || failed.Actions[2].Committed || index.ActionsCommitted != 4 {
			t.Fatal("failed later opcode lost full available diagnostics or committed prior actions")
		}
	}
}

func TestCampaignAllInputValidationPrecedesAnyExecution(t *testing.T) {
	for _, kind := range []string{"missing", "same path", "hardlink", "symlink", "identical copy", "bad route", "first continuation", "resource drift", "optional drift", "missing optional", "actual hash", "layout drift", "encoding drift"} {
		t.Run(kind, func(t *testing.T) {
			dir, first := journeyFixture(t)
			second := first
			second.Name = "second"
			second.Actions = []journeyAction{{Type: "interact", Inputs: []palflow.Input{}}}
			firstPath := journeyRouteFile(t, first)
			secondPath := journeyRouteFile(t, second)
			switch kind {
			case "missing":
				secondPath = filepath.Join(t.TempDir(), "missing.json")
			case "same path":
				secondPath = firstPath
			case "hardlink", "symlink":
				secondPath = filepath.Join(t.TempDir(), "alias.json")
				link := os.Link
				if kind == "symlink" {
					link = os.Symlink
				}
				if err := link(firstPath, secondPath); err != nil {
					t.Fatal(err)
				}
			case "identical copy":
				secondPath = journeyRouteBytes(t, mustReadCampaignInput(t, firstPath))
			case "bad route":
				secondPath = journeyRouteBytes(t, []byte(`{"version":1}`))
			case "first continuation":
				firstPath = secondPath
			case "resource drift", "actual hash":
				second.FilesSHA256 = map[string]string{}
				for name, hash := range first.FilesSHA256 {
					second.FilesSHA256[name] = hash
				}
				second.FilesSHA256["MAP.MKF"] = strings.Repeat("0", 64)
				if kind == "actual hash" {
					first.FilesSHA256 = second.FilesSHA256
					firstPath = journeyRouteFile(t, first)
				}
				secondPath = journeyRouteFile(t, second)
			case "optional drift", "missing optional":
				second.OptionalFilesSHA256 = map[string]string{"RNG.MKF": strings.Repeat("0", 64)}
				if kind == "missing optional" {
					first.OptionalFilesSHA256 = second.OptionalFilesSHA256
					firstPath = journeyRouteFile(t, first)
				}
				secondPath = journeyRouteFile(t, second)
			case "layout drift":
				second.Layout = "dos"
				secondPath = journeyRouteFile(t, second)
			case "encoding drift":
				second.Encoding = "big5"
				secondPath = journeyRouteFile(t, second)
			}
			manifest := campaignManifestFile(t, firstPath, secondPath)
			dest := filepath.Join(t.TempDir(), "reports")
			var out bytes.Buffer
			if err := runCampaign([]string{"-data", dir, "-manifest", manifest, "-out-dir", dest}, &out, &bytes.Buffer{}); err == nil || out.Len() != 0 {
				t.Fatal("invalid later input executed a campaign prefix or emitted a success result", err)
			}
			if _, err := os.Stat(dest); !os.IsNotExist(err) {
				t.Fatal("upfront input failure created or published campaign output", err)
			}
		})
	}
}

func TestCampaignStrictManifestAndLimits(t *testing.T) {
	_, spec := journeyFixture(t)
	path := journeyRouteFile(t, spec)
	good, _ := json.Marshal(campaignManifest{Version: 1, Segments: []string{path}})
	cases := map[string][]byte{
		"duplicate": bytes.Replace(good, []byte(`"version":1`), []byte(`"version":1,"version":1`), 1),
		"case":      bytes.Replace(good, []byte(`"version":1`), []byte(`"Version":1`), 1),
		"null":      []byte(`{"version":1,"segments":null}`),
		"empty":     []byte(`{"version":1,"segments":[]}`),
		"unknown":   []byte(`{"version":1,"segments":[],"world":{}}`),
		"invalid":   append(append([]byte(nil), good...), 0xff),
		"trailing":  append(append([]byte(nil), good...), []byte(` {}`)...),
		"huge":      bytes.Repeat([]byte(" "), journeyRouteLimit+1),
		"blank":     []byte(`{"version":1,"segments":[" "]}`),
	}
	tooMany, _ := json.Marshal(campaignManifest{Version: 1, Segments: make([]string, campaignMaxSegments+1)})
	cases["too many"] = tooMany
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			if _, _, _, _, err := loadCampaign(journeyRouteBytes(t, data)); err == nil {
				t.Fatal("invalid manifest accepted")
			}
		})
	}
	if _, err := campaignReportBytes(struct{ Large string }{strings.Repeat("x", journeyReportLimit)}); err == nil {
		t.Fatal("oversized segment report accepted")
	}
}

func TestCampaignCumulativeActionLimitBeforeExecution(t *testing.T) {
	_, spec := journeyFixture(t)
	paths := []string{}
	for i := 0; i <= campaignMaxActions/journeyMaxActions; i++ {
		part := spec
		part.Name = fmt.Sprintf("budget-%d", i)
		part.Actions = make([]journeyAction, journeyMaxActions)
		for j := range part.Actions {
			part.Actions[j] = journeyAction{Type: "interact", Inputs: []palflow.Input{}}
		}
		if i == 0 {
			part.Actions[0].Type = "enter"
		}
		paths = append(paths, journeyRouteFile(t, part))
	}
	if _, _, _, _, err := loadCampaign(campaignManifestFile(t, paths...)); err == nil || !strings.Contains(err.Error(), "cumulative 250000") {
		t.Fatal("cumulative action bound was not enforced before execution", err)
	}
}

func TestCampaignCumulativeInputByteLimitBeforeExecution(t *testing.T) {
	_, spec := journeyFixture(t)
	paths := []string{}
	for i := 0; i <= campaignMaxBytes/journeyRouteLimit; i++ {
		part := spec
		part.Name = fmt.Sprintf("byte-budget-%d", i)
		if i > 0 {
			part.Actions = []journeyAction{{Type: "interact", Inputs: []palflow.Input{}}}
		}
		data, err := json.Marshal(part)
		if err != nil {
			t.Fatal(err)
		}
		data = append(data, bytes.Repeat([]byte(" "), journeyRouteLimit-len(data))...)
		paths = append(paths, journeyRouteBytes(t, data))
	}
	if _, _, _, _, err := loadCampaign(campaignManifestFile(t, paths...)); err == nil || !strings.Contains(err.Error(), "cumulative 64MiB") {
		t.Fatal("aggregate input bound was not enforced while retaining only a few actions", err)
	}
}

func TestCampaignOutputAliasesExistingFilesAndConcurrentPublishAreRejected(t *testing.T) {
	for _, alias := range []string{"manifest", "segment", "resource", "optional", "symlink dir", "symlink parent", "prior report"} {
		t.Run(alias, func(t *testing.T) {
			dir, spec := journeyFixture(t)
			first := journeyRouteFile(t, spec)
			manifest := campaignManifestFile(t, first)
			dest := t.TempDir()
			protected := first
			switch alias {
			case "manifest":
				protected = manifest
			case "resource":
				protected = filepath.Join(dir, "MGO.MKF")
			case "optional":
				protected = filepath.Join(dir, "PAT.MKF")
				if err := os.WriteFile(protected, mkf(make([]byte, 768)), 0600); err != nil {
					t.Fatal(err)
				}
			case "prior report":
				protected = filepath.Join(t.TempDir(), "old.json")
				if err := os.WriteFile(protected, []byte("previous evidence"), 0600); err != nil {
					t.Fatal(err)
				}
			case "symlink dir", "symlink parent":
				parent := filepath.Join(t.TempDir(), "alias")
				if err := os.Symlink(dest, parent); err != nil {
					t.Fatal(err)
				}
				dest = parent
				if alias == "symlink parent" {
					dest = filepath.Join(parent, "nested")
				}
			}
			before := mustReadCampaignInput(t, protected)
			if !strings.HasPrefix(alias, "symlink") {
				if err := os.Link(protected, filepath.Join(dest, campaignSegmentName(0))); err != nil {
					t.Fatal(err)
				}
			}
			var out bytes.Buffer
			if err := runCampaign([]string{"-data", dir, "-manifest", manifest, "-out-dir", dest}, &out, &bytes.Buffer{}); err == nil || out.Len() != 0 {
				t.Fatal("unsafe or existing output path accepted", err)
			}
			if !bytes.Equal(before, mustReadCampaignInput(t, protected)) {
				t.Fatal("output validation overwrote a protected input")
			}
		})
	}
	dest := filepath.Join(t.TempDir(), "new")
	output, err := prepareCampaignOutput(dest, 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dest, campaignSegmentName(0))
	old := []byte("appeared after preflight")
	if err := os.WriteFile(path, old, 0600); err != nil {
		t.Fatal(err)
	}
	if err := output.write(campaignSegmentName(0), []byte("{}\n")); err == nil || !bytes.Equal(old, mustReadCampaignInput(t, path)) {
		t.Fatal("atomic publication overwrote a concurrently created file", err)
	}
	entries, err := os.ReadDir(dest)
	if err != nil || len(entries) != 1 {
		t.Fatal("failed publication leaked a temporary file", err)
	}
}
