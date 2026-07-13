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

var (
	dependencyDescriptorShapeBenchmarkSink      *DependencyDescriptor
	dependencyDescriptorShapeBenchmarkBytesRead int
)

func BenchmarkDependencyDescriptorUnmarshalMinimalZeroFrameDiffs(b *testing.B) {
	benchmarkDependencyDescriptorUnmarshalShape(b, 1, 0, 0)
}

func BenchmarkDependencyDescriptorUnmarshalMinimalOneFrameDiff(b *testing.B) {
	benchmarkDependencyDescriptorUnmarshalShape(b, 1, 1, 0)
}

func BenchmarkDependencyDescriptorUnmarshalFallbackShape(b *testing.B) {
	benchmarkDependencyDescriptorUnmarshalShape(b, 10, 2, 4)
}

func benchmarkDependencyDescriptorUnmarshalShape(b *testing.B, decodeTargets, frameDiffs, chains int) {
	b.Helper()

	structure := dependencyDescriptorBenchmarkStructure(decodeTargets, frameDiffs, chains)
	packet := dependencyDescriptorBenchmarkPacket(b, structure)
	packets := [][]byte{packet, append([]byte(nil), packet...)}
	packets[1][2] ^= 1 // Vary the mandatory frame number while preserving the template-only path.

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		decoded := &DependencyDescriptor{}
		extension := DependencyDescriptorExtension{
			Descriptor: decoded,
			Structure:  structure,
		}

		bytesRead, err := extension.Unmarshal(packets[i%len(packets)])
		if err != nil {
			b.Fatal(err)
		}

		dependencyDescriptorShapeBenchmarkSink = decoded
		dependencyDescriptorShapeBenchmarkBytesRead = bytesRead
	}
}

func dependencyDescriptorBenchmarkStructure(decodeTargets, frameDiffs, chains int) *FrameDependencyStructure {
	template := &FrameDependencyTemplate{
		DecodeTargetIndications: make([]DecodeTargetIndication, decodeTargets),
		FrameDiffs:              make([]int, frameDiffs),
		ChainDiffs:              make([]int, chains),
	}
	for i := range template.DecodeTargetIndications {
		template.DecodeTargetIndications[i] = DecodeTargetRequired
	}
	for i := range template.FrameDiffs {
		template.FrameDiffs[i] = i + 1
	}
	for i := range template.ChainDiffs {
		template.ChainDiffs[i] = i + 1
	}

	return &FrameDependencyStructure{
		NumDecodeTargets: decodeTargets,
		NumChains:        chains,
		Templates:        []*FrameDependencyTemplate{template},
	}
}

func dependencyDescriptorBenchmarkPacket(b *testing.B, structure *FrameDependencyStructure) []byte {
	b.Helper()

	extension := DependencyDescriptorExtension{
		Descriptor: &DependencyDescriptor{
			FirstPacketInFrame: true,
			LastPacketInFrame:  true,
			FrameNumber:        0x1234,
			FrameDependencies:  structure.Templates[0],
		},
		Structure: structure,
	}
	packet, err := extension.Marshal()
	if err != nil {
		b.Fatal(err)
	}
	if len(packet) != 3 {
		b.Fatalf("template-only packet length = %d, want 3", len(packet))
	}
	return packet
}
