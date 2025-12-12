package rawdb

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseDirBlockNumber(t *testing.T) {
	tests := []struct {
		name        string
		dirPath     string
		wantStart   uint64
		wantEnd     uint64
		wantErr     bool
		expectedErr string
	}{
		{
			name:      "valid directory path",
			dirPath:   "/path/to/incr-1000-1999",
			wantStart: 1000,
			wantEnd:   1999,
			wantErr:   false,
		},
		{
			name:      "valid directory path with different numbers",
			dirPath:   "/some/path/incr-5000-5999",
			wantStart: 5000,
			wantEnd:   5999,
			wantErr:   false,
		},
		{
			name:      "valid directory path with single digit",
			dirPath:   "/test/incr-1-9",
			wantStart: 1,
			wantEnd:   9,
			wantErr:   false,
		},
		{
			name:        "invalid directory name format",
			dirPath:     "/path/to/invalid_dir",
			wantErr:     true,
			expectedErr: "invalid directory name format: invalid_dir",
		},
		{
			name:        "invalid directory name with wrong pattern",
			dirPath:     "/path/to/incr-abc-def",
			wantErr:     true,
			expectedErr: "invalid directory name format: incr-abc-def",
		},
		{
			name:        "invalid directory name with missing parts",
			dirPath:     "/path/to/incr_1000",
			wantErr:     true,
			expectedErr: "invalid directory name format: incr_1000",
		},
		{
			name:        "invalid start block number",
			dirPath:     "/path/to/incr_abc-1999",
			wantErr:     true,
			expectedErr: "invalid directory name format: incr_abc-1999",
		},
		{
			name:        "invalid end block number",
			dirPath:     "/path/to/incr-1000_def",
			wantErr:     true,
			expectedErr: "invalid directory name format: incr-1000_def",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			start, end, err := parseDirBlockNumber(tt.dirPath)

			if tt.wantErr {
				if err == nil {
					t.Fatalf("parseDirBlockNumber() expected error but got none")
				}
				if tt.expectedErr != "" && err.Error() != tt.expectedErr {
					t.Fatalf("parseDirBlockNumber() error = %v, expected %v", err.Error(), tt.expectedErr)
				}
			}

			if start != tt.wantStart {
				t.Fatalf("parseDirBlockNumber() start = %v, want %v", start, tt.wantStart)
			}
			if end != tt.wantEnd {
				t.Fatalf("parseDirBlockNumber() end = %v, want %v", end, tt.wantEnd)
			}
		})
	}
}

func TestFindLatestIncrDir(t *testing.T) {
	tests := []struct {
		name        string
		setupDirs   []string
		startBlock  uint64
		blockLimit  uint64
		expectedDir string
		wantErr     bool
	}{
		{
			name:        "no existing directories - should create first one",
			setupDirs:   []string{},
			startBlock:  1000,
			blockLimit:  1000,
			expectedDir: "incr-1000-1999",
			wantErr:     false,
		},
		{
			name: "existing directories - should return latest",
			setupDirs: []string{
				"incr-1000-1999",
				"incr-2000-2999",
				"incr-3000-3999",
			},
			startBlock:  1000,
			blockLimit:  1000,
			expectedDir: "incr-3000-3999",
			wantErr:     false,
		},
		{
			name: "existing directories with gaps - should return latest",
			setupDirs: []string{
				"incr-1000-1999",
				"incr-3000-3999",
				"incr-5000-5999",
			},
			startBlock:  1000,
			blockLimit:  1000,
			expectedDir: "incr-5000-5999",
			wantErr:     false,
		},
		{
			name: "existing directories with invalid names - should ignore invalid ones",
			setupDirs: []string{
				"incr-1000-1999",
				"invalid_dir",
				"incr-2000-2999",
				"another_invalid",
			},
			startBlock:  1000,
			blockLimit:  1000,
			expectedDir: "incr-2000-2999",
			wantErr:     false,
		},
		{
			name: "existing directories with invalid block numbers - should ignore invalid ones",
			setupDirs: []string{
				"incr-1000-1999",
				"incr_abc-def",
				"incr-2000-2999",
			},
			startBlock:  1000,
			blockLimit:  1000,
			expectedDir: "incr-2000-2999",
			wantErr:     false,
		},
		{
			name: "single existing directory",
			setupDirs: []string{
				"incr-1000-1999",
			},
			startBlock:  1000,
			blockLimit:  1000,
			expectedDir: "incr-1000-1999",
			wantErr:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create independent test directory for each test case
			testDir := t.TempDir()

			// Create setup directories
			for _, dirName := range tt.setupDirs {
				dirPath := filepath.Join(testDir, dirName)
				if err := os.MkdirAll(dirPath, 0755); err != nil {
					t.Fatalf("Failed to create setup directory %s: %v", dirPath, err)
				}
			}

			// Test the function
			gotDir, err := findLatestIncrDir(testDir, tt.startBlock, tt.blockLimit)

			if tt.wantErr {
				if err == nil {
					t.Fatalf("findLatestIncrDir() expected error but got none")
				}
				return
			}

			if err != nil {
				t.Fatalf("findLatestIncrDir() unexpected error = %v", err)
			}

			// Verify the result
			expectedDir := filepath.Join(testDir, tt.expectedDir)
			if gotDir != expectedDir {
				t.Fatalf("findLatestIncrDir() = %v, want %v", gotDir, expectedDir)
			}
		})
	}
}

