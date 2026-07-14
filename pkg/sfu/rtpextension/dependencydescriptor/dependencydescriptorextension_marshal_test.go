// Copyright 2026 LiveKit, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package dependencydescriptor

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDependencyDescriptorMarshalRoundTrip(t *testing.T) {
	structure := &FrameDependencyStructure{
		NumDecodeTargets: 1,
		Templates: []*FrameDependencyTemplate{
			{
				DecodeTargetIndications: []DecodeTargetIndication{DecodeTargetRequired},
				FrameDiffs:              []int{},
				ChainDiffs:              []int{},
			},
		},
	}
	activeDecodeTargets := uint32(1)
	descriptor := &DependencyDescriptor{
		FirstPacketInFrame:         true,
		LastPacketInFrame:          true,
		FrameNumber:                42,
		FrameDependencies:          structure.Templates[0].Clone(),
		ActiveDecodeTargetsBitmask: &activeDecodeTargets,
	}
	extension := DependencyDescriptorExtension{
		Descriptor: descriptor,
		Structure:  structure,
	}

	payload, err := extension.Marshal()
	require.NoError(t, err)
	require.NotEmpty(t, payload)
	payloadSnapshot := append([]byte(nil), payload...)

	var decoded DependencyDescriptor
	reader := DependencyDescriptorExtension{
		Descriptor: &decoded,
		Structure:  structure,
	}
	_, err = reader.Unmarshal(payload)
	require.NoError(t, err)
	require.Equal(t, descriptor.FirstPacketInFrame, decoded.FirstPacketInFrame)
	require.Equal(t, descriptor.LastPacketInFrame, decoded.LastPacketInFrame)
	require.Equal(t, descriptor.FrameNumber, decoded.FrameNumber)
	require.Equal(t, descriptor.FrameDependencies, decoded.FrameDependencies)
	require.NotNil(t, decoded.ActiveDecodeTargetsBitmask)
	require.Equal(t, activeDecodeTargets, *decoded.ActiveDecodeTargetsBitmask)

	descriptor.FrameNumber++
	secondPayload, err := extension.Marshal()
	require.NoError(t, err)
	require.False(t, &payload[0] == &secondPayload[0])
	require.Equal(t, payloadSnapshot, payload)

	decoded = DependencyDescriptor{}
	reader = DependencyDescriptorExtension{
		Descriptor: &decoded,
		Structure:  structure,
	}
	_, err = reader.Unmarshal(secondPayload)
	require.NoError(t, err)
	require.Equal(t, descriptor.FrameNumber, decoded.FrameNumber)
}

func TestDependencyDescriptorMarshalWithActiveChainsConcurrent(t *testing.T) {
	const (
		workers           = 16
		callsPerWorker    = 32
		totalMarshalCalls = workers * callsPerWorker
	)

	structure := &FrameDependencyStructure{
		NumDecodeTargets:             2,
		NumChains:                    2,
		DecodeTargetProtectedByChain: []int{0, 1},
		Templates: []*FrameDependencyTemplate{
			{
				DecodeTargetIndications: []DecodeTargetIndication{DecodeTargetRequired, DecodeTargetRequired},
				FrameDiffs:              []int{},
				ChainDiffs:              []int{1, 1},
			},
		},
	}
	descriptor := &DependencyDescriptor{
		FirstPacketInFrame: true,
		LastPacketInFrame:  true,
		FrameNumber:        42,
		FrameDependencies: &FrameDependencyTemplate{
			DecodeTargetIndications: []DecodeTargetIndication{DecodeTargetRequired, DecodeTargetRequired},
			FrameDiffs:              []int{},
			ChainDiffs:              []int{2, 3},
		},
	}
	extension := &DependencyDescriptorExtension{
		Descriptor: descriptor,
		Structure:  structure,
	}

	expectedChainDiffs := map[uint32][]int{
		0: {1, 1},
		1: {2, 0},
		2: {0, 3},
		3: {2, 3},
	}
	type marshalResult struct {
		activeChains uint32
		payload      []byte
		err          error
	}

	results := make(chan marshalResult, totalMarshalCalls)
	var wg sync.WaitGroup
	for worker := range workers {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for call := range callsPerWorker {
				activeChains := uint32((worker + call) % len(expectedChainDiffs))
				payload, err := extension.MarshalWithActiveChains(activeChains)
				results <- marshalResult{
					activeChains: activeChains,
					payload:      payload,
					err:          err,
				}
			}
		}(worker)
	}
	wg.Wait()
	close(results)

	allResults := make([]marshalResult, 0, totalMarshalCalls)
	for result := range results {
		require.NoError(t, result.err)
		require.NotEmpty(t, result.payload)
		allResults = append(allResults, result)
	}
	require.Len(t, allResults, totalMarshalCalls)

	payloadStarts := make(map[*byte]struct{}, len(allResults))
	for _, result := range allResults {
		payloadStart := &result.payload[0]
		_, alreadyRetained := payloadStarts[payloadStart]
		require.False(t, alreadyRetained)
		payloadStarts[payloadStart] = struct{}{}

		var decoded DependencyDescriptor
		reader := DependencyDescriptorExtension{
			Descriptor: &decoded,
			Structure:  structure,
		}
		_, err := reader.Unmarshal(result.payload)
		require.NoError(t, err)
		require.Equal(t, descriptor.FirstPacketInFrame, decoded.FirstPacketInFrame)
		require.Equal(t, descriptor.LastPacketInFrame, decoded.LastPacketInFrame)
		require.Equal(t, descriptor.FrameNumber, decoded.FrameNumber)
		require.Equal(t, expectedChainDiffs[result.activeChains], decoded.FrameDependencies.ChainDiffs)
	}
}
