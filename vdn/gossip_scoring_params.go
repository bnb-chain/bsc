package vdn

import (
	"math"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/log"

	"github.com/pkg/errors"

	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/peer"
)

const (
	// decayToZero specifies the terminal value that we will use when decaying
	// a value.
	decayToZero = 0.01

	// a bool to check if we enable scoring for messages in the mesh sent for near first deliveries.
	meshDeliveryIsScored = false

	// beaconBlockWeight specifies the scoring weight that we apply to
	// our beacon block topic.
	beaconBlockWeight = 0.8

	// aggregateWeight specifies the scoring weight that we apply to
	// our aggregate topic.
	aggregateWeight = 0.5

	defaultTopicWeight = 0.2

	// maxInMeshScore describes the max score a peer can attain from being in the mesh.
	maxInMeshScore = 10

	// maxFirstDeliveryScore describes the max score a peer can obtain from first deliveries.
	maxFirstDeliveryScore = 40

	// dampeningFactor reduces the amount by which the various thresholds and caps are created.
	dampeningFactor = 90
)

func peerScoringParams() (*pubsub.PeerScoreParams, *pubsub.PeerScoreThresholds) {
	thresholds := &pubsub.PeerScoreThresholds{
		GossipThreshold:             -4000,
		PublishThreshold:            -8000,
		GraylistThreshold:           -16000,
		AcceptPXThreshold:           100,
		OpportunisticGraftThreshold: 5,
	}
	scoreParams := &pubsub.PeerScoreParams{
		Topics:        make(map[string]*pubsub.TopicScoreParams),
		TopicScoreCap: 32.72,
		AppSpecificScore: func(p peer.ID) float64 {
			return 0
		},
		AppSpecificWeight:           1,
		IPColocationFactorWeight:    -35.11,
		IPColocationFactorThreshold: 10,
		IPColocationFactorWhitelist: nil,
		BehaviourPenaltyWeight:      -15.92,
		BehaviourPenaltyThreshold:   6,
		BehaviourPenaltyDecay:       scoreDecay(10 * oneEpochDuration()),
		DecayInterval:               2 * oneSlotDuration(),
		DecayToZero:                 decayToZero,
		RetainScore:                 100 * oneEpochDuration(),
	}
	return scoreParams, thresholds
}

func (s *Server) topicScoreParams(topic string) (*pubsub.TopicScoreParams, error) {
	switch {
	case strings.Contains(topic, BlockMsgSuffix):
		return defaultBlockTopicParams(), nil
	case strings.Contains(topic, VoteMsgSuffix):
		// TODO(galaio): set correct validator size
		return defaultVoteTopicParams(21), nil
	default:
		return defaultTopicParams(), nil
	}
}

// Based on the lighthouse beacon block parameters.
// https://gist.github.com/blacktemplar/5c1862cb3f0e32a1a7fb0b25e79e6e2c
func defaultBlockTopicParams() *pubsub.TopicScoreParams {
	decayEpoch := 5
	decayEpochDuration := 5 * oneEpochDuration()
	blocksInEpoch := blocksPerEpoch()
	meshWeight := -0.717
	invalidDecayPeriod := 50 * oneEpochDuration()
	if !meshDeliveryIsScored {
		// Set the mesh weight as zero as a temporary measure, so as to prevent
		// the average nodes from being penalised.
		meshWeight = 0
	}
	return &pubsub.TopicScoreParams{
		TopicWeight:                     beaconBlockWeight,
		TimeInMeshWeight:                maxInMeshScore / inMeshCap(),
		TimeInMeshQuantum:               inMeshTime(),
		TimeInMeshCap:                   inMeshCap(),
		FirstMessageDeliveriesWeight:    1,
		FirstMessageDeliveriesDecay:     scoreDecay(20 * oneEpochDuration()),
		FirstMessageDeliveriesCap:       23,
		MeshMessageDeliveriesWeight:     meshWeight,
		MeshMessageDeliveriesDecay:      scoreDecay(decayEpochDuration),
		MeshMessageDeliveriesCap:        float64(blocksInEpoch * uint64(decayEpoch)),
		MeshMessageDeliveriesThreshold:  float64(blocksInEpoch*uint64(decayEpoch)) / 10,
		MeshMessageDeliveriesWindow:     2 * time.Second,
		MeshMessageDeliveriesActivation: 4 * oneEpochDuration(),
		MeshFailurePenaltyWeight:        meshWeight,
		MeshFailurePenaltyDecay:         scoreDecay(decayEpochDuration),
		InvalidMessageDeliveriesWeight:  -140.4475,
		InvalidMessageDeliveriesDecay:   scoreDecay(invalidDecayPeriod),
	}
}

