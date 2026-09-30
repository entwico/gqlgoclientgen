package schema

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFetchSchema_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

		_, _ = w.Write([]byte(`{"data": {"__schema": {"types": [{"kind": "OBJECT", "name": "Query"}], "queryType": {"name": "Query"}}}}`))
	}))
	defer server.Close()

	schema, err := FetchSchema(context.Background(), server.URL, server.Client())
	require.NoError(t, err)
	require.Len(t, schema.Types, 1)
	assert.Equal(t, "Query", *schema.Types[0].Name)
}

func TestFetchSchema_Non200Status(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte("nope"))
	}))
	defer server.Close()

	_, err := FetchSchema(context.Background(), server.URL, server.Client())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "403")
}

func TestFetchSchema_GraphQLErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"errors": [{"message": "introspection is disabled"}, {"message": "second"}]}`))
	}))
	defer server.Close()

	_, err := FetchSchema(context.Background(), server.URL, server.Client())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "introspection is disabled")
	assert.Contains(t, err.Error(), "second")
}

func TestFetchSchema_MalformedJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`not json`))
	}))
	defer server.Close()

	_, err := FetchSchema(context.Background(), server.URL, server.Client())
	require.Error(t, err)
}

func TestFetchSchema_ContextCancelled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := FetchSchema(ctx, server.URL, server.Client())
	require.Error(t, err)
}

func TestFetchSchema_RetriesWithoutIsRepeatable(t *testing.T) {
	var queries []string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Query string `json:"query"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)

		queries = append(queries, body.Query)

		if strings.Contains(body.Query, "isRepeatable") {
			_, _ = w.Write([]byte(`{"errors": [{"message": "Cannot query field \"isRepeatable\" on type \"__Directive\"."}]}`))

			return
		}

		_, _ = w.Write([]byte(`{"data": {"__schema": {"types": [{"kind": "OBJECT", "name": "Query"}], "queryType": {"name": "Query"}}}}`))
	}))
	defer server.Close()

	schema, err := FetchSchema(context.Background(), server.URL, server.Client())
	require.NoError(t, err)
	require.Len(t, schema.Types, 1)

	// first attempt with isRepeatable, second without
	require.Len(t, queries, 2)
	assert.Contains(t, queries[0], "isRepeatable")
	assert.NotContains(t, queries[1], "isRepeatable")
}
