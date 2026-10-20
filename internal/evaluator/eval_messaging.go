package evaluator

import (
	"context"
	"fmt"
	"geblang/internal/ast"
	"geblang/internal/native"
	"geblang/internal/runtime"
	"time"

	amqp091 "github.com/rabbitmq/amqp091-go"
	kafkago "github.com/segmentio/kafka-go"
)

func (e *Evaluator) amqpDial(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("%s expects amqp url", call.Callee.String())
	}
	url, ok := args[0].(runtime.String)
	if !ok {
		return nil, fmt.Errorf("%s url must be string", call.Callee.String())
	}
	conn, err := amqp091.Dial(url.Value)
	if err != nil {
		return nil, fmt.Errorf("amqp.dial: %w", err)
	}
	e.amqpMu.Lock()
	defer e.amqpMu.Unlock()
	e.nextAmqpConnID++
	id := e.nextAmqpConnID
	e.amqpConns[id] = conn
	return runtime.NewInt64(id), nil
}

func (e *Evaluator) amqpChannel(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("%s expects connection handle", call.Callee.String())
	}
	id, err := handleID(args[0], "amqp connection")
	if err != nil {
		return nil, err
	}
	e.amqpMu.Lock()
	conn, ok := e.amqpConns[id]
	e.amqpMu.Unlock()
	if !ok {
		return nil, fmt.Errorf("amqp.channel: unknown connection handle %d", id)
	}
	ch, err := conn.Channel()
	if err != nil {
		return nil, fmt.Errorf("amqp.channel: %w", err)
	}
	e.amqpMu.Lock()
	defer e.amqpMu.Unlock()
	e.nextAmqpChanID++
	chID := e.nextAmqpChanID
	e.amqpChans[chID] = ch
	return runtime.NewInt64(chID), nil
}

func (e *Evaluator) amqpDeclareQueue(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) < 2 || len(args) > 3 {
		return nil, fmt.Errorf("%s expects channel, name, and optional opts", call.Callee.String())
	}
	id, err := handleID(args[0], "amqp channel")
	if err != nil {
		return nil, err
	}
	name, ok := args[1].(runtime.String)
	if !ok {
		return nil, fmt.Errorf("amqp.declareQueue: name must be string")
	}
	durable, autoDelete, exclusive := true, false, false
	if len(args) == 3 {
		opts, ok := args[2].(runtime.Dict)
		if !ok {
			return nil, fmt.Errorf("amqp.declareQueue: opts must be dict")
		}
		durable = dictBoolDefault(opts, "durable", true)
		autoDelete = dictBoolDefault(opts, "autoDelete", false)
		exclusive = dictBoolDefault(opts, "exclusive", false)
	}
	e.amqpMu.Lock()
	ch, ok := e.amqpChans[id]
	e.amqpMu.Unlock()
	if !ok {
		return nil, fmt.Errorf("amqp.declareQueue: unknown channel handle %d", id)
	}
	q, err := ch.QueueDeclare(name.Value, durable, autoDelete, exclusive, false, nil)
	if err != nil {
		return nil, fmt.Errorf("amqp.declareQueue: %w", err)
	}
	entries := map[string]runtime.DictEntry{}
	nameKey := runtime.String{Value: "name"}
	entries["s"+"name"] = runtime.DictEntry{Key: nameKey, Value: runtime.String{Value: q.Name}}
	messagesKey := runtime.String{Value: "messages"}
	entries["s"+"messages"] = runtime.DictEntry{Key: messagesKey, Value: runtime.NewInt64(int64(q.Messages))}
	consumersKey := runtime.String{Value: "consumers"}
	entries["s"+"consumers"] = runtime.DictEntry{Key: consumersKey, Value: runtime.NewInt64(int64(q.Consumers))}
	return runtime.Dict{Entries: entries}, nil
}

