# Ferry

Files between your iPhone and your PC, over your own Wi-Fi.

[![CI](https://github.com/1etu/ferry/actions/workflows/ci.yml/badge.svg)](https://github.com/1etu/ferry/actions/workflows/ci.yml) [![Release](https://img.shields.io/github/v/release/1etu/ferry)](https://github.com/1etu/ferry/releases/latest) [![License](https://img.shields.io/badge/license-MIT-black)](LICENSE)

[![Ferry pairs an iPhone, sends three photos to the PC and gets a file back](assets/demo.webp)](https://1etu.github.io/ferry/)

## Contents

- [Install](#install)
- [Pair your iPhone](#pair-your-iphone)
- [Send and receive](#send-and-receive)
- [Originals](#originals)
- [Speed](#speed)
- [Compared to others](#compared-to-others)
- [Security](#security)
- [Settings](#settings)
- [Updates](#updates)
- [Uninstall](#uninstall)
- [Build](#build)
- [License](#license)

## Install

1. Download [`Ferry.exe`](https://github.com/1etu/ferry/releases/latest/download/Ferry.exe) from the [latest release](https://github.com/1etu/ferry/releases/latest) (Windows 10 or 11).
2. Open it.
3. Click **Allow** when Windows Firewall asks.

Ferry is not code-signed, so the first run of each downloaded copy asks for confirmation. If Edge holds the download, click `…` › **Keep** › **Keep anyway**. If SmartScreen says "Windows protected your PC", click **More info** › **Run anyway**. Ferry then installs itself into `%LOCALAPPDATA%\Programs\Ferry`, adds a Start menu entry and starts with Windows.

## Pair your iPhone

1. Point the iPhone's Camera at the code in the Ferry window and tap the banner.
2. Click **Allow** on the PC.

Without a camera, open `http://<pc-name>.local:8080/` in Safari and type the code shown on the PC. To keep Ferry on the Home Screen, tap Share › **Add to Home Screen** within 24 hours of pairing; it opens paired, with no code.

## Send and receive

- **iPhone to PC:** tap **Send** and pick photos, videos or files.
- **PC to iPhone:** right-click files › **Send to** › **Send to iPhone** (on Windows 11, **Show more options** first), or **Send Files…** in the tray menu, or **Send** in the Ferry window. Tap the file on the iPhone to download it.

Received files land in `Downloads\Ferry` on the PC (change it in Settings) and in Files › Downloads on the iPhone.

## Originals

Ferry never recompresses anything: what the iPhone sends is what the PC writes. Videos arrive as stored. Photos depend on how you pick them:

- The Photo Library sends JPEG by default. For the stored HEIC, tap **Options** in the picker and choose **Current**.
- Location is removed unless you turn on **Location** in the same **Options** menu.
- **Choose File** (the Files app) sends files byte for byte.
- For JPEG everywhere instead, set Settings › Camera › Formats › **Most Compatible** on the iPhone.

## Speed

Your Wi-Fi is the limit, not Ferry. Measured on one PC with no network in between, Ferry encrypts, verifies and saves files several times faster than home Wi-Fi can carry them.

```text
One 2 GB video, iPhone to PC                                    MB/s

Ferry, measured with no network   ████████████████████████████  689
Wi-Fi 6, close to the router      ███                           ~80
Wi-Fi 5                           ██                            ~45


500 photos, 3 MB each                                           photos/s

Ferry, measured with no network   ████████████████████████████  290
Wi-Fi 6, close to the router      ███                           ~27
```

Ferry's numbers are medians of repeated runs over loopback with encryption on, measured with `tools/bench`. The Wi-Fi figures are typical real-world throughput, not lab maximums. Downloads from the PC to the phone run at about 2.6 GB/s over loopback.

## Compared to others

| | Ferry | LocalSend | PairDrop | iCloud Drive |
|---|---|---|---|---|
| Nothing to install on the iPhone | Yes | No | Yes | Yes |
| Works without internet | Yes | Yes | No¹ | No |
| No account | Yes | Yes | Yes | No |
| Resumes an interrupted transfer | Yes | No | No | Yes |
| Files never leave your network | Yes | Yes | Usually² | No |
| Open source | Yes | Yes | Yes | No |
| Runs on | Windows and iPhone | Most platforms | Any browser | Apple devices and Windows |

¹ Unless you host your own PairDrop server. ² Files go peer to peer over WebRTC, and a relay is used when a direct path fails.

As of October 2026. Spotted something wrong? [Open an issue](https://github.com/1etu/ferry/issues).

**Where Ferry is good**

- No app on the iPhone. Scan one QR code and Safari does the rest.
- Works with the internet unplugged. Nothing leaves your network.
- Huge files survive a locked screen and a dropped Wi-Fi.
- One small exe that installs and updates itself.

**Where it falls short**

- Windows and iPhone only, for now.
- Files sent from the PC to the phone are not encrypted on your Wi-Fi.
- Both devices must be on the same network.
- The exe is unsigned, so Windows asks once before the first run.
- With Lockdown Mode on, iPhone uploads are much slower.

## Security

- **Nothing leaves your network.** Ferry talks only between the phone and the PC on the same Wi-Fi, plus one HTTPS request a day to GitHub for updates (off in Settings).
- **Only devices you approve.** Every phone is approved on the PC; approval can be revoked.
- **Encrypted from iPhone to PC.** What the phone sends is encrypted end to end with a key that only that phone and that PC hold, and every piece is verified before it is written. File names are encrypted in both directions.
- **Verified updates.** Updates are signed; Ferry installs only what the key in it can verify.

Files sent from the PC to the phone travel unencrypted on your Wi-Fi; and someone who controls your Wi-Fi, not just listens to it, could alter the Ferry page your phone loads. Use a network you trust for things you would not say out loud on it.

With Lockdown Mode on, iPhone uploads run at a few MB/s, because iOS disables the JavaScript JIT that Ferry's encryption needs.

Threat model and reporting: [SECURITY.md](SECURITY.md).

## Settings

- **Name:** the PC's name as the iPhone shows it.
- **Start at Login:** on by default, so Ferry is there after a restart.
- **Received Files:** the folder incoming files go to.
- **Check Automatically:** the daily update check.
- **Ferry *version*:** **Check for Updates**, or **Restart to Update** when one is ready.

`port`, `maxUploadBytes`, `reserveBytes` and `maxActiveUploadsPerDevice` live in `%LOCALAPPDATA%\Ferry\config.json`; edit it while Ferry is not running.

## Updates

Once a day, starting a minute after launch, Ferry fetches the latest release's manifest from GitHub. It installs an update only if the manifest's signature verifies against the key built into Ferry and the download matches the manifest's size and SHA-256; older versions are refused. **Check for Updates** in Settings checks now, and **Check Automatically** turns the daily check off. When an update is ready, **Restart to Update** in Settings or the tray installs it.

## Uninstall

Windows Settings › Apps › Installed apps › Ferry › **Uninstall**, or run `Ferry.exe uninstall`. This removes the program, its shortcuts and `%LOCALAPPDATA%\Ferry`. Received files stay.

## Build

Go 1.27.1, Node 22 and pnpm 10. Commands are for Git Bash or any POSIX shell.

```sh
pnpm -C web install
pnpm -C web build && CGO_ENABLED=0 go build -o bin/Ferry.exe ./cmd/ferry
```

A build without `-ldflags "-X main.version=<version>"` is a dev build: it neither installs itself nor updates.

Development, with the API on port 8080 and Vite in front of it:

```sh
FERRY_DEV=1 go run ./cmd/ferry
pnpm -C web dev
```

| Variable | Effect |
|---|---|
| `FERRY_DATA_DIR` | Data directory instead of `%LOCALAPPDATA%\Ferry` |
| `FERRY_PORT` | Port instead of the one in `config.json` |
| `FERRY_HEADLESS=1` | No tray and no window |
| `FERRY_DEV=1` | Trusts the Vite proxy, relaxes the port check, logs text to stderr |

Checks, tests and the rules CI enforces are in [CONTRIBUTING.md](CONTRIBUTING.md).

## License

[MIT](LICENSE)
