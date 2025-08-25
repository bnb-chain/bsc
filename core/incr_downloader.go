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

	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/triedb"
	"github.com/pierrec/lz4/v4"
)

const (
	incrSnapshotNamePattern = `(.*)-incr-(\d+)-(\d+)\.tar\.lz4`
	maxRetries              = 5
	baseDelay               = time.Second
)

// Database keys for download status
var (
	incrDownloadedFilesKey = []byte("incr_downloaded_files")
	incrToDownloadFilesKey = []byte("incr_to_download_files")
	incrMergedFilesKey     = []byte("incr_merged_files")
	incrToMergeFilesKey    = []byte("incr_to_merge_files")
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
	downloadedFilesMap     map[uint64]*IncrFileInfo // Downloaded files ready for merge, key is StartBlock
	mergeMutex             sync.Mutex               // Protects merge-related state
}

// NewIncrDownloader creates a new incremental downloader
func NewIncrDownloader(db ethdb.Database, triedb *triedb.Database, remoteURL, incrPath string, localBlockNum uint64) *IncrDownloader {
	ctx, cancel := context.WithCancel(context.Background())
	// we don't validate the url and assume it's valid
	newURL := strings.TrimSuffix(remoteURL, "/")

	downloader := &IncrDownloader{
		db:                 db,
		triedb:             triedb,
		remoteURL:          newURL,
		incrPath:           incrPath,
		localBlockNum:      localBlockNum,
		downloadChan:       make(chan *IncrFileInfo, 100),
		progressChan:       make(chan *DownloadProgress, 100),
		errorChan:          make(chan error, 10),
		ctx:                ctx,
		cancel:             cancel,
		downloadedFilesMap: make(map[uint64]*IncrFileInfo),
	}

	return downloader
}

// saveDownloadedFiles saves list of downloaded files to db
func (d *IncrDownloader) saveDownloadedFiles(files []string) error {
	data, err := json.Marshal(files)
	if err != nil {
		return fmt.Errorf("failed to marshal downloaded files: %v", err)
	}
	return d.db.Put(incrDownloadedFilesKey, data)
}

// loadDownloadedFiles loads list of downloaded files from db
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

// saveToDownloadFiles saves list of currently to download files to db
func (d *IncrDownloader) saveToDownloadFiles(files []string) error {
	data, err := json.Marshal(files)
	if err != nil {
		return fmt.Errorf("failed to marshal to download files: %v", err)
	}
	return d.db.Put(incrToDownloadFilesKey, data)
}

// loadToDownloadFiles loads list of currently to download files from db
func (d *IncrDownloader) loadToDownloadFiles() ([]string, error) {
	data, err := d.db.Get(incrToDownloadFilesKey)
	if err != nil {
		return nil, err
	}

	var files []string
	if err = json.Unmarshal(data, &files); err != nil {
		return nil, fmt.Errorf("failed to unmarshal to download files: %v", err)
	}
	return files, nil
}

// saveDownloadedFiles saves list of downloaded files to db
func (d *IncrDownloader) saveMergedFiles(files []string) error {
	data, err := json.Marshal(files)
	if err != nil {
		return fmt.Errorf("failed to marshal merged files: %v", err)
	}
	return d.db.Put(incrMergedFilesKey, data)
}

// loadMergedFiles loads list of merged files from db
func (d *IncrDownloader) loadMergedFiles() ([]string, error) {
	data, err := d.db.Get(incrMergedFilesKey)
	if err != nil {
		return nil, err
	}

	var files []string
	if err = json.Unmarshal(data, &files); err != nil {
		return nil, fmt.Errorf("failed to unmarshal merged files: %v", err)
	}
	return files, nil
}

// saveToMergeFiles saves list of currently to merge files to db
func (d *IncrDownloader) saveToMergeFiles(files []string) error {
	data, err := json.Marshal(files)
	if err != nil {
		return fmt.Errorf("failed to marshal to merge files: %v", err)
	}
	return d.db.Put(incrToMergeFilesKey, data)
}

