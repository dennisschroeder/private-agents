package cmd

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/dennisschroeder/asto-waste2mqtt/internal/mqtt"
	"github.com/dennisschroeder/asto-waste2mqtt/internal/service"
	"github.com/dennisschroeder/asto-waste2mqtt/internal/source"
)

var (
	RootCmd = &cobra.Command{
		Use:   "asto-waste2mqtt",
		Short: "ASTO Waste Collection to MQTT bridge",
		Run:   runRoot,
	}

	rootArgs struct {
		logLevel string
		mqtt     mqtt.Config
		source   source.Config
		service  service.Config
	}
)

func init() {
	f := RootCmd.Flags()

	// Generic flags
	f.StringVar(&rootArgs.logLevel, "log-level", "info", "Log level (debug, info, warn, error)")

	// MQTT flags
	f.StringVar(&rootArgs.mqtt.Host, "mqtt-host", "mosquitto.mqtt.svc.cluster.local", "MQTT broker host")
	f.IntVar(&rootArgs.mqtt.Port, "mqtt-port", 1883, "MQTT broker port")
	f.StringVar(&rootArgs.mqtt.ClientID, "mqtt-client-id", "asto-waste2mqtt", "MQTT Client ID")

	// ASTO source flags
	f.StringVar(&rootArgs.source.DistrictID, "district-id", "57589", "District ID for the ASTO calendar")

	// Service specific flags
	f.DurationVar(&rootArgs.service.PollInterval, "poll-interval", 12*time.Hour, "Interval to poll the ASTO calendar (default: 12h)")
}

func runRoot(cmd *cobra.Command, args []string) {
	// Setup structured logging (slog)
	var level slog.Level
	if err := level.UnmarshalText([]byte(rootArgs.logLevel)); err != nil {
		level = slog.LevelInfo
	}
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: level}))
	slog.SetDefault(logger)

	slog.Info("Starting asto-waste2mqtt...")

	// Context for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 1. Prepare MQTT Client (Target)
	mqttClient, err := mqtt.NewClient(rootArgs.mqtt, logger)
	if err != nil {
		slog.Error("Failed to initialize MQTT client", "error", err)
		os.Exit(1)
	}
	if err := mqttClient.Connect(ctx); err != nil {
		slog.Error("Failed to connect to MQTT broker", "error", err)
		os.Exit(1)
	}
	defer mqttClient.Disconnect()

	// 2. Prepare Source Client (e.g., Modbus, REST)
	srcClient, err := source.NewClient(rootArgs.source, logger)
	if err != nil {
		slog.Error("Failed to initialize source client", "error", err)
		os.Exit(1)
	}
	if err := srcClient.Connect(ctx); err != nil {
		slog.Error("Failed to connect to source device", "error", err)
		os.Exit(1)
	}
	defer srcClient.Disconnect()

	// 3. Prepare and run Main Service (Business Logic)
	svc := service.New(rootArgs.service, logger, srcClient, mqttClient)

	// Listen for OS signals to trigger graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		slog.Info("Received shutdown signal, terminating...")
		cancel()
	}()

	// Block and run
	if err := svc.Run(ctx); err != nil {
		slog.Error("Service exited with error", "error", err)
		os.Exit(1)
	}

	slog.Info("Service stopped cleanly")
}
