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