// loadToMergeFiles loads list of currently to merge files from db
func (d *IncrDownloader) loadToMergeFiles() ([]string, error) {
	data, err := d.db.Get(incrToMergeFilesKey)
	if err != nil {
		return nil, err
	}

	var files []string
	if err = json.Unmarshal(data, &files); err != nil {
		return nil, fmt.Errorf("failed to unmarshal to merge files: %v", err)
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

	// Parse and filter file info
	files, err := d.parseFileInfo(metadata)
	if err != nil {
		log.Error("Failed to parse file info", "error", err)
		return err
	}

	// Process file status and categorize files
	if err = d.processFileStatus(files); err != nil {
		return err
	}
	return nil
}

// processFileStatus processes file status and categorizes files for download and merge
// toDownload -> downloaded -> toMerge -> merged
func (d *IncrDownloader) processFileStatus(files []*IncrFileInfo) error {
	// Load existing file status from database
	downloadedFiles, err := d.loadDownloadedFiles()
	if err != nil {
		log.Warn("Failed to load downloaded files list, starting fresh")
		downloadedFiles = []string{}
	}

	mergedFiles, err := d.loadMergedFiles()
	if err != nil {
		log.Warn("Failed to load merged files list, starting fresh")
		mergedFiles = []string{}
	}

	toDownloadFiles, err := d.loadToDownloadFiles()
	if err != nil {
		log.Warn("Failed to load to download files list, starting fresh")
		toDownloadFiles = []string{}
	}

	toMergeFiles, err := d.loadToMergeFiles()
	if err != nil {
		log.Warn("Failed to load to merge files list, starting fresh")
		toMergeFiles = []string{}
	}

	// Create sets for quick lookup
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

	// Filter and categorize files based on their current status
	var newToDownloadFiles []*IncrFileInfo
	var newToDownloadFileNames []string
	var newToMergeFiles []*IncrFileInfo
	var newToMergeFileNames []string

	for _, file := range files {
		fileName := file.Metadata.FileName

		// Skip already merged files
		if mergedSet[fileName] {
			log.Debug("Skipping already merged file", "fileName", fileName)
			continue
		}

		// Check if file is already downloaded
		if downloadedSet[fileName] {
			log.Debug("File already downloaded, checking merge status", "fileName", fileName)
			d.downloadedFiles++

			// If downloaded but not merged, add to merge queue
			if !mergedSet[fileName] {
				if !toMergeSet[fileName] {
					newToMergeFiles = append(newToMergeFiles, file)
					newToMergeFileNames = append(newToMergeFileNames, fileName)
					log.Debug("Adding downloaded file to merge queue", "fileName", fileName)
				}
			}
			continue
		}

		// Check if file is currently being downloaded
		if toDownloadSet[fileName] {
			log.Debug("File is currently being downloaded, keeping in download queue", "fileName", fileName)
			newToDownloadFiles = append(newToDownloadFiles, file)
			newToDownloadFileNames = append(newToDownloadFileNames, fileName)
			continue
		}

		// Check if file is currently being merged
		if toMergeSet[fileName] {
			log.Debug("File is currently being merged, keeping in merge queue", "fileName", fileName)
			newToMergeFiles = append(newToMergeFiles, file)
			newToMergeFileNames = append(newToMergeFileNames, fileName)
			continue
		}

		// New file to download
		log.Debug("Adding new file to download queue", "fileName", fileName)
		newToDownloadFiles = append(newToDownloadFiles, file)
		newToDownloadFileNames = append(newToDownloadFileNames, fileName)
	}

	// Update file lists
	d.files = newToDownloadFiles
	d.totalFiles = len(d.files)

	if err = d.saveToDownloadFiles(newToDownloadFileNames); err != nil {
		log.Error("Failed to save to download files", "error", err)
		return err
	}

	if err = d.saveToMergeFiles(newToMergeFileNames); err != nil {
		log.Error("Failed to save to merge files", "error", err)
		return err
	}

	// Initialize downloaded files map with files that are ready for merge
	for _, file := range newToMergeFiles {
		d.downloadedFilesMap[file.StartBlock] = file
		log.Debug("Added file to downloaded files map for merge", "fileName", file.Metadata.FileName, "startBlock", file.StartBlock)
	}

	// Initialize expected next block start for merge ordering
	if len(newToMergeFiles) > 0 {
		// Find the earliest start block among files ready for merge
		earliestBlock := newToMergeFiles[0].StartBlock
		for _, file := range newToMergeFiles {
			if file.StartBlock < earliestBlock {
				earliestBlock = file.StartBlock
			}
		}
		d.expectedNextBlockStart = earliestBlock
		log.Info("Initialized expected next block start from existing merge queue", "expectedNextBlockStart", d.expectedNextBlockStart)
	} else if len(d.files) > 0 {
		d.expectedNextBlockStart = d.files[0].StartBlock
		log.Info("Initialized expected next block start from new files", "expectedNextBlockStart", d.expectedNextBlockStart)
	}

	log.Info("Preparation completed", "totalFiles", d.totalFiles, "downloadedFiles", len(downloadedFiles),
		"mergedFiles", len(mergedFiles), "toDownloadFiles", len(newToDownloadFileNames),
		"toMergeFiles", len(newToMergeFileNames), "firstBlock", d.getFirstBlockNum(), "lastBlock", d.getLastBlockNum())
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
		log.Debug("Download goroutine completed")
	}()

	wg.Wait()
	log.Info("All downloads completed, waiting for merge to complete")

	// Wait for merge worker to complete
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

	// Sort files by end block in ascending order
	sort.Slice(files, func(i, j int) bool {
		return files[i].EndBlock < files[j].EndBlock
	})
	// Check continuity of all files before filtering
	if err := d.checkFileContinuity(files); err != nil {
		return nil, err
	}
	// filter the block number that matches local data
	for index, file := range files {
		// Check if local block number falls within this file's range
		if file.StartBlock <= d.localBlockNum && d.localBlockNum <= file.EndBlock {
			// Found the file containing local block number, add all remaining files from this point
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
		// Mark file as to download
		d.markFileAsToDownload(file.Metadata.FileName)

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

		log.Debug("File completed, queuing for merge", "file", file.Metadata.FileName, "downloadedFiles", d.downloadedFiles)
		d.queueForMerge(file)
	}
}

// markFileAsToDownload marks a file as currently to download
func (d *IncrDownloader) markFileAsToDownload(fileName string) {
	d.mu.Lock()
	defer d.mu.Unlock()

	downloadingFiles, _ := d.loadToDownloadFiles()
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
		d.saveToDownloadFiles(downloadingFiles)
		log.Debug("Marked file as downloading", "fileName", fileName)
	}
}

