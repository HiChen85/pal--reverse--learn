# PAL media request model

The Go script model records validated audiovisual requests and the persistent
state they affect. It does not play audio, render animations, or sleep. The
reference inspected for these transitions is SDLPal commit
`d4bf69b64d335cd680555af9df90e7ec4f3006a3`; this implementation independently
expresses its behavior with checked resource access. No reference source or
game resource payload is included here.

| Opcode | Modeled behavior and evidence |
| --- | --- |
| 0035 | Store shake time/strength; zero strength means 4. Time zero immediately requests one full-screen update. Full-screen updates consume remaining shake requests; rectangle/palette updates do not. |
| 0036 | Select the current RNG movie, including movie zero. A separate known flag prevents an unspecified snapshot from silently selecting zero. |
| 0037 | Validate each requested RNG frame before recording playback. A positive endpoint is inclusive; endpoint zero means continue until resource exhaustion. Successful frames request display and clear a pending fade only after a checked palette lookup. Missing/empty frames, a frame larger than the 65000-byte read buffer, or a declared decompression output larger than the destination stop playback. No world update occurs. |
| 0076 | Use an explicit DOS/Windows model profile. Windows requests black; DOS resets the ending effect sprite and requests a checked FBP or the reference's known black fallback. A nonzero fade requests 96 blend displays followed by one final display; the delay retains the reference's WORD arithmetic. DOS picture 49 skips its final picture blit. |
| 008C | Check the palette, retain the low color byte, record 64 color transitions plus the final palette write, and clear the pending fade flag. Zero delay means 10 milliseconds per transition. |
| 009B | Request framebuffer backup, compose the loaded scene once, and request the reference's fixed-speed-2 screen transition. Composition updates wave magnitude/phase and resolves a pending fade. The transition requests 72 blend displays plus a final display, with 71 nominal 30-millisecond wait intervals. No world update occurs. |
| 00A3 | Store the fallback music ID. An explicit logical CD availability/playback result selects a CD request or looping fallback music. The signed track -1 is passed to the CD layer as -2. Device availability and success are never inferred from missing configuration. |
| 00A6 | Request backup of the current framebuffer and retain the request generation counter. |

RNG uses cumulative frame deadlines. Speed zero means 16 frames per second;
after the first pending fade is requested, the next deadline resets to half a
frame interval. These records describe intended timing; they do not certify
elapsed wall time, display completion, or audio playback.
The RNG archive identity is required even for an empty frame range: the
reference opens its required archive before entering the playback loop.

The local RNG/FBP catalogs snapshot regular files, validate MKF directories,
decode DOS YJ1 with bounded output, and return hashes and size evidence. RNG
validation checks operand reads and every pair-write against 320x200 pixels.
Unknown RNG command bytes consume only their opcode, as in the reference.
Unknown compression, malformed YJ1, unsafe command spans, and a nonempty FBP
decode shorter than 64000 pixels block the instruction atomically. Such input
cannot establish that the reference safely reached its next instruction.

0076's model profile is a user-selected logical branch, not authentication of
the original distribution. 00A4/00A5, actual framebuffer pixels, renderer
pause/lock/allocation failures, and CD device I/O remain outside this model.
Scene composition helpers must be called at every modeled MakeScene request
to retain wave/fade/shake state across other script and control operations.

Synthetic regression tests cover inclusive/empty/exhausted movie ranges,
partial-resource failure atomicity, fade state, budgets, directory and command
boundaries, WORD delay wrapping, CD fallback, and immutable concurrent reads.
`PAL_MEDIA_DIR=/path/to/local/data go test ./internal/palasset -run
TestMediaLocalCorpus -v` additionally validates supplied RNG/FBP resources and
prints counts and archive identities without retaining game payloads.
