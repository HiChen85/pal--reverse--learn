package main

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/HiChen85/pal--reverse--learn/internal/content"
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

func TestRiverChapterEndings(t *testing.T) {
	const extension = "../../examples/chapter-two.json"
	const bridgeChoice = "把三盏渡灯挂上旧缆桩"

	t.Run("standalone by road", func(t *testing.T) {
		commands := "next\nchoose 2\nskill\nskill\nitem reed-balm\nskill\nnext\nchoose 1\nquit\n"
		out := playRiverRoute(t, extension, commands, "[river-arrival]", "HP 97/110 MP 6/12", "[river-end]")
		if strings.Contains(out, bridgeChoice) {
			t.Fatal("bridge choice visible without learning the ferry signal")
		}
	})

	t.Run("merged by bridge", func(t *testing.T) {
		dir := t.TempDir()
		basePath := filepath.Join(dir, "base.json")
		mergedPath := filepath.Join(dir, "merged.json")
		// Merge a valid base before linking its ending to the extension entry.
		if err := run([]string{"export-demo", basePath}, nil, &bytes.Buffer{}); err != nil {
			t.Fatal(err)
		}
		if err := run([]string{"merge", basePath, extension, mergedPath}, nil, &bytes.Buffer{}); err != nil {
			t.Fatal(err)
		}
		merged, err := content.Load(mergedPath)
		if err != nil {
			t.Fatal(err)
		}
		for i, node := range merged.Nodes {
			if node.ID == "ending" {
				merged.Nodes[i].Next = "river-arrival"
				break
			}
		}
		if err := content.Save(mergedPath, merged); err != nil {
			t.Fatal(err)
		}
		// Use a relative save path because the CLI splits commands on whitespace.
		t.Chdir(dir)
		commands := "next\nchoose 1\nnext\nskill\nskill\nskill\nnext\nchoose 1\nnext\nnext\n" +
			"next\nchoose 1\nnext\nchoose 1\n" +
			"save river.sav.json\nskill\nload river.sav.json\nskill\nskill\n" +
			"next\nchoose 2\nnext\nquit\n"
		out := playRiverRoute(t, mergedPath, commands, "[arrival]", "[spring-cave]", "[river-arrival]", "[river-lampkeeper]", bridgeChoice, "[river-bridge-end]")
		var enemyHP []string
		for line := range strings.SplitSeq(out, "\n") {
			if hp, found := strings.CutPrefix(line, "敌方1 缠索苔 HP "); found {
				enemyHP = append(enemyHP, hp)
			}
		}
		// The first skill changes HP, loading restores it, then both skills win.
		wantHP := []string{"92/92", "92/92", "50/92", "92/92", "50/92", "0/92"}
		if !slices.Equal(enemyHP, wantHP) {
			t.Fatalf("river enemy HP = %v, want %v\n%s", enemyHP, wantHP, out)
		}
	})
}

func playRiverRoute(t *testing.T, path, commands string, want ...string) string {
	t.Helper()
	var out bytes.Buffer
	if err := run([]string{"play", path}, strings.NewReader(commands), &out); err != nil {
		t.Fatal(err)
	}
	result := out.String()
	if strings.Contains(result, "错误:") {
		t.Fatal(result)
	}
	for _, match := range append(want, "战斗: victory", "本章结束。") {
		if !strings.Contains(result, match) {
			t.Fatalf("missing %q from play output:\n%s", match, result)
		}
	}
	return result
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
