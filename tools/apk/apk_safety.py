"""Shared input bounds and atomic JSON output for offline APK helpers."""
from contextlib import contextmanager
import os
from pathlib import Path
import stat
import struct
import tempfile
import zipfile

MAX_APK_BYTES = 1 << 30
MAX_DIRECTORY_BYTES = 64 << 20
MAX_ENTRIES = 100_000
EOCD = b'PK\x05\x06'


def preflight_zip(reader):
    """Validate bounded, single-disk non-ZIP64 directory before ZipFile allocates."""
    info = os.fstat(reader.fileno())
    if not stat.S_ISREG(info.st_mode):
        raise ValueError('APK input must be a regular file')
    size = info.st_size
    if size < 22 or size > MAX_APK_BYTES:
        raise ValueError('APK file size outside supported range (22 bytes to 1 GiB)')
    tail_size = min(size, 22 + 65535)
    reader.seek(size - tail_size)
    tail = reader.read(tail_size)
    position = tail.rfind(EOCD)
    if position < 0 or position + 22 > len(tail):
        raise ValueError('missing or truncated ZIP end record')
    fields = struct.unpack_from('<4sHHHHIIH', tail, position)
    _, disk, directory_disk, disk_count, count, directory_size, directory_offset, comment = fields
    if position + 22 + comment != len(tail):
        raise ValueError('unsupported trailing data or ambiguous ZIP end record')
    eocd_offset = size - tail_size + position
    if (count == 0xffff or disk_count == 0xffff
            or directory_size == 0xffffffff or directory_offset == 0xffffffff
            or (position >= 20 and tail[position - 20:position - 16] == b'PK\x06\x07')):
        raise ValueError('ZIP64 is unsupported by Python identity helpers')
    if disk or directory_disk or disk_count != count:
        raise ValueError('multi-disk ZIP is unsupported')
    if directory_size > MAX_DIRECTORY_BYTES:
        raise ValueError('ZIP central directory exceeds 64 MiB limit')
    if count > MAX_ENTRIES:
        raise ValueError('ZIP entry count exceeds 100000 limit')
    directory_end = directory_offset + directory_size
    if directory_end != eocd_offset:
        raise ValueError('invalid or unsupported ZIP central directory extent')
    # Do not trust EOCD count alone: Python parses every record in its declared
    # byte span. Count records before allowing it to allocate all ZipInfo objects.
    reader.seek(directory_offset)
    cursor = directory_offset
    actual_count = 0
    while cursor < directory_end:
        if cursor + 46 > directory_end:
            raise ValueError('truncated ZIP central directory header')
        header = reader.read(46)
        if len(header) != 46 or header[:4] != b'PK\x01\x02':
            raise ValueError('invalid ZIP central directory record')
        name, extra, record_comment, start_disk = struct.unpack_from('<HHHH', header, 28)
        if start_disk:
            raise ValueError('multi-disk or ZIP64 central record is unsupported')
        if any(struct.unpack_from('<I', header, offset)[0] == 0xffffffff
               for offset in (20, 24, 42)):
            raise ValueError('ZIP64 central record is unsupported')
        cursor += 46 + name + extra + record_comment
        if cursor > directory_end:
            raise ValueError('ZIP central record exceeds directory bounds')
        actual_count += 1
        if actual_count > MAX_ENTRIES:
            raise ValueError('ZIP entry count exceeds 100000 limit')
        reader.seek(cursor)
    if actual_count != count:
        raise ValueError('ZIP central directory entry count mismatch')
    reader.seek(0)


@contextmanager
def open_bounded_zip(apk):
    path = Path(apk)
    if not stat.S_ISREG(path.stat().st_mode):
        raise ValueError('APK input must be a regular file')
    # Preserve this descriptor through preflight and inspection.
    with path.open('rb') as reader:
        preflight_zip(reader)
        with zipfile.ZipFile(reader) as archive:
            yield archive


def _validate_output(apk, output):
    source = Path(apk)
    destination = Path(output)
    if destination.suffix.lower() != '.json':
        raise ValueError('output must have a .json extension')
    if source.resolve() == destination.resolve():
        raise ValueError('output must not overwrite the APK input')
    if destination.exists() and os.path.samefile(source, destination):
        raise ValueError('output must not alias the APK input')
    return destination


def write_json_output(apk, output, encoded):
    """Write caller-selected JSON atomically; reject input aliases and non-JSON."""
    destination = _validate_output(apk, output)
    temporary = None
    try:
        with tempfile.NamedTemporaryFile(mode='w', encoding='utf-8',
                                         dir=destination.parent,
                                         prefix='.' + destination.name + '.',
                                         suffix='.tmp', delete=False) as stream:
            temporary = Path(stream.name)
            stream.write(encoded)
            stream.flush()
            os.fsync(stream.fileno())
        _validate_output(apk, destination)
        os.replace(temporary, destination)
        temporary = None
    finally:
        if temporary is not None:
            temporary.unlink(missing_ok=True)
