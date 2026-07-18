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

// BenchmarkForwarderGetTranslationParamsVP8 covers the selected video
// forwarding path, including the transfer of the munged VP8 header into the
// TranslationParams value that DownTrack consumes. The first key frame starts
// the forwarder outside the timed section; every timed operation is the next
// in-order, marker-delimited VP8 frame.
func BenchmarkForwarderGetTranslationParamsVP8(b *testing.B) {
	packets := newOrderedVP8Packets(b)
	forwarder := newVP8BenchmarkForwarder()

	var (
		frame  uint64
		params TranslationParams
		err    error
	)
	params, err = forwarder.GetTranslationParams(packets[frame], 0)
	if err != nil {
		b.Fatal(err)
	}
	requireForwardedVP8Header(b, params, frame)

	for b.Loop() {
		frame++
		packet := packets[frame&vp8BenchmarkPictureIDMask]
		setOrderedVP8PacketTiming(packet, frame)
		params, err = forwarder.GetTranslationParams(packet, 0)
	}
	if err != nil {
		b.Fatal(err)
	}
	requireForwardedVP8Header(b, params, frame)
}