// removeFromToDownload removes a file from to download list
func (d *IncrDownloader) removeFromToDownload(fileName string) {
	toDownloadFiles, err := d.loadToDownloadFiles()
	if err != nil {
		log.Error("Failed to load to download files", "error", err)
	}

	if toDownloadFiles != nil {
		var newDownloadingFiles []string
		for _, file := range toDownloadFiles {
			if file != fileName {
				newDownloadingFiles = append(newDownloadingFiles, file)
			}
		}
		d.saveToDownloadFiles(newDownloadingFiles)
		log.Debug("Removed file from to download list", "fileName", fileName)
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
		log.Debug("Marked file as downloaded", "fileName", fileName)
	}

	// Remove from to download files
	d.removeFromToDownload(fileName)
}

// markFileAsToMerge marks a file as currently to merge
func (d *IncrDownloader) markFileAsToMerge(fileName string) {
	d.mu.Lock()
	defer d.mu.Unlock()

	toMergeFiles, _ := d.loadToMergeFiles()
	if toMergeFiles == nil {
		toMergeFiles = []string{}
	}

	// Check if already in list
	found := false
	for _, file := range toMergeFiles {
		if file == fileName {
			found = true
			break
		}
	}

	if !found {
		toMergeFiles = append(toMergeFiles, fileName)
		d.saveToMergeFiles(toMergeFiles)
		log.Debug("Marked file as to merge", "fileName", fileName)
	}
}

// removeFromToMerge removes a file from to merge list
func (d *IncrDownloader) removeFromToMerge(fileName string) {
	toMergeFiles, err := d.loadToMergeFiles()
	if err != nil {
		log.Error("Failed to load to merge files", "error", err)
		return
	}

	if toMergeFiles != nil {
		var newToMergeFiles []string
		for _, file := range toMergeFiles {
			if file != fileName {
				newToMergeFiles = append(newToMergeFiles, file)
			}
		}
		d.saveToMergeFiles(newToMergeFiles)
		log.Debug("Removed file from to merge list", "fileName", fileName)
	}
}

