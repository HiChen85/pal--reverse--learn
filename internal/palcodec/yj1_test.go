package palcodec

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/HiChen85/pal--reverse--learn/internal/pal"
)

func packedBits(bits string) []byte {
	d := make([]byte, ((len(bits)+15)/16)*2)
	for i, c := range bits {
		if c == '1' {
			p := (i / 16) * 2
			w := binary.LittleEndian.Uint16(d[p:])
			w |= 1 << uint(15-i%16)
			binary.LittleEndian.PutUint16(d[p:], w)
		}
	}
	return d
}
func fileYJ(tree, flags []byte, output int, blocks ...[]byte) []byte {
	d := make([]byte, 16)
	copy(d, "YJ_1")
	binary.LittleEndian.PutUint32(d[4:], uint32(output))
	binary.LittleEndian.PutUint16(d[12:], uint16(len(blocks)))
	d[15] = byte(len(tree) / 2)
	d = append(d, tree...)
	d = append(d, flags...)
	for _, b := range blocks {
		d = append(d, b...)
	}
	binary.LittleEndian.PutUint32(d[8:], uint32(len(d)))
	return d
}
func rawYJ(data string) []byte {
	b := make([]byte, 4)
	binary.LittleEndian.PutUint16(b, uint16(len(data)))
	return append(b, []byte(data)...)
}
func compressedYJ(plain int, b yjBlock, bits string) []byte {
	d := make([]byte, 24)
	payload := packedBits(bits)
	binary.LittleEndian.PutUint16(d, uint16(plain))
	binary.LittleEndian.PutUint16(d[2:], uint16(24+len(payload)))
	for i, v := range b.repeat {
		binary.LittleEndian.PutUint16(d[4+i*2:], v)
	}
	copy(d[12:16], b.offsetWidths[:])
	copy(d[16:19], b.repeatWidths[:])
	copy(d[19:22], b.countWidths[:])
	copy(d[22:24], b.counts[:])
	return append(d, payload...)
}
func overlapYJ() []byte {
	// Hand-constructed golden: literal A, then five overlapping copies at
	// distance1, then a zero literal-loop marker. Stored as 16-bit LE words.
	b := yjBlock{repeat: [4]uint16{5}, offsetWidths: [4]byte{1}, counts: [2]byte{1, 0}}
	return fileYJ([]byte{'A', 'B'}, packedBits("00"), 6, compressedYJ(6, b, "1"+"0"+"1"+"00"+"00"+"1"+"000"))
}
func TestDecodeYJ1RawBlocksAndPadding(t *testing.T) {
	d := fileYJ(nil, nil, 5, rawYJ("ab"), rawYJ("cde"))
	d = append(d, 1, 2, 3)
	out, err := DecodeYJ1(d, 5)
	if err != nil || string(out) != "abcde" {
		t.Fatalf("%q %v", out, err)
	}
	out[0] = 'X'
	again, err := DecodeYJ1(d, 5)
	if err != nil || string(again) != "abcde" {
		t.Fatal("output aliases source")
	}
	empty, err := DecodeYJ1(fileYJ(nil, nil, 0), 0)
	if err != nil || len(empty) != 0 {
		t.Fatal("empty output", err)
	}
}
func TestDecodeYJ1CompressedOverlapGolden(t *testing.T) {
	out, err := DecodeYJ1(overlapYJ(), 6)
	if err != nil || string(out) != "AAAAAA" {
		t.Fatalf("golden overlap %q %v", out, err)
	}
}
func TestDecodeYJ1TreeTraversalAndVariableLiteralCount(t *testing.T) {
	// Root0 -> leaf1 A / internal2 -> leaf3 B / leaf4 C.
	// Loop count: 0, selector01, three-bit count100 =4. ABCA codes0,10,11,0.
	b := yjBlock{countWidths: [3]byte{3}, counts: [2]byte{1, 0}}
	d := fileYJ([]byte{'A', 1, 'B', 'C'}, packedBits("0100"), 4, compressedYJ(4, b, "0"+"01"+"100"+"0"+"10"+"11"+"0"+"000"))
	out, err := DecodeYJ1(d, 4)
	if err != nil || string(out) != "ABCA" {
		t.Fatalf("tree golden %q %v", out, err)
	}
}
func TestDecodeYJ1VariableRepeatAndCrossBlockHistory(t *testing.T) {
	// Previous raw block AB. Literal A then copy3 bytes at distance3 =ABAABA.
	// Repeat selector10 + variable1 + three-bit011 yields repeat3.
	b := yjBlock{offsetWidths: [4]byte{2}, repeatWidths: [3]byte{0, 3}, counts: [2]byte{1, 0}}
	d := fileYJ([]byte{'A', 'B'}, packedBits("00"), 6, rawYJ("AB"), compressedYJ(4, b, "1"+"0"+"1"+"10"+"1"+"011"+"00"+"11"+"000"))
	out, err := DecodeYJ1(d, 6)
	if err != nil || string(out) != "ABAABA" {
		t.Fatalf("history golden %q %v", out, err)
	}
}
func TestYJ1BitOrderAcrossStorageWords(t *testing.T) {
	r := yjBits{data: []byte{0x23, 0x81, 0xcd, 0xab}}
	for i, want := range []uint16{0x812, 0x3a, 0xbcd} {
		n := []int{12, 8, 12}[i]
		got, err := r.read(n)
		if err != nil || got != want {
			t.Fatalf("read%d=%x want%x err%v", n, got, want, err)
		}
	}
	if _, err := r.read(1); err == nil {
		t.Fatal("read beyond stream")
	}
	if _, err := r.read(17); err == nil {
		t.Fatal("wide code")
	}
}
func TestDecodeYJ1RejectMalformed(t *testing.T) {
	cases := map[string]func([]byte) []byte{
		"signature":              func(d []byte) []byte { d[0] = 'X'; return d },
		"short header":           func(d []byte) []byte { return d[:15] },
		"length beyond input":    func(d []byte) []byte { binary.LittleEndian.PutUint32(d[8:], uint32(len(d)+1)); return d },
		"short declared span":    func(d []byte) []byte { binary.LittleEndian.PutUint32(d[8:], 15); return d },
		"huge output":            func(d []byte) []byte { binary.LittleEndian.PutUint32(d[4:], MaxYJ1Bytes+1); return d },
		"output undercount":      func(d []byte) []byte { binary.LittleEndian.PutUint32(d[4:], 5); return d },
		"output overcount":       func(d []byte) []byte { binary.LittleEndian.PutUint32(d[4:], 7); return d },
		"tree bounds":            func(d []byte) []byte { d[16] = 1; copy(d[18:20], packedBits("10")); return d },
		"tree truncated":         func(d []byte) []byte { d[15] = 250; return d },
		"missing block":          func(d []byte) []byte { binary.LittleEndian.PutUint16(d[12:], 2); return d },
		"short block span":       func(d []byte) []byte { binary.LittleEndian.PutUint16(d[22:], 23); return d },
		"long block span":        func(d []byte) []byte { binary.LittleEndian.PutUint16(d[22:], 60000); return d },
		"wide code":              func(d []byte) []byte { d[32] = 17; return d },
		"zero distance":          func(d []byte) []byte { copy(d[44:], packedBits("1"+"0"+"1"+"00"+"00"+"0"+"000")); return d },
		"repeat output overflow": func(d []byte) []byte { binary.LittleEndian.PutUint16(d[24:], 6); return d },
		"early terminator":       func(d []byte) []byte { copy(d[44:], packedBits("000")); return d },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			out, err := DecodeYJ1(mutate(overlapYJ()), MaxYJ1Bytes)
			if err == nil || out != nil {
				t.Fatalf("accepted malformed %q: %x %v", name, out, err)
			}
		})
	}
	if _, err := DecodeYJ1(overlapYJ(), -1); err == nil {
		t.Fatal("negative budget")
	}
	if _, err := DecodeYJ1(overlapYJ(), MaxYJ1Bytes+1); err == nil {
		t.Fatal("excess budget")
	}
	if _, err := DecodeYJ1(overlapYJ(), 5); err == nil {
		t.Fatal("small budget")
	}
	cycle := fileYJ([]byte{'A', 0, 'B', 'C'}, packedBits("0100"), 0)
	if _, err := DecodeYJ1(cycle, 1); err == nil {
		t.Fatal("tree self cycle")
	}
	longLiteral := yjBlock{counts: [2]byte{17, 0}}
	truncated := fileYJ([]byte{'A', 'B'}, packedBits("00"), 17, compressedYJ(17, longLiteral, "1"+strings.Repeat("0", 15)))
	if _, err := DecodeYJ1(truncated, 17); err == nil {
		t.Fatal("bit stream truncation")
	}
	raw := fileYJ(nil, nil, 4, rawYJ("abc"))
	binary.LittleEndian.PutUint16(raw[16:], 4)
	if _, err := DecodeYJ1(raw, 4); err == nil {
		t.Fatal("raw truncation")
	}
}
func FuzzDecodeYJ1(f *testing.F) {
	f.Add(overlapYJ())
	f.Add(fileYJ(nil, nil, 3, rawYJ("raw")))
	f.Add([]byte("YJ_1"))
	f.Fuzz(func(t *testing.T, d []byte) {
		out, err := DecodeYJ1(d, 1<<20)
		if err == nil {
			if len(d) < 16 || len(out) != int(binary.LittleEndian.Uint32(d[4:])) {
				t.Fatal("successful length invariant")
			}
		} else if out != nil {
			t.Fatal("partial output")
		}
	})
}

