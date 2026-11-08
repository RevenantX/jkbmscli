package main

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
	"os"

	"fyne.io/systray"
)

func runTray(ctx context.Context, stop context.CancelFunc, address string) error {
	monitorDone := make(chan error, 1)
	systray.Run(func() {
		systray.SetTitle("JK-BMS")
		systray.SetTooltip("JK-BMS: waiting for battery data")
		systray.SetIcon(batteryIcon(0))

		quitItem := systray.AddMenuItem("Quit", "Disconnect from the battery and exit")
		go func() {
			<-quitItem.ClickedCh
			systray.Quit()
		}()

		go func() {
			err := monitor(ctx, address, updateTray)
			monitorDone <- err
			systray.Quit()
		}()
	}, func() {
		stop()
	})
	return <-monitorDone
}

func updateTray(reading batteryReading) {
	systray.SetTitle(fmt.Sprintf("%d%%", reading.stateOfCharge))
	systray.SetIcon(batteryIcon(reading.stateOfCharge))
	systray.SetTooltip(formatTrayTooltip(reading))
}

func formatTrayTooltip(reading batteryReading) string {
	return fmt.Sprintf(
		"JK-BMS\nCharge: %d%%\nCurrent (A): %+.1f\nPower (W): %.1f\nRem. Cap. (Ah): %.1f\nCapacity (Ah): %.1f\nEstimated time to full/empty: %s",
		reading.stateOfCharge,
		reading.currentAmps,
		reading.powerWatts,
		reading.remainingCapacityAh,
		reading.capacityAh,
		reading.estimatedRuntime(),
	)
}

func batteryIcon(stateOfCharge int) []byte {
	const size = 32
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	white := color.RGBA{R: 245, G: 245, B: 245, A: 255}
	dark := color.RGBA{R: 45, G: 45, B: 45, A: 255}
	fill := color.RGBA{R: 70, G: 190, B: 95, A: 255}
	if stateOfCharge <= 20 {
		fill = color.RGBA{R: 220, G: 65, B: 65, A: 255}
	} else if stateOfCharge <= 50 {
		fill = color.RGBA{R: 225, G: 175, B: 45, A: 255}
	}

	draw.Draw(img, img.Bounds(), image.NewUniform(color.Transparent), image.Point{}, draw.Src)
	draw.Draw(img, image.Rect(4, 8, 27, 25), image.NewUniform(white), image.Point{}, draw.Src)
	draw.Draw(img, image.Rect(6, 10, 25, 23), image.NewUniform(dark), image.Point{}, draw.Src)
	draw.Draw(img, image.Rect(27, 13, 30, 20), image.NewUniform(white), image.Point{}, draw.Src)

	usableWidth := 17
	level := int(math.Round(float64(usableWidth) * float64(stateOfCharge) / 100))
	if level > 0 {
		draw.Draw(img, image.Rect(7, 11, 7+level, 22), image.NewUniform(fill), image.Point{}, draw.Src)
	}

	var encoded bytes.Buffer
	if err := png.Encode(&encoded, img); err != nil {
		fmt.Fprintf(os.Stderr, "encode tray icon: %v\n", err)
		return nil
	}
	return encoded.Bytes()
}
