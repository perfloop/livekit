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
	"fmt"
	"reflect"
	"testing"
)

func TestDependencyDescriptorReaderSteadyStateDoesNotMutateTemplates(t *testing.T) {
	packets := dependencyDescriptorSteadyStatePackets(t)
	for i, packet := range packets {
		t.Run(fmt.Sprintf("packet_%d", i), func(t *testing.T) {
			structure := dependencyDescriptorTestStructure(t)
			before := dependencyDescriptorTemplateSnapshot(structure.Templates)

			first := dependencyDescriptorTestParse(t, packet, structure)
			want := dependencyDescriptorTemplateSnapshot([]*FrameDependencyTemplate{first.FrameDependencies})[0]
			if len(first.FrameDependencies.DecodeTargetIndications) == 0 ||
				len(first.FrameDependencies.FrameDiffs) == 0 ||
				len(first.FrameDependencies.ChainDiffs) == 0 {
				t.Fatalf("steady-state packet did not expose every mutable dependency slice: %#v", first.FrameDependencies)
			}

			first.FrameDependencies.DecodeTargetIndications[0] = (first.FrameDependencies.DecodeTargetIndications[0] + 1) % 4
			first.FrameDependencies.FrameDiffs[0]++
			first.FrameDependencies.ChainDiffs[0]++

			if !reflect.DeepEqual(structure.Templates, before) {
				t.Fatal("structure templates changed after mutating steady-state descriptor")
			}

			second := dependencyDescriptorTestParse(t, packet, structure)
			if !reflect.DeepEqual(second.FrameDependencies, want) {
				t.Fatalf("second steady-state descriptor = %#v, want %#v", second.FrameDependencies, want)
			}
		})
	}
}

var (
	dependencyDescriptorCustomBenchmarkSink      *DependencyDescriptor
	dependencyDescriptorCustomBenchmarkBytesRead int
)

func BenchmarkDependencyDescriptorUnmarshalCustomOverrides(b *testing.B) {
	structure := dependencyDescriptorTestStructure(b)
	packet, _ := dependencyDescriptorCustomOverridePacket(b, structure)
	packets := [][]byte{packet, append([]byte(nil), packet...)}
	packets[1][2] ^= 1 // Vary the mandatory frame number while preserving the custom fields.

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		decoded := &DependencyDescriptor{}
		extension := DependencyDescriptorExtension{
			Descriptor: decoded,
			Structure:  structure,
		}

		selectedPacket := packets[i%len(packets)]
		bytesRead, err := extension.Unmarshal(selectedPacket)
		if err != nil {
			b.Fatal(err)
		}

		dependencyDescriptorCustomBenchmarkSink = decoded
		dependencyDescriptorCustomBenchmarkBytesRead = bytesRead
	}
}
