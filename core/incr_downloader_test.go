package core

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/triedb"
)

const (
	testURL = "http://test.com"
)

func createTestDB() ethdb.Database {
	return rawdb.NewMemoryDatabase()
}

// Create test HTTP server
func createTestHTTPServer() (*httptest.Server, string) {
	metadata := []IncrMetadata{
		{
			FileName: "test-incr-1000-1999.tar.lz4",
			MD5Sum:   "d41d8cd98f00b204e9800998ecf8427e",
			Size:     1024,
		},
		{
			FileName: "test-incr-2000-2999.tar.lz4",
			MD5Sum:   "d41d8cd98f00b204e9800998ecf8427e",
			Size:     2048,
		},
	}

	testFileContent := "This is a test file content for incremental snapshot download testing."

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path

		switch {
		case path == "/incr_metadata.json":
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(metadata)

		case strings.HasSuffix(path, ".tar.lz4"):
			w.Header().Set("Content-Length", fmt.Sprintf("%d", len(testFileContent)))
			w.Header().Set("Accept-Ranges", "bytes")
			w.Write([]byte(testFileContent))

		default:
			http.NotFound(w, r)
		}
	}))

	return server, server.URL
}

func TestIncrDownloader_HTTPDownload(t *testing.T) {
	server, serverURL := createTestHTTPServer()
	defer server.Close()

	// Setup test environment
	db := createTestDB()
	defer db.Close()
	trieDB := triedb.NewDatabase(db, nil)
	defer trieDB.Close()

	tempDir := t.TempDir()
	downloader := NewIncrDownloader(db, trieDB, serverURL, tempDir, 1000)

	// Create test file info
	file := &IncrFileInfo{
		Metadata: IncrMetadata{
			FileName: "test-incr-1000-1999.tar.lz4",
			MD5Sum:   "d41d8cd98f00b204e9800998ecf8427e",
			Size:     70,
		},
		StartBlock: 1000,
		EndBlock:   1999,
		LocalPath:  filepath.Join(tempDir, "test-incr-1000-2000.tar.lz4"),
	}

	// Test HTTP download
	err := downloader.downloadWithHTTP(file)
	require.NoError(t, err)

	// Verify file downloaded
	_, err = os.Stat(file.LocalPath)
	require.NoError(t, err)

	// Verify file content
	content, err := os.ReadFile(file.LocalPath)
	require.NoError(t, err)
	assert.Contains(t, string(content), "test file content")
}

func TestIncrDownloader_HTTPDownloadWithRangeSupport(t *testing.T) {
	// Create test server with Range support
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		testContent := "This is a test file content for range download testing."

		if strings.HasSuffix(path, ".tar.lz4") {
			w.Header().Set("Content-Length", fmt.Sprintf("%d", len(testContent)))
			w.Header().Set("Accept-Ranges", "bytes")

			// Handle Range requests
			if rangeHeader := r.Header.Get("Range"); rangeHeader != "" {
				// Simple Range handling
				w.WriteHeader(http.StatusPartialContent)
				w.Write([]byte(testContent))
			} else {
				w.Write([]byte(testContent))
			}
		} else {
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	// Setup test environment
	db := createTestDB()
	defer db.Close()
	trieDB := triedb.NewDatabase(db, nil)
	defer trieDB.Close()

	tempDir := t.TempDir()
	downloader := NewIncrDownloader(db, trieDB, server.URL, tempDir, 1000)

	// Create test file info
	file := &IncrFileInfo{
		Metadata: IncrMetadata{
			FileName: "test-incr-1000-1999.tar.lz4",
			MD5Sum:   "d41d8cd98f00b204e9800998ecf8427e",
			Size:     55,
		},
		StartBlock: 1000,
		EndBlock:   1999,
		LocalPath:  filepath.Join(tempDir, "test-incr-1000-1999.tar.lz4"),
	}

	// Test Range-supported download
	err := downloader.downloadWithHTTP(file)
	require.NoError(t, err)

	// Verify file downloaded
	_, err = os.Stat(file.LocalPath)
	require.NoError(t, err)
}

func TestIncrDownloader_HTTPDownloadWithoutRangeSupport(t *testing.T) {
	// Create test server without Range support
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		testContent := "This is a test file content for single-threaded download testing."

		if strings.HasSuffix(path, ".tar.lz4") {
			w.Header().Set("Content-Length", fmt.Sprintf("%d", len(testContent)))
			// Don't set Accept-Ranges header
			w.Write([]byte(testContent))
		} else {
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	// Setup test environment
	db := createTestDB()
	defer db.Close()
	trieDB := triedb.NewDatabase(db, nil)
	defer trieDB.Close()

	tempDir := t.TempDir()
	downloader := NewIncrDownloader(db, trieDB, server.URL, tempDir, 1000)

	// Create test file info
	file := &IncrFileInfo{
		Metadata: IncrMetadata{
			FileName: "test-incr-1000-1999.tar.lz4",
			MD5Sum:   "d41d8cd98f00b204e9800998ecf8427e",
			Size:     1024,
		},
		StartBlock: 1000,
		EndBlock:   1999,
		LocalPath:  filepath.Join(tempDir, "test-incr-1000-1999.tar.lz4"),
	}

	// Test single-threaded download
	err := downloader.downloadWithHTTP(file)
	require.NoError(t, err)

	// Verify file downloaded
	_, err = os.Stat(file.LocalPath)
	require.NoError(t, err)
}

func TestIncrDownloader_HTTPDownloadError(t *testing.T) {
	// Create test server that returns errors
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if strings.HasSuffix(path, ".tar.lz4") {
			// Return 404 error
			http.NotFound(w, r)
		} else {
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	// Setup test environment
	db := createTestDB()
	defer db.Close()
	trieDB := triedb.NewDatabase(db, nil)
	defer trieDB.Close()

	tempDir := t.TempDir()
	downloader := NewIncrDownloader(db, trieDB, server.URL, tempDir, 1000)

	// Create test file info
	file := &IncrFileInfo{
		Metadata: IncrMetadata{
			FileName: "test-incr-1000-1999.tar.lz4",
			MD5Sum:   "d41d8cd98f00b204e9800998ecf8427e",
			Size:     1024,
		},
		StartBlock: 1000,
		EndBlock:   1999,
		LocalPath:  filepath.Join(tempDir, "test-incr-1000-1999.tar.lz4"),
	}

	// Test download error
	err := downloader.downloadWithHTTP(file)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "HTTP error: 404")
}

func TestIncrDownloader_FetchMetadata(t *testing.T) {
	// Create test metadata
	metadata := []IncrMetadata{
		{
			FileName: "test-incr-1000-1999.tar.lz4",
			MD5Sum:   "d41d8cd98f00b204e9800998ecf8427e",
			Size:     1024,
		},
		{
			FileName: "test-incr-2000-2999.tar.lz4",
			MD5Sum:   "d41d8cd98f00b204e9800998ecf8427e",
			Size:     2048,
		},
	}

	// Create test HTTP server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/incr_metadata.json" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(metadata)
		} else {
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	// Setup test environment
	db := createTestDB()
	defer db.Close()
	trieDB := triedb.NewDatabase(db, nil)
	defer trieDB.Close()

	tempDir := t.TempDir()
	downloader := NewIncrDownloader(db, trieDB, server.URL, tempDir, 1000)

	// Test metadata fetching
	fetchedMetadata, err := downloader.fetchMetadata()
	require.NoError(t, err)
	assert.Len(t, fetchedMetadata, 2)
	assert.Equal(t, "test-incr-1000-1999.tar.lz4", fetchedMetadata[0].FileName)
	assert.Equal(t, uint64(1024), fetchedMetadata[0].Size)
}

func TestIncrDownloader_FetchMetadataError(t *testing.T) {
	// Create test server that returns errors
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}))
	defer server.Close()

	// Setup test environment
	db := createTestDB()
	defer db.Close()
	trieDB := triedb.NewDatabase(db, nil)
	defer trieDB.Close()

	tempDir := t.TempDir()
	downloader := NewIncrDownloader(db, trieDB, server.URL, tempDir, 1000)

	// Test metadata fetching error
	_, err := downloader.fetchMetadata()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "HTTP error: 500")
}

