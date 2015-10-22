package nozzle

import (
	"crypto/tls"
	"fmt"
	"log"

	"github.com/cloudfoundry/noaa"
	"github.com/cloudfoundry/sonde-go/events"
)

// Consumer defines the interface of consumer it receives
// upstream firehose events and slowConsumerAlerts events and errors.
type Consumer interface {
	// Events returns the read channel for the events that consumed by
	// rawConsumer(by default Noaa).
	Events() <-chan *events.Envelope

	// Detects returns the read channel that is notified slowConsumerAlerts
	// handled by SlowDetector.
	Detects() <-chan struct{}

	// Error returns the read channel of erros that occured during consuming.
	Errors() <-chan error

	// Close stop consuming upstream events by RawConsumer and stop SlowDetector.
	Close() error
}

type consumer struct {
	rawConsumer  RawConsumer
	slowDetector SlowDetector

	eventCh  chan *events.Envelope
	errCh    chan error
	detectCh chan struct{}
}

// Events returns the read channel for the events that consumed by rawConsumer
func (c *consumer) Events() <-chan *events.Envelope {
	return c.eventCh
}

// Detects returns the read channel that is notified slowConsumerAlerts
func (c *consumer) Detects() <-chan struct{} {
	return c.detectCh
}

// Error returns the read channel of erros that occured during consuming.
func (c *consumer) Errors() <-chan error {
	return c.errCh
}

// Close closes connection with firehose and stop slowDetector.
func (c *consumer) Close() error {
	if err := c.rawConsumer.Close(); err != nil {
		return err
	}

	return c.slowDetector.Stop()
}

// RawConsumer defines the interface for consuming events from doppler firehose.
// The events pulled by RawConsumer pass to slowDetector and check slowDetector.
//
// By default, rawConsumer (which is implemented by https://github.com/cloudfoundry/noaa)
// is used.
type RawConsumer interface {
	// Consume starts cosuming firehose events. It must return 2 channel. The one
	// is for sending the events from firehose and the other is for error occured while
	// consuming. These channels are used donwstream process (SlowConsumer).
	Consume() (chan *events.Envelope, chan error)

	// Close closes connection with firehose. If any, returns error.
	Close() error
}

type rawConsumer struct {
	connection *noaa.Consumer

	dopplerAddr    string
	token          string
	subscriptionID string
	insecure       bool
	debugPrinter   noaa.DebugPrinter

	logger *log.Logger
}

// Consume consumes firehose events from doppler.
// Retry function is handled in noaa library (It will retry 5 times).
func (c *rawConsumer) Consume() (chan *events.Envelope, chan error) {
	c.logger.Printf(
		"[INFO] Start consuming firehose events from Doppler (%s) with subscription ID %q",
		c.dopplerAddr, c.subscriptionID)

	// Setup Noaa consumer
	tlsConfig := &tls.Config{
		InsecureSkipVerify: c.insecure,
	}
	connection := noaa.NewConsumer(c.dopplerAddr, tlsConfig, nil)
	if c.debugPrinter != nil {
		connection.SetDebugPrinter(c.debugPrinter)
	}

	// Start connection
	eventChan, errChan := make(chan *events.Envelope), make(chan error)
	go connection.Firehose(c.subscriptionID, c.token, eventChan, errChan)

	// Store conenction in rawConsumer struct
	// to close it from other function
	c.connection = connection

	return eventChan, errChan
}

func (c *rawConsumer) Close() error {
	c.logger.Printf("[INFO] Stop consuming firehose events")
	if c.connection == nil {
		return fmt.Errorf("no connection with firehose")
	}

	return c.connection.Close()
}

// validate validates struct has requirement fields or not
func (c *rawConsumer) validate() error {
	if c.dopplerAddr == "" {
		return fmt.Errorf("DopplerAddr must not be empty")
	}

	if c.token == "" {
		return fmt.Errorf("Token must not be empty")
	}

	if c.subscriptionID == "" {
		return fmt.Errorf("SubscriptionID must not be empty")
	}

	return nil
}

// newRawConsumer constructs new rawConsumer.
func newRawConsumer(config *Config) (*rawConsumer, error) {
	c := &rawConsumer{
		dopplerAddr:    config.DopplerAddr,
		token:          config.Token,
		subscriptionID: config.SubscriptionID,
		insecure:       config.Insecure,
		debugPrinter:   config.DebugPrinter,
		logger:         config.Logger,
	}

	if err := c.validate(); err != nil {
		return nil, err
	}

	return c, nil

}