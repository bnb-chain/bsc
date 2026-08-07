package paymentlane

import (
	"errors"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/systemcontracts"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/params"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"
)

// accountFn is an AccountReader backed by a function, so each test states exactly
// what the parent state looks like.
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

// absentAccounts is the common case: the destination has never been seen.
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

// txOpts describes a transaction along the axes the classifier actually looks at.
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

// TestNoStateReadUntilEveryStaticGatePasses walks the whole static gate space and
// asserts two things at once: which combinations reach the state read, and what
// class every combination produces.
//
// The first half is what keeps the tail of the packing loop cheap - a reordering
// that hoists the state read turns each rejected account into a trie query. The
// second half kills every gate-order mutation that changes a CLASS, which is all of
// the correctness-relevant ones. Two reorderings survive it and are meant to:
// swapping gates 3 and 4, or gates 6 and 7, changes only which free test runs first,
// so no observable behaviour differs.
func TestNoStateReadUntilEveryStaticGatePasses(t *testing.T) {
	oneEntryAccessList := types.AccessList{{Address: plainDest}}
	// ContractAddress is in the set on purpose: without a reserved address here,
	// swapping gates 4 and 5 leaves every expectation below unchanged and survives
	// this test. It stays general because gate 4 dominates.
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
			{"reserved", &ContractAddress},
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
						wantRead := dest.addr != nil && typeOK && !withAL &&
							dest.name != "reserved" && dest.name != "listed" && !withData && withValue

						var wantClass Class
						switch {
						case dest.addr == nil, !typeOK, withAL, dest.name == "reserved":
							wantClass = ClassGeneral
						case dest.name == "listed":
							wantClass = ClassPayment
						case withData, !withValue:
							wantClass = ClassGeneral
						default:
							wantClass = ClassPayment // reaches gate 8; the account is absent below
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

// TestReservedRangeIsExact walks the whole reserved range and its boundary, and
// cross-checks that every real precompile and system contract falls inside it -
// which is what makes it safe for the classifier to depend on an address range
// instead of on params.Rules and the fork-dependent precompile tables.
func TestReservedRangeIsExact(t *testing.T) {
	for n := 0; n <= maxReservedAddress; n++ {
		addr := common.BigToAddress(big.NewInt(int64(n)))
		require.True(t, isReserved(addr), "%x should be reserved", addr)
	}
	// One past the range, and every single-bit perturbation above the low two bytes.
	require.False(t, isReserved(common.BigToAddress(big.NewInt(maxReservedAddress+1))))
	for i := 0; i < common.AddressLength-2; i++ {
		for bit := 0; bit < 8; bit++ {
			var addr common.Address
			addr[i] = 1 << bit
			require.False(t, isReserved(addr), "byte %d bit %d must escape the range", i, bit)
		}
	}

	// Every precompile active under any fork rule set this tree can produce.
	for _, rules := range []params.Rules{
		{IsHomestead: true},
		{IsHomestead: true, IsByzantium: true},
		{IsHomestead: true, IsByzantium: true, IsIstanbul: true},
		{IsHomestead: true, IsByzantium: true, IsIstanbul: true, IsBerlin: true, IsCancun: true},
		{IsHomestead: true, IsByzantium: true, IsIstanbul: true, IsBerlin: true, IsCancun: true, IsPrague: true},
		{IsHomestead: true, IsByzantium: true, IsIstanbul: true, IsBerlin: true, IsCancun: true, IsPrague: true, IsOsaka: true},
		{IsHomestead: true, IsByzantium: true, IsIstanbul: true, IsBerlin: true, IsCancun: true, IsPrague: true, IsOsaka: true, IsInBSC: true},
		{IsPasteur: true},
		// Note vm.ActivePrecompiles has no IsUBT branch - unlike the unexported
		// activePrecompiledContracts - so an {IsUBT: true} row would silently
		// duplicate the Homestead set rather than test anything.
	} {
		for _, addr := range vm.ActivePrecompiles(rules) {
			require.True(t, isReserved(addr),
				"precompile %x is outside the reserved range: it would be classified payment and could exhaust the lane", addr)
		}
	}

	// And every Parlia system contract, so a leaked system transaction is general.
	for _, s := range []string{
		systemcontracts.ValidatorContract, systemcontracts.SlashContract, systemcontracts.SystemRewardContract,
		systemcontracts.LightClientContract, systemcontracts.TokenHubContract, systemcontracts.RelayerIncentivizeContract,
		systemcontracts.RelayerHubContract, systemcontracts.GovHubContract, systemcontracts.TokenManagerContract,
		systemcontracts.CrossChainContract, systemcontracts.StakingContract, systemcontracts.StakeHubContract,
		systemcontracts.StakeCreditContract, systemcontracts.GovernorContract, systemcontracts.GovTokenContract,
		systemcontracts.TimelockContract, systemcontracts.TokenRecoverPortalContract,
		systemcontracts.PaymentLaneContract,
	} {
		require.True(t, isReserved(common.HexToAddress(s)), "system contract %s is outside the reserved range", s)
	}
}

// TestAbsentAccountIsPayment guards the highest-value silent bug in the package.
//
// Writing gate 8 as codeHash == types.EmptyCodeHash instead misclassifies every
// transfer to a brand-new address - first deposits and new wallets, which is the
// lane's core use case. The symptom is not an error: it is a lane that quietly
// never fills, indistinguishable from low demand.
func TestAbsentAccountIsPayment(t *testing.T) {
	tx := makeTx(t, txOpts{txType: types.LegacyTxType, to: &plainDest, value: big.NewInt(1)})
	got, err := NewClassifier(common.Hash{}, absentAccounts(), nil).Classify(tx)
	require.NoError(t, err)
	require.Equal(t, ClassPayment, got)
}

// TestCodeHashBoundaryCases covers the three encodings of "no code" that exist in
// this tree. flatReader normalises an existing code-less account to EmptyCodeHash,
// mptTrieReader does not, and StateDB.GetCodeHash returns the zero hash for an
// absent account - so the same state can reach the classifier in different shapes.
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

// TestDelegationDesignatorIsGeneral covers EIP-7702. A delegated account's code
// hash is keccak(0xef0100||target), so gate 8 excludes it without any special
// case - and the AccountReader interface has no code accessor, so an
// implementation cannot be tempted to follow the delegation instead.
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

// TestListedContractSurvivesDataAndValueGates is the test that keeps the whitelist
// from becoming dead code. Every real ERC-20 transfer has calldata and zero value,
// so hoisting either of those gates above the whitelist lookup silently disables
// the entire second and third BEP-703 categories, with no error and no other
// failing test.
func TestListedContractSurvivesDataAndValueGates(t *testing.T) {
	// transfer(address,uint256) with a recipient and an amount: 68 bytes, no value.
	calldata := make([]byte, 68)
	copy(calldata, []byte{0xa9, 0x05, 0x9c, 0xbb})

	for _, txType := range []byte{types.LegacyTxType, types.AccessListTxType, types.DynamicFeeTxType} {
		tx := makeTx(t, txOpts{txType: txType, to: &listedDest, data: calldata})
		got, err := NewClassifier(common.Hash{}, forbidReads(t), listedSet(listedDest)).Classify(tx)
		require.NoError(t, err)
		require.Equal(t, ClassPayment, got, "tx type %d", txType)
	}
}

// TestBlobAndSetCodeToListedContractAreGeneral keeps the type whitelist above the
// membership lookup. Otherwise the type gate can be bypassed through the listed
// path, which re-opens 7702 bulk authorisation - 8.02M gas of pure state writes -
// at lane price.
func TestBlobAndSetCodeToListedContractAreGeneral(t *testing.T) {
	for _, txType := range []byte{types.BlobTxType, types.SetCodeTxType} {
		tx := makeTx(t, txOpts{txType: txType, to: &listedDest, value: big.NewInt(1)})
		got, err := NewClassifier(common.Hash{}, forbidReads(t), listedSet(listedDest)).Classify(tx)
		require.NoError(t, err)
		require.Equal(t, ClassGeneral, got, "tx type %d", txType)
	}
}

// TestReservedListingCannotBecomePayment documents why the classifier neither
// copies nor validates the caller's set: the reserved-range gate dominates the
// membership lookup, so one mis-governed listing cannot reclassify traffic to
// Parlia's own contracts.
func TestReservedListingCannotBecomePayment(t *testing.T) {
	timelock := common.HexToAddress(systemcontracts.TimelockContract)
	listed := listedSet(ContractAddress, timelock)
	for _, dest := range []common.Address{ContractAddress, timelock} {
		tx := makeTx(t, txOpts{txType: types.LegacyTxType, to: &dest, value: big.NewInt(1), data: []byte{0x01}})
		got, err := NewClassifier(common.Hash{}, forbidReads(t), listed).Classify(tx)
		require.NoError(t, err)
		require.Equal(t, ClassGeneral, got, "destination %x", dest)
	}
}

// TestTypeGateAcceptsExactlyThreeTypes pins the whitelist over every type this tree
// can construct.
//
// Types beyond 0x04 do not exist yet, so no test can construct one; they are
// covered structurally by the switch's default branch. That is precisely why the
// gate must stay a whitelist - a blacklist would compile, pass this test, and
// silently admit whatever type BSC adopts next.
func TestTypeGateAcceptsExactlyThreeTypes(t *testing.T) {
	for _, tc := range []struct {
		txType byte
		want   Class
	}{
		{types.LegacyTxType, ClassPayment},
		{types.AccessListTxType, ClassPayment},
		{types.DynamicFeeTxType, ClassPayment},
		{types.BlobTxType, ClassGeneral},
		{types.SetCodeTxType, ClassGeneral},
	} {
		tx := makeTx(t, txOpts{txType: tc.txType, to: &plainDest, value: big.NewInt(1)})
		got, err := NewClassifier(common.Hash{}, absentAccounts(), nil).Classify(tx)
		require.NoError(t, err)
		require.Equal(t, tc.want, got, "tx type %d", tc.txType)
	}
	require.Equal(t, byte(0x04), byte(types.SetCodeTxType),
		"a new transaction type has appeared: confirm the gate is still a whitelist and extend this table")
}

// TestErrorPropagatesFailShutAndSticks covers the failure path.
//
// Fail-shut must be ClassGeneral: a producer that ignored the error would
// under-fill the lane, costing revenue but still producing a valid block, whereas
// ClassPayment would shrink IdleLane, widen general headroom and over-pack into an
// invalid block.
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

	// A subsequent success must not clear the sticky error, or a caller that only
	// checks at the end would produce a block built on one bad classification.
	other := common.HexToAddress("0x00000000000000000000000000000000000f000a")
	got, err = c.Classify(makeTx(t, txOpts{txType: types.LegacyTxType, to: &other, value: big.NewInt(1)}))
	require.NoError(t, err)
	require.Equal(t, ClassPayment, got)
	require.Error(t, c.Err(), "Err must stay set after a later success")
	require.ErrorIs(t, c.Err(), boom)
}

// TestMemoDoesOneReadPerDistinctDestination pins both the memo and the decision not
// to memoise failures.
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

	// A failed read must not be cached: the retry has to reach the reader again.
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

// TestNilPaymentContractsMeansEmpty pins the contract with LoadPaymentContracts: nil is the
// activation-day state, not a failure signal.
func TestNilPaymentContractsMeansEmpty(t *testing.T) {
	tx := makeTx(t, txOpts{txType: types.LegacyTxType, to: &listedDest, data: []byte{0x01}})
	got, err := NewClassifier(common.Hash{}, forbidReads(t), nil).Classify(tx)
	require.NoError(t, err)
	require.Equal(t, ClassGeneral, got)
}

// TestCodedDestinationIsGeneral is the plain contract-call case.
func TestCodedDestinationIsGeneral(t *testing.T) {
	tx := makeTx(t, txOpts{txType: types.LegacyTxType, to: &plainDest, value: big.NewInt(1)})
	got, err := NewClassifier(common.Hash{}, codedAccounts(), nil).Classify(tx)
	require.NoError(t, err)
	require.Equal(t, ClassGeneral, got)
}
