package main

import (
	"sync"
	"time"
)

// MessageType indicates what type of Body is encoded by Message
type MessageType uint8

const (
	Text MessageType = iota
	Audio
	Image
	Attachment
)

// Message holds information
type Message struct {
	sync.Mutex

	// Type is the type of Message
	Type MessageType

	// Conversation tag
	Conversation uint64

	// ID of the Message
	ID uint64

	// sender Contact.ID for this client
	Sender uint64 `cbor:"-"`

	// Sent is the sender timestamp
	Sent time.Time

	// Received is the reciver timestamp
	Received time.Time `cbor:"-"`

	// Acked is when the receiver acknowledged this message
	Acked time.Time

	// Body is the message body
	Body []byte
}
