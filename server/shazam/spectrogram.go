package shazam

import (
	"errors"
	"fmt"
	"math"
	"math/cmplx"
)

func Spectrogram(sample []float64, sampleRate int, cfg FingerprintConfig) ([][]float64, error) {
	filteredSample := LowPassFilter(cfg.MaxFreqHz, float64(sampleRate), sample)

	targetRate := sampleRate / cfg.DSPRatio
	downsampledSample, err := Downsample(filteredSample, sampleRate, targetRate)
	if err != nil {
		return nil, fmt.Errorf("couldn't downsample audio sample: %v", err)
	}

	// free the filtered copy early
	filteredSample = nil

	window := make([]float64, cfg.WindowSize)
	for i := range window {
		theta := 2 * math.Pi * float64(i) / float64(cfg.WindowSize-1)
		window[i] = 0.5 - 0.5*math.Cos(theta) // hanning
	}

	spectrogram := make([][]float64, 0, len(downsampledSample)/cfg.HopSize)

	for start := 0; start+cfg.WindowSize <= len(downsampledSample); start += cfg.HopSize {
		frame := make([]float64, cfg.WindowSize)
		copy(frame, downsampledSample[start:start+cfg.WindowSize])

		for j := range window {
			frame[j] *= window[j]
		}

		fftResult := FFT(frame)

		magnitude := make([]float64, len(fftResult)/2)
		for j := range magnitude {
			magnitude[j] = cmplx.Abs(fftResult[j])
		}

		spectrogram = append(spectrogram, magnitude)
	}

	return spectrogram, nil
}

// LowPassFilter is a first-order low-pass filter that attenuates high
// frequencies above the cutoffFrequency.
func LowPassFilter(cutoffFrequency, sampleRate float64, input []float64) []float64 {
	rc := 1.0 / (2 * math.Pi * cutoffFrequency)
	dt := 1.0 / sampleRate
	alpha := dt / (rc + dt)

	filteredSignal := make([]float64, len(input))
	var prevOutput float64 = 0

	for i, x := range input {
		if i == 0 {
			filteredSignal[i] = x * alpha
		} else {

			filteredSignal[i] = alpha*x + (1-alpha)*prevOutput
		}
		prevOutput = filteredSignal[i]
	}
	return filteredSignal
}

// Downsample downsamples the input audio from originalSampleRate to targetSampleRate
func Downsample(input []float64, originalSampleRate, targetSampleRate int) ([]float64, error) {
	if targetSampleRate <= 0 || originalSampleRate <= 0 {
		return nil, errors.New("sample rates must be positive")
	}
	if targetSampleRate > originalSampleRate {
		return nil, errors.New("target sample rate must be less than or equal to original sample rate")
	}

	ratio := originalSampleRate / targetSampleRate
	if ratio <= 0 {
		return nil, errors.New("invalid ratio calculated from sample rates")
	}

	resampled := make([]float64, 0, len(input)/ratio)
	for i := 0; i < len(input); i += ratio {
		end := i + ratio
		if end > len(input) {
			end = len(input)
		}

		sum := 0.0
		for j := i; j < end; j++ {
			sum += input[j]
		}
		resampled = append(resampled, sum/float64(end-i))
	}

	return resampled, nil
}

// Peak represents a significant point in the spectrogram.
type Peak struct {
	Freq float64 // frequency in Hz
	Time float64 // time in seconds
}

// ExtractPeaks analyzes a spectrogram and extracts significant peaks
// in the frequency domain over time.
func ExtractPeaks(spectrogram [][]float64, audioDuration float64, sampleRate int, cfg FingerprintConfig) []Peak {
	if len(spectrogram) < 1 {
		return []Peak{}
	}

	type bandMax struct {
		mag     float64
		freqIdx int
	}

	effectiveSampleRate := float64(sampleRate) / float64(cfg.DSPRatio)
	freqResolution := effectiveSampleRate / float64(cfg.WindowSize)
	frameDuration := audioDuration / float64(len(spectrogram))

	halfWindow := cfg.WindowSize / 2

	var peaks []Peak
	for frameIdx, frame := range spectrogram {
		var maxMags []float64
		var freqIndices []int

		for _, band := range cfg.FreqBands {
			hi := band[1]
			if hi > halfWindow {
				hi = halfWindow
			}
			if hi > len(frame) {
				hi = len(frame)
			}
			if band[0] >= hi {
				continue
			}

			var best bandMax
			for idx := band[0]; idx < hi; idx++ {
				if frame[idx] > best.mag {
					best = bandMax{frame[idx], idx}
				}
			}

			maxMags = append(maxMags, best.mag)
			freqIndices = append(freqIndices, best.freqIdx)
		}

		if len(maxMags) == 0 {
			continue
		}

		var sum float64
		for _, m := range maxMags {
			sum += m
		}
		avg := sum / float64(len(maxMags))

		for i, mag := range maxMags {
			if mag > avg {
				peaks = append(peaks, Peak{
					Time: float64(frameIdx) * frameDuration,
					Freq: float64(freqIndices[i]) * freqResolution,
				})
			}
		}
	}

	return peaks
}
