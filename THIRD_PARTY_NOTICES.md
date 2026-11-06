# Third-party references and dependencies

- MyGo v0.7.4, github.com/egoist/mygo — MIT. A direct Go module dependency; its transitive dependencies retain their own licenses. Use `go list -m all` for the resolved module list.
- SDLPAL, github.com/sdlpal/sdlpal — GPL-3.0. Used as a public source research reference; not vendored or linked into this project. A standalone build of its public YJ1 C decoder was run temporarily for byte-for-byte comparison against four local Go-decoded samples. No APK or supplied native target library was executed; the reference decoder source and executable are not included in this repository. See docs/RESEARCH.md for pinned commit and evidence locations.
- PAL / 仙剑奇侠传 original programs, dialogue, images, music, game data, and trademarks belong to their respective owners. No original game assets are bundled. The sample story and combat rules in this repository are independently authored demonstrations, not a canonical continuation or a validated original-game emulation.

This project's independently authored code retains the repository's MIT license; see LICENSE. SDLPAL source is not incorporated. Any future copied or translated GPL code needs its own license review before inclusion.