// markFileAsMerged marks a file as merged and removes from to merge list
func (d *IncrDownloader) markFileAsMerged(fileName string) {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Add to merged files
	mergedFiles, _ := d.loadMergedFiles()
	if mergedFiles == nil {
		mergedFiles = []string{}
	}
	// Check if already in list
	found := false
	for _, file := range mergedFiles {
		if file == fileName {
			found = true
			break
		}
	}

	if !found {
		mergedFiles = append(mergedFiles, fileName)
		d.saveMergedFiles(mergedFiles)
		log.Debug("Marked file as merged", "fileName", fileName)
	}

	// Remove from to merge files
	d.removeFromToMerge(fileName)
}

// queueForMerge queues a file for merge (non-blocking)
func (d *IncrDownloader) queueForMerge(file *IncrFileInfo) {
	d.mergeMutex.Lock()
	defer d.mergeMutex.Unlock()

	// Check if file is already in downloaded files map
	if existingFile, exists := d.downloadedFilesMap[file.StartBlock]; exists {
		if existingFile.Metadata.FileName == file.Metadata.FileName {
			log.Debug("File already in downloaded files map, skipping", "file", file.Metadata.FileName, "startBlock", file.StartBlock)
			return
		}
	}

	// Check if file has already been merged
	if file.Merged {
		log.Debug("File already merged, skipping queue", "file", file.Metadata.FileName, "startBlock", file.StartBlock)
		return
	}

	// Add to downloaded files map
	d.downloadedFilesMap[file.StartBlock] = file

	// Mark file as to merge in database
	d.markFileAsToMerge(file.Metadata.FileName)

	log.Debug("File queued for merge", "file", file.Metadata.FileName, "startBlock", file.StartBlock)
}

// downloadFile downloads a single file with retry mechanism
func (d *IncrDownloader) downloadFile(file *IncrFileInfo) error {
	for attempt := 1; attempt <= maxRetries; attempt++ {
		err := d.downloadWithHTTP(file)
		if err == nil {
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
	log.Info("Start downloading incremental snapshot", "url", url)

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
	numChunks := 8
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

	log.Debug("Download completed successfully", "file", file.Metadata.FileName, "size", totalSize)
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

	log.Debug("Finished verifying md5 hash", "file", file.LocalPath)
	return nil
}

// extractFile extracts tar.lz4 file
func (d *IncrDownloader) extractFile(file *IncrFileInfo) error {
	// Extract directory
	extractDir := filepath.Join(d.incrPath)
	if err := os.MkdirAll(extractDir, 0755); err != nil {
		return err
	}
	log.Debug("Extracting file", "file", file.Metadata.FileName, "extractDir", extractDir)

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

	log.Debug("File extracted successfully", "file", file.Metadata.FileName, "extractDir", extractDir)
	return nil
}

// mergeWorker handles sequential merging of files
func (d *IncrDownloader) mergeWorker() {
	defer d.mergeWG.Done()

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			// Check if there are files ready for merge
			d.mergeMutex.Lock()
			currFile, exists := d.downloadedFilesMap[d.expectedNextBlockStart]
			d.mergeMutex.Unlock()

			if exists {
				// Process the next file in sequence
				log.Info("Processing file for merge", "file", currFile.Metadata.FileName,
					"startBlock", currFile.StartBlock, "endBlock", currFile.EndBlock)

				// Check if file has already been merged
				if currFile.Merged {
					log.Warn("File already merged, removing from map", "file", currFile.Metadata.FileName)
					d.mergeMutex.Lock()
					delete(d.downloadedFilesMap, d.expectedNextBlockStart)
					d.mergeMutex.Unlock()
					d.expectedNextBlockStart = currFile.EndBlock + 1
					continue
				}

				// Remove from map before processing to prevent race conditions
				d.mergeMutex.Lock()
				delete(d.downloadedFilesMap, d.expectedNextBlockStart)
				d.mergeMutex.Unlock()

				// Check if this is the last incr snapshot
				d.mu.RLock()
				isLastSnapshot := (d.mergedFiles + 1) == len(d.files)
				d.mu.RUnlock()

				// Perform merge operation
				path := filepath.Join(d.incrPath, fmt.Sprintf("incr-%d-%d", currFile.StartBlock, currFile.EndBlock))
				if isLastSnapshot {
					log.Info("This is the last incremental snapshot, performing final cleanup and optimization")
					complete, err := rawdb.CheckIncrSnapshotComplete(path)
					if err != nil {
						log.Error("Failed to check last incr snapshot complete", "err", err)
						d.errorChan <- err
						return
					}
					if !complete {
						log.Warn("Skip last incr snapshot due to data is incomplete")
						return
					}
				}
				if err := MergeIncrSnapshot(d.db, d.triedb, path); err != nil {
					log.Error("Failed to merge", "file", currFile.Metadata.FileName, "error", err)
					d.errorChan <- err
					return
				}

				currFile.Merged = true
				d.markFileAsMerged(currFile.Metadata.FileName)

				d.mu.Lock()
				d.mergedFiles++
				d.mu.Unlock()

				// Update expected next start block
				d.expectedNextBlockStart = currFile.EndBlock + 1
				log.Info("File merged successfully", "file", currFile.Metadata.FileName,
					"progress", fmt.Sprintf("%d/%d", d.mergedFiles, d.totalFiles),
					"startBlock", currFile.StartBlock, "endBlock", currFile.EndBlock)
			} else {
				// Check if all files have been processed
				d.mu.RLock()
				downloadedCount := d.downloadedFiles
				mergedCount := d.mergedFiles
				d.mu.RUnlock()

				if mergedCount == downloadedCount && downloadedCount == len(d.files) {
					log.Info("All files processed, merge worker exiting")
					return
				}
			}

		case <-d.ctx.Done():
			log.Info("Context cancelled, merge worker exiting")
			return
		}
	}
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
			stats := d.GetFileStatusStats()
			log.Info("Overall progress", "downloaded", d.downloadedFiles,
				"merged", d.mergedFiles, "total", d.totalFiles, "stats", stats)
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

	// Update progress every 30 seconds
	if time.Since(pr.lastUpdate) > (time.Second * 30) {
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

// Cancel cancels all operations
func (d *IncrDownloader) Cancel() {
	d.cancel()
}

// Close closes all channels and cleans up
func (d *IncrDownloader) Close() {
	d.cancel()

	// Clean up pending merge files
	d.mergeMutex.Lock()
	d.downloadedFilesMap = make(map[uint64]*IncrFileInfo)
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

	client := &http.Client{}
	resp, err := client.Do(req)
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

	client := &http.Client{}
	resp, err := client.Get(url)
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
				log.Debug("Found completed chunk", "chunk", chunk.Index, "size", downloaded)
			} else if downloaded > 0 {
				chunk.Downloaded = downloaded
				// Update start position for resume
				chunk.Start += downloaded
				log.Debug("Found partial chunk", "chunk", chunk.Index, "downloaded", downloaded, "remaining", expectedSize-downloaded)
			}
		}
	}
}

