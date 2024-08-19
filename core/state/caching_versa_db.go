package state

import (
	"errors"
	"fmt"
	"sync/atomic"

	versa "github.com/bnb-chain/versioned-state-database"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/lru"
	"github.com/ethereum/go-ethereum/common/math"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/trie"
	"github.com/ethereum/go-ethereum/trie/trienode"
	"github.com/ethereum/go-ethereum/triedb"
)

const InvalidSateObjectVersion int64 = math.MinInt64

type cachingVersaDB struct {
	triedb        *triedb.Database
	versionDB     versa.Database
	codeDB        ethdb.KeyValueStore
	codeSizeCache *lru.Cache[common.Hash, int]
	codeCache     *lru.SizeConstrainedCache[common.Hash, []byte]

	accTree  *VersaTree
	state    versa.StateHandler
	root     common.Hash
	mode     versa.StateMode
	hasState atomic.Bool

	debug *DebugVersionState
}

// NewVersaDatabase should be call by NewDatabaseWithNodeDB
// TODO:: NewDatabaseWithNodeDB should add mode param.
func NewVersaDatabase(db ethdb.Database, triedb *triedb.Database, mode versa.StateMode) Database {
	return &cachingVersaDB{
		triedb:        triedb,
		versionDB:     triedb.VersaDB(),
		codeDB:        db,
		codeSizeCache: lru.NewCache[common.Hash, int](codeSizeCacheSize),
		codeCache:     lru.NewSizeConstrainedCache[common.Hash, []byte](codeCacheSize),
		mode:          mode,
		state:         versa.ErrStateHandler,
	}
}

func (cv *cachingVersaDB) Copy() Database {
	cp := &cachingVersaDB{}
	cp.codeCache = cv.codeCache
	cp.codeSizeCache = cv.codeSizeCache
	cp.triedb = cv.triedb
	cp.versionDB = cv.versionDB
	cp.codeDB = cv.codeDB
	cp.mode = versa.S_RW // it is important

	// TODO:: maybe add lock for cv.root
	if cv.hasState.Load() {
		_, err := cp.OpenTrie(cv.root)
		if err != nil {
			if cv.debug != nil {
				cv.debug.OnError(fmt.Errorf("failed to open trie in copy caching versa db, error: %s", err.Error()))
			}
			return cp
		}
	}
	return cp
}

// CopyTrie is used with Copy()
func (cv *cachingVersaDB) CopyTrie(tr Trie) Trie {
	vtr, ok := tr.(*VersaTree)
	if !ok {
		panic("caching versa db copy non versa tree")
	}
	if vtr.accountTree {
		if cv.accTree != nil {
			if cv.accTree.root.Cmp(vtr.root) != 0 {
				panic("copy acc trie mismatch")
			}
			return cv.accTree
		}
		tree, err := cv.OpenTrie(vtr.root)
		if err != nil {
			if cv.debug != nil {
				cv.debug.OnError(fmt.Errorf("failed to open trie in copy versa trie, error: %s", err.Error()))
			}
			return nil
		}
		return tree
	} else {
		tree, err := cv.OpenStorageTrie(vtr.stateRoot, vtr.address, vtr.root, nil)
		if err != nil {
			if cv.debug != nil {
				cv.debug.OnError(fmt.Errorf("failed to open storage trie in copy versa trie, error: %s", err.Error()))
			}
			return nil
		}
		return tree
	}
	return nil
}

func (cv *cachingVersaDB) HasState(root common.Hash) bool {
	return cv.versionDB.HasState(root)
}

func (cv *cachingVersaDB) OpenTrie(root common.Hash) (Trie, error) {
	if cv.hasState.Load() {
		//TODO:: will change to log.Error after stabilization
		panic("account tree has open")
	}

	// TODO:: if root tree, versa db should ignore check version, temp use -1
	state, err := cv.versionDB.OpenState(-1, root, cv.mode)
	if err != nil {
		if cv.debug != nil {
			cv.debug.OnError(fmt.Errorf("failed to open state, root:%s, error: %s", root.String(), err.Error()))
		}
		return nil, err
	}

	handler, err := cv.versionDB.OpenTree(state, -1, common.Hash{}, root)
	if err != nil {
		if cv.debug != nil {
			cv.debug.OnError(fmt.Errorf("failed to open account trie, root:%s, error: %s", root.String(), err.Error()))
		}
		return nil, err
	}

	tree := &VersaTree{
		db:          cv.versionDB,
		handler:     handler,
		accountTree: true,
		root:        root,
		mode:        cv.mode,
		debug:       cv.debug,
	}

	cv.state = state
	cv.hasState.Store(true) // if set, can't change
	cv.accTree = tree
	cv.root = root

	if cv.debug != nil {
		cv.debug.OnOpenTree(handler, common.Hash{}, common.Address{})
	}

	return tree, nil
}