// Optional authorized local probe logs metadata only; no assets enter fixtures.
func TestYJ1LocalAPK(t *testing.T) {
	path := os.Getenv("PALCODEC_APK")
	if path == "" {
		t.Skip("set PALCODEC_APK for local resource metadata probe")
	}
	z, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer z.Close()
	for _, entry := range z.File {
		if entry.Name != "assets/sdlpal/abc.mkf" {
			continue
		}
		if entry.UncompressedSize64 > MaxYJ1Bytes {
			t.Fatal("archive resource too large")
		}
		r, err := entry.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(io.LimitReader(r, MaxYJ1Bytes+1))
		r.Close()
		if err != nil || len(data) > MaxYJ1Bytes {
			t.Fatal("read bounded resource", err)
		}
		a, err := pal.Open(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			t.Fatal(err)
		}
		chunk, err := a.ReadChunk(1)
		if err != nil {
			t.Fatal(err)
		}
		out, err := DecodeYJ1(chunk, MaxYJ1Bytes)
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(out)
		frames := -1
		if len(out) >= 2 {
			frames = int(binary.LittleEndian.Uint16(out)) - 1
		}
		t.Logf("ABC chunk1 compressed=%d decoded=%d sha256=%x spriteFrames=%d", len(chunk), len(out), sum, frames)
		if dest := os.Getenv("PALCODEC_OUTPUT"); dest != "" {
			if err := os.WriteFile(dest, out, 0600); err != nil {
				t.Fatal(err)
			}
		}
		return
	}
	t.Fatal(fmt.Sprintf("required ABC resource not present in selected APK"))
}