func (e *Evaluator) amqpDeclareExchange(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) < 3 || len(args) > 4 {
		return nil, fmt.Errorf("%s expects channel, name, kind, and optional opts", call.Callee.String())
	}
	id, err := handleID(args[0], "amqp channel")
	if err != nil {
		return nil, err
	}
	name, ok := args[1].(runtime.String)
	if !ok {
		return nil, fmt.Errorf("amqp.declareExchange: name must be string")
	}
	kind, ok := args[2].(runtime.String)
	if !ok {
		return nil, fmt.Errorf("amqp.declareExchange: kind must be string (fanout/topic/direct/headers)")
	}
	durable, autoDelete := true, false
	if len(args) == 4 {
		opts, ok := args[3].(runtime.Dict)
		if !ok {
			return nil, fmt.Errorf("amqp.declareExchange: opts must be dict")
		}
		durable = dictBoolDefault(opts, "durable", true)
		autoDelete = dictBoolDefault(opts, "autoDelete", false)
	}
	e.amqpMu.Lock()
	ch, ok := e.amqpChans[id]
	e.amqpMu.Unlock()
	if !ok {
		return nil, fmt.Errorf("amqp.declareExchange: unknown channel handle %d", id)
	}
	if err := ch.ExchangeDeclare(name.Value, kind.Value, durable, autoDelete, false, false, nil); err != nil {
		return nil, fmt.Errorf("amqp.declareExchange: %w", err)
	}
	return runtime.Null{}, nil
}

func (e *Evaluator) amqpQueueBind(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 4 {
		return nil, fmt.Errorf("%s expects channel, queue, exchange, routingKey", call.Callee.String())
	}
	id, err := handleID(args[0], "amqp channel")
	if err != nil {
		return nil, err
	}
	queue, ok := args[1].(runtime.String)
	if !ok {
		return nil, fmt.Errorf("amqp.queueBind: queue must be string")
	}
	exchange, ok := args[2].(runtime.String)
	if !ok {
		return nil, fmt.Errorf("amqp.queueBind: exchange must be string")
	}
	routingKey, ok := args[3].(runtime.String)
	if !ok {
		return nil, fmt.Errorf("amqp.queueBind: routingKey must be string")
	}
	e.amqpMu.Lock()
	ch, ok := e.amqpChans[id]
	e.amqpMu.Unlock()
	if !ok {
		return nil, fmt.Errorf("amqp.queueBind: unknown channel handle %d", id)
	}
	if err := ch.QueueBind(queue.Value, routingKey.Value, exchange.Value, false, nil); err != nil {
		return nil, fmt.Errorf("amqp.queueBind: %w", err)
	}
	return runtime.Null{}, nil
}

func (e *Evaluator) amqpPublish(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) < 4 || len(args) > 5 {
		return nil, fmt.Errorf("%s expects channel, exchange, routingKey, body, and optional opts", call.Callee.String())
	}
	id, err := handleID(args[0], "amqp channel")
	if err != nil {
		return nil, err
	}
	exchange, ok := args[1].(runtime.String)
	if !ok {
		return nil, fmt.Errorf("amqp.publish: exchange must be string")
	}
	routingKey, ok := args[2].(runtime.String)
	if !ok {
		return nil, fmt.Errorf("amqp.publish: routingKey must be string")
	}
	var body []byte
	switch b := args[3].(type) {
	case runtime.String:
		body = []byte(b.Value)
	case runtime.Bytes:
		body = b.Value
	default:
		return nil, fmt.Errorf("amqp.publish: body must be string or bytes")
	}
	contentType := "application/octet-stream"
	persistent := true
	if len(args) == 5 {
		opts, ok := args[4].(runtime.Dict)
		if !ok {
			return nil, fmt.Errorf("amqp.publish: opts must be dict")
		}
		if v := dictStringDefault(opts, "contentType", ""); v != "" {
			contentType = v
		}
		persistent = dictBoolDefault(opts, "persistent", true)
	}
	e.amqpMu.Lock()
	ch, ok := e.amqpChans[id]
	e.amqpMu.Unlock()
	if !ok {
		return nil, fmt.Errorf("amqp.publish: unknown channel handle %d", id)
	}
	deliveryMode := uint8(amqp091.Persistent)
	if !persistent {
		deliveryMode = uint8(amqp091.Transient)
	}
	err = ch.PublishWithContext(context.Background(), exchange.Value, routingKey.Value, false, false, amqp091.Publishing{
		ContentType:  contentType,
		Body:         body,
		DeliveryMode: deliveryMode,
	})
	if err != nil {
		return nil, fmt.Errorf("amqp.publish: %w", err)
	}
	return runtime.Null{}, nil
}

