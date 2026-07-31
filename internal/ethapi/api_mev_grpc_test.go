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
	"errors"
	"math/big"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/status"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	buildertypes "github.com/ethereum/go-ethereum/core/types/builder"
	"github.com/ethereum/go-ethereum/internal/ethapi/mevpb"
	"github.com/ethereum/go-ethereum/rlp"
)

type testMevBidBlockAPI struct {
	called atomic.Bool
	args   buildertypes.BidBlockArgs
	hash   common.Hash
	err    error
}

func (a *testMevBidBlockAPI) SendBidBlock(_ context.Context, args buildertypes.BidBlockArgs) (common.Hash, error) {
	a.called.Store(true)
	a.args = args
	return a.hash, a.err
}

func testGRPCBidBlock(t *testing.T) (*buildertypes.BidBlock, []byte) {
	t.Helper()
	block := &buildertypes.BidBlock{
		Header: &types.Header{
			ParentHash: common.HexToHash("0x01"),
			Root:       common.HexToHash("0x02"),
			TxHash:     common.HexToHash("0x03"),
			Number:     big.NewInt(2),
			Difficulty: big.NewInt(1),
			GasLimit:   140_000_000,
			GasUsed:    21_000,
			Time:       1_800_000_000,
			BaseFee:    big.NewInt(1),
		},
		Transactions: []hexutil.Bytes{{0x01, 0x02, 0x03}},
	}
	encoded, err := rlp.EncodeToBytes(block)
	require.NoError(t, err)
	return block, encoded
}

func TestMevGRPCSendBidBlock(t *testing.T) {
	original, encoded := testGRPCBidBlock(t)
	requestCount := grpcBidBlockRequests.Snapshot().Count()
	wantHash := common.HexToHash("0x1234")
	api := &testMevBidBlockAPI{hash: wantHash}
	server := &MevGRPCServer{api: api}
	signature := []byte{0xaa, 0xbb}

	response, err := server.SendBidBlock(context.Background(), &mevpb.BidBlockRequest{
		BidBlockRlp:       encoded,
		Signature:         signature,
		ValidatorHostName: "ignored-by-validator",
	})
	require.NoError(t, err)
	require.Equal(t, wantHash.Bytes(), response.BidHash)
	require.True(t, api.called.Load())
	require.Equal(t, original.Header.Hash(), api.args.BidBlock.Header.Hash())
	require.Equal(t, original.Transactions, api.args.BidBlock.Transactions)
	require.Equal(t, signature, []byte(api.args.Signature))
	require.Equal(t, requestCount+1, grpcBidBlockRequests.Snapshot().Count())
}

func TestMevGRPCHandlerRejectsBeforeBusinessCall(t *testing.T) {
	tests := []struct {
		name string
		ctx  context.Context
		req  *mevpb.BidBlockRequest
		code codes.Code
	}{
		{name: "nil request", ctx: context.Background(), req: nil, code: codes.InvalidArgument},
		{name: "empty RLP", ctx: context.Background(), req: &mevpb.BidBlockRequest{}, code: codes.InvalidArgument},
		{name: "invalid RLP", ctx: context.Background(), req: &mevpb.BidBlockRequest{BidBlockRlp: []byte{0xff}}, code: codes.InvalidArgument},
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	tests = append(tests, struct {
		name string
		ctx  context.Context
		req  *mevpb.BidBlockRequest
		code codes.Code
	}{name: "canceled", ctx: canceled, req: &mevpb.BidBlockRequest{BidBlockRlp: []byte{0x01}}, code: codes.Canceled})

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			api := new(testMevBidBlockAPI)
			_, err := (&MevGRPCServer{api: api}).SendBidBlock(test.ctx, test.req)
			require.Equal(t, test.code, status.Code(err))
			require.False(t, api.called.Load())
		})
	}
}

func TestToMevGRPCStatus(t *testing.T) {
	tests := []struct {
		err  error
		code codes.Code
	}{
		{buildertypes.NewInvalidBidError("invalid"), codes.InvalidArgument},
		{buildertypes.NewBidBlockPreSealVerifyError("invalid"), codes.InvalidArgument},
		{buildertypes.ErrMevNotRunning, codes.Unavailable},
		{buildertypes.ErrMevBusy, codes.ResourceExhausted},
		{buildertypes.ErrMevNotInTurn, codes.FailedPrecondition},
		{buildertypes.NewBidBlockPermissionRevokedError("revoked"), codes.PermissionDenied},
		{buildertypes.NewBidBlockTooLateError("late"), codes.DeadlineExceeded},
		{context.Canceled, codes.Canceled},
		{context.DeadlineExceeded, codes.DeadlineExceeded},
		{errors.New("secret internal detail"), codes.Internal},
	}
	for _, test := range tests {
		mapped := toMevGRPCStatus(test.err)
		require.Equal(t, test.code, status.Code(mapped), "error: %v", test.err)
		if test.code == codes.Internal {
			require.Equal(t, "internal error", status.Convert(mapped).Message())
		}
	}

	mapped := toMevGRPCStatus(buildertypes.NewBidBlockTooLateError("late"))
	details := status.Convert(mapped).Details()
	require.Len(t, details, 1)
	info, ok := details[0].(*errdetails.ErrorInfo)
	require.True(t, ok)
	require.Equal(t, "-38008", info.Reason)
	require.Equal(t, mevErrorDomain, info.Domain)
}

type testBidBlockServiceServer struct {
	mevpb.UnimplementedBidBlockServiceServer
	handle func(context.Context, *mevpb.BidBlockRequest) (*mevpb.BidBlockResponse, error)
}

func (s *testBidBlockServiceServer) SendBidBlock(ctx context.Context, req *mevpb.BidBlockRequest) (*mevpb.BidBlockResponse, error) {
	return s.handle(ctx, req)
}

