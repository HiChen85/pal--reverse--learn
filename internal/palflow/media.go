package palflow

import "fmt"

// Media source methods inspect resources or resolve an explicitly selected
// logical device profile; they must not play audio, execute code, or render.
type MediaProfileSource interface{ MediaProfile() (string, error) }
type RNGResourceSource interface {
	RNGArchiveIdentity() (string, error)
	RNGFrameResource(movie uint16, frame uint32) (RNGFrameResource, error)
}
type FBPResourceSource interface {
	FBPResource(id uint16) (FBPResource, error)
}
type CDPlaybackSource interface {
	CDPlayback(track int16) (CDPlaybackResult, error)
}

type RNGFrameResource struct {
	Movie          uint16 `json:"movie"`
	Frame          uint32 `json:"frame"`
	FrameCount     uint16 `json:"frame_count"`
	Status         string `json:"status"`
	Reason         string `json:"reason,omitempty"`
	ArchiveSHA256  string `json:"archive_sha256"`
	SHA256         string `json:"sha256,omitempty"`
	CompressedSize int    `json:"compressed_size"`
	DecodedSize    int    `json:"decoded_size"`
}
type FBPResource struct {
	ID             uint16 `json:"id"`
	Status         string `json:"status"`
	Reason         string `json:"reason,omitempty"`
	ArchiveSHA256  string `json:"archive_sha256"`
	SHA256         string `json:"sha256,omitempty"`
	CompressedSize int    `json:"compressed_size"`
	DecodedSize    int    `json:"decoded_size"`
}
type CDPlaybackResult struct {
	Available     bool   `json:"available"`
	PlaySucceeded bool   `json:"play_succeeded"`
	Basis         string `json:"basis"`
}
type RNGPlaybackRecord struct {
	ArchiveSHA256                   string             `json:"archive_sha256"`
	Movie                           uint16             `json:"movie"`
	StartFrame                      uint16             `json:"start_frame"`
	EndFrame                        int                `json:"end_frame"`
	Speed                           uint16             `json:"speed"`
	FrameDelayMilliseconds          float64            `json:"frame_delay_milliseconds"`
	DelayAfterFirstFadeMilliseconds float64            `json:"delay_after_first_fade_milliseconds,omitempty"`
	Timing                          string             `json:"timing"`
	Frames                          []RNGFrameResource `json:"frames,omitempty"`
	Stop                            *RNGFrameResource  `json:"stop,omitempty"`
	FadeInRequested                 bool               `json:"fade_in_requested"`
	Palette                         uint16             `json:"palette"`
	Night                           bool               `json:"night"`
}
type ColorFadeRecord struct {
	Color                  uint8  `json:"color"`
	FromColor              bool   `json:"from_color"`
	Palette                uint16 `json:"palette"`
	Night                  bool   `json:"night"`
	Steps                  uint16 `json:"steps"`
	PaletteWrites          uint16 `json:"palette_writes"`
	FrameDelayMilliseconds uint32 `json:"frame_delay_milliseconds"`
}
type FBPRecord struct {
	Profile                string       `json:"profile"`
	ProfileBasis           string       `json:"profile_basis"`
	Picture                *FBPResource `json:"picture,omitempty"`
	BlackScreen            bool         `json:"black_screen"`
	Fade                   uint16       `json:"fade"`
	TransitionFrames       uint16       `json:"transition_frames"`
	FrameDelayMilliseconds uint16       `json:"frame_delay_milliseconds"`
	FinalBlitSkipped       bool         `json:"final_blit_skipped"`
}
type CDRecord struct {
	RequestedTrack    int16            `json:"requested_track"`
	DeviceTrack       int16            `json:"device_track"`
	FallbackMusic     uint16           `json:"fallback_music"`
	Resolution        CDPlaybackResult `json:"resolution"`
	FallbackRequested bool             `json:"fallback_requested"`
}
type ShakeRecord struct {
	Time  uint16 `json:"time"`
	Level uint16 `json:"level"`
}
type MediaRecord struct {
	Kind                      string              `json:"kind"`
	ScreenUpdates             int                 `json:"screen_updates"`
	BackupRequested           bool                `json:"backup_requested"`
	SceneCompositionRequested bool                `json:"scene_composition_requested"`
	FadeSpeed                 uint16              `json:"fade_speed,omitempty"`
	TransitionFrames          uint16              `json:"transition_frames,omitempty"`
	FrameDelayMilliseconds    uint16              `json:"frame_delay_milliseconds,omitempty"`
	DelayIntervals            uint16              `json:"delay_intervals,omitempty"`
	ImplicitFadeInRequested   bool                `json:"implicit_fade_in_requested"`
	SceneResources            *SceneResourceState `json:"scene_resources,omitempty"`
	Shake                     *ShakeRecord        `json:"shake,omitempty"`
	RNG                       *RNGPlaybackRecord  `json:"rng,omitempty"`
	FBP                       *FBPRecord          `json:"fbp,omitempty"`
	ColorFade                 *ColorFadeRecord    `json:"color_fade,omitempty"`
	CD                        *CDRecord           `json:"cd,omitempty"`
}

