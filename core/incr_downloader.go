package core

import (
	"archive/tar"
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/triedb"
	"github.com/pierrec/lz4/v4"
)

const (
	incrSnapshotNamePattern = `(.*)-incr-(\d+)-(\d+)\.tar\.lz4`
	maxRetries              = 3
	baseDelay               = time.Second
)

// Database keys for download status
var (
	incrDownloadedFilesKey  = []byte("incr_downloaded_files")
	incrDownloadingFilesKey = []byte("incr_downloading_files")
)

// metadata file contains many IncrMetadata, array
type IncrMetadata struct {
	FileName string `json:"file_name"`
	MD5Sum   string `json:"md5_sum"`
	Size     uint64 `json:"size"`
}

// IncrFileInfo represents parsed incremental file information
type IncrFileInfo struct {
	Metadata   IncrMetadata
	StartBlock uint64
	EndBlock   uint64
	LocalPath  string
	Downloaded bool
	Verified   bool
	Extracted  bool
	Merged     bool
	Processing bool
}

// DownloadProgress represents download progress for a file
type DownloadProgress struct {
	FileName       string
	TotalSize      uint64
	DownloadedSize uint64
	Progress       float64
	Speed          string
	Status         string
	Error          error
}

// IncrDownloader handles incremental snapshot downloading and merging
type IncrDownloader struct {
	db            ethdb.Database
	triedb        *triedb.Database
	remoteURL     string
	incrPath      string
	localBlockNum uint64

	// Download management
	downloadChan chan *IncrFileInfo
	mergeChan    chan *IncrFileInfo
	progressChan chan *DownloadProgress
	errorChan    chan error

	// State management
	files      []*IncrFileInfo
	downloadWG sync.WaitGroup
	mergeWG    sync.WaitGroup
	ctx        context.Context
	cancel     context.CancelFunc

	// Statistics
	totalFiles      int
	downloadedFiles int
	mergedFiles     int
	mu              sync.RWMutex

	// Merge order control
	expectedNextBlockStart uint64                   // Next expected start block for merge
	pendingMergeFiles      map[uint64]*IncrFileInfo // Downloaded but pending merge files, key is StartBlock
	mergeMutex             sync.Mutex               // Protects merge-related state
}

// NewIncrDownloader creates a new incremental downloader
func NewIncrDownloader(db ethdb.Database, triedb *triedb.Database, remoteURL, incrPath string, localBlockNum uint64) *IncrDownloader {
	ctx, cancel := context.WithCancel(context.Background())
	// we don't validate the url and assume it's valid
	newURL := strings.TrimSuffix(remoteURL, "/")

	downloader := &IncrDownloader{
		db:                db,
		triedb:            triedb,
		remoteURL:         newURL,
		incrPath:          incrPath,
		localBlockNum:     localBlockNum,
		downloadChan:      make(chan *IncrFileInfo, 100),
		mergeChan:         make(chan *IncrFileInfo, 10),
		progressChan:      make(chan *DownloadProgress, 100),
		errorChan:         make(chan error, 10),
		ctx:               ctx,
		cancel:            cancel,
		pendingMergeFiles: make(map[uint64]*IncrFileInfo),
	}

	return downloader
}

// saveDownloadedFiles saves list of downloaded files to database
func (d *IncrDownloader) saveDownloadedFiles(files []string) error {
	data, err := json.Marshal(files)
	if err != nil {
		return fmt.Errorf("failed to marshal downloaded files: %v", err)
	}
	return d.db.Put(incrDownloadedFilesKey, data)
}

// loadDownloadedFiles loads list of downloaded files from database
func (d *IncrDownloader) loadDownloadedFiles() ([]string, error) {
	data, err := d.db.Get(incrDownloadedFilesKey)
	if err != nil {
		return nil, err
	}

	var files []string
	if err = json.Unmarshal(data, &files); err != nil {
		return nil, fmt.Errorf("failed to unmarshal downloaded files: %v", err)
	}
	return files, nil
}

// saveDownloadingFiles saves list of currently downloading files to database
func (d *IncrDownloader) saveDownloadingFiles(files []string) error {
	data, err := json.Marshal(files)
	if err != nil {
		return fmt.Errorf("failed to marshal downloading files: %v", err)
	}
	return d.db.Put(incrDownloadingFilesKey, data)
}

// loadDownloadingFiles loads list of currently downloading files from database
func (d *IncrDownloader) loadDownloadingFiles() ([]string, error) {
	data, err := d.db.Get(incrDownloadingFilesKey)
	if err != nil {
		return nil, err
	}

	var files []string
	if err = json.Unmarshal(data, &files); err != nil {
		return nil, fmt.Errorf("failed to unmarshal downloading files: %v", err)
	}
	return files, nil
}

