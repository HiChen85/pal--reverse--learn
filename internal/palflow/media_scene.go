package palflow

// applyScenePresentation models the persistent changes made by MakeScene.
// Its return value is the additional full-screen update before a needed fade
// in. Palette writes and framebuffer composition remain presentation requests.
// Callers must first check palette availability when NeedToFadeIn is true.
// The caller records its own subsequent full-screen update separately.
func applyScenePresentation(w *WorldState) int {
	if w == nil {
		return 0
	}
	applyWavePresentation(w)
	if !w.NeedToFadeIn {
		return 0
	}
	w.NeedToFadeIn = false
	w.ScreenTint = ""
	return 1
}

// PAL_BattleMakeScene and PAL_MakeScene both apply the wave; only the latter
// performs the pending overworld fade-in.
func applyWavePresentation(w *WorldState) {
	if w == nil {
		return
	}
	w.ScreenWave += uint16(w.WaveProgression)
	if w.ScreenWave == 0 || w.ScreenWave >= 256 {
		w.ScreenWave, w.WaveProgression = 0, 0
	} else {
		w.WavePhase = (w.WavePhase + 1) % 32
	}
}

// advanceScreenRequests models shake consumption on successful full-screen
// display requests in the logical presentation model. Partial rectangle and
// palette-only updates do not consume the counter in the reference. Renderer
// pause/lock/allocation failures are outside this request model.
func advanceScreenRequests(w *WorldState, count int) {
	if w == nil || count <= 0 {
		return
	}
	if count >= int(w.ScreenShakeTime) {
		w.ScreenShakeTime = 0
	} else {
		w.ScreenShakeTime -= uint16(count)
	}
}
