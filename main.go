package main

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"os"
	"os/signal"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/godbus/dbus/v5"
)

const (
	bluetoothReqDeadlineTime = 15 * time.Second
	bluetoothReqTickerTime   = 200 * time.Millisecond
)

var (
	realtimeTelemetryRequest = mustDecodeHex("AA5590EB97000000000000000000000000000011")
	mainPageDataRequest      = mustDecodeHex("AA5590EB96000000000000000000000000000010")
)

func main() {
	address, trayMode, err := parseArguments(os.Args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "Usage: %s [--tray] <bluetooth-device-address>\n", os.Args[0])
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}

	address, err = normalizeAddress(address)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if !trayMode {
		if err := monitor(ctx, address, printReading); err != nil && !errors.Is(err, context.Canceled) {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	err = runTray(ctx, stop, address)
	if err != nil && !errors.Is(err, context.Canceled) {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func parseArguments(args []string) (address string, trayMode bool, err error) {
	for _, arg := range args {
		if arg == "--tray" {
			trayMode = true
			continue
		}
		if strings.HasPrefix(arg, "-") {
			return "", false, fmt.Errorf("unknown option %q", arg)
		}
		if address != "" {
			return "", false, errors.New("expected exactly one Bluetooth device address")
		}
		address = arg
	}
	if address == "" {
		return "", false, errors.New("Bluetooth device address is required")
	}
	return address, trayMode, nil
}

func printReading(reading batteryReading) {
	fmt.Printf(
		"SOC: %d%% | Current (A): %+.1f | Power (W): %.1f | Rem. Cap.(Ah): %.1f | Capacity(Ah): %.1f | Est. runtime: %s\n",
		reading.stateOfCharge,
		reading.currentAmps,
		reading.powerWatts,
		reading.remainingCapacityAh,
		reading.capacityAh,
		reading.estimatedRuntime(),
	)
}

func normalizeAddress(address string) (string, error) {
	mac, err := net.ParseMAC(address)
	if err != nil || len(mac) != 6 {
		return "", fmt.Errorf("invalid Bluetooth device address %q; expected a MAC address", address)
	}
	return strings.ToUpper(mac.String()), nil
}

func mustDecodeHex(value string) []byte {
	decoded, err := hex.DecodeString(value)
	if err != nil {
		panic(err)
	}
	return decoded
}

func monitor(ctx context.Context, address string, onReading func(batteryReading)) (resultErr error) {
	conn, err := dbus.ConnectSystemBus()
	if err != nil {
		return fmt.Errorf("connect to the system D-Bus: %w", err)
	}
	defer conn.Close()

	objects, err := getManagedObjects(conn)
	if err != nil {
		return err
	}
	adapterPath, ok := findObjectPath(objects, adapterInterface)
	if !ok {
		return errors.New("BlueZ has no Bluetooth adapter")
	}

	devicePath, found := findDevicePath(objects, address)
	if !found {
		devicePath, resultErr = discoverDevice(ctx, conn, adapterPath, address, objects)
		if resultErr != nil {
			return resultErr
		}
	}

	device := conn.Object(bluezService, devicePath)
	connectedHere := false
	if !getBoolProperty(objects, devicePath, deviceInterface, "Connected") {
		fmt.Fprintf(os.Stderr, "Connecting to %s…\n", address)
		if err := call(device, deviceInterface+".Connect"); err != nil {
			return fmt.Errorf("connect to %s: %w", address, err)
		}
		connectedHere = true
	}
	defer func() {
		if connectedHere {
			if err := call(device, deviceInterface+".Disconnect"); err != nil && resultErr == nil {
				resultErr = fmt.Errorf("disconnect from %s: %w", address, err)
			}
		}
	}()

	characteristicPath, err := findNotifyCharacteristic(ctx, conn, devicePath)
	if err != nil {
		return err
	}

	matchOptions := []dbus.MatchOption{
		dbus.WithMatchObjectPath(characteristicPath),
		dbus.WithMatchInterface(propertiesInterface),
		dbus.WithMatchMember("PropertiesChanged"),
	}
	if err := conn.AddMatchSignal(matchOptions...); err != nil {
		return fmt.Errorf("subscribe to GATT notifications: %w", err)
	}
	defer func() {
		if err := conn.RemoveMatchSignal(matchOptions...); err != nil && resultErr == nil {
			resultErr = fmt.Errorf("remove GATT notification subscription: %w", err)
		}
	}()

	signals := make(chan *dbus.Signal, 64)
	conn.Signal(signals)
	defer conn.RemoveSignal(signals)

	characteristic := conn.Object(bluezService, characteristicPath)
	if err := call(characteristic, characteristicInterface+".StartNotify"); err != nil {
		return fmt.Errorf("enable GATT notifications: %w", err)
	}
	notifying := true
	defer func() {
		if notifying {
			if err := call(characteristic, characteristicInterface+".StopNotify"); err != nil && resultErr == nil {
				resultErr = fmt.Errorf("stop GATT notifications: %w", err)
			}
		}
	}()

	if err := wait(ctx, 80*time.Millisecond); err != nil {
		return err
	}
	if err := writeCommand(characteristic, realtimeTelemetryRequest); err != nil {
		return fmt.Errorf("request realtime telemetry: %w", err)
	}

	fmt.Fprintln(os.Stderr, "Waiting for battery charge…")
	parser := &frameParser{}
	mainPageRequestSent := false
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case signal := <-signals:
			if signal == nil || signal.Path != characteristicPath {
				continue
			}
			value, ok := notificationValue(signal)
			if !ok {
				continue
			}

			var completedFrameTypes []byte
			readings := parser.append(value, func(frameType byte) {
				completedFrameTypes = append(completedFrameTypes, frameType)
			})
			for _, reading := range readings {
				onReading(reading)
			}
			if !mainPageRequestSent && slices.Contains(completedFrameTypes, 0x03) {
				if err := wait(ctx, 100*time.Millisecond); err != nil {
					return err
				}
				if err := writeCommand(characteristic, mainPageDataRequest); err != nil {
					return fmt.Errorf("request main-page data: %w", err)
				}
				mainPageRequestSent = true
			}
		}
	}
}

func wait(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
