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
	"slices"
	"testing"
)

type dependencyDescriptorMarshalFixture struct {
	descriptor DependencyDescriptor
	extension  DependencyDescriptorExtension
}

func newDependencyDescriptorMarshalFixture(spatialLayers, temporalLayers int) *dependencyDescriptorMarshalFixture {
	numDecodeTargets := spatialLayers * temporalLayers
	decodeTargetIndications := make([]DecodeTargetIndication, numDecodeTargets)
	for i := range decodeTargetIndications {
		decodeTargetIndications[i] = DecodeTargetRequired
	}

	templateChainDiffs := make([]int, spatialLayers)
	for i := range templateChainDiffs {
		templateChainDiffs[i] = i + 1
	}
	decodeTargetProtectedByChain := make([]int, numDecodeTargets)
	templates := make([]*FrameDependencyTemplate, 0, numDecodeTargets)
	for spatial := range spatialLayers {
		for temporal := range temporalLayers {
			templates = append(templates, &FrameDependencyTemplate{
				SpatialId:               spatial,
				TemporalId:              temporal,
				DecodeTargetIndications: slices.Clone(decodeTargetIndications),
				FrameDiffs:              []int{1},
				ChainDiffs:              slices.Clone(templateChainDiffs),
			})
			decodeTargetProtectedByChain[spatial*temporalLayers+temporal] = spatial
		}
	}

	frameChainDiffs := make([]int, spatialLayers)
	for i := range frameChainDiffs {
		frameChainDiffs[i] = i + 2
	}
	activeDecodeTargets := uint32((1 << numDecodeTargets) - 1)
	fixture := &dependencyDescriptorMarshalFixture{
		descriptor: DependencyDescriptor{
			FirstPacketInFrame: true,
			LastPacketInFrame:  true,
			FrameDependencies: &FrameDependencyTemplate{
				SpatialId:               spatialLayers - 1,
				TemporalId:              temporalLayers - 1,
				DecodeTargetIndications: slices.Clone(decodeTargetIndications),
				FrameDiffs:              []int{1, 17},
				ChainDiffs:              frameChainDiffs,
			},
			ActiveDecodeTargetsBitmask: &activeDecodeTargets,
		},
	}
	fixture.extension = DependencyDescriptorExtension{
		Descriptor: &fixture.descriptor,
		Structure: &FrameDependencyStructure{
			NumDecodeTargets:             numDecodeTargets,
			NumChains:                    spatialLayers,
			DecodeTargetProtectedByChain: decodeTargetProtectedByChain,
			Templates:                    templates,
		},
	}
	return fixture
}

func BenchmarkDependencyDescriptorExtensionMarshalMixedStructures(b *testing.B) {
	fixtures := []*dependencyDescriptorMarshalFixture{
		newDependencyDescriptorMarshalFixture(3, 3),
		newDependencyDescriptorMarshalFixture(4, 4),
	}

	b.ReportAllocs()
	var (
		checksum        uint64
		index           int
		lastFixture     *dependencyDescriptorMarshalFixture
		lastPayload     []byte
		lastFrameNumber uint16
	)
	for b.Loop() {
		fixture := fixtures[index%len(fixtures)]
		index++
		fixture.descriptor.FrameNumber += uint16(index)
		payload, err := fixture.extension.Marshal()
		if err != nil || len(payload) == 0 {
			b.Fatal("dependency descriptor marshal failed")
		}
		checksum += uint64(len(payload)) + uint64(payload[0])
		lastFixture = fixture
		lastPayload = payload
		lastFrameNumber = fixture.descriptor.FrameNumber
	}
	if checksum == 0 {
		b.Fatal("dependency descriptor marshal did not consume output")
	}

	var decoded DependencyDescriptor
	reader := DependencyDescriptorExtension{
		Descriptor: &decoded,
		Structure:  lastFixture.extension.Structure,
	}
	if _, err := reader.Unmarshal(lastPayload); err != nil || decoded.FrameNumber != lastFrameNumber {
		b.Fatal("dependency descriptor marshal did not round-trip output")
	}
}
