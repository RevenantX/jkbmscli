# jkbmscli

`jkbmscli` is a Linux command-line and system-tray monitor for JK-BMS batteries.
It connects to a battery over Bluetooth Low Energy (BLE), requests its live
telemetry, and reports the battery readings without needing the Android app.

## Requirements

- Linux with Bluetooth LE support and an active BlueZ system D-Bus service
- Go 1.24 or newer to build from source
- For tray mode, a desktop session that supports system-tray icons

## Build

From this directory, build the executable with:

```sh
go build -o jkbmscli .
```

## Command-line mode

Pass the battery's Bluetooth MAC address as the only required argument:

```sh
./jkbmscli 00:11:22:33:44:55
```

The program connects to the battery, subscribes to BLE notifications, and
prints readings as telemetry arrives. Press `Ctrl+C` to disconnect and exit.

Example:

```text
SOC: 18% | Current (A): -7.4 | Power (W): 186.0 | Rem. Cap.(Ah): 17.1 | Capacity(Ah): 100.0 | Est. runtime: 2h 19m
```

Current is signed: a positive value indicates charging and a negative value
indicates discharging. Power is displayed as an unsigned magnitude.

## System-tray mode

Add `--tray` before or after the Bluetooth address:

```sh
./jkbmscli --tray 00:11:22:33:44:55
```

Tray mode shows a battery icon with the state-of-charge percentage. The tooltip
contains SOC, current, power, remaining and configured capacity, and estimated
runtime. Battery readings are not written to standard output in tray mode.
Choose **Quit** from the tray menu to disconnect and exit.

## Reading details

- **SOC** is the state of charge reported by the BMS.
- **Current (A)** is signed; positive means charging, negative means
  discharging.
- **Power (W)** is reported without a sign for either direction.
- **Rem. Cap. (Ah)** is the remaining capacity reported by the BMS.
- **Capacity (Ah)** is the configured capacity reported in the main-page data.
- **Estimated runtime** is remaining Ah divided by the absolute current in
  amps, rounded to the nearest minute. It is an approximate discharge estimate;
  it shows `Charging` while current is positive and `N/A` when current is zero
  or remaining capacity is unavailable.

The program uses the BlueZ system D-Bus to discover and connect to the BMS. It
does not request or send the Android app's password. The BMS must be reachable
and allow the connection for monitoring to work.

## Development

Run the tests and static checks from this directory:

```sh
go test ./...
go vet ./...
```