// Stage 1: Prepare - fetch metadata and validate
func (d *IncrDownloader) Prepare() error {
	log.Info("Starting preparation phase", "remoteURL", d.remoteURL, "localBlockNum", d.localBlockNum)

	// Download metadata file
	metadata, err := d.fetchMetadata()
	if err != nil {
		log.Error("Failed to fetch metadata", "error", err)
		return err
	}

	files, err := d.parseFileInfo(metadata)
	if err != nil {
		log.Error("Failed to parse file info", "error", err)
		return err
	}
	d.files = files
	d.totalFiles = len(d.files)

	downloadedFiles, err := d.loadDownloadedFiles()
	if err != nil {
		log.Warn("Failed to load downloaded files list, starting fresh")
		downloadedFiles = []string{}
	}

	// Create set for quick lookup
	downloadedSet := make(map[string]bool)
	for _, file := range downloadedFiles {
		downloadedSet[file] = true
	}

	// Filter out already downloaded files
	var remainingFiles []*IncrFileInfo
	var remainingFileNames []string
	for _, file := range d.files {
		if downloadedSet[file.Metadata.FileName] {
			log.Debug("Skipping already downloaded file", "fileName", file.Metadata.FileName)
			d.downloadedFiles++
			continue
		}
		remainingFiles = append(remainingFiles, file)
		remainingFileNames = append(remainingFileNames, file.Metadata.FileName)
	}

	d.files = remainingFiles
	if err = d.saveDownloadingFiles(remainingFileNames); err != nil {
		log.Error("Failed to save downloading files", "error", err)
		return err
	}
	log.Info("Filtered files", "totalFiles", d.totalFiles, "downloadedFiles", len(downloadedFiles), "remainingFiles", len(remainingFiles))

	// Initialize expected next block start for merge ordering
	if len(d.files) > 0 {
		d.expectedNextBlockStart = d.files[0].StartBlock
		log.Info("Initialized expected next block start", "expectedNextBlockStart", d.expectedNextBlockStart)
	}

	log.Info("Preparation completed", "totalFiles", d.totalFiles, "firstBlock", d.getFirstBlockNum(),
		"lastBlock", d.getLastBlockNum())

	return nil
}

// Download incremental files
func (d *IncrDownloader) Download() error {
	log.Info("Starting download phase", "totalFiles", d.totalFiles)

	// Create download directory
	if err := os.MkdirAll(d.incrPath, 0755); err != nil {
		return fmt.Errorf("failed to create download directory: %v", err)
	}

	// Start download workers
	numWorkers := 4
	for i := 0; i < numWorkers; i++ {
		d.downloadWG.Add(1)
		go d.downloadWorker()
	}

	// Start progress monitor
	go d.progressMonitor()

	// Queue files for download
	for _, file := range d.files {
		select {
		case d.downloadChan <- file:
		case <-d.ctx.Done():
			return d.ctx.Err()
		}
	}
	close(d.downloadChan)

	// Wait for all downloads to complete
	d.downloadWG.Wait()

	log.Info("Download phase completed", "downloadedFiles", d.downloadedFiles)
	return nil
}

// RunConcurrent executes download and merge concurrently
func (d *IncrDownloader) RunConcurrent() error {
	if err := d.Prepare(); err != nil {
		return err
	}

	// Start merge worker
	d.mergeWG.Add(1)
	go d.mergeWorker()

	// Start download and merge concurrently
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := d.Download(); err != nil {
			log.Error("Download failed", "error", err)
			d.errorChan <- err
		}
		log.Info("Download goroutine completed")
	}()

	wg.Wait()
	log.Info("All downloads completed, closing merge channel")
	close(d.mergeChan)
	d.mergeWG.Wait()
	log.Info("All merges completed")

	return nil
}

// fetchMetadata downloads and parses metadata file
func (d *IncrDownloader) fetchMetadata() ([]IncrMetadata, error) {
	resp, err := http.Get(fmt.Sprintf("%s/incr_metadata.json", d.remoteURL))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP error: %d", resp.StatusCode)
	}

	var metadata []IncrMetadata
	if err = json.NewDecoder(resp.Body).Decode(&metadata); err != nil {
		return nil, err
	}
	log.Info("Metadata fetched", "metadata", metadata)

	return metadata, nil
}

