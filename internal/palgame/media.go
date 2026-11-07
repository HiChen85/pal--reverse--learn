package palgame

import (
	"fmt"
	"github.com/HiChen85/pal--reverse--learn/internal/palflow"
)

// MediaProfile is an explicit interpretation profile selected at import. It
// does not authenticate the historical release represented by an APK label.
func (s Source) MediaProfile() (string, error) {
	if s.Game == nil || (s.Game.Layout != "dos" && s.Game.Layout != "windows") {
		return "", fmt.Errorf("media requires an explicit dos/windows interpretation profile")
	}
	return s.Game.Layout, nil
}

func (s Source) RNGFrameResource(movie uint16, frame uint32) (palflow.RNGFrameResource, error) {
	if s.RNG == nil {
		return palflow.RNGFrameResource{}, fmt.Errorf("RNG animation requires a local RNG.MKF catalog")
	}
	f, err := s.RNG.Frame(movie, frame)
	return palflow.RNGFrameResource{Movie: f.Movie, Frame: f.Frame, FrameCount: f.FrameCount, Status: f.Status, Reason: f.Reason, ArchiveSHA256: s.RNG.SourceSHA256(), SHA256: f.SHA256, CompressedSize: f.CompressedSize, DecodedSize: f.DecodedSize}, err
}

func (s Source) RNGArchiveIdentity() (string, error) {
	if s.RNG == nil {
		return "", fmt.Errorf("RNG animation requires a local RNG.MKF catalog")
	}
	return s.RNG.SourceSHA256(), nil
}

func (s Source) FBPResource(id uint16) (palflow.FBPResource, error) {
	if s.FBP == nil {
		return palflow.FBPResource{}, fmt.Errorf("FBP picture requires a local FBP.MKF catalog")
	}
	f, err := s.FBP.Picture(id)
	return palflow.FBPResource{ID: f.ID, Status: f.Status, Reason: f.Reason, ArchiveSHA256: s.FBP.SourceSHA256(), SHA256: f.SHA256, CompressedSize: f.CompressedSize, DecodedSize: f.DecodedSize}, err
}

// The Go workbench has no CD playback backend. This is its declared device
// model, not an observation about the user's physical audio hardware.
func (s Source) CDPlayback(track int16) (palflow.CDPlaybackResult, error) {
	return palflow.CDPlaybackResult{Available: false, PlaySucceeded: false, Basis: "Go workbench has no CD playback backend; use RIX fallback request"}, nil
}
