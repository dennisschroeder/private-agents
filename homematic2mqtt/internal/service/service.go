package service

import (
	"context"
	"log/slog"
	"strconv"
	"time"

	"github.com/dennisschroeder/homematic2mqtt/internal/mqtt"
	"github.com/dennisschroeder/homematic2mqtt/internal/source"
)

type Device struct {
	ID   string
	Name string
	MAC  string
}

type Config struct {
	ShutterMapping map[string]string
	DryRun         bool
}

type Service struct {
	config    Config
	logger    *slog.Logger
	source    *source.Client
	mqtt      *mqtt.Client
	lastState map[string]int
}

func New(cfg Config, logger *slog.Logger, src *source.Client, mq *mqtt.Client) *Service {
	return &Service{
		config:    cfg,
		logger:    logger,
		source:    src,
		mqtt:      mq,
		lastState: make(map[string]int),
	}
}

func (s *Service) Run(ctx context.Context) error {
	s.logger.Info("Starting Homematic Shutter Bridge", "dry_run", s.config.DryRun)

	// Initial Sync
	s.logger.Info("Performing initial device sync...")
	devices, err := s.source.ListDevices(ctx)
	if err != nil {
		s.logger.Error("Failed to list devices for initial sync", "error", err)
	} else {
		s.logger.Info("Syncing devices", "count", len(devices))
		for _, devAddr := range devices {
			s.logger.Info("Syncing device", "address", devAddr)
			level, err := s.source.GetValue(ctx, devAddr, "LEVEL")
			if err == nil {
				deviceName := s.source.GetDeviceName(devAddr)
				s.handleLevelUpdate(devAddr, deviceName, level)
			} else {
				s.logger.Warn("Failed to get level for device", "address", devAddr, "error", err)
			}
		}
	}

	// Subscribe to MQTT Commands
	if !s.config.DryRun && s.mqtt != nil {
		for i := 0; i < 30; i++ {
			if s.mqtt.IsConnected() {
				s.logger.Info("MQTT connected, subscribing to commands")
				break
			}
			s.logger.Info("Waiting for MQTT connection...", "attempt", i+1)
			time.Sleep(1 * time.Second)
		}

		if !s.mqtt.IsConnected() {
			s.logger.Error("MQTT still not connected after timeout, skipping subscription")
		} else {
			err := s.mqtt.SubscribeCommands(func(id, cmd string, payload []byte) {
				s.handleMQTTCommand(ctx, id, cmd, payload)
			})
			if err != nil {
				s.logger.Error("Failed to subscribe to MQTT commands", "error", err)
			}
		}
	}

	events := s.source.Events()
	for {
		select {
		case <-ctx.Done():
			return nil
		case event := <-events:
			deviceName := s.source.GetDeviceName(event.DeviceID)
			if event.Key == "LEVEL" {
				s.handleLevelUpdate(event.DeviceID, deviceName, event.Value)
			}
		}
	}
}

func (s *Service) handleLevelUpdate(id, name string, value interface{}) {
	var floatVal float64
	switch v := value.(type) {
	case float64:
		floatVal = v
	case int:
		floatVal = float64(v)
	default:
		return
	}

	pos := int(floatVal * 100)
	s.logger.Info("Level update processing", "id", id, "name", name, "pos", pos)

	if s.config.DryRun {
		s.logger.Info("[DRY-RUN] Shutter position", "name", name, "id", id, "pos", pos)
	} else {
		// Auto-Discovery on first sight
		if _, exists := s.config.ShutterMapping[id]; !exists {
			s.logger.Info("New device discovered, sending discovery", "id", id, "name", name)
			s.mqtt.PublishShutterDiscovery(id, name)
			s.config.ShutterMapping[id] = name
		}
		s.mqtt.PublishPosition(id, pos)
		s.logger.Info("Published position", "name", name, "pos", pos)
	}
}

func (s *Service) handleMQTTCommand(ctx context.Context, id, cmd string, payload []byte) {
	s.logger.Info("Received MQTT command", "id", id, "cmd", cmd, "payload", string(payload))

	switch cmd {
	case "set":
		p := string(payload)
		var val float64
		if p == "OPEN" { val = 1.0 } else if p == "CLOSE" { val = 0.0 } else { return }
		s.source.SetValue(ctx, id, "LEVEL", val)
	case "set_position":
		pos, err := strconv.Atoi(string(payload))
		if err == nil {
			val := float64(pos) / 100.0
			s.source.SetValue(ctx, id, "LEVEL", val)
		}
	case "set_stop":
		s.source.SetValue(ctx, id, "STOP", true)
	}
}
