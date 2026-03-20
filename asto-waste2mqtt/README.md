# asto-waste2mqtt

A stateless Go-based bridge that fetches waste collection dates from ASTO (Abfall-Sammel- und Transportverband Oberberg) and publishes them to MQTT using Home Assistant Auto-Discovery.

## Overview

This service polls the ASTO ICS calendar for a specific district and extracts the next collection dates for:
* Bio-waste (Biotonne)
* Residual waste (Residual waste)
* Paper (Paper)
* Recyclables (Yellow Sack)

## Features

* **ICS Parsing**: Automatically downloads and parses the official ASTO iCal export.
* **HA Auto-Discovery**: Registers sensors automatically in Home Assistant as `date` entities.
* **Cloud Native**: Multi-stage build resulting in a minimal scratch-based container (<10MB).
* **Stateless**: Polling based architecture (default: 12h interval).

## Configuration

The application is configured via CLI flags:

| Flag | Description | Default |
| :--- | :--- | :--- |
| `--mqtt-host` | MQTT broker hostname | `mosquitto.mqtt.svc.cluster.local` |
| `--mqtt-port` | MQTT broker port | `1883` |
| `--district-id` | ASTO District ID (e.g. 57589 for Wiehl) | `57589` |
| `--poll-interval` | Interval to fetch the calendar | `12h` |
| `--log-level` | Log level (debug, info, warn, error) | `info` |

## Deployment

This service is deployed via GitOps (FluxCD) in the Homelab cluster. 
The image is hosted on GitHub Container Registry (`ghcr.io`).

### Local Development

```bash
go build -o service .
./service --mqtt-host=localhost --district-id=57589
```

## License

MIT
