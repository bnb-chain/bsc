// Copyright 2022 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

package pathdb

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/rlp"
)

var (
	errMissJournal       = errors.New("journal not found")
	errMissVersion       = errors.New("version not found")
	errUnexpectedVersion = errors.New("unexpected journal version")
	errMissDiskRoot      = errors.New("disk layer root not found")
	errUnmatchedJournal  = errors.New("unmatched journal")
)

// journalVersion ensures that an incompatible journal is detected and discarded.
//
// Changelog:
//
// - Version 0: initial version
// - Version 1: storage.Incomplete field is removed
// - Version 2: add post-modification state values
// - Version 3: a flag has been added to indicate whether the storage slot key is the raw key or a hash
const journalVersion uint64 = 3

type JournalWriter interface {
	io.Writer

	Close()
	Size() uint64
}

type JournalReader interface {
	io.Reader
	Close()
}

type JournalFileWriter struct {
	file *os.File
}

type JournalFileReader struct {
	file *os.File
}

type JournalKVWriter struct {
	journalBuf bytes.Buffer
	diskdb     ethdb.Database
}

type JournalKVReader struct {
	journalBuf *bytes.Buffer
}

// Write appends b directly to the encoder output.
func (fw *JournalFileWriter) Write(b []byte) (int, error) {
	return fw.file.Write(b)
}

func (fw *JournalFileWriter) Close() {
	fw.file.Close()
}

func (fw *JournalFileWriter) Size() uint64 {
	if fw.file == nil {
		return 0
	}
	fileInfo, err := fw.file.Stat()
	if err != nil {
		log.Crit("Failed to stat journal", "err", err)
	}
	return uint64(fileInfo.Size())
}

func (kw *JournalKVWriter) Write(b []byte) (int, error) {
	return kw.journalBuf.Write(b)
}

func (kw *JournalKVWriter) Close() {
	rawdb.WriteTrieJournal(kw.diskdb, kw.journalBuf.Bytes())
	kw.journalBuf.Reset()
}

func (kw *JournalKVWriter) Size() uint64 {
	return uint64(kw.journalBuf.Len())
}

func (fr *JournalFileReader) Read(p []byte) (n int, err error) {
	return fr.file.Read(p)
}

func (fr *JournalFileReader) Close() {
	fr.file.Close()
}

func (kr *JournalKVReader) Read(p []byte) (n int, err error) {
	return kr.journalBuf.Read(p)
}

func (kr *JournalKVReader) Close() {
}

func newJournalWriter(file string, db ethdb.Database, journalType JournalType) JournalWriter {
	if journalType == JournalKVType {
		log.Info("New journal writer for journal kv")
		return &JournalKVWriter{
			diskdb: db,
		}
	} else {
		log.Info("New journal writer for journal file", "path", file)
		fd, err := os.OpenFile(file, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			return nil
		}
		return &JournalFileWriter{
			file: fd,
		}
	}
}

func newJournalReader(file string, db ethdb.Database, journalType JournalType) (JournalReader, error) {
	if journalType == JournalKVType {
		log.Info("New journal reader for journal kv")
		journal := rawdb.ReadTrieJournal(db)
		if len(journal) == 0 {
			return nil, errMissJournal
		}
		return &JournalKVReader{
			journalBuf: bytes.NewBuffer(journal),
		}, nil
	} else {
		log.Info("New journal reader for journal file", "path", file)
		fd, err := os.Open(file)
		if errors.Is(err, fs.ErrNotExist) {
			return nil, errMissJournal
		}
		if err != nil {
			return nil, err
		}
		return &JournalFileReader{
			file: fd,
		}, nil
	}
}

