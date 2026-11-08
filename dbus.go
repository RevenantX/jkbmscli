package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/godbus/dbus/v5"
)

const (
	bluezService            = "org.bluez"
	objectManagerInterface  = "org.freedesktop.DBus.ObjectManager"
	adapterInterface        = "org.bluez.Adapter1"
	deviceInterface         = "org.bluez.Device1"
	characteristicInterface = "org.bluez.GattCharacteristic1"
	propertiesInterface     = "org.freedesktop.DBus.Properties"
	ffe1UUID                = "0000ffe1-0000-1000-8000-00805f9b34fb"
	ffe2UUID                = "0000ffe2-0000-1000-8000-00805f9b34fb"
)

var writeCommandOptions = map[string]dbus.Variant{
	"type": dbus.MakeVariant("command"),
}

type managedObjects map[dbus.ObjectPath]map[string]map[string]dbus.Variant

func discoverDevice(
	ctx context.Context,
	conn *dbus.Conn,
	adapterPath dbus.ObjectPath,
	address string,
	objects managedObjects,
) (devicePath dbus.ObjectPath, resultErr error) {
	adapter := conn.Object(bluezService, adapterPath)
	startedDiscovery := false
	if !getBoolProperty(objects, adapterPath, adapterInterface, "Discovering") {
		if err := call(adapter, adapterInterface+".StartDiscovery"); err != nil {
			return "", fmt.Errorf("start Bluetooth discovery: %w", err)
		}
		startedDiscovery = true
	}
	if startedDiscovery {
		defer func() {
			if err := call(adapter, adapterInterface+".StopDiscovery"); err != nil && resultErr == nil {
				resultErr = fmt.Errorf("stop Bluetooth discovery: %w", err)
			}
		}()
	}

	deadline := time.NewTimer(bluetoothReqDeadlineTime)
	defer deadline.Stop()
	ticker := time.NewTicker(bluetoothReqTickerTime)
	defer ticker.Stop()

	fmt.Fprintf(os.Stderr, "Searching for %s…\n", address)
	for {
		objects, err := getManagedObjects(conn)
		if err != nil {
			return "", err
		}
		if path, found := findDevicePath(objects, address); found {
			return path, nil
		}

		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-deadline.C:
			return "", fmt.Errorf("Bluetooth device %s was not found", address)
		case <-ticker.C:
		}
	}
}

func findNotifyCharacteristic(
	ctx context.Context,
	conn *dbus.Conn,
	devicePath dbus.ObjectPath,
) (dbus.ObjectPath, error) {
	deadline := time.NewTimer(bluetoothReqDeadlineTime)
	defer deadline.Stop()
	ticker := time.NewTicker(bluetoothReqTickerTime)
	defer ticker.Stop()

	for {
		objects, err := getManagedObjects(conn)
		if err != nil {
			return "", err
		}
		if path, found := findNotifyCharacteristicPath(objects, devicePath); found {
			return path, nil
		}

		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-deadline.C:
			return "", errors.New("BlueZ did not expose a JK-BMS notification characteristic")
		case <-ticker.C:
		}
	}
}

func getManagedObjects(conn *dbus.Conn) (managedObjects, error) {
	var objects managedObjects
	err := conn.Object(bluezService, dbus.ObjectPath("/")).
		Call(objectManagerInterface+".GetManagedObjects", 0).
		Store(&objects)
	if err != nil {
		return nil, fmt.Errorf("read BlueZ GATT objects: %w", err)
	}
	return objects, nil
}

func findObjectPath(objects managedObjects, interfaceName string) (dbus.ObjectPath, bool) {
	for path, interfaces := range objects {
		if _, ok := interfaces[interfaceName]; ok {
			return path, true
		}
	}
	return "", false
}

func findDevicePath(objects managedObjects, address string) (dbus.ObjectPath, bool) {
	for path, interfaces := range objects {
		properties, ok := interfaces[deviceInterface]
		if !ok {
			continue
		}
		if getString(properties, "Address") == address {
			return path, true
		}
	}
	return "", false
}

func findNotifyCharacteristicPath(
	objects managedObjects,
	devicePath dbus.ObjectPath,
) (dbus.ObjectPath, bool) {
	for _, uuid := range []string{ffe1UUID, ffe2UUID} {
		for path, interfaces := range objects {
			properties, ok := interfaces[characteristicInterface]
			if !ok || !strings.EqualFold(getString(properties, "UUID"), uuid) {
				continue
			}
			if !hasNotifyFlag(properties) {
				continue
			}
			servicePath, ok := getObjectPath(properties, "Service")
			if ok && strings.HasPrefix(string(servicePath), string(devicePath)+"/") {
				return path, true
			}
		}
	}
	return "", false
}

func hasNotifyFlag(properties map[string]dbus.Variant) bool {
	value, ok := properties["Flags"]
	if !ok {
		return false
	}
	flags, ok := value.Value().([]string)
	if !ok {
		return false
	}
	for _, flag := range flags {
		if flag == "notify" {
			return true
		}
	}
	return false
}

func getString(properties map[string]dbus.Variant, name string) string {
	value, ok := properties[name]
	if !ok {
		return ""
	}
	result, _ := value.Value().(string)
	return result
}

func getBoolProperty(
	objects managedObjects,
	path dbus.ObjectPath,
	interfaceName string,
	name string,
) bool {
	properties, ok := objects[path][interfaceName]
	if !ok {
		return false
	}
	value, ok := properties[name]
	if !ok {
		return false
	}
	result, _ := value.Value().(bool)
	return result
}

func getObjectPath(properties map[string]dbus.Variant, name string) (dbus.ObjectPath, bool) {
	value, ok := properties[name]
	if !ok {
		return "", false
	}
	path, ok := value.Value().(dbus.ObjectPath)
	return path, ok
}

func notificationValue(signal *dbus.Signal) ([]byte, bool) {
	if signal.Name != propertiesInterface+".PropertiesChanged" || len(signal.Body) < 2 {
		return nil, false
	}
	interfaceName, ok := signal.Body[0].(string)
	if !ok || interfaceName != characteristicInterface {
		return nil, false
	}
	changed, ok := signal.Body[1].(map[string]dbus.Variant)
	if !ok {
		return nil, false
	}
	value, ok := changed["Value"]
	if !ok {
		return nil, false
	}
	bytes, ok := value.Value().([]byte)
	return bytes, ok
}

func writeCommand(characteristic dbus.BusObject, command []byte) error {
	return characteristic.Call(
		characteristicInterface+".WriteValue",
		0,
		command,
		writeCommandOptions,
	).Err
}

func call(object dbus.BusObject, method string) error {
	return object.Call(method, 0).Err
}