// Based on the prysm AggregateTopic parameters.
func defaultVoteTopicParams(activeValidators uint64) *pubsub.TopicScoreParams {
	// Determine the expected message rate for the particular gossip topic.
	aggPerSlot := activeValidators
	firstMessageCap, err := decayLimit(scoreDecay(1*oneEpochDuration()), float64(aggPerSlot*2/gossipSubD))
	if err != nil {
		log.Warn("skipping initializing topic scoring", "err", err)
		return nil
	}

	invalidDecayPeriod := 50 * oneEpochDuration()
	firstMessageWeight := maxFirstDeliveryScore / firstMessageCap
	meshThreshold, err := decayThreshold(scoreDecay(1*oneEpochDuration()), float64(aggPerSlot)/dampeningFactor)
	if err != nil {
		log.Warn("skipping initializing topic scoring", "err", err)
		return nil
	}
	meshWeight := -scoreByWeight(aggregateWeight, meshThreshold)
	meshCap := 4 * meshThreshold
	if !meshDeliveryIsScored {
		// Set the mesh weight as zero as a temporary measure, so as to prevent
		// the average nodes from being penalised.
		meshWeight = 0
	}
	return &pubsub.TopicScoreParams{
		TopicWeight:                     aggregateWeight,
		TimeInMeshWeight:                maxInMeshScore / inMeshCap(),
		TimeInMeshQuantum:               inMeshTime(),
		TimeInMeshCap:                   inMeshCap(),
		FirstMessageDeliveriesWeight:    firstMessageWeight,
		FirstMessageDeliveriesDecay:     scoreDecay(1 * oneEpochDuration()),
		FirstMessageDeliveriesCap:       firstMessageCap,
		MeshMessageDeliveriesWeight:     meshWeight,
		MeshMessageDeliveriesDecay:      scoreDecay(1 * oneEpochDuration()),
		MeshMessageDeliveriesCap:        meshCap,
		MeshMessageDeliveriesThreshold:  meshThreshold,
		MeshMessageDeliveriesWindow:     2 * time.Second,
		MeshMessageDeliveriesActivation: 1 * oneEpochDuration(),
		MeshFailurePenaltyWeight:        meshWeight,
		MeshFailurePenaltyDecay:         scoreDecay(1 * oneEpochDuration()),
		InvalidMessageDeliveriesWeight:  -maxScore() / aggregateWeight,
		InvalidMessageDeliveriesDecay:   scoreDecay(invalidDecayPeriod),
	}
}

func defaultTopicParams() *pubsub.TopicScoreParams {
	decayEpoch := 5
	decayEpochDuration := 5 * oneEpochDuration()
	blocksInEpoch := blocksPerEpoch()
	meshWeight := -0.717
	invalidDecayPeriod := 50 * oneEpochDuration()
	if !meshDeliveryIsScored {
		// Set the mesh weight as zero as a temporary measure, so as to prevent
		// the average nodes from being penalised.
		meshWeight = 0
	}
	return &pubsub.TopicScoreParams{
		TopicWeight:                     defaultTopicWeight,
		TimeInMeshWeight:                maxInMeshScore / inMeshCap(),
		TimeInMeshQuantum:               inMeshTime(),
		TimeInMeshCap:                   inMeshCap(),
		FirstMessageDeliveriesWeight:    1,
		FirstMessageDeliveriesDecay:     scoreDecay(20 * oneEpochDuration()),
		FirstMessageDeliveriesCap:       23,
		MeshMessageDeliveriesWeight:     meshWeight,
		MeshMessageDeliveriesDecay:      scoreDecay(decayEpochDuration),
		MeshMessageDeliveriesCap:        float64(blocksInEpoch * uint64(decayEpoch)),
		MeshMessageDeliveriesThreshold:  float64(blocksInEpoch*uint64(decayEpoch)) / 10,
		MeshMessageDeliveriesWindow:     2 * time.Second,
		MeshMessageDeliveriesActivation: 4 * oneEpochDuration(),
		MeshFailurePenaltyWeight:        meshWeight,
		MeshFailurePenaltyDecay:         scoreDecay(decayEpochDuration),
		InvalidMessageDeliveriesWeight:  -140.4475,
		InvalidMessageDeliveriesDecay:   scoreDecay(invalidDecayPeriod),
	}
}

// determines the decay rate from the provided time period till
// the decayToZero value. Ex: ( 1 -> 0.01)
func scoreDecay(totalDurationDecay time.Duration) float64 {
	numOfTimes := totalDurationDecay / oneSlotDuration()
	return math.Pow(decayToZero, 1/float64(numOfTimes))
}

func oneSlotDuration() time.Duration {
	// TODO(galaio): block interval
	return 750 * time.Millisecond
}

func oneEpochDuration() time.Duration {
	// TODO(galaio): epoch length
	return time.Duration(blocksPerEpoch()) * oneSlotDuration()
}

func blocksPerEpoch() uint64 {
	// TODO(galaio): epoch length
	return 1000
}

// denotes the unit time in mesh for scoring tallying.
func inMeshTime() time.Duration {
	return 1 * oneSlotDuration()
}

// the cap for `inMesh` time scoring.
func inMeshCap() float64 {
	return float64((3600 * time.Second) / inMeshTime())
}

// decayLimit provides the value till which a decay process will
// limit till provided with an expected growth rate.
func decayLimit(decayRate, rate float64) (float64, error) {
	if 1 <= decayRate {
		return 0, errors.Errorf("got an invalid decayLimit rate: %f", decayRate)
	}
	return rate / (1 - decayRate), nil
}

// is used to determine the threshold from the decay limit with
// a provided growth rate. This applies the decay rate to a
// computed limit.
func decayThreshold(decayRate, rate float64) (float64, error) {
	d, err := decayLimit(decayRate, rate)
	if err != nil {
		return 0, err
	}
	return d * decayRate, nil
}

// provides the relevant score by the provided weight and threshold.
func scoreByWeight(weight, threshold float64) float64 {
	return maxScore() / (weight * threshold * threshold)
}

// maxScore attainable by a peer.
func maxScore() float64 {
	totalWeight := beaconBlockWeight + aggregateWeight
	return (maxInMeshScore + maxFirstDeliveryScore) * totalWeight
}
