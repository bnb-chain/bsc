// Package v2 is used for tendermint v0.34.22 and its compatible version.
package v2

import (
	"bytes"
	"encoding/hex"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cometbft/cometbft/crypto/ed25519"
	tmproto "github.com/cometbft/cometbft/proto/tendermint/types"
	"github.com/cometbft/cometbft/types"
)

type validatorInfo struct {
	pubKey         string
	votingPower    int64
	relayerAddress string
	relayerBlsKey  string
}

var testcases = []struct {
	chainID              string
	height               uint64
	nextValidatorSetHash string
	vals                 []validatorInfo
	consensusStateBytes  string
}{
	{
		chainID:              "chain_9000-121",
		height:               1,
		nextValidatorSetHash: "0CE856B1DC9CDCF3BF2478291CF02C62AEEB3679889E9866931BF1FB05A10EDA",
		vals: []validatorInfo{
			{
				pubKey:         "c3d9a1082f42ca161402f8668f8e39ec9e30092affd8d3262267ac7e248a959e",
				votingPower:    int64(10000),
				relayerAddress: "B32d0723583040F3A16D1380D1e6AA874cD1bdF7",
				relayerBlsKey:  "a60afe627fd78b19e07e07e19d446009dd53a18c6c8744176a5d851a762bbb51198e7e006f2a6ea7225661a61ecd832d",
			},
		},
		consensusStateBytes: "636861696e5f393030302d31323100000000000000000000000000000000000000000000000000010ce856b1dc9cdcf3bf2478291cf02c62aeeb3679889e9866931bf1fb05a10edac3d9a1082f42ca161402f8668f8e39ec9e30092affd8d3262267ac7e248a959e0000000000002710b32d0723583040f3a16d1380d1e6aa874cd1bdf7a60afe627fd78b19e07e07e19d446009dd53a18c6c8744176a5d851a762bbb51198e7e006f2a6ea7225661a61ecd832d",
	},
	{
		chainID:              "chain_9000-121",
		height:               1,
		nextValidatorSetHash: "A5F1AF4874227F1CDBE5240259A365AD86484A4255BFD65E2A0222D733FCDBC3",
		vals: []validatorInfo{
			{
				pubKey:         "20cc466ee9412ddd49e0fff04cdb41bade2b7622f08b6bdacac94d4de03bdb97",
				votingPower:    int64(10000),
				relayerAddress: "d5e63aeee6e6fa122a6a23a6e0fca87701ba1541",
				relayerBlsKey:  "aa2d28cbcd1ea3a63479f6fb260a3d755853e6a78cfa6252584fee97b2ec84a9d572ee4a5d3bc1558bb98a4b370fb861",
			},
			{
				pubKey:         "6b0b523ee91ad18a63d63f21e0c40a83ef15963f4260574ca5159fd90a1c5270",
				votingPower:    int64(10000),
				relayerAddress: "6fd1ceb5a48579f322605220d4325bd9ff90d5fa",
				relayerBlsKey:  "b31e74a881fc78681e3dfa440978d2b8be0708a1cbbca2c660866216975fdaf0e9038d9b7ccbf9731f43956dba7f2451",
			},
			{
				pubKey:         "919606ae20bf5d248ee353821754bcdb456fd3950618fda3e32d3d0fb990eeda",
				votingPower:    int64(10000),
				relayerAddress: "97376a436bbf54e0f6949b57aa821a90a749920a",
				relayerBlsKey:  "b32979580ea04984a2be033599c20c7a0c9a8d121b57f94ee05f5eda5b36c38f6e354c89328b92cdd1de33b64d3a0867",
			},
		},
		consensusStateBytes: "636861696e5f393030302d3132310000000000000000000000000000000000000000000000000001a5f1af4874227f1cdbe5240259a365ad86484a4255bfd65e2a0222d733fcdbc320cc466ee9412ddd49e0fff04cdb41bade2b7622f08b6bdacac94d4de03bdb970000000000002710d5e63aeee6e6fa122a6a23a6e0fca87701ba1541aa2d28cbcd1ea3a63479f6fb260a3d755853e6a78cfa6252584fee97b2ec84a9d572ee4a5d3bc1558bb98a4b370fb8616b0b523ee91ad18a63d63f21e0c40a83ef15963f4260574ca5159fd90a1c527000000000000027106fd1ceb5a48579f322605220d4325bd9ff90d5fab31e74a881fc78681e3dfa440978d2b8be0708a1cbbca2c660866216975fdaf0e9038d9b7ccbf9731f43956dba7f2451919606ae20bf5d248ee353821754bcdb456fd3950618fda3e32d3d0fb990eeda000000000000271097376a436bbf54e0f6949b57aa821a90a749920ab32979580ea04984a2be033599c20c7a0c9a8d121b57f94ee05f5eda5b36c38f6e354c89328b92cdd1de33b64d3a0867",
	},
}

