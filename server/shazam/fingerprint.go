package shazam

import (
	"fmt"
	"log"
	"os"
	"runtime"
	"song-recognition/models"
	"song-recognition/utils"
	"song-recognition/wav"
	"time"
)

const (
	maxFreqBits  = 9
	maxDeltaBits = 14
)

// Fingerprint generates fingerprints from a list of peaks.
// each fingerprint is an (address -> couple) entry where the address
// encodes a frequency pair + time delta, and the couple holds the
// anchor time and song ID.
func Fingerprint(peaks []Peak, songID uint32, cfg FingerprintConfig) map[uint32]models.Couple {
	fingerprints := map[uint32]models.Couple{}

	for i, anchor := range peaks {
		for j := i + 1; j < len(peaks) && j <= i+cfg.TargetZoneSize; j++ {
			target := peaks[j]
			address := createAddress(anchor, target)
			fingerprints[address] = models.Couple{
				AnchorTimeMs: uint32(anchor.Time * 1000),
				SongID:       songID,
			}
		}
	}

	return fingerprints
}

func createAddress(anchor, target Peak) uint32 {
	anchorFreqBin := uint32(anchor.Freq / 10)
	targetFreqBin := uint32(target.Freq / 10)
	deltaMsRaw := uint32((target.Time - anchor.Time) * 1000)

	anchorFreqBits := anchorFreqBin & ((1 << maxFreqBits) - 1)
	targetFreqBits := targetFreqBin & ((1 << maxFreqBits) - 1)
	deltaBits := deltaMsRaw & ((1 << maxDeltaBits) - 1)

	return (anchorFreqBits << 23) | (targetFreqBits << 14) | deltaBits
}

// FingerprintAudioChunked processes an audio file in bounded-memory
// chunks using ffmpeg for segment extraction. each chunk is independently
// converted to WAV, fingerprinted, and merged into the result map.
// memory usage is proportional to chunkDurationSec, not total file length.
func FingerprintAudioChunked(inputPath string, songID uint32, cfg FingerprintConfig) (map[uint32]models.Couple, error) {
	duration, err := wav.GetAudioDuration(inputPath)
	if err != nil {
		return nil, fmt.Errorf("failed to get audio duration: %v", err)
	}

	log.Printf("[fingerprint] file duration: %.0fs (%.1f hours), chunk size: %.0fs",
		duration, duration/3600, cfg.ChunkDurationSec)

	fingerprints := make(map[uint32]models.Couple)

	chunkDur := cfg.ChunkDurationSec
	if chunkDur <= 0 {
		chunkDur = duration
	}

	// small overlap avoids losing peak pairs that straddle chunk boundaries
	overlap := 5.0
	step := chunkDur - overlap
	if step <= 0 {
		step = chunkDur
	}

	chunkIdx := 0
	for start := 0.0; start < duration; start += step {
		dur := chunkDur
		if start+dur > duration {
			dur = duration - start
		}
		if dur <= 0 {
			break
		}

		chunkStart := time.Now()
		log.Printf("[chunk %d] extracting %.0fs - %.0fs", chunkIdx, start, start+dur)

		chunkPath, err := wav.ExtractChunkAsWAV(inputPath, start, dur)
		if err != nil {
			return nil, fmt.Errorf("chunk extraction at %.0fs failed: %v", start, err)
		}

		wavInfo, err := wav.ReadWavInfo(chunkPath)
		os.Remove(chunkPath)
		if err != nil {
			return nil, fmt.Errorf("reading chunk wav at %.0fs failed: %v", start, err)
		}

		spectro, err := Spectrogram(wavInfo.LeftChannelSamples, wavInfo.SampleRate, cfg)
		if err != nil {
			return nil, fmt.Errorf("spectrogram at %.0fs failed: %v", start, err)
		}

		peaks := ExtractPeaks(spectro, wavInfo.Duration, wavInfo.SampleRate, cfg)

		// offset peak times so they reflect position in the full file
		for i := range peaks {
			peaks[i].Time += start
		}

		chunkFP := Fingerprint(peaks, songID, cfg)
		utils.ExtendMap(fingerprints, chunkFP)

		log.Printf("[chunk %d] %d peaks, %d fingerprints, took %s",
			chunkIdx, len(peaks), len(chunkFP), time.Since(chunkStart))

		// release chunk memory before next iteration
		wavInfo = nil
		spectro = nil
		runtime.GC()

		chunkIdx++
	}

	log.Printf("[fingerprint] total: %d fingerprints from %d chunks", len(fingerprints), chunkIdx)
	return fingerprints, nil
}

// FingerprintAudio is a convenience wrapper that processes the entire
// file using the default music config. kept for backward compatibility.
func FingerprintAudio(songFilePath string, songID uint32) (map[uint32]models.Couple, error) {
	return FingerprintAudioChunked(songFilePath, songID, DefaultMusicConfig())
}
