package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDemoCompletesBattleAndStory(t *testing.T) {
	var out bytes.Buffer
	if err := run([]string{"demo"}, strings.NewReader(""), &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "演示完成") || !strings.Contains(out.String(), "spring-cave") {
		t.Fatalf("demo did not reach gated ending: %s", out.String())
	}
}
func TestExportMergeValidateAndPlay(t *testing.T) {
	dir := t.TempDir()
	base := filepath.Join(dir, "base.json")
	merged := filepath.Join(dir, "merged.json")
	save := filepath.Join(dir, "play.sav.json")
	var out bytes.Buffer
	for _, args := range [][]string{{"export-demo", base}, {"validate", base}, {"merge", base, "../../examples/chapter-two.json", merged}, {"validate", merged}} {
		if err := run(args, strings.NewReader(""), &out); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
	}
	commands := "next\nchoose 1\nnext\nskill\nsave " + save + "\nload " + save + "\nquit\n"
	if err := run([]string{"play", merged}, strings.NewReader(commands), &out); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "错误:") {
		t.Fatal(out.String())
	}
	if _, err := os.Stat(save); err != nil {
		t.Fatal(err)
	}
}
func TestMalformedMKFAndCommandErrors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.mkf")
	if err := os.WriteFile(path, []byte{8, 0, 0, 0, 100, 0, 0, 0}, 0600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := run([]string{"mkf", path}, nil, &out); err == nil {
		t.Fatal("accepted out-of-file MKF offset")
	}
	if err := run([]string{"unknown"}, nil, &out); err == nil {
		t.Fatal("accepted unknown command")
	}
}
