package evaluator

import (
	"bytes"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"net/smtp"
	"net/textproto"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"geblang/internal/ast"
	"geblang/internal/native"
	"geblang/internal/runtime"
)

type smtpMessage struct {
	from        string
	to          []string
	cc          []string
	bcc         []string
	replyTo     string
	subject     string
	text        string
	html        string
	headers     map[string][]string
	attachments []smtpAttachment
}

type smtpAttachment struct {
	filename    string
	contentType string
	content     []byte
	inline      bool
	contentID   string
}

type smtpConfig struct {
	host               string
	port               int
	username           string
	password           string
	from               string
	hello              string
	implicitTLS        bool
	startTLS           bool
	insecureSkipVerify bool
}

func smtpMessageBuiltin(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("%s expects a message dict", call.Callee.String())
	}
	message, err := smtpMessageFromValue(args[0])
	if err != nil {
		return nil, fmt.Errorf("%s %v", call.Callee.String(), err)
	}
	data, err := buildSMTPMessage(message)
	if err != nil {
		return nil, fmt.Errorf("%s %v", call.Callee.String(), err)
	}
	return runtime.String{Value: string(data)}, nil
}

func smtpSendBuiltin(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("%s expects config and message dicts", call.Callee.String())
	}
	config, err := smtpConfigFromValue(args[0])
	if err != nil {
		return nil, fmt.Errorf("%s %v", call.Callee.String(), err)
	}
	message, err := smtpMessageFromValue(args[1])
	if err != nil {
		return nil, fmt.Errorf("%s %v", call.Callee.String(), err)
	}
	if message.from == "" {
		message.from = config.from
	}
	if message.from == "" {
		return nil, fmt.Errorf("%s message.from or config.from is required", call.Callee.String())
	}
	data, err := buildSMTPMessage(message)
	if err != nil {
		return nil, fmt.Errorf("%s %v", call.Callee.String(), err)
	}
	recipients := append(append([]string{}, message.to...), message.cc...)
	recipients = append(recipients, message.bcc...)
	if len(recipients) == 0 {
		return nil, fmt.Errorf("%s message must include to, cc, or bcc recipients", call.Callee.String())
	}
	envelopeFrom, err := envelopeAddress(message.from)
	if err != nil {
		return nil, fmt.Errorf("%s from: %v", call.Callee.String(), err)
	}
	envelopeTo := make([]string, len(recipients))
	for i, recipient := range recipients {
		if envelopeTo[i], err = envelopeAddress(recipient); err != nil {
			return nil, fmt.Errorf("%s recipient: %v", call.Callee.String(), err)
		}
	}
	if err := sendSMTP(config, envelopeFrom, envelopeTo, data); err != nil {
		return nil, fmt.Errorf("%s %v", call.Callee.String(), err)
	}
	entries := map[string]runtime.DictEntry{}
	putDict(entries, "ok", runtime.Bool{Value: true})
	putDict(entries, "recipients", runtime.SmallInt{Value: int64(len(recipients))})
	return runtime.Dict{Entries: entries}, nil
}

// envelopeAddress reduces "Name <addr>" to the bare address SMTP commands require.
func envelopeAddress(address string) (string, error) {
	parsed, err := mail.ParseAddress(address)
	if err != nil {
		return "", err
	}
	return parsed.Address, nil
}

func smtpConfigFromValue(value runtime.Value) (smtpConfig, error) {
	dict, ok := value.(runtime.Dict)
	if !ok {
		return smtpConfig{}, fmt.Errorf("config must be dict")
	}
	config := smtpConfig{port: 587, startTLS: true}
	var found bool
	if config.host, found = dictStringField(dict, "host"); !found || config.host == "" {
		return smtpConfig{}, fmt.Errorf("config.host is required")
	}
	if portValue, ok := dictField(dict, "port"); ok {
		port, err := intField(portValue, "config.port")
		if err != nil {
			return smtpConfig{}, err
		}
		config.port = port
	}
	config.username, _ = dictStringField(dict, "username")
	config.password, _ = dictStringField(dict, "password")
	config.from, _ = dictStringField(dict, "from")
	config.hello, _ = dictStringField(dict, "hello")
	config.implicitTLS = dictBoolFieldDefault(dict, "tls", false)
	_, hasStartTLS := dictField(dict, "startTLS")
	config.startTLS = dictBoolFieldDefault(dict, "startTLS", true)
	if config.implicitTLS && !hasStartTLS {
		config.startTLS = false
	}
	config.insecureSkipVerify = dictBoolFieldDefault(dict, "insecureSkipVerify", false)
	return config, nil
}