func TestIncrDownloader_CheckRangeSupport(t *testing.T) {
	// Create test server with Range support
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "HEAD" {
			w.Header().Set("Accept-Ranges", "bytes")
			w.Header().Set("Content-Length", "1024")
		} else {
			w.Write([]byte("test content"))
		}
	}))
	defer server.Close()

	// Setup test environment
	db := createTestDB()
	defer db.Close()
	trieDB := triedb.NewDatabase(db, nil)
	defer trieDB.Close()

	tempDir := t.TempDir()
	downloader := NewIncrDownloader(db, trieDB, server.URL, tempDir, 1000)

	// Test Range support check
	supportsRange, contentLength, err := downloader.checkRangeSupport(server.URL + "/test-file.tar.lz4")
	require.NoError(t, err)
	assert.True(t, supportsRange)
	assert.Equal(t, int64(1024), contentLength)
}

func TestIncrDownloader_CheckRangeSupportNotSupported(t *testing.T) {
	// Create test server without Range support
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "HEAD" {
			// Don't set Accept-Ranges header
			w.Header().Set("Content-Length", "1024")
		} else {
			w.Write([]byte("test content"))
		}
	}))
	defer server.Close()

	// Setup test environment
	db := createTestDB()
	defer db.Close()
	trieDB := triedb.NewDatabase(db, nil)
	defer trieDB.Close()

	tempDir := t.TempDir()
	downloader := NewIncrDownloader(db, trieDB, server.URL, tempDir, 1000)

	// Test Range support check
	supportsRange, contentLength, err := downloader.checkRangeSupport(server.URL + "/test-file.tar.lz4")
	require.NoError(t, err)
	assert.False(t, supportsRange)
	assert.Equal(t, int64(1024), contentLength)
}

func TestIncrDownloader_DownloadChunk(t *testing.T) {
	// Create test server
	testContent := "This is test content for chunk download testing."
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rangeHeader := r.Header.Get("Range")
		if rangeHeader != "" {
			w.Header().Set("Content-Range", "bytes 0-1023/1024")
			w.WriteHeader(http.StatusPartialContent)
		}
		w.Write([]byte(testContent))
	}))
	defer server.Close()

	// Setup test environment
	db := createTestDB()
	defer db.Close()
	trieDB := triedb.NewDatabase(db, nil)
	defer trieDB.Close()

	tempDir := t.TempDir()
	downloader := NewIncrDownloader(db, trieDB, server.URL, tempDir, 1000)

	// Create test chunk
	chunk := &ChunkInfo{
		Index:    0,
		Start:    0,
		End:      1023,
		TempFile: filepath.Join(tempDir, "test-chunk.part0"),
	}
	progressChan := make(chan *ChunkProgress, 1)

	// Test chunk download
	err := downloader.downloadChunk(server.URL+"/test-file.tar.lz4", chunk, progressChan)
	require.NoError(t, err)

	// Verify chunk file created
	_, err = os.Stat(chunk.TempFile)
	require.NoError(t, err)

	// Verify chunk content
	content, err := os.ReadFile(chunk.TempFile)
	require.NoError(t, err)
	assert.Contains(t, string(content), "test content")
}

