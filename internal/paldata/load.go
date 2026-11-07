// Package paldata provides bounded, read-only import of PAL1 script and text
// tables. It interprets an explicitly requested DOS or Windows object layout; it does not identify
// the release, execute scripts, decompress resources, or establish a game route.
package paldata

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/HiChen85/pal--reverse--learn/internal/pal"
	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/traditionalchinese"
)

const Profile = "pal1-win98-requested-windows-layout"
const ProfileDOS = "pal1-requested-dos-layout"
const (
	MaxArchiveBytes  = 64 << 20
	MaxMessageBytes  = 32 << 20
	MaxWordBytes     = 1 << 20
	MaxMessages      = 65536
	MaxScripts       = 65536
	MaxScenes        = 300
	MaxEvents        = 5500
	MaxObjects       = 600
	MaxMessageLength = 65536
	WordWidth        = 10
)

type Options struct {
	Encoding string
	// Layout selects object record width. Empty defaults to "windows" for
	// compatibility; "dos" must be explicit. This is not release detection.
	Layout string
}
type FileInfo struct {
	Name   string
	SHA256 string
	Size   int64
}
type Instruction struct {
	Opcode   uint16
	Operands [3]uint16
}
type Scene struct{ Map, OnEnter, OnTeleport, EventStart uint16 }
type Event struct {
	Raw                                                                                                                                                      [16]uint16
	VanishTime, Layer, State                                                                                                                                 int16
	X, Y, Trigger, AutoScript, TriggerMode, Sprite, SpriteFrames, Direction, CurrentFrame, ScriptIdleFrame, SpritePtrOffset, SpriteFramesAuto, AutoIdleFrame uint16
}
type ChunkInfo struct {
	Index int
	Size  int64
}
type Game struct {
	Layout         string
	ObjectWords    int
	RawObjects     [][]uint16
	Profile        string
	Encoding       string
	Files          []FileInfo
	Scripts        []Instruction
	Scenes         []Scene
	Events         []Event
	Objects        [][7]uint16
	Messages       []string
	Words          []string
	MessageOffsets []uint32
	RawMessages    [][]byte
	RawWords       [][]byte
	DataChunks     []ChunkInfo
	Warnings       []string
}

