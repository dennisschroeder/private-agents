# Homelab Cluster Dokumentation

Dies ist die zentrale Dokumentation für den lokalen Kubernetes-Cluster auf Basis von Talos Linux.

## Architektur & Hardware
- **OS**: Talos Linux (v1.12.5)
- **Kubernetes Version**: v1.35.2
- **CNI**: Flannel (Standard)
- **Nodes**:
  - `192.168.178.88` - Lenovo ThinkCentre M700 (AMD64) - **Control Plane** (Taints entfernt für Workloads)
  - `192.168.178.139` - Lenovo ThinkCentre M700 (AMD64) - **Worker**
  - *(Geplant)*: Raspberry Pi 4 (ARM64) als dedizierter IoT/USB-Gateway-Worker.

## Kern-Infrastruktur
- **Storage (Longhorn)**: Eingerichtet unter `longhorn-system`. Mounts auf `/var/mnt/longhorn` gepatcht via `talosctl`. Nutzt die lokalen Festplatten der Lenovos.
- **LoadBalancer (MetalLB)**: Vergibt IPs aus dem Pool `192.168.178.240-250`.
- **Ingress (NGINX)**: Läuft auf IP `192.168.178.240`.
- **mDNS (external-mdns)**: Sendet automatisch Ingress-Hosts in das lokale Netz, um Domains wie `ha.local` und `longhorn.local` ohne DNS-Server nutzbar zu machen.

## Smart Home & IoT Architektur (Zero-Downtime Migration)
Das Smart Home wurde in eine verteilte Container-Architektur entkoppelt:

1. **MQTT Broker (Eclipse Mosquitto)**: 
   - Läuft im Cluster unter `192.168.178.241` (Port 1883) via MetalLB.
   - Fungiert als zentrales Nervensystem für alle Funk-Sensoren.
2. **Die MQTT-Bridge (Shadowing)**: 
   - Das alte System (Raspberry Pi) wurde so konfiguriert, dass das lokale Mosquitto-Add-on alle Nachrichten parallel in den neuen Cluster-Broker (auf `.241`) spiegelt (`bridge.conf`). So wird Live-Traffic ohne Ausfallzeit repliziert.
3. **USB-Funk-Architektur (Geplant)**:
   - `zigbee2mqtt` und `zwave-js-ui` liegen vorbereitet im Cluster (`replicas: 0`).
   - Sie sind mit einem `nodeSelector` auf `kubernetes.io/arch: arm64` festgesetzt.
   - Sobald der Raspberry Pi dem Cluster beitritt, übernehmen diese Container die USB-Sticks `/dev/ttyACM0` und `/dev/ttyACM1`.
4. **Home Assistant**:
   - Läuft im Cluster unter `ha.local`.
   - `hostNetwork: true` wurde gesetzt, um lokale Integrationen wie **Homematic (CCU)** (IP `192.168.178.29`) nativ per XML-RPC Callbacks (Port 2010 etc.) betreiben zu können.
   - Die alte `homematic` Config wurde in die `configuration.yaml` injiziert.

## Observability (Monitoring & Logging)
Der Cluster wird vollumfänglich überwacht, um Metriken und Logs zentral zur Verfügung zu stellen:

1. **Prometheus & Grafana**:
   - Installiert via `kube-prometheus-stack`.
   - Limitiert auf 1.5GB RAM und 10GB Longhorn-Speicher (7 Tage Retention).
   - Grafana ist unter `grafana.local` erreichbar.
2. **Loki & Promtail**:
   - Installiert via `loki-stack` für zentrales Container-Logging.
   - **WICHTIGER WORKAROUND**: Das offizielle Helm-Chart installiert eine veraltete Loki-Version (2.6.x), die mit neuen Grafana-Versionen (v11+) Inkompatibilitäten aufweist (`syntax error: unexpected IDENTIFIER`). Das Image muss zwingend auf `2.9.3` überschrieben werden (`loki-values.yaml`).
   - Die Datenquelle in Grafana wird via Helm `additionalDataSources` vollautomatisch samt `X-Scope-OrgID: 1` injiziert.
3. **Home Assistant Logging**:
   - Home Assistant schreibt standardmäßig alles lautlos in `/config/home-assistant.log` und ist für Promtail auf `stdout` unsichtbar.
   - **Workaround:** Im Deployment wurde ein `log-tailer` Sidecar-Container (`busybox`) hinzugefügt, der die Datei ausliest und per `tail -F` auf seinen `stdout` streamt, sodass die Logs in Loki/Grafana auftauchen.

## Wichtige Konfigurationsdateien
Alle YAML-Manifeste und Configs liegen lokal unter `~/private/homelab/talos/`:
- `talosconfig`: Die API-Konfiguration für `talosctl`.
- `kubeconfig`: Der Admin-Schlüssel für den Kubernetes-Zugriff via `kubectl`.
- `controlplane.yaml` / `worker.yaml`: Die Talos-Maschinenkonfigurationen.
- `longhorn.yaml`, `metallb-config.yaml`, `mosquitto.yaml`, `homeassistant.yaml`, `iot-stack.yaml`.

## Bekannte Probleme & Workarounds
**macOS Netzwerk-Problem (Tailscale/Firewall):**
Wenn Tailscale aktiv ist, blockiert der macOS-Kernel oft native Go-Anwendungen (`talosctl`, `kubectl`) mit einem `no route to host` Fehler, selbst wenn das Gerät erreichbar ist.
*Lösung:* Ein Docker-Workaround (`hk` Alias) oder Deaktivierung von Tailscale (gefolgt von WLAN-Neustart) ist erforderlich.
```bash
alias hk='docker run --rm -it -v ~/private/homelab/talos:/talos -e KUBECONFIG=/talos/kubeconfig bitnami/kubectl:latest'
```

## Nächste anstehende Schritte
1. Hinzufügen des Raspberry Pi (ARM64) als 3. Node.
2. Kopieren der alten Zigbee/Z-Wave Konfiguration in die Longhorn-Laufwerke (`zigbee2mqtt-data`, `zwave-data`).
3. Hochfahren der IoT-Pods (`replicas: 1`).
