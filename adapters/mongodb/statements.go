package mongodb

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

// mongoStatementTimeLimit bounds the server time of one document-transaction
// call when its caller allows longer or sets no deadline. It equals MongoDB's
// default transactionLifetimeLimitSeconds, after which the server aborts the
// transaction anyway, so no successful statement is cut short; without it an
// abandoned query keeps running on the server after its transaction is gone.
const mongoStatementTimeLimit = 60 * time.Second

// mongoStatementContext gives ctx a deadline no later than the statement time
// limit. The driver sends the remaining time of a context deadline as
// maxTimeMS, so the server stops the statement when its caller gives up.
func mongoStatementContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, mongoStatementTimeLimit)
}

// mongoFindCommand is a find command's optional clauses.
type mongoFindCommand struct {
	sort       bson.D
	projection bson.D
	hint       string
	skip       int64
	limit      int64
}

// mongoFind runs find with the server time limit of ctx's deadline. The
// driver omits maxTimeMS from Collection.Find, whose cursor a caller could
// keep open past the deadline; document-transaction callers consume their
// cursors within the call, so the command interface carries the limit.
func mongoFind(ctx context.Context, collection *mongo.Collection, filter bson.D, clauses mongoFindCommand) (*mongo.Cursor, error) {
	command := bson.D{{Key: "find", Value: collection.Name()}, {Key: "filter", Value: filter}}
	if len(clauses.sort) != 0 {
		command = append(command, bson.E{Key: "sort", Value: clauses.sort})
	}
	if len(clauses.projection) != 0 {
		command = append(command, bson.E{Key: "projection", Value: clauses.projection})
	}
	if clauses.hint != "" {
		command = append(command, bson.E{Key: "hint", Value: clauses.hint})
	}
	if clauses.skip > 0 {
		command = append(command, bson.E{Key: "skip", Value: clauses.skip})
	}
	if clauses.limit > 0 {
		command = append(command, bson.E{Key: "limit", Value: clauses.limit})
	}
	return collection.Database().RunCommandCursor(ctx, command)
}

// mongoAggregate runs an aggregation with the server time limit of ctx's
// deadline; Collection.Aggregate omits it for the same reason as Find.
func mongoAggregate(ctx context.Context, collection *mongo.Collection, pipeline mongo.Pipeline) (*mongo.Cursor, error) {
	return collection.Database().RunCommandCursor(ctx, bson.D{
		{Key: "aggregate", Value: collection.Name()},
		{Key: "pipeline", Value: pipeline},
		{Key: "cursor", Value: bson.D{}},
	})
}
