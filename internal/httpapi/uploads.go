package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"

	operationengine "github.com/riducms/ridu/internal/operation"
	"github.com/riducms/ridu/protocol"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

// UploadInput is the transport's one document-and-file mutation contract.
type UploadInput struct {
	Filename         string                    `json:"filename"`
	Data             store.Values              `json:"data"`
	Image            *protocol.UploadImageEdit `json:"image"`
	Publish          bool                      `json:"publish"`
	Reader           io.Reader                 `json:"-"`
	Locale           LocaleOptions             `json:"-"`
	ExpectedRevision int                       `json:"-"`
}

func (api *API) saveUpload(writer http.ResponseWriter, request *http.Request, requestID string, collection schema.Collection, id string, identity *AuthIdentity) {
	method := http.MethodPatch
	if id == "" {
		method = http.MethodPost
	}
	if request.Method != method {
		api.methodNotAllowed(writer, requestID, method)
		return
	}
	if collection.Upload == nil || api.config.Upload == nil || id != "" && api.config.UpdateUpload == nil {
		api.writeError(writer, requestID, &operationengine.Error{Code: "upload_unavailable", Status: 503, Message: "upload storage is unavailable"})
		return
	}
	locale, err := decodeLocaleQuery(request.URL.Query())
	if err != nil {
		api.writeError(writer, requestID, err)
		return
	}
	var input UploadInput
	admissionHeld := false
	if strings.HasPrefix(request.Header.Get("Content-Type"), "multipart/form-data") {
		if api.config.AcquireUpload != nil {
			release, err := api.config.AcquireUpload(request.Context(), string(collection.Slug))
			if err != nil {
				api.writeError(writer, requestID, err)
				return
			}
			defer release()
			admissionHeld = true
		}
		request.Body = http.MaxBytesReader(writer, request.Body, collection.Upload.MaxFileSize+1<<20)
		err := request.ParseMultipartForm(maxMultipartMemory)
		if request.MultipartForm != nil {
			defer request.MultipartForm.RemoveAll()
		}
		if err != nil {
			var maximum *http.MaxBytesError
			if errors.As(err, &maximum) {
				api.writeError(writer, requestID, &operationengine.Error{Code: "body_too_large", Status: 413, Message: fmt.Sprintf("multipart upload exceeds %d bytes", maximum.Limit)})
			} else {
				api.writeError(writer, requestID, &operationengine.Error{Code: "bad_request", Status: 400, Message: "invalid multipart upload", Cause: err})
			}
			return
		}
		file, header, err := request.FormFile("file")
		if err != nil {
			api.writeError(writer, requestID, &operationengine.Error{Code: "validation", Status: 422, Message: "multipart field \"file\" is required", Cause: err})
			return
		}
		defer file.Close()
		input.Reader, input.Filename = file, header.Filename
		if data := request.FormValue("data"); data != "" {
			if err := decodeDynamicValues([]byte(data), &input.Data); err != nil {
				api.writeError(writer, requestID, &operationengine.Error{Code: "bad_request", Status: 400, Message: "multipart field \"data\" must be a JSON object", Cause: err})
				return
			}
		}
		if image := request.FormValue("image"); image != "" {
			if err := json.Unmarshal([]byte(image), &input.Image); err != nil {
				api.writeError(writer, requestID, &operationengine.Error{Code: "bad_request", Status: 400, Message: "invalid image edit", Cause: err})
				return
			}
		}
		input.Publish = request.FormValue("publish") == "true"
	} else if id != "" {
		if err := api.decodeJSON(writer, request, &input); err != nil {
			api.writeError(writer, requestID, err)
			return
		}
	} else {
		api.writeError(writer, requestID, &operationengine.Error{Code: "validation", Status: 422, Message: "a file is required"})
		return
	}
	input.Locale, input.ExpectedRevision = locale.public(), revisionHeader(request)
	var document store.Document
	status := http.StatusOK
	if id == "" {
		document, err = api.config.Upload(request.Context(), string(collection.Slug), input, identity, admissionHeld)
		status = http.StatusCreated
	} else {
		document, err = api.config.UpdateUpload(request.Context(), string(collection.Slug), id, input, identity, admissionHeld)
	}
	if err != nil {
		api.writeError(writer, requestID, err)
		return
	}
	writeJSON(writer, status, protocol.DocumentEnvelope[json.RawMessage]{Doc: documentJSON(document)})
	api.audit(request, requestID, identityActor(identity), "upload", string(collection.Slug), document.ID)
}

func (api *API) createRemoteUpload(writer http.ResponseWriter, request *http.Request, requestID string, collection schema.Collection, identity *AuthIdentity) {
	if collection.Upload == nil || api.config.RemoteUpload == nil {
		api.writeError(writer, requestID, &operationengine.Error{Code: "not_found", Status: 404, Message: "remote upload was not found"})
		return
	}
	if collection.Auth != nil {
		api.authCollectionCreateError(writer, requestID, collection)
		return
	}
	var input struct {
		URL string `json:"url"`
		UploadInput
	}
	if err := api.decodeJSON(writer, request, &input); err != nil {
		api.writeError(writer, requestID, err)
		return
	}
	locale, err := decodeLocaleQuery(request.URL.Query())
	if err != nil {
		api.writeError(writer, requestID, err)
		return
	}
	input.Locale = locale.public()
	document, err := api.config.RemoteUpload(request.Context(), string(collection.Slug), input.URL, input.UploadInput, identity)
	if err != nil {
		api.writeError(writer, requestID, err)
		return
	}
	writeJSON(writer, http.StatusCreated, protocol.DocumentEnvelope[json.RawMessage]{Doc: documentJSON(document)})
	api.audit(request, requestID, identityActor(identity), "remote-upload", string(collection.Slug), document.ID)
}

func (api *API) uploadSource(writer http.ResponseWriter, request *http.Request, requestID string, collection schema.Collection, id string, identity *AuthIdentity) {
	if request.Method != http.MethodGet && request.Method != http.MethodHead {
		api.methodNotAllowed(writer, requestID, http.MethodGet, http.MethodHead)
		return
	}
	if collection.Upload == nil || api.config.OpenUploadSource == nil {
		api.writeError(writer, requestID, &operationengine.Error{Code: "not_found", Status: 404, Message: "upload source was not found"})
		return
	}
	reader, object, err := api.config.OpenUploadSource(request.Context(), string(collection.Slug), id, identity)
	if err != nil {
		api.writeError(writer, requestID, err)
		return
	}
	serveUpload(writer, request, reader, object)
}

func (api *API) previewUpload(writer http.ResponseWriter, request *http.Request, requestID string, collection schema.Collection, identity *AuthIdentity) {
	if collection.Upload == nil || api.config.PreviewUpload == nil {
		api.writeError(writer, requestID, &operationengine.Error{Code: "not_found", Status: 404, Message: "upload preview was not found"})
		return
	}
	var input struct {
		URL string `json:"url"`
		ID  string `json:"id"`
	}
	if err := api.decodeJSON(writer, request, &input); err != nil {
		api.writeError(writer, requestID, err)
		return
	}
	reader, object, filename, err := api.config.PreviewUpload(request.Context(), string(collection.Slug), input.ID, input.URL, identity)
	if err != nil {
		api.writeError(writer, requestID, err)
		return
	}
	writer.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": filename}))
	serveUpload(writer, request, reader, object)
}