// Load imports exactly four required regular files with case-insensitive,
// unambiguous names. Encoding must explicitly select "big5" or "gbk"; no release
// or codepage is guessed. All opened resources are closed before returning.
func Load(dir string, options Options) (*Game, error) {
	var codepage encoding.Encoding
	switch options.Encoding {
	case "big5":
		codepage = traditionalchinese.Big5
	case "gbk":
		codepage = simplifiedchinese.GBK
	default:
		return nil, errors.New("paldata: Encoding must explicitly be big5 or gbk")
	}
	layout := options.Layout
	if layout == "" {
		layout = "windows"
	}
	profile, objectWords := Profile, 7
	switch layout {
	case "windows":
	case "dos":
		profile, objectWords = ProfileDOS, 6
	default:
		return nil, errors.New("paldata: Layout must be windows or dos (empty defaults to windows)")
	}
	names, err := requiredFiles(dir)
	if err != nil {
		return nil, err
	}
	g := &Game{Profile: profile, Encoding: options.Encoding, Layout: layout, ObjectWords: objectWords}
	payloads := make(map[string][]byte, 4)
	for _, name := range []string{"SSS.MKF", "DATA.MKF", "M.MSG", "WORD.DAT"} {
		limit := int64(MaxArchiveBytes)
		if name == "M.MSG" {
			limit = MaxMessageBytes
		}
		if name == "WORD.DAT" {
			limit = MaxWordBytes
		}
		data, err := readBounded(filepath.Join(dir, names[name]), limit)
		if err != nil {
			return nil, fmt.Errorf("paldata %s: %w", name, err)
		}
		payloads[name] = data
		digest := sha256.Sum256(data)
		g.Files = append(g.Files, FileInfo{Name: names[name], SHA256: fmt.Sprintf("%x", digest), Size: int64(len(data))})
	}
	sss, err := pal.Open(bytes.NewReader(payloads["SSS.MKF"]), int64(len(payloads["SSS.MKF"])))
	if err != nil {
		return nil, fmt.Errorf("SSS.MKF: %w", err)
	}
	if sss.Count() > 4096 {
		return nil, errors.New("SSS.MKF: exceeds 4096 chunk inventory limit")
	}
	if sss.Count() < 5 {
		return nil, errors.New("SSS.MKF: requires chunks 0 through 4")
	}
	var chunks [5][]byte
	for i := range chunks {
		chunks[i], err = sss.ReadChunk(i)
		if err != nil {
			return nil, fmt.Errorf("SSS.MKF chunk %d: %w", i, err)
		}
	}
	if err := readScripts(g, chunks[4]); err != nil {
		return nil, err
	}
	if err := readEvents(g, chunks[0]); err != nil {
		return nil, err
	}
	if err := readScenes(g, chunks[1]); err != nil {
		return nil, err
	}
	if err := readObjects(g, chunks[2]); err != nil {
		return nil, err
	}
	if err := readMessages(g, chunks[3], payloads["M.MSG"], codepage); err != nil {
		return nil, err
	}
	for i, script := range g.Scripts {
		if script.Opcode == 0xffff && int(script.Operands[0]) >= len(g.Messages) {
			g.Warnings = append(g.Warnings, fmt.Sprintf("script %d: 0xffff message index %d outside message table (%d); interpretation depends on trigger/auto context", i, script.Operands[0], len(g.Messages)))
		}
	}
	if err := readWords(g, payloads["WORD.DAT"], codepage); err != nil {
		return nil, err
	}
	dataMKF, err := pal.Open(bytes.NewReader(payloads["DATA.MKF"]), int64(len(payloads["DATA.MKF"])))
	if err != nil {
		return nil, fmt.Errorf("DATA.MKF: %w", err)
	}
	if dataMKF.Count() < 15 {
		return nil, fmt.Errorf("DATA.MKF: requires chunks 0 through 14 for requested %s layout", layout)
	}
	if dataMKF.Count() > 4096 {
		return nil, errors.New("DATA.MKF: exceeds 4096 chunk inventory limit")
	}
	for i := 0; i < dataMKF.Count(); i++ {
		size, _ := dataMKF.ChunkSize(i)
		g.DataChunks = append(g.DataChunks, ChunkInfo{Index: i, Size: size})
	}
	return g, nil
}

