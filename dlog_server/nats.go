package main

import (
	"fmt"
	"log"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
)

type NatsContext struct {
	server  *server.Server
	conn    *nats.Conn
	jstream nats.JetStreamContext
	sub     *nats.Subscription
}

const (
	unishardStreamName  string = "UNISHARD"     // Must match the client
	unishardSubjectName string = "unishard.>"   // Must match the client's stream definition
	subscribeSubject    string = "unishard.log" // Specific subject to listen to
	serverAddr          string = "127.0.0.1"
	serverPort          int    = 4222
)

func (nctx *NatsContext) Shutdown() error {
	// TODO: 에러처리
	nctx.sub.Unsubscribe()
	nctx.conn.Drain()
	nctx.conn.Close()
	nctx.server.Shutdown()
	return nil
}

// runEmbeddedServer starts and returns an embedded NATS server instance.
func runEmbeddedServer() (*server.Server, error) {
	opts := &server.Options{
		Host:      serverAddr,
		Port:      serverPort,
		JetStream: true, // Enable JetStream
		// StoreDir: "./nats-data", // Optional: specify storage directory for JetStream persistence
		// LogFile: "./nats-server.log", // Optional: Log server output to file
		// Trace: true, // Optional: Enable verbose tracing
	}

	s, err := server.NewServer(opts)
	if err != nil {
		return nil, fmt.Errorf("creating server failed: %w", err)
	}

	// Start server in a goroutine
	go s.Start()

	// Wait for server to be ready
	if !s.ReadyForConnections(10 * time.Second) {
		return nil, fmt.Errorf("server did not become ready in time")
	}

	return s, nil
}

// connectAndSetupJetStream connects a NATS client to the given address and gets JetStream context.
func connectAndSetupJetStream(serverURL string) (*nats.Conn, nats.JetStreamContext, error) {
	nc, err := nats.Connect(serverURL, nats.Timeout(10*time.Second)) // Add timeout
	if err != nil {
		return nil, nil, fmt.Errorf("failed to connect to NATS at %s: %w", serverURL, err)
	}

	js, err := nc.JetStream()
	if err != nil {
		nc.Close()
		return nil, nil, fmt.Errorf("failed to create JetStream context: %w", err)
	}

	return nc, js, nil
}

// ensureStreamExists checks if the target stream exists and optionally creates it.
// In this setup, the client is expected to create it, so this is mostly a check.
func ensureStreamExists(js nats.JetStreamContext) error {
	streamInfo, err := js.StreamInfo(unishardStreamName)
	if err != nil {
		if err == nats.ErrStreamNotFound {
			_, err = js.AddStream(&nats.StreamConfig{
				Name:     unishardStreamName,
				Subjects: []string{unishardSubjectName},
			})
			if err != nil {
				return fmt.Errorf("failed to create stream '%s': %w", unishardStreamName, err)
			}
			log.Printf("Stream '%s' created by server.", unishardStreamName)
			return nil // Return nil assuming client will create it
		}
		// Other error getting stream info
		return fmt.Errorf("failed to get stream info for '%s': %w", unishardStreamName, err)
	}

	// Stream exists, log info (optional)
	log.Printf("Found existing stream '%s' with %d messages.", streamInfo.Config.Name, streamInfo.State.Msgs)
	return nil
}

// receiveCallback method should do only user's logic, message ACK is sent by internal setupNATS method.
func setupNATS(receiveCallback func(msg *nats.Msg, ackError error)) (*NatsContext, error) {
	// 1. run server
	s, err := runEmbeddedServer()
	if err != nil {
		return nil, fmt.Errorf("starting embedded NATS server failed: %w", err)
	}

	// 2. setup client in server side
	nc, js, err := connectAndSetupJetStream(s.ClientURL())
	if err != nil {
		s.Shutdown()
		return nil, fmt.Errorf("connecting client or setting up JetStream failed: %w", err)
	}

	// 3. setup jetstream
	err = ensureStreamExists(js)
	if err != nil {
		nc.Close()
		s.Shutdown()
		return nil, fmt.Errorf("ensuring stream '%s' exists failed: %w", unishardStreamName, err)
	}

	// 4. subscribe at client in server side
	sub, err := js.Subscribe(subscribeSubject, func(msg *nats.Msg) {
		receiveCallback(msg, msg.Ack())
	}, nats.DeliverNew(), nats.ManualAck()) // Durable consumer named "LOG_PROCESSOR"
	if err != nil {
		return nil, fmt.Errorf("subscribing to subject '%s' failed: %v", subscribeSubject, err)
	}

	log.Printf("Subscribed to subject [%s]. Waiting for logs...", subscribeSubject)
	return &NatsContext{
		server:  s,
		conn:    nc,
		jstream: js,
		sub:     sub,
	}, nil
}