func smtpMessageFromValue(value runtime.Value) (smtpMessage, error) {
	dict, ok := value.(runtime.Dict)
	if !ok {
		return smtpMessage{}, fmt.Errorf("message must be dict")
	}
	message := smtpMessage{headers: map[string][]string{}}
	message.from, _ = dictStringField(dict, "from")
	message.replyTo, _ = dictStringField(dict, "replyTo")
	message.subject, _ = dictStringField(dict, "subject")
	message.text, _ = dictStringField(dict, "text")
	message.html, _ = dictStringField(dict, "html")
	var err error
	message.to, err = addressListField(dict, "to")
	if err != nil {
		return smtpMessage{}, err
	}
	message.cc, err = addressListField(dict, "cc")
	if err != nil {
		return smtpMessage{}, err
	}
	message.bcc, err = addressListField(dict, "bcc")
	if err != nil {
		return smtpMessage{}, err
	}
	if headersValue, ok := dictField(dict, "headers"); ok {
		message.headers, err = smtpHeaderMap(headersValue)
		if err != nil {
			return smtpMessage{}, err
		}
	}
	if attachmentsValue, ok := dictField(dict, "attachments"); ok {
		attachments, err := smtpAttachmentsFromValue(attachmentsValue)
		if err != nil {
			return smtpMessage{}, err
		}
		message.attachments = attachments
	}
	if message.text == "" && message.html == "" && len(message.attachments) == 0 {
		return smtpMessage{}, fmt.Errorf("message must include text, html, or attachments")
	}
	return message, nil
}

func buildSMTPMessage(message smtpMessage) ([]byte, error) {
	if message.from != "" {
		if _, err := mail.ParseAddress(message.from); err != nil {
			return nil, fmt.Errorf("invalid from address: %w", err)
		}
	}
	if err := validateAddressList("to", message.to); err != nil {
		return nil, err
	}
	if err := validateAddressList("cc", message.cc); err != nil {
		return nil, err
	}
	if err := validateAddressList("bcc", message.bcc); err != nil {
		return nil, err
	}
	var body bytes.Buffer
	headers := textproto.MIMEHeader{}
	if message.from != "" {
		headers.Set("From", message.from)
	}
	if len(message.to) > 0 {
		headers.Set("To", strings.Join(message.to, ", "))
	}
	if len(message.cc) > 0 {
		headers.Set("Cc", strings.Join(message.cc, ", "))
	}
	if message.replyTo != "" {
		headers.Set("Reply-To", message.replyTo)
	}
	headers.Set("Subject", mime.QEncoding.Encode("utf-8", sanitizeHeaderValue(message.subject)))
	headers.Set("Date", time.Now().Format(time.RFC1123Z))
	headers.Set("MIME-Version", "1.0")
	for key, values := range message.headers {
		canonical := textproto.CanonicalMIMEHeaderKey(key)
		if reservedSMTPHeader(canonical) {
			continue
		}
		for _, value := range values {
			headers.Add(canonical, sanitizeHeaderValue(value))
		}
	}
	if len(message.attachments) > 0 {
		writer := multipart.NewWriter(&body)
		headers.Set("Content-Type", "multipart/mixed; boundary="+writer.Boundary())
		writeHeaders(&body, headers)
		if err := writeBodyPart(writer, message.text, message.html); err != nil {
			return nil, err
		}
		for _, attachment := range message.attachments {
			if err := writeAttachmentPart(writer, attachment); err != nil {
				return nil, err
			}
		}
		if err := writer.Close(); err != nil {
			return nil, err
		}
		return body.Bytes(), nil
	}
	if message.text != "" && message.html != "" {
		writer := multipart.NewWriter(&body)
		headers.Set("Content-Type", "multipart/alternative; boundary="+writer.Boundary())
		writeHeaders(&body, headers)
		if err := writeTextPart(writer, "text/plain; charset=utf-8", message.text); err != nil {
			return nil, err
		}
		if err := writeTextPart(writer, "text/html; charset=utf-8", message.html); err != nil {
			return nil, err
		}
		if err := writer.Close(); err != nil {
			return nil, err
		}
		return body.Bytes(), nil
	}
	if message.html != "" {
		headers.Set("Content-Type", "text/html; charset=utf-8")
	} else {
		headers.Set("Content-Type", "text/plain; charset=utf-8")
	}
	headers.Set("Content-Transfer-Encoding", "quoted-printable")
	writeHeaders(&body, headers)
	qp := quotedPrintableWriter{Writer: quotedprintable.NewWriter(&body)}
	if message.html != "" {
		_, _ = qp.Write([]byte(message.html))
	} else {
		_, _ = qp.Write([]byte(message.text))
	}
	_ = qp.Close()
	return body.Bytes(), nil
}

