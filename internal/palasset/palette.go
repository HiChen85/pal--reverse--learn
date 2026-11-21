package palasset

import (
	"errors"
	"fmt"
	"image/color"
)

// Format research: SDLPAL palette.c (GPL-3.0-or-later). PAT chunks contain
// 256 RGB triplets, optionally followed by another 256 night-color triplets.
const PaletteBytes = 256 * 3

type Palette [256]color.RGBA

// PaletteCatalog owns a bounded immutable PAT.MKF snapshot. Palette reads
// return arrays by value; no source bytes or mutable cache are exposed.
type PaletteCatalog struct {
	source mapSource
}

func OpenPAT(path string) (*PaletteCatalog, error) {
	source, err := readMapSource(path, "PAT")
	if err != nil {
		return nil, err
	}
	return &PaletteCatalog{source: source}, nil
}

func (c *PaletteCatalog) Count() int {
	if c == nil || c.source.archive == nil {
		return 0
	}
	return c.source.archive.Count()
}

func (c *PaletteCatalog) SourceSize() int64 {
	if c == nil {
		return 0
	}
	return c.source.size
}

func (c *PaletteCatalog) SourceSHA256() string {
	if c == nil {
		return ""
	}
	return c.source.sha256
}

// Palette reads exactly one or two 768-byte color sets. Missing night colors
// use the day set, matching PAL. Components must fit the format's six bits;
// conversion shifts left twice (63 becomes 252), with alpha fixed at 255.
func (c *PaletteCatalog) Palette(id uint16, night bool) (Palette, error) {
	var out Palette
	if c == nil || c.source.archive == nil {
		return out, errors.New("PAT: nil palette catalog")
	}
	size, err := c.source.archive.ChunkSize(int(id))
	if err != nil {
		return out, fmt.Errorf("PAT palette %d: %w", id, err)
	}
	if size != PaletteBytes && size != 2*PaletteBytes {
		return out, fmt.Errorf("PAT palette %d: chunk size %d is neither %d nor %d", id, size, PaletteBytes, 2*PaletteBytes)
	}
	data, err := c.source.archive.ReadChunk(int(id))
	if err != nil {
		return out, fmt.Errorf("PAT palette %d: %w", id, err)
	}
	for _, component := range data {
		if component > 63 {
			return out, fmt.Errorf("PAT palette %d: component exceeds six bits", id)
		}
	}
	start := 0
	if night && len(data) == 2*PaletteBytes {
		start = PaletteBytes
	}
	for i := range out {
		pos := start + i*3
		out[i] = color.RGBA{R: data[pos] << 2, G: data[pos+1] << 2, B: data[pos+2] << 2, A: 255}
	}
	return out, nil
}
