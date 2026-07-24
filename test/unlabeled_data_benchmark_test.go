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
	// possible to three subscribers. This benchmark uses the same sustained
	// producer shape on the unlabeled ingress route exercised by
	// scenarioDataUnlabeledPublish. Each benchmark iteration is one input
	// record; the producer has no batch-sized record group.
	unlabeledFanoutBenchmarkRecipients = 3
	unlabeledFanoutBenchmarkBytes      = 100
	// TestDataPublishSlowSubscriber uses a 21,024-byte data-channel slow
	// threshold. At its 100-byte payload size, 210 is that source test's
	// record-equivalent backpressure boundary, not a batch size.
	unlabeledFanoutBenchmarkMaxInFlight = 210
	unlabeledFanoutBenchmarkQueue       = unlabeledFanoutBenchmarkMaxInFlight
	unlabeledFanoutDeliveryTimeout      = 30 * time.Second
	legacyUnlabeledProtocol             = 17
)

func fillUnlabeledFanoutBenchmarkPayload(payload []byte, sequence uint64) {
	binary.LittleEndian.PutUint64(payload[:8], sequence)
	binary.LittleEndian.PutUint64(payload[8:16], ^sequence)
	for i := 16; i < len(payload); i++ {
		payload[i] = byte(sequence + uint64(i*31))
	}
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

func unlabeledDataClientOptions(opts *testclient.Options) *testclient.Options {
	if opts == nil {
		opts = &testclient.Options{AutoSubscribe: true}
	}
	opts.DisableSTUN = true
	return opts
}

func unlabeledFanoutClientOptions(newOptions func() *testclient.Options) *testclient.Options {
	if newOptions == nil {
		return unlabeledDataClientOptions(nil)
	}
	return unlabeledDataClientOptions(newOptions())
}

type unlabeledFanoutBenchmarkRecipient struct {
	client    *testclient.RTCClient
	received  chan []byte
	delivered atomic.Uint64
	notify    chan struct{}
	errs      chan error
	stop      chan struct{}
}

func (r *unlabeledFanoutBenchmarkRecipient) consume() {
	var expected uint64
	for {
		select {
		case data := <-r.received:
			if len(data) != unlabeledFanoutBenchmarkBytes {
				r.fail(fmt.Errorf("recipient %s received %d bytes, want %d", r.client.ID(), len(data), unlabeledFanoutBenchmarkBytes))
				return
			}
			got := binary.LittleEndian.Uint64(data[:8])
			if got != expected {
				r.fail(fmt.Errorf("recipient %s sequence = %d, want %d", r.client.ID(), got, expected))
				return
			}
			if binary.LittleEndian.Uint64(data[8:16]) != ^got {
				r.fail(fmt.Errorf("recipient %s complement for sequence %d is invalid", r.client.ID(), got))
				return
			}
			expected++
			r.delivered.Store(expected)
			select {
			case r.notify <- struct{}{}:
			default:
			}
		case <-r.stop:
			return
		}
	}
}

func (r *unlabeledFanoutBenchmarkRecipient) fail(err error) {
	select {
	case r.errs <- err:
	default:
	}
}

func waitForUnlabeledFanoutDelivery(b *testing.B, recipients []*unlabeledFanoutBenchmarkRecipient, records uint64) {
	b.Helper()

	deadline := time.NewTimer(unlabeledFanoutDeliveryTimeout)
	defer deadline.Stop()
	for _, recipient := range recipients {
		for recipient.delivered.Load() < records {
			select {
			case err := <-recipient.errs:
				b.Fatal(err)
			case <-recipient.notify:
			case <-deadline.C:
				b.Fatalf("recipient %s received %d records, want %d", recipient.client.ID(), recipient.delivered.Load(), records)
			}
		}
		select {
		case err := <-recipient.errs:
			b.Fatal(err)
		default:
		}
	}
}

func benchmarkUnlabeledDataFanout(b *testing.B, newOptions func() *testclient.Options) {
	_, finish := setupSingleNodeTest(b.Name())
	b.Cleanup(finish)

	publisher := createRTCClient("unlabeled-benchmark-publisher", defaultServerPort, testRTCServicePathv0, unlabeledFanoutClientOptions(newOptions))
	b.Cleanup(publisher.Stop)

	recipients := make([]*unlabeledFanoutBenchmarkRecipient, 0, unlabeledFanoutBenchmarkRecipients)
	clients := make([]*testclient.RTCClient, 0, unlabeledFanoutBenchmarkRecipients+1)
	var receivedFrames atomic.Uint64
	clients = append(clients, publisher)
	for i := 0; i < unlabeledFanoutBenchmarkRecipients; i++ {
		client := createRTCClient(fmt.Sprintf("unlabeled-benchmark-recipient-%d", i), defaultServerPort, testRTCServicePathv0, unlabeledFanoutClientOptions(newOptions))
		recipient := &unlabeledFanoutBenchmarkRecipient{
			client:   client,
			received: make(chan []byte, unlabeledFanoutBenchmarkQueue),
			notify:   make(chan struct{}, 1),
			errs:     make(chan error, 1),
			stop:     make(chan struct{}),
		}
		client.OnDataFrameReceived = func() {
			receivedFrames.Add(1)
		}
		client.OnDataReceived = func(data []byte, _ string) {
			select {
			case recipient.received <- append([]byte(nil), data...):
			case <-recipient.stop:
			}
		}
		go recipient.consume()
		b.Cleanup(client.Stop)
		b.Cleanup(func() { close(recipient.stop) })
		recipients = append(recipients, recipient)
		clients = append(clients, client)
	}
	for _, client := range clients {
		if err := client.WaitUntilConnected(10 * time.Second); err != nil {
			b.Fatal(err)
		}
	}

	payload := make([]byte, unlabeledFanoutBenchmarkBytes)
	fillUnlabeledFanoutBenchmarkPayload(payload, 0)
	if err := publisher.PublishDataUnlabeled(payload); err != nil {
		b.Fatal(err)
	}
	// Wait for each raw data channel before timing so connection setup does not
	// become a variable part of the per-record transport measurement.
	waitForUnlabeledFanoutDelivery(b, recipients, 1)

	var sent uint64
	receivedFrames.Store(0)
	b.ResetTimer()
	for b.Loop() {
		fillUnlabeledFanoutBenchmarkPayload(payload, sent+1)
		if err := publisher.PublishDataUnlabeled(payload); err != nil {
			b.Fatal(err)
		}
		sent++
		if sent > unlabeledFanoutBenchmarkMaxInFlight {
			waitForUnlabeledFanoutDelivery(b, recipients, sent+1-unlabeledFanoutBenchmarkMaxInFlight)
		}
	}
	waitForUnlabeledFanoutDelivery(b, recipients, sent+1)
	b.StopTimer()
	b.ReportMetric(float64(receivedFrames.Load())/float64(sent), "frames/record")
}