func requiredFiles(dir string) (map[string]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	result := map[string]string{}
	for _, entry := range entries {
		for _, wanted := range []string{"SSS.MKF", "DATA.MKF", "M.MSG", "WORD.DAT"} {
			if !strings.EqualFold(entry.Name(), wanted) {
				continue
			}
			if prior := result[wanted]; prior != "" {
				return nil, fmt.Errorf("paldata: ambiguous %s: %q and %q", wanted, prior, entry.Name())
			}
			info, err := entry.Info()
			if err != nil {
				return nil, err
			}
			if !info.Mode().IsRegular() {
				return nil, fmt.Errorf("paldata: %s must be a regular file (symlinks are not accepted)", entry.Name())
			}
			result[wanted] = entry.Name()
		}
	}
	for _, wanted := range []string{"SSS.MKF", "DATA.MKF", "M.MSG", "WORD.DAT"} {
		if result[wanted] == "" {
			return nil, fmt.Errorf("paldata: missing required file %s", wanted)
		}
	}
	return result, nil
}
func readBounded(path string, limit int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("requires regular file")
	}
	if info.Size() > limit {
		return nil, fmt.Errorf("file size %d exceeds limit %d", info.Size(), limit)
	}
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("file exceeds limit %d", limit)
	}
	if int64(len(data)) != info.Size() {
		return nil, errors.New("file changed size while importing")
	}
	return data, nil
}
func recordCount(data []byte, width, max int, kind string) (int, error) {
	if len(data)%width != 0 {
		return 0, fmt.Errorf("%s: truncated %d-byte record", kind, width)
	}
	count := len(data) / width
	if count < 1 || count > max {
		return 0, fmt.Errorf("%s: record count %d outside 1..%d", kind, count, max)
	}
	return count, nil
}
func word(data []byte, index int) uint16 { return binary.LittleEndian.Uint16(data[index*2:]) }
func scriptRef(g *Game, v uint16, where string) error {
	if int(v) >= len(g.Scripts) {
		return fmt.Errorf("%s: script index %d outside table (%d)", where, v, len(g.Scripts))
	}
	return nil
}
func readScripts(g *Game, data []byte) error {
	count, err := recordCount(data, 8, MaxScripts, "SSS chunk4 scripts")
	if err != nil {
		return err
	}
	g.Scripts = make([]Instruction, count)
	for i := range g.Scripts {
		record := data[i*8 : (i+1)*8]
		g.Scripts[i] = Instruction{Opcode: word(record, 0), Operands: [3]uint16{word(record, 1), word(record, 2), word(record, 3)}}
	}
	return nil
}
func readEvents(g *Game, data []byte) error {
	count, err := recordCount(data, 32, MaxEvents, "SSS chunk0 events")
	if err != nil {
		return err
	}
	g.Events = make([]Event, count)
	for i := range g.Events {
		r := data[i*32 : (i+1)*32]
		e := &g.Events[i]
		for j := range e.Raw {
			e.Raw[j] = word(r, j)
		}
		e.VanishTime = int16(e.Raw[0])
		e.X = e.Raw[1]
		e.Y = e.Raw[2]
		e.Layer = int16(e.Raw[3])
		e.Trigger = e.Raw[4]
		e.AutoScript = e.Raw[5]
		e.State = int16(e.Raw[6])
		e.TriggerMode = e.Raw[7]
		e.Sprite = e.Raw[8]
		e.SpriteFrames = e.Raw[9]
		e.Direction = e.Raw[10]
		e.CurrentFrame = e.Raw[11]
		e.ScriptIdleFrame = e.Raw[12]
		e.SpritePtrOffset = e.Raw[13]
		e.SpriteFramesAuto = e.Raw[14]
		e.AutoIdleFrame = e.Raw[15]
		if err := scriptRef(g, e.Trigger, fmt.Sprintf("event %d trigger", i+1)); err != nil {
			return err
		}
		if err := scriptRef(g, e.AutoScript, fmt.Sprintf("event %d auto", i+1)); err != nil {
			return err
		}
	}
	return nil
}
func readScenes(g *Game, data []byte) error {
	count, err := recordCount(data, 8, MaxScenes, "SSS chunk1 scenes")
	if err != nil {
		return err
	}
	g.Scenes = make([]Scene, count)
	for i := range g.Scenes {
		r := data[i*8 : (i+1)*8]
		scene := Scene{Map: word(r, 0), OnEnter: word(r, 1), OnTeleport: word(r, 2), EventStart: word(r, 3)}
		if int(scene.EventStart) > len(g.Events) {
			return fmt.Errorf("scene %d: event boundary %d outside event table (%d)", i+1, scene.EventStart, len(g.Events))
		}
		if err := scriptRef(g, scene.OnEnter, fmt.Sprintf("scene %d on-enter", i+1)); err != nil {
			return err
		}
		if err := scriptRef(g, scene.OnTeleport, fmt.Sprintf("scene %d teleport", i+1)); err != nil {
			return err
		}
		g.Scenes[i] = scene
	}
	return nil
}

