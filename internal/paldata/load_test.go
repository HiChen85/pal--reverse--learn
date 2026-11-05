package paldata

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/traditionalchinese"
)

func u16(values ...uint16) []byte {
	out := make([]byte, len(values)*2)
	for i, v := range values {
		binary.LittleEndian.PutUint16(out[i*2:], v)
	}
	return out
}
func u32(values ...uint32) []byte {
	out := make([]byte, len(values)*4)
	for i, v := range values {
		binary.LittleEndian.PutUint32(out[i*4:], v)
	}
	return out
}
func mkf(chunks ...[]byte) []byte {
	head := make([]byte, 4*(len(chunks)+1))
	cursor := len(head)
	for i, c := range chunks {
		binary.LittleEndian.PutUint32(head[i*4:], uint32(cursor))
		cursor += len(c)
	}
	binary.LittleEndian.PutUint32(head[len(chunks)*4:], uint32(cursor))
	for _, c := range chunks {
		head = append(head, c...)
	}
	return head
}
func fixture(t *testing.T, encodingName string) (map[string][]byte, [5][]byte) {
	t.Helper()
	var cp encoding.Encoding = traditionalchinese.Big5
	if encodingName == "gbk" {
		cp = simplifiedchinese.GBK
	}
	msg, err := cp.NewEncoder().Bytes([]byte("你好$12~3\n_控制"))
	if err != nil {
		t.Fatal(err)
	}
	word, err := cp.NewEncoder().Bytes([]byte("寶劍1"))
	if err != nil {
		t.Fatal(err)
	}
	word = append(word, bytes.Repeat([]byte{' '}, WordWidth-len(word))...)
	chunks := [5][]byte{
		u16(65535, 12, 34, 65534, 1, 0, 65535, 1, 2, 3, 4, 5, 6, 7, 8, 9),
		u16(1, 1, 0, 0, 0, 0, 0, 1),
		u16(0, 1, 2, 3, 4, 5, 6),
		u32(0, uint32(len(msg))),
		u16(0, 0, 0, 0, 0xffff, 0, 0, 0),
	}
	files := map[string][]byte{"SsS.MkF": mkf(chunks[:]...), "data.mkf": mkf(make([][]byte, 15)...), "m.MSG": msg, "Word.DAT": word}
	return files, chunks
}
func writeFixture(t *testing.T, files map[string][]byte) string {
	t.Helper()
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}
func TestLoadRequestedWindowsLayoutBothEncodings(t *testing.T) {
	for _, cp := range []string{"big5", "gbk"} {
		t.Run(cp, func(t *testing.T) {
			files, _ := fixture(t, cp)
			g, err := Load(writeFixture(t, files), Options{Encoding: cp})
			if err != nil {
				t.Fatal(err)
			}
			if g.Profile != Profile || g.Encoding != cp {
				t.Fatal("missing explicit requested profile")
			}
			if len(g.Scripts) != 2 || g.Scripts[1].Opcode != 0xffff || g.Scripts[1].Operands[0] != 0 {
				t.Fatal("script table changed")
			}
			if len(g.Events) != 1 || g.Events[0].State != -1 || g.Events[0].Layer != -2 || g.Events[0].Trigger != 1 || g.Events[0].Raw[15] != 9 {
				t.Fatal("event layout changed")
			}
			if len(g.Scenes) != 2 || g.Scenes[0].Map != 1 || g.Scenes[1].EventStart != 1 {
				t.Fatal("scene boundaries changed")
			}
			if g.Objects[0] != [7]uint16{0, 1, 2, 3, 4, 5, 6} {
				t.Fatal("Windows object width changed")
			}
			if g.Messages[0] != "你好$12~3\n_控制" {
				t.Fatalf("dialogue control syntax changed: %q", g.Messages[0])
			}
			if g.Words[0] != "寶劍1" {
				t.Fatalf("raw word suffix lost: %q", g.Words[0])
			}
			if !bytes.Equal(g.RawMessages[0], files["m.MSG"]) || len(g.RawWords[0]) != 10 {
				t.Fatal("raw evidence not preserved")
			}
			if len(g.DataChunks) != 15 || g.DataChunks[14].Index != 14 {
				t.Fatal("DATA inventory incomplete")
			}
			if len(g.Files) != 4 {
				t.Fatal("missing file hashes")
			}
			for _, info := range g.Files {
				sum := sha256.Sum256(files[info.Name])
				if info.SHA256 != fmt.Sprintf("%x", sum) || info.Size != int64(len(files[info.Name])) {
					t.Fatal("incorrect evidence fingerprint")
				}
			}
		})
	}
}
func TestRejectMissingAmbiguousAndNonRegularFiles(t *testing.T) {
	for _, name := range []string{"SsS.MkF", "data.mkf", "m.MSG", "Word.DAT"} {
		t.Run("missing "+name, func(t *testing.T) {
			files, _ := fixture(t, "big5")
			delete(files, name)
			if _, err := Load(writeFixture(t, files), Options{Encoding: "big5"}); err == nil {
				t.Fatal("accepted missing file")
			}
		})
	}
	files, _ := fixture(t, "big5")
	files["SSS.MKF"] = files["SsS.MkF"]
	if _, err := Load(writeFixture(t, files), Options{Encoding: "big5"}); err == nil {
		t.Fatal("accepted ambiguous names")
	}
	files, _ = fixture(t, "big5")
	delete(files, "m.MSG")
	dir := writeFixture(t, files)
	if err := os.Mkdir(filepath.Join(dir, "m.msg"), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(dir, Options{Encoding: "big5"}); err == nil {
		t.Fatal("accepted directory as resource")
	}
	files, _ = fixture(t, "big5")
	dir = writeFixture(t, files)
	if err := os.Remove(filepath.Join(dir, "m.MSG")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(dir, "Word.DAT"), filepath.Join(dir, "m.msg")); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(dir, Options{Encoding: "big5"}); err == nil {
		t.Fatal("accepted symlink")
	}
}
func TestRejectMalformedTables(t *testing.T) {
	tests := map[string]func(map[string][]byte, *[5][]byte){
		"event truncated":      func(_ map[string][]byte, c *[5][]byte) { c[0] = c[0][:31] },
		"scene truncated":      func(_ map[string][]byte, c *[5][]byte) { c[1] = c[1][:15] },
		"DOS object width":     func(_ map[string][]byte, c *[5][]byte) { c[2] = c[2][:12] },
		"script truncated":     func(_ map[string][]byte, c *[5][]byte) { c[4] = c[4][:15] },
		"event script outside": func(_ map[string][]byte, c *[5][]byte) { binary.LittleEndian.PutUint16(c[0][8:], 2) },
		"scene script outside": func(_ map[string][]byte, c *[5][]byte) { binary.LittleEndian.PutUint16(c[1][2:], 2) },
		"scene event outside":  func(_ map[string][]byte, c *[5][]byte) { binary.LittleEndian.PutUint16(c[1][6:], 2) },
		"offset truncated":     func(_ map[string][]byte, c *[5][]byte) { c[3] = c[3][:7] },
		"offset outside":       func(_ map[string][]byte, c *[5][]byte) { c[3] = u32(0, 9999) },
		"offset descending":    func(_ map[string][]byte, c *[5][]byte) { c[3] = u32(0, 5, 4) },
		"bad text encoding":    func(f map[string][]byte, c *[5][]byte) { f["m.MSG"] = []byte{0x81}; c[3] = u32(0, 1) },
		"bad DATA container":   func(f map[string][]byte, _ *[5][]byte) { f["data.mkf"] = []byte{0, 0, 0, 0} },
		"missing DATA chunks":  func(f map[string][]byte, _ *[5][]byte) { f["data.mkf"] = mkf(make([][]byte, 14)...) },
		"too many scenes":      func(_ map[string][]byte, c *[5][]byte) { c[1] = make([]byte, (MaxScenes+1)*8) },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			files, chunks := fixture(t, "big5")
			mutate(files, &chunks)
			files["SsS.MkF"] = mkf(chunks[:]...)
			if _, err := Load(writeFixture(t, files), Options{Encoding: "big5"}); err == nil {
				t.Fatal("accepted malformed resource")
			}
		})
	}
}
func TestBoundsAndExplicitEncoding(t *testing.T) {
	files, _ := fixture(t, "big5")
	dir := writeFixture(t, files)
	for _, cp := range []string{"", "auto", "utf8", "BIG5"} {
		if _, err := Load(dir, Options{Encoding: cp}); err == nil {
			t.Fatalf("accepted implicit/unknown encoding %q", cp)
		}
	}
	f, err := os.OpenFile(filepath.Join(dir, "m.MSG"), os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(MaxMessageBytes + 1); err != nil {
		f.Close()
		t.Fatal(err)
	}
	f.Close()
	if _, err := Load(dir, Options{Encoding: "big5"}); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("missing file limit: %v", err)
	}
}
func TestEmptyMessageAndWordPadding(t *testing.T) {
	files, c := fixture(t, "big5")
	files["m.MSG"] = nil
	c[3] = u32(0, 0)
	files["SsS.MkF"] = mkf(c[:]...)
	files["Word.DAT"] = make([]byte, 10)
	g, err := Load(writeFixture(t, files), Options{Encoding: "big5"})
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Messages) != 1 || g.Messages[0] != "" || g.Words[0] != "" {
		t.Fatal("empty entries not preserved")
	}
}

