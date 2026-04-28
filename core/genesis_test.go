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

package core

import (
	"bytes"
	"encoding/json"
	"math/big"
	"reflect"
	"strings"
	"testing"

	"github.com/davecgh/go-spew/spew"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus/ethash"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/triedb"
	"github.com/ethereum/go-ethereum/triedb/pathdb"
)

func TestSetupGenesis(t *testing.T) {
	testSetupGenesis(t, rawdb.HashScheme)
	testSetupGenesis(t, rawdb.PathScheme)
}

func testSetupGenesis(t *testing.T, scheme string) {
	var (
		customghash = common.HexToHash("0x89c99d90b79719238d2645c7642f2c9295246e80775b38cfd162b696817fbd50")
		customg     = Genesis{
			Config: &params.ChainConfig{HomesteadBlock: big.NewInt(3), Ethash: &params.EthashConfig{}},
			Alloc: types.GenesisAlloc{
				{1}: {Balance: big.NewInt(1), Storage: map[common.Hash]common.Hash{{1}: {1}}},
			},
		}
		oldcustomg = customg
	)
	oldcustomg.Config = &params.ChainConfig{HomesteadBlock: big.NewInt(2), Ethash: &params.EthashConfig{}}

	tests := []struct {
		name           string
		fn             func(ethdb.Database) (*params.ChainConfig, common.Hash, *params.ConfigCompatError, error)
		wantConfig     *params.ChainConfig
		wantHash       common.Hash
		wantErr        error
		wantCompactErr *params.ConfigCompatError
	}{
		{
			name: "genesis without ChainConfig",
			fn: func(db ethdb.Database) (*params.ChainConfig, common.Hash, *params.ConfigCompatError, error) {
				return SetupGenesisBlock(db, triedb.NewDatabase(db, newDbConfig(scheme)), new(Genesis))
			},
			wantErr: errGenesisNoConfig,
		},
		{
			name: "no block in DB, genesis == nil",
			fn: func(db ethdb.Database) (*params.ChainConfig, common.Hash, *params.ConfigCompatError, error) {
				return SetupGenesisBlock(db, triedb.NewDatabase(db, newDbConfig(scheme)), nil)
			},
			wantHash:   params.BSCGenesisHash,
			wantConfig: params.BSCChainConfig,
		},
		{
			name: "mainnet block in DB, genesis == nil",
			fn: func(db ethdb.Database) (*params.ChainConfig, common.Hash, *params.ConfigCompatError, error) {
				DefaultBSCGenesisBlock().MustCommit(db, triedb.NewDatabase(db, newDbConfig(scheme)))
				return SetupGenesisBlock(db, triedb.NewDatabase(db, newDbConfig(scheme)), nil)
			},
			wantHash:   params.BSCGenesisHash,
			wantConfig: params.BSCChainConfig,
		},
		{
			name: "chapel block in DB, genesis == nil",
			fn: func(db ethdb.Database) (*params.ChainConfig, common.Hash, *params.ConfigCompatError, error) {
				DefaultChapelGenesisBlock().MustCommit(db, triedb.NewDatabase(db, newDbConfig(scheme)))
				return SetupGenesisBlock(db, triedb.NewDatabase(db, newDbConfig(scheme)), nil)
			},
			wantHash:   params.ChapelGenesisHash,
			wantConfig: params.ChapelChainConfig,
		},
		{
			name: "chapel block in DB, genesis == chapel",
			fn: func(db ethdb.Database) (*params.ChainConfig, common.Hash, *params.ConfigCompatError, error) {
				DefaultChapelGenesisBlock().MustCommit(db, triedb.NewDatabase(db, newDbConfig(scheme)))
				return SetupGenesisBlock(db, triedb.NewDatabase(db, newDbConfig(scheme)), DefaultChapelGenesisBlock())
			},
			wantHash:   params.ChapelGenesisHash,
			wantConfig: params.ChapelChainConfig,
		},
		{
			name: "custom block in DB, genesis == nil",
			fn: func(db ethdb.Database) (*params.ChainConfig, common.Hash, *params.ConfigCompatError, error) {
				tdb := triedb.NewDatabase(db, newDbConfig(scheme))
				customg.Commit(db, tdb)
				return SetupGenesisBlock(db, tdb, nil)
			},
			wantHash:   customghash,
			wantConfig: customg.Config,
		},
		{
			name: "custom block in DB, genesis == chapel",
			fn: func(db ethdb.Database) (*params.ChainConfig, common.Hash, *params.ConfigCompatError, error) {
				tdb := triedb.NewDatabase(db, newDbConfig(scheme))
				customg.Commit(db, tdb)
				return SetupGenesisBlock(db, tdb, DefaultChapelGenesisBlock())
			},
			wantErr: &GenesisMismatchError{Stored: customghash, New: params.ChapelGenesisHash},
		},
		{
			name: "compatible config in DB",
			fn: func(db ethdb.Database) (*params.ChainConfig, common.Hash, *params.ConfigCompatError, error) {
				tdb := triedb.NewDatabase(db, newDbConfig(scheme))
				oldcustomg.Commit(db, tdb)
				return SetupGenesisBlock(db, tdb, &customg)
			},
			wantHash:   customghash,
			wantConfig: customg.Config,
		},
		{
			name: "incompatible config in DB",
			fn: func(db ethdb.Database) (*params.ChainConfig, common.Hash, *params.ConfigCompatError, error) {
				// Commit the 'old' genesis block with Homestead transition at #2.
				// Advance to block #4, past the homestead transition block of customg.
				tdb := triedb.NewDatabase(db, newDbConfig(scheme))
				oldcustomg.Commit(db, tdb)

				bc, _ := NewBlockChain(db, &oldcustomg, ethash.NewFullFaker(), DefaultConfig().WithStateScheme(scheme))
				defer bc.Stop()

				_, blocks, _ := GenerateChainWithGenesis(&oldcustomg, ethash.NewFaker(), 4, nil)
				bc.InsertChain(blocks)

				// This should return a compatibility error.
				return SetupGenesisBlock(db, tdb, &customg)
			},
			wantHash:   customghash,
			wantConfig: customg.Config,
			wantCompactErr: &params.ConfigCompatError{
				What:          "Homestead fork block",
				StoredBlock:   big.NewInt(2),
				NewBlock:      big.NewInt(3),
				RewindToBlock: 1,
			},
		},
	}

	for _, test := range tests {
		db := rawdb.NewMemoryDatabase()
		config, hash, compatErr, err := test.fn(db)
		// Check the return values.
		if !reflect.DeepEqual(err, test.wantErr) {
			spew := spew.ConfigState{DisablePointerAddresses: true, DisableCapacities: true}
			t.Errorf("%s: returned error %#v, want %#v", test.name, spew.NewFormatter(err), spew.NewFormatter(test.wantErr))
		}
		if !reflect.DeepEqual(compatErr, test.wantCompactErr) {
			spew := spew.ConfigState{DisablePointerAddresses: true, DisableCapacities: true}
			t.Errorf("%s: returned error %#v, want %#v", test.name, spew.NewFormatter(compatErr), spew.NewFormatter(test.wantCompactErr))
		}
		if !reflect.DeepEqual(config, test.wantConfig) {
			t.Errorf("%s:\nreturned %v\nwant     %v", test.name, config, test.wantConfig)
		}
		if hash != test.wantHash {
			t.Errorf("%s: returned hash %s, want %s", test.name, hash.Hex(), test.wantHash.Hex())
		} else if err == nil {
			// Check database content.
			stored := rawdb.ReadBlock(db, test.wantHash, 0)
			if stored.Hash() != test.wantHash {
				t.Errorf("%s: block in DB has hash %s, want %s", test.name, stored.Hash(), test.wantHash)
			}
		}
	}
}