// parseFileInfo parses file names to extract block information
func (d *IncrDownloader) parseFileInfo(metadata []IncrMetadata) ([]*IncrFileInfo, error) {
	if len(metadata) == 0 {
		return nil, fmt.Errorf("no metadata found")
	}

	pattern := regexp.MustCompile(incrSnapshotNamePattern)

	var (
		files         []*IncrFileInfo
		filteredFiles []*IncrFileInfo
	)

	// Parse all files from metadata and separate by format
	for _, meta := range metadata {
		if matches := pattern.FindStringSubmatch(meta.FileName); len(matches) == 4 {
			startBlock, err := strconv.ParseUint(matches[2], 10, 64)
			if err != nil {
				return nil, fmt.Errorf("invalid start block in %s: %v", meta.FileName, err)
			}
			endBlock, err := strconv.ParseUint(matches[3], 10, 64)
			if err != nil {
				return nil, fmt.Errorf("invalid end block in %s: %v", meta.FileName, err)
			}

			files = append(files, &IncrFileInfo{
				Metadata:   meta,
				StartBlock: startBlock,
				EndBlock:   endBlock,
				LocalPath:  filepath.Join(d.incrPath, meta.FileName),
			})
		} else {
			log.Warn("Invalid file name format", "fileName", meta.FileName)
			continue
		}
	}

	// Sort new format files by end block
	sort.Slice(files, func(i, j int) bool {
		return files[i].EndBlock < files[j].EndBlock
	})
	// Check continuity of all files before filtering
	if err := d.checkFileContinuity(files); err != nil {
		return nil, err
	}

	// filter the block number that matches local data
	for index, file := range files {
		if file.StartBlock >= d.localBlockNum && file.EndBlock > d.localBlockNum {
			filteredFiles = append(filteredFiles, files[index:]...)
			break
		}
	}

	if len(filteredFiles) == 0 {
		return nil, fmt.Errorf("remote incr snapshots don't match local data: %d", d.localBlockNum)
	}

	log.Info("Filtered incremental files", "totalFiles", len(files), "keptFiles", len(filteredFiles),
		"localBlockNum", d.localBlockNum)
	return filteredFiles, nil
}

// checkFileContinuity checks if all files have continuous block ranges
// startBlock = previousEndBlock + 1 (except for the first file)
func (d *IncrDownloader) checkFileContinuity(files []*IncrFileInfo) error {
	if len(files) == 0 {
		return nil
	}

	// For the first file, we don't check continuity since we don't know the previous end block
	// We'll check from the second file onwards
	for i := 1; i < len(files); i++ {
		prevFile := files[i-1]
		currFile := files[i]

		expectedStartBlock := prevFile.EndBlock + 1
		if currFile.StartBlock != expectedStartBlock {
			return fmt.Errorf("file continuity broken: file %s ends at %d, but file %s starts at %d (expected %d)",
				prevFile.Metadata.FileName, prevFile.EndBlock,
				currFile.Metadata.FileName, currFile.StartBlock, expectedStartBlock)
		}
	}

	log.Info("File continuity check completed successfully", "totalFiles", len(files))
	return nil
}

// downloadWorker handles file downloads
func (d *IncrDownloader) downloadWorker() {
	defer d.downloadWG.Done()

	for file := range d.downloadChan {
		// Mark file as downloading
		d.markFileAsDownloading(file.Metadata.FileName)

		if err := d.downloadFile(file); err != nil {
			log.Error("Failed to download file", "file", file.Metadata.FileName, "error", err)
			d.errorChan <- err
			continue
		}

		if err := d.verifyAndExtract(file); err != nil {
			log.Error("Failed to verify or extract failed", "file", file.Metadata.FileName, "error", err)
			d.errorChan <- err
			continue
		}

		log.Info("Finished downloading and verifying file", "file", file.Metadata.FileName)
		// Mark file as downloaded
		d.markFileAsDownloaded(file.Metadata.FileName)

		d.mu.Lock()
		d.downloadedFiles++
		d.mu.Unlock()

		log.Info("File completed, queuing for merge", "file", file.Metadata.FileName, "downloadedFiles", d.downloadedFiles)

		// Queue file for merge (non-blocking)
		d.queueForMerge(file)
	}

	log.Info("Download worker completed")
}

// markFileAsDownloading marks a file as currently downloading
func (d *IncrDownloader) markFileAsDownloading(fileName string) {
	d.mu.Lock()
	defer d.mu.Unlock()

	downloadingFiles, _ := d.loadDownloadingFiles()
	if downloadingFiles == nil {
		downloadingFiles = []string{}
	}

	// Check if already in list
	found := false
	for _, file := range downloadingFiles {
		if file == fileName {
			found = true
			break
		}
	}

	if !found {
		downloadingFiles = append(downloadingFiles, fileName)
		d.saveDownloadingFiles(downloadingFiles)
		log.Debug("Marked file as downloading", "fileName", fileName)
	}
}

// removeFromDownloading removes a file from downloading list
func (d *IncrDownloader) removeFromDownloading(fileName string) {
	downloadingFiles, err := d.loadDownloadingFiles()
	if err != nil {
		log.Error("Failed to load downloading files", "error", err)
	}

	if downloadingFiles != nil {
		var newDownloadingFiles []string
		for _, file := range downloadingFiles {
			if file != fileName {
				newDownloadingFiles = append(newDownloadingFiles, file)
			}
		}
		d.saveDownloadingFiles(newDownloadingFiles)
		log.Debug("Removed file from downloading list", "fileName", fileName)
	}
}

// markFileAsDownloaded marks a file as downloaded and removes from downloading list
func (d *IncrDownloader) markFileAsDownloaded(fileName string) {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Add to downloaded files
	downloadedFiles, _ := d.loadDownloadedFiles()
	if downloadedFiles == nil {
		downloadedFiles = []string{}
	}
	// Check if already in list
	found := false
	for _, file := range downloadedFiles {
		if file == fileName {
			found = true
			break
		}
	}

	if !found {
		downloadedFiles = append(downloadedFiles, fileName)
		d.saveDownloadedFiles(downloadedFiles)
		log.Info("Marked file as downloaded", "fileName", fileName)
	}

	// Remove from downloading files
	d.removeFromDownloading(fileName)
}

