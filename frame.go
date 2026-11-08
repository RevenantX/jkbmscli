package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
)

var frameHeader = []byte{0x55, 0xAA, 0xEB, 0x90}

const (
	frameLength = 300
	// Offset 173 is payload byte 167 after the six-byte protocol header.
	socOffset               = 173
	remainingCapacityOffset = 174
	powerOffset             = 154 // Payload bytes 148-151; power is reported as an unsigned magnitude.
	currentOffset           = 158 // Payload bytes 152-155; the APK decodes this as signed int32.
	ratedCapacityOffset     = 130 // Payload bytes 124-127 in the type-01 main-page frame.
	capacityFieldLength     = 4
	maximumStateOfCharge    = 100
)

type batteryReading struct {
	stateOfCharge       int
	remainingCapacityAh float64
	capacityAh          float64
	currentAmps         float64
	powerWatts          float64
}

func (reading batteryReading) estimatedRuntime() string {
	if reading.currentAmps == 0 {
		return "N/A"
	}

	capacityToUse := reading.remainingCapacityAh
	if reading.currentAmps > 0 {
		if reading.capacityAh <= 0 {
			return "N/A"
		}
		capacityToUse = reading.capacityAh - reading.remainingCapacityAh
		if capacityToUse <= 0 {
			return "0h 00m"
		}
	} else if capacityToUse <= 0 {
		return "N/A"
	}

	minutes := int(math.Round(capacityToUse / math.Abs(reading.currentAmps) * 60))
	return fmt.Sprintf("%dh %02dm", minutes/60, minutes%60)
}

// frameParser joins notification chunks and extracts complete JK-BMS frames.
type frameParser struct {
	buffer          []byte
	capacityAh      float64
	hasCapacity     bool
	latestTelemetry batteryReading
	hasTelemetry    bool
}

func (parser *frameParser) append(data []byte, onFrame func(byte)) []batteryReading {
	parser.buffer = append(parser.buffer, data...)
	var readings []batteryReading

	for {
		headerOffset := bytes.Index(parser.buffer, frameHeader)
		if headerOffset < 0 {
			if len(parser.buffer) > len(frameHeader)-1 {
				parser.buffer = append([]byte(nil), parser.buffer[len(parser.buffer)-(len(frameHeader)-1):]...)
			}
			break
		}

		if headerOffset > 0 {
			parser.buffer = parser.buffer[headerOffset:]
		}
		if len(parser.buffer) < frameLength {
			break
		}

		frameType := parser.buffer[4]
		if onFrame != nil {
			onFrame(frameType)
		}
		switch frameType {
		case 0x01:
			capacity := binary.LittleEndian.Uint32(
				parser.buffer[ratedCapacityOffset : ratedCapacityOffset+capacityFieldLength],
			)
			parser.capacityAh = float64(capacity) / 1000
			parser.hasCapacity = true
			if parser.hasTelemetry {
				parser.latestTelemetry.capacityAh = parser.capacityAh
				readings = append(readings, parser.latestTelemetry)
			}
		case 0x02:
			stateOfCharge := int(parser.buffer[socOffset])
			if stateOfCharge <= maximumStateOfCharge {
				remainingCapacity := binary.LittleEndian.Uint32(
					parser.buffer[remainingCapacityOffset : remainingCapacityOffset+capacityFieldLength],
				)
				power := binary.LittleEndian.Uint32(
					parser.buffer[powerOffset : powerOffset+capacityFieldLength],
				)
				current := int32(binary.LittleEndian.Uint32(
					parser.buffer[currentOffset : currentOffset+capacityFieldLength],
				))
				parser.latestTelemetry = batteryReading{
					stateOfCharge:       stateOfCharge,
					remainingCapacityAh: float64(remainingCapacity) / 1000,
					currentAmps:         float64(current) / 1000,
					powerWatts:          float64(power) / 1000,
				}
				parser.hasTelemetry = true
				if parser.hasCapacity {
					parser.latestTelemetry.capacityAh = parser.capacityAh
					readings = append(readings, parser.latestTelemetry)
				}
			}
		}

		parser.buffer = parser.buffer[frameLength:]
	}

	return readings
}
