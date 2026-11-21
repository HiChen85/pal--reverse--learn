package palasset

import (
	"encoding/binary"
	"errors"
	"fmt"
	"image"
)

// Format research: SDLPAL palcommon.c (GPL-3.0-or-later). This decoder is an
// independent, bounded implementation of PAL's indexed bitmap format.
const (
	MaxRLEPixels = 16 << 20
	MaxRLEBytes  = 64 << 20
)

// IndexedImage keeps coverage separate from color indexes. A literal index of
// zero is a visible pixel; only skip runs produce transparent pixels. Returned
// images own their slices and do not retain the source stream.
type IndexedImage struct {
	Width, Height int
	Pix           []uint8
	Opaque        []bool
	// BytesConsumed includes dimensions and any optional prefix, excluding
	// trailing directory padding or unrelated bytes.
	BytesConsumed int
}

// DecodeRLE expands a PAL bitmap, optionally prefixed by 02 00 00 00. Both
// dimensions are little-endian WORDs. maxPixels must be in 1..MaxRLEPixels;
// input is independently limited to MaxRLEBytes. A complete bitmap may have
// trailing bytes, since sprite directory spans include WORD alignment padding.
func DecodeRLE(data []byte, maxPixels int) (*IndexedImage, error) {
	width, height, consumed, err := walkRLE(data, maxPixels, nil)
	if err != nil {
		return nil, err
	}
	img := &IndexedImage{Width: width, Height: height, Pix: make([]uint8, width*height), Opaque: make([]bool, width*height), BytesConsumed: consumed}
	// Validate the entire stream before allocating. A second walk only copies
	// the already checked literals and cannot retain or mutate caller bytes.
	_, _, _, err = walkRLE(data, maxPixels, func(pixel int, literal []byte) {
		copy(img.Pix[pixel:], literal)
		for i := range literal {
			img.Opaque[pixel+i] = true
		}
	})
	if err != nil {
		return nil, err
	}
	return img, nil
}

// walkRLE also validates streams without allocating a pixel buffer. In PAL a
// high-bit control is a skip only if its value is at most 0x80+width; larger
// values are literal lengths. Literals may continue into the following row.
// Zero-length controls still consume input, so truncated no-op streams terminate.
func walkRLE(data []byte, maxPixels int, literal func(int, []byte)) (width, height, consumed int, err error) {
	fail := func(reason string) (int, int, int, error) {
		return 0, 0, 0, fmt.Errorf("RLE: %s", reason)
	}
	if maxPixels < 1 || maxPixels > MaxRLEPixels {
		return fail(fmt.Sprintf("maxPixels outside 1..%d", MaxRLEPixels))
	}
	if len(data) > MaxRLEBytes {
		return fail(fmt.Sprintf("stream exceeds %d bytes", MaxRLEBytes))
	}
	pos := 0
	if len(data) >= 4 && binary.LittleEndian.Uint32(data) == 2 {
		pos = 4
	}
	if len(data)-pos < 4 {
		return fail("truncated dimensions")
	}
	width = int(binary.LittleEndian.Uint16(data[pos:]))
	height = int(binary.LittleEndian.Uint16(data[pos+2:]))
	pos += 4
	if width == 0 || height == 0 {
		return fail("zero bitmap dimension")
	}
	if height > maxPixels/width {
		return fail(fmt.Sprintf("%dx%d bitmap exceeds pixel bound %d", width, height, maxPixels))
	}
	pixels := width * height
	for pixel := 0; pixel < pixels; {
		if pos >= len(data) {
			return fail("truncated run control")
		}
		control := int(data[pos])
		pos++
		if control&0x80 != 0 && control <= 0x80+width {
			run := control - 0x80
			if run > pixels-pixel {
				return fail("skip run exceeds bitmap")
			}
			pixel += run
			continue
		}
		if control > pixels-pixel {
			return fail("literal run exceeds bitmap")
		}
		if control > len(data)-pos {
			return fail("truncated literal run")
		}
		if literal != nil {
			literal(pixel, data[pos:pos+control])
		}
		pixel += control
		pos += control
	}
	return width, height, pos, nil
}

// RGBA returns a standard image with skipped pixels fully transparent. It
// checks exported slice lengths so malformed caller-built images cannot panic.
func (img *IndexedImage) RGBA(palette Palette) (*image.RGBA, error) {
	if img == nil || img.Width < 1 || img.Height < 1 || img.Width > MaxRLEPixels || img.Height > MaxRLEPixels/img.Width {
		return nil, errors.New("RLE: invalid indexed image dimensions")
	}
	pixels := img.Width * img.Height
	if len(img.Pix) != pixels || len(img.Opaque) != pixels {
		return nil, errors.New("RLE: indexed image slice lengths differ from dimensions")
	}
	out := image.NewRGBA(image.Rect(0, 0, img.Width, img.Height))
	for i, index := range img.Pix {
		if img.Opaque[i] {
			color := palette[index]
			out.Pix[i*4], out.Pix[i*4+1], out.Pix[i*4+2], out.Pix[i*4+3] = color.R, color.G, color.B, color.A
		}
	}
	return out, nil
}
