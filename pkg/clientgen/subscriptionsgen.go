package clientgen

import (
	"fmt"

	"github.com/dave/jennifer/jen"
	"github.com/vektah/gqlparser/v2/ast"
)

// generateSubscriptionMethod generates a method on Client for executing a subscription.
//
//nolint:maintidx // long but linear code-generation function
func (og *operationsGenerator) generateSubscriptionMethod(op *ast.OperationDefinition, queryStr string) {
	methodName := og.formatMethodName(op.Name)
	responseName := og.operationResponseName(op.Name)

	// build method signature with individual parameters
	methodParams := make([]jen.Code, 0, len(op.VariableDefinitions)+1)
	methodParams = append(methodParams, jen.Id("ctx").Qual("context", "Context"))

	// add individual parameters for each variable
	for _, varDef := range op.VariableDefinitions {
		paramName := og.formatParameterName(varDef.Variable)
		paramType := og.goTypeFromGraphQLType(varDef.Type)
		methodParams = append(methodParams, jen.Id(paramName).Add(paramType))
	}

	// build method body
	var methodBody []jen.Code

	// check if subscription client is configured
	methodBody = append(methodBody,
		jen.If(jen.Id("s").Dot("client").Dot("subscriptionClient").Op("==").Nil()).Block(
			jen.Return(jen.Nil(), jen.Qual("fmt", "Errorf").Call(jen.Lit("graphql subscription client is not configured"))),
		),
		jen.Line(),
	)

	// check if subscription client is running
	methodBody = append(methodBody,
		jen.If(jen.Op("!").Id("s").Dot("client").Dot("IsSubscriptionClientRunning").Call()).Block(
			jen.Return(jen.Nil(), jen.Qual("fmt", "Errorf").Call(jen.Lit("graphql subscription client is not running. Call StartSubscriptionClient() before using subscriptions"))),
		),
		jen.Line(),
	)

	// define the subscription string
	methodBody = append(methodBody,
		jen.Id("subscription").Op(":=").Op("`"+queryStr+"`"),
		jen.Line(),
	)

	// prepare variables map from individual parameters if needed
	var varsParam jen.Code

	if len(op.VariableDefinitions) > 0 {
		methodBody = append(methodBody, og.buildVariablesMapFromParams(op))
		varsParam = jen.Id("vars")
	} else {
		varsParam = jen.Nil()
	}

	// create message channel plus a done channel the iterator closes on exit
	methodBody = append(methodBody,
		jen.Id("messageChan").Op(":=").Make(jen.Chan().Id("subscriptionMessage")),
		jen.Id("done").Op(":=").Make(jen.Chan().Struct()),
		jen.Line(),
	)

	// snapshot the client callbacks under the read lock so the handler never
	// races with StartSubscriptionClient writing them
	methodBody = append(methodBody,
		jen.Id("s").Dot("client").Dot("subscriptionMutex").Dot("RLock").Call(),
		jen.Id("onStarted").Op(":=").Id("s").Dot("client").Dot("onSubscriptionStarted"),
		jen.Id("onData").Op(":=").Id("s").Dot("client").Dot("onSubscriptionData"),
		jen.Id("onError").Op(":=").Id("s").Dot("client").Dot("onSubscriptionError"),
		jen.Id("onComplete").Op(":=").Id("s").Dot("client").Dot("onSubscriptionComplete"),
		jen.Id("s").Dot("client").Dot("subscriptionMutex").Dot("RUnlock").Call(),
		jen.Line(),
	)

	// declare subscriptionID variable and channel closed tracking
	methodBody = append(methodBody,
		jen.Var().Id("subscriptionID").String(),
		jen.Var().Id("channelClosed").Bool(),
		jen.Var().Id("mu").Qual("sync", "Mutex"),
		jen.Line(),
	)

	// create handler function for subscription messages (as closure to access subscriptionID)
	methodBody = append(methodBody,
		jen.Id("handler").Op(":=").Id("func").Params(
			jen.Id("messageData").Index().Byte(),
			jen.Id("err").Error(),
		).Error().Block(
			jen.Comment("protect channel operations with mutex"),
			jen.Id("mu").Dot("Lock").Call(),
			jen.Defer().Id("mu").Dot("Unlock").Call(),
			jen.Line(),
			jen.Comment("check if channel is already closed"),
			jen.If(jen.Id("channelClosed")).Block(
				jen.Return(jen.Nil()),
			),
			jen.Line(),
			jen.If(jen.Id("err").Op("!=").Nil()).Block(
				jen.Comment("io.EOF means exactly one thing: the server sent GraphQL \"complete\"."),
				jen.Comment("transport failures arrive wrapped in ErrSubscriptionTransport and are"),
				jen.Comment("checked first, because a transport error may itself wrap io.EOF."),
				jen.If(jen.Qual("errors", "Is").Call(jen.Id("err"), jen.Qual("io", "EOF")).Op("&&").Op("!").Qual("errors", "Is").Call(jen.Id("err"), jen.Id("ErrSubscriptionTransport"))).Block(
					jen.Comment("call subscription complete callback if set"),
					jen.If(jen.Id("onComplete").Op("!=").Nil()).Block(
						jen.Id("onComplete").Call(
							jen.Id("subscriptionID"),
							jen.Lit(op.Name),
						),
					),
					jen.Comment("prevent re-registration on reconnect; reconnection is the caller's responsibility."),
					jen.Id("s").Dot("client").Dot("subscriptionClient").Dot("Unsubscribe").Call(jen.Id("subscriptionID")),
					jen.Id("channelClosed").Op("=").True(),
					jen.Close(jen.Id("messageChan")),
					jen.Return(jen.Nil()),
				),
				jen.Line(),
				jen.Comment("transport failure or GraphQL error: terminate the stream with the real"),
				jen.Comment("error so the consumer can distinguish a partial stream from a complete one."),
				jen.If(jen.Id("onError").Op("!=").Nil()).Block(
					jen.Id("onError").Call(
						jen.Id("subscriptionID"),
						jen.Lit(op.Name),
						jen.Id("err"),
					),
				),
				jen.Select().Block(
					jen.Case(jen.Id("messageChan").Op("<-").Id("subscriptionMessage").Values(jen.Id("err").Op(":").Id("err"))),
					jen.Case(jen.Op("<-").Id("ctx").Dot("Done").Call()),
					jen.Case(jen.Op("<-").Id("done")),
				),
				jen.Comment("prevent re-registration on reconnect; reconnection is the caller's responsibility."),
				jen.Id("s").Dot("client").Dot("subscriptionClient").Dot("Unsubscribe").Call(jen.Id("subscriptionID")),
				jen.Id("channelClosed").Op("=").True(),
				jen.Close(jen.Id("messageChan")),
				jen.Comment("MUST return nil: this handler may be invoked synchronously from the"),
				jen.Comment("client's OnError callback inside the library's Run loop. A non-nil return"),
				jen.Comment("is sent to the client's unbuffered errorChan, whose only reader is that"),
				jen.Comment("same Run loop - returning an error here would deadlock the client."),
				jen.Return(jen.Nil()),
			),
			jen.Comment("call subscription data callback if set"),
			jen.If(jen.Id("onData").Op("!=").Nil()).Block(
				jen.Id("onData").Call(
					jen.Id("subscriptionID"),
					jen.Lit(op.Name),
				),
			),
			// send data through channel; the done case releases the handler when
			// the consumer stops iterating without cancelling ctx
			jen.Select().Block(
				jen.Case(jen.Id("messageChan").Op("<-").Id("subscriptionMessage").Values(jen.Id("data").Op(":").Id("messageData"))),
				jen.Case(jen.Op("<-").Id("ctx").Dot("Done").Call()),
				jen.Case(jen.Op("<-").Id("done")),
			),
			jen.Return(jen.Nil()),
		),
		jen.Line(),
	)

	// start the subscription
	methodBody = append(methodBody,
		jen.Comment("register the subscription while holding mu: the handler reads subscriptionID"),
		jen.Comment("under the same mutex and may fire before Exec returns"),
		jen.Id("mu").Dot("Lock").Call(),
		jen.List(jen.Id("id"), jen.Id("err")).Op(":=").Id("s").Dot("client").Dot("subscriptionClient").Dot("Exec").Call(
			jen.Id("subscription"),
			varsParam,
			jen.Id("handler"),
		),
		jen.Id("subscriptionID").Op("=").Id("id"),
		jen.Id("mu").Dot("Unlock").Call(),
		jen.If(jen.Id("err").Op("!=").Nil()).Block(
			jen.Return(jen.Nil(), jen.Qual("fmt", "Errorf").Call(
				jen.Lit("failed to start subscription (is the subscription client running?): %w"),
				jen.Id("err"),
			)),
		),
		jen.Line(),
		jen.Comment("call subscription started callback if set"),
		jen.If(jen.Id("onStarted").Op("!=").Nil()).Block(
			jen.Id("onStarted").Call(
				jen.Id("subscriptionID"),
				jen.Lit(op.Name),
			),
		),
		jen.Line(),
	)

	// create and return the iterator function
	methodBody = append(methodBody,
		jen.Id("iterator").Op(":=").Id("func").Params(
			jen.Id("yield").Id("func").Params(
				jen.Op("*").Id(responseName),
				jen.Error(),
			).Bool(),
		).Block(
			jen.Comment("when the consumer stops iterating early, unsubscribe and release the"),
			jen.Comment("handler, which may be blocked sending on messageChan"),
			jen.Defer().Id("func").Params().Block(
				jen.Close(jen.Id("done")),
				jen.Id("mu").Dot("Lock").Call(),
				jen.If(jen.Op("!").Id("channelClosed")).Block(
					jen.Id("channelClosed").Op("=").True(),
					jen.Id("s").Dot("client").Dot("subscriptionClient").Dot("Unsubscribe").Call(jen.Id("subscriptionID")),
				),
				jen.Id("mu").Dot("Unlock").Call(),
			).Call(),
			jen.For().Block(
				jen.Select().Block(
					jen.Case(jen.List(jen.Id("msg"), jen.Id("ok")).Op(":=").Op("<-").Id("messageChan")).Block(
						jen.If(jen.Op("!").Id("ok")).Block(
							// channel closed without error: server sent "complete"
							jen.Return(),
						),
						jen.If(jen.Id("msg").Dot("err").Op("!=").Nil()).Block(
							// terminal error: the stream is aborted and may be partial
							jen.Id("yield").Call(jen.Nil(), jen.Id("msg").Dot("err")),
							jen.Return(),
						),
						// unmarshal the response
						jen.Var().Id("resp").Id(responseName),
						jen.If(jen.Id("err").Op(":=").Qual("encoding/json", "Unmarshal").Call(
							jen.Id("msg").Dot("data"),
							jen.Op("&").Id("resp"),
						), jen.Id("err").Op("!=").Nil()).Block(
							jen.If(jen.Op("!").Id("yield").Call(jen.Nil(), jen.Id("err"))).Block(
								jen.Return(),
							),
							jen.Continue(),
						),
						// yield the response
						jen.If(jen.Op("!").Id("yield").Call(jen.Op("&").Id("resp"), jen.Nil())).Block(
							jen.Return(),
						),
					),
					jen.Case(jen.Op("<-").Id("ctx").Dot("Done").Call()).Block(
						// context cancelled
						jen.Return(),
					),
				),
			),
		),
		jen.Line(),
		jen.Return(jen.Id("iterator"), jen.Nil()),
	)

	og.gen.file.Comment(fmt.Sprintf("%s executes the %s subscription and returns an iterator.", methodName, op.Name))
	og.gen.file.Comment("Note: The subscription client must be running. Call StartSubscriptionClient() before using this method.")
	og.gen.file.Func().Params(
		jen.Id("s").Op("*").Id("Subscriptions"),
	).Id(methodName).Params(methodParams...).Params(
		jen.Qual("iter", "Seq2").Types(jen.Op("*").Id(responseName), jen.Error()),
		jen.Error(),
	).Block(methodBody...)
	og.gen.file.Line()
}
