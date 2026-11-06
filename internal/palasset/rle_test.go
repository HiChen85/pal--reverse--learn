package palasset

import (
	"bytes"
	"encoding/binary"
	"image/color"
	"reflect"
	"testing"
)

func rleBytes(width, height uint16, runs ...byte) []byte {
	out := make([]byte, 4, 4+len(runs))
	binary.LittleEndian.PutUint16(out, width)
	binary.LittleEndian.PutUint16(out[2:], height)
	return append(out, runs...)
}

func TestRLEGoldenCoverageAndRGBA(t *testing.T) {
	stream := append([]byte{2, 0, 0, 0}, rleBytes(4, 2, 2, 0, 7, 0x82, 0x81, 3, 9, 0, 11)...)
	stream = append(stream, 0xff) // A directory's alignment byte is not a run.
	img, err := DecodeRLE(stream, 8)
	if err != nil {
		t.Fatal(err)
	}
	if img.Width != 4 || img.Height != 2 || img.BytesConsumed != len(stream)-1 || !bytes.Equal(img.Pix, []byte{0, 7, 0, 0, 0, 9, 0, 11}) || !reflect.DeepEqual(img.Opaque, []bool{true, true, false, false, false, true, true, true}) {
		t.Fatalf("golden bitmap changed: %+v", img)
	}
	var palette Palette
	palette[0] = color.RGBA{4, 8, 12, 255}
	palette[7] = color.RGBA{28, 32, 36, 255}
	palette[9] = color.RGBA{40, 44, 48, 255}
	palette[11] = color.RGBA{52, 56, 60, 255}
	rgba, err := img.RGBA(palette)
	if err != nil {
		t.Fatal(err)
	}
	want := []byte{4, 8, 12, 255, 28, 32, 36, 255, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 40, 44, 48, 255, 4, 8, 12, 255, 52, 56, 60, 255}
	if rgba.Bounds().Dx() != 4 || rgba.Bounds().Dy() != 2 || !bytes.Equal(rgba.Pix, want) {
		t.Fatalf("RGBA golden changed: %x", rgba.Pix)
	}
	stream[9] = 88
	if img.Pix[0] != 0 || img.Pix[1] != 7 {
		t.Fatal("decoded image retains caller stream")
	}
	rgba.Pix[0] = 200
	again, err := img.RGBA(palette)
	if err != nil || again.Pix[0] != 4 {
		t.Fatal("converted RGBA aliases another conversion")
	}
}

func TestRLEControlThresholdAndRowCrossing(t *testing.T) {
	// A high-bit control above 0x80+width is a literal, not a skip.
	values := make([]byte, 130)
	for i := range values {
		values[i] = byte(i)
	}
	img, err := DecodeRLE(rleBytes(1, 130, append([]byte{0x82}, values...)...), 130)
	if err != nil || !bytes.Equal(img.Pix, values) {
		t.Fatalf("high-bit literal not decoded: %v", err)
	}
	for _, covered := range img.Opaque {
		if !covered {
			t.Fatal("literal was treated as a transparent run")
		}
	}
	img, err = DecodeRLE(rleBytes(3, 2, 4, 1, 2, 3, 4, 0x82), 6)
	if err != nil || !bytes.Equal(img.Pix, []byte{1, 2, 3, 4, 0, 0}) || !reflect.DeepEqual(img.Opaque, []bool{true, true, true, true, false, false}) {
		t.Fatalf("runs crossing rows differ: %+v %v", img, err)
	}
	// Width >=128 makes every high-bit control a skip. Skip length remains
	// seven bits; two consecutive skips can finish a 128-pixel row.
	img, err = DecodeRLE(rleBytes(128, 1, 0xff, 0x81), 128)
	if err != nil || img.BytesConsumed != 6 {
		t.Fatalf("wide transparent bitmap differs: %+v %v", img, err)
	}
}

func TestRLETruncationBoundsAndNoOpControls(t *testing.T) {
	valid := append([]byte{2, 0, 0, 0}, rleBytes(4, 2, 2, 0, 7, 0x82, 0x81, 3, 9, 0, 11)...)
	for n := 0; n < len(valid); n++ {
		if _, err := DecodeRLE(valid[:n], 8); err == nil {
			t.Errorf("truncation at %d accepted", n)
		}
	}
	invalid := [][]byte{
		rleBytes(0, 1), rleBytes(1, 0), rleBytes(65535, 65535),
		rleBytes(1, 1, 2, 8, 9),    // Literal exceeds the bitmap.
		rleBytes(5, 1, 1, 9, 0x85), // Skip exceeds remaining pixels.
		rleBytes(2, 1, 2, 9),       // Truncated literal.
		rleBytes(1, 1, bytes.Repeat([]byte{0, 0x80}, 1024)...),
	}
	for i, stream := range invalid {
		if _, err := DecodeRLE(stream, 8); err == nil {
			t.Errorf("invalid case %d accepted", i)
		}
	}
	for _, max := range []int{-1, 0, 7, MaxRLEPixels + 1} {
		if _, err := DecodeRLE(valid, max); err == nil {
			t.Errorf("invalid pixel bound %d accepted", max)
		}
	}
	if _, err := DecodeRLE(make([]byte, MaxRLEBytes+1), 1); err == nil {
		t.Fatal("stream bound ignored")
	}
	noOps := bytes.Repeat([]byte{0, 0x80}, 1024)
	img, err := DecodeRLE(rleBytes(1, 1, append(noOps, 1, 0)...), 1)
	if err != nil || !img.Opaque[0] || img.Pix[0] != 0 {
		t.Fatal("finite no-op controls failed to advance input", err)
	}
}

func TestRGBARejectsMalformedPublicImages(t *testing.T) {
	for _, img := range []*IndexedImage{nil, {}, {Width: -1, Height: 1}, {Width: 1, Height: 0}, {Width: MaxRLEPixels + 1, Height: 1}, {Width: 1, Height: 1}, {Width: 1, Height: 1, Pix: []byte{0}, Opaque: []bool{true, false}}} {
		if _, err := img.RGBA(Palette{}); err == nil {
			t.Errorf("malformed indexed image accepted: %+v", img)
		}
	}
}
