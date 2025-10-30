package dlog

import (
	"context"
	"fmt"
	"os"

	"github.com/nats-io/nats.go"
)

const (
	unishardStreamName  string = "UNISHARD"
	unishardSubjectName string = "unishard.>"
)

type natsWriter struct {
	js        nats.JetStreamContext
	subject   string
	ctx       context.Context
	ctxCancel context.CancelFunc
	// futures   chan nats.PubAckFuture
	publishErrors chan error
}

func NewNATSWriter() *natsWriter {
	nw := &natsWriter{
		// futures: make(chan nats.PubAckFuture, 1000),
		publishErrors: make(chan error, 1000),
	}

	nw.ctx, nw.ctxCancel = context.WithCancel(context.Background())
	return nw
}

// Connect to NATS endpoint & Try to add jetstream
func (nw *natsWriter) Connect(address string) error {
	nc, err := nats.Connect(address)
	if err != nil {
		return fmt.Errorf("failed to connect to NATS: %w", err)
	}

	js, err := nc.JetStream(
		nats.PublishAsyncErrHandler(nw.publishErrorHandler),
		nats.PublishAsyncMaxPending(10000),
		nats.Context(nw.ctx),
	)
	if err != nil {
		return fmt.Errorf("failed to create JetStream context: %w", err)
	}

	_, err = js.StreamInfo(unishardStreamName)
	if err != nil {
		if err == nats.ErrStreamNotFound {
			_, err = js.AddStream(&nats.StreamConfig{
				Name:     unishardStreamName,
				Subjects: []string{unishardSubjectName},
			})
			if err != nil {
				return fmt.Errorf("failed to create stream: %w", err)
			}
		} else {
			return fmt.Errorf("failed to get stream info: %w", err)
		}
	}

	nw.js = js
	nw.subject = "unishard.log"
	return nil
}

// zerolog is expected to call this Write function per log
func (nw *natsWriter) Write(p []byte) (int, error) {
	_, err := nw.js.PublishAsync(nw.subject, p)
	if err != nil {
		fmt.Fprintf(os.Stderr, "NATS publish error: %v\n", err)
		return 0, err
	}

	return len(p), nil
}

func (nw *natsWriter) publishErrorHandler(js nats.JetStream, msg *nats.Msg, err error) {
	nw.publishErrors <- err
}

func (nw *natsWriter) Close() error {
	nw.ctxCancel()
	return nil
}
