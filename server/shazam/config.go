package shazam

// FingerprintConfig controls all tunable parameters in the
// spectrogram, peak extraction, and fingerprint generation pipeline.
type FingerprintConfig struct {
	DSPRatio         int      // downsample factor applied to input audio
	WindowSize       int      // FFT window size in samples (must be power of 2)
	HopSize          int      // samples between successive FFT frames
	MaxFreqHz        float64  // low-pass cutoff before downsampling
	TargetZoneSize   int      // number of neighboring peaks to pair with each anchor
	FreqBands        [][2]int // (minBin, maxBin) pairs for peak extraction
	ChunkDurationSec float64  // seconds per processing chunk (0 = whole file)
}

// DefaultAudiobookConfig returns parameters optimised for long-form
// spoken word. produces ~16 fingerprints per second of audio instead
// of ~430, which keeps storage and memory practical for multi-hour files.
func DefaultAudiobookConfig() FingerprintConfig {
	return FingerprintConfig{
		DSPRatio:       8,    // effective rate 5512 Hz, covers speech fine
		WindowSize:     2048, // ~371ms frames at 5512 Hz
		HopSize:        2048, // no overlap, ~2.7 fps
		MaxFreqHz:      3000, // speech doesn't need above 3 kHz
		TargetZoneSize: 3,
		FreqBands: [][2]int{
			{0, 100},     // 0-269 Hz: fundamental frequency
			{100, 350},   // 269-942 Hz: first formant region
			{350, 1024},  // 942-2756 Hz: higher formants
		},
		ChunkDurationSec: 120,
	}
}

// DefaultMusicConfig returns the original Shazam-style parameters
// tuned for short music clips with high time-frequency resolution.
func DefaultMusicConfig() FingerprintConfig {
	return FingerprintConfig{
		DSPRatio:       4,
		WindowSize:     1024,
		HopSize:        512,
		MaxFreqHz:      5000,
		TargetZoneSize: 5,
		FreqBands: [][2]int{
			{0, 10}, {10, 20}, {20, 40},
			{40, 80}, {80, 160}, {160, 512},
		},
		ChunkDurationSec: 300,
	}
}
