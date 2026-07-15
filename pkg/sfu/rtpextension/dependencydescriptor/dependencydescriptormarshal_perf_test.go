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

import "testing"

func BenchmarkDependencyDescriptorMarshalWithActiveChains3x3(b *testing.B) {
	const (
		spatialLayers  = 3
		temporalLayers = 3
	)

	decodeTargets := spatialLayers * temporalLayers
	templateDtis := make([]DecodeTargetIndication, decodeTargets)
	for i := range templateDtis {
		templateDtis[i] = DecodeTargetSwitch
	}
	templateChains := []int{0, 0, 0}
	templates := make([]*FrameDependencyTemplate, 0, decodeTargets)
	for spatial := range spatialLayers {
		for temporal := range temporalLayers {
			templates = append(templates, &FrameDependencyTemplate{
				SpatialId:               spatial,
				TemporalId:              temporal,
				DecodeTargetIndications: templateDtis,
				ChainDiffs:              templateChains,
			})
		}
	}

	structure := &FrameDependencyStructure{
		NumDecodeTargets:             decodeTargets,
		NumChains:                    spatialLayers,
		DecodeTargetProtectedByChain: []int{0, 0, 0, 1, 1, 1, 2, 2, 2},
		Templates:                    templates,
	}
	frameDtis := make([]DecodeTargetIndication, decodeTargets)
	for i := range frameDtis {
		if i < decodeTargets-1 {
			frameDtis[i] = DecodeTargetNotPresent
		} else {
			frameDtis[i] = DecodeTargetRequired
		}
	}
	descriptor := &DependencyDescriptor{
		FirstPacketInFrame: true,
		LastPacketInFrame:  true,
		FrameDependencies: &FrameDependencyTemplate{
			SpatialId:               spatialLayers - 1,
			TemporalId:              temporalLayers - 1,
			DecodeTargetIndications: frameDtis,
			FrameDiffs:              []int{1, 17},
			ChainDiffs:              []int{1, 1, 1},
		},
	}
	extension := &DependencyDescriptorExtension{Descriptor: descriptor, Structure: structure}

	for b.Loop() {
		descriptor.FrameNumber++
		encoded, err := extension.MarshalWithActiveChains(^uint32(0))
		if err != nil || len(encoded) == 0 {
			b.Fatalf("marshal: %v, len=%d", err, len(encoded))
		}
	}
}
