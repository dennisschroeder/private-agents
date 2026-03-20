package cmd

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/dennisschroeder/fritz-presence2mqtt/internal/mqtt"
	"github.com/dennisschroeder/fritz-presence2mqtt/internal/service"
	"github.com/dennisschroeder/fritz-presence2mqtt/internal/source"
)

var (
	RootCmd = &cobra.Command{
		Use:   "fritz-presence2mqtt",
		Short: "FritzBox presence bridge via TR-064 to MQTT",
		Run:   runRoot,
	}

	rootArgs struct {
		logLevel string
		dryRun   bool
		mqtt     mqtt.Config
		source   source.Config
		service  service.Config
		devices  []string
	}
)

func init() {
	f := RootCmd.Flags()
	f.StringVar(&rootArgs.logLevel, "log-level", "info", "Log level")
	f.BoolVar(&rootArgs.dryRun, "dry-run", false, "Log presence only, do not publish to MQTT")
	
	f.StringVar(&rootArgs.mqtt.Host, "mqtt-host", "localhost", "MQTT broker host")
	f.IntVar(&rootArgs.mqtt.Port, "mqtt-port", 1883, "MQTT broker port")
	f.StringVar(&rootArgs.mqtt.ClientID, "mqtt-client-id", "fritz-presence", "MQTT Client ID")

	f.StringVar(&rootArgs.source.Address, "fritz-host", "192.168.178.1", "FritzBox IP")
	f.StringVar(&rootArgs.source.Username, "fritz-user", "", "FritzBox Username")
	f.StringVar(&rootArgs.source.Password, "fritz-password", "", "FritzBox Password")

	f.StringSliceVar(&rootArgs.devices, "devices", []string{}, "List of devices to track in format 'Name:MAC'")
	f.DurationVar(&rootArgs.service.PollInterval, "poll-interval", 60*time.Second, "Polling interval")
	f.DurationVar(&rootArgs.service.ConsiderHomeDuration, "consider-home-duration", 3*time.Minute, "Time a device must be inactive before being marked as away")
}

func runRoot(cmd *cobra.Command, args []string) {
	var level slog.Level
	_ = level.UnmarshalText([]byte(rootArgs.logLevel))
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: level}))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var mq *mqtt.Client
	if !rootArgs.dryRun {
		mq, _ = mqtt.NewClient(rootArgs.mqtt, logger)
		if err := mq.Connect(ctx); err != nil {
			logger.Error("MQTT connection failed", "error", err)
			os.Exit(1)
		}
	} else {
		logger.Info("[DRY-RUN] MQTT connection skipped")
	}

	src, _ := source.NewClient(rootArgs.source, logger)
	
	// Pass dry-run flag to service
	rootArgs.service.DryRun = rootArgs.dryRun

	// Parse devices from flag
	var devices []service.Device
	for _, d := range rootArgs.devices {
		parts := strings.SplitN(d, ":", 2)
		if len(parts) < 2 {
			logger.Warn("Invalid device format, skipping", "input", d)
			continue
		}
		name := parts[0]
		mac := parts[1]
		
		id := strings.ToLower(strings.ReplaceAll(name, " ", "_"))
		devices = append(devices, service.Device{
			ID:   id,
			Name: name,
			MAC:  mac,
		})
	}
	rootArgs.service.Devices = devices

	svc := service.New(rootArgs.service, logger, src, mq)

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigChan
		cancel()
	}()

	if err := svc.Run(ctx); err != nil {
		logger.Error("Service error", "error", err)
		os.Exit(1)
	}
}
