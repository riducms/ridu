package operation

import (
	"fmt"

	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

func (engine *Engine) prepareCreateID(request *Request) error {
	if request.Operation != operation.Create {
		return nil
	}
	request.Data = store.CloneValues(request.Data)

	if request.ImportID != "" {
		if request.ID != "" {
			return callerIDError("id", "a migration import cannot also supply an ordinary create ID", nil)
		}
		if _, exists := request.Data["id"]; exists {
			return callerIDError("id", "a migration import cannot also contain an ordinary create ID", nil)
		}
		if err := store.ValidateDocumentID(request.ImportID); err != nil {
			return callerIDError("id", "migration document ID is invalid", err)
		}
		request.ID = request.ImportID
		return nil
	}

	requestedID := request.ID
	idProvided := requestedID != ""
	if value, exists := request.Data["id"]; exists {
		idProvided = true
		delete(request.Data, "id")
		fromData, valid := value.StringValue()
		if !valid {
			return callerIDError("id", "caller-supplied document ID must be a string", nil)
		}
		if requestedID != "" && requestedID != fromData {
			return callerIDError("id", "create options and document data contain different IDs", nil)
		}
		requestedID = fromData
	}
	if !idProvided {
		return nil
	}
	if !engine.allowIDOnCreate {
		return callerIDError("id", "caller-supplied document IDs are not enabled", nil)
	}
	if err := store.ValidateDocumentID(requestedID); err != nil {
		return callerIDError("id", "caller-supplied document ID is invalid", err)
	}
	request.ID = requestedID
	return nil
}

func callerIDError(path, message string, cause error) error {
	if cause != nil {
		message = fmt.Sprintf("%s: %v", message, cause)
	}
	return &Error{
		Code: "validation", Status: 422, Message: "document validation failed",
		Issues: []schema.Issue{{Code: "invalid_document_id", Path: path, Message: message}},
		Cause:  cause,
	}
}
