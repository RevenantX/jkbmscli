package main

import (
	"encoding/binary"
	"encoding/hex"
	"reflect"
	"testing"
)

func TestRequestsMatchAPKFrames(t *testing.T) {
	tests := []struct {
		name string
		got  []byte
		want string
	}{
		{
			name: "realtime telemetry",
			got:  realtimeTelemetryRequest,
			want: "aa5590eb97000000000000000000000000000011",
		},
		{
			name: "main page data",
			got:  mainPageDataRequest,
			want: "aa5590eb96000000000000000000000000000010",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := hex.EncodeToString(test.got); got != test.want {
				t.Fatalf("request = %s, want %s", got, test.want)
			}
		})
	}
}

func TestParserCombinesTelemetryWithMainPageCapacity(t *testing.T) {
	parser := &frameParser{}
	if got := parser.append(testFrame(0x01, 0, 0, 100_000, 0, 0), nil); len(got) != 0 {
		t.Fatalf("main-page append() = %v, want no readings", got)
	}

	want := []batteryReading{{
		stateOfCharge:       23,
		remainingCapacityAh: 18.5,
		capacityAh:          100,
		currentAmps:         -12.345,
		powerWatts:          276.5,
	}}
	if got := parser.append(testFrame(0x02, 23, 18_500, 0, -12_345, 276_500), nil); !reflect.DeepEqual(got, want) {
		t.Fatalf("append() = %v, want %v", got, want)
	}
}

func TestParserReassemblesSplitFrame(t *testing.T) {
	parser := &frameParser{}
	parser.append(testFrame(0x01, 0, 0, 100_000, 0, 0), nil)
	frame := testFrame(0x02, 64, 42_750, 0, 5_000, 1_250)

	if got := parser.append(frame[:128], nil); len(got) != 0 {
		t.Fatalf("first append() = %v, want no readings", got)
	}
	want := []batteryReading{{
		stateOfCharge:       64,
		remainingCapacityAh: 42.75,
		capacityAh:          100,
		currentAmps:         5,
		powerWatts:          1.25,
	}}
	if got := parser.append(frame[128:], nil); !reflect.DeepEqual(got, want) {
		t.Fatalf("second append() = %v, want %v", got, want)
	}
}

func TestParserUsesCapacityWhenMainPageArrivesAfterTelemetry(t *testing.T) {
	parser := &frameParser{}
	if got := parser.append(testFrame(0x02, 64, 42_750, 0, 5_000, 1_250), nil); len(got) != 0 {
		t.Fatalf("telemetry append() = %v, want capacity to arrive first", got)
	}

	want := []batteryReading{{
		stateOfCharge:       64,
		remainingCapacityAh: 42.75,
		capacityAh:          100,
		currentAmps:         5,
		powerWatts:          1.25,
	}}
	if got := parser.append(testFrame(0x01, 0, 0, 100_000, 0, 0), nil); !reflect.DeepEqual(got, want) {
		t.Fatalf("main-page append() = %v, want %v", got, want)
	}
}

func TestParserReportsNonTelemetryFrames(t *testing.T) {
	parser := &frameParser{}
	frame := testFrame(0x03, 0, 0, 0, 0, 0)
	var frameTypes []byte

	parser.append(frame[:100], func(frameType byte) {
		frameTypes = append(frameTypes, frameType)
	})
	parser.append(frame[100:], func(frameType byte) {
		frameTypes = append(frameTypes, frameType)
	})

	if !reflect.DeepEqual(frameTypes, []byte{0x03}) {
		t.Fatalf("frame types = % X, want [03]", frameTypes)
	}
}

func TestParserRejectsOutOfRangeSOC(t *testing.T) {
	parser := &frameParser{}
	frame := testFrame(0x02, 101, 18_500, 100_000, -1_000, 1_000)

	if got := parser.append(frame, nil); len(got) != 0 {
		t.Fatalf("append() = %v, want no readings", got)
	}
}

func TestEstimatedRuntime(t *testing.T) {
	tests := []struct {
		name    string
		reading batteryReading
		want    string
	}{
		{
			name:    "discharging",
			reading: batteryReading{remainingCapacityAh: 18.5, currentAmps: -7.5},
			want:    "2h 28m",
		},
		{
			name:    "charging",
			reading: batteryReading{remainingCapacityAh: 18.5, currentAmps: 7.5},
			want:    "Charging",
		},
		{
			name:    "idle",
			reading: batteryReading{remainingCapacityAh: 18.5},
			want:    "N/A",
		},
		{
			name:    "empty",
			reading: batteryReading{remainingCapacityAh: 0, currentAmps: -7.5},
			want:    "N/A",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := test.reading.estimatedRuntime(); got != test.want {
				t.Fatalf("estimatedRuntime() = %q, want %q", got, test.want)
			}
		})
	}
}

func testFrame(
	frameType, stateOfCharge byte,
	remainingCapacityMilliAh, ratedCapacityMilliAh uint32,
	currentMilliAmps int32,
	powerMilliWatts uint32,
) []byte {
	frame := make([]byte, frameLength)
	copy(frame, frameHeader)
	frame[4] = frameType
	switch frameType {
	case 0x01:
		binary.LittleEndian.PutUint32(
			frame[ratedCapacityOffset:ratedCapacityOffset+capacityFieldLength],
			ratedCapacityMilliAh,
		)
	case 0x02:
		frame[socOffset] = stateOfCharge
		binary.LittleEndian.PutUint32(
			frame[remainingCapacityOffset:remainingCapacityOffset+capacityFieldLength],
			remainingCapacityMilliAh,
		)
		binary.LittleEndian.PutUint32(
			frame[currentOffset:currentOffset+capacityFieldLength],
			uint32(currentMilliAmps),
		)
		binary.LittleEndian.PutUint32(
			frame[powerOffset:powerOffset+capacityFieldLength],
			powerMilliWatts,
		)
	}
	return frame
}