// TestGenesisHashes checks the congruity of default genesis data to
// corresponding hardcoded genesis hash values.
func TestGenesisHashes(t *testing.T) {
	for i, c := range []struct {
		genesis *Genesis
		want    common.Hash
	}{
		{DefaultGenesisBlock(), params.MainnetGenesisHash},
	} {
		// Test via MustCommit
		db := rawdb.NewMemoryDatabase()
		if have := c.genesis.MustCommit(db, triedb.NewDatabase(db, triedb.HashDefaults)).Hash(); have != c.want {
			t.Errorf("case: %d a), want: %s, got: %s", i, c.want.Hex(), have.Hex())
		}
		// Test via ToBlock
		if have := c.genesis.ToBlock().Hash(); have != c.want {
			t.Errorf("case: %d a), want: %s, got: %s", i, c.want.Hex(), have.Hex())
		}
	}
}

func TestGenesis_Commit(t *testing.T) {
	genesis := &Genesis{
		BaseFee: big.NewInt(params.InitialBaseFee),
		Config:  params.TestChainConfig,
		// difficulty is nil
	}

	db := rawdb.NewMemoryDatabase()
	genesisBlock := genesis.MustCommit(db, triedb.NewDatabase(db, triedb.HashDefaults))

	if genesis.Difficulty != nil {
		t.Fatalf("assumption wrong")
	}

	// This value should have been set as default in the ToBlock method.
	if genesisBlock.Difficulty().Cmp(params.GenesisDifficulty) != 0 {
		t.Errorf("assumption wrong: want: %d, got: %v", params.GenesisDifficulty, genesisBlock.Difficulty())
	}

	// Expect the stored total difficulty to be the difficulty of the genesis block.
	stored := rawdb.ReadTd(db, genesisBlock.Hash(), genesisBlock.NumberU64())

	if stored.Cmp(genesisBlock.Difficulty()) != 0 {
		t.Errorf("inequal difficulty; stored: %v, genesisBlock: %v", stored, genesisBlock.Difficulty())
	}
}

