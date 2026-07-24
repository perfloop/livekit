// Copyright 2026 LiveKit, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package rtc

import (
	"encoding/binary"
	"errors"
	"fmt"
)

const (
	// UnlabeledBatchTopic identifies an encoded batch in a UserPacket. A topic,
	// rather than a payload prefix, keeps arbitrary application bytes unambiguous.
	UnlabeledBatchTopic = "lk.unlabeled.batch.v1"

	UnlabeledBatchMaxRecords = 16
	UnlabeledBatchMaxBytes   = 12 * 1024
)

var errMalformedUnlabeledBatch = errors.New("malformed unlabeled batch")

// MarshalUnlabeledBatch encodes individual raw payloads while preserving their
// order and byte boundaries. The caller carries the encoding in a UserPacket
// tagged with UnlabeledBatchTopic.
func MarshalUnlabeledBatch(records [][]byte) ([]byte, error) {
	if len(records) == 0 || len(records) > UnlabeledBatchMaxRecords {
		return nil, fmt.Errorf("unlabeled batch record count %d out of range", len(records))
	}

	total := binary.MaxVarintLen64
	for _, record := range records {
		total += binary.MaxVarintLen64 + len(record)
		if total > UnlabeledBatchMaxBytes {
			return nil, fmt.Errorf("unlabeled batch size %d exceeds %d", total, UnlabeledBatchMaxBytes)
		}
	}

	encoded := make([]byte, 0, total)
	encoded = binary.AppendUvarint(encoded, uint64(len(records)))
	for _, record := range records {
		encoded = binary.AppendUvarint(encoded, uint64(len(record)))
		encoded = append(encoded, record...)
	}
	return encoded, nil
}

// UnmarshalUnlabeledBatch decodes a UserPacket payload tagged with
// UnlabeledBatchTopic. It returns independent record slices so callers may
// retain callback data after the packet buffer is released.
func UnmarshalUnlabeledBatch(encoded []byte) ([][]byte, error) {
	count, consumed := binary.Uvarint(encoded)
	if consumed <= 0 || count == 0 || count > UnlabeledBatchMaxRecords {
		return nil, errMalformedUnlabeledBatch
	}
	encoded = encoded[consumed:]

	records := make([][]byte, 0, count)
	total := 0
	for range count {
		length, consumed := binary.Uvarint(encoded)
		if consumed <= 0 || length > uint64(len(encoded)-consumed) {
			return nil, errMalformedUnlabeledBatch
		}
		encoded = encoded[consumed:]
		if length > uint64(UnlabeledBatchMaxBytes-total) {
			return nil, errMalformedUnlabeledBatch
		}
		record := append([]byte(nil), encoded[:length]...)
		records = append(records, record)
		total += int(length)
		encoded = encoded[length:]
	}
	if len(encoded) != 0 {
		return nil, errMalformedUnlabeledBatch
	}
	return records, nil
}
