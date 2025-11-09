package main

import (
	"context"
	"flag"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

func collectFiles(targetPath string, logger *slog.Logger) ([]FileData, error) {
	var files []FileData
	ignorePatterns := readGitignore(targetPath)
	validExtensions := map[string]bool{
		".txt":        true,
		".md":         true,
		".go":         true,
		".py":         true,
		".js":         true,
		".ts":         true,
		".json":       true,
		".yaml":       true,
		".yml":        true,
		".xml":        true,
		".html":       true,
		".css":        true,
		".sh":         true,
		".rs":         true,
		".java":       true,
		".c":          true,
		".cpp":        true,
		".h":          true,
		".hpp":        true,
		".sql":        true,
		".dockerfile": true,
		".gitignore":  true,
		".toml":       true,
		".ini":        true,
		".cfg":        true,
		".conf":       true,
		".nix":        true,
	}

	err := filepath.Walk(targetPath, func(path string, info fs.FileInfo, err error) error {
		if err != nil {
			return err
		}
		relPath, err := filepath.Rel(targetPath, path)
		if err != nil {
			return err
		}
		if info.IsDir() && (info.Name() == "node_modules" || info.Name() == ".git") {
			return filepath.SkipDir
		}
		if shouldIgnore(relPath, ignorePatterns) {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if info.IsDir() || strings.HasPrefix(info.Name(), ".") {
			if info.IsDir() && strings.HasPrefix(info.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if !validExtensions[ext] {
			return nil
		}

		content, err := os.ReadFile(path)
		if err != nil {
			logger.Warn("could not read file", "path", path, "error", err)
			return nil
		}

		files = append(files, FileData{
			Path:    path,
			Name:    info.Name(),
			Content: string(content),
			Size:    info.Size(),
		})

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("error walking filepath: %w", err)
	}

	return files, nil
}
func readGitignore(targetPath string) []string {
	gitignorePath := filepath.Join(targetPath, ".gitignore")
	content, err := os.ReadFile(gitignorePath)
	if err != nil {
		return []string{}
	}

	var patterns []string
	lines := strings.Split(string(content), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		patterns = append(patterns, line)
	}
	return patterns
}
func shouldIgnore(relPath string, patterns []string) bool {
	for _, pattern := range patterns {
		if matchesPattern(relPath, pattern) {
			return true
		}
	}
	return false
}
func matchesPattern(path, pattern string) bool {
	if strings.HasSuffix(pattern, "/") {
		pattern = strings.TrimSuffix(pattern, "/")
		return strings.HasPrefix(path, pattern+"/") || path == pattern
	}
	if strings.Contains(pattern, "*") {
		parts := strings.Split(pattern, "*")
		if len(parts) == 2 {
			return strings.HasPrefix(path, parts[0]) && strings.HasSuffix(path, parts[1])
		}
	}
	return path == pattern || strings.HasPrefix(path, pattern+"/")
}

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

	files, err := collectFiles(targetPath, logger)
	if err != nil {
		logger.Error("Failed to collect files", "error", err)
		os.Exit(1)
	}

	if len(files) == 0 {
		fmt.Println("No files found to index")
		return
	}

	var documents []string
	var ids []string
	var metadatas []FileMetadata

	for _, file := range files {
		fmt.Printf("Indexing: %s\n", file.Path)
		documents = append(documents, file.Content)
		ids = append(ids, strings.ReplaceAll(file.Path, "/", "_"))
		metadatas = append(metadatas, FileMetadata{
			Filename: file.Name,
			Path:     file.Path,
			Size:     file.Size,
		})
	}

	err = coll.AddDocuments(ctx, ids, documents, metadatas)
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

	coll, err = client.GetCollection(ctx, collection)
	if err != nil {
		logger.Error("Failed to get collection", "error", err)
		os.Exit(1)
	}

	results, err = coll.Query(ctx, query, 5)
	if err != nil {
		logger.Error("Failed to query collection", "error", err)
		os.Exit(1)
	}

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
	defer client.Close()

	err = client.DeleteCollection(ctx, collection)
	if err != nil {
		logger.Error("Failed to delete collection", "error", err)
		os.Exit(1)
	}

	fmt.Printf("Collection '%s' deleted successfully\n", collection)
}
