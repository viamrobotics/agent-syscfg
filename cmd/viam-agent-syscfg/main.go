package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/edaniels/golog"
	"github.com/jessevdk/go-flags"

	syscfg "github.com/viamrobotics/agent-syscfg"
)

var (
	// only changed/set at startup, so no mutex.
	log = golog.NewDevelopmentLogger("agent-syscfg")
	activeBackgroundWorkers sync.WaitGroup
)

func main() {
	//nolint:lll
	var opts struct {
		Config             string `default:"/opt/viam/etc/agent-syscfg.json"       description:"Path to config file"                              long:"config"       short:"c"`
		Debug              bool   `description:"Enable debug logging"              long:"debug"                                                   short:"d"`
		Help               bool   `description:"Show this help message"            long:"help"                                                    short:"h"`
		Version            bool   `description:"Show version"                      long:"version"                                                 short:"v"`
	}

	parser := flags.NewParser(&opts, flags.IgnoreUnknown)
	parser.Usage = "runs as a background service and manages updates and the process lifecycle for viam-server."

	_, err := parser.Parse()
	if err != nil {
		log.Fatal(err)
	}

	if opts.Help {
		var b bytes.Buffer
		parser.WriteHelp(&b)
		//nolint:forbidigo
		fmt.Println(b.String())
		return
	}

	if opts.Version {
		//nolint:forbidigo
		fmt.Printf("Version: %s\nGit Revision: %s\n", syscfg.GetVersion(), syscfg.GetRevision())
		return
	}

	if opts.Debug {
		log = golog.NewDebugLogger("agent-syscfg")
	}

	ctx := setupExitSignalHandling()
	defer activeBackgroundWorkers.Wait()

	cfg, err := syscfg.LoadConfig(opts.Config)
	if err != nil {
		log.Warn(err)
	}

	log.Debugf("Config: %+v", cfg)

	// set journald max size limits
	syscfg.EnforceLogging(cfg.Logging, log)

	// exact text "startup complete" is important, the parent process will watch for this line to indicate startup is successful
	log.Info("agent-syscfg startup complete")

	// do nothing forever, just respond to health checks
	for {
		if !syscfg.HealthySleep(ctx, time.Minute) {
			break
		}
	}

	log.Info("agent-syscfg subsystem exiting")
}

func setupExitSignalHandling() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	sigChan := make(chan os.Signal, 16)

	healthcheckRequest := &atomic.Bool{}
	ctx = context.WithValue(ctx, syscfg.HCReqKey, healthcheckRequest)

	activeBackgroundWorkers.Add(1)
	go func() {
		defer activeBackgroundWorkers.Done()
		defer cancel()
		for {
			sig := <-sigChan
			switch sig {
			// things we exit for
			case os.Interrupt:
				fallthrough
			case syscall.SIGQUIT:
				fallthrough
			case syscall.SIGABRT:
				fallthrough
			case syscall.SIGTERM:
				log.Info("exit signal received")
				signal.Ignore(os.Interrupt, syscall.SIGTERM, syscall.SIGABRT) // keeping SIGQUIT for stack trace debugging
				return

			// this will eventually be handled elsewhere as a restart, not exit
			case syscall.SIGHUP:

			// ignore SIGURG entirely, it's used for real-time scheduling notifications
			case syscall.SIGURG:

			// used by parent viam-agent for healthchecks
			case syscall.SIGUSR1:
				healthcheckRequest.Store(true)

			// log everything else
			default:
				log.Debugw("received unknown signal", "signal", sig)
			}
		}
	}()

	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM, syscall.SIGABRT, syscall.SIGUSR1)
	return ctx
}