// checkAndScheduleMerge checks if file can be merged immediately or should be queued
// queueForMerge queues a file for merge (non-blocking)
func (d *IncrDownloader) queueForMerge(file *IncrFileInfo) {
	d.mergeMutex.Lock()
	defer d.mergeMutex.Unlock()

	log.Info("Queueing file for merge, only called 7 times", "file", file.Metadata.FileName, "startBlock", file.StartBlock)
	// Check if file is already in pending queue
	if existingFile, exists := d.pendingMergeFiles[file.StartBlock]; exists {
		if existingFile.Metadata.FileName == file.Metadata.FileName {
			log.Debug("File already in pending queue, skipping", "file", file.Metadata.FileName, "startBlock", file.StartBlock)
			return
		}
	}

	// Check if file has already been merged
	if file.Merged {
		log.Debug("File already merged, skipping queue", "file", file.Metadata.FileName, "startBlock", file.StartBlock)
		return
	}

	// Check if file is already being processed
	if file.Processing {
		log.Info("File already being processed, skipping queue", "file", file.Metadata.FileName, "startBlock", file.StartBlock)
		return
	}

	// Add to pending queue
	d.pendingMergeFiles[file.StartBlock] = file
	log.Debug("File queued for merge", "file", file.Metadata.FileName, "startBlock", file.StartBlock)

	// Try to send the next available file to merge channel (non-blocking)
	d.trySendNextFileToMerge()
}

// trySendNextFileToMerge tries to send the next file in sequence to merge channel
func (d *IncrDownloader) trySendNextFileToMerge() {
	// Try to send as many consecutive files as possible
	for {
		// Check if the next expected file is available
		nextFile, exists := d.pendingMergeFiles[d.expectedNextBlockStart]
		if !exists {
			break
		}

		// Check if file has already been merged
		if nextFile.Merged {
			log.Warn("File already merged, removing from pending queue", "file", nextFile.Metadata.FileName)
			delete(d.pendingMergeFiles, d.expectedNextBlockStart)
			d.expectedNextBlockStart = nextFile.EndBlock + 1
			continue
		}

		// Check if file is already being processed
		if nextFile.Processing {
			log.Info("File already being processed, skipping", "file", nextFile.Metadata.FileName)
			break
		}

		// Remove from pending queue BEFORE sending to channel to prevent race conditions
		delete(d.pendingMergeFiles, d.expectedNextBlockStart)

		// Mark file as processing
		nextFile.Processing = true

		// Try to send to merge channel (non-blocking)
		select {
		case d.mergeChan <- nextFile:
			log.Info("File sent to merge channel", "file", nextFile.Metadata.FileName,
				"startBlock", nextFile.StartBlock, "expectedNextBlockStart", d.expectedNextBlockStart)
			// Update expected next start block for next iteration
			d.expectedNextBlockStart = nextFile.EndBlock + 1
		default:
			// Channel is full, put back in pending queue and reset processing flag
			nextFile.Processing = false
			d.pendingMergeFiles[d.expectedNextBlockStart] = nextFile
			log.Info("Merge channel full, file put back in queue", "file", nextFile.Metadata.FileName)
			return
		}
	}
}

// processNextMergeFiles processes files that can now be merged after current file is merged
func (d *IncrDownloader) processNextMergeFiles(completedFile *IncrFileInfo) {
	d.mergeMutex.Lock()
	defer d.mergeMutex.Unlock()

	// Update expected next start block
	d.expectedNextBlockStart = completedFile.EndBlock + 1

	log.Info("Updated expected next block start", "expectedNextBlockStart", d.expectedNextBlockStart,
		"completedFile", completedFile.Metadata.FileName, "endBlock", completedFile.EndBlock)

	// Try to send the next available file to merge channel (non-blocking)
	d.trySendNextFileToMerge()
}

// downloadFile downloads a single file with retry mechanism
func (d *IncrDownloader) downloadFile(file *IncrFileInfo) error {
	for attempt := 1; attempt <= maxRetries; attempt++ {
		err := d.downloadWithHTTP(file)
		if err == nil {
			// Success, no need to retry
			return nil
		}

		// Log the error for this attempt
		log.Warn("Download attempt failed", "file", file.Metadata.FileName, "attempt", attempt,
			"maxRetries", maxRetries, "error", err)

		// If this is the last attempt, return the error
		if attempt == maxRetries {
			return fmt.Errorf("download failed after %d attempts, last error: %w", maxRetries, err)
		}

		// Calculate delay with exponential backoff
		delay := baseDelay * time.Duration(attempt)
		log.Info("Retrying download", "file", file.Metadata.FileName, "attempt", attempt+1, "delay", delay)

		// Wait before retrying
		select {
		case <-d.ctx.Done():
			return d.ctx.Err()
		case <-time.After(delay):
			continue
		}
	}

	return fmt.Errorf("failed to download file after %d attempts", maxRetries)
}

