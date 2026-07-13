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
	"bytes"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/livekit/livekit-server/pkg/sfu/buffer"
	dd "github.com/livekit/livekit-server/pkg/sfu/rtpextension/dependencydescriptor"
	"github.com/livekit/protocol/logger"
)

type dependencyDescriptorSelectorFixture struct {
	setup     []*buffer.ExtPacket
	target    *buffer.ExtPacket
	structure *dd.FrameDependencyStructure
	expected  []byte
}

func newDependencyDescriptorSelector(tb testing.TB) *DependencyDescriptor {
	tb.Helper()

	selector := NewDependencyDescriptor(logger.GetLogger())
	targetLayer := buffer.VideoLayer{Spatial: 2, Temporal: 2}
	selector.SetTarget(targetLayer)
	selector.SetRequestSpatial(targetLayer.Spatial)
	return selector
}

func newDependencyDescriptorSelectorFixture(tb testing.TB, startFrameNumber uint16) dependencyDescriptorSelectorFixture {
	tb.Helper()

	frames := createDDFrames(buffer.VideoLayer{Spatial: 2, Temporal: 2}, startFrameNumber)
	selector := newDependencyDescriptorSelector(tb)
	keyFrameResult := selector.Select(frames[0], 0)
	if !keyFrameResult.IsSelected {
		tb.Fatal("dependency descriptor selector did not select the key frame")
	}

	for index, frame := range frames[1:] {
		result := selector.Select(frame, 0)
		if result.IsSelected {
			return dependencyDescriptorSelectorFixture{
				setup:     frames[:index+1],
				target:    frame,
				structure: frames[0].DependencyDescriptor.Descriptor.AttachedStructure,
				expected:  bytes.Clone(result.DependencyDescriptorExtension),
			}
		}
	}

	tb.Fatal("dependency descriptor selector did not select a non-key frame")
	return dependencyDescriptorSelectorFixture{}
}

func validateDependencyDescriptorSelectorExtension(tb testing.TB, structure *dd.FrameDependencyStructure, frame *buffer.ExtPacket, extension []byte) {
	tb.Helper()
	require.NotEmpty(tb, extension)

	var decoded dd.DependencyDescriptor
	reader := dd.DependencyDescriptorExtension{
		Descriptor: &decoded,
		Structure:  structure,
	}
	_, err := reader.Unmarshal(extension)
	require.NoError(tb, err)

	expected := frame.DependencyDescriptor.Descriptor
	require.Equal(tb, expected.FirstPacketInFrame, decoded.FirstPacketInFrame)
	require.Equal(tb, expected.LastPacketInFrame, decoded.LastPacketInFrame)
	require.Equal(tb, expected.FrameNumber, decoded.FrameNumber)
	require.Equal(tb, expected.FrameDependencies.SpatialId, decoded.FrameDependencies.SpatialId)
	require.Equal(tb, expected.FrameDependencies.TemporalId, decoded.FrameDependencies.TemporalId)
	require.True(tb, slices.Equal(expected.FrameDependencies.DecodeTargetIndications, decoded.FrameDependencies.DecodeTargetIndications))
	require.True(tb, slices.Equal(expected.FrameDependencies.FrameDiffs, decoded.FrameDependencies.FrameDiffs))
	require.True(tb, slices.Equal(expected.FrameDependencies.ChainDiffs, decoded.FrameDependencies.ChainDiffs))
}

func TestDependencyDescriptorOutputIsolation(t *testing.T) {
	frames := createDDFrames(buffer.VideoLayer{Spatial: 2, Temporal: 2}, 1)
	selector := newDependencyDescriptorSelector(t)
	keyFrameResult := selector.Select(frames[0], 0)
	require.True(t, keyFrameResult.IsSelected)

	var first, firstSnapshot []byte
	for _, frame := range frames[1:] {
		result := selector.Select(frame, 0)
		if !result.IsSelected {
			continue
		}

		validateDependencyDescriptorSelectorExtension(t, frames[0].DependencyDescriptor.Descriptor.AttachedStructure, frame, result.DependencyDescriptorExtension)
		if first == nil {
			first = result.DependencyDescriptorExtension
			firstSnapshot = bytes.Clone(first)
			continue
		}

		require.NotEqual(t, &first[0], &result.DependencyDescriptorExtension[0])
		require.Equal(t, firstSnapshot, first)
		return
	}

	t.Fatal("dependency descriptor selector did not select two non-key frames")
}

func BenchmarkDependencyDescriptorSelector(b *testing.B) {
	fixtures := []dependencyDescriptorSelectorFixture{
		newDependencyDescriptorSelectorFixture(b, 1),
		newDependencyDescriptorSelectorFixture(b, 1000),
		newDependencyDescriptorSelectorFixture(b, 2000),
		newDependencyDescriptorSelectorFixture(b, 3000),
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		fixture := &fixtures[i%len(fixtures)]

		b.StopTimer()
		selector := newDependencyDescriptorSelector(b)
		for _, frame := range fixture.setup {
			selector.Select(frame, 0)
		}
		b.StartTimer()

		result := selector.Select(fixture.target, 0)
		if !result.IsSelected || len(result.DependencyDescriptorExtension) == 0 {
			b.StopTimer()
			b.Fatal("dependency descriptor selector did not return an extension")
		}
		b.StopTimer()

		if !bytes.Equal(fixture.expected, result.DependencyDescriptorExtension) {
			b.Fatal("dependency descriptor selector changed the marshaled extension")
		}
		validateDependencyDescriptorSelectorExtension(b, fixture.structure, fixture.target, result.DependencyDescriptorExtension)
	}
}
