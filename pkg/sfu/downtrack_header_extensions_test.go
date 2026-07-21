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
	"time"

	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"

	"github.com/livekit/protocol/livekit"
	"github.com/livekit/protocol/logger"

	"github.com/livekit/livekit-server/pkg/sfu/buffer"
	"github.com/livekit/livekit-server/pkg/sfu/pacer"
	"github.com/livekit/livekit-server/pkg/sfu/rtpstats"
)

const (
	benchmarkAbsSendTimeExtensionID = 1
	benchmarkTransportCCExtensionID = 2
)

type noOpDownTrackListener struct{}

func (noOpDownTrackListener) OnBindAndConnected() {}

func (noOpDownTrackListener) OnStatsUpdate(*livekit.AnalyticsStat) {}

func (noOpDownTrackListener) OnMaxSubscribedLayerChanged(int32) {}

func (noOpDownTrackListener) OnRttUpdate(uint32) {}

func (noOpDownTrackListener) OnCodecNegotiated(webrtc.RTPCodecCapability) {}

func (noOpDownTrackListener) OnDownTrackClose(bool) {}

func (noOpDownTrackListener) OnStreamStarted() {}

type headerExtensionWriter struct {
	capturePackets bool
	packets        [][]byte
	checksum       uint64
	invalid        bool
}

func (w *headerExtensionWriter) WriteRTP(header *rtp.Header, payload []byte) (int, error) {
	absSendTime := header.GetExtension(benchmarkAbsSendTimeExtensionID)
	transportCC := header.GetExtension(benchmarkTransportCCExtensionID)
	if header.Version != 2 || len(header.Extensions) != 2 || len(absSendTime) != 3 || !bytes.Equal(transportCC, dummyTransportCCExt) {
		w.invalid = true
	}

	w.checksum += uint64(header.SequenceNumber) + uint64(header.Timestamp) + uint64(len(payload)) + uint64(len(absSendTime)) + uint64(len(transportCC))
	if w.capturePackets {
		packet, err := (&rtp.Packet{Header: *header, Payload: payload}).Marshal()
		if err != nil {
			w.invalid = true
			return 0, err
		}
		w.packets = append(w.packets, packet)
	}

	return header.MarshalSize() + len(payload), nil
}

func (w *headerExtensionWriter) Write(payload []byte) (int, error) {
	w.checksum += uint64(len(payload))
	return len(payload), nil
}

func newHeaderExtensionsDownTrack() (*DownTrack, *buffer.ExtPacket, *headerExtensionWriter) {
	log := logger.GetLogger()
	stats := rtpstats.NewRTPStatsSender(rtpstats.RTPStatsParams{}, 64)
	stats.SetClockRate(48_000)
	stats.SetLogger(log)

	forwarder := NewForwarder(
		webrtc.RTPCodecTypeAudio,
		log,
		true, // skipReferenceTS
		true, // disableOpportunisticAllocation
		false,
		stats,
	)
	forwarder.DetermineCodec(
		webrtc.RTPCodecCapability{MimeType: "audio/opus", ClockRate: 48_000},
		nil,
		livekit.VideoLayer_MODE_UNUSED,
	)

	writer := &headerExtensionWriter{}
	downTrack := &DownTrack{
		params: DownTrackParams{
			Logger:   log,
			Listener: noOpDownTrackListener{},
		},
		kind:               webrtc.RTPCodecTypeAudio,
		ssrc:               0x13572468,
		forwarder:          forwarder,
		rtpStats:           stats,
		absSendTimeExtID:   benchmarkAbsSendTimeExtensionID,
		transportWideExtID: benchmarkTransportCCExtensionID,
		writeStream:        writer,
		pacer:              pacer.NewPassThrough(log, nil),
	}
	downTrack.payloadType.Store(111)
	downTrack.writable.Store(true)

	packet := &buffer.ExtPacket{
		Packet: &rtp.Packet{
			Header: rtp.Header{
				Version:        2,
				PayloadType:    111,
				SequenceNumber: 1000,
				Timestamp:      96_000,
				SSRC:           0x24681357,
			},
			Payload: []byte{0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x88},
		},
		ExtSequenceNumber: 1000,
		ExtTimestamp:      96_000,
		Arrival:           time.Now().UnixNano(),
	}

	return downTrack, packet, writer
}