// ChunkInfo represents a download chunk
type ChunkInfo struct {
	Index      int
	Start      int64
	End        int64
	Downloaded int64
	TempFile   string
	Completed  bool
}

// ChunkProgress represents progress of a chunk download
type ChunkProgress struct {
	ChunkIndex int
	Downloaded int64
	Total      int64
	FileName   string
}

// downloadWithHTTP downloads file using concurrent HTTP requests
func (d *IncrDownloader) downloadWithHTTP(file *IncrFileInfo) error {
	url := fmt.Sprintf("%s/%s", d.remoteURL, file.Metadata.FileName)

	log.Info("Starting concurrent HTTP download", "file", file.Metadata.FileName, "url", url)

	// Check if file already exists and has correct size
	if info, err := os.Stat(file.LocalPath); err == nil {
		if uint64(info.Size()) == file.Metadata.Size {
			log.Info("File already exists with correct size, skipping download", "file", file.Metadata.FileName)
			return nil
		}
	}

	// Check if server supports range requests
	supportsRange, contentLength, err := d.checkRangeSupport(url)
	if err != nil {
		return fmt.Errorf("failed to check range support: %v", err)
	}

	if !supportsRange {
		log.Info("Server doesn't support range requests, using single-threaded download", "file", file.Metadata.FileName)
		return d.downloadSingleThreaded(url, file)
	}

	// Use expected size from metadata, fallback to content-length
	totalSize := file.Metadata.Size
	if totalSize == 0 {
		totalSize = uint64(contentLength)
	}

	// Calculate chunk size and number of chunks
	numChunks := 8 // Number of concurrent downloads
	chunkSize := int64(totalSize) / int64(numChunks)
	if chunkSize < 1024*1024 { // Minimum 1MB per chunk
		chunkSize = 1024 * 1024
		numChunks = int(int64(totalSize) / chunkSize)
		if numChunks < 1 {
			numChunks = 1
		}
	}

	// Create chunk info
	chunks := make([]*ChunkInfo, numChunks)
	for i := 0; i < numChunks; i++ {
		start := int64(i) * chunkSize
		end := start + chunkSize - 1
		if i == numChunks-1 {
			end = int64(totalSize) - 1
		}

		chunks[i] = &ChunkInfo{
			Index:    i,
			Start:    start,
			End:      end,
			TempFile: fmt.Sprintf("%s.part%d", file.LocalPath, i),
		}
	}

	// Check for existing partial downloads
	d.checkExistingChunks(chunks)

	// Download chunks concurrently
	var wg sync.WaitGroup
	progressChan := make(chan *ChunkProgress, numChunks)
	errorChan := make(chan error, numChunks)

	// Download each chunk
	for _, chunk := range chunks {
		if chunk.Completed {
			continue // Skip already completed chunks
		}

		wg.Add(1)
		go func(chunk *ChunkInfo) {
			defer wg.Done()
			if err = d.downloadChunk(url, chunk, progressChan); err != nil {
				errorChan <- fmt.Errorf("failed to download chunk %d: %v", chunk.Index, err)
			}
		}(chunk)
	}

	wg.Wait()
	close(progressChan)

	// Check for errors
	select {
	case err = <-errorChan:
		return err
	default:
	}

	// Merge chunks
	log.Debug("Merging chunks", "file", file.Metadata.FileName, "chunks", numChunks)
	if err = d.mergeChunks(chunks, file.LocalPath); err != nil {
		return fmt.Errorf("failed to merge chunks: %v", err)
	}

	// Clean up temporary files
	d.cleanupTempFiles(chunks)

	// Verify final file size
	if info, err := os.Stat(file.LocalPath); err != nil {
		return fmt.Errorf("downloaded file not found: %v", err)
	} else if uint64(info.Size()) != totalSize {
		return fmt.Errorf("downloaded file size mismatch: expected %d, got %d", totalSize, info.Size())
	}

	log.Info("Concurrent HTTP download completed successfully", "file", file.Metadata.FileName, "size", totalSize)
	return nil
}

// verifyAndExtract verifies MD5 hash and extracts file with retry mechanism
func (d *IncrDownloader) verifyAndExtract(file *IncrFileInfo) error {
	for attempt := 1; attempt <= maxRetries; attempt++ {
		// Verify MD5
		if err := d.verifyHash(file); err != nil {
			log.Warn("Hash verification attempt failed", "file", file.Metadata.FileName, "attempt", attempt,
				"maxRetries", maxRetries, "error", err)

			if attempt == maxRetries {
				return fmt.Errorf("hash verification failed after %d attempts, last error: %w", maxRetries, err)
			}

			// Wait before retrying
			delay := baseDelay * time.Duration(attempt)
			select {
			case <-d.ctx.Done():
				return d.ctx.Err()
			case <-time.After(delay):
				continue
			}
		}
		file.Verified = true
		break
	}

	// Extract file with retry mechanism
	for attempt := 1; attempt <= maxRetries; attempt++ {
		if err := d.extractFile(file); err != nil {
			log.Warn("Failed to extract file", "file", file.Metadata.FileName, "attempt", attempt,
				"maxRetries", maxRetries, "error", err)

			if attempt == maxRetries {
				return fmt.Errorf("file extraction failed after %d attempts, last error: %w", maxRetries, err)
			}

			// Wait before retrying
			delay := baseDelay * time.Duration(attempt)
			select {
			case <-d.ctx.Done():
				return d.ctx.Err()
			case <-time.After(delay):
				continue
			}
		}
		file.Extracted = true
		break
	}

	return nil
}

