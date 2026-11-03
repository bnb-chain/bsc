// Copyright 2026 The go-ethereum Authors
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

package main

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/ethdb/leveldb"
	"github.com/ethereum/go-ethereum/ethdb/pebble"
	"github.com/ethereum/go-ethereum/triedb/pathdb"
	"github.com/gofrs/flock"
)

type copyManifest struct {
	Source      string `json:"source"`
	Destination string `json:"destination"`
	Complete    bool   `json:"complete"`
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "RECOVERY FAILED:", err)
		os.Exit(1)
	}
}

func parseHash(s string) (common.Hash, error) {
	b, e := hex.DecodeString(strings.TrimPrefix(s, "0x"))
	if e != nil || len(b) != 32 {
		return common.Hash{}, errors.New("expected exact 32-byte hash")
	}
	return common.BytesToHash(b), nil
}

func validateCopy(dir string) error {
	b, e := os.ReadFile(filepath.Join(dir, "offline-recovery-copy.json"))
	if e != nil {
		return e
	}
	var m copyManifest
	if e = json.Unmarshal(b, &m); e != nil {
		return e
	}
	src, e := filepath.EvalSymlinks(m.Source)
	if e != nil {
		return e
	}
	dst, e := filepath.EvalSymlinks(dir)
	if e != nil {
		return e
	}
	if !m.Complete || m.Destination != dst || src == dst || strings.HasPrefix(dst, src+string(os.PathSeparator)) || strings.HasPrefix(src, dst+string(os.PathSeparator)) {
		return errors.New("invalid independent-copy manifest")
	}
	return filepath.WalkDir(filepath.Join(dst, "geth"), func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlink forbidden: %s", p)
		}
		if d.IsDir() {
			return nil
		}
		info, e := d.Info()
		if e != nil {
			return e
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("non-regular file in copy: %s", p)
		}
		rel, e := filepath.Rel(dst, p)
		if e != nil {
			return e
		}
		orig, e := os.Stat(filepath.Join(src, rel))
		if e == nil && os.SameFile(info, orig) {
			return fmt.Errorf("hardlink to source forbidden: %s", p)
		}
		if e != nil && !os.IsNotExist(e) {
			return e
		}
		return nil
	})
}

func run() error { return runWithArgs(os.Args[1:]) }