// loadJournal tries to parse the layer journal from the disk.
func (db *Database) loadJournal(diskRoot common.Hash) (layer, error) {
	start := time.Now()
	journalTypeForReader := db.DetermineJournalTypeForReader()
	reader, err := newJournalReader(db.config.JournalFilePath, db.diskdb, journalTypeForReader)

	if err != nil {
		return nil, err
	}
	if reader != nil {
		defer reader.Close()
	}
	r := rlp.NewStream(reader, 0)

	// Firstly, resolve the first element as the journal version
	version, err := r.Uint64()
	if err != nil {
		return nil, errMissVersion
	}
	if version != journalVersion {
		return nil, fmt.Errorf("%w want %d got %d", errUnexpectedVersion, journalVersion, version)
	}
	// Secondly, resolve the disk layer root, ensure it's continuous
	// with disk layer. Note now we can ensure it's the layer journal
	// correct version, so we expect everything can be resolved properly.
	var root common.Hash
	if err := r.Decode(&root); err != nil {
		return nil, errMissDiskRoot
	}
	// The journal is not matched with persistent state, discard them.
	// It can happen that geth crashes without persisting the journal.
	if !bytes.Equal(root.Bytes(), diskRoot.Bytes()) {
		return nil, fmt.Errorf("%w want %x got %x", errUnmatchedJournal, root, diskRoot)
	}
	// Load the disk layer from the journal
	base, err := db.loadDiskLayer(r, journalTypeForReader, version)
	if err != nil {
		return nil, err
	}
	// Load all the diff layers from the journal
	head, err := db.loadDiffLayer(base, r, journalTypeForReader)
	if err != nil {
		return nil, err
	}
	log.Info("Loaded layer journal", "diskroot", diskRoot, "diffhead", head.rootHash(), "elapsed", common.PrettyDuration(time.Since(start)))
	return head, nil
}

// journalGenerator is a disk layer entry containing the generator progress marker.
type journalGenerator struct {
	// Indicator that whether the database was in progress of being wiped.
	// It's deprecated but keep it here for backward compatibility.
	Wiping bool

	Done     bool // Whether the generator finished creating the snapshot
	Marker   []byte
	Accounts uint64
	Slots    uint64
	Storage  uint64
}

// loadGenerator loads the state generation progress marker from the database.
func loadGenerator(db ethdb.KeyValueReader, hash nodeHasher) (*journalGenerator, common.Hash, error) {
	trieRoot, err := hash(rawdb.ReadAccountTrieNode(db, nil))
	if err != nil {
		return nil, common.Hash{}, err
	}
	// State generation progress marker is lost, rebuild it
	blob := rawdb.ReadSnapshotGenerator(db)
	if len(blob) == 0 {
		log.Info("State snapshot generator is not found")
		return nil, trieRoot, nil
	}
	// State generation progress marker is not compatible, rebuild it
	var generator journalGenerator
	if err := rlp.DecodeBytes(blob, &generator); err != nil {
		log.Info("State snapshot generator is not compatible")
		return nil, trieRoot, nil
	}
	// The state snapshot is inconsistent with the trie data and must
	// be rebuilt.
	//
	// Note: The SnapshotRoot and SnapshotGenerator are always consistent
	// with each other, both in the legacy state snapshot and the path database.
	// Therefore, if the SnapshotRoot does not match the trie root,
	// the entire generator is considered stale and must be discarded.
	stateRoot := rawdb.ReadSnapshotRoot(db)
	if trieRoot != stateRoot {
		log.Info("State snapshot is not consistent", "trie", trieRoot, "state", stateRoot)
		return nil, trieRoot, nil
	}
	// Slice null-ness is lost after rlp decoding, reset it back to empty
	if !generator.Done && generator.Marker == nil {
		generator.Marker = []byte{}
	}
	return &generator, trieRoot, nil
}

