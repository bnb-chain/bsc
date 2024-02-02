// Copyright 2019 The go-ethereum Authors
// This file is part of go-ethereum.
//
// go-ethereum is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// go-ethereum is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with go-ethereum. If not, see <http://www.gnu.org/licenses/>.

// Package utils contains internal helper functions for go-ethereum commands.
package utils

import (
	"os"
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ethereum/go-ethereum/core/rawdb"
)

func Test_SplitTagsFlag(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		args string
		want map[string]string
	}{
		{
			"2 tags case",
			"host=localhost,bzzkey=123",
			map[string]string{
				"host":   "localhost",
				"bzzkey": "123",
			},
		},
		{
			"1 tag case",
			"host=localhost123",
			map[string]string{
				"host": "localhost123",
			},
		},
		{
			"empty case",
			"",
			map[string]string{},
		},
		{
			"garbage",
			"smth=smthelse=123",
			map[string]string{},
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := SplitTagsFlag(tt.args); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("splitTagsFlag() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_parseConfig(t *testing.T) {
	tests := []struct {
		name         string
		fn           func() string
		wantedResult string
		wantedIsErr  bool
		wantedErrStr string
	}{
		{
			name: "path",
			fn: func() string {
				tomlString := `[Eth]NetworkId = 56StateScheme = "path"`
				return createTempTomlFile(t, tomlString)
			},
			wantedResult: rawdb.PathScheme,
			wantedIsErr:  false,
			wantedErrStr: "",
		},
		{
			name: "hash",
			fn: func() string {
				tomlString := `[Eth]NetworkId = 56StateScheme = "hash"`
				return createTempTomlFile(t, tomlString)
			},
			wantedResult: rawdb.HashScheme,
			wantedIsErr:  false,
			wantedErrStr: "",
		},
		{
			name: "empty state scheme",
			fn: func() string {
				tomlString := `[Eth]NetworkId = 56StateScheme = ""`
				return createTempTomlFile(t, tomlString)
			},
			wantedResult: "",
			wantedIsErr:  false,
			wantedErrStr: "",
		},
		{
			name: "unset state scheme",
			fn: func() string {
				tomlString := `[Eth]NetworkId = 56`
				return createTempTomlFile(t, tomlString)
			},
			wantedResult: "",
			wantedIsErr:  false,
			wantedErrStr: "",
		},
		{
			name:         "failed to open file",
			fn:           func() string { return "" },
			wantedResult: "",
			wantedIsErr:  true,
			wantedErrStr: "open : no such file or directory",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := scanConfigForStateScheme(tt.fn())
			if tt.wantedIsErr {
				assert.Contains(t, err.Error(), tt.wantedErrStr)
			} else {
				assert.Nil(t, err)
			}
			assert.Equal(t, tt.wantedResult, result)
		})
	}
}

// createTempTomlFile is a helper function to create a temp file with the provided TOML content
func createTempTomlFile(t *testing.T, content string) string {
	t.Helper()

	dir := t.TempDir()
	file, err := os.CreateTemp(dir, "config.toml")
	if err != nil {
		t.Fatalf("Unable to create temporary file: %v", err)
	}
	defer file.Close()

	_, err = file.WriteString(content)
	if err != nil {
		t.Fatalf("Unable to write to temporary file: %v", err)
	}
	return file.Name()
}

func Test_parseString(t *testing.T) {
	tests := []struct {
		name       string
		arg        string
		wantResult string
	}{
		{
			name:       "hash string",
			arg:        "\"hash\"",
			wantResult: rawdb.HashScheme,
		},
		{
			name:       "path string",
			arg:        "\"path\"",
			wantResult: rawdb.PathScheme,
		},
		{
			name:       "empty string",
			arg:        "",
			wantResult: "",
		},
		{
			name:       "empty string",
			arg:        "\"\"",
			wantResult: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := indexStateScheme(tt.arg); got != tt.wantResult {
				t.Errorf("parseString() = %v, want %v", got, tt.wantResult)
			}
		})
	}
}