func TestReadWriteGenesisAlloc(t *testing.T) {
	var (
		db    = rawdb.NewMemoryDatabase()
		alloc = &types.GenesisAlloc{
			{1}: {Balance: big.NewInt(1), Storage: map[common.Hash]common.Hash{{1}: {1}}},
			{2}: {Balance: big.NewInt(2), Storage: map[common.Hash]common.Hash{{2}: {2}}},
		}
		hash, _ = hashAlloc(alloc, false)
	)
	blob, _ := json.Marshal(alloc)
	rawdb.WriteGenesisStateSpec(db, hash, blob)

	var reload types.GenesisAlloc
	err := reload.UnmarshalJSON(rawdb.ReadGenesisStateSpec(db, hash))
	if err != nil {
		t.Fatalf("Failed to load genesis state %v", err)
	}
	if len(reload) != len(*alloc) {
		t.Fatal("Unexpected genesis allocation")
	}
	for addr, account := range reload {
		want, ok := (*alloc)[addr]
		if !ok {
			t.Fatal("Account is not found")
		}
		if !reflect.DeepEqual(want, account) {
			t.Fatal("Unexpected account")
		}
	}
}

func TestConfigOrDefault(t *testing.T) {
	defaultGenesis := DefaultGenesisBlock()
	if defaultGenesis.Config.PlanckBlock != nil {
		t.Errorf("initial config should have PlanckBlock = nil, but instead PlanckBlock = %v", defaultGenesis.Config.PlanckBlock)
	}
	gHash := params.BSCGenesisHash
	config := defaultGenesis.chainConfigOrDefault(gHash, nil)

	if config.ChainID.Cmp(params.BSCChainConfig.ChainID) != 0 {
		t.Errorf("ChainID of resulting config should be %v, but is %v instead", params.BSCChainConfig.ChainID, config.ChainID)
	}

	if config.HomesteadBlock.Cmp(params.BSCChainConfig.HomesteadBlock) != 0 {
		t.Errorf("resulting config should have HomesteadBlock = %v, but instead is %v", params.BSCChainConfig, config.HomesteadBlock)
	}

	if config.PlanckBlock == nil {
		t.Errorf("resulting config should have PlanckBlock = %v , but instead is nil", params.BSCChainConfig.PlanckBlock)
	}

	if config.PlanckBlock.Cmp(params.BSCChainConfig.PlanckBlock) != 0 {
		t.Errorf("resulting config should have PlanckBlock = %v , but instead is %v", params.BSCChainConfig.PlanckBlock, config.PlanckBlock)
	}
}

