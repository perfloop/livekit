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

package pacer

import (
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/livekit/protocol/logger"
	"github.com/pion/rtp"
)

type finalizableExtensionPayload [17]byte

type resetTestWriter struct {
	wire [64]byte
}

func (w *resetTestWriter) WriteRTP(header *rtp.Header, payload []byte) (int, error) {
	n, err := header.MarshalTo(w.wire[:])
	if err != nil {
		return 0, err
	}
	return n + len(payload), nil
}

func (w *resetTestWriter) Write(payload []byte) (int, error) {
	return len(payload), nil
}

func sendFinalizableExtension(t *testing.T) (<-chan struct{}, *rtp.Header) {
	t.Helper()

	finalized := make(chan struct{}, 1)
	payload := new(finalizableExtensionPayload)
	runtime.SetFinalizer(payload, func(*finalizableExtensionPayload) {
		finalized <- struct{}{}
	})

	header := &rtp.Header{
		Version:        2,
		PayloadType:    111,
		SequenceNumber: 1,
		Timestamp:      960,
		SSRC:           0x11223344,
	}
	if err := header.SetExtension(1, payload[:]); err != nil {
		t.Fatalf("set header extension: %v", err)
	}

	headerPool := &sync.Pool{}
	packet := PacketFactory.Get().(*Packet)
	*packet = Packet{
		Header:      header,
		HeaderPool:  headerPool,
		HeaderSize:  header.MarshalSize(),
		Payload:     []byte{1},
		WriteStream: &resetTestWriter{},
	}
	if _, err := NewBase(logger.GetLogger(), nil).SendPacket(packet); err != nil {
		t.Fatalf("send packet: %v", err)
	}

	// Retain the returned header directly across GC. If the reset only shortens
	// Extensions without clearing its elements, the backing array keeps payload
	// alive and its finalizer cannot run.
	reused, ok := headerPool.Get().(*rtp.Header)
	if !ok {
		t.Fatal("header pool returned an unexpected value")
	}
	if len(reused.Extensions) != 0 {
		t.Fatalf("returned header has %d extensions, want 0", len(reused.Extensions))
	}

	return finalized, reused
}

func TestBaseSendPacketClearsExtensionPayloadReferences(t *testing.T) {
	finalized, reused := sendFinalizableExtension(t)
	defer runtime.KeepAlive(reused)

	for range 100 {
		runtime.GC()
		select {
		case <-finalized:
			return
		default:
			time.Sleep(time.Millisecond)
		}
	}

	t.Fatal("pooled RTP header retained an extension payload reference")
}