func (e *Evaluator) amqpGet(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) < 2 || len(args) > 3 {
		return nil, fmt.Errorf("%s expects channel, queue, and optional autoAck", call.Callee.String())
	}
	id, err := handleID(args[0], "amqp channel")
	if err != nil {
		return nil, err
	}
	queue, ok := args[1].(runtime.String)
	if !ok {
		return nil, fmt.Errorf("amqp.get: queue must be string")
	}
	autoAck := false
	if len(args) == 3 {
		b, ok := args[2].(runtime.Bool)
		if !ok {
			return nil, fmt.Errorf("amqp.get: autoAck must be bool")
		}
		autoAck = b.Value
	}
	e.amqpMu.Lock()
	ch, ok := e.amqpChans[id]
	e.amqpMu.Unlock()
	if !ok {
		return nil, fmt.Errorf("amqp.get: unknown channel handle %d", id)
	}
	msg, ok, err := ch.Get(queue.Value, autoAck)
	if err != nil {
		return nil, fmt.Errorf("amqp.get: %w", err)
	}
	if !ok {
		return runtime.Null{}, nil
	}
	entries := map[string]runtime.DictEntry{}
	bodyKey := runtime.String{Value: "body"}
	entries["s"+"body"] = runtime.DictEntry{Key: bodyKey, Value: runtime.Bytes{Value: msg.Body}}
	tagKey := runtime.String{Value: "deliveryTag"}
	entries["s"+"deliveryTag"] = runtime.DictEntry{Key: tagKey, Value: runtime.NewInt64(int64(msg.DeliveryTag))}
	ctKey := runtime.String{Value: "contentType"}
	entries["s"+"contentType"] = runtime.DictEntry{Key: ctKey, Value: runtime.String{Value: msg.ContentType}}
	rkKey := runtime.String{Value: "routingKey"}
	entries["s"+"routingKey"] = runtime.DictEntry{Key: rkKey, Value: runtime.String{Value: msg.RoutingKey}}
	exchKey := runtime.String{Value: "exchange"}
	entries["s"+"exchange"] = runtime.DictEntry{Key: exchKey, Value: runtime.String{Value: msg.Exchange}}
	return runtime.Dict{Entries: entries}, nil
}

func (e *Evaluator) amqpAck(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("%s expects channel and deliveryTag", call.Callee.String())
	}
	id, err := handleID(args[0], "amqp channel")
	if err != nil {
		return nil, err
	}
	tag, ok := native.AsInt64(args[1])
	if !ok {
		return nil, fmt.Errorf("amqp.ack: deliveryTag must be int")
	}
	e.amqpMu.Lock()
	ch, ok := e.amqpChans[id]
	e.amqpMu.Unlock()
	if !ok {
		return nil, fmt.Errorf("amqp.ack: unknown channel handle %d", id)
	}
	if err := ch.Ack(uint64(tag), false); err != nil {
		return nil, fmt.Errorf("amqp.ack: %w", err)
	}
	return runtime.Null{}, nil
}

func (e *Evaluator) amqpClose(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("%s expects handle", call.Callee.String())
	}
	id, err := handleID(args[0], "amqp")
	if err != nil {
		return nil, err
	}
	e.amqpMu.Lock()
	defer e.amqpMu.Unlock()
	if ch, ok := e.amqpChans[id]; ok {
		delete(e.amqpChans, id)
		_ = ch.Close()
		return runtime.Null{}, nil
	}
	if conn, ok := e.amqpConns[id]; ok {
		delete(e.amqpConns, id)
		_ = conn.Close()
		return runtime.Null{}, nil
	}
	return runtime.Null{}, nil
}

