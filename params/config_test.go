// Copyright 2017 The go-ethereum Authors
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

package params

import (
	"math/big"
	"reflect"
	"testing"

	"github.com/ethereum/go-ethereum/common"
)

func TestCheckCompatible(t *testing.T) {
	type test struct {
		stored, new *ChainConfig
		head        uint64
		wantErr     *ConfigCompatError
	}
	tests := []test{
		{stored: AllEthashProtocolChanges, new: AllEthashProtocolChanges, head: 0, wantErr: nil},
		{stored: AllEthashProtocolChanges, new: AllEthashProtocolChanges, head: 100, wantErr: nil},
		{
			stored:  &ChainConfig{EIP150Block: big.NewInt(10)},
			new:     &ChainConfig{EIP150Block: big.NewInt(20)},
			head:    9,
			wantErr: nil,
		},
		{
			stored: AllEthashProtocolChanges,
			new:    &ChainConfig{HomesteadBlock: nil},
			head:   3,
			wantErr: &ConfigCompatError{
				What:         "Homestead fork block",
				StoredConfig: big.NewInt(0),
				NewConfig:    nil,
				RewindTo:     0,
			},
		},
		{
			stored: AllEthashProtocolChanges,
			new:    &ChainConfig{HomesteadBlock: big.NewInt(1)},
			head:   3,
			wantErr: &ConfigCompatError{
				What:         "Homestead fork block",
				StoredConfig: big.NewInt(0),
				NewConfig:    big.NewInt(1),
				RewindTo:     0,
			},
		},
		{
			stored: &ChainConfig{HomesteadBlock: big.NewInt(30), EIP150Block: big.NewInt(10)},
			new:    &ChainConfig{HomesteadBlock: big.NewInt(25), EIP150Block: big.NewInt(20)},
			head:   25,
			wantErr: &ConfigCompatError{
				What:         "EIP150 fork block",
				StoredConfig: big.NewInt(10),
				NewConfig:    big.NewInt(20),
				RewindTo:     9,
			},
		},
		{
			stored:  &ChainConfig{ConstantinopleBlock: big.NewInt(30)},
			new:     &ChainConfig{ConstantinopleBlock: big.NewInt(30), PetersburgBlock: big.NewInt(30)},
			head:    40,
			wantErr: nil,
		},
		{
			stored: &ChainConfig{ConstantinopleBlock: big.NewInt(30)},
			new:    &ChainConfig{ConstantinopleBlock: big.NewInt(30), PetersburgBlock: big.NewInt(31)},
			head:   40,
			wantErr: &ConfigCompatError{
				What:         "Petersburg fork block",
				StoredConfig: nil,
				NewConfig:    big.NewInt(31),
				RewindTo:     30,
			},
		},
	}

	for _, test := range tests {
		err := test.stored.CheckCompatible(test.new, test.head)
		if !reflect.DeepEqual(err, test.wantErr) {
			t.Errorf("error mismatch:\nstored: %v\nnew: %v\nhead: %v\nerr: %v\nwant: %v", test.stored, test.new, test.head, err, test.wantErr)
		}
	}
}

func TestChainConfig_VerifyForkConfig(t *testing.T) {
	type fields struct {
		ChainID             *big.Int
		HomesteadBlock      *big.Int
		DAOForkBlock        *big.Int
		DAOForkSupport      bool
		EIP150Block         *big.Int
		EIP150Hash          common.Hash
		EIP155Block         *big.Int
		EIP158Block         *big.Int
		ByzantiumBlock      *big.Int
		ConstantinopleBlock *big.Int
		PetersburgBlock     *big.Int
		IstanbulBlock       *big.Int
		MuirGlacierBlock    *big.Int
		BerlinBlock         *big.Int
		YoloV3Block         *big.Int
		EWASMBlock          *big.Int
		CatalystBlock       *big.Int
		RamanujanBlock      *big.Int
		NielsBlock          *big.Int
		MirrorSyncBlock     *big.Int
		BrunoBlock          *big.Int
		EulerBlock          *big.Int
		Ethash              *EthashConfig
		Clique              *CliqueConfig
		Parlia              *ParliaConfig
	}
	tests := []struct {
		name    string
		fields  fields
		wantErr bool
	}{
		{
			name:    "Test VerifyForkConfig Case 1",
			fields:  fields{},
			wantErr: false,
		},
		{
			name: "Test VerifyForkConfig Case 2",
			fields: fields{
				EulerBlock: big.NewInt(0),
			},
			wantErr: false,
		},
		{
			name: "Test VerifyForkConfig Case 3",
			fields: fields{
				EulerBlock: big.NewInt(200),
			},
			wantErr: true,
		},
		{
			name: "Test VerifyForkConfig Case 4",
			fields: fields{
				EulerBlock: big.NewInt(19231208),
			},
			wantErr: false,
		},
		{
			name: "Test VerifyForkConfig Case 5",
			fields: fields{
				EulerBlock: big.NewInt(1998200),
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &ChainConfig{
				ChainID:             tt.fields.ChainID,
				HomesteadBlock:      tt.fields.HomesteadBlock,
				DAOForkBlock:        tt.fields.DAOForkBlock,
				DAOForkSupport:      tt.fields.DAOForkSupport,
				EIP150Block:         tt.fields.EIP150Block,
				EIP150Hash:          tt.fields.EIP150Hash,
				EIP155Block:         tt.fields.EIP155Block,
				EIP158Block:         tt.fields.EIP158Block,
				ByzantiumBlock:      tt.fields.ByzantiumBlock,
				ConstantinopleBlock: tt.fields.ConstantinopleBlock,
				PetersburgBlock:     tt.fields.PetersburgBlock,
				IstanbulBlock:       tt.fields.IstanbulBlock,
				MuirGlacierBlock:    tt.fields.MuirGlacierBlock,
				BerlinBlock:         tt.fields.BerlinBlock,
				YoloV3Block:         tt.fields.YoloV3Block,
				EWASMBlock:          tt.fields.EWASMBlock,
				CatalystBlock:       tt.fields.CatalystBlock,
				RamanujanBlock:      tt.fields.RamanujanBlock,
				NielsBlock:          tt.fields.NielsBlock,
				MirrorSyncBlock:     tt.fields.MirrorSyncBlock,
				BrunoBlock:          tt.fields.BrunoBlock,
				EulerBlock:          tt.fields.EulerBlock,
				Ethash:              tt.fields.Ethash,
				Clique:              tt.fields.Clique,
				Parlia:              tt.fields.Parlia,
			}
			if err := c.VerifyForkConfig(); (err != nil) != tt.wantErr {
				t.Errorf("ChainConfig.VerifyForkConfig() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
