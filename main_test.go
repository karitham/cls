package main

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"
)

func TestCollectFiles(t *testing.T) {
	tests := []struct {
		name          string
		setupFiles    map[string]string
		setupDirs     []string
		expectedCount int
		expectError   bool
	}{
		{
			name: "basic file collection",
			setupFiles: map[string]string{
				"test.txt": "This is a test file",
				"test.go":  "package main\n\nfunc main() {}",
				"test.md":  "# Test\n\nThis is markdown",
			},
			setupDirs:     []string{},
			expectedCount: 3,
			expectError:   false,
		},
		{
			name: "ignore invalid extensions",
			setupFiles: map[string]string{
				"valid.txt":   "valid content",
				"invalid.bin": "binary content",
				"invalid.exe": "executable",
			},
			setupDirs:     []string{},
			expectedCount: 1,
			expectError:   false,
		},
		{
			name: "ignore hidden files",
			setupFiles: map[string]string{
				"visible.txt": "visible content",
				".hidden.txt": "hidden content",
				".hidden.go":  "hidden go",
			},
			setupDirs:     []string{},
			expectedCount: 1,
			expectError:   false,
		},
		{
			name: "collect from subdirectories",
			setupFiles: map[string]string{
				"root.txt":       "root file",
				"subdir/sub.txt": "sub file",
			},
			setupDirs:     []string{"subdir"},
			expectedCount: 2,
			expectError:   false,
		},
		{
			name:          "empty directory",
			setupFiles:    map[string]string{},
			setupDirs:     []string{},
			expectedCount: 0,
			expectError:   false,
		},
		{
			name: "skip node_modules",
			setupFiles: map[string]string{
				"valid.txt":            "valid content",
				"node_modules/file.js": "ignored",
				"build/output.txt":     "collected",
				"src/main.go":          "valid",
			},
			setupDirs:     []string{"node_modules", "build", "src"},
			expectedCount: 3, // valid.txt, src/main.go, build/output.txt
			expectError:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tempDir, err := os.MkdirTemp("", "cls_test")
			if err != nil {
				t.Fatalf("Failed to create temp dir: %v", err)
			}
			t.Cleanup(func() { os.RemoveAll(tempDir) })

			logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
			for _, dir := range tt.setupDirs {
				dirPath := filepath.Join(tempDir, dir)
				if err := os.MkdirAll(dirPath, 0755); err != nil {
					t.Fatalf("Failed to create subdir %s: %v", dir, err)
				}
			}
			for name, content := range tt.setupFiles {
				path := filepath.Join(tempDir, name)
				if err := os.WriteFile(path, []byte(content), 0644); err != nil {
					t.Fatalf("Failed to write test file %s: %v", name, err)
				}
			}
			files, err := collectFiles(tempDir, logger)
			if tt.expectError && err == nil {
				t.Error("Expected error but got none")
				return
			}
			if !tt.expectError && err != nil {
				t.Fatalf("collectFiles failed: %v", err)
			}

			if len(files) != tt.expectedCount {
				t.Errorf("Expected %d files, got %d", tt.expectedCount, len(files))
				for _, f := range files {
					t.Logf("Collected: %s", f.Path)
				}
			}
			for _, file := range files {
				if file.Content == "" {
					t.Errorf("File %s has empty content", file.Path)
				}
				if file.Name == "" {
					t.Errorf("File %s has empty name", file.Path)
				}
				if file.Path == "" {
					t.Errorf("File %s has empty path", file.Path)
				}
				if file.Size < 0 {
					t.Errorf("File %s has negative size: %d", file.Path, file.Size)
				}
			}
		})
	}
}
