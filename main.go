package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"slices"
	"strings"

	"github.com/karitham/cls/dirextractor"
)

func main() {
	var (
		chromaURL  = flag.String("url", "http://localhost:8000", "ChromaDB server URL")
		collection = flag.String("collection", "files", "ChromaDB collection name")
	)

	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	if len(flag.Args()) < 1 {
		fmt.Println("Usage: cls [command] [options]")
		fmt.Println("Commands:")
		fmt.Println("  index <filepath>  - Index a file or directory")
		fmt.Println("  query <search>     - Query the indexed content")
		fmt.Println("  delete             - Delete the collection")
		fmt.Println("Flags:")
		flag.PrintDefaults()
		os.Exit(1)
	}

	command := flag.Args()[0]

	switch command {
	case "index":
		if len(flag.Args()) < 2 {
			logger.Error("Please provide a filepath to index")
			os.Exit(1)
		}
		filepath := flag.Args()[1]
		indexFile(*chromaURL, *collection, filepath, logger)
	case "query":
		if len(flag.Args()) < 2 {
			logger.Error("Please provide a search query")
			os.Exit(1)
		}
		query := flag.Args()[1]
		queryDB(*chromaURL, *collection, query, logger)
	case "delete":
		deleteCollection(*chromaURL, *collection, logger)
	default:
		logger.Error("Unknown command", "command", command)
		os.Exit(1)
	}
}

func indexFile(chromaURL, collection, targetPath string, logger *slog.Logger) {
	ctx := context.Background()

	client, err := NewChromaClient(chromaURL, logger)
	if err != nil {
		logger.Error("Failed to create ChromaDB client", "error", err)
		os.Exit(1)
	}
	defer client.Close()

	coll, err := client.GetOrCreateCollection(ctx, collection)
	if err != nil {
		logger.Error("Failed to get/create collection", "error", err)
		os.Exit(1)
	}

	files := slices.Collect(dirextractor.New(
		targetPath,
		dirextractor.WithExtensions(dirextractor.DefaultExtractionExtensions),
		dirextractor.WithIgnoreHidden(),
		dirextractor.WithIgnoreRegs(".*node_modules.*"),
	).Files())

	err = coll.AddDocuments(ctx, files)
	if err != nil {
		logger.Error("Failed to add documents to collection", "error", err)
		os.Exit(1)
	}

	fmt.Printf("Successfully indexed %d files\n", len(files))
}

func queryDB(chromaURL, collection, query string, logger *slog.Logger) {
	ctx := context.Background()

	client, err := NewChromaClient(chromaURL, logger)
	if err != nil {
		logger.Error("Failed to create ChromaDB client", "error", err)
		os.Exit(1)
	}
	defer client.Close()

	var coll Collection
	coll, err = client.GetCollection(ctx, collection)
	if err != nil {
		logger.Error("Failed to get collection", "error", err)
		os.Exit(1)
	}

	var results []QueryResult
	results, err = coll.Query(ctx, query, 5)
	if err != nil {
		logger.Error("Failed to query collection", "error", err)
		os.Exit(1)
	}
	defer client.Close()

	if len(results) == 0 {
		fmt.Println("No results found")
		return
	}

	fmt.Printf("Found %d results:\n\n", len(results))
	for i := len(results) - 1; i >= 0; i-- {
		result := results[i]
		fmt.Printf("File: %s\n", result.FileName)
		fmt.Printf("Path: %s\n", result.Path)
		fmt.Printf("Content:\n%s\n", result.Content)
		fmt.Println(strings.Repeat("-", 50))
	}
}

func deleteCollection(chromaURL, collection string, logger *slog.Logger) {
	ctx := context.Background()

	client, err := NewChromaClient(chromaURL, logger)
	if err != nil {
		logger.Error("Failed to create ChromaDB client", "error", err)
		os.Exit(1)
	}
	defer client.Close()

	err = client.DeleteCollection(ctx, collection)
	if err != nil {
		logger.Error("Failed to delete collection", "error", err)
		os.Exit(1)
	}

	fmt.Printf("Collection '%s' deleted successfully\n", collection)
}
