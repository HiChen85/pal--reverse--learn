# Offline APK identity helpers

Self-authored Python standard-library helpers for a supplied APK. They read ZIP
members and selected binary tables without executing APK code or using network
services. No dependency installation is required.

```sh
python3 tools/apk/inspect_manifest.py --apk /path/to/supplied.apk
python3 tools/apk/inspect_dex_identity.py --apk /path/to/supplied.apk
python3 -m unittest discover -s tools/apk -p 'test_*.py' -v
```

Both tools print JSON; optional `--output file.json` writes the same JSON to a
caller-selected `.json` file via atomic replacement. Input paths and input aliases
(including symlinks and hardlinks) are rejected as outputs; an existing separate
JSON output is replaced only after a complete write succeeds. The manifest helper
projects a fixed set of identity/SDK/launcher fields, counts permissions and
activities, and resolves the application label when it is a simple ARSC string.
It does not dump arbitrary metadata, complete manifest attributes, or service
addresses. The DEX helper reports counts and a fixed selection of SDK/class
identity references; it does not dump the string table or method bodies.

These are limited identity inspection tools, not full Android format validators
or signature verifiers. Selected member allocations are capped at 16 MiB;
duplicate selected identity members are rejected. Before Python allocates the
central directory, shared preflight checks cap the APK at 1 GiB, the directory at
64 MiB, and entries at 100,000, and validate the actual central record count.
These helpers reject ZIP64, multi-disk archives, directory extent mismatches,
and ambiguous end records; this is intentionally narrower than the Go inventory
CLI. Input files must remain unchanged throughout inspection. ARSC complex values, sparse or
offset16 entries and references are unresolved. DEX supports the ordinary
little-endian primary classes.dex tables used by the supplied sample; method
bodies, multidex traversal and complete MUTF-8 semantics are out of scope.
A declared SDK level does not establish runtime support. Class presence does not
establish API invocation.
