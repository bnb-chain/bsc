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
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestCopyGuardRejectsAliasedFiles(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()
	os.Mkdir(filepath.Join(src, "geth"), 0700)
	os.Mkdir(filepath.Join(dst, "geth"), 0700)
	orig := filepath.Join(src, "geth", "data")
	copy := filepath.Join(dst, "geth", "data")
	os.WriteFile(orig, []byte("state"), 0600)
	src, _ = filepath.EvalSymlinks(src)
	dst, _ = filepath.EvalSymlinks(dst)
	b, _ := json.Marshal(copyManifest{Source: src, Destination: dst, Complete: true})
	os.WriteFile(filepath.Join(dst, "offline-recovery-copy.json"), b, 0600)
	if err := os.Link(orig, copy); err != nil {
		t.Fatal(err)
	}
	if err := validateCopy(dst); err == nil {
		t.Fatal("accepted hardlinked original")
	}
	os.Remove(copy)
	os.Symlink(orig, copy)
	if err := validateCopy(dst); err == nil {
		t.Fatal("accepted symlink")
	}
	os.Remove(copy)
	os.WriteFile(copy, []byte("state"), 0600)
	if err := validateCopy(dst); err != nil {
		t.Fatal(err)
	}
}

func TestExactHashesRequired(t *testing.T) {
	for _, s := range []string{"", "0x1234", "garbage"} {
		if _, err := parseHash(s); err == nil {
			t.Fatal("accepted", s)
		}
	}
}

func TestJournalMovesPreflight(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "geth", "triedb")
	if e := os.MkdirAll(sub, 0700); e != nil {
		t.Fatal(e)
	}
	name := filepath.Join(sub, "merkle.journal")
	if e := os.WriteFile(name, []byte("journal"), 0600); e != nil {
		t.Fatal(e)
	}
	paths, e := journalMoves(dir)
	if e != nil || len(paths) != 1 || paths[0] != name {
		t.Fatalf("%v %v", paths, e)
	}
	if _, e = os.Stat(name); e != nil {
		t.Fatal("preflight moved journal", e)
	}
	if e = os.WriteFile(name+".before-offline-recovery", []byte("existing"), 0600); e != nil {
		t.Fatal(e)
	}
	if _, e = journalMoves(dir); e == nil {
		t.Fatal("accepted occupied quarantine")
	}
}

func TestOfflineCommandRejectsIncompleteArguments(t *testing.T) {
	for _, args := range [][]string{{}, {"--datadir", t.TempDir()}, {"--unknown"}, {"unexpected"}} {
		if e := runWithArgs(args); e == nil {
			t.Fatalf("accepted %v", args)
		}
	}
}
