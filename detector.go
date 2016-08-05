package nozzle

import (
	"context"
	"fmt"
	"log"

	"github.com/cloudfoundry/sonde-go/events"
	"github.com/gorilla/websocket"
)

// SlowDetectCh is channel used to send `slowConsumerAlert` event.
type slowDetectCh chan error

type noaaEventsCh <-chan *events.Envelope

// SlowDetector defines the interface for detecting `slowConsumerAlert`
// event. By default, defaultSlowDetetor is used. It implements same detection
// logic as https://github.com/cloudfoundry-incubator/datadog-firehose-nozzle.
type slowDetector interface {
	// Detect detects `slowConsumerAlert`. It works as pipe.
	// It receives events from upstream (RawConsumer) and inspects that events
	// and pass it to to downstream without modification.
	//
	// It returns SlowDetectCh and notify `slowConsumerAlert` there.
	Detect(<-chan *events.Envelope, <-chan error) (<-chan *events.Envelope, <-chan error, slowDetectCh)

	// DetectContext detects `slowConsumerAlert` using the given context
	DetectContext(context.Context, noaaEventsCh, <-chan error) (noaaEventsCh, <-chan error, slowDetectCh)

	// Stop stops slow consumer detection. If any returns error.
	Stop() error
}

// defaultSlowDetector implements SlowDetector interface
type defaultSlowDetector struct {
	cancelFunc context.CancelFunc
	logger     *log.Logger
}

func (sd *defaultSlowDetector) DetectContext(ctx context.Context, eventCh noaaEventsCh, errCh <-chan error) (noaaEventsCh, <-chan error, slowDetectCh) {

	if ctx == nil {
		panic("nil context")
	}

	sd.logger.Println("[INFO] Start detecting slowConsumerAlert event")

	// Create new channel to pass producer
	eventCh_ := make(chan *events.Envelope)
	errCh_ := make(chan error)

	// deteCh is used to send `slowConsumerAlert` event
	detectCh := make(slowDetectCh)

	// Detect from from trafficcontroller event messages
	go func() {
		defer close(eventCh_)
		for event := range eventCh {
			// Check nozzle can catch up firehose outputs speed.
			if isTruncated(event) {
				detectCh <- fmt.Errorf(
					"doppler dropped messages from its queue because nozzle is slow")
			}

			select {
			case eventCh_ <- event:
			case <-ctx.Done():
				// Send errCh_ that context is closed
				sd.logger.Println("[INFO] Canceled parent context: closing event channel")
				errCh_ <- ctx.Err()

				// close downstream eventCh
				return
			}

		}
	}()

	// Detect from websocket errors
	go func() {
		defer close(errCh_)
		for err := range errCh {
			switch t := err.(type) {
			case *websocket.CloseError:
				if t.Code == websocket.ClosePolicyViolation {
					// ClosePolicyViolation (1008)
					// indicates that an endpoint is terminating the connection
					// because it has received a message that violates its policy.
					//
					// This is a generic status code that can be returned when there is no
					// other more suitable status code (e.g., 1003 or 1009) or if there
					// is a need to hide specific details about the policy.
					//
					// http://tools.ietf.org/html/rfc6455#section-11.7
					detectCh <- fmt.Errorf(
						"websocket terminates the connection because connection is too slow (ClosePolicyViolation)")
				}
			}
			select {
			case errCh_ <- err:
			case <-ctx.Done():
				// Send errCh_ that context is closed
				sd.logger.Println("[INFO] Canceled parent context: closing error channel")
				errCh_ <- ctx.Err()

				// close downstream errCh and eventCh
				return
			}

		}
	}()

	return eventCh_, errCh_, detectCh
}

// Detect start to detect `slowConsumerAlert` event.
func (sd *defaultSlowDetector) Detect(eventCh <-chan *events.Envelope, errCh <-chan error) (<-chan *events.Envelope, <-chan error, slowDetectCh) {
	ctx, cancel := context.WithCancel(context.Background())
	sd.cancelFunc = cancel
	return sd.DetectContext(ctx, eventCh, errCh)
}

func (sd *defaultSlowDetector) Stop() error {
	sd.logger.Println("[INFO] Stop detecting slowConsumerAlert event")
	if sd.cancelFunc == nil {
		return fmt.Errorf("cancel function is not given")
	}

	sd.cancelFunc()

	return nil
}

// isTruncated detects message from the Doppler that the nozzle
// could not consume messages as quickly as the firehose was sending them.
func isTruncated(envelope *events.Envelope) bool {
	if envelope.GetEventType() == events.Envelope_CounterEvent &&
		envelope.CounterEvent.GetName() == "TruncatingBuffer.DroppedMessages" &&
		envelope.GetOrigin() == "doppler" {
		return true
	}

	return false
}