func runWithArgs(args []string) error {
	flags := flag.NewFlagSet("offline-recover", flag.ContinueOnError)
	dir := flags.String("datadir", "", "Geth datadir (inspection default)")
	height := flags.Uint64("target", 0, "target block height")
	hashArg := flags.String("target-hash", "", "required exact block hash")
	rootArg := flags.String("target-root", "", "required exact state root")
	applyChanges := flags.Bool("apply", false, "apply recovery (copy required unless explicit in-place acknowledgement)")
	inPlace := flags.Bool("in-place", false, "allow original datadir; requires --acknowledge-in-place equal to target hash")
	ack := flags.String("acknowledge-in-place", "", "exact target block hash confirming original-data mutation")
	repairRoot := flags.Bool("reconstruct-missing-root", false, "verify reverse replay in memory and reconstruct missing account root only if its hash matches saved state")
	maxSteps := flags.Uint64("max-state-steps", 10000, "maximum replay transitions")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("unexpected positional arguments")
	}
	if *dir == "" || *height == 0 {
		return errors.New("datadir and nonzero target required")
	}
	targetHash, e := parseHash(*hashArg)
	if e != nil {
		return e
	}
	targetRoot, e := parseHash(*rootArg)
	if e != nil {
		return e
	}
	abs, e := filepath.Abs(*dir)
	if e != nil {
		return e
	}
	abs, e = filepath.EvalSymlinks(abs)
	if e != nil {
		return e
	}
	if *inPlace && (!*applyChanges || *ack != targetHash.Hex()) {
		return errors.New("in-place requires --apply and exact --acknowledge-in-place target hash")
	}
	if *applyChanges && !*inPlace {
		if e = validateCopy(abs); e != nil {
			return e
		}
	}
	// Same instance lock used by geth. Opening an active datadir is forbidden.
	lock := flock.New(filepath.Join(abs, "geth", "LOCK"))
	ok, e := lock.TryLock()
	if e != nil {
		return e
	}
	if !ok {
		return errors.New("geth instance is active/locked")
	}
	defer lock.Unlock()
	dbpath := filepath.Join(abs, "geth", "chaindata")
	if _, e = os.Stat(filepath.Join(dbpath, "CURRENT")); e != nil {
		return fmt.Errorf("existing database required: %w", e)
	}
	var kv ethdb.KeyValueStore
	matches, _ := filepath.Glob(filepath.Join(dbpath, "OPTIONS-*"))
	if len(matches) > 0 {
		kv, e = pebble.New(dbpath, 128, 64, "offline-recovery/", !*applyChanges)
	} else {
		kv, e = leveldb.New(dbpath, 128, 64, "offline-recovery/", !*applyChanges)
	}
	if e != nil {
		return e
	}
	if rawdb.ReadLegacyOffset(kv) > 0 {
		kv.Close()
		return errors.New("legacy freezer migration unsupported")
	}
	// Chain freezer stays read-only even in apply mode. No freezer goroutine.
	db, e := rawdb.NewDatabaseWithFreezer(kv, filepath.Join(dbpath, "ancient"), "offline-recovery/", true)
	if e != nil {
		kv.Close()
		return e
	}
	defer db.Close()
	block := rawdb.ReadBlock(db, targetHash, *height)
	if block == nil || block.Root() != targetRoot || rawdb.ReadCanonicalHash(db, *height) != targetHash {
		return errors.New("target block/hash/root/canonical mismatch")
	}
	frozen, e := db.Ancients()
	if e != nil {
		return e
	}
	if *height+1 < frozen {
		return errors.New("target crosses chain freezer")
	}
	head, ok := rawdb.ReadHeaderNumber(db, rawdb.ReadHeadHeaderHash(db))
	if !ok || head < *height || head-*height > 100000 {
		return errors.New("header rewind is out of supported range")
	}
	if !rawdb.HasAccountTrieNode(db, nil) && !*repairRoot {
		return errors.New("account root is missing; reconstruction requires --reconstruct-missing-root")
	}
	report, e := pathdb.OfflineReplayCheck(db, targetRoot, *maxSteps)
	json.NewEncoder(os.Stdout).Encode(map[string]interface{}{"target": *height, "header_head": head, "check": report})
	if e != nil {
		return e
	}
	if !*applyChanges {
		return nil
	}
	// Check journal destinations before the first state mutation.
	journals, e := journalMoves(abs)
	if e != nil {
		return e
	}
	if _, e = os.Lstat(filepath.Join(abs, "OFFLINE-RECOVERY-COMPLETE")); !os.IsNotExist(e) {
		return errors.New("previous completion marker exists or is inaccessible")
	}
	// Any failed apply leaves this sentinel behind; never retry automatically.
	marker := filepath.Join(abs, "OFFLINE-RECOVERY-IN-PROGRESS")
	f, e := os.OpenFile(marker, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return e
	}
	if _, e = fmt.Fprintf(f, "target=%d hash=%s root=%s\n", *height, targetHash, targetRoot); e != nil {
		f.Close()
		return e
	}
	if e = f.Sync(); e != nil {
		f.Close()
		return e
	}
	f.Close()
	if *repairRoot && !rawdb.HasAccountTrieNode(db, nil) {
		blob, err := pathdb.OfflineReconstructedRoot(db)
		if err != nil {
			return err
		}
		rawdb.WriteAccountTrieNode(db, nil, blob)
		if err = db.SyncKeyValue(); err != nil {
			return err
		}
	}
	report, e = pathdb.OfflineRecover(db, targetRoot, *maxSteps, true)
	if e != nil {
		return e
	}
	// File journals can resurrect layers from the abandoned branch. Remove only
	// these known journal filenames, within the validated datadir.
	for _, p := range journals {
		if e = os.Rename(p, p+".before-offline-recovery"); e != nil {
			return e
		}
	}
	if e = rawdb.OfflineResetMarkers(db, *height, targetHash); e != nil {
		return e
	}
	if rawdb.ReadHeadBlockHash(db) != targetHash || rawdb.ReadHeadHeaderHash(db) != targetHash || rawdb.ReadHeadFastBlockHash(db) != targetHash || rawdb.ReadSnapSyncStatusFlag(db) != rawdb.StateSyncFinished {
		return errors.New("post-recovery markers mismatch")
	}
	if e = os.Rename(marker, filepath.Join(abs, "OFFLINE-RECOVERY-COMPLETE")); e != nil {
		return e
	}
	return json.NewEncoder(os.Stdout).Encode(map[string]interface{}{"result": "state replay and markers verified; isolated restart still required", "report": report})
}

// journalMoves validates all destinations without renaming any journal.
func journalMoves(dir string) ([]string, error) {
	var paths []string
	err := filepath.WalkDir(filepath.Join(dir, "geth"), func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && d.Name() == "merkle.journal" {
			if !d.Type().IsRegular() {
				return fmt.Errorf("journal must be a regular file: %s", p)
			}
			if _, e := os.Lstat(p + ".before-offline-recovery"); !os.IsNotExist(e) {
				return errors.New("journal quarantine exists or is inaccessible")
			}
			paths = append(paths, p)
		}
		return nil
	})
	return paths, err
}
