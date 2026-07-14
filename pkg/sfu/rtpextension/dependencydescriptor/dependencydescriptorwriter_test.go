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

package dependencydescriptor_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	dd "github.com/livekit/livekit-server/pkg/sfu/rtpextension/dependencydescriptor"
)

func TestDependencyDescriptorWriterTracksPostConstructionDescriptorMutation(t *testing.T) {
	structure := &dd.FrameDependencyStructure{
		NumDecodeTargets: 1,
		Templates: []*dd.FrameDependencyTemplate{
			{
				DecodeTargetIndications: []dd.DecodeTargetIndication{dd.DecodeTargetRequired},
				FrameDiffs:              []int{},
				ChainDiffs:              []int{},
			},
		},
	}
	descriptor := dd.DependencyDescriptor{
		FrameNumber:       1,
		FrameDependencies: structure.Templates[0].Clone(),
	}
	buf := make([]byte, 8)
	writer, err := dd.NewDependencyDescriptorWriter(buf, structure, ^uint32(0), &descriptor)
	require.NoError(t, err)

	activeDecodeTargets := uint32(1)
	descriptor.FrameNumber = 2
	descriptor.ActiveDecodeTargetsBitmask = &activeDecodeTargets
	descriptor.FrameDependencies.FrameDiffs = []int{1}
	writer.ResetBuf(buf)
	require.NoError(t, writer.Write())

	var decoded dd.DependencyDescriptor
	reader := dd.DependencyDescriptorExtension{
		Descriptor: &decoded,
		Structure:  structure,
	}
	_, err = reader.Unmarshal(buf)
	require.NoError(t, err)
	require.Equal(t, descriptor.FrameNumber, decoded.FrameNumber)
	require.NotNil(t, decoded.ActiveDecodeTargetsBitmask)
	require.Equal(t, activeDecodeTargets, *decoded.ActiveDecodeTargetsBitmask)
	require.Equal(t, []int{1}, decoded.FrameDependencies.FrameDiffs)
}
