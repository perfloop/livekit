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

import (
	"bytes"
	"sync"
	"testing"

	"github.com/livekit/livekit-server/pkg/sfu/pacer"
	"github.com/livekit/protocol/logger"
	"github.com/pion/rtp"
)

type retainedHeaderCapacityGuardWriter struct {
	wire []byte
}

func (w *retainedHeaderCapacityGuardWriter) WriteRTP(header *rtp.Header, payload []byte) (int, error) {
	headerBytes, err := header.Marshal()
	if err != nil {
		return 0, err
	}
	w.wire = append(w.wire[:0], headerBytes...)
	w.wire = append(w.wire, payload...)
	return len(w.wire), nil
}

func (w *retainedHeaderCapacityGuardWriter) Write(payload []byte) (int, error) {
	w.wire = append(w.wire[:0], payload...)
	return len(w.wire), nil
}

func newRetainedHeaderCapacityGuardHeader(t testing.TB) (*rtp.Header, []byte) {
	t.Helper()

	header := &rtp.Header{
		Version:     2,
		PayloadType: 111,
		SSRC:        56,
	}
	stalePayload := []byte{0x51, 0x84, 0x13, 0xa7, 0x3e}
	for id := uint8(1); id <= 5; id++ {
		payload := []byte{id}
		if id == 5 {
			payload = stalePayload
		}
		if err := header.SetExtension(id, payload); err != nil {
			t.Fatal(err)
		}
	}
	if cap(header.Extensions) < 5 {
		t.Fatalf("header extension capacity = %d, want at least 5", cap(header.Extensions))
	}

	// This is the pooled-header transition in DownTrack.WriteRTP: a previous
	// five-extension packet is followed by one with only the two BWE extensions.
	*header = rtp.Header{
		Version:     2,
		PayloadType: 111,
		SSRC:        56,
		Extensions:  header.Extensions[:0],
	}
	if err := header.SetExtension(1, []byte{0x11, 0x22, 0x33}); err != nil {
		t.Fatal(err)
	}
	if err := header.SetExtension(2, []byte{0x44, 0x55}); err != nil {
		t.Fatal(err)
	}

	return header, stalePayload
}

func assertRetainedHeaderCapacityReleased(t testing.TB, header *rtp.Header, stalePayload []byte) {
	t.Helper()

	// The baseline drops the backing slice entirely. The retained-slice candidate
	// must instead leave every retained descriptor slot empty.
	if cap(header.Extensions) == 0 {
		return
	}
	if got := header.GetExtensionIDs(); len(got) != 0 {
		t.Fatalf("header extensions after return = %v, want none", got)
	}

	header.Extensions = header.Extensions[:cap(header.Extensions)]
	header.Extension = true
	header.ExtensionProfile = rtp.ExtensionProfileOneByte
	if got := header.GetExtension(5); got != nil {
		t.Fatalf("pooled header retained stale extension payload = %x", got)
	}
	wire, err := header.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(wire, stalePayload) {
		t.Fatalf("pooled header retained stale extension bytes in serialized form: %x", wire)
	}
}

func TestDownTrackHeaderExtensionsCapacityCleanup(t *testing.T) {
	header, stalePayload := newRetainedHeaderCapacityGuardHeader(t)
	writer := &retainedHeaderCapacityGuardWriter{}
	packet := &pacer.Packet{
		Header:      header,
		HeaderPool:  &sync.Pool{},
		Payload:     []byte{0x01},
		WriteStream: writer,
	}
	if _, err := pacer.NewBase(logger.GetLogger(), nil).SendPacket(packet); err != nil {
		t.Fatal(err)
	}
	if len(writer.wire) == 0 {
		t.Fatal("header was not serialized before cleanup")
	}
	assertRetainedHeaderCapacityReleased(t, header, stalePayload)
}

func newRetainedFiveRTPHeaderPool() *sync.Pool {
	return &sync.Pool{
		New: func() any {
			header := &rtp.Header{}
			for id := uint8(1); id <= 5; id++ {
				if err := header.SetExtension(id, []byte{id}); err != nil {
					panic(err)
				}
			}
			if cap(header.Extensions) < 5 {
				panic("five RTP extensions did not establish retained capacity")
			}
			*header = rtp.Header{Extensions: header.Extensions[:0]}
			return header
		},
	}
}

func BenchmarkDownTrackWriteRTPHeaderExtensionsRetainedFive(b *testing.B) {
	originalHeaderPool := RTPHeaderFactory
	RTPHeaderFactory = newRetainedFiveRTPHeaderPool()
	b.Cleanup(func() {
		RTPHeaderFactory = originalHeaderPool
	})

	downTrack, packet, writer, benchmarkBWE := newDownTrackHeaderExtensionsBenchmark()
	advanceDownTrackHeaderExtensionsBenchmarkPacket(packet)
	if got := downTrack.WriteRTP(packet, 0); got != 1 {
		b.Fatalf("warmup WriteRTP result = %d, want 1", got)
	}

	b.ReportAllocs()
	for b.Loop() {
		advanceDownTrackHeaderExtensionsBenchmarkPacket(packet)
		if got := downTrack.WriteRTP(packet, 0); got != 1 {
			b.Fatalf("WriteRTP result = %d, want 1", got)
		}
	}
	b.StopTimer()

	if writer.writes != b.N+1 {
		b.Fatalf("writer calls = %d, want %d", writer.writes, b.N+1)
	}
	assertDownTrackHeaderExtensionsBenchmarkOutput(b, writer, packet, benchmarkBWE)
}
