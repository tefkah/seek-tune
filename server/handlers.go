package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"song-recognition/db"
	"song-recognition/shazam"
	"song-recognition/utils"
	"song-recognition/wav"
	"strings"
	"time"
)

const maxUploadSize = 5000 << 20 // 5 GB

var fpConfig = shazam.DefaultAudiobookConfig()

type indexResponse struct {
	Title           string `json:"title"`
	Author          string `json:"author"`
	Fingerprints    int    `json:"fingerprints"`
	StorageEstimate string `json:"storageEstimate"`
	DurationSec     int    `json:"durationSec"`
}

type matchResult struct {
	Title  string  `json:"title"`
	Author string  `json:"author"`
	Score  float64 `json:"score"`
}

type statsResponse struct {
	TotalEntries      int    `json:"totalEntries"`
	TotalFingerprints int    `json:"totalFingerprints"`
	StorageEstimate   string `json:"storageEstimate"`
}

type entryResponse struct {
	ID     uint32 `json:"id"`
	Title  string `json:"title"`
	Author string `json:"author"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	log.Printf("[error] %d: %s", status, msg)
	writeJSON(w, status, map[string]string{"error": msg})
}

func logMemUsage(label string) {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	log.Printf("[mem] %s: alloc=%s, sys=%s, heap_in_use=%s",
		label, formatBytes(int64(m.Alloc)), formatBytes(int64(m.Sys)), formatBytes(int64(m.HeapInuse)))
}

func formatBytes(b int64) string {
	switch {
	case b >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(b)/float64(1<<30))
	case b >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(b)/float64(1<<20))
	case b >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(b)/float64(1<<10))
	default:
		return fmt.Sprintf("%d B", b)
	}
}

func processAndSave(filePath, title, author string) (uint32, int, error) {
	log.Printf("[process] registering '%s' by '%s' in database", title, author)

	dbClient, err := db.NewDBClient()
	if err != nil {
		return 0, 0, fmt.Errorf("failed to create DB client: %v", err)
	}
	defer dbClient.Close()

	songID, err := dbClient.RegisterSong(title, author, "")
	if err != nil {
		return 0, 0, fmt.Errorf("failed to register entry: %v", err)
	}
	log.Printf("[process] registered with songID=%d, starting chunked fingerprinting...", songID)

	logMemUsage("before fingerprint")
	fpStart := time.Now()

	fingerprint, err := shazam.FingerprintAudioChunked(filePath, songID, fpConfig)
	if err != nil {
		dbClient.DeleteSongByID(songID)
		return 0, 0, fmt.Errorf("failed to fingerprint: %v", err)
	}
	log.Printf("[process] fingerprinting done: %d fingerprints in %s", len(fingerprint), time.Since(fpStart))
	logMemUsage("after fingerprint")

	log.Printf("[process] storing %d fingerprints in database...", len(fingerprint))
	storeStart := time.Now()
	if err := dbClient.StoreFingerprints(fingerprint); err != nil {
		dbClient.DeleteSongByID(songID)
		return 0, 0, fmt.Errorf("failed to store fingerprints: %v", err)
	}
	log.Printf("[process] fingerprints stored in %s", time.Since(storeStart))

	return songID, len(fingerprint), nil
}

func saveUploadedFile(r *http.Request) (string, string, int64, error) {
	file, header, err := r.FormFile("file")
	if err != nil {
		return "", "", 0, fmt.Errorf("no file provided: %v", err)
	}
	defer file.Close()

	if err := utils.CreateFolder("tmp"); err != nil {
		return "", "", 0, fmt.Errorf("failed to create tmp dir: %v", err)
	}

	tmpPath := filepath.Join("tmp", header.Filename)
	dst, err := os.Create(tmpPath)
	if err != nil {
		return "", "", 0, fmt.Errorf("failed to create temp file: %v", err)
	}
	defer dst.Close()

	written, err := io.Copy(dst, file)
	if err != nil {
		return "", "", 0, fmt.Errorf("failed to write file: %v", err)
	}

	return tmpPath, header.Filename, written, nil
}

func handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	reqStart := time.Now()
	log.Printf("[index] received request from %s", r.RemoteAddr)

	r.Body = http.MaxBytesReader(w, r.Body, maxUploadSize)
	if err := r.ParseMultipartForm(maxUploadSize); err != nil {
		writeError(w, http.StatusBadRequest, "file too large or invalid form")
		return
	}

	tmpPath, filename, fileSize, err := saveUploadedFile(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	defer os.Remove(tmpPath)

	log.Printf("[index] file saved: %s (%s)", filename, formatBytes(fileSize))

	title := r.FormValue("title")
	author := r.FormValue("author")

	metadata, metaErr := wav.GetMetadata(tmpPath)
	if metaErr != nil {
		log.Printf("[index] warning: could not read metadata from %s: %v", filename, metaErr)
	}

	if metaErr == nil {
		if author == "" {
			if a := metadata.Format.Tags["artist"]; a != "" {
				author = a
			}
		}
		if title == "" {
			if t := metadata.Format.Tags["title"]; t != "" {
				title = t
			}
		}
	}

	if title == "" {
		title = strings.TrimSuffix(filename, filepath.Ext(filename))
	}
	if author == "" {
		author = "unknown"
	}

	log.Printf("[index] title=%q, author=%q", title, author)

	dbClient, err := db.NewDBClient()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	defer dbClient.Close()

	key := utils.GenerateSongKey(title, author)
	_, exists, _ := dbClient.GetSongByKey(key)
	if exists {
		writeError(w, http.StatusConflict, fmt.Sprintf("'%s' by '%s' already exists", title, author))
		return
	}

	dur, _ := wav.GetAudioDuration(tmpPath)
	log.Printf("[index] audio duration: %.0f seconds (%.1f hours)", dur, dur/3600)

	logMemUsage("before processing")
	songID, fpCount, err := processAndSave(tmpPath, title, author)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	logMemUsage("after processing")

	_ = songID

	resp := indexResponse{
		Title:           title,
		Author:          author,
		Fingerprints:    fpCount,
		StorageEstimate: formatBytes(int64(fpCount) * 20),
		DurationSec:     int(dur),
	}

	log.Printf("[index] completed %q: %d fingerprints, %s total time", title, fpCount, time.Since(reqStart))
	writeJSON(w, http.StatusOK, resp)
}

func handleMatch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	reqStart := time.Now()
	log.Printf("[match] received request from %s", r.RemoteAddr)

	r.Body = http.MaxBytesReader(w, r.Body, maxUploadSize)
	if err := r.ParseMultipartForm(maxUploadSize); err != nil {
		writeError(w, http.StatusBadRequest, "file too large or invalid form")
		return
	}

	tmpPath, filename, fileSize, err := saveUploadedFile(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	defer os.Remove(tmpPath)

	log.Printf("[match] file saved: %s (%s)", filename, formatBytes(fileSize))
	logMemUsage("before processing")

	log.Printf("[match] fingerprinting sample with chunked processing...")
	fpStart := time.Now()
	fingerprint, err := shazam.FingerprintAudioChunked(tmpPath, utils.GenerateUniqueID(), fpConfig)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("fingerprint error: %v", err))
		return
	}
	log.Printf("[match] fingerprinted: %d entries in %s", len(fingerprint), time.Since(fpStart))
	logMemUsage("after fingerprint")

	sampleFP := make(map[uint32]uint32, len(fingerprint))
	for addr, couple := range fingerprint {
		sampleFP[addr] = couple.AnchorTimeMs
	}

	log.Printf("[match] searching database for matches...")
	matches, searchDuration, err := shazam.FindMatchesFGP(sampleFP)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("match error: %v", err))
		return
	}
	log.Printf("[match] search done: %d matches (db query: %s)", len(matches), searchDuration)

	limit := 20
	if len(matches) < limit {
		limit = len(matches)
	}

	results := make([]matchResult, 0, limit)
	for _, m := range matches[:limit] {
		results = append(results, matchResult{
			Title:  m.SongTitle,
			Author: m.SongArtist,
			Score:  m.Score,
		})
	}

	log.Printf("[match] completed in %s, returning %d results", time.Since(reqStart), len(results))
	writeJSON(w, http.StatusOK, map[string]any{
		"matches":            results,
		"searchTimeMs":       searchDuration.Milliseconds(),
		"sampleFingerprints": len(sampleFP),
	})
}

func handleStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	dbClient, err := db.NewDBClient()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	defer dbClient.Close()

	totalSongs, _ := dbClient.TotalSongs()
	totalFP, _ := dbClient.TotalFingerprints()

	writeJSON(w, http.StatusOK, statsResponse{
		TotalEntries:      totalSongs,
		TotalFingerprints: totalFP,
		StorageEstimate:   formatBytes(int64(totalFP) * 20),
	})
}

func handleEntries(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	dbClient, err := db.NewDBClient()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	defer dbClient.Close()

	songs, err := dbClient.GetAllSongs()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list entries")
		return
	}

	entries := make([]entryResponse, 0, len(songs))
	for _, s := range songs {
		entries = append(entries, entryResponse{ID: s.ID, Title: s.Title, Author: s.Artist})
	}

	writeJSON(w, http.StatusOK, entries)
}
