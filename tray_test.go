package main

import (
	"bytes"
	"image/png"
	"testing"
)

func TestTrayTooltipIncludesBatteryReadings(t *testing.T) {
	reading := batteryReading{
		stateOfCharge:       42,
		currentAmps:         -7.5,
		powerWatts:          199.8,
		remainingCapacityAh: 18.5,
		capacityAh:          100,
	}
	want := "JK-BMS\nCharge: 42%\nCurrent (A): -7.5\nPower (W): 199.8\nRem. Cap. (Ah): 18.5\nCapacity (Ah): 100.0\nEstimated runtime: 2h 28m"
	if got := formatTrayTooltip(reading); got != want {
		t.Fatalf("formatTrayTooltip() = %q, want %q", got, want)
	}
}

func TestBatteryIconIsValidPNG(t *testing.T) {
	icon := batteryIcon(50)
	if _, err := png.Decode(bytes.NewReader(icon)); err != nil {
		t.Fatalf("batteryIcon() is not a valid PNG: %v", err)
	}
}
