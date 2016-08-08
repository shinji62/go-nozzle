// This directory contains a example usage of go-nozzle.
// See details on README.md
package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"time"

	"github.com/cloudfoundry/sonde-go/events"
	"github.com/rakutentech/go-nozzle"
)

const (
	EnvDopplerAddr = "DOPPLER_ADDR"
	EnvToken       = "CF_ACCESS_TOKEN"
	EnvUaaAddr     = "UAA_ADDR"
	EnvUsername    = "CF_USERNAME"
	EnvPassword    = "CF_PASSWORD"
)

const (
	// SubscriptionID is
	SubscriptionID = "go-nozzle-example-A"

	// UAATimeout is timeout duration while waiting getting
	// access token from UAA
	UAATimeout = 60 * time.Second
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {

	var insecure bool
	flags := flag.NewFlagSet("example", flag.ContinueOnError)
	flags.BoolVar(&insecure, "insecure", false, "Enable insecure ssl skip verify")
	if err := flags.Parse(args); err != nil {
		return 1
	}

	// Construct Nozzle opt
	config := &nozzle.Config{
		DopplerAddr:    os.Getenv(EnvDopplerAddr),
		UaaAddr:        os.Getenv(EnvUaaAddr),
		Username:       os.Getenv(EnvUsername),
		Password:       os.Getenv(EnvPassword),
		SubscriptionID: SubscriptionID,
		Insecure:       insecure,
		Logger:         log.New(os.Stdout, "", log.LstdFlags),
	}

	ctxUaa, cancelUaa := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelUaa()

	consumer, err := nozzle.NewConsumerContext(ctxUaa, config)
	if err != nil {
		log.Printf("[ERROR] Failed to construct nozzle consumer: %s", err)
		return 1
	}

	// Start consumer
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle signaling
	signalCh := make(chan os.Signal)
	signal.Notify(signalCh, os.Interrupt, os.Kill)
	go func() {
		<-signalCh
		log.Printf("[INFO] Interrupt Received")
		cancel()
	}()

	log.Printf("[INFO] Start example producer")
	consumer.StartWithContext(ctx)
	doneCh := make(chan struct{}, 1)
	go func() {
		defer close(doneCh)
		for {
			select {
			case event := <-consumer.Events():
				if event.GetEventType() != events.Envelope_ValueMetric {
					continue
				}
				log.Printf("[INFO] ValueMetric: %v", event.GetValueMetric())
			case <-consumer.Detects():
				log.Printf("[WARN] Detected SlowConsumerAlert")
			case err := <-consumer.Errors():
				log.Printf("[ERROR] Failed to consume nozzle events", err)
				return
			case <-ctx.Done():
				log.Printf("[ERROR] context is cancelled: %s", ctx.Err())
				return
			}
		}
	}()

	<-doneCh
	return 0
}
