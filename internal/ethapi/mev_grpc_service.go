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
	"fmt"
	"math"
	"net"
	"runtime/debug"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/tap"

	"github.com/ethereum/go-ethereum/core/types/builder/mevpb"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/params"
)

const (
	maxMevGRPCMessageSize        = 2 * params.MaxBlockSize
	mevGRPCStreamHeadroom        = uint32(16)
	defaultMevGRPCConcurrency    = uint32(32)
	defaultMevGRPCRequestTimeout = 10 * time.Second
	mevGRPCShutdownTimeout       = 15 * time.Second
)

func mevGRPCConcurrentStreams(concurrency uint32) uint32 {
	if concurrency > math.MaxUint32-mevGRPCStreamHeadroom {
		return math.MaxUint32
	}
	return concurrency + mevGRPCStreamHeadroom
}

// MevGRPCService owns the independent BidBlockService listener and its lifecycle.
type MevGRPCService struct {
	listenAddr     string
	concurrency    uint32
	requestTimeout time.Duration
	handler        mevpb.BidBlockServiceServer

	mu        sync.Mutex
	server    *grpc.Server
	health    *health.Server
	listener  net.Listener
	serveDone chan struct{}
}

// NewMevGRPCService creates a stopped service. An empty address is not valid;
// callers should only register the lifecycle when gRPC is configured.
func NewMevGRPCService(listenAddr string, concurrency uint32, requestTimeout time.Duration, backend Backend) *MevGRPCService {
	return newMevGRPCService(listenAddr, concurrency, requestTimeout, newMevGRPCServer(backend))
}

func newMevGRPCService(listenAddr string, concurrency uint32, requestTimeout time.Duration, handler mevpb.BidBlockServiceServer) *MevGRPCService {
	if concurrency == 0 {
		concurrency = defaultMevGRPCConcurrency
	}
	if requestTimeout <= 0 {
		requestTimeout = defaultMevGRPCRequestTimeout
	}
	return &MevGRPCService{listenAddr: listenAddr, concurrency: concurrency, requestTimeout: requestTimeout, handler: handler}
}

// Start binds synchronously so address conflicts fail node startup.
func (s *MevGRPCService) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.server != nil {
		return errors.New("MEV gRPC service already started")
	}
	if s.listenAddr == "" {
		return errors.New("empty MEV gRPC listen address")
	}

	listener, err := net.Listen("tcp", s.listenAddr)
	if err != nil {
		return fmt.Errorf("listen for MEV gRPC on %s: %w", s.listenAddr, err)
	}
	sem := make(chan struct{}, s.concurrency)
	maxConcurrentStreams := mevGRPCConcurrentStreams(s.concurrency)
	server := grpc.NewServer(
		grpc.MaxRecvMsgSize(maxMevGRPCMessageSize),
		grpc.MaxSendMsgSize(maxMevGRPCMessageSize),
		grpc.MaxConcurrentStreams(maxConcurrentStreams),
		grpc.InTapHandle(mevGRPCAdmission(sem, s.requestTimeout)),
		grpc.ChainUnaryInterceptor(mevGRPCRecoveryInterceptor),
	)
	mevpb.RegisterBidBlockServiceServer(server, s.handler)
	healthServer := health.NewServer()
	healthpb.RegisterHealthServer(server, healthServer)

	done := make(chan struct{})
	s.server = server
	s.health = healthServer
	s.listener = listener
	s.serveDone = done

	go func() {
		defer close(done)
		if err := server.Serve(listener); err != nil && !errors.Is(err, grpc.ErrServerStopped) {
			healthServer.Shutdown()
			log.Error("MEV gRPC server stopped unexpectedly", "err", err)
		}
	}()
	healthServer.SetServingStatus("", healthpb.HealthCheckResponse_SERVING)
	healthServer.SetServingStatus(mevpb.BidBlockService_ServiceDesc.ServiceName, healthpb.HealthCheckResponse_SERVING)
	log.Info("MEV gRPC server started", "addr", listener.Addr(), "concurrency", s.concurrency,
		"maxStreams", maxConcurrentStreams, "requestTimeout", s.requestTimeout)
	return nil
}

// Stop marks the service unhealthy, drains in-flight calls, then forces a stop
// after a bounded timeout. It is safe to call more than once.
func (s *MevGRPCService) Stop() error {
	s.mu.Lock()
	server, healthServer, done := s.server, s.health, s.serveDone
	if server == nil {
		s.mu.Unlock()
		return nil
	}
	s.server, s.health, s.listener, s.serveDone = nil, nil, nil, nil
	s.mu.Unlock()

	healthServer.Shutdown()
	gracefulDone := make(chan struct{})
	go func() {
		server.GracefulStop()
		close(gracefulDone)
	}()
	select {
	case <-gracefulDone:
	case <-time.After(mevGRPCShutdownTimeout):
		server.Stop()
		<-gracefulDone
	}
	<-done
	return nil
}

// Addr returns the bound address after Start and the configured address before
// Start. It is intended for logs and tests.
func (s *MevGRPCService) Addr() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.listener != nil {
		return s.listener.Addr().String()
	}
	return s.listenAddr
}

func mevGRPCRecoveryInterceptor(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp any, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			log.Error("Panic in MEV gRPC handler", "method", info.FullMethod, "panic", recovered, "stack", string(debug.Stack()))
			grpcBidBlockErrors.Inc(1)
			err = status.Error(codes.Internal, "internal error")
		}
	}()
	return handler(ctx, req)
}

// mevGRPCAdmission bounds BidBlock requests before protobuf decoding.
// It also applies the server deadline to body upload; health bypasses both.
func mevGRPCAdmission(sem chan struct{}, timeout time.Duration) tap.ServerInHandle {
	if timeout <= 0 {
		timeout = defaultMevGRPCRequestTimeout
	}
	return func(ctx context.Context, info *tap.Info) (context.Context, error) {
		if info.FullMethodName != mevpb.BidBlockService_SendBidBlock_FullMethodName {
			return ctx, nil
		}
		select {
		case sem <- struct{}{}:
			grpcBidBlockActive.Inc(1)
		default:
			grpcBidBlockRejected.Inc(1)
			return ctx, status.Error(codes.ResourceExhausted, "concurrency limit reached")
		}
		ctx, cancel := context.WithTimeout(ctx, timeout)
		context.AfterFunc(ctx, func() {
			cancel()
			grpcBidBlockActive.Dec(1)
			<-sem
		})
		return ctx, nil
	}
}