func writeBodyPart(writer *multipart.Writer, text, html string) error {
	if text != "" && html != "" {
		var nested bytes.Buffer
		alt := multipart.NewWriter(&nested)
		if err := writeTextPart(alt, "text/plain; charset=utf-8", text); err != nil {
			return err
		}
		if err := writeTextPart(alt, "text/html; charset=utf-8", html); err != nil {
			return err
		}
		if err := alt.Close(); err != nil {
			return err
		}
		header := textproto.MIMEHeader{}
		header.Set("Content-Type", "multipart/alternative; boundary="+alt.Boundary())
		part, err := writer.CreatePart(header)
		if err != nil {
			return err
		}
		_, err = part.Write(nested.Bytes())
		return err
	}
	if html != "" {
		return writeTextPart(writer, "text/html; charset=utf-8", html)
	}
	return writeTextPart(writer, "text/plain; charset=utf-8", text)
}

func writeTextPart(writer *multipart.Writer, contentType, text string) error {
	header := textproto.MIMEHeader{}
	header.Set("Content-Type", contentType)
	header.Set("Content-Transfer-Encoding", "quoted-printable")
	part, err := writer.CreatePart(header)
	if err != nil {
		return err
	}
	qp := quotedPrintableWriter{Writer: quotedprintable.NewWriter(part)}
	if _, err := qp.Write([]byte(text)); err != nil {
		return err
	}
	return qp.Close()
}

func writeAttachmentPart(writer *multipart.Writer, attachment smtpAttachment) error {
	header := textproto.MIMEHeader{}
	contentType := attachment.contentType
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	header.Set("Content-Type", contentType+"; name="+quoteMIMEParam(attachment.filename))
	header.Set("Content-Transfer-Encoding", "base64")
	disposition := "attachment"
	if attachment.inline {
		disposition = "inline"
	}
	header.Set("Content-Disposition", disposition+"; filename="+quoteMIMEParam(attachment.filename))
	if attachment.contentID != "" {
		header.Set("Content-ID", "<"+sanitizeHeaderValue(strings.Trim(attachment.contentID, "<>"))+">")
	}
	part, err := writer.CreatePart(header)
	if err != nil {
		return err
	}
	encoder := base64.NewEncoder(base64.StdEncoding, newBase64LineWriter(part))
	if _, err := encoder.Write(attachment.content); err != nil {
		return err
	}
	return encoder.Close()
}

