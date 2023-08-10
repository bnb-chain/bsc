package main

import (
	"encoding/json"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/log"
)

func (s SlashIndicatorVoteData) ToWrapper() *types.SlashIndicatorVoteDataWrapper {
	wrapper := &types.SlashIndicatorVoteDataWrapper{
		SrcNum: s.SrcNum,
		TarNum: s.TarNum,
	}

	if len(s.Sig) != types.BLSSignatureLength {
		log.Crit("wrong length of sig", "wanted", types.BLSSignatureLength, "get", len(s.Sig))
	}
	wrapper.SrcHash = common.Bytes2Hex(s.SrcHash[:])
	wrapper.TarHash = common.Bytes2Hex(s.TarHash[:])
	wrapper.Sig = common.Bytes2Hex(s.Sig)
	return wrapper
}

func (s *SlashIndicatorVoteData) FromWrapper(wrapper *types.SlashIndicatorVoteDataWrapper) {
	if len(wrapper.SrcHash) != common.HashLength*2 {
		log.Crit("wrong length of SrcHash", "wanted", common.HashLength*2, "get", len(wrapper.SrcHash))
	}
	if len(wrapper.TarHash) != common.HashLength*2 {
		log.Crit("wrong length of TarHash", "wanted", common.HashLength*2, "get", len(wrapper.TarHash))
	}
	if len(wrapper.Sig) != types.BLSSignatureLength*2 {
		log.Crit("wrong length of Sig", "wanted", types.BLSSignatureLength*2, "get", len(wrapper.Sig))
	}

	s.SrcNum = wrapper.SrcNum
	s.TarNum = wrapper.TarNum
	copy(s.SrcHash[:], common.Hex2Bytes(wrapper.SrcHash))
	copy(s.TarHash[:], common.Hex2Bytes(wrapper.TarHash))
	s.Sig = common.Hex2Bytes(wrapper.Sig)
}

func (s SlashIndicatorFinalityEvidence) MarshalJSON() ([]byte, error) {
	wrapper := &types.SlashIndicatorFinalityEvidenceWrapper{
		VoteA: *s.VoteA.ToWrapper(),
		VoteB: *s.VoteB.ToWrapper(),
	}

	if len(s.VoteAddr) != types.BLSPublicKeyLength {
		log.Crit("wrong length of VoteAddr", "wanted", types.BLSPublicKeyLength, "get", len(s.VoteAddr))
	}
	wrapper.VoteAddr = common.Bytes2Hex(s.VoteAddr)

	return json.Marshal(wrapper)
}

func (s *SlashIndicatorFinalityEvidence) UnmarshalJSON(data []byte) error {
	var wrapper = &types.SlashIndicatorFinalityEvidenceWrapper{}
	if err := json.Unmarshal(data, wrapper); err != nil {
		log.Crit("failed to Unmarshal", "error", err)
	}

	s.VoteA.FromWrapper(&wrapper.VoteA)
	s.VoteB.FromWrapper(&wrapper.VoteB)
	if len(wrapper.VoteAddr) != types.BLSPublicKeyLength*2 {
		log.Crit("wrong length of VoteAddr", "wanted", types.BLSPublicKeyLength*2, "get", len(wrapper.VoteAddr))
	}
	s.VoteAddr = common.Hex2Bytes(wrapper.VoteAddr)

	return nil
}
