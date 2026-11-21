// Package apkinspect inspects ZIP/APK metadata without extracting members,
// decompressing assets, or executing application code. Filename matches do not
// identify a PAL release or validate a Windows PAL dataset.
package apkinspect

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path"
	"sort"
	"strings"
	"unicode/utf8"
)

const MaxArchiveBytes int64 = 1 << 30
const MaxMembers = 100000
const MaxCentralDirectoryBytes uint64 = 64 << 20
const MaxMemberNameBytes = 4096
const MaxMemberBytes uint64 = 128 << 20
const MaxUncompressedBytes uint64 = 2 << 30
const MaxCompressionRatio uint64 = 1000

var requiredDataNames = []string{"SSS.MKF", "DATA.MKF", "M.MSG", "WORD.DAT"}

type Report struct {
	SHA256               string              `json:"sha256"`
	ArchiveBytes         int64               `json:"archive_bytes"`
	Members              int                 `json:"members"`
	CompressedBytes      uint64              `json:"compressed_bytes"`
	UncompressedBytes    uint64              `json:"uncompressed_bytes"`
	HasManifest          bool                `json:"has_manifest"`
	HasDEX               bool                `json:"has_dex"`
	NativeLibraries      []string            `json:"native_libraries"`
	CandidateDataFiles   map[string][]string `json:"candidate_data_files"`
	NameComplete         bool                `json:"name_complete"`
	WindowsDataValidated bool                `json:"windows_data_validated"`
	MissingDataFiles     []string            `json:"missing_data_files"`
	Warnings             []string            `json:"warnings"`
}