// loadLayers loads a pre-existing state layer backed by a key-value store.
func (db *Database) loadLayers() layer {
	// Retrieve the root node of persistent state.
	root, err := db.hasher(rawdb.ReadAccountTrieNode(db.diskdb, nil))
	if err != nil {
		log.Crit("Failed to compute node hash", "err", err)
	}
	// Load the layers by resolving the journal
	head, err := db.loadJournal(root)
	if err == nil {
		return head
	}
	// journal is not matched(or missing) with the persistent state, discard
	// it. Display log for discarding journal, but try to avoid showing
	// useless information when the db is created from scratch.
	if !(root == types.EmptyRootHash && errors.Is(err, errMissJournal)) {
		log.Info("Failed to load journal, discard it", "err", err)
	}
	// try to load node buffer only, it can be compatible with old journal ver
	base, err := db.loadNodeBufferAsJournalV0V1(root)
	if err == nil {
		log.Info("load legacy node buffer successful", "new root", base.rootHash(), "disk root", root)
		return base
	}
	log.Warn("Failed to load disk journal with node buffer, discard it", "err", err)
	// Return single layer with persistent state.
	return newDiskLayer(root, rawdb.ReadPersistentStateID(db.diskdb), db, nil, nil, newBuffer(db.config.WriteBufferSize, nil, nil, 0), nil)
}

// loadNodeBufferAsJournalV0V1 try to load legacy node buffer data from journal
// TODO(galaio): the method is a temporary solution for legacy journal, it could be removed in the future
func (db *Database) loadNodeBufferAsJournalV0V1(diskRoot common.Hash) (layer, error) {
	journalTypeForReader := db.DetermineJournalTypeForReader()
	reader, err := newJournalReader(db.config.JournalFilePath, db.diskdb, journalTypeForReader)

	if err != nil {
		return nil, err
	}
	if reader != nil {
		defer reader.Close()
	}
	r := rlp.NewStream(reader, 0)

	// read & check journal version
	version, err := r.Uint64()
	if err != nil {
		return nil, errMissVersion
	}
	if version >= journalVersion {
		return nil, fmt.Errorf("%w, only handle legacy journal version, got %v", errUnexpectedVersion, version)
	}

	// read & check disk root
	var root common.Hash
	if err := r.Decode(&root); err != nil {
		return nil, errMissDiskRoot
	}
	if !bytes.Equal(root.Bytes(), diskRoot.Bytes()) {
		return nil, fmt.Errorf("%w want %x got %x", errUnmatchedJournal, root, diskRoot)
	}
	return db.loadDiskLayer(r, journalTypeForReader, version)
}

// loadDiskLayer reads the binary blob from the layer journal, reconstructing
// a new disk layer on it.
func (db *Database) loadDiskLayer(r *rlp.Stream, journalTypeForReader JournalType, version uint64) (layer, error) {
	// Resolve disk layer root
	var (
		root               common.Hash
		journalBuf         *rlp.Stream
		journalEncodedBuff []byte
	)
	if journalTypeForReader == JournalFileType {
		if err := r.Decode(&journalEncodedBuff); err != nil {
			return nil, fmt.Errorf("load disk journal: %v", err)
		}
		journalBuf = rlp.NewStream(bytes.NewReader(journalEncodedBuff), 0)
	} else {
		journalBuf = r
	}

	if err := journalBuf.Decode(&root); err != nil {
		return nil, fmt.Errorf("load disk root: %v", err)
	}
	// Resolve the state id of disk layer, it can be different
	// with the persistent id tracked in disk, the id distance
	// is the number of transitions aggregated in disk layer.
	var id uint64
	if err := journalBuf.Decode(&id); err != nil {
		return nil, fmt.Errorf("load state id: %v", err)
	}
	stored := rawdb.ReadPersistentStateID(db.diskdb)
	if stored > id {
		return nil, fmt.Errorf("invalid state id: stored %d resolved %d", stored, id)
	}
	// Resolve nodes cached in aggregated buffer
	var nodes nodeSet
	if err := nodes.decode(journalBuf); err != nil {
		return nil, err
	}

	// handle new states in journal v2
	var states = newStates(nil, nil, false)
	if version == journalVersion {
		// Resolve flat state sets in aggregated buffer
		if err := states.decode(journalBuf); err != nil {
			return nil, err
		}
	}

	if journalTypeForReader == JournalFileType {
		var shaSum [32]byte
		if err := r.Decode(&shaSum); err != nil {
			return nil, fmt.Errorf("load shasum: %v", err)
		}

		expectSum := sha256.Sum256(journalEncodedBuff)
		if shaSum != expectSum {
			return nil, fmt.Errorf("expect shaSum: %v, real:%v", expectSum, shaSum)
		}
	}

	// Calculate the internal state transitions by id difference.
	return newDiskLayer(root, id, db, nil, nil, newBuffer(db.config.WriteBufferSize, &nodes, states, id-stored), nil), nil
}

