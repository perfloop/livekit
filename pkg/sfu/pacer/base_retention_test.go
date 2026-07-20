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
	"reflect"
	"sync"
	"testing"

	"github.com/livekit/protocol/logger"
	"github.com/pion/rtp"
)

type retainedHeaderTestWriter struct {
	header *rtp.Header
}

func (w *retainedHeaderTestWriter) WriteRTP(header *rtp.Header, payload []byte) (int, error) {
	w.header = header
	return header.MarshalSize() + len(payload), nil
}

func (w *retainedHeaderTestWriter) Write(payload []byte) (int, error) {
	return len(payload), nil
}

func sendHeader(t *testing.T, packet *Packet) *rtp.Header {
	t.Helper()

	writer := &retainedHeaderTestWriter{}
	packet.WriteStream = writer
	if _, err := NewBase(logger.GetLogger(), nil).SendPacket(packet); err != nil {
		t.Fatalf("send packet: %v", err)
	}
	if writer.header == nil {
		t.Fatal("writer did not receive the header")
	}
	return writer.header
}

func sendRetainedHeader(t *testing.T, extensionCount int) *rtp.Header {
	t.Helper()

	packet := GetPacketWithRetainedHeader()
	header := packet.Header
	extensions := header.Extensions
	*header = rtp.Header{
		Version:        2,
		PayloadType:    111,
		SequenceNumber: 1,
		Timestamp:      960,
		SSRC:           0x11223344,
		Extensions:     extensions,
	}
	for id := uint8(1); id <= uint8(extensionCount); id++ {
		if err := header.SetExtension(id, make([]byte, 17)); err != nil {
			t.Fatalf("set header extension %d: %v", id, err)
		}
	}

	packet.HeaderSize = header.MarshalSize()
	packet.Payload = []byte{1}
	return sendHeader(t, packet)
}

func TestBaseSendPacketRetainedHeaderClearsExtensionPayloadReferences(t *testing.T) {
	reused := sendRetainedHeader(t, retainedHeaderExtensionCapacity)
	if len(reused.Extensions) != 0 {
		t.Fatalf("returned header has %d extensions, want 0", len(reused.Extensions))
	}
	if cap(reused.Extensions) != retainedHeaderExtensionCapacity {
		t.Fatalf("returned header extension capacity = %d, want %d", cap(reused.Extensions), retainedHeaderExtensionCapacity)
	}
	for i, extension := range reused.Extensions[:cap(reused.Extensions)] {
		payload := reflect.ValueOf(extension).FieldByName("payload")
		if payload.Kind() != reflect.Slice || !payload.IsNil() {
			t.Fatalf("returned header extension %d retains a payload reference", i)
		}
	}
}

func TestBaseSendPacketRetainedHeaderDropsReplacementExtensions(t *testing.T) {
	reused := sendRetainedHeader(t, retainedHeaderExtensionCapacity+1)
	if len(reused.Extensions) != 0 {
		t.Fatalf("returned header has %d extensions, want 0", len(reused.Extensions))
	}
	if cap(reused.Extensions) != retainedHeaderExtensionCapacity {
		t.Fatalf("returned header extension capacity = %d, want %d", cap(reused.Extensions), retainedHeaderExtensionCapacity)
	}
}

func TestBaseSendPacketDropsInteriorExtensionSubslice(t *testing.T) {
	header := &rtp.Header{
		Version:        2,
		PayloadType:    111,
		SequenceNumber: 1,
		Timestamp:      960,
		SSRC:           0x11223344,
		Extensions:     make([]rtp.Extension, 0, 4),
	}
	for id := uint8(1); id <= 4; id++ {
		if err := header.SetExtension(id, make([]byte, 17)); err != nil {
			t.Fatalf("set header extension %d: %v", id, err)
		}
	}

	header.Extensions = header.Extensions[2:4:4]
	headerPool := &sync.Pool{}
	packet := PacketFactory.Get().(*Packet)
	*packet = Packet{
		Header:     header,
		HeaderPool: headerPool,
		HeaderSize: header.MarshalSize(),
		Payload:    []byte{1},
	}
	reused := sendHeader(t, packet)
	if len(reused.Extensions) != 0 {
		t.Fatalf("returned header has %d extensions, want 0", len(reused.Extensions))
	}
	if cap(reused.Extensions) != 0 {
		t.Fatalf("returned header extension capacity = %d, want 0", cap(reused.Extensions))
	}
}
