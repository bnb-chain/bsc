package eth

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/ethereum/go-ethereum/internal/ethapi"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/rpc"
)

type sentryProxy struct {
	minerClient  *rpc.Client
	relayClients []*rpc.Client
}

func logProxyingError(logName string, args any, result any, err error) {
	logCtx := make([]any, 0)

	if err != nil {
		logCtx = append(logCtx, "err", err)
	}

	if args != nil {
		argsBytes, argsMarshalErr := json.Marshal(args)
		if argsMarshalErr != nil {
			logCtx = append(logCtx, "argsMarshalErr", argsMarshalErr)
		} else {
			logCtx = append(logCtx, "args", string(argsBytes))
		}
	}

	if result != nil {
		resultBytes, resultMarshalErr := json.Marshal(result)
		if resultMarshalErr != nil {
			logCtx = append(logCtx, "resultMarshalErr", resultMarshalErr)
		} else {
			logCtx = append(logCtx, "result", string(resultBytes))
		}
	}

	log.Error(logName, logCtx...)
}

// RegisterValidator register a validator
func (p *sentryProxy) RegisterValidator(ctx context.Context, args *ethapi.RegisterValidatorArgs) error {
	if len(p.relayClients) == 0 {
		logProxyingError("No relay clients", args, nil, nil)

		return nil
	}

	errs := make([]error, 0, len(p.relayClients))

	for _, relayClient := range p.relayClients {
		var result any

		err := relayClient.CallContext(ctx, &result, "eth_registerValidator", args)
		if err == nil {
			continue
		}

		errs = append(errs, err)

		logProxyingError("Failed to register validator", args, result, err)
	}

	if len(errs) == 0 {
		return nil
	}

	var combinedErr error

	for _, err := range errs {
		combinedErr = fmt.Errorf("%w: %v", combinedErr, err)
	}

	return combinedErr
}

// ProposedBlock add the block to the list of works
func (p *sentryProxy) ProposedBlock(ctx context.Context, args *ethapi.ProposedBlockArgs) error {
	noPayloadArgs := *args
	noPayloadArgs.Payload = nil

	if p.minerClient == nil {
		logProxyingError("No miner client", noPayloadArgs, nil, nil)

		return nil
	}

	var result any

	err := p.minerClient.CallContext(ctx, &result, "eth_proposedBlock", args)
	if err == nil {
		return nil
	}

	logProxyingError("Failed to propose block to validator", noPayloadArgs, result, err)

	return err
}