func TestIncrDownloader_MergeChunks(t *testing.T) {
	// Setup test environment
	db := createTestDB()
	defer db.Close()
	trieDB := triedb.NewDatabase(db, nil)
	defer trieDB.Close()

	tempDir := t.TempDir()
	downloader := NewIncrDownloader(db, trieDB, testURL, tempDir, 1000)

	// Create test chunks
	chunks := []*ChunkInfo{
		{
			Index:    0,
			Start:    0,
			End:      1023,
			TempFile: filepath.Join(tempDir, "chunk1.part0"),
		},
		{
			Index:    1,
			Start:    1024,
			End:      2047,
			TempFile: filepath.Join(tempDir, "chunk2.part1"),
		},
	}

	// Create test chunk files
	err := os.WriteFile(chunks[0].TempFile, []byte("chunk1 content"), 0644)
	require.NoError(t, err)
	err = os.WriteFile(chunks[1].TempFile, []byte("chunk2 content"), 0644)
	require.NoError(t, err)

	outputPath := filepath.Join(tempDir, "merged-file.tar.lz4")

	// Test chunk merging
	err = downloader.mergeChunks(chunks, outputPath)
	require.NoError(t, err)

	// Verify merged file
	_, err = os.Stat(outputPath)
	require.NoError(t, err)

	// Verify merged content
	content, err := os.ReadFile(outputPath)
	require.NoError(t, err)
	assert.Contains(t, string(content), "chunk1 content")
	assert.Contains(t, string(content), "chunk2 content")
}

func TestIncrDownloader_VerifyAndExtract(t *testing.T) {
	// Setup test environment
	db := createTestDB()
	defer db.Close()
	trieDB := triedb.NewDatabase(db, nil)
	defer trieDB.Close()

	tempDir := t.TempDir()
	testContent := "test file content"
	testFile := filepath.Join(tempDir, "test-file.tar.lz4")
	err := os.WriteFile(testFile, []byte(testContent), 0644)
	require.NoError(t, err)

	file := &IncrFileInfo{
		Metadata: IncrMetadata{
			FileName: "test-file.tar.lz4",
			MD5Sum:   "d41d8cd98f00b204e9800998ecf8427e", // Empty file MD5
			Size:     uint64(len(testContent)),
		},
		LocalPath: testFile,
	}

	downloader := NewIncrDownloader(db, trieDB, testURL, tempDir, 1000)
	err = downloader.verifyAndExtract(file)
	assert.Error(t, err)
}

func TestIncrDownloader_VerifyHash(t *testing.T) {
	// Setup test environment
	db := createTestDB()
	defer db.Close()
	trieDB := triedb.NewDatabase(db, nil)
	defer trieDB.Close()

	tempDir := t.TempDir()
	downloader := NewIncrDownloader(db, trieDB, testURL, tempDir, 1000)

	// Create test file
	testContent := ""
	testFile := filepath.Join(tempDir, "test-file.tar.lz4")
	err := os.WriteFile(testFile, []byte(testContent), 0644)
	require.NoError(t, err)

	// Create test file info (empty file MD5)
	file := &IncrFileInfo{
		Metadata: IncrMetadata{
			FileName: "test-file.tar.lz4",
			MD5Sum:   "d41d8cd98f00b204e9800998ecf8427e", // Empty file MD5
			Size:     0,
		},
		LocalPath: testFile,
	}

	// Test MD5 verification
	err = downloader.verifyHash(file)
	require.NoError(t, err)
}

func TestIncrDownloader_VerifyHashMismatch(t *testing.T) {
	// Setup test environment
	db := createTestDB()
	defer db.Close()
	trieDB := triedb.NewDatabase(db, nil)
	defer trieDB.Close()

	tempDir := t.TempDir()
	downloader := NewIncrDownloader(db, trieDB, testURL, tempDir, 1000)

	// Create test file
	testContent := "test content"
	testFile := filepath.Join(tempDir, "test-file.tar.lz4")
	err := os.WriteFile(testFile, []byte(testContent), 0644)
	require.NoError(t, err)

	// Create test file info (wrong MD5)
	file := &IncrFileInfo{
		Metadata: IncrMetadata{
			FileName: "test-file.tar.lz4",
			MD5Sum:   "wrong-md5-hash",
			Size:     uint64(len(testContent)),
		},
		LocalPath: testFile,
	}

	// Test MD5 verification failure
	err = downloader.verifyHash(file)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "hash mismatch for test-file.tar.lz4")
}

func TestIncrDownloader_Prepare(t *testing.T) {
	// Setup test environment
	db := createTestDB()
	defer db.Close()
	trieDB := triedb.NewDatabase(db, nil)
	defer trieDB.Close()

	tempDir := t.TempDir()
	downloader := NewIncrDownloader(db, trieDB, testURL, tempDir, 1000)

	// Create test metadata
	metadata := []IncrMetadata{
		{
			FileName: "test-incr-1000-1999.tar.lz4",
			MD5Sum:   "test-md5-1",
			Size:     1024,
		},
		{
			FileName: "test-incr-2000-2999.tar.lz4",
			MD5Sum:   "test-md5-2",
			Size:     2048,
		},
		{
			FileName: "test-incr-3000-3999.tar.lz4",
			MD5Sum:   "test-md5-3",
			Size:     3072,
		},
	}

	// Test preparation with empty state
	// Since fetchMetadata is private, we directly test parseFileInfo
	files, err := downloader.parseFileInfo(metadata)
	require.NoError(t, err)
	assert.Len(t, files, 3)

	// Verify file list
	assert.Equal(t, "test-incr-1000-1999.tar.lz4", files[0].Metadata.FileName)
	assert.Equal(t, uint64(1000), files[0].StartBlock)
	assert.Equal(t, uint64(1999), files[0].EndBlock)
}