func advanceHeaderExtensionsPacket(packet *buffer.ExtPacket) {
	packet.Packet.SequenceNumber++
	packet.Packet.Timestamp += 960
	packet.Packet.Payload[0]++
	packet.ExtSequenceNumber++
	packet.ExtTimestamp += 960
	packet.Arrival += int64(20 * time.Millisecond)
}

func TestWriteRTPHeaderExtensionsAreResetBeforeReuse(t *testing.T) {
	originalHeaderFactory := RTPHeaderFactory
	header := &rtp.Header{}
	headerPool := &sync.Pool{New: func() any { return header }}
	RTPHeaderFactory = headerPool
	t.Cleanup(func() {
		RTPHeaderFactory = originalHeaderFactory
	})

	downTrack, packet, writer := newHeaderExtensionsDownTrack()
	writer.capturePackets = true
	for range 2 {
		if wrote := downTrack.WriteRTP(packet, 0); wrote != 1 {
			t.Fatalf("WriteRTP wrote %d packets, want 1", wrote)
		}
		advanceHeaderExtensionsPacket(packet)
	}

	if writer.invalid {
		t.Fatal("writer observed an invalid RTP header extension set")
	}
	if len(writer.packets) != 2 {
		t.Fatalf("captured %d packets, want 2", len(writer.packets))
	}
	for i, raw := range writer.packets {
		var received rtp.Packet
		if err := received.Unmarshal(raw); err != nil {
			t.Fatalf("packet %d did not unmarshal: %v", i, err)
		}
		if got := len(received.Extensions); got != 2 {
			t.Fatalf("packet %d has %d extensions, want 2", i, got)
		}
		if got := len(received.GetExtension(benchmarkAbsSendTimeExtensionID)); got != 3 {
			t.Fatalf("packet %d abs-send-time length = %d, want 3", i, got)
		}
		if got := received.GetExtension(benchmarkTransportCCExtensionID); !bytes.Equal(got, dummyTransportCCExt) {
			t.Fatalf("packet %d transport-cc = %x, want %x", i, got, dummyTransportCCExt)
		}
	}

	pooledHeader := headerPool.Get().(*rtp.Header)
	if pooledHeader.Version != 0 || pooledHeader.Extension || pooledHeader.ExtensionProfile != 0 || len(pooledHeader.Extensions) != 0 {
		t.Fatalf("pooled header retained RTP semantics: %#v", pooledHeader)
	}
	if cap(pooledHeader.Extensions) > 5 {
		t.Fatalf("pooled header retained extension capacity %d, want at most 5", cap(pooledHeader.Extensions))
	}
}

func BenchmarkDownTrackWriteRTPHeaderExtensions(b *testing.B) {
	downTrack, packet, writer := newHeaderExtensionsDownTrack()
	if wrote := downTrack.WriteRTP(packet, 0); wrote != 1 {
		b.Fatalf("warmup WriteRTP wrote %d packets, want 1", wrote)
	}
	advanceHeaderExtensionsPacket(packet)
	b.SetBytes(int64(len(packet.Packet.Payload)))
	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		if wrote := downTrack.WriteRTP(packet, 0); wrote != 1 {
			b.Fatalf("WriteRTP wrote %d packets, want 1", wrote)
		}
		advanceHeaderExtensionsPacket(packet)
	}

	b.StopTimer()
	if writer.invalid {
		b.Fatal("writer observed an invalid RTP header extension set")
	}
	if writer.checksum == 0 {
		b.Fatal("writer did not consume benchmark output")
	}
}