func dictBoolDefault(d runtime.Dict, key string, def bool) bool {
	result := def
	d.ForEachEntry(func(_ string, entry runtime.DictEntry) bool {
		k, ok := entry.Key.(runtime.String)
		if !ok || k.Value != key {
			return true
		}
		if b, ok := entry.Value.(runtime.Bool); ok {
			result = b.Value
		}
		return false
	})
	return result
}

func dictStringDefault(d runtime.Dict, key, def string) string {
	result := def
	d.ForEachEntry(func(_ string, entry runtime.DictEntry) bool {
		k, ok := entry.Key.(runtime.String)
		if !ok || k.Value != key {
			return true
		}
		if s, ok := entry.Value.(runtime.String); ok {
			result = s.Value
		}
		return false
	})
	return result
}

type kafkaReaderHandle struct {
	reader  *kafkago.Reader
	pending kafkago.Message
	hasMsg  bool
}

func dictStringList(d runtime.Dict, key string) []string {
	var out []string
	d.ForEachEntry(func(_ string, entry runtime.DictEntry) bool {
		k, ok := entry.Key.(runtime.String)
		if !ok || k.Value != key {
			return true
		}
		list, ok := entry.Value.(*runtime.List)
		if !ok {
			out = nil
			return false
		}
		out = make([]string, 0, len(list.Elements))
		for _, el := range list.Elements {
			if s, ok := el.(runtime.String); ok {
				out = append(out, s.Value)
			}
		}
		return false
	})
	return out
}

func (e *Evaluator) kafkaWriter(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("%s expects an options dict", call.Callee.String())
	}
	opts, ok := args[0].(runtime.Dict)
	if !ok {
		return nil, fmt.Errorf("kafka.writer: opts must be dict")
	}
	brokers := dictStringList(opts, "brokers")
	if len(brokers) == 0 {
		return nil, fmt.Errorf("kafka.writer: opts.brokers must be a non-empty list<string>")
	}
	topic := dictStringDefault(opts, "topic", "")
	if topic == "" {
		return nil, fmt.Errorf("kafka.writer: opts.topic is required")
	}
	w := &kafkago.Writer{
		Addr:                   kafkago.TCP(brokers...),
		Topic:                  topic,
		Balancer:               &kafkago.Hash{},
		AllowAutoTopicCreation: dictBoolDefault(opts, "autoCreateTopic", false),
	}
	e.kafkaMu.Lock()
	defer e.kafkaMu.Unlock()
	e.nextKafkaWriterID++
	id := e.nextKafkaWriterID
	e.kafkaWriters[id] = w
	return runtime.NewInt64(id), nil
}

func (e *Evaluator) kafkaWrite(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) < 2 || len(args) > 3 {
		return nil, fmt.Errorf("%s expects writer handle, value, and optional key", call.Callee.String())
	}
	id, err := handleID(args[0], "kafka writer")
	if err != nil {
		return nil, err
	}
	var value []byte
	switch v := args[1].(type) {
	case runtime.String:
		value = []byte(v.Value)
	case runtime.Bytes:
		value = v.Value
	default:
		return nil, fmt.Errorf("kafka.write: value must be string or bytes")
	}
	var key []byte
	if len(args) == 3 {
		switch k := args[2].(type) {
		case runtime.String:
			key = []byte(k.Value)
		case runtime.Bytes:
			key = k.Value
		case runtime.Null:
		default:
			return nil, fmt.Errorf("kafka.write: key must be string, bytes, or null")
		}
	}
	e.kafkaMu.Lock()
	w, ok := e.kafkaWriters[id]
	e.kafkaMu.Unlock()
	if !ok {
		return nil, fmt.Errorf("kafka.write: unknown writer handle %d", id)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := w.WriteMessages(ctx, kafkago.Message{Key: key, Value: value}); err != nil {
		return nil, fmt.Errorf("kafka.write: %w", err)
	}
	return runtime.Null{}, nil
}

