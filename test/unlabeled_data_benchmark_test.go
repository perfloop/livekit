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

package test

import (
	"encoding/binary"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/livekit/protocol/livekit"

	testclient "github.com/livekit/livekit-server/test/client"
)

const (
	// TestDataPublishSlowSubscriber sends 100-byte reliable data as fast as
	// possible to three subscribers. This uses the same width and payload size
	// on the unlabeled ingress path, which scenarioDataUnlabeledPublish covers.
	unlabeledFanoutBenchmarkRecipients = 3
	unlabeledFanoutBenchmarkMessages   = 24
	unlabeledFanoutBenchmarkBytes      = 100
	legacyUnlabeledProtocol            = 17
)

func unlabeledFanoutBenchmarkPayload(sequence uint64) []byte {
	payload := make([]byte, unlabeledFanoutBenchmarkBytes)
	binary.LittleEndian.PutUint64(payload[:8], sequence)
	binary.LittleEndian.PutUint64(payload[8:16], ^sequence)
	for i := 16; i < len(payload); i++ {
		payload[i] = byte(sequence + uint64(i*31))
	}
	return payload
}

func legacyUnlabeledClientOptions() *testclient.Options {
	return &testclient.Options{
		AutoSubscribe: true,
		ClientInfo: &livekit.ClientInfo{
			Sdk:      livekit.ClientInfo_GO,
			Protocol: legacyUnlabeledProtocol,
		},
	}
}

func BenchmarkUnlabeledDataFanout(b *testing.B) {
	benchmarkUnlabeledDataFanout(b, nil)
}

func BenchmarkUnlabeledDataLegacyFanout(b *testing.B) {
	benchmarkUnlabeledDataFanout(b, legacyUnlabeledClientOptions)
}

func unlabeledFanoutClientOptions(newOptions func() *testclient.Options) *testclient.Options {
	if newOptions == nil {
		return nil
	}
	return newOptions()
}

func benchmarkUnlabeledDataFanout(b *testing.B, newOptions func() *testclient.Options) {
	_, finish := setupSingleNodeTest(b.Name())
	b.Cleanup(finish)

	publisher := createRTCClient("unlabeled-benchmark-publisher", defaultServerPort, testRTCServicePathv0, unlabeledFanoutClientOptions(newOptions))
	b.Cleanup(publisher.Stop)

	type recipient struct {
		client   *testclient.RTCClient
		received chan []byte
	}
	recipients := make([]recipient, 0, unlabeledFanoutBenchmarkRecipients)
	clients := make([]*testclient.RTCClient, 0, unlabeledFanoutBenchmarkRecipients+1)
	var receivedFrames atomic.Uint64
	clients = append(clients, publisher)
	for i := 0; i < unlabeledFanoutBenchmarkRecipients; i++ {
		client := createRTCClient(fmt.Sprintf("unlabeled-benchmark-recipient-%d", i), defaultServerPort, testRTCServicePathv0, unlabeledFanoutClientOptions(newOptions))
		received := make(chan []byte, unlabeledFanoutBenchmarkMessages)
		client.OnDataFrameReceived = func() {
			receivedFrames.Add(1)
		}
		client.OnDataReceived = func(data []byte, _ string) {
			if len(data) == 0 {
				return
			}
			received <- append([]byte(nil), data...)
		}
		b.Cleanup(client.Stop)
		recipients = append(recipients, recipient{client: client, received: received})
		clients = append(clients, client)
	}
	for _, client := range clients {
		if err := client.WaitUntilConnected(10 * time.Second); err != nil {
			b.Fatal(err)
		}
	}

	receivedFrames.Store(0)
	var sequence uint64
	for b.Loop() {
		first := sequence
		for i := 0; i < unlabeledFanoutBenchmarkMessages; i++ {
			if err := publisher.PublishDataUnlabeled(unlabeledFanoutBenchmarkPayload(sequence)); err != nil {
				b.Fatal(err)
			}
			sequence++
		}
		for _, recipient := range recipients {
			for i := 0; i < unlabeledFanoutBenchmarkMessages; i++ {
				data := <-recipient.received
				if len(data) != unlabeledFanoutBenchmarkBytes {
					b.Fatalf("recipient %s received %d bytes, want %d", recipient.client.ID(), len(data), unlabeledFanoutBenchmarkBytes)
				}
				got := binary.LittleEndian.Uint64(data[:8])
				want := first + uint64(i)
				if got != want {
					b.Fatalf("recipient %s record %d sequence = %d, want %d", recipient.client.ID(), i, got, want)
				}
			}
		}
	}
	b.ReportMetric(float64(receivedFrames.Load())/float64(sequence), "frames/record")
}