func TestIncrDownloader_FileStatusManagement(t *testing.T) {
	db := createTestDB()
	defer db.Close()
	trieDB := triedb.NewDatabase(db, nil)
	defer trieDB.Close()

	tempDir := t.TempDir()
	downloader := NewIncrDownloader(db, trieDB, testURL, tempDir, 1000)

	// Test file status management methods
	fileName := "test-file.tar.lz4"

	// Test mark as to download
	downloader.markFileAsToDownload(fileName)
	toDownloadFiles, err := downloader.loadToDownloadFiles()
	require.NoError(t, err)
	assert.Contains(t, toDownloadFiles, fileName)

	// Test mark as downloaded
	downloader.markFileAsDownloaded(fileName)
	downloadedFiles, err := downloader.loadDownloadedFiles()
	require.NoError(t, err)
	assert.Contains(t, downloadedFiles, fileName)

	// Verify removed from to-download list
	toDownloadFiles, err = downloader.loadToDownloadFiles()
	require.NoError(t, err)
	assert.NotContains(t, toDownloadFiles, fileName)

	// Test mark as to merge
	downloader.markFileAsToMerge(fileName)
	toMergeFiles, err := downloader.loadToMergeFiles()
	require.NoError(t, err)
	assert.Contains(t, toMergeFiles, fileName)

	// Test mark as merged
	downloader.markFileAsMerged(fileName)
	mergedFiles, err := downloader.loadMergedFiles()
	require.NoError(t, err)
	assert.Contains(t, mergedFiles, fileName)

	// Verify removed from to-merge list
	toMergeFiles, err = downloader.loadToMergeFiles()
	require.NoError(t, err)
	assert.NotContains(t, toMergeFiles, fileName)
}

func TestIncrDownloader_PrepareWithExistingStatus(t *testing.T) {
	db := createTestDB()
	defer db.Close()
	trieDB := triedb.NewDatabase(db, nil)
	defer trieDB.Close()

	tempDir := t.TempDir()
	downloader := NewIncrDownloader(db, trieDB, testURL, tempDir, 1000)

	// Pre-set some file statuses
	err := downloader.saveDownloadedFiles([]string{"test-incr-1000-1999.tar.lz4"})
	assert.NoError(t, err)
	err = downloader.saveMergedFiles([]string{"test-incr-2000-2999.tar.lz4"})
	assert.NoError(t, err)
	err = downloader.saveToDownloadFiles([]string{"test-incr-3000-3999.tar.lz4"})
	assert.NoError(t, err)
	err = downloader.saveToMergeFiles([]string{"test-incr-4000-4999.tar.lz4"})
	assert.NoError(t, err)

	// Create test metadata
	metadata := []IncrMetadata{
		{
			FileName: "test-incr-1000-1999.tar.lz4", // Downloaded
			MD5Sum:   "test-md5-1",
			Size:     1024,
		},
		{
			FileName: "test-incr-2000-2999.tar.lz4", // Merged
			MD5Sum:   "test-md5-2",
			Size:     2048,
		},
		{
			FileName: "test-incr-3000-3999.tar.lz4", // Currently downloading
			MD5Sum:   "test-md5-3",
			Size:     3072,
		},
		{
			FileName: "test-incr-4000-4999.tar.lz4", // Currently merging
			MD5Sum:   "test-md5-4",
			Size:     4096,
		},
		{
			FileName: "test-incr-5000-5999.tar.lz4", // New file
			MD5Sum:   "test-md5-5",
			Size:     5120,
		},
	}

	// Directly test parseFileInfo and file status management logic
	files, err := downloader.parseFileInfo(metadata)
	require.NoError(t, err)
	assert.Len(t, files, 5)

	// Manually test file status filtering logic
	downloadedFiles, err := downloader.loadDownloadedFiles()
	require.NoError(t, err)
	mergedFiles, err := downloader.loadMergedFiles()
	require.NoError(t, err)
	toDownloadFiles, err := downloader.loadToDownloadFiles()
	require.NoError(t, err)
	toMergeFiles, err := downloader.loadToMergeFiles()
	require.NoError(t, err)

	// Create lookup sets
	downloadedSet := make(map[string]bool)
	for _, file := range downloadedFiles {
		downloadedSet[file] = true
	}
	mergedSet := make(map[string]bool)
	for _, file := range mergedFiles {
		mergedSet[file] = true
	}
	toDownloadSet := make(map[string]bool)
	for _, file := range toDownloadFiles {
		toDownloadSet[file] = true
	}
	toMergeSet := make(map[string]bool)
	for _, file := range toMergeFiles {
		toMergeSet[file] = true
	}

	// Verify file status filtering
	var newToDownloadFiles []*IncrFileInfo
	for _, file := range files {
		fileName := file.Metadata.FileName
		if !downloadedSet[fileName] && !mergedSet[fileName] && !toDownloadSet[fileName] && !toMergeSet[fileName] {
			newToDownloadFiles = append(newToDownloadFiles, file)
		}
	}

	// Only new files should be in download queue
	assert.Len(t, newToDownloadFiles, 1)
	assert.Equal(t, "test-incr-5000-5999.tar.lz4", newToDownloadFiles[0].Metadata.FileName)
}

