package clientgen

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vektah/gqlparser/v2/ast"
)

// renderGenerator renders the accumulated file of a codeGenerator to a string.
func renderGenerator(t *testing.T, gen *codeGenerator) string {
	t.Helper()

	buf, err := gen.WriteToBuffer()
	require.NoError(t, err)

	return buf.String()
}

func TestClientTemplate_SubscriptionSharedDecls(t *testing.T) {
	content, err := renderClientTemplate("testpkg", codegenHeader)
	require.NoError(t, err)

	out := string(content)
	assert.Contains(t, out, "package testpkg")
	assert.Contains(t, out, `ErrSubscriptionTransport = errors.New("subscription transport error")`)
	assert.Contains(t, out, "type subscriptionMessage struct {")
	assert.Contains(t, out, "data []byte")
}

func TestClientTemplate_OnErrorWrapsTransportError(t *testing.T) {
	content, err := renderClientTemplate("testpkg", codegenHeader)
	require.NoError(t, err)

	out := string(content)

	// the OnError callback must deliver the wrapped real error, never io.EOF
	onErrorStart := strings.Index(out, "OnError(func")
	require.GreaterOrEqual(t, onErrorStart, 0, "OnError callback not found in output")
	onErrorEnd := strings.Index(out[onErrorStart:], "return err")
	require.GreaterOrEqual(t, onErrorEnd, 0, "OnError callback end not found")
	onErrorBody := out[onErrorStart : onErrorStart+onErrorEnd]

	assert.Contains(t, onErrorBody, `fmt.Errorf("%w: %w", ErrSubscriptionTransport, err)`)
	assert.Contains(t, onErrorBody, "handler(nil, transportErr)")
	assert.NotContains(t, onErrorBody, "handler(nil, io.EOF)")

	// the OnSubscriptionComplete callback remains the only source of io.EOF
	completeStart := strings.Index(out, "OnSubscriptionComplete(func")
	require.GreaterOrEqual(t, completeStart, 0, "OnSubscriptionComplete callback not found in output")
	completeBody := out[completeStart:onErrorStart]
	assert.Contains(t, completeBody, "handler(nil, io.EOF)")
}

func TestGenerateSubscriptionMethod_ErrorRouting(t *testing.T) {
	gen := newCodeGenerator("testpkg")
	og := newOperationsGenerator(testSchema(), nil, gen, Options{})

	op := &ast.OperationDefinition{
		Name:      "getAllPosts",
		Operation: ast.Subscription,
	}
	og.generateSubscriptionMethod(op, "subscription getAllPosts { posts { id } }")

	out := renderGenerator(t, gen)

	// complete branch: io.EOF honored only when not wrapped in a transport error
	assert.Contains(t, out, "errors.Is(err, io.EOF) && !errors.Is(err, ErrSubscriptionTransport)")

	// error branch: real error sent over the channel, terminal, and handler returns nil
	assert.Contains(t, out, "messageChan <- subscriptionMessage{err: err}:")
	assert.Contains(t, out, "messageChan <- subscriptionMessage{data: messageData}:")
	assert.Contains(t, out, "make(chan subscriptionMessage)")

	// the handler must never return an error: a non-nil return from a handler invoked
	// inside the OnError callback would deadlock the library's run loop
	handlerStart := strings.Index(out, "handler := func")
	require.GreaterOrEqual(t, handlerStart, 0)
	handlerEnd := strings.Index(out[handlerStart:], "mu.Lock()\n\tid, err :=")
	require.GreaterOrEqual(t, handlerEnd, 0)
	handlerBody := out[handlerStart : handlerStart+handlerEnd]
	assert.NotContains(t, handlerBody, "return err")

	// subscriptionID is assigned under mu so the handler cannot race the Exec return
	assert.Contains(t, out, "subscriptionID = id")

	// iterator: terminal error handling with the real error; generic sentinel gone
	assert.Contains(t, out, "if msg.err != nil {")
	assert.NotContains(t, out, `"subscription error"`)
}
