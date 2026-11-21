package apkinspect

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type memberSpec struct {
	name, data string
	mode       os.FileMode
}

func archive(t *testing.T, entries ...memberSpec) []byte {
	t.Helper()
	var out bytes.Buffer
	w := zip.NewWriter(&out)
	for _, entry := range entries {
		header := &zip.FileHeader{Name: entry.name, Method: zip.Store}
		if entry.mode != 0 {
			header.SetMode(entry.mode)
		}
		f, err := w.CreateHeader(header)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = f.Write([]byte(entry.data)); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}
func inspectBytes(t *testing.T, data []byte) (Report, error) {
	t.Helper()
	filename := filepath.Join(t.TempDir(), "sample.apk")
	if err := os.WriteFile(filename, data, 0600); err != nil {
		t.Fatal(err)
	}
	return Inspect(filename)
}
func centralOffset(t *testing.T, data []byte) int {
	t.Helper()
	offset := bytes.Index(data, []byte{0x50, 0x4b, 0x01, 0x02})
	if offset < 0 {
		t.Fatal("missing central header")
	}
	return offset
}
func patch(t *testing.T, data []byte, offset int, value uint32) []byte {
	t.Helper()
	copyData := append([]byte(nil), data...)
	binary.LittleEndian.PutUint32(copyData[offset:offset+4], value)
	return copyData
}
func requireError(t *testing.T, data []byte, substring string) {
	t.Helper()
	_, err := inspectBytes(t, data)
	if err == nil || !strings.Contains(err.Error(), substring) {
		t.Fatalf("expected %q error, received %v", substring, err)
	}
}
func TestMetadataAndHashDoNotValidateFilenameCandidates(t *testing.T) {
	data := archive(t, memberSpec{name: "AndroidManifest.xml", data: "not parsed"}, memberSpec{name: "classes.dex", data: "not executed"}, memberSpec{name: "classes2.dex", data: "not executed"}, memberSpec{name: "lib/arm64-v8a/libcocos2dcpp.so", data: "not loaded"}, memberSpec{name: "assets/sss.mkf", data: "not an MKF"}, memberSpec{name: "assets/DATA.MKF", data: "not an MKF"}, memberSpec{name: "assets/M.MSG", data: "unknown"}, memberSpec{name: "assets/WORD.DAT", data: "unknown"})
	report, err := inspectBytes(t, data)
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(data)
	if report.SHA256 != hex.EncodeToString(hash[:]) || report.ArchiveBytes != int64(len(data)) || report.Members != 8 || !report.HasManifest || !report.HasDEX {
		t.Fatalf("bad archive inventory: %+v", report)
	}
	if !report.NameComplete || report.WindowsDataValidated || len(report.MissingDataFiles) != 0 {
		t.Fatal("filename presence was treated as content validation")
	}
	if len(report.NativeLibraries) != 1 || report.CandidateDataFiles["SSS.MKF"][0] != "assets/sss.mkf" {
		t.Fatal("case-insensitive basename candidate or native path missing")
	}
	if report.CompressedBytes != report.UncompressedBytes || len(report.Warnings) < 2 {
		t.Fatal("metadata-only coverage missing")
	}
}
func TestMissingDatasetAndExactBasenameMatching(t *testing.T) {
	data := archive(t, memberSpec{name: "assets/SSS.MKF.backup", data: "x"}, memberSpec{name: "assets/nestedclasses.dex", data: "x"}, memberSpec{name: "assets/AndroidManifest.xml", data: "x"}, memberSpec{name: "classes0.dex", data: "x"})
	report, err := inspectBytes(t, data)
	if err != nil {
		t.Fatal(err)
	}
	if report.NameComplete || report.WindowsDataValidated || report.HasManifest || report.HasDEX || len(report.MissingDataFiles) != 4 || len(report.CandidateDataFiles) != 0 {
		t.Fatalf("loose matching marked dataset present: %+v", report)
	}
}
func TestAmbiguousBasenameCandidatesAreRetained(t *testing.T) {
	data := archive(t, memberSpec{name: "a/SSS.MKF", data: "a"}, memberSpec{name: "b/sss.mkf", data: "b"})
	report, err := inspectBytes(t, data)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.CandidateDataFiles["SSS.MKF"]) != 2 || !strings.Contains(strings.Join(report.Warnings, " "), "Multiple SSS.MKF") {
		t.Fatal("ambiguous basename candidates were hidden")
	}
}
func TestMalformedAndLFSPointer(t *testing.T) {
	requireError(t, []byte("version https://git-lfs.github.com/spec/v1\noid sha256:abc\nsize 100\n"), "Git LFS pointer")
	requireError(t, []byte("not a zip"), "too small")
	requireError(t, bytes.Repeat([]byte{'x'}, 100), "end-of-central-directory")
	data := archive(t, memberSpec{name: "asset", data: "x"})
	requireError(t, data[:len(data)-3], "end-of-central-directory")
	central := centralOffset(t, data)
	broken := patch(t, data, central, 0x12345678)
	requireError(t, broken, "signature")
	eocd := len(data) - 22
	broken = append([]byte(nil), data...)
	binary.LittleEndian.PutUint16(broken[eocd+8:eocd+10], 2)
	binary.LittleEndian.PutUint16(broken[eocd+10:eocd+12], 2)
	requireError(t, broken, "count")
}
func TestUnsafeDuplicateAndSpecialNames(t *testing.T) {
	for _, name := range []string{"../escape", "/absolute", "C:/drive", "a\\b", "a/../b", "a//b", "./dot", "a\x00b", "a\nb"} {
		t.Run(name, func(t *testing.T) { requireError(t, archive(t, memberSpec{name: name, data: "x"}), "archive member") })
	}
	for _, pair := range [][]string{{"same", "same"}, {"Same", "same"}, {"a/", "a"}} {
		requireError(t, archive(t, memberSpec{name: pair[0]}, memberSpec{name: pair[1]}), "duplicate")
	}
	requireError(t, archive(t, memberSpec{name: "target", data: "x"}, memberSpec{name: "target/child", data: "x"}), "path collision")
	requireError(t, archive(t, memberSpec{name: "target/child", data: "x"}, memberSpec{name: "target", data: "x"}), "path collision")
	requireError(t, archive(t, memberSpec{name: "link", data: "target", mode: os.ModeSymlink | 0777}), "symlink")
}
func TestEncryptedAndDeclaredSizeBounds(t *testing.T) {
	data := archive(t, memberSpec{name: "asset", data: "x"})
	central := centralOffset(t, data)
	encrypted := append([]byte(nil), data...)
	binary.LittleEndian.PutUint16(encrypted[central+8:central+10], 1)
	requireError(t, encrypted, "encrypted")
	requireError(t, patch(t, data, central+24, uint32(MaxMemberBytes+1)), "uncompressed limit")
	requireError(t, patch(t, data, central+24, 1001), "compression ratio")
	requireError(t, patch(t, data, central+20, uint32(len(data)+1)), "outside archive")
	// Metadata-only inspection does not decompress or validate CRC/actual size.
	exact := patch(t, data, central+24, 1000)
	report, err := inspectBytes(t, exact)
	if err != nil || report.UncompressedBytes != 1000 || report.WindowsDataValidated {
		t.Fatalf("exact ratio bound: %+v %v", report, err)
	}
}
func TestCentralDirectoryAndTotalLimits(t *testing.T) {
	entries := make([]memberSpec, 17)
	for i := range entries {
		entries[i] = memberSpec{name: strings.Repeat("a", i+1), data: strings.Repeat("x", 1<<20)}
	}
	data := archive(t, entries...)
	cursor := centralOffset(t, data)
	for range entries {
		binary.LittleEndian.PutUint32(data[cursor+20:cursor+24], 1<<20)
		binary.LittleEndian.PutUint32(data[cursor+24:cursor+28], uint32(MaxMemberBytes))
		size := 46 + int(binary.LittleEndian.Uint16(data[cursor+28:cursor+30])) + int(binary.LittleEndian.Uint16(data[cursor+30:cursor+32])) + int(binary.LittleEndian.Uint16(data[cursor+32:cursor+34]))
		cursor += size
	}
	requireError(t, data, "total uncompressed limit")
	// Use preflight directly for a count-over-limit regression without allocating
	// that number of archive/zip File records. A ZIP64 end supplies the count.
	var zip64 bytes.Buffer
	zip64.Write(make([]byte, 46))
	record := make([]byte, 56)
	binary.LittleEndian.PutUint32(record[:4], 0x06064b50)
	binary.LittleEndian.PutUint64(record[4:12], 44)
	binary.LittleEndian.PutUint64(record[24:32], MaxMembers+1)
	binary.LittleEndian.PutUint64(record[32:40], MaxMembers+1)
	zip64.Write(record)
	locator := make([]byte, 20)
	binary.LittleEndian.PutUint32(locator[:4], 0x07064b50)
	binary.LittleEndian.PutUint64(locator[8:16], 46)
	binary.LittleEndian.PutUint32(locator[16:20], 1)
	zip64.Write(locator)
	end := make([]byte, 22)
	binary.LittleEndian.PutUint32(end[:4], 0x06054b50)
	binary.LittleEndian.PutUint16(end[8:10], 0xffff)
	binary.LittleEndian.PutUint16(end[10:12], 0xffff)
	zip64.Write(end)
	_, err := preflight(bytes.NewReader(zip64.Bytes()), int64(zip64.Len()))
	if err == nil || !strings.Contains(err.Error(), "member limit") {
		t.Fatalf("ZIP64 count was not bounded: %v", err)
	}
}

