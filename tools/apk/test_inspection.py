"""Synthetic regression tests for the limited offline identity helpers."""
import json
import os
import subprocess
import sys
from pathlib import Path
import struct
import tempfile
import unittest
from unittest import mock
import zipfile

import apk_safety
import inspect_manifest as manifest
import inspect_dex_identity as dex


def pool(strings):
    data = bytearray()
    offsets = []
    for text in strings:
        raw = text.encode('utf-8')
        if len(raw) >= 128 or len(text) >= 128:
            raise ValueError('test helper supports short strings')
        offsets.append(len(data))
        data += bytes([len(text), len(raw)]) + raw + b'\0'
    while len(data) % 4:
        data += b'\0'
    start = 28 + 4 * len(strings)
    header = struct.pack('<HHIIIIII', 1, 28, start + len(data), len(strings), 0, 256, start, 0)
    return header + b''.join(struct.pack('<I', x) for x in offsets) + data


def axml():
    strings = ['http://schemas.android.com/apk/res/android', 'manifest', 'package',
               'example.app', 'uses-sdk', 'minSdkVersion', 'targetSdkVersion',
               'application', 'name', 'example.App', 'label', 'Example',
               'meta-data', 'api_key', 'SENSITIVE_METADATA_VALUE', 'value']
    index = {s: i for i, s in enumerate(strings)}

    def start(name, attributes):
        attrs = b''
        for attr, value, android, typ in attributes:
            data = index[value] if typ == 3 else value
            attrs += struct.pack('<IIIHBBI', 0 if android else 0xffffffff,
                                 index[attr], 0xffffffff, 8, 0, typ, data)
        size = 16 + 20 + len(attrs)
        return (struct.pack('<HHIII', 0x102, 16, size, 1, 0xffffffff)
                + struct.pack('<IIHHHHHH', 0xffffffff, index[name], 20, 20,
                              len(attributes), 0, 0, 0) + attrs)

    def end(name):
        return (struct.pack('<HHIII', 0x103, 16, 24, 1, 0xffffffff)
                + struct.pack('<II', 0xffffffff, index[name]))

    content = (pool(strings)
               + start('manifest', [('package', 'example.app', False, 3)])
               + start('uses-sdk', [('targetSdkVersion', 26, True, 16),
                                    ('minSdkVersion', 1, True, 16)])
               + end('uses-sdk')
               + start('application', [('label', 'Example', True, 3),
                                       ('name', 'example.App', True, 3)])
               + start('meta-data', [('name', 'api_key', True, 3),
                                    ('value', 'SENSITIVE_METADATA_VALUE', True, 3)])
               + end('meta-data') + end('application') + end('manifest'))
    return struct.pack('<HHI', 3, 8, len(content) + 8) + content