const pqForkTimeGenesisJSON = `{
	"config": {
		"chainId": 714,
		"homesteadBlock": 0,
		"eip150Block": 0,
		"eip155Block": 0,
		"eip158Block": 0,
		"byzantiumBlock": 0,
		"constantinopleBlock": 0,
		"petersburgBlock": 0,
		"istanbulBlock": 0,
		"muirGlacierBlock": 0,
		"mirrorSyncBlock": 1,
		"brunoBlock": 1,
		"eulerBlock": 2,
		"nanoBlock": 3,
		"moranBlock": 3,
		"gibbsBlock": 4,
		"planckBlock": 5,
		"lubanBlock": 6,
		"platoBlock": 7,
		"berlinBlock": 8,
		"londonBlock": 8,
		"hertzBlock": 8,
		"hertzfixBlock": 8,
		"pqForkTime": 0,
		"shanghaiTime": 0,
		"keplerTime": 0,
		"feynmanTime": 0,
		"feynmanFixTime": 0,
		"cancunTime": 0,
		"haberTime": 0,
		"haberFixTime": 0,
		"bohrTime": 0,
		"pascalTime": 0,
		"pragueTime": 0,
		"lorentzTime": 0,
		"maxwellTime": 0,
		"fermiTime": 0,
		"osakaTime": 0,
		"mendelTime": 0,
		"pasteurTime": 0,
		"blobSchedule": {
			"cancun": {
				"target": 3,
				"max": 6,
				"baseFeeUpdateFraction": 3338477
			},
			"prague": {
				"target": 3,
				"max": 6,
				"baseFeeUpdateFraction": 3338477
			},
			"osaka": {
				"target": 3,
				"max": 6,
				"baseFeeUpdateFraction": 3338477
			}
		},
		"parlia": {
			"period": 3,
			"epoch": 200
		}
	},
	"nonce": "0x0",
	"timestamp": "0x5e9da7ce",
	"extraData": "0x00",
	"gasLimit": "0x2625a00",
	"difficulty": "0x1",
	"mixHash": "0x0000000000000000000000000000000000000000000000000000000000000000",
	"coinbase": "0x0000000000000000000000000000000000000000",
	"alloc": {}
}`

func decodePQForkTimeGenesis(t *testing.T) *Genesis {
	t.Helper()

	var genesis Genesis
	if err := json.Unmarshal([]byte(pqForkTimeGenesisJSON), &genesis); err != nil {
		t.Fatalf("unmarshal genesis: %v", err)
	}
	if genesis.Config == nil {
		t.Fatal("decoded genesis config is nil")
	}
	if genesis.Config.PQForkTime == nil || *genesis.Config.PQForkTime != 0 {
		t.Fatalf("decoded PQForkTime lost: have %v want 0", genesis.Config.PQForkTime)
	}
	return &genesis
}

func TestLoadChainConfigWithRialtoHashPreservesPQForkTime(t *testing.T) {
	genesis := decodePQForkTimeGenesis(t)

	db := rawdb.NewMemoryDatabase()
	tdb := triedb.NewDatabase(db, triedb.HashDefaults)
	block, err := genesis.Commit(db, tdb)
	if err != nil {
		t.Fatalf("commit genesis: %v", err)
	}
	oldRialtoHash := params.RialtoGenesisHash
	params.RialtoGenesisHash = block.Hash()
	defer func() {
		params.RialtoGenesisHash = oldRialtoHash
	}()

	got, hash, err := LoadChainConfig(db, nil)
	if err != nil {
		t.Fatalf("LoadChainConfig: %v", err)
	}
	if hash != block.Hash() {
		t.Fatalf("unexpected genesis hash: have %s want %s", hash, block.Hash())
	}
	if got.PQForkTime == nil || *got.PQForkTime != 0 {
		t.Fatalf("loaded PQForkTime lost: have %v want 0", got.PQForkTime)
	}
}

func TestCustomGenesisPQForkTimeRoundTrip(t *testing.T) {
	genesis := decodePQForkTimeGenesis(t)

	db := rawdb.NewMemoryDatabase()
	tdb := triedb.NewDatabase(db, triedb.HashDefaults)
	block, err := genesis.Commit(db, tdb)
	if err != nil {
		t.Fatalf("commit genesis: %v", err)
	}
	stored := rawdb.ReadChainConfig(db, block.Hash())
	if stored == nil {
		t.Fatal("stored config is nil after commit")
	}
	if stored.PQForkTime == nil || *stored.PQForkTime != 0 {
		t.Fatalf("stored PQForkTime lost after commit: have %v want 0", stored.PQForkTime)
	}
	oldRialtoHash := params.RialtoGenesisHash
	params.RialtoGenesisHash = block.Hash()
	defer func() {
		params.RialtoGenesisHash = oldRialtoHash
	}()

	passedForkTime := uint64(1)
	lastHardforkTime := uint64(2)
	updatedCfg, _, _, err := SetupGenesisBlockWithOverride(db, tdb, nil, &ChainOverrides{
		OverridePassedForkTime: &passedForkTime,
		OverrideLorentz:        &passedForkTime,
		OverrideMaxwell:        &passedForkTime,
		OverrideFermi:          &lastHardforkTime,
		OverrideOsaka:          &lastHardforkTime,
		OverrideMendel:         &lastHardforkTime,
		OverridePasteur:        &lastHardforkTime,
		OverridePQHardfork:     &lastHardforkTime,
	})
	if err != nil {
		t.Fatalf("setup genesis with override: %v", err)
	}
	if updatedCfg == nil {
		t.Fatal("updated config is nil")
	}
	if updatedCfg.PQForkTime == nil || *updatedCfg.PQForkTime != lastHardforkTime {
		t.Fatalf("updated PQForkTime lost after override path: have %v want %d", updatedCfg.PQForkTime, lastHardforkTime)
	}
	if !strings.Contains(updatedCfg.String(), "PQForkTime: 2") {
		t.Fatalf("chain config string missing PQForkTime: %s", updatedCfg.String())
	}
	stored = rawdb.ReadChainConfig(db, block.Hash())
	if stored == nil {
		t.Fatal("stored config is nil after override path")
	}
	if stored.PQForkTime == nil || *stored.PQForkTime != lastHardforkTime {
		t.Fatalf("stored PQForkTime lost after override path: have %v want %d", stored.PQForkTime, lastHardforkTime)
	}
}