func TestIncrDownloader_QueueForMerge(t *testing.T) {
	db := createTestDB()
	defer db.Close()
	trieDB := triedb.NewDatabase(db, nil)
	defer trieDB.Close()

	tempDir := t.TempDir()
	downloader := NewIncrDownloader(db, trieDB, testURL, tempDir, 1000)

	// Create test file
	file := &IncrFileInfo{
		Metadata: IncrMetadata{
			FileName: "test-incr-1000-1999.tar.lz4",
			MD5Sum:   "test-md5",
			Size:     1024,
		},
		StartBlock: 1000,
		EndBlock:   1999,
		LocalPath:  filepath.Join(tempDir, "test-incr-1000-1999.tar.lz4"),
	}

	// Test adding to merge queue
	downloader.queueForMerge(file)

	// Verify file added to merge queue
	toMergeFiles, err := downloader.loadToMergeFiles()
	require.NoError(t, err)
	assert.Contains(t, toMergeFiles, file.Metadata.FileName)

	// Verify file added to memory map
	downloader.mergeMutex.Lock()
	assert.Contains(t, downloader.downloadedFilesMap, uint64(1000))
	assert.Equal(t, file, downloader.downloadedFilesMap[1000])
	downloader.mergeMutex.Unlock()

	// Test duplicate addition (should be ignored)
	downloader.queueForMerge(file)
	toMergeFiles, err = downloader.loadToMergeFiles()
	require.NoError(t, err)
	// Should have only one entry
	count := 0
	for _, f := range toMergeFiles {
		if f == file.Metadata.FileName {
			count++
		}
	}
	assert.Equal(t, 1, count)
}

func TestIncrDownloader_GetFileStatusStats(t *testing.T) {
	db := createTestDB()
	defer db.Close()
	trieDB := triedb.NewDatabase(db, nil)
	defer trieDB.Close()

	tempDir := t.TempDir()
	downloader := NewIncrDownloader(db, trieDB, testURL, tempDir, 1000)

	// Set some test data
	downloader.totalFiles = 5
	downloader.downloadedFiles = 2
	downloader.mergedFiles = 1
	downloader.expectedNextBlockStart = 1000

	err := downloader.saveDownloadedFiles([]string{"file1.tar.lz4", "file2.tar.lz4"})
	assert.NoError(t, err)
	err = downloader.saveMergedFiles([]string{"file3.tar.lz4"})
	assert.NoError(t, err)
	err = downloader.saveToDownloadFiles([]string{"file4.tar.lz4"})
	assert.NoError(t, err)
	err = downloader.saveToMergeFiles([]string{"file5.tar.lz4"})
	assert.NoError(t, err)

	// Get statistics
	stats := downloader.GetFileStatusStats()

	// Verify statistics
	assert.Equal(t, 5, stats["totalFiles"])
	assert.Equal(t, 2, stats["downloadedFiles"])
	assert.Equal(t, 1, stats["mergedFiles"])
	assert.Equal(t, 1, stats["toDownloadFiles"])
	assert.Equal(t, 1, stats["toMergeFiles"])
	assert.Equal(t, uint64(1000), stats["expectedNextBlockStart"])
}

func TestIncrDownloader_ParseFileInfo(t *testing.T) {
	db := createTestDB()
	defer db.Close()
	trieDB := triedb.NewDatabase(db, nil)
	defer trieDB.Close()

	tempDir := t.TempDir()
	downloader := NewIncrDownloader(db, trieDB, testURL, tempDir, 1000)

	// Test valid metadata
	metadata := []IncrMetadata{
		{
			FileName: "test-incr-1000-1999.tar.lz4",
			MD5Sum:   "test-md5-1",
			Size:     1024,
		},
		{
			FileName: "test-incr-2000-2999.tar.lz4",
			MD5Sum:   "test-md5-2",
			Size:     2048,
		},
		{
			FileName: "test-incr-3000-3999.tar.lz4",
			MD5Sum:   "test-md5-3",
			Size:     3072,
		},
	}

	files, err := downloader.parseFileInfo(metadata)
	require.NoError(t, err)
	assert.Len(t, files, 3)

	// Verify file info
	assert.Equal(t, "test-incr-1000-1999.tar.lz4", files[0].Metadata.FileName)
	assert.Equal(t, uint64(1000), files[0].StartBlock)
	assert.Equal(t, uint64(1999), files[0].EndBlock)
	assert.Equal(t, filepath.Join(tempDir, "test-incr-1000-1999.tar.lz4"), files[0].LocalPath)

	// Test invalid filename format
	invalidMetadata := []IncrMetadata{
		{
			FileName: "invalid-filename.txt",
			MD5Sum:   "test-md5",
			Size:     1024,
		},
	}

	_, err = downloader.parseFileInfo(invalidMetadata)
	assert.Error(t, err)
}