// verifyHash verifies file MD5 hash
func (d *IncrDownloader) verifyHash(file *IncrFileInfo) error {
	f, err := os.Open(file.LocalPath)
	if err != nil {
		return err
	}
	defer f.Close()

	h := md5.New()
	if _, err = io.Copy(h, f); err != nil {
		return err
	}

	actualHash := hex.EncodeToString(h.Sum(nil))
	if actualHash != file.Metadata.MD5Sum {
		return fmt.Errorf("hash mismatch for %s: expected %s, got %s",
			file.Metadata.FileName, file.Metadata.MD5Sum, actualHash)
	}

	log.Info("Finished verifying md5 hash", "file", file.LocalPath)
	return nil
}

// extractFile extracts tar.lz4 file using Go code
// To use this implementation, first add the lz4 dependency:
// go get github.com/pierrec/lz4/v4
func (d *IncrDownloader) extractFile(file *IncrFileInfo) error {
	// Extract directory
	extractDir := filepath.Join(d.incrPath)
	if err := os.MkdirAll(extractDir, 0755); err != nil {
		return err
	}
	log.Info("Extracting file", "file", file.Metadata.FileName, "extractDir", extractDir)

	// Open the lz4 file
	inputFile, err := os.Open(file.LocalPath)
	if err != nil {
		return fmt.Errorf("failed to open file %s: %v", file.LocalPath, err)
	}
	defer inputFile.Close()

	// Create lz4 reader
	lz4Reader := lz4.NewReader(inputFile)
	// Create tar reader from lz4 output
	tarReader := tar.NewReader(lz4Reader)

	// Extract tar contents
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("failed to read tar header: %v", err)
		}

		// Create the full path for the file
		targetPath := filepath.Join(extractDir, header.Name)

		// Ensure the target directory exists
		if err = os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
			return fmt.Errorf("failed to create directory %s: %v", filepath.Dir(targetPath), err)
		}

		switch header.Typeflag {
		case tar.TypeDir:
			// Create directory
			if err = os.MkdirAll(targetPath, os.FileMode(header.Mode)); err != nil {
				return fmt.Errorf("failed to create directory %s: %v", targetPath, err)
			}
		case tar.TypeReg:
			// Create regular file
			outFile, err := os.OpenFile(targetPath, os.O_CREATE|os.O_RDWR, os.FileMode(header.Mode))
			if err != nil {
				return fmt.Errorf("failed to create file %s: %v", targetPath, err)
			}

			// Copy file content
			if _, err = io.Copy(outFile, tarReader); err != nil {
				outFile.Close()
				return fmt.Errorf("failed to write file %s: %v", targetPath, err)
			}
			outFile.Close()
		case tar.TypeSymlink:
			// Create symbolic link
			if err = os.Symlink(header.Linkname, targetPath); err != nil {
				return fmt.Errorf("failed to create symlink %s: %v", targetPath, err)
			}
		default:
			log.Warn("Unsupported file type in tar", "name", header.Name, "type", header.Typeflag)
		}
	}

	log.Info("File extracted successfully", "file", file.Metadata.FileName, "extractDir", extractDir)
	return nil
}

// mergeWorker handles sequential merging of files
func (d *IncrDownloader) mergeWorker() {
	defer d.mergeWG.Done()

	for file := range d.mergeChan {
		log.Info("Merge worker processing file", "file", file.Metadata.FileName,
			"startBlock", file.StartBlock, "endBlock", file.EndBlock, "merged", file.Merged)

		// Check if file has already been merged
		if file.Merged {
			log.Warn("File already merged, skipping", "file", file.Metadata.FileName,
				"startBlock", file.StartBlock, "endBlock", file.EndBlock)
			// Reset processing flag even for already merged files
			file.Processing = false
			continue
		}

		if err := d.mergeFile(file); err != nil {
			log.Error("Failed to merge", "file", file.Metadata.FileName, "error", err)
			d.errorChan <- err
			// Reset processing flag on error
			file.Processing = false
			continue
		}

		file.Merged = true
		file.Processing = false // Reset processing flag after successful merge
		d.mu.Lock()
		d.mergedFiles++
		d.mu.Unlock()

		log.Info("File merged successfully", "file", file.Metadata.FileName,
			"progress", fmt.Sprintf("%d/%d", d.mergedFiles, d.totalFiles),
			"startBlock", file.StartBlock, "endBlock", file.EndBlock)

		// // Process other files that may now be ready for merge
		// d.processNextMergeFiles(file)
	}
}

