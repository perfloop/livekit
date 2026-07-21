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

	"github.com/pion/rtp"
)

type headerExtensionTestWriter struct {
	header []byte
}

func (w *headerExtensionTestWriter) WriteRTP(header *rtp.Header, payload []byte) (int, error) {
	marshaled, err := header.Marshal()
	if err != nil {
		return 0, err
	}

	w.header = append(w.header[:0], marshaled...)
	return len(marshaled) + len(payload), nil
}

func (w *headerExtensionTestWriter) Write(payload []byte) (int, error) {
	return len(payload), nil
}

func TestBaseSendPacketClearsHeaderExtensionDescriptors(t *testing.T) {
	payloads := [][]byte{
		{0x10, 0x11},
		{0x20, 0x21},
		{0x30, 0x31},
		{0x40, 0x41},
		{0x50, 0x51},
	}

	header := &rtp.Header{
		Version:        2,
		Marker:         true,
		PayloadType:    111,
		SequenceNumber: 1234,
		Timestamp:      5678,
		SSRC:           9012,
	}
	for id, payload := range payloads {
		if err := header.SetExtension(uint8(id+1), payload); err != nil {
			t.Fatalf("set extension %d: %v", id+1, err)
		}
	}

	writer := &headerExtensionTestWriter{}
	packet := &Packet{
		Header:      header,
		HeaderPool:  &sync.Pool{},
		Payload:     []byte{0x01, 0x02, 0x03},
		WriteStream: writer,
	}
	if _, err := NewBase(nil, nil).SendPacket(packet); err != nil {
		t.Fatalf("send packet: %v", err)
	}

	var sent rtp.Header
	if _, err := sent.Unmarshal(writer.header); err != nil {
		t.Fatalf("unmarshal sent header: %v", err)
	}
	for id, payload := range payloads {
		if got := sent.GetExtension(uint8(id + 1)); !reflect.DeepEqual(got, payload) {
			t.Fatalf("sent extension %d = %v, want %v", id+1, got, payload)
		}
	}

	if header.Version != 0 || header.Padding || header.Extension || header.Marker ||
		header.PayloadType != 0 || header.SequenceNumber != 0 || header.Timestamp != 0 ||
		header.SSRC != 0 || len(header.CSRC) != 0 || header.ExtensionProfile != 0 ||
		header.PaddingSize != 0 || header.PayloadOffset != 0 {
		t.Fatalf("header fields were not reset: %+v", header)
	}
	if len(header.Extensions) != 0 {
		t.Fatalf("header retains %d visible extensions", len(header.Extensions))
	}

	for i, extension := range header.Extensions[:cap(header.Extensions)] {
		value := reflect.ValueOf(extension)
		if id := value.FieldByName("id").Uint(); id != 0 {
			t.Fatalf("retained extension descriptor %d has id %d", i, id)
		}
		if !value.FieldByName("payload").IsNil() {
			t.Fatalf("retained extension descriptor %d keeps a payload reference", i)
		}
	}
}