func TestEncodeConsensusState(t *testing.T) {
	for i := 0; i < len(testcases); i++ {
		testcase := testcases[i]

		var validatorSet []*types.Validator

		for j := 0; j < len(testcase.vals); j++ {
			valInfo := testcase.vals[j]

			pubKeyBytes, err := hex.DecodeString(valInfo.pubKey)
			require.NoError(t, err)
			relayerAddress, err := hex.DecodeString(valInfo.relayerAddress)
			require.NoError(t, err)
			relayerBlsKey, err := hex.DecodeString(valInfo.relayerBlsKey)
			require.NoError(t, err)

			pubkey := ed25519.PubKey(make([]byte, ed25519.PubKeySize))
			copy(pubkey[:], pubKeyBytes)
			validator := types.NewValidator(pubkey, valInfo.votingPower)
			validator.SetRelayerAddress(relayerAddress)
			validator.SetBlsKey(relayerBlsKey)
			validatorSet = append(validatorSet, validator)
		}

		nextValidatorHash, err := hex.DecodeString(testcase.nextValidatorSetHash)
		require.NoError(t, err)

		consensusState := ConsensusState{
			ChainID:              testcase.chainID,
			Height:               testcase.height,
			NextValidatorSetHash: nextValidatorHash,
			ValidatorSet: &types.ValidatorSet{
				Validators: validatorSet,
			},
		}

		csBytes, err := consensusState.EncodeConsensusState()
		require.NoError(t, err)

		expectCsBytes, err := hex.DecodeString(testcase.consensusStateBytes)
		require.NoError(t, err)

		if !bytes.Equal(csBytes, expectCsBytes) {
			t.Fatalf("Consensus state mimatch, expect: %s, real:%s\n", testcase.consensusStateBytes, hex.EncodeToString(csBytes))
		}
	}
}

func TestDecodeConsensusState(t *testing.T) {
	for i := 0; i < len(testcases); i++ {
		testcase := testcases[i]

		csBytes, err := hex.DecodeString(testcase.consensusStateBytes)
		require.NoError(t, err)

		cs, err := DecodeConsensusState(csBytes)
		require.NoError(t, err)

		if cs.ChainID != testcase.chainID {
			t.Fatalf("Chain ID mimatch, expect: %s, real:%s\n", testcase.chainID, cs.ChainID)
		}

		if cs.Height != testcase.height {
			t.Fatalf("Height mimatch, expect: %d, real:%d\n", testcase.height, cs.Height)
		}

		nextValidatorSetHashBytes, err := hex.DecodeString(testcase.nextValidatorSetHash)
		if err != nil {
			t.Fatalf("Decode next validator set hash failed: %v\n", err)
		}

		if !bytes.Equal(cs.NextValidatorSetHash, nextValidatorSetHashBytes) {
			t.Fatalf("Next validator set hash mimatch, expect: %s, real:%s\n", testcase.nextValidatorSetHash, hex.EncodeToString(cs.NextValidatorSetHash))
		}
	}
}

