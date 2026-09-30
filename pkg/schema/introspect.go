package schema

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// maxResponseSize bounds the introspection response body so a misbehaving
// server cannot exhaust memory.
const maxResponseSize = 50 << 20

type graphQLResponse struct {
	Data   json.RawMessage `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors,omitempty"`
}

// FetchSchema fetches the GraphQL schema from the specified endpoint via
// introspection. The context controls cancellation and timeout.
func FetchSchema(ctx context.Context, schemaURL string, client *http.Client) (*Schema, error) {
	schema, err := fetchSchemaWithQuery(ctx, schemaURL, client, introspectionQuery)

	// servers predating the October 2021 spec reject isRepeatable on
	// __Directive; retry without it rather than failing the introspection
	if err != nil && strings.Contains(err.Error(), "isRepeatable") {
		return fetchSchemaWithQuery(ctx, schemaURL, client, strings.Replace(introspectionQuery, "isRepeatable\n", "", 1))
	}

	return schema, err
}

func fetchSchemaWithQuery(ctx context.Context, schemaURL string, client *http.Client, query string) (*Schema, error) {
	jsonBody, err := json.Marshal(map[string]any{"query": query})
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, schemaURL, bytes.NewBuffer(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute request: %w", err)
	}

	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseSize))
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d, response: %s", resp.StatusCode, string(body))
	}

	var gqlResp graphQLResponse
	if err := json.Unmarshal(body, &gqlResp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if len(gqlResp.Errors) > 0 {
		messages := make([]string, 0, len(gqlResp.Errors))
		for _, gqlErr := range gqlResp.Errors {
			messages = append(messages, gqlErr.Message)
		}

		return nil, fmt.Errorf("graphql errors: %s", strings.Join(messages, "; "))
	}

	var data struct {
		Schema Schema `json:"__schema"`
	}

	if err := json.Unmarshal(gqlResp.Data, &data); err != nil {
		return nil, fmt.Errorf("failed to unmarshal schema: %w", err)
	}

	return &data.Schema, nil
}
