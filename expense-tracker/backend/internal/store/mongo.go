package store

import (
	"context"
	"errors"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/anshulsao/demo-apps/expense-tracker/backend/internal/expense"
)

// Collection / document names in the shared Firestore (MongoDB-compatible) db.
const (
	expensesColl = "expenses"
	summaryColl  = "summary"
	summaryDocID = "agg" // the single running-aggregate document
)

// MongoStore is the shared, durable Store backed by Firestore's MongoDB-compatible
// API. All three services connect to the same database, so the web tier reads back
// exactly what ReceiptUploaded and SummaryConsumer wrote. On Cloud Run the driver
// authenticates as the runtime service account via OIDC (encoded in the URI) — no
// credentials in code.
type MongoStore struct {
	client *mongo.Client
	db     *mongo.Database
}

// expenseDoc is the BSON shape of an expense document.
type expenseDoc struct {
	ID           string    `bson:"_id"`
	Vendor       string    `bson:"vendor"`
	AmountCents  int64     `bson:"amount_cents"`
	Currency     string    `bson:"currency"`
	SpentOn      time.Time `bson:"spent_on"`
	SourceObject string    `bson:"source_object"`
	CreatedAt    time.Time `bson:"created_at"`
}

func toDoc(e expense.Expense) expenseDoc {
	return expenseDoc{
		ID: e.ID, Vendor: e.Vendor, AmountCents: e.AmountCents, Currency: e.Currency,
		SpentOn: e.SpentOn, SourceObject: e.SourceObject, CreatedAt: e.CreatedAt,
	}
}

func (d expenseDoc) toExpense() expense.Expense {
	return expense.Expense{
		ID: d.ID, Vendor: d.Vendor, AmountCents: d.AmountCents, Currency: d.Currency,
		SpentOn: d.SpentOn, SourceObject: d.SourceObject, CreatedAt: d.CreatedAt,
	}
}

// NewMongoStore connects to the database at uri and pings it to fail fast on
// misconfiguration.
func NewMongoStore(ctx context.Context, uri, dbName string) (*MongoStore, error) {
	if dbName == "" {
		return nil, errors.New("mongo db name is required")
	}
	cctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	client, err := mongo.Connect(cctx, options.Client().ApplyURI(uri))
	if err != nil {
		return nil, err
	}
	if err := client.Ping(cctx, nil); err != nil {
		return nil, err
	}
	return &MongoStore{client: client, db: client.Database(dbName)}, nil
}

// Save inserts one expense document.
func (m *MongoStore) Save(ctx context.Context, e expense.Expense) (expense.Expense, error) {
	if e.CreatedAt.IsZero() {
		e.CreatedAt = time.Now().UTC()
	}
	if _, err := m.db.Collection(expensesColl).InsertOne(ctx, toDoc(e)); err != nil {
		return expense.Expense{}, err
	}
	return e, nil
}

// List returns the most recent expenses, newest first.
func (m *MongoStore) List(ctx context.Context, limit int) ([]expense.Expense, error) {
	if limit <= 0 {
		limit = 100
	}
	opts := options.Find().SetSort(bson.D{{Key: "created_at", Value: -1}}).SetLimit(int64(limit))
	cur, err := m.db.Collection(expensesColl).Find(ctx, bson.D{}, opts)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)

	var docs []expenseDoc
	if err := cur.All(ctx, &docs); err != nil {
		return nil, err
	}
	out := make([]expense.Expense, 0, len(docs))
	for _, d := range docs {
		out = append(out, d.toExpense())
	}
	return out, nil
}

// GetSummary reads the running aggregate document; absent means zero.
func (m *MongoStore) GetSummary(ctx context.Context) (Summary, error) {
	var s Summary
	err := m.db.Collection(summaryColl).FindOne(ctx, bson.D{{Key: "_id", Value: summaryDocID}}).Decode(&s)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return Summary{}, nil
	}
	if err != nil {
		return Summary{}, err
	}
	return s, nil
}

// IncrementSummary atomically adds one expense of amountCents to the aggregate
// and returns the updated value, creating the document on first use.
func (m *MongoStore) IncrementSummary(ctx context.Context, amountCents int64) (Summary, error) {
	update := bson.D{{Key: "$inc", Value: bson.D{
		{Key: "count", Value: 1},
		{Key: "total_cents", Value: amountCents},
	}}}
	opts := options.FindOneAndUpdate().SetUpsert(true).SetReturnDocument(options.After)

	var s Summary
	err := m.db.Collection(summaryColl).
		FindOneAndUpdate(ctx, bson.D{{Key: "_id", Value: summaryDocID}}, update, opts).
		Decode(&s)
	if err != nil {
		return Summary{}, err
	}
	return s, nil
}

// Close disconnects the client.
func (m *MongoStore) Close(ctx context.Context) error { return m.client.Disconnect(ctx) }