func newDbConfig(scheme string) *triedb.Config {
	if scheme == rawdb.HashScheme {
		return triedb.HashDefaults
	}
	config := *pathdb.Defaults
	config.NoAsyncFlush = true
	return &triedb.Config{PathDB: &config}
}

func TestVerkleGenesisCommit(t *testing.T) {
	var verkleTime uint64 = 0
	verkleConfig := &params.ChainConfig{
		ChainID:                 big.NewInt(1),
		HomesteadBlock:          big.NewInt(0),
		DAOForkBlock:            nil,
		DAOForkSupport:          false,
		EIP150Block:             big.NewInt(0),
		EIP155Block:             big.NewInt(0),
		EIP158Block:             big.NewInt(0),
		ByzantiumBlock:          big.NewInt(0),
		ConstantinopleBlock:     big.NewInt(0),
		PetersburgBlock:         big.NewInt(0),
		IstanbulBlock:           big.NewInt(0),
		MuirGlacierBlock:        big.NewInt(0),
		BerlinBlock:             big.NewInt(0),
		LondonBlock:             big.NewInt(0),
		ArrowGlacierBlock:       big.NewInt(0),
		GrayGlacierBlock:        big.NewInt(0),
		MergeNetsplitBlock:      nil,
		ShanghaiTime:            &verkleTime,
		CancunTime:              &verkleTime,
		PragueTime:              &verkleTime,
		OsakaTime:               &verkleTime,
		VerkleTime:              &verkleTime,
		TerminalTotalDifficulty: big.NewInt(0),
		EnableVerkleAtGenesis:   true,
		Ethash:                  nil,
		Clique:                  nil,
		BlobScheduleConfig: &params.BlobScheduleConfig{
			Cancun: params.DefaultCancunBlobConfig,
			Prague: params.DefaultPragueBlobConfig,
			Osaka:  params.DefaultOsakaBlobConfig,
			Verkle: params.DefaultPragueBlobConfig,
		},
	}

	genesis := &Genesis{
		BaseFee:    big.NewInt(params.InitialBaseFee),
		Config:     verkleConfig,
		Timestamp:  verkleTime,
		Difficulty: big.NewInt(0),
		Alloc: types.GenesisAlloc{
			{1}: {Balance: big.NewInt(1), Storage: map[common.Hash]common.Hash{{1}: {1}}},
		},
	}

	expected := common.FromHex("018d20eebb130b5e2b796465fe36aafab650650729a92435aec071bf2386f080")
	got := genesis.ToBlock().Root().Bytes()
	if !bytes.Equal(got, expected) {
		t.Fatalf("invalid genesis state root, expected %x, got %x", expected, got)
	}

	db := rawdb.NewMemoryDatabase()

	config := *pathdb.Defaults
	config.NoAsyncFlush = true

	triedb := triedb.NewDatabase(db, &triedb.Config{
		IsVerkle: true,
		PathDB:   &config,
	})
	block := genesis.MustCommit(db, triedb)
	if !bytes.Equal(block.Root().Bytes(), expected) {
		t.Fatalf("invalid genesis state root, expected %x, got %x", expected, block.Root())
	}

	// Test that the trie is verkle
	if !triedb.IsVerkle() {
		t.Fatalf("expected trie to be verkle")
	}
	vdb := rawdb.NewTable(db, string(rawdb.VerklePrefix))
	if !rawdb.HasAccountTrieNode(vdb, nil) {
		t.Fatal("could not find node")
	}
}
