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

	"github.com/livekit/livekit-server/pkg/sfu/buffer"
	dd "github.com/livekit/livekit-server/pkg/sfu/rtpextension/dependencydescriptor"
	"github.com/livekit/protocol/logger"
)

var dependencyDescriptorSelectorBenchmarkSink []byte

func prepareDependencyDescriptorSelector(tb testing.TB) (*DependencyDescriptor, []*buffer.ExtPacket) {
	tb.Helper()

	selector := NewDependencyDescriptor(logger.GetLogger())
	selector.SetTarget(buffer.VideoLayer{Spatial: 1, Temporal: 2})
	selector.SetRequestSpatial(1)

	frames := createDDFrames(buffer.VideoLayer{Spatial: 2, Temporal: 2}, 1)
	if result := selector.Select(frames[0], 0); !result.IsSelected {
		tb.Fatal("initial dependency descriptor frame was not selected")
	}

	selected := make([]*buffer.ExtPacket, 0, 2)
	for _, frame := range frames[1:] {
		if result := selector.Select(frame, 0); result.IsSelected {
			selected = append(selected, frame)
			if len(selected) == cap(selected) {
				break
			}
		}
	}
	if len(selected) != cap(selected) {
		tb.Fatalf("got %d selected dependency descriptor frames, want %d", len(selected), cap(selected))
	}

	return selector, selected
}

func TestDependencyDescriptorSelectorReturnsOwnedExtension(t *testing.T) {
	selector, frames := prepareDependencyDescriptorSelector(t)

	first := selector.Select(frames[0], 0)
	if !first.IsSelected || len(first.DependencyDescriptorExtension) == 0 {
		t.Fatal("first dependency descriptor frame was not selected")
	}
	firstSnapshot := slices.Clone(first.DependencyDescriptorExtension)

	second := selector.Select(frames[1], 0)
	if !second.IsSelected || len(second.DependencyDescriptorExtension) == 0 {
		t.Fatal("second dependency descriptor frame was not selected")
	}

	if &first.DependencyDescriptorExtension[0] == &second.DependencyDescriptorExtension[0] {
		t.Fatal("dependency descriptor extensions share output storage")
	}
	if !slices.Equal(firstSnapshot, first.DependencyDescriptorExtension) {
		t.Fatal("first dependency descriptor extension changed after the next selection")
	}
}

func TestDependencyDescriptorSelectorUsesUpdatedStructure(t *testing.T) {
	selector, _ := prepareDependencyDescriptorSelector(t)

	updatedFrames := createDDFrames(buffer.VideoLayer{Spatial: 2, Temporal: 2}, 1000)
	updated := updatedFrames[0]
	updated.DependencyDescriptor.Descriptor.AttachedStructure.StructureId = 1

	result := selector.Select(updated, 0)
	if !result.IsSelected {
		t.Fatal("updated dependency descriptor frame was not selected")
	}

	expected, err := (&dd.DependencyDescriptorExtension{
		Descriptor: updated.DependencyDescriptor.Descriptor,
		Structure:  updated.DependencyDescriptor.Descriptor.AttachedStructure,
	}).Marshal()
	if err != nil {
		t.Fatalf("marshal expected dependency descriptor: %v", err)
	}
	if !slices.Equal(expected, result.DependencyDescriptorExtension) {
		t.Fatal("updated dependency descriptor used stale structure")
	}
}

func BenchmarkDependencyDescriptorSelector(b *testing.B) {
	selector, frames := prepareDependencyDescriptorSelector(b)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; b.Loop(); i++ {
		result := selector.Select(frames[i%len(frames)], 0)
		if !result.IsSelected || len(result.DependencyDescriptorExtension) == 0 {
			b.Fatal("dependency descriptor frame was not selected")
		}
		dependencyDescriptorSelectorBenchmarkSink = result.DependencyDescriptorExtension
	}
}
