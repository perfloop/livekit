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

package videolayerselector

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/livekit/livekit-server/pkg/sfu/buffer"
	dd "github.com/livekit/livekit-server/pkg/sfu/rtpextension/dependencydescriptor"
	"github.com/livekit/protocol/logger"
)

func TestDependencyDescriptorSelectorMarshalStructureAndActiveTargets(t *testing.T) {
	frames := createDDFrames(buffer.VideoLayer{Spatial: 2, Temporal: 2}, 1)
	targetLayer := buffer.VideoLayer{Spatial: 1, Temporal: 1}
	selector := NewDependencyDescriptor(logger.GetLogger())
	selector.SetTarget(targetLayer)
	selector.SetRequestSpatial(targetLayer.Spatial)

	keyFrame := frames[0]
	keyResult := selector.Select(keyFrame, 0)
	require.True(t, keyResult.IsSelected)

	var decodedKey dd.DependencyDescriptor
	keyReader := dd.DependencyDescriptorExtension{Descriptor: &decodedKey}
	_, err := keyReader.Unmarshal(keyResult.DependencyDescriptorExtension)
	require.NoError(t, err)
	keyDescriptor := keyFrame.DependencyDescriptor.Descriptor
	require.Equal(t, keyDescriptor.AttachedStructure, decodedKey.AttachedStructure)
	require.NotNil(t, decodedKey.ActiveDecodeTargetsBitmask)
	require.Equal(t, *keyDescriptor.ActiveDecodeTargetsBitmask, *decodedKey.ActiveDecodeTargetsBitmask)

	var selectedFrame *buffer.ExtPacket
	var selectedResult VideoLayerSelectorResult
	for _, frame := range frames[1:] {
		result := selector.Select(frame, 0)
		if result.IsSelected {
			selectedFrame = frame
			selectedResult = result
			break
		}
	}
	require.NotNil(t, selectedFrame)
	require.Nil(t, selectedFrame.DependencyDescriptor.Descriptor.ActiveDecodeTargetsBitmask)

	var decodedFrame dd.DependencyDescriptor
	frameReader := dd.DependencyDescriptorExtension{
		Descriptor: &decodedFrame,
		Structure:  keyDescriptor.AttachedStructure,
	}
	_, err = frameReader.Unmarshal(selectedResult.DependencyDescriptorExtension)
	require.NoError(t, err)
	require.Nil(t, decodedFrame.AttachedStructure)

	expectedActiveTargets := uint32(0)
	currentLayer := selector.GetCurrent()
	for _, decodeTarget := range keyFrame.DependencyDescriptor.DecodeTargets {
		if decodeTarget.Layer.Spatial <= currentLayer.Spatial && decodeTarget.Layer.Temporal <= currentLayer.Temporal {
			expectedActiveTargets |= 1 << decodeTarget.Target
		}
	}
	require.NotNil(t, decodedFrame.ActiveDecodeTargetsBitmask)
	require.Equal(t, expectedActiveTargets, *decodedFrame.ActiveDecodeTargetsBitmask)

	expectedFrame := selectedFrame.DependencyDescriptor.Descriptor
	require.Equal(t, expectedFrame.FrameNumber, decodedFrame.FrameNumber)
	require.Equal(t, expectedFrame.FrameDependencies.SpatialId, decodedFrame.FrameDependencies.SpatialId)
	require.Equal(t, expectedFrame.FrameDependencies.TemporalId, decodedFrame.FrameDependencies.TemporalId)
	require.True(t, slices.Equal(expectedFrame.FrameDependencies.DecodeTargetIndications, decodedFrame.FrameDependencies.DecodeTargetIndications))
	require.True(t, slices.Equal(expectedFrame.FrameDependencies.FrameDiffs, decodedFrame.FrameDependencies.FrameDiffs))
	require.True(t, slices.Equal(expectedFrame.FrameDependencies.ChainDiffs, decodedFrame.FrameDependencies.ChainDiffs))
}
