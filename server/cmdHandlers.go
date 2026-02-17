package main

import (
	"fmt"
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

const (
	SONGS_DIR = "songs"
)

func find(filePath string) {
	log.Printf("[find] fingerprinting %s with chunked processing...", filePath)

	fingerprint, err := shazam.FingerprintAudioChunked(filePath, utils.GenerateUniqueID(), fpConfig)
	if err != nil {
		fmt.Println("error generating fingerprint:", err)
		return
	}

	sampleFingerprint := make(map[uint32]uint32, len(fingerprint))
	for address, couple := range fingerprint {
		sampleFingerprint[address] = couple.AnchorTimeMs
	}

	log.Printf("[find] searching database with %d fingerprints...", len(sampleFingerprint))

	matches, searchDuration, err := shazam.FindMatchesFGP(sampleFingerprint)
	if err != nil {
		fmt.Println("error finding matches:", err)
		return
	}

	if len(matches) == 0 {
		fmt.Println("\nno match found.")
		fmt.Printf("\nsearch took: %s\n", searchDuration)
		return
	}

	topMatches := matches
	if len(matches) >= 20 {
		fmt.Println("top 20 matches:")
		topMatches = matches[:20]
	} else {
		fmt.Println("matches:")
	}

	for _, match := range topMatches {
		fmt.Printf("\t- %s by %s, score: %.2f\n",
			match.SongTitle, match.SongArtist, match.Score)
	}

	fmt.Printf("\nsearch took: %s\n", searchDuration)
	topMatch := topMatches[0]
	fmt.Printf("\nfinal prediction: %s by %s, score: %.2f\n",
		topMatch.SongTitle, topMatch.SongArtist, topMatch.Score)
}

func serve(protocol, port string) {
	protocol = strings.ToLower(protocol)

	mux := http.NewServeMux()

	mux.HandleFunc("/api/index", handleIndex)
	mux.HandleFunc("/api/match", handleMatch)
	mux.HandleFunc("/api/stats", handleStats)
	mux.HandleFunc("/api/entries", handleEntries)

	mux.Handle("/", http.FileServer(http.Dir("static")))

	handler := requestLogger(corsMiddleware(mux))

	log.Printf("starting server on port %s (%s)\n", port, protocol)
	if err := http.ListenAndServe(":"+port, handler); err != nil {
		log.Fatalf("server error: %v", err)
	}
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func requestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: 200}
		next.ServeHTTP(rec, r)

		// skip noisy static file / stats polling logs
		if strings.HasPrefix(r.URL.Path, "/api/") {
			log.Printf("[http] %s %s -> %d (%s)", r.Method, r.URL.Path, rec.status, time.Since(start))
		}
	})
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func erase(songsDir string, dbOnly bool, all bool) {
	dbClient, err := db.NewDBClient()
	if err != nil {
		fmt.Printf("error creating DB client: %v\n", err)
		return
	}

	if err := dbClient.DeleteCollection("fingerprints"); err != nil {
		fmt.Printf("error deleting fingerprints: %v\n", err)
	}

	if err := dbClient.DeleteCollection("songs"); err != nil {
		fmt.Printf("error deleting songs: %v\n", err)
	}

	fmt.Println("database cleared")

	if !all {
		fmt.Println("erase complete")
		return
	}

	err = filepath.Walk(songsDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		ext := filepath.Ext(path)
		if ext == ".wav" || ext == ".m4a" || ext == ".mp3" || ext == ".flac" || ext == ".ogg" {
			return os.Remove(path)
		}
		return nil
	})
	if err != nil {
		fmt.Printf("error cleaning files in %s: %v\n", songsDir, err)
	}
	fmt.Println("audio files cleared")
	fmt.Println("erase complete")
}

func save(path string, force bool) {
	fileInfo, err := os.Stat(path)
	if err != nil {
		fmt.Printf("error: %v\n", err)
		return
	}

	if !fileInfo.IsDir() {
		if err := saveEntry(path, force); err != nil {
			fmt.Printf("error saving (%v): %v\n", path, err)
		}
		return
	}

	var filePaths []string
	filepath.Walk(path, func(fp string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			filePaths = append(filePaths, fp)
		}
		return nil
	})

	processFilesConcurrently(filePaths, force)
}

func processFilesConcurrently(filePaths []string, force bool) {
	maxWorkers := runtime.NumCPU() / 2
	numFiles := len(filePaths)

	if numFiles == 0 {
		return
	}
	if numFiles < maxWorkers {
		maxWorkers = numFiles
	}
	if maxWorkers < 1 {
		maxWorkers = 1
	}

	jobs := make(chan string, numFiles)
	results := make(chan error, numFiles)

	for w := 0; w < maxWorkers; w++ {
		go func() {
			for fp := range jobs {
				results <- saveEntry(fp, force)
			}
		}()
	}

	for _, fp := range filePaths {
		jobs <- fp
	}
	close(jobs)

	successCount, errorCount := 0, 0
	for i := 0; i < numFiles; i++ {
		if err := <-results; err != nil {
			fmt.Printf("error: %v\n", err)
			errorCount++
		} else {
			successCount++
		}
	}

	fmt.Printf("\nprocessed %d files: %d successful, %d failed\n", numFiles, successCount, errorCount)
}

func saveEntry(filePath string, force bool) error {
	metadata, err := wav.GetMetadata(filePath)

	title := ""
	author := ""

	if err == nil {
		title = metadata.Format.Tags["title"]
		author = metadata.Format.Tags["artist"]
	}

	if title == "" {
		title = strings.TrimSuffix(filepath.Base(filePath), filepath.Ext(filePath))
	}
	if author == "" {
		author = "unknown"
	}

	_, fpCount, err := processAndSave(filePath, title, author)
	if err != nil {
		return fmt.Errorf("failed to process '%s': %v", filePath, err)
	}

	fmt.Printf("indexed '%s' by '%s' (%d fingerprints)\n", title, author, fpCount)
	return nil
}