// Inspect reads the archive hash and central-directory metadata. The resource
// bounds are checked before opening compressed data, and no member is opened.
// WindowsDataValidated is always false: only a data importer can validate the
// four files' contents and version/layout; the names alone are not evidence.
func Inspect(filename string) (Report, error) {
	report := Report{CandidateDataFiles: map[string][]string{}, Warnings: []string{
		"Metadata only: no archive members were extracted, decompressed or executed.",
		"Dataset filename presence does not identify a game release or validate Windows PAL data.",
	}}
	f, err := os.Open(filename)
	if err != nil {
		return report, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return report, err
	}
	if !info.Mode().IsRegular() {
		return report, errors.New("APK input must be a regular file")
	}
	report.ArchiveBytes = info.Size()
	if info.Size() > MaxArchiveBytes {
		return report, fmt.Errorf("archive exceeds %d-byte compressed-file limit", MaxArchiveBytes)
	}
	prefix := make([]byte, min(int64(1024), info.Size()))
	if len(prefix) > 0 {
		if _, err := f.ReadAt(prefix, 0); err != nil {
			return report, err
		}
	}
	if strings.HasPrefix(string(prefix), "version https://git-lfs.github.com/spec/v1\n") || strings.HasPrefix(string(prefix), "version https://git-lfs.github.com/spec/v1\r\n") {
		return report, errors.New("input is a Git LFS pointer, not the APK bytes; fetch the LFS object before inspecting")
	}
	declaredMembers, err := preflight(f, info.Size())
	if err != nil {
		return report, err
	}
	reader, err := zip.NewReader(f, info.Size())
	if err != nil {
		return report, fmt.Errorf("read ZIP central directory: %w", err)
	}
	if len(reader.File) > MaxMembers || uint64(len(reader.File)) != declaredMembers {
		return report, errors.New("archive member count exceeds limit or disagrees with central directory")
	}
	seen := make(map[string]bool, len(reader.File))
	filePaths := make(map[string]bool, len(reader.File))
	implicitDirectories := make(map[string]bool)
	for _, member := range reader.File {
		name := member.Name
		if err := validateName(name); err != nil {
			return report, err
		}
		// Case-folding catches ambiguous paths before any later Windows extraction.
		key := strings.ToLower(strings.TrimSuffix(name, "/"))
		if seen[key] {
			return report, fmt.Errorf("duplicate or case-ambiguous archive member %q", name)
		}
		seen[key] = true
		mode := member.Mode()
		if !mode.IsDir() && implicitDirectories[key] {
			return report, fmt.Errorf("file/directory path collision at %q", name)
		}
		for ancestor := path.Dir(key); ancestor != "."; ancestor = path.Dir(ancestor) {
			if filePaths[ancestor] {
				return report, fmt.Errorf("file/directory path collision at %q", name)
			}
			implicitDirectories[ancestor] = true
		}
		if !mode.IsDir() {
			filePaths[key] = true
		}
		if mode&os.ModeSymlink != 0 {
			return report, fmt.Errorf("symlink archive member %q is not accepted", name)
		}
		if mode&os.ModeType != 0 && !mode.IsDir() {
			return report, fmt.Errorf("special archive member %q is not accepted", name)
		}
		if member.Flags&1 != 0 || member.Flags&(1<<6) != 0 {
			return report, fmt.Errorf("encrypted archive member %q is not accepted", name)
		}
		dataOffset, err := member.DataOffset()
		if err != nil {
			return report, fmt.Errorf("member %q local header: %w", name, err)
		}
		if dataOffset < 0 || dataOffset > info.Size() || member.CompressedSize64 > uint64(info.Size()-dataOffset) {
			return report, fmt.Errorf("member %q data extent is outside archive", name)
		}
		if member.UncompressedSize64 > MaxMemberBytes {
			return report, fmt.Errorf("member %q exceeds %d-byte uncompressed limit", name, MaxMemberBytes)
		}
		if member.CompressedSize64 > uint64(info.Size()) {
			return report, fmt.Errorf("member %q compressed size is outside archive", name)
		}
		if member.UncompressedSize64 > 0 && (member.CompressedSize64 == 0 || (member.UncompressedSize64-1)/member.CompressedSize64 >= MaxCompressionRatio) {
			return report, fmt.Errorf("member %q exceeds compression ratio limit %d", name, MaxCompressionRatio)
		}
		if report.UncompressedBytes > MaxUncompressedBytes-member.UncompressedSize64 {
			return report, fmt.Errorf("archive exceeds %d-byte total uncompressed limit", MaxUncompressedBytes)
		}
		if math.MaxUint64-report.CompressedBytes < member.CompressedSize64 {
			return report, errors.New("compressed size sum overflows")
		}
		report.UncompressedBytes += member.UncompressedSize64
		report.CompressedBytes += member.CompressedSize64
		if report.CompressedBytes > uint64(info.Size()) {
			return report, errors.New("member compressed-size sum exceeds archive size")
		}
		report.Members++
		if mode.IsDir() {
			continue
		}
		if strings.EqualFold(name, "AndroidManifest.xml") {
			report.HasManifest = true
		}
		base := path.Base(name)
		if !strings.Contains(name, "/") && isDEX(base) {
			report.HasDEX = true
		}
		parts := strings.Split(name, "/")
		if len(parts) == 3 && parts[0] == "lib" && parts[1] != "" && strings.HasSuffix(parts[2], ".so") {
			report.NativeLibraries = append(report.NativeLibraries, name)
		}
		for _, required := range requiredDataNames {
			if strings.EqualFold(base, required) {
				report.CandidateDataFiles[required] = append(report.CandidateDataFiles[required], name)
			}
		}
	}
	report.NameComplete = true
	for _, required := range requiredDataNames {
		if len(report.CandidateDataFiles[required]) == 0 {
			report.MissingDataFiles = append(report.MissingDataFiles, required)
			report.NameComplete = false
		}
		if len(report.CandidateDataFiles[required]) > 1 {
			report.Warnings = append(report.Warnings, fmt.Sprintf("Multiple %s filename candidates exist; no candidate contents were validated.", required))
		}
		sort.Strings(report.CandidateDataFiles[required])
	}
	sort.Strings(report.NativeLibraries)
	hash := sha256.New()
	if _, err := io.Copy(hash, io.NewSectionReader(f, 0, info.Size())); err != nil {
		return report, err
	}
	report.SHA256 = hex.EncodeToString(hash.Sum(nil))
	return report, nil
}
func isDEX(base string) bool {
	if base == "classes.dex" {
		return true
	}
	if !strings.HasPrefix(base, "classes") || !strings.HasSuffix(base, ".dex") {
		return false
	}
	digits := strings.TrimSuffix(strings.TrimPrefix(base, "classes"), ".dex")
	if digits == "" || digits[0] == '0' {
		return false
	}
	for _, digit := range digits {
		if digit < '0' || digit > '9' {
			return false
		}
	}
	return true
}
func validateName(name string) error {
	if len(name) > MaxMemberNameBytes {
		return fmt.Errorf("archive member name exceeds %d-byte limit", MaxMemberNameBytes)
	}
	if name == "" || !utf8.ValidString(name) || strings.ContainsAny(name, "\\\x00") || strings.HasPrefix(name, "/") {
		return fmt.Errorf("unsafe archive member name %q", name)
	}
	clean := strings.TrimSuffix(name, "/")
	for _, part := range strings.Split(clean, "/") {
		if part == "" || part == "." || part == ".." || strings.Contains(part, ":") {
			return fmt.Errorf("unsafe archive member name %q", name)
		}
	}
	for _, r := range name {
		if r < 0x20 || r == 0x7f {
			return fmt.Errorf("control character in archive member %q", name)
		}
	}
	return nil
}