func (cv *cachingVersaDB) OpenStorageTrie(stateRoot common.Hash, address common.Address, root common.Hash, _ Trie) (Trie, error) {
	version, _, err := cv.accTree.getAccountWithVersion(address)
	if err != nil {
		return nil, err
	}
	return cv.openStorageTreeWithVersion(version, stateRoot, address, root)
}

func (cv *cachingVersaDB) openStorageTreeWithVersion(version int64, stateRoot common.Hash, address common.Address, root common.Hash) (Trie, error) {
	if !cv.hasState.Load() {
		//TODO:: will change to log.Error after stabilization
		panic("open account tree, before open storage tree")
	}
	if cv.root.Cmp(stateRoot) != 0 {
		panic(fmt.Sprintf("account root mismatch, on open storage tree, actual: %s, expect: %s", root.String(), cv.root.String()))
	}

	owner := crypto.Keccak256Hash(address.Bytes())
	handler, err := cv.versionDB.OpenTree(cv.state, version, owner, root)
	if err != nil {
		if cv.debug != nil {
			cv.debug.OnError(fmt.Errorf("failed to open storage trie, version: %d,stateRoot:%s, address:%s, root: %s, error: %s",
				version, stateRoot.String(), address.String(), root.String(), err.Error()))
		}
		return nil, err
	}

	if cv.debug != nil {
		cv.debug.OnOpenTree(handler, owner, address)
	}
	tree := &VersaTree{
		db:        cv.versionDB,
		handler:   handler,
		version:   version,
		root:      stateRoot,
		stateRoot: root,
		address:   address,
		mode:      cv.mode,
		debug:     cv.debug,
	}
	return tree, nil
}

// Flush unique to versa
func (cv *cachingVersaDB) Flush() error {
	err := cv.versionDB.Flush(cv.state)
	if err != nil && cv.debug != nil {
		cv.debug.OnError(fmt.Errorf("failed to flush state, version: %d, root:%s, mode:%d, error: %s",
			cv.accTree.version, cv.accTree.root.String(), cv.accTree.mode, err.Error()))
	}
	return err
}

func (cv *cachingVersaDB) SetVersion(version int64) {
	cv.debug = NewDebugVersionState(cv.codeDB, cv.versionDB)
	cv.debug.Version = version
}

// Release unique to versa
func (cv *cachingVersaDB) Release() error {
	//log.Info("close state", "state info", cv.versionDB.ParseStateHandler(cv.state))
	if cv.state != versa.ErrStateHandler {
		if cv.debug != nil {
			cv.debug.OnCloseState(cv.state)
		}
		if err := cv.versionDB.CloseState(cv.state); err != nil {
			if cv.debug != nil {
				cv.debug.OnError(fmt.Errorf("failed to close state in release, version: %d, root:%s, mode:%d, error: %s",
					cv.accTree.version, cv.accTree.root.String(), cv.accTree.mode, err.Error()))
			}
			return err
		}
		cv.hasState.Store(false)
		cv.accTree = nil
		cv.state = versa.ErrStateHandler
		cv.root = common.Hash{}
		cv.debug = nil
	}
	return nil
}

func (cv *cachingVersaDB) Reset() {
	if cv.state != versa.ErrStateHandler {
		_ = cv.versionDB.CloseState(cv.state)
		panic("close state in reset")
	}
	cv.hasState.Store(false)
	cv.accTree = nil
	cv.state = versa.ErrStateHandler
	cv.root = common.Hash{}
}

func (cv *cachingVersaDB) HasTreeExpired(tr Trie) bool {
	vtr, ok := tr.(*VersaTree)
	if !ok {
		panic("trie type mismatch")
	}
	return cv.versionDB.HasTreeExpired(vtr.handler)
}

func (cv *cachingVersaDB) Scheme() string {
	return cv.triedb.Scheme()
}