func (e *Evaluator) kafkaReader(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("%s expects an options dict", call.Callee.String())
	}
	opts, ok := args[0].(runtime.Dict)
	if !ok {
		return nil, fmt.Errorf("kafka.reader: opts must be dict")
	}
	brokers := dictStringList(opts, "brokers")
	if len(brokers) == 0 {
		return nil, fmt.Errorf("kafka.reader: opts.brokers must be a non-empty list<string>")
	}
	topic := dictStringDefault(opts, "topic", "")
	if topic == "" {
		return nil, fmt.Errorf("kafka.reader: opts.topic is required")
	}
	groupID := dictStringDefault(opts, "groupId", "")
	if groupID == "" {
		return nil, fmt.Errorf("kafka.reader: opts.groupId is required")
	}
	r := kafkago.NewReader(kafkago.ReaderConfig{
		Brokers: brokers,
		Topic:   topic,
		GroupID: groupID,
	})
	e.kafkaMu.Lock()
	defer e.kafkaMu.Unlock()
	e.nextKafkaReaderID++
	id := e.nextKafkaReaderID
	e.kafkaReaders[id] = &kafkaReaderHandle{reader: r}
	return runtime.NewInt64(id), nil
}

func (e *Evaluator) kafkaRead(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) < 1 || len(args) > 2 {
		return nil, fmt.Errorf("%s expects reader handle and optional timeoutMs", call.Callee.String())
	}
	id, err := handleID(args[0], "kafka reader")
	if err != nil {
		return nil, err
	}
	timeoutMs := int64(30000)
	if len(args) == 2 {
		n, ok := native.AsInt64(args[1])
		if !ok {
			return nil, fmt.Errorf("kafka.read: timeoutMs must be int")
		}
		timeoutMs = n
	}
	e.kafkaMu.Lock()
	handle, ok := e.kafkaReaders[id]
	e.kafkaMu.Unlock()
	if !ok {
		return nil, fmt.Errorf("kafka.read: unknown reader handle %d", id)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeoutMs)*time.Millisecond)
	defer cancel()
	msg, err := handle.reader.FetchMessage(ctx)
	if err != nil {
		if ctx.Err() != nil {
			return runtime.Null{}, nil
		}
		return nil, fmt.Errorf("kafka.read: %w", err)
	}
	handle.pending = msg
	handle.hasMsg = true
	entries := map[string]runtime.DictEntry{}
	put := func(key string, value runtime.Value) {
		k := runtime.String{Value: key}
		entries["s"+key] = runtime.DictEntry{Key: k, Value: value}
	}
	put("value", runtime.Bytes{Value: msg.Value})
	put("key", runtime.Bytes{Value: msg.Key})
	put("topic", runtime.String{Value: msg.Topic})
	put("partition", runtime.NewInt64(int64(msg.Partition)))
	put("offset", runtime.NewInt64(msg.Offset))
	return runtime.Dict{Entries: entries}, nil
}

func (e *Evaluator) kafkaCommit(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("%s expects reader handle", call.Callee.String())
	}
	id, err := handleID(args[0], "kafka reader")
	if err != nil {
		return nil, err
	}
	e.kafkaMu.Lock()
	handle, ok := e.kafkaReaders[id]
	e.kafkaMu.Unlock()
	if !ok {
		return nil, fmt.Errorf("kafka.commit: unknown reader handle %d", id)
	}
	if !handle.hasMsg {
		return runtime.Null{}, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := handle.reader.CommitMessages(ctx, handle.pending); err != nil {
		return nil, fmt.Errorf("kafka.commit: %w", err)
	}
	handle.hasMsg = false
	return runtime.Null{}, nil
}

func (e *Evaluator) kafkaClose(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("%s expects a writer or reader handle", call.Callee.String())
	}
	id, err := handleID(args[0], "kafka")
	if err != nil {
		return nil, err
	}
	e.kafkaMu.Lock()
	defer e.kafkaMu.Unlock()
	if w, ok := e.kafkaWriters[id]; ok {
		delete(e.kafkaWriters, id)
		_ = w.Close()
		return runtime.Null{}, nil
	}
	if r, ok := e.kafkaReaders[id]; ok {
		delete(e.kafkaReaders, id)
		_ = r.reader.Close()
		return runtime.Null{}, nil
	}
	return runtime.Null{}, nil
}
