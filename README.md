# macOSStat

[![build](https://github.com/yuriyskliarenko/macOSStat/actions/workflows/build.yml/badge.svg)](https://github.com/yuriyskliarenko/macOSStat/actions/workflows/build.yml)

English | [Українська](README.uk.md)

A native system inspector for macOS in a single binary: a window, a menu bar item and a command-line interface.
It collects hardware, kernel, battery, sensor and network data in one place and explains what the numbers mean.

No `sudo`, no third-party Go modules, no network requests.

![macOSStat overview window](docs/overview-en.png)

## Features

| Area | What it shows |
|---|---|
| Diagnosis | Plain-language verdicts on memory, swap, CPU, temperatures, disks, battery and charging, each with its threshold |
| Live rates | Swap, memory compression and disk throughput; average disk writes per day |
| Battery | Cycles, capacity, wear forecast, negotiated versus actual charger power |
| Sensors | Temperatures, fan speeds, system power |
| Wake-ups | How often the Mac wakes, which process requested it, charge lost during sleep |
| Network | IP address, router, DNS, traffic; Wi-Fi link rate, channel, signal and noise |
| CPU passport | Available Arm instruction set extensions and what they are used for |
| Snapshots | Saves `sysctl` parameters and compares them later |
| Hardware and limits | CPU, GPU, memory, disks, displays, devices, security settings, kernel limits |

The interface is available in English and Ukrainian and follows the system light or dark appearance.

## Requirements

- macOS 11 or later (build target)
- Apple Silicon or Intel (universal binary)

Development and testing are done on Apple Silicon with macOS 27. Intel and older macOS versions are built but not yet tested.

## Installation

### Terminal

```sh
curl -fL https://github.com/yuriyskliarenko/macOSStat/releases/latest/download/macOSStat.tar.gz | tar -xz
./macOSStat
```

### Browser

Download `macOSStat.tar.gz` from the [latest release](https://github.com/yuriyskliarenko/macOSStat/releases/latest), then run:

```sh
cd ~/Downloads
tar -xzf macOSStat.tar.gz                  # skip if the browser has already unpacked it
xattr -d com.apple.quarantine macOSStat    # "No such xattr" is fine
./macOSStat
```

### Gatekeeper

The binary is not notarized by Apple, so macOS blocks copies downloaded with a browser.
Remove the quarantine attribute as shown above, or try to open the app once and then choose
**System Settings → Privacy & Security → Open Anyway**. Do this only for software you trust.
Managed Macs may not allow it.

### Optional: add to PATH

```sh
sudo mkdir -p /usr/local/bin
sudo mv macOSStat /usr/local/bin/
```

After this, use `macOSStat` instead of `./macOSStat`.

## Usage

Run without arguments to open the window and the menu bar item:

```sh
./macOSStat
```

Closing the window keeps the app in the menu bar; quit with Cmd+Q or from the menu.
Opening the binary from Finder also opens a Terminal window, which must stay open while the app runs.

### Command line

```sh
./macOSStat --cli                             # overview
./macOSStat --cli diagnosis                   # one category
./macOSStat --cli all --json                  # everything as JSON
./macOSStat --cli watch -n 1 sensors          # refresh every second (default: 5)
./macOSStat --cli list swap                   # search all sysctl parameters
./macOSStat --cli get hw.memsize vm.swapusage # specific keys or subtrees
./macOSStat --cli snapshot save before        # save a snapshot
./macOSStat --cli snapshot diff before        # compare it with the current state
./macOSStat --help
```

Categories: `summary`, `diagnosis`, `system`, `cpu`, `passport`, `memory`, `disks`, `rates`, `battery`, `sensors`,
`wakes`, `devices`, `network`, `security`, `limits`, `all`.

### Language

Ukrainian is used if it is the first system language, English otherwise. To change it, use the selector in the window or:

```sh
./macOSStat --cli lang uk      # save the choice: en or uk
./macOSStat --cli --lang en    # this run only
```

## Data and privacy

macOSStat does not change system settings and makes no network requests.
Its own files are stored in `~/Library/Application Support/macOSStat`:

- battery history, one record per 12 hours while the app is running;
- snapshots created by the user;
- the selected language.

Snapshots contain the hostname and other identifiers. Review them before sharing.

To uninstall, quit the app, delete the binary and remove that directory.

## Limitations

- Temperatures rely on an undocumented Apple API. Available sensors depend on the model and macOS version and may be missing on Intel.
- Most diagnosis thresholds are rules of thumb rather than limits published by Apple.
- Wake-up sources are inferred from the order of power log entries.
- The Wi-Fi network name is not shown because macOS requires location access for it.

## Building from source

Requires Go 1.23 or later and the Xcode Command Line Tools.

```sh
git clone https://github.com/yuriyskliarenko/macOSStat.git
cd macOSStat
./build.sh
```

| Command | Result |
|---|---|
| `./build.sh` | `./macOSStat` for the current Mac |
| `./build.sh release` | Universal `dist/macOSStat` and `dist/macOSStat.tar.gz` |
| `./build.sh app` | `dist/macOSStat.app` and `dist/macOSStat.zip` |
| `./build.sh check` | Formatting, `go vet` and tests |
| `./build.sh clean` | Removes build output |

GitHub Actions builds every push to `main` and every pull request. Pushing a `v*` tag publishes a release.

## License

[MIT](LICENSE)