// loadDiffLayer reads the next sections of a layer journal, reconstructing a new
// diff and verifying that it can be linked to the requested parent.
func (db *Database) loadDiffLayer(parent layer, r *rlp.Stream, journalTypeForReader JournalType) (layer, error) {
	// Read the next diff journal entry
	var (
		root               common.Hash
		journalBuf         *rlp.Stream
		journalEncodedBuff []byte
	)
	if journalTypeForReader == JournalFileType {
		if err := r.Decode(&journalEncodedBuff); err != nil {
			// The first read may fail with EOF, marking the end of the journal
			if err == io.EOF {
				return parent, nil
			}
			return nil, fmt.Errorf("load disk journal buffer: %v", err)
		}
		journalBuf = rlp.NewStream(bytes.NewReader(journalEncodedBuff), 0)
	} else {
		journalBuf = r
	}

	if err := journalBuf.Decode(&root); err != nil {
		// The first read may fail with EOF, marking the end of the journal
		if err == io.EOF {
			return parent, nil
		}
		return nil, fmt.Errorf("load diff root: %v", err)
	}
	var block uint64
	if err := journalBuf.Decode(&block); err != nil {
		return nil, fmt.Errorf("load block number: %v", err)
	}
	// Read in-memory trie nodes from journal
	var nodes nodeSet
	if err := nodes.decode(journalBuf); err != nil {
		return nil, err
	}
	// Read flat states set (with original value attached) from journal
	var stateSet StateSetWithOrigin
	if err := stateSet.decode(journalBuf); err != nil {
		return nil, err
	}

	if journalTypeForReader == JournalFileType {
		var shaSum [32]byte
		if err := r.Decode(&shaSum); err != nil {
			return nil, fmt.Errorf("load shasum: %v", err)
		}

		expectSum := sha256.Sum256(journalEncodedBuff)
		if shaSum != expectSum {
			return nil, fmt.Errorf("expect shaSum: %v, real:%v", expectSum, shaSum)
		}
	}

	log.Debug("Loaded diff layer journal", "root", root, "parent", parent.rootHash(), "id", parent.stateID()+1, "block", block)

	return db.loadDiffLayer(newDiffLayer(parent, root, parent.stateID()+1, block, &nodes, &stateSet), r, journalTypeForReader)
}

// journal implements the layer interface, marshaling the un-flushed trie nodes
// along with layer metadata into provided byte buffer.
func (dl *diskLayer) journal(w io.Writer, journalType JournalType) error {
	dl.lock.RLock()
	defer dl.lock.RUnlock()

	// Create a buffer to store encoded data
	journalBuf := new(bytes.Buffer)

	// Ensure the layer didn't get stale
	if dl.stale {
		return errSnapshotStale
	}
	// Step one, write the disk root into the journal.
	if err := rlp.Encode(journalBuf, dl.root); err != nil {
		return err
	}
	// Step two, write the corresponding state id into the journal
	if err := rlp.Encode(journalBuf, dl.id); err != nil {
		return err
	}
	nodes, states := dl.buffer.nodes, dl.buffer.states
	// Step three, write the accumulated trie nodes into the journal
	if err := nodes.encode(journalBuf); err != nil {
		return err
	}
	// Step four, write the accumulated flat states into the journal
	if err := states.encode(journalBuf); err != nil {
		return err
	}

	// Store the journal buf into w and calculate checksum
	if journalType == JournalFileType {
		shasum := sha256.Sum256(journalBuf.Bytes())
		if err := rlp.Encode(w, journalBuf.Bytes()); err != nil {
			return err
		}
		if err := rlp.Encode(w, shasum); err != nil {
			return err
		}
	} else {
		if _, err := w.Write(journalBuf.Bytes()); err != nil {
			return err
		}
	}

	log.Info("Journaled pathdb disk layer", "root", dl.root)
	return nil
}

