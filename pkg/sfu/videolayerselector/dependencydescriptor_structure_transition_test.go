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

func TestDependencyDescriptorSelectorRebuildsWriterForStructureTransition(t *testing.T) {
	initialFrames := createDDFrames(buffer.VideoLayer{Spatial: 2, Temporal: 2}, 1)
	replacementFrames := createDDFrames(buffer.VideoLayer{Spatial: 1, Temporal: 1}, 1000)
	initialStructure := initialFrames[0].DependencyDescriptor.Descriptor.AttachedStructure
	replacementKeyFrame := replacementFrames[0]
	replacementStructure := replacementKeyFrame.DependencyDescriptor.Descriptor.AttachedStructure
	require.NotEqual(t, initialStructure.NumDecodeTargets, replacementStructure.NumDecodeTargets)
	require.NotEqual(t, initialStructure.NumChains, replacementStructure.NumChains)

	selector := NewDependencyDescriptor(logger.GetLogger())
	highTarget := buffer.VideoLayer{Spatial: 2, Temporal: 2}
	selector.SetTarget(highTarget)
	selector.SetRequestSpatial(highTarget.Spatial)
	require.True(t, selector.Select(initialFrames[0], 0).IsSelected)

	var selectedHighFrame *buffer.ExtPacket
	for _, frame := range initialFrames[1:] {
		if selector.Select(frame, 0).IsSelected {
			selectedHighFrame = frame
			break
		}
	}
	require.NotNil(t, selectedHighFrame)

	lowTarget := buffer.VideoLayer{Spatial: 0, Temporal: 1}
	selector.SetTarget(lowTarget)
	selector.SetRequestSpatial(lowTarget.Spatial)
	replacementKeyResult := selector.Select(replacementKeyFrame, 0)
	require.True(t, replacementKeyResult.IsSelected)

	var decodedReplacementKey dd.DependencyDescriptor
	keyReader := dd.DependencyDescriptorExtension{Descriptor: &decodedReplacementKey}
	_, err := keyReader.Unmarshal(replacementKeyResult.DependencyDescriptorExtension)
	require.NoError(t, err)
	replacementKeyDescriptor := replacementKeyFrame.DependencyDescriptor.Descriptor
	require.Equal(t, replacementKeyDescriptor.AttachedStructure, decodedReplacementKey.AttachedStructure)
	require.NotNil(t, decodedReplacementKey.ActiveDecodeTargetsBitmask)
	require.Equal(t, *replacementKeyDescriptor.ActiveDecodeTargetsBitmask, *decodedReplacementKey.ActiveDecodeTargetsBitmask)

	var selectedReplacementFrame *buffer.ExtPacket
	var selectedReplacementResult VideoLayerSelectorResult
	for _, frame := range replacementFrames[1:] {
		result := selector.Select(frame, 0)
		if result.IsSelected {
			selectedReplacementFrame = frame
			selectedReplacementResult = result
			break
		}
	}
	require.NotNil(t, selectedReplacementFrame)
	require.Nil(t, selectedReplacementFrame.DependencyDescriptor.Descriptor.ActiveDecodeTargetsBitmask)

	var decodedReplacementFrame dd.DependencyDescriptor
	frameReader := dd.DependencyDescriptorExtension{
		Descriptor: &decodedReplacementFrame,
		Structure:  replacementStructure,
	}
	_, err = frameReader.Unmarshal(selectedReplacementResult.DependencyDescriptorExtension)
	require.NoError(t, err)
	require.Nil(t, decodedReplacementFrame.AttachedStructure)

	expectedActiveTargets := uint32(0)
	currentLayer := selector.GetCurrent()
	for _, decodeTarget := range replacementKeyFrame.DependencyDescriptor.DecodeTargets {
		if decodeTarget.Layer.Spatial <= currentLayer.Spatial && decodeTarget.Layer.Temporal <= currentLayer.Temporal {
			expectedActiveTargets |= 1 << decodeTarget.Target
		}
	}
	require.NotNil(t, decodedReplacementFrame.ActiveDecodeTargetsBitmask)
	require.Equal(t, expectedActiveTargets, *decodedReplacementFrame.ActiveDecodeTargetsBitmask)

	expectedFrame := selectedReplacementFrame.DependencyDescriptor.Descriptor
	require.Equal(t, expectedFrame.FrameNumber, decodedReplacementFrame.FrameNumber)
	require.Equal(t, expectedFrame.FrameDependencies.SpatialId, decodedReplacementFrame.FrameDependencies.SpatialId)
	require.Equal(t, expectedFrame.FrameDependencies.TemporalId, decodedReplacementFrame.FrameDependencies.TemporalId)
	require.True(t, slices.Equal(expectedFrame.FrameDependencies.DecodeTargetIndications, decodedReplacementFrame.FrameDependencies.DecodeTargetIndications))
	require.True(t, slices.Equal(expectedFrame.FrameDependencies.FrameDiffs, decodedReplacementFrame.FrameDependencies.FrameDiffs))
	require.True(t, slices.Equal(expectedFrame.FrameDependencies.ChainDiffs, decodedReplacementFrame.FrameDependencies.ChainDiffs))
}
