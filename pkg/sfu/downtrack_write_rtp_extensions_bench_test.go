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
	"testing"

	"github.com/livekit/protocol/logger"
	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"

	"github.com/livekit/livekit-server/pkg/sfu/buffer"
	"github.com/livekit/livekit-server/pkg/sfu/pacer"
	"github.com/livekit/livekit-server/pkg/sfu/rtpstats"
)

// writeRTPBenchmarkWriter consumes the outgoing header as the WebRTC writer
// would, without adding a second allocation to the benchmarked forwarding path.
type writeRTPBenchmarkWriter struct {
	wire           [64]byte
	wireLen        int
	packets        int
	extensionCount int
	payloadByte    byte
}

func (w *writeRTPBenchmarkWriter) WriteRTP(header *rtp.Header, payload []byte) (int, error) {
	n, err := header.MarshalTo(w.wire[:])
	if err != nil {
		return 0, err
	}

	w.wireLen = n
	w.packets++
	w.extensionCount = len(header.Extensions)
	w.payloadByte = payload[len(payload)-1]
	return n + len(payload), nil
}

func (w *writeRTPBenchmarkWriter) Write(payload []byte) (int, error) {
	w.payloadByte = payload[len(payload)-1]
	return len(payload), nil
}

func newWriteRTPHeaderExtensionsFixture(tb testing.TB) (*DownTrack, *buffer.ExtPacket, *writeRTPBenchmarkWriter) {
	tb.Helper()

	log := logger.GetLogger()
	stats := rtpstats.NewRTPStatsSender(rtpstats.RTPStatsParams{}, 64)
	stats.SetClockRate(48_000)

	forwarder := NewForwarder(
		webrtc.RTPCodecTypeAudio,
		log,
		true, // Skip sender-report reference timing in this isolated steady-state fixture.
		false,
		false,
		stats,
	)
	forwarder.clockRate = 48_000

	writer := &writeRTPBenchmarkWriter{}
	downTrack := &DownTrack{
		params: DownTrackParams{
			Logger: log,
		},
		kind:               webrtc.RTPCodecTypeAudio,
		ssrc:               0x11223344,
		forwarder:          forwarder,
		rtpStats:           stats,
		pacer:              pacer.NewPassThrough(log, nil),
		absSendTimeExtID:   1,
		transportWideExtID: 2,
		writeStream:        writer,
	}
	downTrack.payloadType.Store(111)
	downTrack.writable.Store(true)

	extPkt := &buffer.ExtPacket{
		ExtSequenceNumber: 1,
		ExtTimestamp:      960,
		Packet: &rtp.Packet{
			Header: rtp.Header{
				Version:        2,
				PayloadType:    111,
				SequenceNumber: 1,
				Timestamp:      960,
				SSRC:           0x55667788,
			},
			Payload: make([]byte, 160),
		},
	}

	// Seed the forwarder as though it had already emitted one packet. The
	// benchmark then measures the common steady-state forwarding branch.
	forwarder.started = true
	forwarder.lastSSRC = extPkt.Packet.SSRC
	forwarder.lastReferencePayloadType = int8(extPkt.Packet.PayloadType)
	forwarder.rtpMunger.SetLastSnTs(extPkt)
	if _, err := forwarder.rtpMunger.UpdateAndGetSnTs(extPkt, false); err != nil {
		tb.Fatalf("seed RTP munger: %v", err)
	}

	return downTrack, extPkt, writer
}

func advanceWriteRTPBenchmarkPacket(extPkt *buffer.ExtPacket) {
	extPkt.ExtSequenceNumber++
	extPkt.ExtTimestamp += 960
	extPkt.Packet.SequenceNumber++
	extPkt.Packet.Timestamp += 960
	// Keep the payload input runtime-varying as it crosses the copied-payload
	// boundary in DownTrack.WriteRTP.
	extPkt.Packet.Payload[0] = byte(extPkt.ExtSequenceNumber)
}

func TestDownTrackWriteRTPWithPacerExtensions(t *testing.T) {
	downTrack, extPkt, writer := newWriteRTPHeaderExtensionsFixture(t)

	for range 2 {
		advanceWriteRTPBenchmarkPacket(extPkt)
		if written := downTrack.WriteRTP(extPkt, 0); written != 1 {
			t.Fatalf("WriteRTP wrote %d packets, want 1", written)
		}
	}

	if writer.packets != 2 {
		t.Fatalf("writer received %d packets, want 2", writer.packets)
	}
	if writer.extensionCount != 2 {
		t.Fatalf("writer received %d RTP extensions, want 2", writer.extensionCount)
	}
	if writer.payloadByte != extPkt.Packet.Payload[len(extPkt.Packet.Payload)-1] {
		t.Fatalf("writer payload byte = %d, want %d", writer.payloadByte, extPkt.Packet.Payload[len(extPkt.Packet.Payload)-1])
	}

	var header rtp.Header
	if _, err := header.Unmarshal(writer.wire[:writer.wireLen]); err != nil {
		t.Fatalf("unmarshal written RTP header: %v", err)
	}
	if header.SequenceNumber != extPkt.Packet.SequenceNumber {
		t.Fatalf("sequence number = %d, want %d", header.SequenceNumber, extPkt.Packet.SequenceNumber)
	}
	if header.Timestamp != extPkt.Packet.Timestamp {
		t.Fatalf("timestamp = %d, want %d", header.Timestamp, extPkt.Packet.Timestamp)
	}
	if header.SSRC != downTrack.ssrc {
		t.Fatalf("SSRC = %#x, want %#x", header.SSRC, downTrack.ssrc)
	}
	if header.PayloadType != uint8(downTrack.payloadType.Load()) {
		t.Fatalf("payload type = %d, want %d", header.PayloadType, downTrack.payloadType.Load())
	}
	if absSendTime := header.GetExtension(uint8(downTrack.absSendTimeExtID)); len(absSendTime) != 3 {
		t.Fatalf("abs-send-time extension length = %d, want 3", len(absSendTime))
	}
	if transportWide := header.GetExtension(uint8(downTrack.transportWideExtID)); !bytes.Equal(transportWide, dummyTransportCCExt) {
		t.Fatalf("transport-wide extension = %v, want %v", transportWide, dummyTransportCCExt)
	}
}

func BenchmarkDownTrackWriteRTPWithHeaderExtensions(b *testing.B) {
	downTrack, extPkt, writer := newWriteRTPHeaderExtensionsFixture(b)

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
	if writer.wireLen == 0 || writer.extensionCount != 2 {
		b.Fatalf("writer did not consume the expected RTP header extensions")
	}
}