func startTestMevGRPCService(t *testing.T, concurrency uint32, handler mevpb.BidBlockServiceServer) (*MevGRPCService, *grpc.ClientConn) {
	return startTestMevGRPCServiceWithTimeout(t, concurrency, defaultMevGRPCRequestTimeout, handler)
}

func startTestMevGRPCServiceWithTimeout(t *testing.T, concurrency uint32, requestTimeout time.Duration, handler mevpb.BidBlockServiceServer) (*MevGRPCService, *grpc.ClientConn) {
	t.Helper()
	service := newMevGRPCService("127.0.0.1:0", concurrency, requestTimeout, handler)
	require.NoError(t, service.Start())
	connection, err := grpc.NewClient(service.Addr(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	t.Cleanup(func() {
		connection.Close()
		require.NoError(t, service.Stop())
	})
	return service, connection
}

func TestMevGRPCServiceHealthAndCall(t *testing.T) {
	handler := &testBidBlockServiceServer{handle: func(_ context.Context, _ *mevpb.BidBlockRequest) (*mevpb.BidBlockResponse, error) {
		return &mevpb.BidBlockResponse{BidHash: []byte{0x01}}, nil
	}}
	_, connection := startTestMevGRPCService(t, 2, handler)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	healthClient := healthpb.NewHealthClient(connection)
	for _, name := range []string{"", mevpb.BidBlockService_ServiceDesc.ServiceName} {
		response, err := healthClient.Check(ctx, &healthpb.HealthCheckRequest{Service: name})
		require.NoError(t, err)
		require.Equal(t, healthpb.HealthCheckResponse_SERVING, response.Status)
	}
	response, err := mevpb.NewBidBlockServiceClient(connection).SendBidBlock(ctx, &mevpb.BidBlockRequest{})
	require.NoError(t, err)
	require.Equal(t, []byte{0x01}, response.BidHash)
}

func TestMevGRPCConcurrencyRejectsImmediatelyAndHealthBypasses(t *testing.T) {
	const concurrency = uint32(64)
	active := grpcBidBlockActive.Snapshot().Value()
	rejected := grpcBidBlockRejected.Snapshot().Count()
	entered := make(chan struct{}, concurrency)
	release := make(chan struct{})
	handler := &testBidBlockServiceServer{handle: func(_ context.Context, _ *mevpb.BidBlockRequest) (*mevpb.BidBlockResponse, error) {
		entered <- struct{}{}
		<-release
		return &mevpb.BidBlockResponse{}, nil
	}}
	service, connection := startTestMevGRPCService(t, concurrency, handler)
	secondConnection, err := grpc.NewClient(service.Addr(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	t.Cleanup(func() { secondConnection.Close() })
	client := mevpb.NewBidBlockServiceClient(connection)
	secondClient := mevpb.NewBidBlockServiceClient(secondConnection)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	callsDone := make(chan error, concurrency)
	for range concurrency {
		go func() {
			_, err := client.SendBidBlock(ctx, &mevpb.BidBlockRequest{})
			callsDone <- err
		}()
	}
	for range concurrency {
		select {
		case <-entered:
		case <-ctx.Done():
			t.Fatal("configured concurrency was blocked by the HTTP/2 stream limit")
		}
	}
	require.Equal(t, active+int64(concurrency), grpcBidBlockActive.Snapshot().Value())

	start := time.Now()
	_, err = secondClient.SendBidBlock(ctx, &mevpb.BidBlockRequest{
		BidBlockRlp: make([]byte, maxMevGRPCMessageSize-1024),
	})
	require.Equal(t, codes.ResourceExhausted, status.Code(err))
	require.Less(t, time.Since(start), 500*time.Millisecond)
	require.Equal(t, rejected+1, grpcBidBlockRejected.Snapshot().Count())

	healthResponse, err := healthpb.NewHealthClient(secondConnection).Check(ctx, &healthpb.HealthCheckRequest{})
	require.NoError(t, err)
	require.Equal(t, healthpb.HealthCheckResponse_SERVING, healthResponse.Status)
	close(release)
	for range concurrency {
		require.NoError(t, <-callsDone)
	}
	require.Eventually(t, func() bool {
		return grpcBidBlockActive.Snapshot().Value() == active
	}, time.Second, time.Millisecond)
}

func TestMevGRPCServerDeadlineWithoutClientDeadline(t *testing.T) {
	handler := &testBidBlockServiceServer{handle: func(ctx context.Context, _ *mevpb.BidBlockRequest) (*mevpb.BidBlockResponse, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}}
	_, connection := startTestMevGRPCServiceWithTimeout(t, 1, 20*time.Millisecond, handler)

	start := time.Now()
	_, err := mevpb.NewBidBlockServiceClient(connection).SendBidBlock(context.Background(), &mevpb.BidBlockRequest{})
	require.Equal(t, codes.DeadlineExceeded, status.Code(err))
	require.Less(t, time.Since(start), time.Second)
}

func TestMevGRPCRejectsOversizedRequest(t *testing.T) {
	var called atomic.Bool
	handler := &testBidBlockServiceServer{handle: func(_ context.Context, _ *mevpb.BidBlockRequest) (*mevpb.BidBlockResponse, error) {
		called.Store(true)
		return &mevpb.BidBlockResponse{}, nil
	}}
	_, connection := startTestMevGRPCService(t, 1, handler)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := mevpb.NewBidBlockServiceClient(connection).SendBidBlock(ctx, &mevpb.BidBlockRequest{
		BidBlockRlp: make([]byte, maxMevGRPCMessageSize),
	})
	require.Equal(t, codes.ResourceExhausted, status.Code(err))
	require.False(t, called.Load())
}
