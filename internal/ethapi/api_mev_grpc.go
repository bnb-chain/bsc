// Copyright 2026 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of MERCHANTABILITY
// or FITNESS FOR A PARTICULAR PURPOSE. See the GNU Lesser General Public License
// for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

package ethapi

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/ethereum/go-ethereum/common"
	buildertypes "github.com/ethereum/go-ethereum/core/types/builder"
	"github.com/ethereum/go-ethereum/core/types/builder/mevpb"
	"github.com/ethereum/go-ethereum/metrics"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/rpc"
)

const (
	mevErrorDomain          = "mev.bnbchain.org"
	jsonRPCDefaultErrorCode = -32000
	jsonRPCErrorDataKey     = "json_rpc_error_data"
)

var (
	grpcBidBlockRequests = metrics.NewRegisteredCounter("bidblock/grpc/requests", nil)
	grpcBidBlockErrors   = metrics.NewRegisteredCounter("bidblock/grpc/errors", nil)
	grpcBidBlockDecode   = metrics.NewRegisteredTimer("bidblock/grpc/decode", nil)
	grpcBidBlockHandler  = metrics.NewRegisteredTimer("bidblock/grpc/handler", nil)
	grpcBidBlockPayload  = metrics.NewRegisteredHistogram("bidblock/grpc/payload", nil, metrics.NewExpDecaySample(1028, 0.015))
	grpcBidBlockActive   = metrics.NewRegisteredGauge("bidblock/grpc/active", nil)
	grpcBidBlockRejected = metrics.NewRegisteredCounter("bidblock/grpc/rejected", nil)
)

// MevGRPCServer adapts the BidBlockService wire protocol to the existing MevAPI.
// All MEV business validation remains in MevAPI, EthAPIBackend and Miner.
type MevGRPCServer struct {
	mevpb.UnimplementedBidBlockServiceServer
	api mevBidBlockAPI
}

type mevBidBlockAPI interface {
	SendBidBlock(context.Context, buildertypes.BidBlockArgs) (common.Hash, error)
}

func newMevGRPCServer(backend Backend) *MevGRPCServer {
	return &MevGRPCServer{api: NewMevAPI(backend)}
}

// SendBidBlock decodes the transport payload and calls the same Go API used by
// JSON-RPC. ValidatorHostName belongs to sentry routing and is intentionally
// ignored by the validator.
func (s *MevGRPCServer) SendBidBlock(ctx context.Context, req *mevpb.BidBlockRequest) (*mevpb.BidBlockResponse, error) {
	grpcBidBlockRequests.Inc(1)
	start := time.Now()
	defer grpcBidBlockHandler.UpdateSince(start)
	if req != nil {
		grpcBidBlockPayload.Update(int64(len(req.BidBlockRlp)))
	}

	fail := func(err error) (*mevpb.BidBlockResponse, error) {
		grpcBidBlockErrors.Inc(1)
		return nil, toMevGRPCStatus(err)
	}
	if req == nil || len(req.BidBlockRlp) == 0 {
		return fail(buildertypes.NewInvalidBidError("empty BidBlock RLP"))
	}
	if err := ctx.Err(); err != nil {
		return fail(err)
	}

	var bidBlock buildertypes.BidBlock
	decodeStart := time.Now()
	err := rlp.DecodeBytes(req.BidBlockRlp, &bidBlock)
	grpcBidBlockDecode.UpdateSince(decodeStart)
	if err != nil {
		return fail(buildertypes.NewInvalidBidError("invalid BidBlock RLP"))
	}

	bidHash, err := s.api.SendBidBlock(ctx, buildertypes.BidBlockArgs{
		BidBlock:  &bidBlock,
		Signature: req.Signature,
	})
	if err != nil {
		return fail(err)
	}
	return &mevpb.BidBlockResponse{BidHash: bidHash.Bytes()}, nil
}

// toMevGRPCStatus maps stable MEV errors to gRPC status while preserving the
// message, JSON-RPC code and optional error data returned by the JSON endpoint.
func toMevGRPCStatus(err error) error {
	if err == nil {
		return nil
	}
	if st, ok := status.FromError(err); ok && st.Code() != codes.Unknown {
		return err
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return status.FromContextError(err).Err()
	}

	jsonRPCCode := jsonRPCDefaultErrorCode
	code := codes.Unknown
	var rpcErr rpc.Error
	if errors.As(err, &rpcErr) {
		jsonRPCCode = rpcErr.ErrorCode()
	}
	switch jsonRPCCode {
	case buildertypes.InvalidBidParamError, buildertypes.InvalidPayBidTxError,
		buildertypes.BidBlockPreSealVerifyError:
		code = codes.InvalidArgument
	case buildertypes.MevNotRunningError:
		code = codes.Unavailable
	case buildertypes.MevBusyError:
		code = codes.ResourceExhausted
	case buildertypes.MevNotInTurnError:
		code = codes.FailedPrecondition
	case buildertypes.BidBlockPermissionRevokedError:
		code = codes.PermissionDenied
	case buildertypes.BidBlockTooLateError:
		code = codes.DeadlineExceeded
	}

	st := status.New(code, err.Error())
	detail := &errdetails.ErrorInfo{
		Reason: strconv.Itoa(jsonRPCCode),
		Domain: mevErrorDomain,
	}
	var dataErr rpc.DataError
	if errors.As(err, &dataErr) {
		if encoded, encodeErr := json.Marshal(dataErr.ErrorData()); encodeErr == nil {
			detail.Metadata = map[string]string{jsonRPCErrorDataKey: string(encoded)}
		}
	}
	if detailed, detailErr := st.WithDetails(detail); detailErr == nil {
		st = detailed
	}
	return st.Err()
}
