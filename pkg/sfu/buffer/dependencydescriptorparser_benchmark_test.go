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

package buffer

import (
	"encoding/hex"
	"testing"

	"github.com/livekit/protocol/logger"
	"github.com/pion/rtp"
)

const dependencyDescriptorBenchmarkExtID = 1

var dependencyDescriptorParserBenchmarkSink uint64

func BenchmarkDependencyDescriptorParserParseSteadyState(b *testing.B) {
	parser := NewDependencyDescriptorParser(dependencyDescriptorBenchmarkExtID, logger.GetLogger(), func(int32, int32) {}, false)

	structurePacket := dependencyDescriptorBenchmarkRTPPacket(b, 1, "c1017280081485214eafffaaaa863cf0430c10c302afc0aaa0063c00430010c002a000a80006000040001d954926e082b04a0941b820ac1282503157f974000ca864330e222222eca8655304224230eca877530077004200ef008601df010d")
	seed, _, err := parser.Parse(structurePacket)
	if err != nil {
		b.Fatal(err)
	}
	ReleaseExtDependencyDescriptor(seed)

	packets := []*rtp.Packet{
		dependencyDescriptorBenchmarkRTPPacket(b, 2, "860173"),
		dependencyDescriptorBenchmarkRTPPacket(b, 3, "460173"),
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ext, _, err := parser.Parse(packets[i%len(packets)])
		if err != nil {
			b.Fatal(err)
		}

		dependencies := ext.Descriptor.FrameDependencies
		if len(dependencies.DecodeTargetIndications) != 9 || len(dependencies.FrameDiffs) != 1 || len(dependencies.ChainDiffs) != 3 {
			b.Fatalf("unexpected template shape: %#v", dependencies)
		}
		dependencyDescriptorParserBenchmarkSink += uint64(dependencies.SpatialId + dependencies.TemporalId + len(dependencies.FrameDiffs))
		ReleaseExtDependencyDescriptor(ext)
	}
}

func dependencyDescriptorBenchmarkRTPPacket(tb testing.TB, sequenceNumber uint16, encodedExtension string) *rtp.Packet {
	tb.Helper()

	extension, err := hex.DecodeString(encodedExtension)
	if err != nil {
		tb.Fatal(err)
	}

	packet := &rtp.Packet{Header: rtp.Header{
		Version:        2,
		SequenceNumber: sequenceNumber,
	}}
	if err = packet.Header.SetExtension(dependencyDescriptorBenchmarkExtID, extension); err != nil {
		tb.Fatal(err)
	}
	return packet
}
