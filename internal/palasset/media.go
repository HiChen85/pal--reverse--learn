package palasset

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"sync"

	"github.com/HiChen85/pal--reverse--learn/internal/pal"
	"github.com/HiChen85/pal--reverse--learn/internal/palcodec"
)

// Media format/behavior research: SDLPAL rngplay.c and ending.c at
// d4bf69b64d335cd680555af9df90e7ec4f3006a3 (GPL-3.0-or-later). This bounded
// importer independently validates nested MKF directories and decoded command
// spans. It does not execute the reference, render pixels, or retain game data
// in reports. Only DOS YJ1 compression is currently decoded.

const RNGBufferBytes = 65000

type RNGFrameInfo struct {
	Movie, FrameCount uint16
	Frame             uint32
	Status            string
	Reason            string
	CompressedSize    int
	DecodedSize       int
	SHA256            string
}

type RNGCatalog struct {
	source      mapSource
	mu          sync.Mutex
	lastMovie   int
	lastArchive *pal.Archive
}

func OpenRNG(path string) (*RNGCatalog, error) {
	source, err := readMapSource(path, "RNG")
	if err != nil {
		return nil, err
	}
	return &RNGCatalog{source: source, lastMovie: -1}, nil
}
func (c *RNGCatalog) Count() int {
	if c == nil {
		return 0
	}
	return c.source.archive.Count()
}
func (c *RNGCatalog) SourceSHA256() string {
	if c == nil {
		return ""
	}
	return c.source.sha256
}
func (c *RNGCatalog) SourceSize() int64 {
	if c == nil {
		return 0
	}
	return c.source.size
}

// Frame distinguishes a known playback stop (missing/empty/oversized frame or
// failed decompression) from an unsafe command stream. Unsafe indexing blocks
// inspection; it cannot be used as evidence of successful reference playback.
func (c *RNGCatalog) Frame(movie uint16, frame uint32) (RNGFrameInfo, error) {
	info := RNGFrameInfo{Movie: movie, Frame: frame}
	if c == nil || c.source.archive == nil {
		return info, fmt.Errorf("RNG: nil catalog")
	}
	if int(movie) >= c.Count() {
		info.Status = "missing_movie"
		return info, nil
	}
	c.mu.Lock()
	archive := c.lastArchive
	if c.lastMovie != int(movie) {
		data, err := c.source.archive.ReadChunk(int(movie))
		if err != nil {
			c.mu.Unlock()
			return info, err
		}
		if len(data) == 0 {
			c.mu.Unlock()
			info.Status = "empty_movie"
			return info, nil
		}
		archive, err = pal.Open(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			c.mu.Unlock()
			return info, fmt.Errorf("RNG movie %d: %w", movie, err)
		}
		if archive.Count() > 65535 {
			c.mu.Unlock()
			return info, fmt.Errorf("RNG movie %d: frame count exceeds 65535", movie)
		}
		c.lastMovie, c.lastArchive = int(movie), archive
	}
	c.mu.Unlock()
	info.FrameCount = uint16(archive.Count())
	if frame >= uint32(archive.Count()) {
		info.Status = "missing_frame"
		return info, nil
	}
	size, err := archive.ChunkSize(int(frame))
	if err != nil {
		return info, err
	}
	info.CompressedSize = int(size)
	if size == 0 {
		info.Status = "empty_frame"
		return info, nil
	}
	if size > RNGBufferBytes {
		info.Status = "frame_exceeds_read_buffer"
		return info, nil
	}
	compressed, err := archive.ReadChunk(int(frame))
	if err != nil {
		return info, err
	}
	if len(compressed) < 16 || string(compressed[:4]) != "YJ_1" {
		return info, fmt.Errorf("RNG movie %d frame %d: unsupported or unrecognized compression; require YJ1", movie, frame)
	}
	if binary.LittleEndian.Uint32(compressed[4:8]) > RNGBufferBytes {
		info.Status = "decompression_failed"
		info.Reason = "declared output exceeds reference destination buffer"
		return info, nil
	}
	decoded, err := palcodec.DecodeYJ1(compressed, RNGBufferBytes)
	if err != nil {
		return info, fmt.Errorf("RNG movie %d frame %d: malformed YJ1 stream: %w", movie, frame, err)
	}
	if err := ValidateRNGCommands(decoded); err != nil {
		return info, fmt.Errorf("RNG movie %d frame %d: %w", movie, frame, err)
	}
	info.Status = "playable"
	info.DecodedSize = len(decoded)
	sum := sha256.Sum256(decoded)
	info.SHA256 = fmt.Sprintf("%x", sum)
	return info, nil
}

