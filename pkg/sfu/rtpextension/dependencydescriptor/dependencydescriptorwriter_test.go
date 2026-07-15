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
	"bytes"
	"testing"
)

func TestDependencyDescriptorWriterValueSizeBitsAndWriteRefreshTemplate(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*DependencyDescriptor)
	}{
		{
			name: "custom fields",
			mutate: func(descriptor *DependencyDescriptor) {
				descriptor.FrameDependencies.DecodeTargetIndications = []DecodeTargetIndication{DecodeTargetNotPresent, DecodeTargetRequired}
				descriptor.FrameDependencies.FrameDiffs = []int{1, 17}
				descriptor.FrameDependencies.ChainDiffs = []int{1}
			},
		},
		{
			name: "layer template",
			mutate: func(descriptor *DependencyDescriptor) {
				descriptor.FrameDependencies.TemporalId = 1
				descriptor.FrameDependencies.DecodeTargetIndications = []DecodeTargetIndication{DecodeTargetSwitch, DecodeTargetRequired}
				descriptor.FrameDependencies.FrameDiffs = []int{2}
				descriptor.FrameDependencies.ChainDiffs = []int{1}
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			structure := dependencyDescriptorWriterTestStructure()
			descriptor := dependencyDescriptorWriterTestDescriptor()
			writerForSize, err := NewDependencyDescriptorWriter(nil, structure, ^uint32(0), descriptor)
			if err != nil {
				t.Fatalf("new writer for size: %v", err)
			}
			writerForWrite, err := NewDependencyDescriptorWriter(nil, structure, ^uint32(0), descriptor)
			if err != nil {
				t.Fatalf("new writer for write: %v", err)
			}

			test.mutate(descriptor)
			expected, err := (&DependencyDescriptorExtension{
				Descriptor: descriptor,
				Structure:  structure,
			}).Marshal()
			if err != nil {
				t.Fatalf("stateless marshal: %v", err)
			}

			if got := (writerForSize.ValueSizeBits() + 7) / 8; got != len(expected) {
				t.Fatalf("value size after mutation = %d bytes, want %d", got, len(expected))
			}

			actual := make([]byte, len(expected))
			writerForWrite.ResetBuf(actual)
			if err = writerForWrite.Write(); err != nil {
				t.Fatalf("write after mutation: %v", err)
			}
			if !bytes.Equal(actual, expected) {
				t.Fatalf("writer output after mutation did not match stateless marshal\nactual:   %x\nexpected: %x", actual, expected)
			}
		})
	}
}

func dependencyDescriptorWriterTestStructure() *FrameDependencyStructure {
	return &FrameDependencyStructure{
		NumDecodeTargets:             2,
		NumChains:                    1,
		DecodeTargetProtectedByChain: []int{0, 0},
		Templates: []*FrameDependencyTemplate{
			{
				SpatialId:               0,
				TemporalId:              0,
				DecodeTargetIndications: []DecodeTargetIndication{DecodeTargetRequired, DecodeTargetRequired},
				FrameDiffs:              []int{1},
				ChainDiffs:              []int{0},
			},
			{
				SpatialId:               0,
				TemporalId:              1,
				DecodeTargetIndications: []DecodeTargetIndication{DecodeTargetSwitch, DecodeTargetRequired},
				FrameDiffs:              []int{2},
				ChainDiffs:              []int{1},
			},
		},
	}
}

func dependencyDescriptorWriterTestDescriptor() *DependencyDescriptor {
	return &DependencyDescriptor{
		FirstPacketInFrame: true,
		LastPacketInFrame:  true,
		FrameNumber:        100,
		FrameDependencies: &FrameDependencyTemplate{
			SpatialId:               0,
			TemporalId:              0,
			DecodeTargetIndications: []DecodeTargetIndication{DecodeTargetRequired, DecodeTargetRequired},
			FrameDiffs:              []int{1},
			ChainDiffs:              []int{0},
		},
	}
}
