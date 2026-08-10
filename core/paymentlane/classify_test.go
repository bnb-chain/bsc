package paymentlane

import (
	"errors"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/params"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"
)

// accountFn lets each test define its own parent-state lookup.
type accountFn struct {
	fn    func(common.Address) (*types.StateAccount, error)
	reads []common.Address // every address actually read, in order
}

func (r *accountFn) Account(addr common.Address) (*types.StateAccount, error) {
	r.reads = append(r.reads, addr)
	return r.fn(addr)
}

// forbidReads fails the test if the classifier touches state at all.
func forbidReads(t *testing.T) *accountFn {
	t.Helper()
	return &accountFn{fn: func(addr common.Address) (*types.StateAccount, error) {
		t.Fatalf("classifier read state for %x, but every static gate should have decided first", addr)
		return nil, nil
	}}
}

// absentAccounts models the common "destination never seen" case.
func absentAccounts() *accountFn {
	return &accountFn{fn: func(common.Address) (*types.StateAccount, error) { return nil, nil }}
}

func codedAccounts() *accountFn {
	return &accountFn{fn: func(common.Address) (*types.StateAccount, error) {
		return &types.StateAccount{CodeHash: common.HexToHash("0xdead").Bytes()}, nil
	}}
}

var (
	plainDest  = common.HexToAddress("0x00000000000000000000000000000000000f0001")
	listedDest = common.HexToAddress("0x00000000000000000000000000000000000f0002")
)

func listedSet(addrs ...common.Address) map[common.Address]struct{} {
	set := make(map[common.Address]struct{}, len(addrs))
	for _, a := range addrs {
		set[a] = struct{}{}
	}
	return set
}

// txOpts covers only the fields Classify inspects.
type txOpts struct {
	txType     byte
	to         *common.Address
	data       []byte
	value      *big.Int
	accessList types.AccessList
}

func makeTx(t *testing.T, o txOpts) *types.Transaction {
	t.Helper()
	value := o.value
	if value == nil {
		value = common.Big0
	}
	switch o.txType {
	case types.LegacyTxType:
		require.Empty(t, o.accessList, "a legacy transaction cannot carry an access list")
		return types.NewTx(&types.LegacyTx{To: o.to, Value: value, Data: o.data, Gas: 21000})
	case types.AccessListTxType:
		return types.NewTx(&types.AccessListTx{To: o.to, Value: value, Data: o.data, Gas: 21000, AccessList: o.accessList})
	case types.DynamicFeeTxType:
		return types.NewTx(&types.DynamicFeeTx{To: o.to, Value: value, Data: o.data, Gas: 21000, AccessList: o.accessList})
	case types.BlobTxType:
		require.NotNil(t, o.to, "BlobTx.To is not nullable")
		return types.NewTx(&types.BlobTx{
			To: *o.to, Value: uint256.MustFromBig(value), Data: o.data, Gas: 21000,
			AccessList: o.accessList, BlobHashes: []common.Hash{{0x01}},
		})
	case types.SetCodeTxType:
		require.NotNil(t, o.to, "SetCodeTx.To is not nullable")
		return types.NewTx(&types.SetCodeTx{
			To: *o.to, Value: uint256.MustFromBig(value), Data: o.data, Gas: 21000,
			AccessList: o.accessList,
			AuthList:   []types.SetCodeAuthorization{{Address: *o.to}},
		})
	default:
		t.Fatalf("unsupported tx type %d", o.txType)
		return nil
	}
}

