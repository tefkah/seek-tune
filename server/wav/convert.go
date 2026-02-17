package wav

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"song-recognition/utils"
	"strconv"
	"strings"
	"time"
)

// ConvertToWAV converts an input audio file to WAV format with specified channels.
func ConvertToWAV(inputFilePath string) (wavFilePath string, err error) {
	_, err = os.Stat(inputFilePath)
	if err != nil {
		return "", fmt.Errorf("input file does not exist: %v", err)
	}

	to_stereoStr := utils.GetEnv("FINGERPRINT_STEREO", "false")
	to_stereo, err := strconv.ParseBool(to_stereoStr)
	if err != nil {
		return "", fmt.Errorf("failed to convert env variable (%s) to bool: %v", "FINGERPRINT_STEREO", err)
	}

	channels := 1
	if to_stereo {
		channels = 2
	}

	fileExt := filepath.Ext(inputFilePath)
	if fileExt != ".wav" {
		defer os.Remove(inputFilePath)
	}

	outputFile := strings.TrimSuffix(inputFilePath, fileExt) + ".wav"

	// Output file may already exists. If it does FFmpeg will fail as
	// it cannot edit existing files in-place. Use a temporary file.
	tmpFile := filepath.Join(filepath.Dir(outputFile), "tmp_"+filepath.Base(outputFile))
	defer os.Remove(tmpFile)

	cmd := exec.Command(
		"ffmpeg",
		"-y",
		"-i", inputFilePath,
		"-c", "pcm_s16le",
		"-ar", "44100",
		"-ac", fmt.Sprint(channels),
		tmpFile,
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("failed to convert to WAV: %v, output %v", err, string(output))
	}

	// Rename the temporary file to the output file
	err = utils.MoveFile(tmpFile, outputFile)
	if err != nil {
		return "", fmt.Errorf("failed to rename temporary file to output file: %v", err)
	}

	return outputFile, nil
}

// ReformatWAV converts a given WAV file to the specified number of channels,
// either mono (1 channel) or stereo (2 channels).
func ReformatWAV(inputFilePath string, channels int) (reformatedFilePath string, errr error) {
	if channels < 1 || channels > 2 {
		channels = 1
	}

	fileExt := filepath.Ext(inputFilePath)
	outputFile := strings.TrimSuffix(inputFilePath, fileExt) + "rfm.wav"

	cmd := exec.Command(
		"ffmpeg",
		"-y",
		"-i", inputFilePath,
		"-c", "pcm_s16le",
		"-ar", "44100",
		"-ac", fmt.Sprint(channels),
		outputFile,
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("failed to convert to WAV: %v, output %v", err, string(output))
	}

	return outputFile, nil
}

// ExtractChunkAsWAV uses ffmpeg to extract a time segment from any audio
// file and write it as a 16-bit PCM mono WAV. the result is a small
// temporary file bounded by durationSec regardless of original file size.
func ExtractChunkAsWAV(inputPath string, startSec, durationSec float64) (string, error) {
	if err := utils.CreateFolder("tmp"); err != nil {
		return "", err
	}

	outputFile := filepath.Join("tmp", fmt.Sprintf("chunk_%d_%.0f.wav", time.Now().UnixNano(), startSec))

	cmd := exec.Command(
		"ffmpeg", "-y",
		"-ss", fmt.Sprintf("%.3f", startSec),
		"-t", fmt.Sprintf("%.3f", durationSec),
		"-i", inputPath,
		"-c", "pcm_s16le",
		"-ar", "44100",
		"-ac", "1",
		outputFile,
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("ffmpeg chunk extraction failed: %v, output: %s", err, output)
	}

	return outputFile, nil
}

// GetAudioDuration returns the duration in seconds of any audio file
// by calling ffprobe.
func GetAudioDuration(inputPath string) (float64, error) {
	cmd := exec.Command(
		"ffprobe",
		"-v", "quiet",
		"-show_entries", "format=duration",
		"-of", "default=noprint_wrappers=1:nokey=1",
		inputPath,
	)

	out, err := cmd.Output()
	if err != nil {
		return 0, fmt.Errorf("ffprobe duration query failed: %v", err)
	}

	return strconv.ParseFloat(strings.TrimSpace(string(out)), 64)
}