// preflight bounds the number and byte span of central-directory records before
// archive/zip allocates its File slice. APK signatures before the directory do
// not affect its ZIP offsets. Multi-disk archives are intentionally unsupported.
func preflight(reader io.ReaderAt, size int64) (uint64, error) {
	if size < 22 {
		return 0, errors.New("input is too small to be a ZIP/APK archive")
	}
	tailSize := min(size, int64(22+65535))
	tail := make([]byte, int(tailSize))
	if _, err := reader.ReadAt(tail, size-tailSize); err != nil {
		return 0, err
	}
	eocd := -1
	for i := len(tail) - 22; i >= 0; i-- {
		if binary.LittleEndian.Uint32(tail[i:i+4]) == 0x06054b50 && i+22+int(binary.LittleEndian.Uint16(tail[i+20:i+22])) == len(tail) {
			eocd = i
			break
		}
	}
	if eocd < 0 {
		return 0, errors.New("ZIP end-of-central-directory record was not found")
	}
	end := tail[eocd:]
	eocdOffset := size - tailSize + int64(eocd)
	if binary.LittleEndian.Uint16(end[4:6]) != 0 || binary.LittleEndian.Uint16(end[6:8]) != 0 {
		return 0, errors.New("multi-disk ZIP archives are not accepted")
	}
	onDisk := uint64(binary.LittleEndian.Uint16(end[8:10]))
	count := uint64(binary.LittleEndian.Uint16(end[10:12]))
	directorySize := uint64(binary.LittleEndian.Uint32(end[12:16]))
	directoryOffset := uint64(binary.LittleEndian.Uint32(end[16:20]))
	directoryEnd := uint64(eocdOffset)
	if count == 0xffff || onDisk == 0xffff || directorySize == 0xffffffff || directoryOffset == 0xffffffff {
		if eocdOffset < 20 {
			return 0, errors.New("missing ZIP64 locator")
		}
		locator := make([]byte, 20)
		if _, err := reader.ReadAt(locator, eocdOffset-20); err != nil {
			return 0, err
		}
		if binary.LittleEndian.Uint32(locator[0:4]) != 0x07064b50 || binary.LittleEndian.Uint32(locator[4:8]) != 0 || binary.LittleEndian.Uint32(locator[16:20]) != 1 {
			return 0, errors.New("invalid or multi-disk ZIP64 locator")
		}
		zip64Offset := binary.LittleEndian.Uint64(locator[8:16])
		if zip64Offset > uint64(eocdOffset-20) || uint64(eocdOffset-20)-zip64Offset < 56 {
			return 0, errors.New("ZIP64 end record is out of bounds")
		}
		record := make([]byte, 56)
		if _, err := reader.ReadAt(record, int64(zip64Offset)); err != nil {
			return 0, err
		}
		if binary.LittleEndian.Uint32(record[0:4]) != 0x06064b50 || binary.LittleEndian.Uint64(record[4:12]) < 44 || binary.LittleEndian.Uint32(record[16:20]) != 0 || binary.LittleEndian.Uint32(record[20:24]) != 0 {
			return 0, errors.New("invalid ZIP64 end record")
		}
		extraSize := binary.LittleEndian.Uint64(record[4:12])
		if extraSize > uint64(eocdOffset-20)-zip64Offset-12 {
			return 0, errors.New("ZIP64 end record length is out of bounds")
		}
		onDisk = binary.LittleEndian.Uint64(record[24:32])
		count = binary.LittleEndian.Uint64(record[32:40])
		directorySize = binary.LittleEndian.Uint64(record[40:48])
		directoryOffset = binary.LittleEndian.Uint64(record[48:56])
		directoryEnd = zip64Offset
	}
	if directorySize > MaxCentralDirectoryBytes {
		return 0, fmt.Errorf("ZIP central directory exceeds %d-byte limit", MaxCentralDirectoryBytes)
	}
	if count != onDisk {
		return 0, errors.New("ZIP per-disk member count disagrees with total")
	}
	if count > MaxMembers {
		return 0, fmt.Errorf("archive exceeds %d-member limit", MaxMembers)
	}
	if directoryOffset > directoryEnd || directorySize > directoryEnd-directoryOffset {
		return 0, errors.New("ZIP central directory is out of bounds")
	}
	if count > directorySize/46 {
		return 0, errors.New("ZIP member count exceeds directory capacity")
	}
	cursor := directoryOffset
	limit := directoryOffset + directorySize
	header := make([]byte, 46)
	for index := uint64(0); index < count; index++ {
		if cursor > limit || limit-cursor < 46 {
			return 0, errors.New("truncated ZIP central directory")
		}
		if _, err := reader.ReadAt(header, int64(cursor)); err != nil {
			return 0, err
		}
		if binary.LittleEndian.Uint32(header[0:4]) != 0x02014b50 {
			return 0, errors.New("invalid central-directory member signature")
		}
		if binary.LittleEndian.Uint16(header[28:30]) > MaxMemberNameBytes {
			return 0, fmt.Errorf("archive member name exceeds %d-byte limit", MaxMemberNameBytes)
		}
		extra := uint64(binary.LittleEndian.Uint16(header[28:30])) + uint64(binary.LittleEndian.Uint16(header[30:32])) + uint64(binary.LittleEndian.Uint16(header[32:34]))
		if extra > limit-cursor-46 {
			return 0, errors.New("central-directory member extends beyond directory")
		}
		cursor += 46 + extra
	}
	if cursor != limit {
		return 0, errors.New("ZIP directory size contains uncounted member records or trailing data")
	}
	return count, nil
}