func TestIncrDownloader_CheckFileContinuity(t *testing.T) {
	db := createTestDB()
	defer db.Close()
	trieDB := triedb.NewDatabase(db, nil)
	defer trieDB.Close()

	tempDir := t.TempDir()
	downloader := NewIncrDownloader(db, trieDB, testURL, tempDir, 1000)

	// Test continuous files
	continuousFiles := []*IncrFileInfo{
		{
			Metadata:   IncrMetadata{FileName: "test-incr-1000-1999.tar.lz4"},
			StartBlock: 1000,
			EndBlock:   1999,
		},
		{
			Metadata:   IncrMetadata{FileName: "test-incr-2000-2999.tar.lz4"},
			StartBlock: 2000,
			EndBlock:   2999,
		},
		{
			Metadata:   IncrMetadata{FileName: "test-incr-3000-3999.tar.lz4"},
			StartBlock: 3000,
			EndBlock:   3999,
		},
	}

	err := downloader.checkFileContinuity(continuousFiles)
	assert.NoError(t, err)

	// Test discontinuous files
	discontinuousFiles := []*IncrFileInfo{
		{
			Metadata:   IncrMetadata{FileName: "test-incr-1000-1999.tar.lz4"},
			StartBlock: 1000,
			EndBlock:   1999,
		},
		{
			Metadata:   IncrMetadata{FileName: "test-incr-2001-3000.tar.lz4"},
			StartBlock: 2001, // Should be 2001
			EndBlock:   3000,
		},
	}

	err = downloader.checkFileContinuity(discontinuousFiles)
	assert.Error(t, err)
}

func TestIncrDownloader_DatabaseOperations(t *testing.T) {
	db := createTestDB()
	defer db.Close()
	trieDB := triedb.NewDatabase(db, nil)
	defer trieDB.Close()

	tempDir := t.TempDir()
	downloader := NewIncrDownloader(db, trieDB, testURL, tempDir, 1000)

	// Test saving and loading various file lists
	testFiles := []string{"file1.tar.lz4", "file2.tar.lz4", "file3.tar.lz4"}

	// Test downloaded files list
	err := downloader.saveDownloadedFiles(testFiles)
	require.NoError(t, err)

	loadedFiles, err := downloader.loadDownloadedFiles()
	require.NoError(t, err)
	assert.Equal(t, testFiles, loadedFiles)

	// Test merged files list
	err = downloader.saveMergedFiles(testFiles)
	require.NoError(t, err)

	mergedFiles, err := downloader.loadMergedFiles()
	require.NoError(t, err)
	assert.Equal(t, testFiles, mergedFiles)

	// Test to-download files list
	err = downloader.saveToDownloadFiles(testFiles)
	require.NoError(t, err)

	toDownloadFiles, err := downloader.loadToDownloadFiles()
	require.NoError(t, err)
	assert.Equal(t, testFiles, toDownloadFiles)

	// Test to-merge files list
	err = downloader.saveToMergeFiles(testFiles)
	require.NoError(t, err)

	toMergeFiles, err := downloader.loadToMergeFiles()
	require.NoError(t, err)
	assert.Equal(t, testFiles, toMergeFiles)
}

func TestIncrDownloader_ContextCancellation(t *testing.T) {
	db := createTestDB()
	defer db.Close()
	trieDB := triedb.NewDatabase(db, nil)
	defer trieDB.Close()

	tempDir := t.TempDir()
	downloader := NewIncrDownloader(db, trieDB, testURL, tempDir, 1000)

	// Test cancellation
	downloader.Cancel()

	// Verify context is cancelled
	select {
	case <-downloader.ctx.Done():
		// Expected behavior
	default:
		t.Error("Context should be cancelled")
	}
}

func TestIncrDownloader_Close(t *testing.T) {
	db := createTestDB()
	defer db.Close()
	trieDB := triedb.NewDatabase(db, nil)
	defer trieDB.Close()

	tempDir := t.TempDir()
	downloader := NewIncrDownloader(db, trieDB, testURL, tempDir, 1000)

	// Add some test data to downloaded files map
	file := &IncrFileInfo{
		Metadata:   IncrMetadata{FileName: "test.tar.lz4"},
		StartBlock: 1000,
		EndBlock:   1999,
	}
	downloader.downloadedFilesMap[1000] = file

	// Test close operation
	downloader.Close()

	// Verify downloaded files map is cleared
	assert.Empty(t, downloader.downloadedFilesMap)
}

// func createTestMetadataFile(t *testing.T, tempDir string, metadata []IncrMetadata) string {
// 	metadataPath := filepath.Join(tempDir, "incr_metadata.json")
// 	data, err := json.Marshal(metadata)
// 	require.NoError(t, err)
//
// 	err = os.WriteFile(metadataPath, data, 0644)
// 	require.NoError(t, err)
//
// 	return metadataPath
// }

func TestIncrDownloader_Integration(t *testing.T) {
	db := createTestDB()
	defer db.Close()
	trieDB := triedb.NewDatabase(db, nil)
	defer trieDB.Close()

	tempDir := t.TempDir()
	downloader := NewIncrDownloader(db, trieDB, testURL, tempDir, 1000)

	metadata := []IncrMetadata{
		{
			FileName: "test-incr-1000-1999.tar.lz4",
			MD5Sum:   "test-md5-1",
			Size:     1024,
		},
		{
			FileName: "test-incr-2000-2999.tar.lz4",
			MD5Sum:   "test-md5-2",
			Size:     2048,
		},
	}

	// Directly test parseFileInfo
	files, err := downloader.parseFileInfo(metadata)
	require.NoError(t, err)
	assert.Len(t, files, 2)

	// Verify initial state
	assert.Equal(t, uint64(1000), files[0].StartBlock)

	// Simulate file download completion
	file := files[0]
	downloader.markFileAsDownloaded(file.Metadata.FileName)
	downloader.queueForMerge(file)

	// Verify status updates
	downloadedFiles, err := downloader.loadDownloadedFiles()
	require.NoError(t, err)
	assert.Contains(t, downloadedFiles, file.Metadata.FileName)

	toMergeFiles, err := downloader.loadToMergeFiles()
	require.NoError(t, err)
	assert.Contains(t, toMergeFiles, file.Metadata.FileName)

	// Verify memory map
	downloader.mergeMutex.Lock()
	assert.Contains(t, downloader.downloadedFilesMap, file.StartBlock)
	downloader.mergeMutex.Unlock()

	// Simulate merge completion
	downloader.markFileAsMerged(file.Metadata.FileName)

	// Verify final state
	mergedFiles, err := downloader.loadMergedFiles()
	require.NoError(t, err)
	assert.Contains(t, mergedFiles, file.Metadata.FileName)

	toMergeFiles, err = downloader.loadToMergeFiles()
	require.NoError(t, err)
	assert.NotContains(t, toMergeFiles, file.Metadata.FileName)
}

