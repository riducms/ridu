package content

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/operation"
)

func notifyPosts(webhookURL string) ridu.Hook {
	// Ridu waits for this hook by default, so limit the delivery delay.
	client := &http.Client{Timeout: 5 * time.Second}
	return func(ctx ridu.HookContext) error {
		// AfterCommit runs on reads too; notify only on saves.
		switch ctx.Operation {
		case operation.Create, operation.Duplicate, operation.Update,
			operation.Publish, operation.Unpublish:
		default:
			return nil
		}
		if ctx.Document == nil {
			return nil
		}

		body, err := json.Marshal(struct {
			ID        string         `json:"id"`
			Operation operation.Kind `json:"operation"`
		}{ID: ctx.Document.ID, Operation: ctx.Operation})
		if err != nil {
			return err
		}
		request, err := http.NewRequestWithContext(
			ctx.Context, http.MethodPost, webhookURL, bytes.NewReader(body),
		)
		if err != nil {
			return err
		}
		request.Header.Set("Content-Type", "application/json")
		// The post stays saved even if delivery fails from this point.
		response, err := client.Do(request)
		if err != nil {
			return err
		}
		defer response.Body.Close()
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			return fmt.Errorf("post webhook returned %s", response.Status)
		}
		return nil
	}
}
