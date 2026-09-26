package generate

func uploadImageSchema() map[string]any {
	properties := make(map[string]any, 6)
	for _, name := range []string{"focalX", "focalY", "cropX", "cropY", "cropWidth", "cropHeight"} {
		properties[name] = map[string]any{"type": "number", "minimum": 0, "maximum": 100}
	}
	return map[string]any{
		"type": "object", "properties": properties,
		"required":    []string{"focalX", "focalY", "cropX", "cropY", "cropWidth", "cropHeight"},
		"description": "Coordinates are percentages of the oriented private source. Zero crop width and height reset to the full source; focal coordinates are constrained to the crop.",
	}
}

func uploadRequestBody(replacement bool) map[string]any {
	multipart := map[string]any{
		"type": "object", "required": []string{"file"},
		"properties": map[string]any{
			"file":    map[string]any{"type": "string", "format": "binary"},
			"data":    map[string]any{"type": "string", "description": "JSON-encoded document fields."},
			"image":   map[string]any{"type": "string", "description": "JSON-encoded image edit, using percentages of the oriented source."},
			"publish": map[string]any{"type": "boolean"},
		},
	}
	content := map[string]any{"multipart/form-data": map[string]any{"schema": multipart}}
	if replacement {
		content["application/json"] = map[string]any{"schema": map[string]any{
			"type": "object", "properties": map[string]any{
				"data": map[string]any{"type": "object"}, "image": uploadImageSchema(),
				"filename": map[string]any{"type": "string"}, "publish": map[string]any{"type": "boolean"},
			},
		}}
	}
	return map[string]any{"required": true, "content": content}
}

func uploadDocumentParameters(locales []map[string]any, revision bool) []map[string]any {
	parameters := append([]map[string]any{{
		"name": "id", "in": "path", "required": true, "schema": map[string]any{"type": "string"},
	}}, locales...)
	if revision {
		parameters = append(parameters, map[string]any{
			"name": "If-Match", "in": "header", "schema": map[string]any{"type": "string"},
			"description": "Expected document _revision. Stale revisions return 409 without changing fields or file references.",
		})
	}
	return parameters
}