// The split-MGO probe checks all sprite containers and can save frame counts
// outside the source repository. It does not emit any decoded pixel data.
func TestYJ1LocalMGO(t *testing.T) {
	path := os.Getenv("PALCODEC_APK")
	if path == "" {
		t.Skip("set PALCODEC_APK for local split-MGO metadata probe")
	}
	z, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer z.Close()
	var resource []byte
	for part := 1; part <= 2; part++ {
		name := fmt.Sprintf("assets/sdlpal/mgo.mkf.part%d", part)
		found := false
		for _, entry := range z.File {
			if entry.Name != name {
				continue
			}
			found = true
			if entry.UncompressedSize64 > MaxYJ1Bytes {
				t.Fatal("MGO part too large")
			}
			r, err := entry.Open()
			if err != nil {
				t.Fatal(err)
			}
			d, err := io.ReadAll(io.LimitReader(r, MaxYJ1Bytes+1))
			r.Close()
			if err != nil || len(d) > MaxYJ1Bytes-len(resource) {
				t.Fatal("MGO resource bounds", err)
			}
			resource = append(resource, d...)
			break
		}
		if !found {
			t.Fatal("missing split MGO part")
		}
	}
	a, err := pal.Open(bytes.NewReader(resource), int64(len(resource)))
	if err != nil {
		t.Fatal(err)
	}
	metadata := map[int]int{}
	invalidFrames := 0
	total := 0
	aggregate := sha256.New()
	for i := 0; i < a.Count(); i++ {
		chunk, err := a.ReadChunk(i)
		if err != nil {
			t.Fatal(err)
		}
		if len(chunk) == 0 {
			continue
		}
		out, err := DecodeYJ1(chunk, MaxYJ1Bytes)
		if err != nil {
			t.Fatalf("MGO chunk%d: %v", i, err)
		}
		if len(out) < 2 {
			t.Fatalf("MGO chunk%d sprite header missing", i)
		}
		count := int(binary.LittleEndian.Uint16(out)) - 1
		if count < 0 || (count+1)*2 > len(out) {
			t.Fatalf("MGO chunk%d invalid frame directory", i)
		}
		for j := 0; j < count; j++ {
			start := int(binary.LittleEndian.Uint16(out[j*2:])) * 2
			if start < 0 || start >= len(out) {
				invalidFrames++
			}
		}
		metadata[i] = count
		total += len(out)
		aggregate.Write(out)
	}
	t.Logf("MGO chunks=%d decodedSprites=%d totalDecoded=%d aggregateSHA256=%x invalidFrameOffsets=%d", a.Count(), len(metadata), total, aggregate.Sum(nil), invalidFrames)
	if path := os.Getenv("PALCODEC_FRAME_METADATA"); path != "" {
		var b strings.Builder
		fmt.Fprintf(&b, "# MGO frame-count metadata only; no sprite pixels\n")
		for i := 0; i < a.Count(); i++ {
			if count, ok := metadata[i]; ok {
				fmt.Fprintf(&b, "%d %d\n", i, count)
			}
		}
		if err := os.WriteFile(path, []byte(b.String()), 0600); err != nil {
			t.Fatal(err)
		}
	}
}