// journal implements the layer interface, writing the memory layer contents
// into a buffer to be stored in the database as the layer journal.
func (dl *diffLayer) journal(w io.Writer, journalType JournalType) error {
	dl.lock.RLock()
	defer dl.lock.RUnlock()

	// journal the parent first
	if err := dl.parent.journal(w, journalType); err != nil {
		return err
	}
	// Create a buffer to store encoded data
	journalBuf := new(bytes.Buffer)
	// Everything below was journaled, persist this layer too
	if err := rlp.Encode(journalBuf, dl.root); err != nil {
		return err
	}
	if err := rlp.Encode(journalBuf, dl.block); err != nil {
		return err
	}
	// Write the accumulated trie nodes into buffer
	if err := dl.nodes.encode(journalBuf); err != nil {
		return err
	}
	// Write the associated flat state set into buffer
	if err := dl.states.encode(journalBuf); err != nil {
		return err
	}

	// Store the journal buf into w and calculate checksum
	if journalType == JournalFileType {
		shasum := sha256.Sum256(journalBuf.Bytes())
		if err := rlp.Encode(w, journalBuf.Bytes()); err != nil {
			return err
		}
		if err := rlp.Encode(w, shasum); err != nil {
			return err
		}
	} else {
		if _, err := w.Write(journalBuf.Bytes()); err != nil {
			return err
		}
	}

	log.Info("Journaled pathdb diff layer", "root", dl.root, "parent", dl.parent.rootHash(), "id", dl.stateID(), "block", dl.block)
	return nil
}

// Journal commits an entire diff hierarchy to disk into a single journal entry.
// This is meant to be used during shutdown to persist the layer without
// flattening everything down (bad for reorgs). And this function will mark the
// database as read-only to prevent all following mutation to disk.
//
// The supplied root must be a valid trie hash value.
func (db *Database) Journal(root common.Hash) error {
	// Run the journaling
	db.lock.Lock()
	defer db.lock.Unlock()

	// Retrieve the head layer to journal from.
	l := db.tree.get(root)
	if l == nil {
		return fmt.Errorf("triedb layer [%#x] missing", root)
	}
	disk := db.tree.bottom()
	if l, ok := l.(*diffLayer); ok {
		log.Info("Persisting dirty state to disk", "head", l.block, "root", root, "layers", l.id-disk.id+disk.buffer.layers)
	} else { // disk layer only on noop runs (likely) or deep reorgs (unlikely)
		log.Info("Persisting dirty state to disk", "root", root, "layers", disk.buffer.layers)
	}
	// Block until the background flushing is finished and terminate
	// the potential active state generator.
	if err := disk.terminate(); err != nil {
		return err
	}
	start := time.Now()

	// Short circuit if the database is in read only mode.
	if db.readOnly {
		return errDatabaseReadOnly
	}
	// Firstly write out the metadata of journal
	db.DeleteTrieJournal(db.diskdb)
	journal := newJournalWriter(db.config.JournalFilePath, db.diskdb, db.DetermineJournalTypeForWriter())
	defer journal.Close()

	if err := rlp.Encode(journal, journalVersion); err != nil {
		return err
	}
	// Secondly write out the state root in disk, ensure all layers
	// on top are continuous with disk.
	diskRoot, err := db.hasher(rawdb.ReadAccountTrieNode(db.diskdb, nil))
	if err != nil {
		return err
	}
	if err := rlp.Encode(journal, diskRoot); err != nil {
		return err
	}
	// Finally write out the journal of each layer in reverse order.
	if err := l.journal(journal, db.DetermineJournalTypeForWriter()); err != nil {
		return err
	}
	// Store the journal into the database and return
	journalSize := journal.Size()

	// Set the db in read only mode to reject all following mutations
	db.readOnly = true
	log.Info("Persisted dirty state to disk", "size", common.StorageSize(journalSize), "elapsed", common.PrettyDuration(time.Since(start)))
	return nil
}