func sendSMTP(config smtpConfig, from string, recipients []string, data []byte) error {
	addr := net.JoinHostPort(config.host, strconv.Itoa(config.port))
	if config.implicitTLS {
		conn, err := tls.Dial("tcp", addr, smtpTLSConfig(config))
		if err != nil {
			return err
		}
		client, err := smtp.NewClient(conn, config.host)
		if err != nil {
			_ = conn.Close()
			return err
		}
		return sendSMTPWithClient(client, config, from, recipients, data)
	}
	client, err := smtp.Dial(addr)
	if err != nil {
		return err
	}
	return sendSMTPWithClient(client, config, from, recipients, data)
}

func sendSMTPWithClient(client *smtp.Client, config smtpConfig, from string, recipients []string, data []byte) error {
	defer client.Close()
	if config.hello != "" {
		if err := client.Hello(config.hello); err != nil {
			return err
		}
	}
	if config.startTLS {
		if ok, _ := client.Extension("STARTTLS"); ok {
			if err := client.StartTLS(smtpTLSConfig(config)); err != nil {
				return err
			}
		}
	}
	if config.username != "" || config.password != "" {
		auth := smtp.PlainAuth("", config.username, config.password, config.host)
		if err := client.Auth(auth); err != nil {
			return err
		}
	}
	if err := client.Mail(from); err != nil {
		return err
	}
	for _, recipient := range recipients {
		if err := client.Rcpt(recipient); err != nil {
			return err
		}
	}
	writer, err := client.Data()
	if err != nil {
		return err
	}
	if _, err := writer.Write(data); err != nil {
		_ = writer.Close()
		return err
	}
	if err := writer.Close(); err != nil {
		return err
	}
	return client.Quit()
}

func smtpTLSConfig(config smtpConfig) *tls.Config {
	return &tls.Config{ServerName: config.host, InsecureSkipVerify: config.insecureSkipVerify} //nolint:gosec
}

func addressListField(dict runtime.Dict, key string) ([]string, error) {
	value, ok := dictField(dict, key)
	if !ok {
		return nil, nil
	}
	switch value := value.(type) {
	case runtime.String:
		return []string{value.Value}, nil
	case *runtime.List:
		out := make([]string, 0, len(value.Elements))
		for _, item := range value.Elements {
			text, ok := item.(runtime.String)
			if !ok {
				return nil, fmt.Errorf("message.%s entries must be strings", key)
			}
			out = append(out, text.Value)
		}
		return out, nil
	default:
		return nil, fmt.Errorf("message.%s must be string or list<string>", key)
	}
}

func validateAddressList(label string, addresses []string) error {
	for _, address := range addresses {
		if _, err := mail.ParseAddress(address); err != nil {
			return fmt.Errorf("invalid %s address %q: %w", label, address, err)
		}
	}
	return nil
}

func smtpHeaderMap(value runtime.Value) (map[string][]string, error) {
	dict, ok := value.(runtime.Dict)
	if !ok {
		return nil, fmt.Errorf("message.headers must be dict")
	}
	out := map[string][]string{}
	for _, __dk := range dict.EntryKeys() {
		entry, _ := dict.GetEntry(__dk)
		key, ok := entry.Key.(runtime.String)
		if !ok {
			return nil, fmt.Errorf("message.headers keys must be strings")
		}
		switch value := entry.Value.(type) {
		case runtime.String:
			out[key.Value] = []string{value.Value}
		case *runtime.List:
			values := make([]string, 0, len(value.Elements))
			for _, item := range value.Elements {
				text, ok := item.(runtime.String)
				if !ok {
					return nil, fmt.Errorf("message.headers list values must be strings")
				}
				values = append(values, text.Value)
			}
			out[key.Value] = values
		default:
			return nil, fmt.Errorf("message.headers values must be strings or list<string>")
		}
	}
	return out, nil
}

func smtpAttachmentsFromValue(value runtime.Value) ([]smtpAttachment, error) {
	list, ok := value.(*runtime.List)
	if !ok {
		return nil, fmt.Errorf("message.attachments must be list<dict>")
	}
	out := make([]smtpAttachment, 0, len(list.Elements))
	for _, item := range list.Elements {
		dict, ok := item.(runtime.Dict)
		if !ok {
			return nil, fmt.Errorf("message.attachments entries must be dict")
		}
		attachment, err := smtpAttachmentFromDict(dict)
		if err != nil {
			return nil, err
		}
		out = append(out, attachment)
	}
	return out, nil
}