func TestIncrDownloader_ProcessFileStatus(t *testing.T) {
	db := createTestDB()
	defer db.Close()
	trieDB := triedb.NewDatabase(db, nil)
	defer trieDB.Close()

	tempDir := t.TempDir()
	downloader := NewIncrDownloader(db, trieDB, testURL, tempDir, 1000)

	// Create test files
	files := []*IncrFileInfo{
		{
			Metadata: IncrMetadata{
				FileName: "test-incr-1000-1999.tar.lz4",
				MD5Sum:   "test-md5-1",
				Size:     1024,
			},
			StartBlock: 1000,
			EndBlock:   1999,
		},
		{
			Metadata: IncrMetadata{
				FileName: "test-incr-2000-2999.tar.lz4",
				MD5Sum:   "test-md5-2",
				Size:     2048,
			},
			StartBlock: 2000,
			EndBlock:   2999,
		},
		{
			Metadata: IncrMetadata{
				FileName: "test-incr-3000-3999.tar.lz4",
				MD5Sum:   "test-md5-3",
				Size:     3072,
			},
			StartBlock: 3000,
			EndBlock:   3999,
		},
		{
			Metadata: IncrMetadata{
				FileName: "test-incr-4000-4999.tar.lz4",
				MD5Sum:   "test-md5-4",
				Size:     4096,
			},
			StartBlock: 4000,
			EndBlock:   4999,
		},
	}

	// Test different file status scenarios
	testCases := []struct {
		name                    string
		downloadedFiles         []string
		mergedFiles             []string
		toDownloadFiles         []string
		toMergeFiles            []string
		expectedToDownloadCount int
		expectedToMergeCount    int
		expectedTotalFiles      int
		expectedNextBlockStart  uint64
	}{
		{
			name:                    "All new files",
			downloadedFiles:         []string{},
			mergedFiles:             []string{},
			toDownloadFiles:         []string{},
			toMergeFiles:            []string{},
			expectedToDownloadCount: 4,
			expectedToMergeCount:    0,
			expectedTotalFiles:      4,
			expectedNextBlockStart:  1000,
		},
		{
			name:                    "Some files already downloaded",
			downloadedFiles:         []string{"test-incr-1000-1999.tar.lz4"},
			mergedFiles:             []string{},
			toDownloadFiles:         []string{},
			toMergeFiles:            []string{},
			expectedToDownloadCount: 3,
			expectedToMergeCount:    1,
			expectedTotalFiles:      3,
			expectedNextBlockStart:  1000,
		},
		{
			name:                    "Some files already merged",
			downloadedFiles:         []string{},
			mergedFiles:             []string{"test-incr-1000-1999.tar.lz4"},
			toDownloadFiles:         []string{},
			toMergeFiles:            []string{},
			expectedToDownloadCount: 3,
			expectedToMergeCount:    0,
			expectedTotalFiles:      3,
			expectedNextBlockStart:  2000,
		},
		{
			name:                    "Some files currently downloading",
			downloadedFiles:         []string{},
			mergedFiles:             []string{},
			toDownloadFiles:         []string{"test-incr-1000-1999.tar.lz4"},
			toMergeFiles:            []string{},
			expectedToDownloadCount: 4, // Including the one currently downloading
			expectedToMergeCount:    0,
			expectedTotalFiles:      4,
			expectedNextBlockStart:  1000,
		},
		{
			name:                    "Some files currently merging",
			downloadedFiles:         []string{},
			mergedFiles:             []string{},
			toDownloadFiles:         []string{},
			toMergeFiles:            []string{"test-incr-1000-1999.tar.lz4"},
			expectedToDownloadCount: 3,
			expectedToMergeCount:    1, // Including the one currently merging
			expectedTotalFiles:      3,
			expectedNextBlockStart:  1000,
		},
		{
			name:                    "Mixed statuses",
			downloadedFiles:         []string{"test-incr-2000-2999.tar.lz4"},
			mergedFiles:             []string{"test-incr-1000-1999.tar.lz4"},
			toDownloadFiles:         []string{"test-incr-3000-3999.tar.lz4"},
			toMergeFiles:            []string{},
			expectedToDownloadCount: 2,
			expectedToMergeCount:    1,
			expectedTotalFiles:      2,
			expectedNextBlockStart:  2000,
		},
		{
			name:                    "All files already merged",
			downloadedFiles:         []string{},
			mergedFiles:             []string{"test-incr-1000-1999.tar.lz4", "test-incr-2000-2999.tar.lz4", "test-incr-3000-3999.tar.lz4", "test-incr-4000-4999.tar.lz4"},
			toDownloadFiles:         []string{},
			toMergeFiles:            []string{},
			expectedToDownloadCount: 0,
			expectedToMergeCount:    0,
			expectedTotalFiles:      0,
			expectedNextBlockStart:  0,
		},
		{
			name:                    "All files currently downloading",
			downloadedFiles:         []string{},
			mergedFiles:             []string{},
			toDownloadFiles:         []string{"test-incr-1000-1999.tar.lz4", "test-incr-2000-2999.tar.lz4", "test-incr-3000-3999.tar.lz4", "test-incr-4000-4999.tar.lz4"},
			toMergeFiles:            []string{},
			expectedToDownloadCount: 4,
			expectedToMergeCount:    0,
			expectedTotalFiles:      4,
			expectedNextBlockStart:  1000,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Reset downloader state
			downloader.downloadedFiles = 0
			downloader.files = nil
			downloader.totalFiles = 0
			downloader.downloadedFilesMap = make(map[uint64]*IncrFileInfo)
			downloader.expectedNextBlockStart = 0

			// Pre-set database state for this test case
			if len(tc.toDownloadFiles) > 0 {
				err := downloader.saveToDownloadFiles(tc.toDownloadFiles)
				require.NoError(t, err)
			}
			if len(tc.downloadedFiles) > 0 {
				err := downloader.saveDownloadedFiles(tc.downloadedFiles)
				require.NoError(t, err)
			}
			if len(tc.toMergeFiles) > 0 {
				err := downloader.saveToMergeFiles(tc.toMergeFiles)
				require.NoError(t, err)
			}
			if len(tc.mergedFiles) > 0 {
				err := downloader.saveMergedFiles(tc.mergedFiles)
				require.NoError(t, err)
			}

			// Test processFileStatus
			err := downloader.processFileStatus(files)

			require.NoError(t, err)
			assert.Len(t, downloader.files, tc.expectedToDownloadCount)
			assert.Equal(t, tc.expectedTotalFiles, downloader.totalFiles)
			assert.Equal(t, tc.expectedNextBlockStart, downloader.expectedNextBlockStart)

			// Verify downloaded files map
			assert.Len(t, downloader.downloadedFilesMap, tc.expectedToMergeCount)

			// Verify database state
			toDownloadFiles, err := downloader.loadToDownloadFiles()
			require.NoError(t, err)
			assert.Len(t, toDownloadFiles, tc.expectedToDownloadCount)

			toMergeFiles, err := downloader.loadToMergeFiles()
			require.NoError(t, err)
			assert.Len(t, toMergeFiles, tc.expectedToMergeCount)

			// Verify specific file assignments
			if tc.expectedToMergeCount > 0 {
				// Check that files are properly assigned to merge queue
				for _, file := range downloader.downloadedFilesMap {
					assert.Contains(t, toMergeFiles, file.Metadata.FileName)
				}
			}

			// Clean up database state for next test
			err = downloader.saveDownloadedFiles([]string{})
			assert.NoError(t, err)
			err = downloader.saveMergedFiles([]string{})
			assert.NoError(t, err)
			err = downloader.saveToDownloadFiles([]string{})
			assert.NoError(t, err)
			err = downloader.saveToMergeFiles([]string{})
			assert.NoError(t, err)
		})
	}
}