func TestConsensusStateApplyLightBlock(t *testing.T) {
	csBytes, err := hex.DecodeString("677265656e6669656c645f373937312d310000000000000000000000000000000000000000000001b0bc20528caf73009a278af04e6d565cd31df93711517c2b7ac6efa4bf7eeb5a9656f518c3169bb921e49dde966d43cc0aebca5232d6cf85385f8ffad270d54b00000000000003e82edd53b48726a887c98adab97e0a8600f855570da22249e548ef1829660521d3ab4496ebee89781f2b91cc237b36caef014f6779ba82e5c784515a9e3702ad356f5bbee1b3b66156ff2463eb82b74f60f6b2c7396dffa8727552af39b5ee5c32440b796300000000000003e8115e247d2771f08fdd94c16ac01381082ebc73d184ed5dc9551e8e44fce09dc30272156fd70a094cc5e9f1d69fabd04630d8961036b9497a563d1de9f7b98a7d2a17841997328a8ace9a8722ec6f9232075425672bb5590b8a1a68a104fe072e30e52ff300000000000003e8a4a2957e858529ffabbbb483d1d704378a9fca6ba5e140ee80a0ff1552a954701f599622adf029916f55b3157a649e16086a0669900f784d03bff79e69eb8eb7ccfd77d84629ce60903edab13f06cfadad4c6646e3a2435c85ad7cbdd1966805400174a000000000000003e84038993e087832d84e2ac855d27f6b0b2eec1907ad10ab912fcf510dfca1d27ab8a3d2a8b197e09bd69cd0e80e960d653e656073e89d417cf6f22de627710ccab352e6c2eba438da90262e1a66869d394498eaf79b9a3c8a2ed1fe902e63ae2ca9c32b5400000000000003e82bbe5c8e5c3eb2b35063b330749f1958206d2ec29762663048cd982ae30da530ed6d0262e8adaafe6da99c9133f8ae2182c5776f35cd8e6a722a3306aa43b543907fca68")
	require.NoError(t, err)
	blockBytes, err := hex.DecodeString("0ab8080ad9030a02080b1211677265656e6669656c645f373937312d311802220c088b81faa0061084cbb080032a480a20c4d48ad5647bfb3c8f2fcd1fd5ed011078f8a5f82c8300d367b45dd3e71e69601224080112205cd4ab624399543ebbbef1cfe3d8abdeab5d90baec79313e4b2b037bb00026653220b456814a4622350736868828f18fd79f63f4fc52b5bc31683871d3128feead203a20e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b8554220b0bc20528caf73009a278af04e6d565cd31df93711517c2b7ac6efa4bf7eeb5a4a20b0bc20528caf73009a278af04e6d565cd31df93711517c2b7ac6efa4bf7eeb5a5220048091bc7ddc283f77bfbf91d73c44da58c3df8a9cbc867405d8b7f3daada22f5a204e6a6e2da7f1d4ae79f25716974c2e63c6bd230b56395ab43b31495dc63a76e36220e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b8556a20e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b85572147fd7b18968785cd71031efcfdca743bb01327c557a400fbad78f30ae7713175d5370ab13725227aa0ec8d68cef6c4c93f54398f563dda497a9bc3062282e1472db41e699ca7c53f7cc0b90cee7c0274c532439f9100b12d90408021a480a20d24290f854bc735e29eddd497dae06d31c2a609dd495d9ba89c0e6e3bad39f2f12240801122010d44c33f63f1e69a093f6beb4a8792bc8d3c0e6afe4a0531db728664035b26a22670802121451b77a1dbacf21d07543f2b1058c7ff1cbc570eb1a0b089181faa00610d6d29b5c224010393eb93cc3188de492131d61b5e966f92052c50441af74d6e33de2156613717e87b3dec2e636d69e38f1adaa5ac99fda6bd91533abcb7410f6896f97a0db032267080212147fd7b18968785cd71031efcfdca743bb01327c551a0b089181faa00610fd87fa5c2240fc6d376650f272af7900b1a434bc9cb1cd85005b4ed82fbef4890a904de4405f63901d34103a18e8271cb1b3edbf3a97169a328365aa4d74b34526c6dac3400a226708021214c6afa4d60d28087890b9f12df08627d3a2ef1e6a1a0b089181faa006108a83905c22404de9c95b4458cafd275e37908becb3fb25a9ba33f01047baac4b463554a2301d8024a471446c118a09b60bea091042ac2258c280ae97781d997d8f1af52d700a226708021214cd4e22cf636e5b643fbf7b58ea57f0c2e84c00601a0b089181faa00610ccaae75c224014b1d14a2bf525143c653103a72a32a45a26640dfaf6dae9cd8cda3d363801c43dd7dfc936773dd9f881e0a71aa09f0a7df46c129627f2d44fdf5a30e0e82903226708021214d6dcbdffd247d1e6dcac721adea95146ace60b861a0b089181faa00610ebcd8c5c2240c367e0601863958a92571a69aeff697824632fbd4d6e9c3b7c373a18dea2736ba1c17f01e44675f7fe979d8fdd1d69c70c234a6158249a7bfa4bbe333818ec0812d6070aa6010a1451b77a1dbacf21d07543f2b1058c7ff1cbc570eb12220a209656f518c3169bb921e49dde966d43cc0aebca5232d6cf85385f8ffad270d54b18e80720e0e0ffffffffffffff012a30a22249e548ef1829660521d3ab4496ebee89781f2b91cc237b36caef014f6779ba82e5c784515a9e3702ad356f5bbee132142edd53b48726a887c98adab97e0a8600f855570d3a144605f360473ae8ffa6d745571e790657b11420710a9e010a147fd7b18968785cd71031efcfdca743bb01327c5512220a20b3b66156ff2463eb82b74f60f6b2c7396dffa8727552af39b5ee5c32440b796318e80720e8072a3084ed5dc9551e8e44fce09dc30272156fd70a094cc5e9f1d69fabd04630d8961036b9497a563d1de9f7b98a7d2a1784193214115e247d2771f08fdd94c16ac01381082ebc73d13a14909f70e60489dc0b529c3ab2c5e7a30250f1e84a0a9e010a14c6afa4d60d28087890b9f12df08627d3a2ef1e6a12220a2097328a8ace9a8722ec6f9232075425672bb5590b8a1a68a104fe072e30e52ff318e80720e8072a30a5e140ee80a0ff1552a954701f599622adf029916f55b3157a649e16086a0669900f784d03bff79e69eb8eb7ccfd77d83214a4a2957e858529ffabbbb483d1d704378a9fca6b3a14006880c1b901cba2d501678999ca7feee0e6ee5a0a9e010a14cd4e22cf636e5b643fbf7b58ea57f0c2e84c006012220a204629ce60903edab13f06cfadad4c6646e3a2435c85ad7cbdd1966805400174a018e80720e8072a30ad10ab912fcf510dfca1d27ab8a3d2a8b197e09bd69cd0e80e960d653e656073e89d417cf6f22de627710ccab352e6c232144038993e087832d84e2ac855d27f6b0b2eec19073a14625b346ca83d47615eeccfe643a51889fdf21b380a9e010a14d6dcbdffd247d1e6dcac721adea95146ace60b8612220a20eba438da90262e1a66869d394498eaf79b9a3c8a2ed1fe902e63ae2ca9c32b5418e80720e8072a309762663048cd982ae30da530ed6d0262e8adaafe6da99c9133f8ae2182c5776f35cd8e6a722a3306aa43b543907fca6832142bbe5c8e5c3eb2b35063b330749f1958206d2ec23a14b85bf907ebe40ec502805ffd3132989e2df729b712a6010a1451b77a1dbacf21d07543f2b1058c7ff1cbc570eb12220a209656f518c3169bb921e49dde966d43cc0aebca5232d6cf85385f8ffad270d54b18e80720e0e0ffffffffffffff012a30a22249e548ef1829660521d3ab4496ebee89781f2b91cc237b36caef014f6779ba82e5c784515a9e3702ad356f5bbee132142edd53b48726a887c98adab97e0a8600f855570d3a144605f360473ae8ffa6d745571e790657b1142071")
	require.NoError(t, err)

	var lbpb tmproto.LightBlock
	err = lbpb.Unmarshal(blockBytes)
	require.NoError(t, err)
	block, err := types.LightBlockFromProto(&lbpb)
	require.NoError(t, err)

	cs, err := DecodeConsensusState(csBytes)
	require.NoError(t, err)
	validatorSetChanged, err := cs.ApplyLightBlock(block)
	require.NoError(t, err)

	if cs.Height != 2 {
		t.Fatalf("Height is unexpected, expected: 2, actual: %d\n", cs.Height)
	}

	if !validatorSetChanged {
		t.Fatalf("Validator set has exchanaged which is not expected.\n")
	}
}