// ValidateRNGCommands checks every read and pair-write against a 320x200
// indexed framebuffer. Unknown command bytes consume only their opcode, as in
// the reference's switch. End markers terminate the stream; trailing padding
// is never interpreted. Reaching the decoded end without a marker is valid.
func ValidateRNGCommands(data []byte) error {
	pos, dst := 0, 0
	readCount := func(width int) (int, error) {
		if width < 1 || width > 2 || pos+width > len(data) {
			return 0, fmt.Errorf("truncated command operand at %d", pos)
		}
		n := int(data[pos])
		if width == 2 {
			n |= int(data[pos+1]) << 8
		}
		pos += width
		return n + 1, nil
	}
	for pos < len(data) {
		op := data[pos]
		pos++
		if op == 0 || op == 0x13 {
			return nil
		}
		pairs, literal, repeat := 0, false, false
		switch {
		case op == 2:
			dst += 2
		case op == 3 || op == 4:
			n, err := readCount(int(op) - 2)
			if err != nil {
				return err
			}
			dst += n * 2
		case op >= 6 && op <= 10:
			pairs, literal = int(op)-5, true
		case op == 11 || op == 12:
			n, err := readCount(int(op) - 10)
			if err != nil {
				return err
			}
			pairs, literal = n, true
		case op >= 13 && op <= 16:
			pairs, repeat = int(op)-11, true
		case op == 17 || op == 18:
			n, err := readCount(int(op) - 16)
			if err != nil {
				return err
			}
			pairs, repeat = n, true
		}
		if pairs > 0 {
			if dst < 0 || dst+pairs*2 > 320*200 {
				return fmt.Errorf("command %02x writes beyond framebuffer at %d", op, dst)
			}
			needed := 0
			if literal {
				needed = pairs * 2
			}
			if repeat {
				needed = 2
			}
			if pos+needed > len(data) {
				return fmt.Errorf("command %02x truncated pixel payload at %d", op, pos)
			}
			pos += needed
			dst += pairs * 2
		}
	}
	return nil
}

type FBPInfo struct {
	ID             uint16
	Status         string
	Reason         string
	CompressedSize int
	DecodedSize    int
	SHA256         string
}
type FBPCatalog struct{ source mapSource }

func OpenFBP(path string) (*FBPCatalog, error) {
	source, err := readMapSource(path, "FBP")
	if err != nil {
		return nil, err
	}
	return &FBPCatalog{source: source}, nil
}
func (c *FBPCatalog) Count() int {
	if c == nil {
		return 0
	}
	return c.source.archive.Count()
}
func (c *FBPCatalog) SourceSHA256() string {
	if c == nil {
		return ""
	}
	return c.source.sha256
}
func (c *FBPCatalog) SourceSize() int64 {
	if c == nil {
		return 0
	}
	return c.source.size
}

// Picture records PAL_ShowFBP's deliberate black fallback when loading or
// decompression fails. A nonempty short decode is unsafe in the reference and
// is rejected instead of pretending the missing pixels are zero-initialized.
func (c *FBPCatalog) Picture(id uint16) (FBPInfo, error) {
	info := FBPInfo{ID: id}
	if c == nil || c.source.archive == nil {
		return info, fmt.Errorf("FBP: nil catalog")
	}
	if int(id) >= c.Count() {
		info.Status = "black_missing_picture"
		return info, nil
	}
	data, err := c.source.archive.ReadChunk(int(id))
	if err != nil {
		return info, err
	}
	info.CompressedSize = len(data)
	if len(data) == 0 {
		info.Status = "black_empty_picture"
		return info, nil
	}
	if len(data) < 16 || string(data[:4]) != "YJ_1" {
		return info, fmt.Errorf("FBP %d: unsupported or unrecognized compression; require YJ1", id)
	}
	if binary.LittleEndian.Uint32(data[4:8]) > 320*200 {
		info.Status = "black_decompression_failed"
		info.Reason = "declared output exceeds reference destination buffer"
		return info, nil
	}
	decoded, err := palcodec.DecodeYJ1(data, 320*200)
	if err != nil {
		return info, fmt.Errorf("FBP %d: malformed YJ1 stream: %w", id, err)
	}
	info.DecodedSize = len(decoded)
	if len(decoded) == 0 {
		info.Status = "black_empty_decode"
		return info, nil
	}
	if len(decoded) != 320*200 {
		return info, fmt.Errorf("FBP %d: decoded %d pixels, need 64000", id, len(decoded))
	}
	info.Status = "picture"
	sum := sha256.Sum256(decoded)
	info.SHA256 = fmt.Sprintf("%x", sum)
	return info, nil
}