func TestIncrDownloader_ProcessFileStatus_EmptyFiles(t *testing.T) {
	db := createTestDB()
	defer db.Close()
	trieDB := triedb.NewDatabase(db, nil)
	defer trieDB.Close()

	tempDir := t.TempDir()
	downloader := NewIncrDownloader(db, trieDB, testURL, tempDir, 1000)

	// Test with empty files list
	files := []*IncrFileInfo{}
	err := downloader.processFileStatus(files)
	require.NoError(t, err)
	assert.Len(t, downloader.files, 0)
	assert.Equal(t, 0, downloader.totalFiles)
	assert.Equal(t, uint64(0), downloader.expectedNextBlockStart)
}

func TestIncrDownloader_ProcessFileStatus_BlockOrdering(t *testing.T) {
	db := createTestDB()
	defer db.Close()
	trieDB := triedb.NewDatabase(db, nil)
	defer trieDB.Close()

	tempDir := t.TempDir()
	downloader := NewIncrDownloader(db, trieDB, testURL, tempDir, 1000)

	// Create test files with non-sequential block numbers
	files := []*IncrFileInfo{
		{
			Metadata: IncrMetadata{
				FileName: "test-incr-3000-3999.tar.lz4",
				MD5Sum:   "test-md5-3",
				Size:     3072,
			},
			StartBlock: 3000,
			EndBlock:   3999,
		},
		{
			Metadata: IncrMetadata{
				FileName: "test-incr-1000-1999.tar.lz4",
				MD5Sum:   "test-md5-1",
				Size:     1024,
			},
			StartBlock: 1000,
			EndBlock:   1999,
		},
		{
			Metadata: IncrMetadata{
				FileName: "test-incr-2000-2999.tar.lz4",
				MD5Sum:   "test-md5-2",
				Size:     2048,
			},
			StartBlock: 2000,
			EndBlock:   2999,
		},
	}

	// Set some files as downloaded
	err := downloader.saveDownloadedFiles([]string{"test-incr-1000-1999.tar.lz4", "test-incr-2000-2999.tar.lz4"})
	assert.NoError(t, err)

	// Test processFileStatus
	err = downloader.processFileStatus(files)
	require.NoError(t, err)

	// Verify that the earliest block start is used for expectedNextBlockStart
	assert.Equal(t, uint64(1000), downloader.expectedNextBlockStart)

	// Verify that both downloaded files are in the merge queue
	assert.Len(t, downloader.downloadedFilesMap, 2)
	assert.Contains(t, downloader.downloadedFilesMap, uint64(1000))
	assert.Contains(t, downloader.downloadedFilesMap, uint64(2000))
}
