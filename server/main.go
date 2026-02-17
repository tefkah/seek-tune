package main

import (
	"flag"
	"fmt"
	"os"
	"song-recognition/utils"

	"github.com/joho/godotenv"
)

func main() {
	_ = utils.CreateFolder("tmp")
	_ = utils.CreateFolder(SONGS_DIR)

	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}
	_ = godotenv.Load()

	switch os.Args[1] {
	case "find":
		if len(os.Args) < 3 {
			fmt.Println("usage: seek-tune find <path_to_audio_file>")
			os.Exit(1)
		}
		find(os.Args[2])

	case "serve":
		serveCmd := flag.NewFlagSet("serve", flag.ExitOnError)
		protocol := serveCmd.String("proto", "http", "protocol to use (http or https)")
		port := serveCmd.String("p", "5000", "port to use")
		serveCmd.Parse(os.Args[2:])
		serve(*protocol, *port)

	case "erase":
		dbOnly := true
		all := false

		if len(os.Args) > 2 {
			switch os.Args[2] {
			case "db":
				dbOnly = true
			case "all":
				dbOnly = false
				all = true
			default:
				fmt.Println("usage: seek-tune erase [db | all]")
				os.Exit(1)
			}
		}

		erase(SONGS_DIR, dbOnly, all)

	case "save":
		indexCmd := flag.NewFlagSet("save", flag.ExitOnError)
		force := indexCmd.Bool("force", false, "index file even without complete metadata")
		indexCmd.BoolVar(force, "f", false, "index file even without complete metadata (shorthand)")
		indexCmd.Parse(os.Args[2:])
		if indexCmd.NArg() < 1 {
			fmt.Println("usage: seek-tune save [-f|--force] <path_to_file_or_dir>")
			os.Exit(1)
		}
		save(indexCmd.Arg(0), *force)

	default:
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println("usage: seek-tune <command>")
	fmt.Println()
	fmt.Println("commands:")
	fmt.Println("  find  <audio_file>              match a file against the database")
	fmt.Println("  save  [-f] <file_or_dir>        index audio file(s) into the database")
	fmt.Println("  erase [db | all]                clear database (and optionally audio files)")
	fmt.Println("  serve [-proto http] [-p 5000]    start the web server")
}
