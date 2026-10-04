# Security

## Supported versions

Only the latest release receives fixes. Ferry updates itself, so a fix reaches every install that keeps **Check Automatically** on.

## Reporting a vulnerability

Report privately through [GitHub private vulnerability reporting](https://github.com/1etu/ferry/security/advisories/new). Do not open a public issue. Include the Ferry version (Settings), the Windows and iOS versions, and the steps to reproduce.

## What Ferry claims

1. **Nothing leaves your network.** Ferry talks only between the phone and the PC on the same Wi-Fi, plus one HTTPS request a day to GitHub for updates (off in Settings).
2. **Only devices you approve.** Every phone is approved on the PC; approval can be revoked.
3. **Encrypted from iPhone to PC.** What the phone sends is encrypted end to end with a key that only that phone and that PC hold, and every piece is verified before it is written. File names are encrypted in both directions.
4. **Verified updates.** Updates are signed; Ferry installs only what the key in it can verify.

Files sent from the PC to the phone travel unencrypted on your Wi-Fi; and someone who controls your Wi-Fi, not just listens to it, could alter the Ferry page your phone loads. Use a network you trust for things you would not say out loud on it.

## Threat model

| Attacker | Can | Cannot |
|---|---|---|
| Passive sniffer on the same Wi-Fi | See that Ferry is used, sizes and timings; see the bodies of files sent from the PC to the phone; capture the device cookie | Read uploaded bytes or any file name; use a captured cookie for uploads or name lists without the device secret; pair |
| Active attacker on the Wi-Fi (ARP spoofing, rogue access point) | Everything above, and replace the page the phone loads, which then leaks whatever it is told to leak | Be stopped by anything on a plain-HTTP origin; this is the honest boundary |
| Someone with the device cookie but not the device secret | Download offered files (plain anyway), cancel transfers | Read names, upload |

The update signing key exists only as the GitHub Actions secret `FERRY_SIGNING_KEY` and in the maintainer's password manager. The release workflow signs with it, so whoever controls the GitHub account controls updates. **Check Automatically** in Settings turns the daily check off.

## Known gaps

No TLS of any kind, no secret on the loopback owner API, and no code signing.
