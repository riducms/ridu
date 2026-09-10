package operation

import (
	"context"

	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/query"
)

// ReadUploadOwner proves object membership through ordinary collection read
// access. Object keys and configured size keys are framework-owned lookup
// constraints, even when their metadata fields are hidden from document reads.
func (engine *Engine) ReadUploadOwner(ctx context.Context, key string, request Request) (Result, error) {
	collection, exists := engine.collections[request.Collection]
	if !exists || collection.Schema.Upload == nil {
		return Result{}, &Error{Code: "not_found", Status: 404, Message: "upload collection was not found"}
	}
	objectKey, _ := query.NewPath("objectKey")
	predicates := []query.Expression{query.Equal(objectKey, query.String(key))}
	for _, size := range collection.Schema.Upload.ImageSizes {
		sizeKey, err := query.NewPath("sizes", size.Name, "objectKey")
		if err != nil {
			return Result{}, err
		}
		predicates = append(predicates, query.Equal(sizeKey, query.String(key)))
	}
	where := predicates[0]
	if len(predicates) > 1 {
		where, _ = query.Or(predicates...)
	}
	filter := where.Node()
	request.Operation, request.ID, request.Limit = operation.Read, "", 1
	request.internalFilter = &filter
	return engine.Execute(ctx, request)
}
