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

package sfu

import "testing"

func BenchmarkDownTrackWriteRTPWithAdditionalHeaderExtension(b *testing.B) {
	downTrack, extPkt, writer := newWriteRTPHeaderExtensionsFixture(b)
	playoutDelay, err := NewPlayoutDelayController(100, 200, downTrack.params.Logger, downTrack.rtpStats)
	if err != nil {
		b.Fatalf("new playout delay controller: %v", err)
	}
	downTrack.playoutDelayExtID = 3
	downTrack.playoutDelay = playoutDelay

	b.ReportAllocs()
	for b.Loop() {
		advanceWriteRTPBenchmarkPacket(extPkt)
		if written := downTrack.WriteRTP(extPkt, 0); written != 1 {
			b.Fatalf("WriteRTP wrote %d packets, want 1", written)
		}
	}

	if writer.packets != b.N {
		b.Fatalf("writer received %d packets, want %d", writer.packets, b.N)
	}
	if writer.wireLen == 0 || writer.extensionCount != 3 {
		b.Fatalf("writer did not consume the expected RTP header extensions")
	}
}