// mergeFile merges extracted incremental data with local data
func (d *IncrDownloader) mergeFile(file *IncrFileInfo) error {
	extractDir := filepath.Join(d.incrPath)
	if err := MergeIncrSnapshot(d.db, d.triedb, extractDir, file.StartBlock); err != nil {
		return err
	}
	log.Info("Merged incremental data", "file", file.Metadata.FileName, "extractDir", extractDir,
		"blocks", fmt.Sprintf("%d-%d", file.StartBlock, file.EndBlock))
	return nil
}

// progressMonitor monitors and reports download progress
func (d *IncrDownloader) progressMonitor() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case progress := <-d.progressChan:
			log.Info("Download progress", "file", progress.FileName,
				"progress", fmt.Sprintf("%.1f%%", progress.Progress),
				"speed", progress.Speed, "status", progress.Status)
		case <-ticker.C:
			d.mu.RLock()
			log.Info("Overall progress", "downloaded", d.downloadedFiles,
				"merged", d.mergedFiles, "total", d.totalFiles)
			d.mu.RUnlock()
		case <-d.ctx.Done():
			return
		}
	}
}

// progressReader tracks download progress
type progressReader struct {
	reader     io.Reader
	total      uint64
	downloaded uint64
	filename   string
	progress   chan<- *DownloadProgress
	lastUpdate time.Time
}

func (pr *progressReader) Read(p []byte) (int, error) {
	n, err := pr.reader.Read(p)
	pr.downloaded += uint64(n)

	// Update progress every 1 second
	if time.Since(pr.lastUpdate) > time.Second {
		progress := &DownloadProgress{
			FileName:       pr.filename,
			TotalSize:      pr.total,
			DownloadedSize: pr.downloaded,
			Progress:       float64(pr.downloaded) / float64(pr.total) * 100,
			Status:         "downloading",
		}

		select {
		case pr.progress <- progress:
		default:
		}

		pr.lastUpdate = time.Now()
	}

	return n, err
}

// GetProgress returns current download progress
func (d *IncrDownloader) GetProgress() (downloaded, merged, total int) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.downloadedFiles, d.mergedFiles, d.totalFiles
}

// GetPendingMergeStatus returns status of pending merge files
func (d *IncrDownloader) GetPendingMergeStatus() (int, []string) {
	d.mergeMutex.Lock()
	defer d.mergeMutex.Unlock()

	var fileNames []string
	for _, file := range d.pendingMergeFiles {
		fileNames = append(fileNames, file.Metadata.FileName)
	}

	return len(d.pendingMergeFiles), fileNames
}

// GetExpectedNextBlockStart returns the expected next block start for merge
func (d *IncrDownloader) GetExpectedNextBlockStart() uint64 {
	d.mergeMutex.Lock()
	defer d.mergeMutex.Unlock()
	return d.expectedNextBlockStart
}

// Cancel cancels all operations
func (d *IncrDownloader) Cancel() {
	d.cancel()
}

// Close closes all channels and cleans up
func (d *IncrDownloader) Close() {
	d.cancel()

	// Clean up pending merge files
	d.mergeMutex.Lock()
	d.pendingMergeFiles = make(map[uint64]*IncrFileInfo)
	d.mergeMutex.Unlock()

	close(d.progressChan)
	close(d.errorChan)
}

// Helper methods
func (d *IncrDownloader) getFirstBlockNum() uint64 {
	if len(d.files) == 0 {
		return 0
	}
	return d.files[0].StartBlock
}

func (d *IncrDownloader) getLastBlockNum() uint64 {
	if len(d.files) == 0 {
		return 0
	}
	return d.files[len(d.files)-1].EndBlock
}

// checkRangeSupport checks if server supports range requests
func (d *IncrDownloader) checkRangeSupport(url string) (bool, int64, error) {
	req, err := http.NewRequest("HEAD", url, nil)
	if err != nil {
		return false, 0, err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false, 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return false, 0, fmt.Errorf("HTTP error: %d", resp.StatusCode)
	}

	acceptRanges := resp.Header.Get("Accept-Ranges")
	contentLength := resp.ContentLength

	return acceptRanges == "bytes", contentLength, nil
}

// downloadSingleThreaded downloads file without range requests
func (d *IncrDownloader) downloadSingleThreaded(url string, file *IncrFileInfo) error {
	log.Info("Starting single-threaded download", "file", file.Metadata.FileName)

	resp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("HTTP GET failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP error: %d", resp.StatusCode)
	}

	out, err := os.Create(file.LocalPath)
	if err != nil {
		return fmt.Errorf("failed to create file: %v", err)
	}
	defer out.Close()

	// Track progress
	pr := &progressReader{
		reader:   resp.Body,
		total:    uint64(resp.ContentLength),
		filename: file.Metadata.FileName,
		progress: d.progressChan,
	}

	_, err = io.Copy(out, pr)
	return err
}