func smtpAttachmentFromDict(dict runtime.Dict) (smtpAttachment, error) {
	var attachment smtpAttachment
	attachment.filename, _ = dictStringField(dict, "filename")
	attachment.contentType, _ = dictStringField(dict, "contentType")
	attachment.inline = dictBoolFieldDefault(dict, "inline", false)
	attachment.contentID, _ = dictStringField(dict, "contentId")
	if attachment.contentID == "" {
		attachment.contentID, _ = dictStringField(dict, "contentID")
	}
	if path, ok := dictStringField(dict, "path"); ok && path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			return smtpAttachment{}, err
		}
		attachment.content = data
		if attachment.filename == "" {
			attachment.filename = filepath.Base(path)
		}
	} else if content, ok := dictField(dict, "content"); ok {
		switch content := content.(type) {
		case runtime.Bytes:
			attachment.content = append([]byte(nil), content.Value...)
		case runtime.String:
			attachment.content = []byte(content.Value)
		default:
			return smtpAttachment{}, fmt.Errorf("attachment.content must be bytes or string")
		}
	} else {
		return smtpAttachment{}, fmt.Errorf("attachment requires path or content")
	}
	if attachment.filename == "" {
		return smtpAttachment{}, fmt.Errorf("attachment.filename is required when content is used directly")
	}
	return attachment, nil
}

func intField(value runtime.Value, label string) (int, error) {
	if _, ok := native.IntValueToBigInt(value); !ok {
		return 0, fmt.Errorf("%s must be int", label)
	}
	n, ok := native.AsInt64(value)
	if !ok {
		return 0, fmt.Errorf("%s is out of range", label)
	}
	return int(n), nil
}

func dictBoolFieldDefault(dict runtime.Dict, key string, fallback bool) bool {
	value, ok := dictField(dict, key)
	if !ok {
		return fallback
	}
	boolValue, ok := value.(runtime.Bool)
	if !ok {
		return fallback
	}
	return boolValue.Value
}

func reservedSMTPHeader(key string) bool {
	switch strings.ToLower(key) {
	case "from", "to", "cc", "bcc", "reply-to", "subject", "date", "mime-version", "content-type", "content-transfer-encoding":
		return true
	default:
		return false
	}
}

func sanitizeHeaderValue(value string) string {
	value = strings.ReplaceAll(value, "\r", " ")
	value = strings.ReplaceAll(value, "\n", " ")
	return strings.TrimSpace(value)
}

func writeHeaders(buf *bytes.Buffer, headers textproto.MIMEHeader) {
	keys := make([]string, 0, len(headers))
	for key := range headers {
		keys = append(keys, key)
	}
	for _, key := range keys {
		for _, value := range headers[key] {
			buf.WriteString(key)
			buf.WriteString(": ")
			buf.WriteString(value)
			buf.WriteString("\r\n")
		}
	}
	buf.WriteString("\r\n")
}

func quoteMIMEParam(value string) string {
	return strconv.Quote(sanitizeHeaderValue(value))
}

type base64LineWriter struct {
	w    io.Writer
	line int
}

func newBase64LineWriter(w io.Writer) *base64LineWriter {
	return &base64LineWriter{w: w}
}

func (w *base64LineWriter) Write(data []byte) (int, error) {
	written := 0
	for _, b := range data {
		if w.line == 76 {
			if _, err := w.w.Write([]byte("\r\n")); err != nil {
				return written, err
			}
			w.line = 0
		}
		if _, err := w.w.Write([]byte{b}); err != nil {
			return written, err
		}
		w.line++
		written++
	}
	return written, nil
}

type quotedPrintableWriter struct {
	*quotedprintable.Writer
}

func (w quotedPrintableWriter) Write(data []byte) (int, error) {
	return w.Writer.Write(data)
}

func (w quotedPrintableWriter) Close() error {
	return w.Writer.Close()
}
