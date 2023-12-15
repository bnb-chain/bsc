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
	"encoding/json"
	"math/big"
	"reflect"
	"testing"

	"github.com/davecgh/go-spew/spew"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus/ethash"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/trie"
	"github.com/ethereum/go-ethereum/trie/triedb/pathdb"
)

func TestSetupGenesis(t *testing.T) {
	testSetupGenesis(t, rawdb.HashScheme)
	testSetupGenesis(t, rawdb.PathScheme)
}

func testSetupGenesis(t *testing.T, scheme string) {
	var (
		customghash = common.HexToHash("0x89c99d90b79719238d2645c7642f2c9295246e80775b38cfd162b696817fbd50")
		customg     = Genesis{
			Config: &params.ChainConfig{HomesteadBlock: big.NewInt(3)},
			Alloc: GenesisAlloc{
				{1}: {Balance: big.NewInt(1), Storage: map[common.Hash]common.Hash{{1}: {1}}},
			},
		}
		oldcustomg = customg
	)
	oldcustomg.Config = &params.ChainConfig{HomesteadBlock: big.NewInt(2)}

	tests := []struct {
		name       string
		fn         func(ethdb.Database) (*params.ChainConfig, common.Hash, error)
		wantConfig *params.ChainConfig
		wantHash   common.Hash
		wantErr    error
	}{
		{
			name: "genesis without ChainConfig",
			fn: func(db ethdb.Database) (*params.ChainConfig, common.Hash, error) {
				return SetupGenesisBlock(db, trie.NewDatabase(db, newDbConfig(scheme)), new(Genesis))
			},
			wantErr:    errGenesisNoConfig,
			wantConfig: params.AllEthashProtocolChanges,
		},
		{
			name: "no block in DB, genesis == nil",
			fn: func(db ethdb.Database) (*params.ChainConfig, common.Hash, error) {
				return SetupGenesisBlock(db, trie.NewDatabase(db, newDbConfig(scheme)), nil)
			},
			wantHash:   params.BSCGenesisHash,
			wantConfig: params.BSCChainConfig,
		},
		{
			name: "mainnet block in DB, genesis == nil",
			fn: func(db ethdb.Database) (*params.ChainConfig, common.Hash, error) {
				DefaultGenesisBlock().MustCommit(db, trie.NewDatabase(db, newDbConfig(scheme)))
				return SetupGenesisBlock(db, trie.NewDatabase(db, newDbConfig(scheme)), nil)
			},
			wantHash:   params.MainnetGenesisHash,
			wantConfig: params.MainnetChainConfig,
		},
		{
			name: "custom block in DB, genesis == nil",
			fn: func(db ethdb.Database) (*params.ChainConfig, common.Hash, error) {
				tdb := trie.NewDatabase(db, newDbConfig(scheme))
				customg.Commit(db, tdb)
				return SetupGenesisBlock(db, tdb, nil)
			},
			wantHash:   customghash,
			wantConfig: customg.Config,
		},
		{
			name: "compatible config in DB",
			fn: func(db ethdb.Database) (*params.ChainConfig, common.Hash, error) {
				tdb := trie.NewDatabase(db, newDbConfig(scheme))
				oldcustomg.Commit(db, tdb)
				return SetupGenesisBlock(db, tdb, &customg)
			},
			wantHash:   customghash,
			wantConfig: customg.Config,
		},
		{
			name: "incompatible config in DB",
			fn: func(db ethdb.Database) (*params.ChainConfig, common.Hash, error) {
				// Commit the 'old' genesis block with Homestead transition at #2.
				// Advance to block #4, past the homestead transition block of customg.
				tdb := trie.NewDatabase(db, newDbConfig(scheme))
				oldcustomg.Commit(db, tdb)

				bc, _ := NewBlockChain(db, DefaultCacheConfigWithScheme(scheme), &oldcustomg, nil, ethash.NewFullFaker(), vm.Config{}, nil, nil)
				defer bc.Stop()

				_, blocks, _ := GenerateChainWithGenesis(&oldcustomg, ethash.NewFaker(), 4, nil)
				bc.InsertChain(blocks)

				// This should return a compatibility error.
				return SetupGenesisBlock(db, tdb, &customg)
			},
			wantHash:   customghash,
			wantConfig: customg.Config,
			wantErr: &params.ConfigCompatError{
				What:          "Homestead fork block",
				StoredBlock:   big.NewInt(2),
				NewBlock:      big.NewInt(3),
				RewindToBlock: 1,
			},
		},
	}

	for _, test := range tests {
		db := rawdb.NewMemoryDatabase()
		config, hash, err := test.fn(db)
		// Check the return values.
		if !reflect.DeepEqual(err, test.wantErr) {
			spew := spew.ConfigState{DisablePointerAddresses: true, DisableCapacities: true}
			t.Errorf("%s: returned error %#v, want %#v", test.name, spew.NewFormatter(err), spew.NewFormatter(test.wantErr))
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
		if have := c.genesis.MustCommit(db, trie.NewDatabase(db, trie.HashDefaults)).Hash(); have != c.want {
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
	genesisBlock := genesis.MustCommit(db, trie.NewDatabase(db, trie.HashDefaults))

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
		alloc = &GenesisAlloc{
			{1}: {Balance: big.NewInt(1), Storage: map[common.Hash]common.Hash{{1}: {1}}},
			{2}: {Balance: big.NewInt(2), Storage: map[common.Hash]common.Hash{{2}: {2}}},
		}
		hash, _ = alloc.deriveHash()
	)
	blob, _ := json.Marshal(alloc)
	rawdb.WriteGenesisStateSpec(db, hash, blob)

	var reload GenesisAlloc
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
	config := defaultGenesis.configOrDefault(gHash)

	if config.ChainID.Cmp(params.MainnetChainConfig.ChainID) != 0 {
		t.Errorf("ChainID of resulting config should be %v, but is %v instead", params.BSCChainConfig.ChainID, config.ChainID)
	}

	if config.HomesteadBlock.Cmp(params.MainnetChainConfig.HomesteadBlock) != 0 {
		t.Errorf("resulting config should have HomesteadBlock = %v, but instead is %v", params.MainnetChainConfig, config.HomesteadBlock)
	}

	if config.PlanckBlock == nil {
		t.Errorf("resulting config should have PlanckBlock = %v , but instead is nil", params.BSCChainConfig.PlanckBlock)
	}

	if config.PlanckBlock.Cmp(params.BSCChainConfig.PlanckBlock) != 0 {
		t.Errorf("resulting config should have PlanckBlock = %v , but instead is %v", params.BSCChainConfig.PlanckBlock, config.PlanckBlock)
	}
}

func TestSetDefaultHardforkValues(t *testing.T) {
	genesis := &Genesis{Config: &params.ChainConfig{ChainID: big.NewInt(66), HomesteadBlock: big.NewInt(11)}}
	genesis.setDefaultHardforkValues(params.BSCChainConfig)

	// Make sure the non-nil block was not modified
	if genesis.Config.HomesteadBlock.Cmp(big.NewInt(11)) != 0 {
		t.Errorf("Homestead block should not have been modified. HomesteadBlock = %v", genesis.Config.HomesteadBlock)
	}

	// Spot check a few blocks
	if genesis.Config.NielsBlock.Cmp(params.BSCChainConfig.NielsBlock) != 0 {
		t.Errorf("Niels block not matching: in genesis = %v , in defaultConfig = %v", genesis.Config.NielsBlock, params.BSCChainConfig.NielsBlock)
	}

	if genesis.Config.NanoBlock.Cmp(params.BSCChainConfig.NanoBlock) != 0 {
		t.Errorf("Nano block not matching: in genesis = %v , in defaultConfig = %v", genesis.Config.NanoBlock, params.BSCChainConfig.NanoBlock)
	}

	if genesis.Config.PlanckBlock.Cmp(params.BSCChainConfig.PlanckBlock) != 0 {
		t.Errorf("Nano block not matching: in genesis = %v , in defaultConfig = %v", genesis.Config.PlanckBlock, params.BSCChainConfig.PlanckBlock)
	}

	// Spot check a few times
	if *genesis.Config.ShanghaiTime != *params.BSCChainConfig.ShanghaiTime {
		t.Errorf("Shanghai Time not matching: in genesis = %d , in defaultConfig = %d", *genesis.Config.ShanghaiTime, *params.BSCChainConfig.ShanghaiTime)
	}
	if *genesis.Config.KeplerTime != *params.BSCChainConfig.KeplerTime {
		t.Errorf("Kepler Time not matching: in genesis = %d , in defaultConfig = %d", *genesis.Config.KeplerTime, *params.BSCChainConfig.KeplerTime)
	}

	// Lastly make sure non-block fields such as ChainID have not been modified
	if genesis.Config.ChainID.Cmp(big.NewInt(66)) != 0 {
		t.Errorf("ChainID should not have been modified. ChainID = %v", genesis.Config.ChainID)
	}
}

func newDbConfig(scheme string) *trie.Config {
	if scheme == rawdb.HashScheme {
		return trie.HashDefaults
	}
	return &trie.Config{PathDB: pathdb.Defaults}
}

func configBlockEqual(x, y *big.Int) bool {
	if x == nil {
		return y == nil
	}
	if y == nil {
		return x == nil
	}
	return x.Cmp(y) == 0
}

func configTimestampEqual(x, y *uint64) bool {
	if x == nil {
		return y == nil
	}
	if y == nil {
		return x == nil
	}
	return *x == *y
}

func newUint64(val uint64) *uint64 { return &val }

func TestLoadChainConfig(t *testing.T) {
	mainetGenesis := *DefaultBSCGenesisBlock()

	mainetGenesisNewgKepler := mainetGenesis
	mainetGenesisNewgKepler.Config = &params.ChainConfig{
		ChainID:             big.NewInt(56),
		HomesteadBlock:      big.NewInt(0),
		EIP150Block:         big.NewInt(0),
		EIP155Block:         big.NewInt(0),
		EIP158Block:         big.NewInt(0),
		ByzantiumBlock:      big.NewInt(0),
		ConstantinopleBlock: big.NewInt(0),
		PetersburgBlock:     big.NewInt(0),
		IstanbulBlock:       big.NewInt(0),
		MuirGlacierBlock:    big.NewInt(0),
		RamanujanBlock:      big.NewInt(0),
		NielsBlock:          big.NewInt(0),
		MirrorSyncBlock:     big.NewInt(5184000),
		BrunoBlock:          big.NewInt(13082000),
		EulerBlock:          big.NewInt(18907621),
		NanoBlock:           big.NewInt(21962149),
		MoranBlock:          big.NewInt(22107423),
		GibbsBlock:          big.NewInt(23846001),
		PlanckBlock:         big.NewInt(27281024),
		LubanBlock:          big.NewInt(29020050),
		PlatoBlock:          big.NewInt(30720096),
		BerlinBlock:         big.NewInt(31302048),
		LondonBlock:         big.NewInt(31302048),
		HertzBlock:          big.NewInt(31302048),
		HertzfixBlock:       big.NewInt(34140700),
		// UnixTime: 1705996800 is January 23, 2024 8:00:00 AM UTC
		ShanghaiTime: newUint64(1705996800),
		KeplerTime:   newUint64(1705996800),

		Parlia: &params.ParliaConfig{
			Period: 3,
			Epoch:  200,
		},
	}
	mainetGenesisNewgKepler.Config.KeplerTime = newUint64(*mainetGenesis.Config.KeplerTime + 1)

	headbeforeKepler := mainetGenesis.ToBlock().Header()
	headbeforeKepler.Number = mainetGenesis.Config.LondonBlock
	headbeforeKepler.Time = *mainetGenesis.Config.KeplerTime - 1

	headAfterKepler := mainetGenesis.ToBlock().Header()
	headAfterKepler.Number = mainetGenesis.Config.LondonBlock
	headAfterKepler.Time = *mainetGenesis.Config.KeplerTime + 1
	headAfterKeplerErrFromNil := params.NewTimestampCompatError("Kepler fork timestamp", nil, mainetGenesis.Config.KeplerTime)
	headAfterKeplerErrFromNonNil := params.NewTimestampCompatError("Kepler fork timestamp", mainetGenesis.Config.KeplerTime, &headAfterKepler.Time)

	mainetGenesisNoKepler := mainetGenesisNewgKepler
	mainetGenesisNoKepler.Config = &params.ChainConfig{
		ChainID:             big.NewInt(56),
		HomesteadBlock:      big.NewInt(0),
		EIP150Block:         big.NewInt(0),
		EIP155Block:         big.NewInt(0),
		EIP158Block:         big.NewInt(0),
		ByzantiumBlock:      big.NewInt(0),
		ConstantinopleBlock: big.NewInt(0),
		PetersburgBlock:     big.NewInt(0),
		IstanbulBlock:       big.NewInt(0),
		MuirGlacierBlock:    big.NewInt(0),
		RamanujanBlock:      big.NewInt(0),
		NielsBlock:          big.NewInt(0),
		MirrorSyncBlock:     big.NewInt(5184000),
		BrunoBlock:          big.NewInt(13082000),
		EulerBlock:          big.NewInt(18907621),
		NanoBlock:           big.NewInt(21962149),
		MoranBlock:          big.NewInt(22107423),
		GibbsBlock:          big.NewInt(23846001),
		PlanckBlock:         big.NewInt(27281024),
		LubanBlock:          big.NewInt(29020050),
		PlatoBlock:          big.NewInt(30720096),
		BerlinBlock:         big.NewInt(31302048),
		LondonBlock:         big.NewInt(31302048),
		HertzBlock:          big.NewInt(31302048),
		HertzfixBlock:       big.NewInt(34140700),
		// UnixTime: 1705996800 is January 23, 2024 8:00:00 AM UTC
		ShanghaiTime: newUint64(1705996800),
		KeplerTime:   newUint64(1705996800),

		Parlia: &params.ParliaConfig{
			Period: 3,
			Epoch:  200,
		},
	}
	mainetGenesisNoKepler.Config.KeplerTime = nil

	privateNetGenesisHash := common.Hash{1}

	for i, c := range []struct {
		storedHash    common.Hash
		storedCfg     *params.ChainConfig
		newGenesisCfg *Genesis
		currentHead   *types.Header
		expectedCfg   *params.ChainConfig
		expectedHash  common.Hash
		expectedErr   error
	}{
		// ------------if---- genesis == nil || genesis.Config == nil----------------//
		// private net
		// stored nil config
		{privateNetGenesisHash, nil, nil, headAfterKepler, nil, common.Hash{}, errGenesisNoConfig},
		// stored config without Kepler
		{privateNetGenesisHash, mainetGenesisNoKepler.Config, nil, headbeforeKepler, mainetGenesisNoKepler.Config, privateNetGenesisHash, nil},
		// stored newest config, current head before kepler
		{privateNetGenesisHash, params.BSCChainConfig, nil, headbeforeKepler, params.BSCChainConfig, privateNetGenesisHash, nil},
		// stored newest config, current head after kepler
		{privateNetGenesisHash, params.BSCChainConfig, nil, headbeforeKepler, params.BSCChainConfig, privateNetGenesisHash, nil},
		// mainnet
		// stored nil config
		{params.BSCGenesisHash, nil, nil, headAfterKepler, params.BSCChainConfig, params.BSCGenesisHash, nil},
		// stored config without Keple
		{params.BSCGenesisHash, mainetGenesisNoKepler.Config, nil, headbeforeKepler, params.BSCChainConfig, params.BSCGenesisHash, nil},
		// stored newest config, current head before kepler
		{params.BSCGenesisHash, params.BSCChainConfig, nil, headbeforeKepler, params.BSCChainConfig, params.BSCGenesisHash, nil},
		// stored newest config, current head after kepler
		{params.BSCGenesisHash, params.BSCChainConfig, nil, headbeforeKepler, params.BSCChainConfig, params.BSCGenesisHash, nil},

		// --------------------------------else-------------------------------------//
		// private net
		// stored config without Kepler, config without kepler, current head before kepler
		{privateNetGenesisHash, mainetGenesisNoKepler.Config, &mainetGenesisNoKepler, headbeforeKepler, mainetGenesisNoKepler.Config, privateNetGenesisHash, nil},
		// stored newest config, config with new Kepler time, current head before Kepler
		{privateNetGenesisHash, params.BSCChainConfig, &mainetGenesisNewgKepler, headbeforeKepler, mainetGenesisNewgKepler.Config, privateNetGenesisHash, nil},
		// stored newest config, config with new Kepler time, current head after Kepler
		{privateNetGenesisHash, params.BSCChainConfig, &mainetGenesisNewgKepler, headAfterKepler, mainetGenesisNewgKepler.Config, privateNetGenesisHash, headAfterKeplerErrFromNonNil},
		// stored newest config, same config
		{privateNetGenesisHash, params.BSCChainConfig, &mainetGenesis, headAfterKepler, params.BSCChainConfig, privateNetGenesisHash, nil},
		// mainnet
		// stored config without Kepler, config without kepler, current head after kepler
		{params.BSCGenesisHash, mainetGenesisNoKepler.Config, &mainetGenesisNoKepler, headAfterKepler, params.BSCChainConfig, params.BSCGenesisHash, headAfterKeplerErrFromNil},
		// stored config without Kepler, config without kepler, current head before kepler, after this case mainetGenesisNoKepler.Config.KeplerTime changed
		{params.BSCGenesisHash, mainetGenesisNoKepler.Config, &mainetGenesisNoKepler, headbeforeKepler, params.BSCChainConfig, params.BSCGenesisHash, nil},
		// stored newest config, config with new Kepler time, current head before Kepler
		{params.BSCGenesisHash, params.BSCChainConfig, &mainetGenesisNewgKepler, headbeforeKepler, mainetGenesisNewgKepler.Config, params.BSCGenesisHash, nil},
		// stored newest config, config with new Kepler time, current head after Kepler
		{params.BSCGenesisHash, params.BSCChainConfig, &mainetGenesisNewgKepler, headAfterKepler, mainetGenesisNewgKepler.Config, params.BSCGenesisHash, headAfterKeplerErrFromNonNil},
		// stored newest config, same config, current head after kepler
		{params.BSCGenesisHash, params.BSCChainConfig, &mainetGenesis, headAfterKepler, params.BSCChainConfig, params.BSCGenesisHash, nil},
	} {
		// prepare
		db := rawdb.NewMemoryDatabase()
		rawdb.WriteCanonicalHash(db, c.storedHash, 0)
		if c.storedCfg != nil {
			rawdb.WriteChainConfig(db, c.storedHash, c.storedCfg)
		}
		rawdb.WriteHeadHeaderHash(db, c.currentHead.Hash())
		rawdb.WriteHeader(db, c.currentHead)

		// load
		loadCfg, loadHash, LoadErr := LoadChainConfig(db, c.newGenesisCfg)

		// spot check
		if (LoadErr == nil && c.expectedErr != nil) ||
			(LoadErr != nil && c.expectedErr == nil) ||
			(LoadErr != nil && c.expectedErr != nil && LoadErr.Error() != c.expectedErr.Error()) {
			t.Errorf("Case %d, Load error not matching: want = %v , load = %v", i, c.expectedErr, LoadErr)
		}
		if loadHash != c.expectedHash {
			t.Errorf("Case %d, Load Genesis Hash not matching: want = %v , load = %v", i, c.expectedHash, loadHash)
		}
		if (loadCfg == nil && c.expectedCfg != nil) ||
			(loadCfg != nil && c.expectedCfg == nil) ||
			((loadCfg != nil && c.expectedCfg != nil) && (!configBlockEqual(loadCfg.HertzBlock, c.expectedCfg.HertzBlock) ||
				!configTimestampEqual(loadCfg.KeplerTime, c.expectedCfg.KeplerTime))) {
			t.Errorf("Case %d, Load Config not matching:  want = %v , load = %v", i, c.expectedCfg, loadCfg)
		}
	}
}