// checkExistingChunks checks for existing partial downloads
func (d *IncrDownloader) checkExistingChunks(chunks []*ChunkInfo) {
	for _, chunk := range chunks {
		if info, err := os.Stat(chunk.TempFile); err == nil {
			downloaded := info.Size()
			expectedSize := chunk.End - chunk.Start + 1

			if downloaded == expectedSize {
				chunk.Completed = true
				chunk.Downloaded = downloaded
				log.Info("Found completed chunk", "chunk", chunk.Index, "size", downloaded)
			} else if downloaded > 0 {
				chunk.Downloaded = downloaded
				// Update start position for resume
				chunk.Start += downloaded
				log.Info("Found partial chunk", "chunk", chunk.Index, "downloaded", downloaded, "remaining", expectedSize-downloaded)
			}
		}
	}
}

// downloadChunk downloads a single chunk with retry mechanism
func (d *IncrDownloader) downloadChunk(url string, chunk *ChunkInfo, progressChan chan<- *ChunkProgress) error {
	const maxRetries = 3
	const baseDelay = time.Second

	for attempt := 1; attempt <= maxRetries; attempt++ {
		err := d.downloadChunkAttempt(url, chunk, progressChan)
		if err == nil {
			// Success, no need to retry
			return nil
		}

		// Log the error for this attempt
		log.Warn("Chunk download attempt failed", "chunk", chunk.Index, "attempt", attempt,
			"maxRetries", maxRetries, "error", err)

		// If this is the last attempt, return the error
		if attempt == maxRetries {
			return fmt.Errorf("chunk download failed after %d attempts, last error: %w", maxRetries, err)
		}

		// Calculate delay with exponential backoff
		delay := baseDelay * time.Duration(attempt)
		log.Info("Retrying chunk download", "chunk", chunk.Index, "attempt", attempt+1, "delay", delay)

		// Wait before retrying
		select {
		case <-d.ctx.Done():
			return d.ctx.Err()
		case <-time.After(delay):
			continue
		}
	}

	return fmt.Errorf("chunk download failed after %d attempts", maxRetries)
}

// downloadChunkAttempt performs a single attempt to download a chunk
func (d *IncrDownloader) downloadChunkAttempt(url string, chunk *ChunkInfo, progressChan chan<- *ChunkProgress) error {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return err
	}

	// Set range header
	rangeHeader := fmt.Sprintf("bytes=%d-%d", chunk.Start, chunk.End)
	req.Header.Set("Range", rangeHeader)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusPartialContent && resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP error: %d", resp.StatusCode)
	}

	// Open temp file for writing (append mode for resume)
	var out *os.File
	if chunk.Downloaded > 0 {
		out, err = os.OpenFile(chunk.TempFile, os.O_WRONLY|os.O_APPEND, 0644)
	} else {
		out, err = os.Create(chunk.TempFile)
	}
	if err != nil {
		return err
	}
	defer out.Close()

	// Track progress
	chunkSize := chunk.End - chunk.Start + 1
	downloaded := chunk.Downloaded

	buffer := make([]byte, 32*1024) // 32KB buffer
	lastProgress := time.Now()

	for {
		select {
		case <-d.ctx.Done():
			return d.ctx.Err()
		default:
		}

		n, err := resp.Body.Read(buffer)
		if n > 0 {
			if _, writeErr := out.Write(buffer[:n]); writeErr != nil {
				return writeErr
			}
			downloaded += int64(n)

			// Send progress update every 100ms
			if time.Since(lastProgress) > 100*time.Millisecond {
				select {
				case progressChan <- &ChunkProgress{
					ChunkIndex: chunk.Index,
					Downloaded: downloaded,
					Total:      chunkSize,
					FileName:   "",
				}:
				default:
				}
				lastProgress = time.Now()
			}
		}

		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
	}

	chunk.Downloaded = downloaded
	chunk.Completed = true
	return nil
}

// mergeChunks merges all chunks into final file
func (d *IncrDownloader) mergeChunks(chunks []*ChunkInfo, outputPath string) error {
	output, err := os.Create(outputPath)
	if err != nil {
		return err
	}
	defer output.Close()

	for _, chunk := range chunks {
		chunkFile, err := os.Open(chunk.TempFile)
		if err != nil {
			return fmt.Errorf("failed to open chunk %d: %v", chunk.Index, err)
		}

		_, err = io.Copy(output, chunkFile)
		chunkFile.Close()

		if err != nil {
			return fmt.Errorf("failed to copy chunk %d: %v", chunk.Index, err)
		}
	}

	return nil
}

// cleanupTempFiles removes temporary chunk files
func (d *IncrDownloader) cleanupTempFiles(chunks []*ChunkInfo) {
	for _, chunk := range chunks {
		if err := os.Remove(chunk.TempFile); err != nil {
			log.Warn("Failed to remove temp file", "file", chunk.TempFile, "error", err)
		}
	}
}