// downloadChunk downloads a single chunk with retry mechanism
func (d *IncrDownloader) downloadChunk(url string, chunk *ChunkInfo, progressChan chan<- *ChunkProgress) error {
	for attempt := 1; attempt <= maxRetries; attempt++ {
		err := d.downloadChunkAttempt(url, chunk, progressChan)
		if err == nil {
			// Success, no need to retry
			return nil
		}

		// Log the error for this attempt
		log.Warn("Failed to download chunk", "chunk", chunk.Index, "attempt", attempt,
			"maxRetries", maxRetries, "error", err)

		// If this is the last attempt, return the error
		if attempt == maxRetries {
			return fmt.Errorf("failed to download chunk after %d attempts, last error: %w", maxRetries, err)
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
				log.Error("Failed to write to file", "error", writeErr)
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
			log.Error("Failed to read from response body", "error", err)
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

// GetFileStatusStats returns current file status statistics
func (d *IncrDownloader) GetFileStatusStats() map[string]interface{} {
	d.mu.RLock()
	defer d.mu.RUnlock()

	downloadedFiles, _ := d.loadDownloadedFiles()
	mergedFiles, _ := d.loadMergedFiles()
	toDownloadFiles, _ := d.loadToDownloadFiles()
	toMergeFiles, _ := d.loadToMergeFiles()

	return map[string]interface{}{
		"totalFiles":             d.totalFiles,
		"downloadedFiles":        len(downloadedFiles),
		"mergedFiles":            len(mergedFiles),
		"toDownloadFiles":        len(toDownloadFiles),
		"toMergeFiles":           len(toMergeFiles),
		"downloadedFilesMap":     len(d.downloadedFilesMap),
		"expectedNextBlockStart": d.expectedNextBlockStart,
	}
}
