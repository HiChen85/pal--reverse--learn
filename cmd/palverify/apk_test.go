package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestAPKCommandReportsMissingPALDataWithoutClaimingFullGame(t *testing.T) {
	path := filepath.Join(t.TempDir(), "game.apk")
	var buf bytes.Buffer
	z := zip.NewWriter(&buf)
	for _, name := range []string{"AndroidManifest.xml", "classes.dex", "lib/armeabi-v7a/libcocos2dcpp.so"} {
		w, err := z.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = w.Write([]byte("synthetic")); err != nil {
			t.Fatal(err)
		}
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	var out, log bytes.Buffer
	if err := run([]string{"apk", "-file", path}, &out, &log); err != nil {
		t.Fatal(err, log.String())
	}
	var report struct {
		FullGameVerified bool `json:"full_game_verified"`
		Archive          struct {
			NameComplete         bool     `json:"name_complete"`
			WindowsDataValidated bool     `json:"windows_data_validated"`
			Missing              []string `json:"missing_data_files"`
		} `json:"archive"`
	}
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.FullGameVerified || report.Archive.NameComplete || report.Archive.WindowsDataValidated || len(report.Archive.Missing) != 4 {
		t.Fatalf("APK incorrectly accepted as PAL dataset: %s", out.String())
	}
}
func TestAPKCommandRejectsLFSPointerAndOutputOverwrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pointer.apk")
	if err := os.WriteFile(path, []byte("version https://git-lfs.github.com/spec/v1\noid sha256:abc\nsize 100\n"), 0600); err != nil {
		t.Fatal(err)
	}
	var out, log bytes.Buffer
	if err := run([]string{"apk", "-file", path}, &out, &log); err == nil {
		t.Fatal("LFS pointer accepted as APK")
	}
	if err := run([]string{"apk", "-file", path, "-out", path}, &out, &log); err == nil {
		t.Fatal("APK overwrite accepted")
	}
}