func mediaOpcodeName(op uint16) string {
	switch op {
	case 0x35:
		return "set screen shake time and strength"
	case 0x36:
		return "select RNG movie"
	case 0x37:
		return "validate RNG frames and request playback without world updates"
	case 0x76:
		return "request FBP picture using explicit DOS/Windows model profile"
	case 0x8c:
		return "request fade from or to a checked palette color"
	case 0x9b:
		return "backup screen, compose current scene and request fixed-speed transition"
	case 0xa3:
		return "resolve CD playback and request configured music fallback"
	case 0xa6:
		return "request backup of current framebuffer"
	}
	return ""
}

func (r *Runner) requireMediaPalette(w *WorldState) error {
	source, ok := r.source.(PaletteAvailabilitySource)
	if !ok {
		return fmt.Errorf("media fade requires explicit palette availability")
	}
	available, err := source.PaletteAvailable(w.Palette, w.NightPalette)
	if err != nil {
		return fmt.Errorf("media palette lookup: %w", err)
	}
	if !available {
		return fmt.Errorf("media fade palette %d is unavailable", w.Palette)
	}
	return nil
}

// evaluateMedia commits only after every requested resource and device outcome
// is known. It models persistent flags and full-screen update requests, without
// pretending that requested pixels, sound, key processing or wall time ran.
func (r *Runner) evaluateMedia(s *State, step *TraceStep, inst Instruction) (bool, error) {
	if mediaOpcodeName(inst.Opcode) == "" {
		return false, nil
	}
	if s == nil || s.World == nil || len(s.Frames) == 0 || step == nil {
		return true, fmt.Errorf("media instruction requires world, script frame and trace step")
	}
	if r == nil || r.source == nil {
		return true, fmt.Errorf("media instruction requires a source")
	}
	next, err := fallthroughPC(s.Frames[len(s.Frames)-1].PC)
	if err != nil {
		return true, err
	}
	w := CloneWorld(s.World)
	evidence := *step
	media := &MediaRecord{Kind: mediaOpcodeName(inst.Opcode)}
	a := inst.Operands
	work := 0
	backup := func() error {
		if w.ScreenBackupRequests == ^uint64(0) {
			return fmt.Errorf("screen backup request counter overflow")
		}
		w.ScreenBackupRequests++
		media.BackupRequested = true
		return nil
	}
	switch inst.Opcode {
	case 0x35:
		w.ScreenShakeTime, w.ScreenShakeLevel = a[0], a[1]
		if w.ScreenShakeLevel == 0 {
			w.ScreenShakeLevel = 4
		}
		media.Shake = &ShakeRecord{Time: w.ScreenShakeTime, Level: w.ScreenShakeLevel}
		if a[0] == 0 {
			media.ScreenUpdates = 1
		}
	case 0x36:
		w.CurrentRNG, w.CurrentRNGKnown = a[0], true
	case 0x37:
		if !w.CurrentRNGKnown {
			return true, fmt.Errorf("RNG playback requires explicitly selected current movie")
		}
		source, ok := r.source.(RNGResourceSource)
		if !ok {
			return true, fmt.Errorf("RNG playback requires a local validated RNG resource source")
		}
		archiveSHA256, err := source.RNGArchiveIdentity()
		if err != nil {
			return true, fmt.Errorf("RNG archive: %w", err)
		}
		if archiveSHA256 == "" {
			return true, fmt.Errorf("RNG playback requires archive identity even for an empty frame range")
		}
		speed := a[2]
		if speed == 0 {
			speed = 16
		}
		end := -1
		if a[1] > 0 {
			end = int(a[1]) + 1
		}
		play := &RNGPlaybackRecord{ArchiveSHA256: archiveSHA256, Movie: w.CurrentRNG, StartFrame: a[0], EndFrame: -1, Speed: speed, FrameDelayMilliseconds: 1000 / float64(speed), Palette: w.Palette, Night: w.NightPalette, Timing: "cumulative deadlines; late frames need not delay"}
		if a[1] > 0 {
			play.EndFrame = int(a[1])
		}
		for frame := uint32(a[0]); int(frame) != end; frame++ {
			if err := r.mediaBudget(s, work+1); err != nil {
				return true, err
			}
			work++
			resource, err := source.RNGFrameResource(w.CurrentRNG, frame)
			if err != nil {
				return true, fmt.Errorf("RNG frame %d: %w", frame, err)
			}
			if resource.Movie != w.CurrentRNG || resource.Frame != frame {
				return true, fmt.Errorf("RNG source returned inconsistent movie/frame")
			}
			if resource.ArchiveSHA256 != archiveSHA256 {
				return true, fmt.Errorf("RNG frame result differs from opened archive identity")
			}
			switch resource.Status {
			case "playable":
				if frame >= uint32(resource.FrameCount) || resource.DecodedSize < 0 || resource.DecodedSize > 65000 || resource.CompressedSize < 1 || resource.CompressedSize > 65000 || resource.SHA256 == "" {
					return true, fmt.Errorf("RNG playable resource lacks bounded decode evidence")
				}
			case "missing_movie", "empty_movie", "missing_frame", "empty_frame", "frame_exceeds_read_buffer", "decompression_failed":
				play.Stop = &resource
			default:
				return true, fmt.Errorf("RNG source returned unknown frame status %q", resource.Status)
			}
			if play.Stop != nil {
				break
			}
			play.Frames = append(play.Frames, resource)
			media.ScreenUpdates++
			if w.NeedToFadeIn {
				if err := r.requireMediaPalette(w); err != nil {
					return true, err
				}
				w.NeedToFadeIn, w.ScreenTint = false, ""
				play.FadeInRequested = true
				play.DelayAfterFirstFadeMilliseconds = play.FrameDelayMilliseconds / 2
			}
		}
		media.RNG = play
	case 0x76:
		source, ok := r.source.(MediaProfileSource)
		if !ok {
			return true, fmt.Errorf("FBP requires an explicit DOS/Windows model profile")
		}
		profile, err := source.MediaProfile()
		if err != nil {
			return true, err
		}
		show := &FBPRecord{Profile: profile, ProfileBasis: "explicit model profile; does not authenticate release identity", Fade: a[1]}
		switch profile {
		case "windows":
			show.BlackScreen = true
			media.ScreenUpdates = 1
		case "dos":
			resources, ok := r.source.(FBPResourceSource)
			if !ok {
				return true, fmt.Errorf("DOS FBP requires local FBP resource source")
			}
			picture, err := resources.FBPResource(a[0])
			if err != nil {
				return true, err
			}
			if picture.ID != a[0] {
				return true, fmt.Errorf("FBP resource ID differs from requested picture")
			}
			if picture.ArchiveSHA256 == "" {
				return true, fmt.Errorf("FBP result lacks archive identity")
			}
			switch picture.Status {
			case "picture":
				if picture.DecodedSize != 64000 || picture.SHA256 == "" || picture.ArchiveSHA256 == "" {
					return true, fmt.Errorf("FBP picture lacks complete decode evidence")
				}
			case "black_missing_picture", "black_empty_picture", "black_decompression_failed", "black_empty_decode":
				show.BlackScreen = true
			default:
				return true, fmt.Errorf("unknown FBP resource status %q", picture.Status)
			}
			w.EndingEffectSprite = 0
			show.Picture = &picture
			show.FinalBlitSkipped = a[0] == 49
			media.ScreenUpdates = 1
			if a[1] != 0 {
				if err := backup(); err != nil {
					return true, err
				}
				show.TransitionFrames = 96
				show.FrameDelayMilliseconds = (a[1] + 1) * 10
				media.ScreenUpdates += 96
				media.DelayIntervals = 96
			}
		default:
			return true, fmt.Errorf("FBP model profile %q is unknown; need dos or windows", profile)
		}
		media.FBP = show
	case 0x8c:
		if err := r.requireMediaPalette(w); err != nil {
			return true, err
		}
		delay := uint32(a[1]) * 10
		if delay == 0 {
			delay = 10
		}
		media.ColorFade = &ColorFadeRecord{Color: uint8(a[0]), FromColor: a[2] != 0, Palette: w.Palette, Night: w.NightPalette, Steps: 64, PaletteWrites: 65, FrameDelayMilliseconds: delay}
		w.NeedToFadeIn = false
		w.ScreenTint = ""
		if a[2] == 0 {
			w.ScreenTint = fmt.Sprintf("palette_color:%d", uint8(a[0]))
		}
	case 0x9b:
		if w.SceneResources == nil {
			return true, fmt.Errorf("scene transition requires loaded scene/map resource identity")
		}
		if w.SceneResources.MapID == 0 || w.SceneResources.MapSHA256 == "" || w.SceneResources.GOPSHA256 == "" || w.SceneResources.MAPArchiveSHA256 == "" || w.SceneResources.GOPArchiveSHA256 == "" {
			return true, fmt.Errorf("scene transition requires complete loaded map/GOP identities")
		}
		if w.NeedToFadeIn {
			if err := r.requireMediaPalette(w); err != nil {
				return true, err
			}
		}
		if err := backup(); err != nil {
			return true, err
		}
		media.SceneCompositionRequested = true
		media.SceneResources = CloneWorld(w).SceneResources
		media.FadeSpeed = 2
		media.TransitionFrames = 72
		media.FrameDelayMilliseconds = 30
		media.DelayIntervals = 71
		extra := applyScenePresentation(w)
		media.ImplicitFadeInRequested = extra > 0
		media.ScreenUpdates = 73 + extra
	case 0xa3:
		source, ok := r.source.(CDPlaybackSource)
		if !ok {
			return true, fmt.Errorf("CD playback requires explicit device availability and playback resolution")
		}
		track := int16(a[0])
		deviceTrack := track
		if track == -1 {
			deviceTrack = -2
		}
		resolution, err := source.CDPlayback(deviceTrack)
		if err != nil {
			return true, err
		}
		if resolution.Basis == "" || (!resolution.Available && resolution.PlaySucceeded) {
			return true, fmt.Errorf("CD resolution needs a basis and consistent availability/result")
		}
		fallback := !resolution.Available || !resolution.PlaySucceeded
		media.CD = &CDRecord{RequestedTrack: track, DeviceTrack: deviceTrack, FallbackMusic: a[1], Resolution: resolution, FallbackRequested: fallback}
		w.Music, w.MusicKnown = a[1], true
		if fallback {
			evidence.Audio = &AudioRecord{Kind: "music", ID: a[1], Loop: true, PlaybackRequested: true}
		} else {
			evidence.Audio = &AudioRecord{Kind: "cd", PlaybackRequested: true}
		}
	case 0xa6:
		if err := backup(); err != nil {
			return true, err
		}
	}
	advanceScreenRequests(w, media.ScreenUpdates)
	frames := append([]Frame(nil), s.Frames...)
	frames[len(frames)-1].PC = next
	evidence.Media = media
	evidence.Screen = &ScreenRecord{Kind: media.Kind, Rendered: false}
	evidence.Effect = mediaOpcodeName(inst.Opcode) + "; validated presentation request and persistent state only; no world updates or audiovisual execution"
	s.World, s.Frames = w, frames
	s.MediaWork += work
	if inst.Opcode == 0xa3 {
		s.Music, s.MusicKnown = a[1], true
	}
	*step = evidence
	return true, nil
}

func (r *Runner) mediaBudget(s *State, extra int) error {
	remaining := r.options.MaxSteps
	for _, used := range []int{s.ExecutedSteps, s.PlayerSubsteps, s.WorldTicks, s.AutoSubsteps, s.CombatWork, s.MediaWork, extra, 1} {
		if used < 0 || used > remaining {
			return fmt.Errorf("RNG frame validation exceeds combined execution budget")
		}
		remaining -= used
	}
	return nil
}
