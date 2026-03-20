package cmd

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/dennisschroeder/homematic2mqtt/internal/mqtt"
	"github.com/dennisschroeder/homematic2mqtt/internal/service"
	"github.com/dennisschroeder/homematic2mqtt/internal/source"
)

var (
	RootCmd = &cobra.Command{
		Use:   "homematic2mqtt",
		Short: "Homematic XML-RPC to MQTT bridge",
		Run:   runRoot,
	}

	rootArgs struct {
		logLevel     string
		dryRun       bool
		mqtt         mqtt.Config
		ccuAddress   string
		ccuPort      int
		callbackIP   string
		callbackPort int
	}
)

func init() {
	f := RootCmd.Flags()
	f.StringVar(&rootArgs.logLevel, "log-level", "info", "Log level")
	f.BoolVar(&rootArgs.dryRun, "dry-run", false, "Log events only, do not publish to MQTT")
	
	f.StringVar(&rootArgs.mqtt.Host, "mqtt-host", "localhost", "MQTT broker host")
	f.IntVar(&rootArgs.mqtt.Port, "mqtt-port", 1883, "MQTT broker port")
	f.StringVar(&rootArgs.mqtt.ClientID, "mqtt-client-id", "homematic-bridge", "MQTT Client ID")

	f.StringVar(&rootArgs.ccuAddress, "ccu-address", "192.168.178.29", "CCU2 IP Address")
	f.IntVar(&rootArgs.ccuPort, "ccu-port", 2001, "CCU2 XML-RPC Port (2001 for RF)")
	f.StringVar(&rootArgs.callbackIP, "callback-ip", "192.168.178.135", "Local IP for CCU callback")
	f.IntVar(&rootArgs.callbackPort, "callback-port", 8081, "Local port for CCU callback")
}

func runRoot(cmd *cobra.Command, args []string) {
	var level slog.Level
	if err := level.UnmarshalText([]byte(rootArgs.logLevel)); err != nil {
		level = slog.LevelInfo
	}
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: level}))

	var err error
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Source Client setup
	src, _ := source.NewClient(source.Config{
		CCUAddress:   rootArgs.ccuAddress,
		CCUPort:      rootArgs.ccuPort,
		CallbackIP:   rootArgs.callbackIP,
		CallbackPort: rootArgs.callbackPort,
	}, logger)

	// MQTT Client setup
	var mq *mqtt.Client
	if !rootArgs.dryRun {
		mq, err = mqtt.NewClient(rootArgs.mqtt, logger)
		if err != nil {
			logger.Error("Failed to initialize MQTT client", "error", err)
			os.Exit(1)
		}
		if err := mq.Connect(ctx); err != nil {
			logger.Error("MQTT connection failed", "error", err)
			os.Exit(1)
		}
	} else {
		logger.Info("[DRY-RUN] MQTT connection skipped")
	}

	// Service setup
	svc := service.New(service.Config{
		ShutterMapping: make(map[string]string),
		DryRun:         rootArgs.dryRun,
	}, logger, src, mq)

	// Fetch user-defined names from CCU2 ReGa layer
	if err := src.FetchDeviceNames(ctx); err != nil {
		logger.Warn("Failed to fetch device names from ReGa", "error", err)
	}

	if err := src.Connect(ctx); err != nil {
		logger.Error("Failed to connect to CCU", "error", err)
		os.Exit(1)
	}

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigChan
		src.Disconnect()
		cancel()
	}()

	if err := svc.Run(ctx); err != nil {
		logger.Error("Service error", "error", err)
		os.Exit(1)
	}
}