// Walk the static gates and assert both class and whether state was read.
func TestNoStateReadUntilEveryStaticGatePasses(t *testing.T) {
	oneEntryAccessList := types.AccessList{{Address: plainDest}}
	// ContractAddress is listed on purpose: membership alone decides.
	listed := listedSet(listedDest, ContractAddress)

	for _, txType := range []byte{
		types.LegacyTxType, types.AccessListTxType, types.DynamicFeeTxType,
		types.BlobTxType, types.SetCodeTxType,
	} {
		for _, dest := range []struct {
			name string
			addr *common.Address
		}{
			{"nil", nil},
			{"listed-system", &ContractAddress},
			{"listed", &listedDest},
			{"plain", &plainDest},
		} {
			for _, withAL := range []bool{false, true} {
				for _, withData := range []bool{false, true} {
					for _, withValue := range []bool{false, true} {
						if txType == types.LegacyTxType && withAL {
							continue // not representable
						}
						if dest.addr == nil && (txType == types.BlobTxType || txType == types.SetCodeTxType) {
							continue // To is not nullable for these
						}
						o := txOpts{txType: txType, to: dest.addr}
						if withAL {
							o.accessList = oneEntryAccessList
						}
						if withData {
							o.data = []byte{0x01, 0x02, 0x03, 0x04}
						}
						if withValue {
							o.value = big.NewInt(1)
						}
						tx := makeTx(t, o)

						typeOK := txType == types.LegacyTxType || txType == types.AccessListTxType || txType == types.DynamicFeeTxType
						isListed := dest.name == "listed" || dest.name == "listed-system"
						wantRead := dest.addr != nil && typeOK && !withAL &&
							!isListed && !withData && withValue

						var wantClass Class
						switch {
						case dest.addr == nil, !typeOK, withAL:
							wantClass = ClassGeneral
						case isListed:
							wantClass = ClassPayment
						case withData, !withValue:
							wantClass = ClassGeneral
						default:
							wantClass = ClassPayment // reaches gate 7; the account is absent below
						}

						reader := absentAccounts()
						got, err := NewClassifier(common.Hash{}, reader, listed).Classify(tx)
						require.NoError(t, err)
						require.Equal(t, wantClass, got,
							"type=%d dest=%s accessList=%v data=%v value=%v", txType, dest.name, withAL, withData, withValue)
						require.Equal(t, wantRead, len(reader.reads) == 1,
							"state read expectation: type=%d dest=%s accessList=%v data=%v value=%v",
							txType, dest.name, withAL, withData, withValue)
					}
				}
			}
		}
	}
}

// Precompiles are still payment destinations here.
func TestPrecompileDestinationIsPayment(t *testing.T) {
	for _, addr := range vm.ActivePrecompiles(params.Rules{
		IsHomestead: true, IsByzantium: true, IsIstanbul: true, IsBerlin: true,
		IsCancun: true, IsPrague: true, IsOsaka: true, IsInBSC: true,
	}) {
		tx := makeTx(t, txOpts{txType: types.LegacyTxType, to: &addr, value: big.NewInt(1)})
		got, err := NewClassifier(common.Hash{}, absentAccounts(), nil).Classify(tx)
		require.NoError(t, err)
		require.Equal(t, ClassPayment, got, "precompile %x", addr)
	}
}

