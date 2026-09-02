// Package events publishes expense.created events. It defines the Publisher
// seam, a Pub/Sub implementation, and an in-memory Fake for tests.
package events

import (
	"context"
	"encoding/json"
	"sync"

	"cloud.google.com/go/pubsub"

	"github.com/anshulsao/demo-apps/expense-tracker/backend/internal/expense"
)

// Publisher publishes an ExpenseCreated event to a message bus.
type Publisher interface {
	Publish(ctx context.Context, ev expense.ExpenseCreated) error
}

// Fake is an in-memory Publisher for tests and local runs.
type Fake struct {
	mu        sync.Mutex
	Published []expense.ExpenseCreated
	Err       error
}

// NewFake returns a ready-to-use in-memory publisher.
func NewFake() *Fake { return &Fake{} }

// Publish records the event in memory.
func (f *Fake) Publish(_ context.Context, ev expense.ExpenseCreated) error {
	if f.Err != nil {
		return f.Err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Published = append(f.Published, ev)
	return nil
}

// PubSubPublisher is the real Pub/Sub implementation.
type PubSubPublisher struct {
	topic *pubsub.Topic
}

// NewPubSubPublisher builds a publisher for the given project + topic.
func NewPubSubPublisher(ctx context.Context, projectID, topicID string) (*PubSubPublisher, error) {
	client, err := pubsub.NewClient(ctx, projectID)
	if err != nil {
		return nil, err
	}
	return &PubSubPublisher{topic: client.Topic(topicID)}, nil
}

// Publish marshals the event to JSON and publishes it, blocking on the result.
func (p *PubSubPublisher) Publish(ctx context.Context, ev expense.ExpenseCreated) error {
	data, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	res := p.topic.Publish(ctx, &pubsub.Message{Data: data})
	_, err = res.Get(ctx)
	return err
}