func (cv *cachingVersaDB) ContractCode(addr common.Address, codeHash common.Hash) ([]byte, error) {
	if cv.debug != nil {
		cv.debug.OnGetCode(addr, codeHash)
	}
	code, _ := cv.codeCache.Get(codeHash)
	if len(code) > 0 {
		return code, nil
	}
	code = rawdb.ReadCode(cv.codeDB, codeHash)
	if len(code) > 0 {
		cv.codeCache.Add(codeHash, code)
		cv.codeSizeCache.Add(codeHash, len(code))
		return code, nil
	}
	return nil, errors.New("not found")
}

func (cv *cachingVersaDB) ContractCodeSize(addr common.Address, codeHash common.Hash) (int, error) {
	if cached, ok := cv.codeSizeCache.Get(codeHash); ok {
		return cached, nil
	}
	code, err := cv.ContractCode(addr, codeHash)
	return len(code), err
}

func (cv *cachingVersaDB) ContractCodeWithPrefix(address common.Address, codeHash common.Hash) ([]byte, error) {
	code, _ := cv.codeCache.Get(codeHash)
	if len(code) > 0 {
		return code, nil
	}
	code = rawdb.ReadCodeWithPrefix(cv.codeDB, codeHash)
	if len(code) > 0 {
		cv.codeCache.Add(codeHash, code)
		cv.codeSizeCache.Add(codeHash, len(code))
		return code, nil
	}
	return nil, errors.New("not found")
}

func (cv *cachingVersaDB) DiskDB() ethdb.KeyValueStore {
	return cv.codeDB
}

func (cv *cachingVersaDB) TrieDB() *triedb.Database {
	return cv.triedb
}

func (cv *cachingVersaDB) NoTries() bool {
	// TODO:: not support fastnode
	return false
}

type VersaTree struct {
	db          versa.Database
	handler     versa.TreeHandler
	version     int64
	accountTree bool
	debug       *DebugVersionState

	// TODO:: debugging, used for logging
	stateRoot common.Hash
	root      common.Hash
	address   common.Address
	mode      versa.StateMode
}

func (vt *VersaTree) GetKey(key []byte) []byte {
	_, val, err := vt.db.Get(vt.handler, key)
	if err != nil {
		log.Warn("failed to get key from version db")
	}
	return val
}

func (vt *VersaTree) GetAccount(address common.Address) (*types.StateAccount, error) {
	_, res, err := vt.getAccountWithVersion(address)
	return res, err
}

func (vt *VersaTree) getAccountWithVersion(address common.Address) (int64, *types.StateAccount, error) {
	vt.CheckAccountTree()
	ver, res, err := vt.db.Get(vt.handler, address.Bytes())
	if res == nil || err != nil {
		if err != nil && vt.debug != nil {
			vt.debug.OnError(fmt.Errorf("failed to get account, root: %s, address: %s, error: %s",
				vt.root.String(), address.String(), err.Error()))
		}
		return ver, nil, err
	}
	ret := new(types.StateAccount)
	err = rlp.DecodeBytes(res, ret)
	if err != nil {
		if vt.debug != nil {
			vt.debug.OnError(fmt.Errorf("failed to rlp decode account, root: %s, address: %s, error: %s",
				vt.root.String(), address.String(), err.Error()))
		}
	}
	if vt.debug != nil {
		vt.debug.OnGetAccount(address, ret)
	}
	return ver, ret, err
}

func (vt *VersaTree) GetStorage(address common.Address, key []byte) ([]byte, error) {
	if vt.address.Cmp(address) != 0 {
		panic(fmt.Sprintf("address mismatch in get storage, expect: %s, actul: %s", vt.address.String(), address.String()))
	}
	vt.CheckStorageTree()
	_, enc, err := vt.db.Get(vt.handler, key)
	if err != nil || len(enc) == 0 {
		if err != nil && vt.debug != nil {
			vt.debug.OnError(fmt.Errorf("failed to get storage, root: %s, stateRoot: %s, address:%s, key: %s, error: %s",
				vt.root.String(), vt.stateRoot.String(), address.String(), common.Bytes2Hex(key), err.Error()))
		}
		return nil, err
	}
	_, content, _, err := rlp.Split(enc)
	if err != nil {
		if vt.debug != nil {
			vt.debug.OnError(fmt.Errorf("failed to rlp decode storage, root: %s, stateRoot: %s, address: %s, key: %s,error: %s",
				vt.root.String(), vt.stateRoot.String(), address.String(), common.Bytes2Hex(key), err.Error()))
		}
	}
	if vt.debug != nil {
		vt.debug.OnGetStorage(vt.handler, address, key, content)
	}
	return content, err
}