// Cover every "no code" encoding the readers can return.
func TestCodeHashBoundaryCases(t *testing.T) {
	tx := makeTx(t, txOpts{txType: types.LegacyTxType, to: &plainDest, value: big.NewInt(1)})
	for _, tc := range []struct {
		name string
		acct *types.StateAccount
		want Class
	}{
		{"absent account", nil, ClassPayment},
		{"empty code hash", &types.StateAccount{CodeHash: types.EmptyCodeHash.Bytes()}, ClassPayment},
		{"nil code hash", &types.StateAccount{CodeHash: nil}, ClassPayment},
		{"zero-length code hash", &types.StateAccount{CodeHash: []byte{}}, ClassPayment},
		{"contract code hash", &types.StateAccount{CodeHash: common.HexToHash("0xbeef").Bytes()}, ClassGeneral},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := &accountFn{fn: func(common.Address) (*types.StateAccount, error) { return tc.acct, nil }}
			got, err := NewClassifier(common.Hash{}, r, nil).Classify(tx)
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}

// EIP-7702 designators must stay general.
func TestDelegationDesignatorIsGeneral(t *testing.T) {
	designator := types.AddressToDelegation(common.HexToAddress("0x00000000000000000000000000000000000f0009"))
	r := &accountFn{fn: func(common.Address) (*types.StateAccount, error) {
		return &types.StateAccount{CodeHash: crypto.Keccak256(designator)}, nil
	}}
	tx := makeTx(t, txOpts{txType: types.LegacyTxType, to: &plainDest, value: big.NewInt(1)})
	got, err := NewClassifier(common.Hash{}, r, nil).Classify(tx)
	require.NoError(t, err)
	require.Equal(t, ClassGeneral, got)
}

// Listed membership must win over calldata/value heuristics.
func TestListedContractSurvivesDataAndValueGates(t *testing.T) {
	// ERC-20 transfer(address,uint256): calldata present, zero value.
	calldata := make([]byte, 68)
	copy(calldata, []byte{0xa9, 0x05, 0x9c, 0xbb})

	for _, txType := range []byte{types.LegacyTxType, types.AccessListTxType, types.DynamicFeeTxType} {
		tx := makeTx(t, txOpts{txType: txType, to: &listedDest, data: calldata})
		got, err := NewClassifier(common.Hash{}, forbidReads(t), listedSet(listedDest)).Classify(tx)
		require.NoError(t, err)
		require.Equal(t, ClassPayment, got, "tx type %d", txType)
	}
}

// Blob and SetCode must stay general even to listed destinations.
func TestBlobAndSetCodeToListedContractAreGeneral(t *testing.T) {
	for _, txType := range []byte{types.BlobTxType, types.SetCodeTxType} {
		tx := makeTx(t, txOpts{txType: txType, to: &listedDest, value: big.NewInt(1)})
		got, err := NewClassifier(common.Hash{}, forbidReads(t), listedSet(listedDest)).Classify(tx)
		require.NoError(t, err)
		require.Equal(t, ClassGeneral, got, "tx type %d", txType)
	}
}

// State-read failures must fail shut and keep Err sticky.
func TestErrorPropagatesFailShutAndSticks(t *testing.T) {
	boom := errors.New("snapshot not covered yet")
	calls := 0
	r := &accountFn{fn: func(common.Address) (*types.StateAccount, error) {
		calls++
		if calls == 1 {
			return nil, boom
		}
		return nil, nil // a later read succeeds
	}}
	c := NewClassifier(common.HexToHash("0x0102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f20"), r, nil)

	tx := makeTx(t, txOpts{txType: types.LegacyTxType, to: &plainDest, value: big.NewInt(1)})
	got, err := c.Classify(tx)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrStateUnavailable)
	require.ErrorIs(t, err, boom)
	require.Equal(t, ClassGeneral, got, "the failure value must be general, not payment")
	require.Error(t, c.Err())

	// A later success must not clear the sticky error.
	other := common.HexToAddress("0x00000000000000000000000000000000000f000a")
	got, err = c.Classify(makeTx(t, txOpts{txType: types.LegacyTxType, to: &other, value: big.NewInt(1)}))
	require.NoError(t, err)
	require.Equal(t, ClassPayment, got)
	require.Error(t, c.Err(), "Err must stay set after a later success")
	require.ErrorIs(t, c.Err(), boom)
}

// Memoize successful reads per destination, but never memoize failures.
func TestMemoDoesOneReadPerDistinctDestination(t *testing.T) {
	dests := []common.Address{
		common.HexToAddress("0x00000000000000000000000000000000000f0011"),
		common.HexToAddress("0x00000000000000000000000000000000000f0012"),
		common.HexToAddress("0x00000000000000000000000000000000000f0013"),
	}
	r := absentAccounts()
	c := NewClassifier(common.Hash{}, r, nil)
	for i := 0; i < 30; i++ {
		dest := dests[i%len(dests)]
		got, err := c.Classify(makeTx(t, txOpts{txType: types.LegacyTxType, to: &dest, value: big.NewInt(1)}))
		require.NoError(t, err)
		require.Equal(t, ClassPayment, got)
	}
	require.Len(t, r.reads, len(dests), "absent accounts must be memoised too - they are the hot path")

	// A failed read must not be cached.
	failing := &accountFn{fn: func(common.Address) (*types.StateAccount, error) {
		return nil, errors.New("missing trie node")
	}}
	c2 := NewClassifier(common.Hash{}, failing, nil)
	tx := makeTx(t, txOpts{txType: types.LegacyTxType, to: &plainDest, value: big.NewInt(1)})
	_, err := c2.Classify(tx)
	require.Error(t, err)
	_, err = c2.Classify(tx)
	require.Error(t, err)
	require.Len(t, failing.reads, 2, "errors must not be memoised")
}