func TestPartialFinalWordKeepsExactRawBytes(t *testing.T) {
	files, _ := fixture(t, "big5")
	files["Word.DAT"] = append([]byte("full-slot "), []byte("tail")...)
	g, err := Load(writeFixture(t, files), Options{Encoding: "big5"})
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Words) != 2 || g.Words[0] != "full-slot" || g.Words[1] != "tail" {
		t.Fatalf("partial word decoding: %#v", g.Words)
	}
	if string(g.RawWords[1]) != "tail" || len(g.RawWords[1]) != 4 {
		t.Fatal("partial slot raw bytes were padded or lost")
	}
}

func TestUnindexedMessagePaddingAndDanglingDialogueAreWarnings(t *testing.T) {
	files, chunks := fixture(t, "big5")
	files["m.MSG"] = []byte("__text__")
	chunks[3] = u32(2, 6)
	binary.LittleEndian.PutUint16(chunks[4][10:], 1)
	files["SsS.MkF"] = mkf(chunks[:]...)
	g, err := Load(writeFixture(t, files), Options{Encoding: "big5"})
	if err != nil {
		t.Fatal(err)
	}
	if g.Messages[0] != "text" || string(g.RawMessages[0]) != "text" {
		t.Fatal("message spans changed")
	}
	if len(g.Warnings) != 3 {
		t.Fatalf("missing diagnostics: %#v", g.Warnings)
	}
	for _, want := range []string{"2 unindexed prefix bytes", "2 unindexed trailing bytes", "0xffff message index 1 outside"} {
		found := false
		for _, warning := range g.Warnings {
			if strings.Contains(warning, want) {
				found = true
			}
		}
		if !found {
			t.Fatalf("missing warning %q: %#v", want, g.Warnings)
		}
	}
}
