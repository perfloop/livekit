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
	"bytes"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/livekit/protocol/logger"
	"github.com/pion/rtp"
)

type testRTPWriter struct {
	wire []byte
}

func (w *testRTPWriter) WriteRTP(header *rtp.Header, payload []byte) (int, error) {
	headerSize := header.MarshalSize()
	w.wire = make([]byte, headerSize+len(payload))
	n, err := header.MarshalTo(w.wire[:headerSize])
	if err != nil {
		return 0, err
	}
	copy(w.wire[n:], payload)
	return n + len(payload), nil
}

func (w *testRTPWriter) Write(payload []byte) (int, error) {
	w.wire = append(w.wire[:0], payload...)
	return len(payload), nil
}

func TestBaseSendPacketSerializesExtensionsBeforeHeaderReset(t *testing.T) {
	header := &rtp.Header{
		Version:        2,
		PayloadType:    111,
		SequenceNumber: 12,
		Timestamp:      34,
		SSRC:           56,
	}
	extension := []byte{0xaa, 0xbb, 0xcc}
	if err := header.SetExtension(1, extension); err != nil {
		t.Fatal(err)
	}

	writer := &testRTPWriter{}
	packet := &Packet{
		Header:      header,
		HeaderPool:  &sync.Pool{},
		Payload:     []byte{0x01, 0x02, 0x03},
		WriteStream: writer,
	}

	written, err := NewBase(logger.GetLogger(), nil).SendPacket(packet)
	if err != nil {
		t.Fatal(err)
	}
	if written != len(writer.wire) {
		t.Fatalf("written bytes = %d, want %d", written, len(writer.wire))
	}

	var serialized rtp.Packet
	if err = serialized.Unmarshal(writer.wire); err != nil {
		t.Fatal(err)
	}
	if got := serialized.GetExtension(1); !bytes.Equal(got, extension) {
		t.Fatalf("serialized extension = %x, want %x", got, extension)
	}
	if got := serialized.Payload; !bytes.Equal(got, []byte{0x01, 0x02, 0x03}) {
		t.Fatalf("serialized payload = %x", got)
	}

	if got := header.GetExtensionIDs(); len(got) != 0 {
		t.Fatalf("header extensions after return = %v, want none", got)
	}
}

type headerExtensionPayload [64]byte

func headerWithFinalizableExtension(t *testing.T) (*rtp.Header, <-chan struct{}) {
	t.Helper()

	finalized := make(chan struct{})
	payload := new(headerExtensionPayload)
	for i := range payload {
		payload[i] = byte(i)
	}
	runtime.SetFinalizer(payload, func(*headerExtensionPayload) {
		close(finalized)
	})

	header := &rtp.Header{
		Version:     2,
		PayloadType: 111,
		SSRC:        56,
	}
	if err := header.SetExtension(1, payload[:]); err != nil {
		t.Fatal(err)
	}
	return header, finalized
}

func waitForHeaderExtensionPayloadFinalizer(t *testing.T, finalized <-chan struct{}, header *rtp.Header) {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		runtime.GC()
		runtime.Gosched()

		select {
		case <-finalized:
			runtime.KeepAlive(header)
			return
		default:
		}
		time.Sleep(time.Millisecond)
	}

	runtime.KeepAlive(header)
	t.Fatal("pooled header retained an extension payload after packet cleanup")
}

func TestBaseSendPacketReleasesPooledHeaderExtensionPayload(t *testing.T) {
	header, finalized := headerWithFinalizableExtension(t)
	packet := &Packet{
		Header:      header,
		HeaderPool:  &sync.Pool{},
		Payload:     []byte{0x01},
		WriteStream: &testRTPWriter{},
	}

	if _, err := NewBase(logger.GetLogger(), nil).SendPacket(packet); err != nil {
		t.Fatal(err)
	}
	if got := header.GetExtensionIDs(); len(got) != 0 {
		t.Fatalf("header extensions after return = %v, want none", got)
	}

	waitForHeaderExtensionPayloadFinalizer(t, finalized, header)
}
