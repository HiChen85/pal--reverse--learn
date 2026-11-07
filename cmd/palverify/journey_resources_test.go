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
)

func journeyMediaFixture(t *testing.T, dir string) map[string]string {
	t.Helper()
	files := map[string][]byte{
		"PAT.MKF": mkf(make([]byte, palasset.PaletteBytes)),
		"RNG.MKF": mkf(mkf(journeyRawYJ1([]byte{0}))),
		"FBP.MKF": mkf(journeyRawYJ1(make([]byte, 320*200))),
	}
	hashes := map[string]string{}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, strings.ToLower(name)), data, 0600); err != nil {
			t.Fatal(err)
		}
		hashes[name] = fmt.Sprintf("%x", sha256.Sum256(data))
	}
	return hashes
}

func TestJourneyOptionalMediaBindingAndObservedIdentities(t *testing.T) {
	dir, spec := journeyFixture(t)
	hashes := journeyMediaFixture(t, dir)
	for _, bind := range []bool{false, true} {
		if bind {
			spec.OptionalFilesSHA256 = hashes
		}
		var out, log bytes.Buffer
		if err := runJourney([]string{"-data", dir, "-route", journeyRouteFile(t, spec)}, &out, &log); err != nil {
			t.Fatal(err, log.String())
		}
		var report journeyReport
		if err := json.Unmarshal(out.Bytes(), &report); err != nil {
			t.Fatal(err)
		}
		if !report.Completed || len(report.Files) != 10 {
			t.Fatalf("media evidence not retained: completed=%t files=%d", report.Completed, len(report.Files))
		}
		if bind && len(report.OptionalFilesSHA256) != 3 || !bind && len(report.OptionalFilesSHA256) != 0 {
			t.Fatal("report confused observed media with route-pinned media")
		}
		for _, file := range report.Files {
			if want := hashes[file.Name]; want != "" && file.SHA256 != want {
				t.Fatal("wrong media identity", file.Name)
			}
		}
	}
	var out, log bytes.Buffer
	path := filepath.Join(t.TempDir(), "starter.json")
	if err := runJourney([]string{"-data", dir, "-encoding", "gbk", "-init-route", path}, &out, &log); err != nil {
		t.Fatal(err)
	}
	starter, _, err := loadJourneyRoute(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(starter.FilesSHA256) != 7 || len(starter.OptionalFilesSHA256) != 3 {
		t.Fatalf("starter identities: %+v", starter)
	}
	for name, hash := range hashes {
		if starter.OptionalFilesSHA256[name] != hash {
			t.Fatal("starter did not bind", name)
		}
	}
}

func TestJourneyOptionalMediaPinsRejectMissingAndChanged(t *testing.T) {
	for _, missing := range []bool{false, true} {
		t.Run(fmt.Sprint(missing), func(t *testing.T) {
			dir, spec := journeyFixture(t)
			spec.OptionalFilesSHA256 = journeyMediaFixture(t, dir)
			if missing {
				if err := os.Remove(filepath.Join(dir, "rng.mkf")); err != nil {
					t.Fatal(err)
				}
			} else {
				spec.OptionalFilesSHA256["RNG.MKF"] = strings.Repeat("0", 64)
			}
			var out, log bytes.Buffer
			err := runJourney([]string{"-data", dir, "-route", journeyRouteFile(t, spec)}, &out, &log)
			if err == nil || !strings.Contains(err.Error(), "RNG.MKF") || out.Len() != 0 {
				t.Fatalf("pin mismatch accepted: %v %s", err, out.String())
			}
		})
	}
}

func TestJourneyOptionalMediaRejectAliasesAndProtectMalformedInput(t *testing.T) {
	for _, kind := range []string{"duplicate", "symlink", "malformed", "hardlink"} {
		t.Run(kind, func(t *testing.T) {
			dir, spec := journeyFixture(t)
			journeyMediaFixture(t, dir)
			path := filepath.Join(dir, "rng.mkf")
			args := []string{"-data", dir, "-route", journeyRouteFile(t, spec)}
			switch kind {
			case "duplicate":
				if err := os.WriteFile(filepath.Join(dir, "RNG.MKF"), mkf(), 0600); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(filepath.Join(dir, "fbp.mkf"), path); err != nil {
					t.Fatal(err)
				}
			case "malformed", "hardlink":
				if err := os.WriteFile(path, []byte("invalid media archive"), 0600); err != nil {
					t.Fatal(err)
				}
				if kind == "hardlink" {
					alias := filepath.Join(t.TempDir(), "report.json")
					if err := os.Link(path, alias); err != nil {
						t.Fatal(err)
					}
					args = append(args, "-out", alias)
				}
			}
			var out, log bytes.Buffer
			err := runJourney(args, &out, &log)
			if err == nil || out.Len() != 0 {
				t.Fatalf("bad media accepted: %v", err)
			}
			if kind == "hardlink" {
				if !strings.Contains(err.Error(), "不能覆盖") {
					t.Fatal("overwrite check must precede parsing", err)
				}
				got, _ := os.ReadFile(path)
				if string(got) != "invalid media archive" {
					t.Fatal("source was overwritten")
				}
			}
		})
	}
}

func TestJourneyOptionalMediaStrictHashSchema(t *testing.T) {
	_, base := journeyFixture(t)
	for _, hashes := range []map[string]string{
		{"rng.mkf": strings.Repeat("0", 64)}, {"RNG.MKF": strings.Repeat("A", 64)},
		{"RNG.MKF": "00"}, {"OTHER.MKF": strings.Repeat("0", 64)},
	} {
		base.OptionalFilesSHA256 = hashes
		if _, _, err := loadJourneyRoute(journeyRouteFile(t, base)); err == nil {
			t.Fatal("invalid optional binding accepted", hashes)
		}
	}
}

func TestJourneyUsesLoadedRNGProvider(t *testing.T) {
	dir, spec := journeyFixture(t)
	spec.OptionalFilesSHA256 = journeyMediaFixture(t, dir)
	path := filepath.Join(dir, "SSS.MKF")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	start := int(binary.LittleEndian.Uint32(data[4*4:]))
	// OnEnter selects RNG zero, validates its one frame, then stop0.
	for i, row := range [][4]uint16{{0x36, 0, 0, 0}, {0x37, 0, 0, 16}, {0, 0, 0, 0}} {
		for j, v := range row {
			binary.LittleEndian.PutUint16(data[start+(i+1)*8+j*2:], v)
		}
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	spec.FilesSHA256["SSS.MKF"] = fmt.Sprintf("%x", sha256.Sum256(data))
	var out, log bytes.Buffer
	if err := runJourney([]string{"-data", dir, "-route", journeyRouteFile(t, spec)}, &out, &log); err != nil {
		t.Fatal(err, log.String(), out.String())
	}
	var report journeyReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if !report.Completed || report.Actions[0].Trace == nil {
		t.Fatal("missing actual RNG trace")
	}
	found := false
	for _, step := range report.Actions[0].Trace.Trace {
		if step.Media != nil && step.Media.RNG != nil {
			found = true
			if len(step.Media.RNG.Frames) != 1 || step.Media.RNG.ArchiveSHA256 != spec.OptionalFilesSHA256["RNG.MKF"] {
				t.Fatal("RNG provider evidence missing")
			}
		}
	}
	if !found {
		t.Fatal("OnEnter did not execute loaded RNG")
	}
}

func TestJourneyBattleModeRejectsInvalidBeforeReadingData(t *testing.T) {
	var out, log bytes.Buffer
	err := runJourney([]string{"-data", "unused", "-route", "unused", "-battle-mode", "disabled"}, &out, &log)
	if err == nil || !strings.Contains(err.Error(), "battle-mode") {
		t.Fatal(err)
	}
}
