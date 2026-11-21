package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func addOriginalMedia(t *testing.T, dir string) {
	t.Helper()
	for name, data := range map[string][]byte{
		"rNg.MkF": originalMKF(originalMKF(originalRawYJ([]byte{0}))),
		"fBp.MkF": originalMKF(originalRawYJ(make([]byte, 320*200))),
	} {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestOriginalMediaProvidersAndExportedIdentities(t *testing.T) {
	for _, session := range []bool{false, true} {
		t.Run(map[bool]string{false: "diagnostic", true: "session"}[session], func(t *testing.T) {
			dir := originalMapRuntimeFixture(t)
			addOriginalMedia(t, dir)
			rewriteOriginalSSS(t, dir, func(c [][]byte) {
				c[1] = originalWords(1, 1, 0, 0, 2, 0, 0, 2)
				c[4] = originalWords(0, 0, 0, 0, 0x36, 0, 0, 0, 0x37, 0, 0, 16, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0)
			})
			o := newOriginalData()
			o.directory = dir
			if err := o.load(); err != nil {
				t.Fatal(err)
			}
			if o.rng == nil || o.fbp == nil || o.mediaError != "" || len(o.mediaIdentities) != 2 {
				t.Fatalf("media not loaded: %s", o.mediaError)
			}
			o.useSession = session
			if err := o.run("on_enter"); err != nil {
				t.Fatal(err)
			}
			found := false
			for _, step := range o.history[len(o.history)-1].Report.Trace {
				if step.Media != nil && step.Media.RNG != nil {
					found = true
					if len(step.Media.RNG.Frames) != 1 {
						t.Fatal("media instruction was not decoded")
					}
				}
			}
			if !found {
				t.Fatal("no RNG trace in actual UI action")
			}
			o.exportPath = filepath.Join(t.TempDir(), "media.paltrace.json")
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
			if len(evidence.MediaIdentities) != 2 || evidence.MediaIdentities[0].SHA256 != o.rng.SourceSHA256() {
				t.Fatal("export omitted media identities")
			}
		})
	}
}

func TestOriginalMediaMissingCatalogStopsDependentOperation(t *testing.T) {
	dir := originalMapRuntimeFixture(t)
	rewriteOriginalSSS(t, dir, func(c [][]byte) {
		c[1] = originalWords(1, 1, 0, 0, 2, 0, 0, 2)
		c[4] = originalWords(0, 0, 0, 0, 0x36, 0, 0, 0, 0x37, 2, 1, 16, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0)
	})
	o := newOriginalData()
	o.directory = dir
	if err := o.load(); err != nil {
		t.Fatal(err)
	}
	if o.mediaError != "" || o.rng != nil {
		t.Fatal("absent optional archives should leave provider nil")
	}
	if err := o.run("on_enter"); err == nil || !strings.Contains(err.Error(), "RNG") {
		t.Fatal("missing required provider was accepted", err)
	}
	if o.history[len(o.history)-1].Report.Completed {
		t.Fatal("failed media operation marked complete")
	}
}

func TestOriginalMediaProtectionDoesNotRequireSuccessfulParsing(t *testing.T) {
	for _, kind := range []string{"malformed", "duplicate", "symlink"} {
		t.Run(kind, func(t *testing.T) {
			dir := originalMapRuntimeFixture(t)
			path := filepath.Join(dir, "RNG.MKF")
			if kind == "symlink" {
				if err := os.Symlink(filepath.Join(dir, "MGO.MKF"), path); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := os.WriteFile(path, []byte("malformed archive"), 0600); err != nil {
					t.Fatal(err)
				}
				if kind == "duplicate" {
					if err := os.WriteFile(filepath.Join(dir, "rng.mkf"), originalMKF(), 0600); err != nil {
						t.Fatal(err)
					}
				}
			}
			o := newOriginalData()
			o.directory = dir
			if err := o.load(); err != nil {
				t.Fatal(err)
			}
			if o.mediaError == "" || o.rng != nil {
				t.Fatal("invalid optional media was silently accepted")
			}
			alias := filepath.Join(t.TempDir(), "alias.json")
			if kind == "symlink" {
				if err := os.Symlink(path, alias); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := os.Link(path, alias); err != nil {
					t.Fatal(err)
				}
			}
			o.exportPath = alias
			if err := o.validateExportPath(); err == nil {
				t.Fatal("failed parse allowed overwriting source alias")
			}
		})
	}
}