func (vt *VersaTree) UpdateAccount(address common.Address, account *types.StateAccount) error {
	vt.CheckAccountTree()
	data, err := rlp.EncodeToBytes(account)
	if err != nil {
		if vt.debug != nil {
			vt.debug.OnError(fmt.Errorf("failed to update account, root: %s, address: %s, account: %s, error: %s",
				vt.root.String(), address.String(), account.String(), err.Error()))
		}
		return err
	}
	if vt.debug != nil {
		vt.debug.OnUpdateAccount(address, account)
	}
	return vt.db.Put(vt.handler, address.Bytes(), data)
}

func (vt *VersaTree) UpdateStorage(address common.Address, key, value []byte) error {
	if vt.address.Cmp(address) != 0 {
		panic(fmt.Sprintf("address mismatch in get storage, expect: %s, actul: %s", vt.address.String(), address.String()))
	}
	vt.CheckStorageTree()
	v, _ := rlp.EncodeToBytes(value)
	err := vt.db.Put(vt.handler, key, v)
	if err != nil {
		if vt.debug != nil {
			vt.debug.OnError(fmt.Errorf("failed to update storage, root: %s, stateRoot: %s, address: %s, key: %s, val: %s, error: %s",
				vt.root.String(), vt.stateRoot.String(), address.String(), common.Bytes2Hex(key), common.Bytes2Hex(value), err.Error()))
		}
	}
	if vt.debug != nil {
		vt.debug.OnUpdateStorage(vt.handler, address, key, value)
	}
	return err
}

func (vt *VersaTree) DeleteAccount(address common.Address) error {
	vt.CheckAccountTree()
	err := vt.db.Delete(vt.handler, address.Bytes())
	if err != nil {
		if vt.debug != nil {
			vt.debug.OnError(fmt.Errorf("failed to delete account, root: %s, address: %s, error: %s",
				vt.root.String(), address.String(), err.Error()))
		}
	}
	if vt.debug != nil {
		vt.debug.OnDeleteAccount(address)
	}
	return err
}

func (vt *VersaTree) DeleteStorage(address common.Address, key []byte) error {
	vt.CheckStorageTree()
	err := vt.db.Delete(vt.handler, key)
	if err != nil {
		if vt.debug != nil {
			vt.debug.OnError(fmt.Errorf("failed to delete storage, root: %s, stateRoot: %s, address: %s, key: %s, error: %s",
				vt.root.String(), vt.stateRoot.String(), address.String(), common.Bytes2Hex(key), err.Error()))
		}
	}
	if vt.debug != nil {
		vt.debug.OnDeleteStorage(vt.handler, address, key)
	}
	return err
}

func (vt *VersaTree) UpdateContractCode(address common.Address, codeHash common.Hash, code []byte) error {
	return nil
}

func (vt *VersaTree) Hash() common.Hash {
	hash, err := vt.db.CalcRootHash(vt.handler)
	if err != nil {
		if vt.debug != nil {
			vt.debug.OnError(fmt.Errorf("failed to calc root, root: %s, stateRoot%s, error:%s",
				vt.root.String(), vt.stateRoot.String(), err.Error()))
		}
	}
	if vt.debug != nil {
		vt.debug.OnCalcHash(vt.address, hash)
	}
	return hash
}

func (vt *VersaTree) Commit(_ bool) (common.Hash, *trienode.NodeSet, error) {
	hash, err := vt.db.Commit(vt.handler)
	if err != nil {
		if vt.debug != nil {
			vt.debug.OnError(fmt.Errorf("failed to commit versa tree, root: %s, stateRoot: %s, error: %s",
				vt.root.String(), vt.stateRoot.String(), err.Error()))
		}
	}
	if vt.debug != nil {
		vt.debug.OnCommitTree(vt.address, vt.handler)
	}
	return hash, nil, err
}

func (vt *VersaTree) NodeIterator(startKey []byte) (trie.NodeIterator, error) {
	panic("versa tree not support iterate")
	return nil, nil
}

func (vt *VersaTree) Prove(key []byte, proofDb ethdb.KeyValueWriter) error {
	panic("versa tree not support prove")
	return nil
}

// TODO:: debug code, will be deleted after stabilization
func (vt *VersaTree) CheckAccountTree() {
	if !vt.accountTree {
		panic("sub tree can't operate account")
	}
}

// TODO:: debug code, will be deleted after stabilization
func (vt *VersaTree) CheckStorageTree() {
	if vt.accountTree {
		panic("root tree can't operate storage")
	}
}
