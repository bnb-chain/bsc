package paymentlane

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"
)

// codeFn lets each test define its own live-state code lookup.
type codeFn struct {
	fn    func(common.Address) common.Hash
	reads []common.Address // every address actually read
}

func (r *codeFn) GetCodeHash(addr common.Address) common.Hash {
	r.reads = append(r.reads, addr)
	return r.fn(addr)
}

// forbidReads fails the test if the classifier touches state at all.
func forbidReads(t *testing.T) *codeFn {
	t.Helper()
	return &codeFn{fn: func(addr common.Address) common.Hash {
		t.Fatalf("classifier read state for %x, but every static gate should have decided first", addr)
		return common.Hash{}
	}}
}

// absentAccounts models the common "destination never seen" case: the zero hash, not EmptyCodeHash.
func absentAccounts() *codeFn {
	return &codeFn{fn: func(common.Address) common.Hash { return common.Hash{} }}
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

// Walk the static gates and assert both lane type and whether state was read.
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
						wantRead := dest.addr != nil && typeOK && !isListed && withValue

						var wantLaneType LaneType
						switch {
						case dest.addr == nil, !typeOK:
							wantLaneType = GeneralLane
						case isListed:
							wantLaneType = PaymentLane
						case !withValue:
							wantLaneType = GeneralLane
						default:
							wantLaneType = PaymentLane // reaches the code gate; the account is absent below
						}

						reader := absentAccounts()
						got := NewClassifier(NoSystemTxs, reader, listed).Classify(tx)
						require.Equal(t, wantLaneType, got,
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

// TestCodeGateFollowsTheLiveState: same destination, same transaction, opposite answers either
// side of the moment it gains code. That is what keeps a transfer to an address this very block
// deployed to, or delegated, from running code inside the quota. The code here arrives by an
// EIP-7702 authorisation; a deployment is the same flip, and TestCodeHashBoundaryCases covers
// both encodings.
func TestCodeGateFollowsTheLiveState(t *testing.T) {
	delegated := false
	designator := types.AddressToDelegation(common.HexToAddress("0x00000000000000000000000000000000000f0009"))
	r := &codeFn{fn: func(common.Address) common.Hash {
		if delegated {
			return common.BytesToHash(crypto.Keccak256(designator))
		}
		return common.Hash{}
	}}
	c := NewClassifier(NoSystemTxs, r, nil)
	tx := makeTx(t, txOpts{txType: types.LegacyTxType, to: &plainDest, value: big.NewInt(1)})

	require.Equal(t, PaymentLane, c.Classify(tx), "no code yet, so a plain transfer")
	delegated = true
	require.Equal(t, GeneralLane, c.Classify(tx),
		"the destination now holds code, so the transfer would execute it - not a payment")
	require.Len(t, r.reads, 2, "the code gate must be re-read every time; a memo here caches a stale answer")
}

// Cover every code-hash encoding the live state can return.
func TestCodeHashBoundaryCases(t *testing.T) {
	tx := makeTx(t, txOpts{txType: types.LegacyTxType, to: &plainDest, value: big.NewInt(1)})
	designator := types.AddressToDelegation(common.HexToAddress("0x00000000000000000000000000000000000f0009"))

	for _, tc := range []struct {
		name     string
		codeHash common.Hash
		want     LaneType
	}{
		// The trap: an account that does not exist reads as the ZERO hash, not EmptyCodeHash.
		{"absent account (zero hash)", common.Hash{}, PaymentLane},
		{"existing account, no code", types.EmptyCodeHash, PaymentLane},
		{"contract code hash", common.HexToHash("0xbeef"), GeneralLane},
		// EIP-7702 designators are code, so a delegated account is not a lane destination.
		{"eip-7702 designator", common.BytesToHash(crypto.Keccak256(designator)), GeneralLane},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := &codeFn{fn: func(common.Address) common.Hash { return tc.codeHash }}
			require.Equal(t, tc.want, NewClassifier(NoSystemTxs, r, nil).Classify(tx))
		})
	}
}

// The list lookup must short-circuit, so that no listed destination is decided by the live state.
func TestListedContractDecidesBeforeValueAndCode(t *testing.T) {
	// ERC-20 transfer(address,uint256) calldata, zero value.
	calldata := make([]byte, 68)
	copy(calldata, []byte{0xa9, 0x05, 0x9c, 0xbb})

	for _, txType := range []byte{types.LegacyTxType, types.AccessListTxType, types.DynamicFeeTxType} {
		tx := makeTx(t, txOpts{txType: txType, to: &listedDest, data: calldata})
		got := NewClassifier(NoSystemTxs, forbidReads(t), listedSet(listedDest)).Classify(tx)
		require.Equal(t, PaymentLane, got, "tx type %d", txType)
	}
}

// Blob and SetCode must stay general even to listed destinations.
func TestBlobAndSetCodeToListedContractAreGeneral(t *testing.T) {
	for _, txType := range []byte{types.BlobTxType, types.SetCodeTxType} {
		tx := makeTx(t, txOpts{txType: txType, to: &listedDest, value: big.NewInt(1)})
		got := NewClassifier(NoSystemTxs, forbidReads(t), listedSet(listedDest)).Classify(tx)
		require.Equal(t, GeneralLane, got, "tx type %d", txType)
	}
}

// systemTxs marks exactly the transactions in the set, the way the engine's own predicate does.
func systemTxs(hashes ...common.Hash) SystemTxOracle {
	set := make(map[common.Hash]struct{}, len(hashes))
	for _, h := range hashes {
		set[h] = struct{}{}
	}
	return func(tx *types.Transaction) bool {
		_, ok := set[tx.Hash()]
		return ok
	}
}

// BEP-703 3.2's first gate. A Parlia system transaction can look exactly like a payment - the
// deposit carries value - so without this gate consensus gas would be booked against the quota.
func TestSystemTransactionIsGeneralBeforeAnythingElse(t *testing.T) {
	// A bare transfer to a codeless account: payment by every other gate.
	tx := makeTx(t, txOpts{txType: types.LegacyTxType, to: &plainDest, value: big.NewInt(1)})
	require.Equal(t, PaymentLane, NewClassifier(NoSystemTxs, absentAccounts(), nil).Classify(tx),
		"premise: this transaction is a payment unless the system gate stops it")

	// forbidReads: the gate must decide before any state is touched.
	require.Equal(t, GeneralLane, NewClassifier(systemTxs(tx.Hash()), forbidReads(t), nil).Classify(tx))

	// And it must win over the listed-destination gate, which otherwise returns payment first.
	listedTx := makeTx(t, txOpts{txType: types.LegacyTxType, to: &listedDest, value: big.NewInt(1)})
	require.Equal(t, GeneralLane,
		NewClassifier(systemTxs(listedTx.Hash()), forbidReads(t), listedSet(listedDest)).Classify(listedTx))
}

func TestNewClassifierRefusesANilOracle(t *testing.T) {
	require.Panics(t, func() { NewClassifier(nil, absentAccounts(), nil) },
		"a silently absent system gate is the one failure that books consensus gas as payment")
}