func TestFindLatestIncrDirWithFiles(t *testing.T) {
	// Test with files in directory (should be ignored)
	tempDir := t.TempDir()

	// Create a file in the directory
	filePath := filepath.Join(tempDir, "test_file.txt")
	if err := os.WriteFile(filePath, []byte("test"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Create a valid directory
	validDir := filepath.Join(tempDir, "incr-1000-1999")
	if err := os.MkdirAll(validDir, 0755); err != nil {
		t.Fatalf("Failed to create valid directory: %v", err)
	}

	// Test that files are ignored and only directories are considered
	gotDir, err := findLatestIncrDir(tempDir, 1000, 1000)
	if err != nil {
		t.Errorf("findLatestIncrDir() unexpected error = %v", err)
		return
	}

	expectedDir := filepath.Join(tempDir, "incr-1000-1999")
	if gotDir != expectedDir {
		t.Errorf("findLatestIncrDir() = %v, want %v", gotDir, expectedDir)
	}
}

func TestGetAllIncrDirs(t *testing.T) {
	tests := []struct {
		name         string
		setupDirs    []string
		expectedDirs []IncrDirInfo
		wantErr      bool
	}{
		{
			name:         "no directories",
			setupDirs:    []string{},
			expectedDirs: []IncrDirInfo{},
			wantErr:      false,
		},
		{
			name: "valid incremental directories",
			setupDirs: []string{
				"incr-1000-1999",
				"incr-2000-2999",
				"incr-3000-3999",
			},
			expectedDirs: []IncrDirInfo{
				{Name: "incr-1000-1999", StartBlockNum: 1000, EndBlockNum: 1999},
				{Name: "incr-2000-2999", StartBlockNum: 2000, EndBlockNum: 2999},
				{Name: "incr-3000-3999", StartBlockNum: 3000, EndBlockNum: 3999},
			},
			wantErr: false,
		},
		{
			name: "mixed valid and invalid directories",
			setupDirs: []string{
				"incr-1000-1999",
				"invalid_dir",
				"incr-2000-2999",
				"another_invalid",
				"incr-3000-3999",
			},
			expectedDirs: []IncrDirInfo{
				{Name: "incr-1000-1999", StartBlockNum: 1000, EndBlockNum: 1999},
				{Name: "incr-2000-2999", StartBlockNum: 2000, EndBlockNum: 2999},
				{Name: "incr-3000-3999", StartBlockNum: 3000, EndBlockNum: 3999},
			},
			wantErr: false,
		},
		{
			name: "directories with invalid block numbers",
			setupDirs: []string{
				"incr-1000-1999",
				"incr_abc_def",
				"incr-2000-2999",
				"incr-xyz-123",
			},
			expectedDirs: []IncrDirInfo{
				{Name: "incr-1000-1999", StartBlockNum: 1000, EndBlockNum: 1999},
				{Name: "incr-2000-2999", StartBlockNum: 2000, EndBlockNum: 2999},
			},
			wantErr: false,
		},
		{
			name: "unsorted directories - should return sorted",
			setupDirs: []string{
				"incr-3000-3999",
				"incr-1000-1999",
				"incr-2000-2999",
			},
			expectedDirs: []IncrDirInfo{
				{Name: "incr-1000-1999", StartBlockNum: 1000, EndBlockNum: 1999},
				{Name: "incr-2000-2999", StartBlockNum: 2000, EndBlockNum: 2999},
				{Name: "incr-3000-3999", StartBlockNum: 3000, EndBlockNum: 3999},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create independent test directory for each test case
			testDir := t.TempDir()

			// Create setup directories
			for _, dirName := range tt.setupDirs {
				dirPath := filepath.Join(testDir, dirName)
				if err := os.MkdirAll(dirPath, 0755); err != nil {
					t.Fatalf("Failed to create setup directory %s: %v", dirPath, err)
				}
			}

			// Test the function
			gotDirs, err := GetAllIncrDirs(testDir)

			if tt.wantErr {
				if err == nil {
					t.Fatalf("GetAllIncrDirs() expected error but got none")
				}
				return
			}
			if err != nil {
				t.Fatalf("GetAllIncrDirs() unexpected error = %v", err)
				return
			}
			// Verify the result
			if len(gotDirs) != len(tt.expectedDirs) {
				t.Fatalf("GetAllIncrDirs() returned %d directories, want %d", len(gotDirs), len(tt.expectedDirs))
			}

			// Build expected directories with full paths
			expectedDirs := make([]IncrDirInfo, len(tt.expectedDirs))
			for i, expected := range tt.expectedDirs {
				expectedDirs[i] = IncrDirInfo{
					Name:          expected.Name,
					Path:          filepath.Join(testDir, expected.Name),
					StartBlockNum: expected.StartBlockNum,
					EndBlockNum:   expected.EndBlockNum,
				}
			}

			verifyDirs(t, gotDirs, expectedDirs)
		})
	}
}

// verifyDirs is a helper function to verify directory information
func verifyDirs(t *testing.T, gotDirs, expectedDirs []IncrDirInfo) {
	for i, gotDir := range gotDirs {
		expectedDir := expectedDirs[i]
		if gotDir.Name != expectedDir.Name {
			t.Fatalf("GetAllIncrDirs()[%d].Name = %v, want %v", i, gotDir.Name, expectedDir.Name)
		}
		if gotDir.Path != expectedDir.Path {
			t.Fatalf("GetAllIncrDirs()[%d].Path = %v, want %v", i, gotDir.Path, expectedDir.Path)
		}
		if gotDir.StartBlockNum != expectedDir.StartBlockNum {
			t.Fatalf("GetAllIncrDirs()[%d].StartBlockNum = %v, want %v", i, gotDir.StartBlockNum, expectedDir.StartBlockNum)
		}
		if gotDir.EndBlockNum != expectedDir.EndBlockNum {
			t.Fatalf("GetAllIncrDirs()[%d].EndBlockNum = %v, want %v", i, gotDir.EndBlockNum, expectedDir.EndBlockNum)
		}
	}
}

func TestGetAllIncrDirsWithFiles(t *testing.T) {
	// Test with files in directory (should be ignored)
	tempDir := t.TempDir()

	// Create files in the directory
	files := []string{"test1.txt", "test2.dat", "incr_1000_1999.txt"}
	for _, fileName := range files {
		filePath := filepath.Join(tempDir, fileName)
		if err := os.WriteFile(filePath, []byte("test"), 0644); err != nil {
			t.Fatalf("Failed to create test file %s: %v", fileName, err)
		}
	}

	// Create a valid directory
	validDir := filepath.Join(tempDir, "incr-1000-1999")
	if err := os.MkdirAll(validDir, 0755); err != nil {
		t.Fatalf("Failed to create valid directory: %v", err)
	}

	// Test that files are ignored and only directories are returned
	gotDirs, err := GetAllIncrDirs(tempDir)
	if err != nil {
		t.Errorf("GetAllIncrDirs() unexpected error = %v", err)
		return
	}

	if len(gotDirs) != 1 {
		t.Errorf("GetAllIncrDirs() returned %d directories, want 1", len(gotDirs))
		return
	}

	expectedDir := IncrDirInfo{
		Name:          "incr-1000-1999",
		Path:          filepath.Join(tempDir, "incr-1000-1999"),
		StartBlockNum: 1000,
		EndBlockNum:   1999,
	}

	if gotDirs[0].Name != expectedDir.Name {
		t.Errorf("GetAllIncrDirs()[0].Name = %v, want %v", gotDirs[0].Name, expectedDir.Name)
	}
	if gotDirs[0].Path != expectedDir.Path {
		t.Errorf("GetAllIncrDirs()[0].Path = %v, want %v", gotDirs[0].Path, expectedDir.Path)
	}
	if gotDirs[0].StartBlockNum != expectedDir.StartBlockNum {
		t.Errorf("GetAllIncrDirs()[0].StartBlockNum = %v, want %v", gotDirs[0].StartBlockNum, expectedDir.StartBlockNum)
	}
	if gotDirs[0].EndBlockNum != expectedDir.EndBlockNum {
		t.Errorf("GetAllIncrDirs()[0].EndBlockNum = %v, want %v", gotDirs[0].EndBlockNum, expectedDir.EndBlockNum)
	}
}

func TestGetAllIncrDirsError(t *testing.T) {
	// Test with non-existent directory
	nonExistentDir := "/non/existent/directory"
	_, err := GetAllIncrDirs(nonExistentDir)
	if err == nil {
		t.Errorf("GetAllIncrDirs() expected error for non-existent directory but got none")
	}
}
