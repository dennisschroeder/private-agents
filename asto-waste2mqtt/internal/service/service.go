package service

import (
	"context"
	"log/slog"
	"time"

	"github.com/dennisschroeder/asto-waste2mqtt/internal/mqtt"
	"github.com/dennisschroeder/asto-waste2mqtt/internal/source"
)

type Config struct {
	PollInterval time.Duration
}

type Service struct {
	config Config
	logger *slog.Logger
	mqtt   *mqtt.Client
	source *source.Client
}

func New(cfg Config, logger *slog.Logger, srcClient *source.Client, mqttClient *mqtt.Client) *Service {
	return &Service{
		config: cfg,
		logger: logger,
		mqtt:   mqttClient,
		source: srcClient,
	}
}

func (s *Service) Run(ctx context.Context) error {
	s.logger.Info("Starting service loop", "poll_interval", s.config.PollInterval)

	// Wait for MQTT to settle
	time.Sleep(2 * time.Second)

	// Discovery
	s.mqtt.PublishDiscovery("organic_waste", "Biotonne", "", "date", "")
	s.mqtt.PublishDiscovery("residual_waste", "Restabfall", "", "date", "")
	s.mqtt.PublishDiscovery("paper_waste", "Altpapier", "", "date", "")
	s.mqtt.PublishDiscovery("recyclable_waste", "Gelber Sack", "", "date", "")

	ticker := time.NewTicker(s.config.PollInterval)
	defer ticker.Stop()

	if err := s.pollAndPublish(ctx); err != nil {
		s.logger.Error("Initial poll failed", "error", err)
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := s.pollAndPublish(ctx); err != nil {
				s.logger.Error("Error during polling cycle", "error", err)
			}
		}
	}
}

func (s *Service) pollAndPublish(ctx context.Context) error {
	data, err := s.source.FetchData(ctx)
	if err != nil {
		return err
	}
	for key, value := range data {
		s.mqtt.PublishState(key, value)
	}
	return nil
}
