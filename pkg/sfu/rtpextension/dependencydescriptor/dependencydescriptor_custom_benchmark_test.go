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
