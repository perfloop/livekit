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
	"testing"

	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"

	"github.com/livekit/protocol/livekit"
	"github.com/livekit/protocol/logger"

	"github.com/livekit/livekit-server/pkg/sfu/buffer"
	"github.com/livekit/livekit-server/pkg/sfu/pacer"
	"github.com/livekit/livekit-server/pkg/sfu/rtpstats"
)

type headerExtensionDownTrackListener struct{}

func (*headerExtensionDownTrackListener) OnBindAndConnected() {}

func (*headerExtensionDownTrackListener) OnStatsUpdate(_ *livekit.AnalyticsStat) {}

func (*headerExtensionDownTrackListener) OnMaxSubscribedLayerChanged(_ int32) {}

func (*headerExtensionDownTrackListener) OnRttUpdate(_ uint32) {}

func (*headerExtensionDownTrackListener) OnCodecNegotiated(_ webrtc.RTPCodecCapability) {}

func (*headerExtensionDownTrackListener) OnDownTrackClose(_ bool) {}

func (*headerExtensionDownTrackListener) OnStreamStarted() {}

type headerExtensionWriter struct {
	buffer   [256]byte
	header   *rtp.Header
	n        int
	checksum uint64
}

func (w *headerExtensionWriter) WriteRTP(header *rtp.Header, payload []byte) (int, error) {
	n, err := header.MarshalTo(w.buffer[:])
	if err != nil {
		return 0, err
	}

	w.header = header
	w.n = n
	w.checksum += uint64(n) + uint64(header.SequenceNumber)
	if len(payload) != 0 {
		w.checksum += uint64(payload[0])
	}
	return n + len(payload), nil
}

func (w *headerExtensionWriter) Write(payload []byte) (int, error) {
	w.checksum += uint64(len(payload))
	return len(payload), nil
}

func newHeaderExtensionDownTrack(writer *headerExtensionWriter) (*DownTrack, *buffer.ExtPacket) {
	log := logger.GetLogger()
	stats := rtpstats.NewRTPStatsSender(rtpstats.RTPStatsParams{}, 128)
	stats.SetLogger(log)
	stats.SetClockRate(48_000)

	forwarder := NewForwarder(
		webrtc.RTPCodecTypeAudio,
		log,
		true, // skipReferenceTS
		true, // disableOpportunisticAllocation
		true, // enableStartAtDesiredQuality
		stats,
	)
	forwarder.DetermineCodec(
		webrtc.RTPCodecCapability{MimeType: "audio/opus", ClockRate: 48_000},
		nil,
		livekit.VideoLayer_MODE_UNUSED,
	)

	downTrack := &DownTrack{
		params: DownTrackParams{
			Logger:   log,
			Listener: &headerExtensionDownTrackListener{},
		},
		kind:               webrtc.RTPCodecTypeAudio,
		ssrc:               0x10203040,
		forwarder:          forwarder,
		rtpStats:           stats,
		absSendTimeExtID:   1,
		transportWideExtID: 2,
		writeStream:        writer,
		pacer:              pacer.NewPassThrough(log, nil),
	}
	downTrack.payloadType.Store(111)
	downTrack.writable.Store(true)

	payload := make([]byte, 64)
	for i := range payload {
		payload[i] = byte(i)
	}

	return downTrack, &buffer.ExtPacket{
		Packet: &rtp.Packet{
			Header: rtp.Header{
				Version:        2,
				Marker:         true,
				PayloadType:    111,
				SequenceNumber: 1,
				Timestamp:      960,
				SSRC:           0x55667788,
			},
			Payload: payload,
		},
		ExtSequenceNumber: 1,
		ExtTimestamp:      960,
		Arrival:           1,
	}
}

func advanceHeaderExtensionPacket(packet *buffer.ExtPacket) {
	packet.Packet.SequenceNumber++
	packet.Packet.Timestamp += 960
	packet.Packet.Payload[0] = byte(packet.Packet.SequenceNumber)
	packet.ExtSequenceNumber++
	packet.ExtTimestamp += 960
	packet.Arrival += 20_000_000
}

func TestDownTrackWriteRTPHeaderExtensions(t *testing.T) {
	writer := &headerExtensionWriter{}
	downTrack, packet := newHeaderExtensionDownTrack(writer)

	if got := downTrack.WriteRTP(packet, 0); got != 1 {
		t.Fatalf("WriteRTP = %d, want 1", got)
	}
	if writer.header == nil || writer.n == 0 {
		t.Fatal("WriteRTP did not write an RTP header")
	}

	var sent rtp.Header
	if _, err := sent.Unmarshal(writer.buffer[:writer.n]); err != nil {
		t.Fatalf("unmarshal sent header: %v", err)
	}
	if got := sent.GetExtension(1); len(got) != len(dummyAbsSendTimeExt) {
		t.Fatalf("abs-send-time extension length = %d, want %d", len(got), len(dummyAbsSendTimeExt))
	}
	if got := sent.GetExtension(2); string(got) != string(dummyTransportCCExt) {
		t.Fatalf("transport-wide extension = %v, want %v", got, dummyTransportCCExt)
	}
	if writer.header.Extension || len(writer.header.Extensions) != 0 {
		t.Fatalf("pooled header retained visible extensions: %+v", writer.header)
	}
}

func BenchmarkDownTrackWriteRTPHeaderExtensions(b *testing.B) {
	writer := &headerExtensionWriter{}
	downTrack, packet := newHeaderExtensionDownTrack(writer)

	if got := downTrack.WriteRTP(packet, 0); got != 1 {
		b.Fatalf("warmup WriteRTP = %d, want 1", got)
	}
	checksum := writer.checksum
	b.ReportAllocs()

	for b.Loop() {
		advanceHeaderExtensionPacket(packet)
		if got := downTrack.WriteRTP(packet, 0); got != 1 {
			b.Fatalf("WriteRTP = %d, want 1", got)
		}
	}

	if writer.checksum == checksum || writer.n == 0 {
		b.Fatal("benchmark writer did not consume the forwarded RTP output")
	}
}
