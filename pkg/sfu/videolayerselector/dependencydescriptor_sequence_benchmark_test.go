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
	"testing"

	"github.com/livekit/livekit-server/pkg/sfu/buffer"
	"github.com/livekit/protocol/logger"
)

type dependencyDescriptorSelectorSequenceFixture struct {
	initial     []*buffer.ExtPacket
	replacement []*buffer.ExtPacket
}

func newDependencyDescriptorSelectorSequenceFixture(initialFrameNumber, replacementFrameNumber uint16) dependencyDescriptorSelectorSequenceFixture {
	maxLayer := buffer.VideoLayer{Spatial: 2, Temporal: 2}
	return dependencyDescriptorSelectorSequenceFixture{
		initial:     createDDFrames(maxLayer, initialFrameNumber),
		replacement: createDDFrames(maxLayer, replacementFrameNumber),
	}
}

func BenchmarkDependencyDescriptorSelectorSequence(b *testing.B) {
	fixtures := []dependencyDescriptorSelectorSequenceFixture{
		newDependencyDescriptorSelectorSequenceFixture(1, 1000),
		newDependencyDescriptorSelectorSequenceFixture(2000, 3000),
		newDependencyDescriptorSelectorSequenceFixture(4000, 5000),
		newDependencyDescriptorSelectorSequenceFixture(6000, 7000),
	}
	logger := logger.GetLogger()
	highTarget := buffer.VideoLayer{Spatial: 2, Temporal: 2}
	lowTarget := buffer.VideoLayer{Spatial: 1, Temporal: 1}

	b.ReportAllocs()
	var (
		lastChecksum uint64
		lastSelected int
		index        int
	)
	for b.Loop() {
		fixture := &fixtures[index%len(fixtures)]
		index++
		selector := NewDependencyDescriptor(logger)
		selector.SetTarget(highTarget)
		selector.SetRequestSpatial(highTarget.Spatial)

		checksum := uint64(0)
		selected := 0
		for _, frame := range fixture.initial {
			result := selector.Select(frame, 0)
			if result.IsSelected {
				if len(result.DependencyDescriptorExtension) == 0 {
					b.Fatal("dependency descriptor selector did not return an extension")
				}
				selected++
				checksum += uint64(len(result.DependencyDescriptorExtension)) + uint64(result.DependencyDescriptorExtension[0])
			}
		}

		selector.SetTarget(lowTarget)
		selector.SetRequestSpatial(lowTarget.Spatial)
		for _, frame := range fixture.replacement {
			result := selector.Select(frame, 0)
			if result.IsSelected {
				if len(result.DependencyDescriptorExtension) == 0 {
					b.Fatal("dependency descriptor selector did not return an extension")
				}
				selected++
				checksum += uint64(len(result.DependencyDescriptorExtension)) + uint64(result.DependencyDescriptorExtension[0])
			}
		}
		lastChecksum = checksum
		lastSelected = selected
	}

	if lastSelected == 0 || lastChecksum == 0 {
		b.Fatal("dependency descriptor selector did not return selected extensions")
	}
}
