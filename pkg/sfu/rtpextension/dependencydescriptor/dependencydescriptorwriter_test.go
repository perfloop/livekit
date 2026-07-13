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

func TestDependencyDescriptorWriterTracksDescriptorMutation(t *testing.T) {
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
	descriptor := DependencyDescriptor{
		FrameNumber:       1,
		FrameDependencies: structure.Templates[0].Clone(),
	}
	writer, err := NewDependencyDescriptorWriter(nil, structure, ^uint32(0), &descriptor)
	require.NoError(t, err)

	activeDecodeTargets := uint32(1)
	descriptor.FrameNumber = 2
	descriptor.ActiveDecodeTargetsBitmask = &activeDecodeTargets
	writer.ResetBuf(make([]byte, (writer.ValueSizeBits()+7)/8))
	require.NoError(t, writer.Write())

	var decoded DependencyDescriptor
	reader := DependencyDescriptorExtension{
		Descriptor: &decoded,
		Structure:  structure,
	}
	_, err = reader.Unmarshal(writer.writer.buf)
	require.NoError(t, err)
	require.Equal(t, descriptor.FrameNumber, decoded.FrameNumber)
	require.NotNil(t, decoded.ActiveDecodeTargetsBitmask)
	require.Equal(t, activeDecodeTargets, *decoded.ActiveDecodeTargetsBitmask)
}