func TestArchiveFileSizeIsBoundedBeforeReading(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "oversized.apk")
	f, err := os.Create(filename)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.Truncate(MaxArchiveBytes + 1); err != nil {
		f.Close()
		t.Fatal(err)
	}
	if err = f.Close(); err != nil {
		t.Fatal(err)
	}
	_, err = Inspect(filename)
	if err == nil || !strings.Contains(err.Error(), "compressed-file limit") {
		t.Fatalf("oversized archive was not rejected: %v", err)
	}
}

func TestPreflightBoundsDirectoryAndNameBeforeZIPAllocation(t *testing.T) {
	// The declaration is enough to reject the archive: preflight must not allocate
	// a directory-sized buffer or pass the unbounded span to archive/zip.
	end := make([]byte, 22)
	binary.LittleEndian.PutUint32(end[:4], 0x06054b50)
	binary.LittleEndian.PutUint32(end[12:16], uint32(MaxCentralDirectoryBytes+1))
	_, err := preflight(bytes.NewReader(end), int64(len(end)))
	if err == nil || !strings.Contains(err.Error(), "central directory exceeds") {
		t.Fatalf("directory allocation bound missing: %v", err)
	}
	data := archive(t, memberSpec{name: "asset", data: "x"})
	offset := centralOffset(t, data)
	binary.LittleEndian.PutUint16(data[offset+28:offset+30], MaxMemberNameBytes+1)
	_, err = preflight(bytes.NewReader(data), int64(len(data)))
	if err == nil || !strings.Contains(err.Error(), "member name exceeds") {
		t.Fatalf("name allocation bound missing: %v", err)
	}
}