class IdentityTests(unittest.TestCase):
    def test_name_lookup_and_safe_projection(self):
        # Attribute order is deliberately target then min; no positional guess.
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / 'sample.apk'
            with zipfile.ZipFile(path, 'w') as archive:
                archive.writestr('AndroidManifest.xml', axml())
            summary = manifest.inspect(path)
        self.assertEqual(summary['package'], 'example.app')
        self.assertEqual(summary['uses_sdk'], [{'android:minSdkVersion': 1,
                                               'android:targetSdkVersion': 26}])
        self.assertEqual(summary['application_label'], 'Example')
        self.assertNotIn('SENSITIVE_METADATA_VALUE', json.dumps(summary))
        self.assertNotIn('api_key', json.dumps(summary))

    def test_reject_invalid_chunk_extent(self):
        data = bytearray(axml())
        struct.pack_into('<I', data, 4, len(data) + 10)
        with self.assertRaises(ValueError):
            manifest.parse_manifest(data)

    def test_duplicate_manifest_rejected(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / 'sample.apk'
            with zipfile.ZipFile(path, 'w') as archive:
                archive.writestr('AndroidManifest.xml', axml())
                with self.assertWarns(UserWarning):
                    archive.writestr('AndroidManifest.xml', axml())
            with self.assertRaises(ValueError):
                manifest.inspect(path)

    def test_simple_arsc_label(self):
        global_pool = pool(['示例应用'])
        config = struct.pack('<I', 64) + bytes(60)
        entry = struct.pack('<HHIHBBI', 8, 0, 0, 8, 0, 3, 0)
        offsets = struct.pack('<IIIII', *([0xffffffff] * 4 + [0]))
        type_chunk = (struct.pack('<HHIBBHII', 0x201, 84, 104 + len(entry),
                                  9, 0, 0, 5, 104) + config + offsets + entry)
        package_header = bytearray(288)
        struct.pack_into('<HHII', package_header, 0, 0x200, 288,
                         288 + len(type_chunk), 0x7f)
        content = global_pool + package_header + type_chunk
        arsc = struct.pack('<HHII', 2, 12, 12 + len(content), 1) + content
        result = manifest.resolve_label(arsc, '@0x7f090004')
        self.assertEqual(result[0]['value'], '示例应用')
        self.assertEqual(manifest.resolve_label(arsc, '@0x7f090003'), [])

    def test_dex_rejects_short_or_wrong_magic(self):
        for data in (b'', b'not dex', b'dex\n035\0' + bytes(104)):
            with self.assertRaises(ValueError):
                dex.dex_info(data)


class SafetyTests(unittest.TestCase):
    def test_output_rejects_same_path_symlink_and_hardlink(self):
        with tempfile.TemporaryDirectory() as directory:
            source = Path(directory) / 'source.json'
            source.write_bytes(b'PRESERVE_INPUT')
            hardlink = Path(directory) / 'hardlink.json'
            symlink = Path(directory) / 'symlink.json'
            os.link(source, hardlink)
            symlink.symlink_to(source)
            for output in (source, hardlink, symlink):
                with self.subTest(output=output.name), self.assertRaises(ValueError):
                    apk_safety.write_json_output(source, output, '{}')
                self.assertEqual(source.read_bytes(), b'PRESERVE_INPUT')

    def test_output_requires_json_and_replaces_atomically(self):
        with tempfile.TemporaryDirectory() as directory:
            source = Path(directory) / 'source.apk'
            source.write_bytes(b'PRESERVE_INPUT')
            bad = Path(directory) / 'output.apk'
            with self.assertRaisesRegex(ValueError, '.json'):
                apk_safety.write_json_output(source, bad, '{}')
            self.assertFalse(bad.exists())
            output = Path(directory) / 'output.json'
            output.write_text('old', encoding='utf-8')
            with mock.patch('apk_safety.os.replace', side_effect=OSError('simulated failure')):
                with self.assertRaises(OSError):
                    apk_safety.write_json_output(source, output, '{}')
            self.assertEqual(output.read_text(), 'old')
            self.assertEqual(list(Path(directory).glob('.output.json.*.tmp')), [])
            apk_safety.write_json_output(source, output, '{"ok":true}')
            self.assertEqual(json.loads(output.read_text()), {'ok': True})
            self.assertEqual(source.read_bytes(), b'PRESERVE_INPUT')

    def test_both_clis_refuse_to_overwrite_the_input(self):
        with tempfile.TemporaryDirectory() as directory:
            source = Path(directory) / 'source.json'
            minimal_dex = bytearray(112)
            minimal_dex[:8] = b'dex\n035\0'
            struct.pack_into('<III', minimal_dex, 32, 112, 112, 0x12345678)
            with zipfile.ZipFile(source, 'w') as archive:
                archive.writestr('AndroidManifest.xml', axml())
                archive.writestr('classes.dex', minimal_dex)
            original = source.read_bytes()
            for name in ('inspect_manifest.py', 'inspect_dex_identity.py'):
                script = Path(__file__).parent / name
                result = subprocess.run([sys.executable, str(script), '--apk', str(source),
                                         '--output', str(source)], capture_output=True, text=True)
                self.assertNotEqual(result.returncode, 0, result.stdout)
                self.assertIn('must not overwrite', result.stderr)
                self.assertEqual(source.read_bytes(), original)

    def test_oversized_directory_rejected_before_zipfile(self):
        with tempfile.TemporaryDirectory() as directory:
            source = Path(directory) / 'bad.apk'
            # A tiny malicious header requests a huge central-directory read.
            source.write_bytes(struct.pack('<4sHHHHIIH', b'PK\x05\x06', 0, 0, 0, 0,
                                           apk_safety.MAX_DIRECTORY_BYTES + 1, 0, 0))
            for inspect in (manifest.inspect, dex.inspect):
                with mock.patch('apk_safety.zipfile.ZipFile') as constructor:
                    with self.assertRaisesRegex(ValueError, '64 MiB'):
                        inspect(source)
                    constructor.assert_not_called()

    def test_directory_count_mismatch_rejected_before_zipfile(self):
        with tempfile.TemporaryDirectory() as directory:
            source = Path(directory) / 'bad.apk'
            with zipfile.ZipFile(source, 'w') as archive:
                archive.writestr('one', b'x')
            data = bytearray(source.read_bytes())
            at = data.rfind(b'PK\x05\x06')
            struct.pack_into('<HH', data, at + 8, 0, 0)
            source.write_bytes(data)
            with mock.patch('apk_safety.zipfile.ZipFile') as constructor:
                with self.assertRaisesRegex(ValueError, 'entry count mismatch'):
                    manifest.inspect(source)
                constructor.assert_not_called()

    def test_zip64_and_entry_limit_rejected_before_zipfile(self):
        with tempfile.TemporaryDirectory() as directory:
            source = Path(directory) / 'bad.apk'
            source.write_bytes(struct.pack('<4sHHHHIIH', b'PK\x05\x06', 0, 0,
                                           0xffff, 0xffff, 0, 0, 0))
            with mock.patch('apk_safety.zipfile.ZipFile') as constructor:
                with self.assertRaisesRegex(ValueError, 'ZIP64'):
                    dex.inspect(source)
                constructor.assert_not_called()
            with zipfile.ZipFile(source, 'w') as archive:
                archive.writestr('one', b'x')
            with mock.patch('apk_safety.MAX_ENTRIES', 0):
                with mock.patch('apk_safety.zipfile.ZipFile') as constructor:
                    with self.assertRaisesRegex(ValueError, 'entry count'):
                        manifest.inspect(source)
                    constructor.assert_not_called()


if __name__ == '__main__':
    unittest.main()