// readObjects preserves the input words before normalizing the DOS object union
// into the seven-word Windows-shaped storage used by downstream inventory code.
// That normalization does not turn the source game into a Windows release.
func readObjects(g *Game, data []byte) error {
	width := g.ObjectWords * 2
	if g.ObjectWords != 6 && g.ObjectWords != 7 {
		return errors.New("invalid object record width")
	}
	count, err := recordCount(data, width, MaxObjects, "SSS chunk2 "+g.Layout+" objects")
	if err != nil {
		return err
	}
	g.Objects = make([][7]uint16, count)
	g.RawObjects = make([][]uint16, count)
	for i := range g.Objects {
		raw := make([]uint16, g.ObjectWords)
		for j := range raw {
			raw[j] = word(data[i*width:(i+1)*width], j)
		}
		g.RawObjects[i] = raw
		copy(g.Objects[i][:], raw)
		if g.Layout == "dos" {
			g.Objects[i][6] = raw[5]
			g.Objects[i][5] = 0
		}
	}
	return nil
}

func readMessages(g *Game, index, data []byte, codepage encoding.Encoding) error {
	if len(index)%4 != 0 || len(index) < 8 || len(index)/4 > MaxMessages+1 {
		return errors.New("SSS chunk3: invalid message offset count/alignment")
	}
	g.MessageOffsets = make([]uint32, len(index)/4)
	for i := range g.MessageOffsets {
		v := binary.LittleEndian.Uint32(index[i*4:])
		g.MessageOffsets[i] = v
		if uint64(v) > uint64(len(data)) {
			return fmt.Errorf("message offset %d is outside M.MSG", i)
		}
		if i > 0 && v < g.MessageOffsets[i-1] {
			return fmt.Errorf("message offset %d descends", i)
		}
	}
	if first := g.MessageOffsets[0]; first != 0 {
		g.Warnings = append(g.Warnings, fmt.Sprintf("M.MSG: %d unindexed prefix bytes", first))
	}
	if last := int(g.MessageOffsets[len(g.MessageOffsets)-1]); last != len(data) {
		g.Warnings = append(g.Warnings, fmt.Sprintf("M.MSG: %d unindexed trailing bytes", len(data)-last))
	}
	g.Messages = make([]string, len(g.MessageOffsets)-1)
	g.RawMessages = make([][]byte, len(g.Messages))
	for i := range g.Messages {
		raw := data[int(g.MessageOffsets[i]):int(g.MessageOffsets[i+1])]
		if len(raw) > MaxMessageLength {
			return fmt.Errorf("message %d exceeds %d-byte length limit", i, MaxMessageLength)
		}
		text, err := decodeText(raw, codepage)
		if err != nil {
			return fmt.Errorf("message %d: %w", i, err)
		}
		g.Messages[i] = text
		g.RawMessages[i] = bytes.Clone(raw)
	}
	return nil
}
func readWords(g *Game, data []byte, codepage encoding.Encoding) error {
	count := (len(data) + WordWidth - 1) / WordWidth
	if count < 1 || count > 65536 {
		return fmt.Errorf("WORD.DAT words: record count %d outside 1..65536", count)
	}
	g.Words = make([]string, count)
	g.RawWords = make([][]byte, count)
	for i := range g.Words {
		end := min((i+1)*WordWidth, len(data))
		raw := data[i*WordWidth : end]
		g.RawWords[i] = bytes.Clone(raw)
		// The engine pads the final partial slot. Preserve exact raw bytes separately.
		padded := make([]byte, WordWidth)
		copy(padded, raw)
		trimmed := bytes.TrimRight(padded, " \x00")
		text, err := decodeText(trimmed, codepage)
		if err != nil {
			return fmt.Errorf("word %d: %w", i, err)
		}
		g.Words[i] = text
	}
	return nil
}

func decodeText(raw []byte, codepage encoding.Encoding) (string, error) {
	decoded, err := codepage.NewDecoder().Bytes(raw)
	if err != nil {
		return "", err
	}
	if !utf8.Valid(decoded) || bytes.Contains(decoded, []byte("\xef\xbf\xbd")) {
		return "", errors.New("invalid or unmapped bytes for selected encoding; raw input was not silently replaced")
	}
	return string(decoded), nil
}
