package native

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"net/textproto"
	"strings"

	"golang.org/x/net/html/charset"

	"geblang/internal/runtime"
)

type mailLimits struct {
	message    int64
	parts      int
	header     int64
	attachment int64
}

type mailState struct {
	limits      mailLimits
	parts       int
	text        runtime.Value
	html        runtime.Value
	attachments []runtime.Value
}

func registerMailparse(r *Registry) {
	r.Register("mailparse_native", "parse", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 5 {
			return nil, fmt.Errorf("mailparse_native.parse expects data and four limits")
		}
		raw, ok := args[0].(runtime.Bytes)
		if !ok {
			return nil, fmt.Errorf("mailparse_native.parse data must be bytes")
		}
		values := [4]int64{}
		for i := 1; i < len(args); i++ {
			n, ok := AsInt64(args[i])
			if !ok || n <= 0 {
				return nil, fmt.Errorf("mailparse_native.parse limits must be positive integers")
			}
			values[i-1] = n
		}
		limits := mailLimits{message: values[0], parts: int(values[1]), header: values[2], attachment: values[3]}
		if int64(len(raw.Value)) > limits.message {
			return nil, fmt.Errorf("mailparse: message exceeds maxMessageBytes")
		}
		headerEnd := mailHeaderEnd(raw.Value)
		if headerEnd < 0 {
			return nil, fmt.Errorf("mailparse: missing message header separator")
		}
		if int64(headerEnd) > limits.header {
			return nil, fmt.Errorf("mailparse: headers exceed maxHeaderBytes")
		}
		message, err := mail.ReadMessage(bytes.NewReader(raw.Value))
		if err != nil {
			return nil, fmt.Errorf("mailparse: message: %w", err)
		}
		state := &mailState{limits: limits, text: runtime.Null{}, html: runtime.Null{}}
		if err := parseMailEntity(textproto.MIMEHeader(message.Header), message.Body, state, "message"); err != nil {
			return nil, err
		}
		headers := make(map[string]runtime.Value, len(message.Header))
		for name, values := range message.Header {
			items := make([]runtime.Value, len(values))
			for i, value := range values {
				items[i] = runtime.String{Value: value}
			}
			headers[strings.ToLower(name)] = &runtime.List{Elements: items}
		}
		subject, err := decodeMailWords(message.Header.Get("Subject"))
		if err != nil {
			return nil, fmt.Errorf("mailparse: subject: %w", err)
		}
		from, err := mailAddresses(message.Header.Get("From"))
		if err != nil {
			return nil, fmt.Errorf("mailparse: from: %w", err)
		}
		to, err := mailAddresses(message.Header.Get("To"))
		if err != nil {
			return nil, fmt.Errorf("mailparse: to: %w", err)
		}
		cc, err := mailAddresses(message.Header.Get("Cc"))
		if err != nil {
			return nil, fmt.Errorf("mailparse: cc: %w", err)
		}
		var date runtime.Value = runtime.Null{}
		if value := message.Header.Get("Date"); value != "" {
			parsed, err := mail.ParseDate(value)
			if err != nil {
				return nil, fmt.Errorf("mailparse: date: %w", err)
			}
			date = runtime.NewInt64(parsed.Unix())
		}
		return mailDict(map[string]runtime.Value{
			"headers": mailDict(headers),
			"subject": runtime.String{Value: subject},
			"from":    from, "to": to, "cc": cc, "date": date,
			"text": state.text, "html": state.html,
			"attachments": &runtime.List{Elements: state.attachments},
		}), nil
	})
}

func mailHeaderEnd(raw []byte) int {
	if index := bytes.Index(raw, []byte("\r\n\r\n")); index >= 0 {
		return index + 4
	}
	if index := bytes.Index(raw, []byte("\n\n")); index >= 0 {
		return index + 2
	}
	return -1
}

func decodeMailWords(value string) (string, error) {
	return (&mime.WordDecoder{CharsetReader: charset.NewReaderLabel}).DecodeHeader(value)
}

func mailAddresses(value string) (*runtime.List, error) {
	out := &runtime.List{Elements: []runtime.Value{}}
	if value == "" {
		return out, nil
	}
	parsed, err := mail.ParseAddressList(value)
	if err != nil {
		return nil, err
	}
	for _, address := range parsed {
		value := address.Address
		if address.Name != "" {
			value = address.String()
		}
		out.Elements = append(out.Elements, runtime.String{Value: value})
	}
	return out, nil
}

func parseMailEntity(headers textproto.MIMEHeader, body io.Reader, state *mailState, label string) error {
	contentType := headers.Get("Content-Type")
	if contentType == "" {
		contentType = "text/plain"
	}
	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		return fmt.Errorf("mailparse: %s content type: %w", label, err)
	}
	if strings.HasPrefix(mediaType, "multipart/") {
		boundary := params["boundary"]
		if boundary == "" {
			return fmt.Errorf("mailparse: %s missing multipart boundary", label)
		}
		reader := multipart.NewReader(body, boundary)
		for {
			part, err := reader.NextRawPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				return fmt.Errorf("mailparse: %s multipart: %w", label, err)
			}
			state.parts++
			partLabel := fmt.Sprintf("%s part %d", label, state.parts)
			if state.parts > state.limits.parts {
				_ = part.Close()
				return fmt.Errorf("mailparse: %s exceeds maxParts", partLabel)
			}
			if mailHeaderSize(part.Header) > state.limits.header {
				_ = part.Close()
				return fmt.Errorf("mailparse: %s headers exceed maxHeaderBytes", partLabel)
			}
			if err := parseMailEntity(part.Header, part, state, partLabel); err != nil {
				_ = part.Close()
				return err
			}
			_ = part.Close()
		}
		return nil
	}
	disposition := "inline"
	filename := ""
	if raw := headers.Get("Content-Disposition"); raw != "" {
		kind, values, err := mime.ParseMediaType(raw)
		if err != nil {
			return fmt.Errorf("mailparse: %s disposition: %w", label, err)
		}
		disposition = strings.ToLower(kind)
		filename = values["filename"]
	}
	if filename == "" {
		filename = params["name"]
	}
	contentID := strings.Trim(headers.Get("Content-Id"), "<>")
	isAttachment := disposition == "attachment" || filename != "" || contentID != ""
	limit := state.limits.message
	if isAttachment && state.limits.attachment < limit {
		limit = state.limits.attachment
	}
	reader, err := mailTransferReader(body, headers.Get("Content-Transfer-Encoding"), label, state.limits.message)
	if err != nil {
		return err
	}
	data, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return fmt.Errorf("mailparse: %s transfer encoding: %w", label, err)
	}
	if int64(len(data)) > limit {
		if isAttachment {
			return fmt.Errorf("mailparse: %s exceeds maxAttachmentBytes", label)
		}
		return fmt.Errorf("mailparse: %s exceeds maxMessageBytes", label)
	}
	if isAttachment {
		state.attachments = append(state.attachments, mailDict(map[string]runtime.Value{
			"filename":    runtime.String{Value: filename},
			"contentType": runtime.String{Value: mediaType},
			"disposition": runtime.String{Value: disposition},
			"contentId":   runtime.String{Value: contentID},
			"data":        runtime.Bytes{Value: data},
		}))
		return nil
	}
	if mediaType != "text/plain" && mediaType != "text/html" {
		return nil
	}
	if len(data) == 0 && label == "message" && headers.Get("Content-Type") == "" {
		return nil
	}
	encoding := params["charset"]
	if encoding == "" {
		encoding = "us-ascii"
	}
	decoded, err := charset.NewReaderLabel(encoding, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("mailparse: %s charset %s: %w", label, encoding, err)
	}
	text, err := io.ReadAll(io.LimitReader(decoded, state.limits.message+1))
	if err != nil {
		return fmt.Errorf("mailparse: %s charset %s: %w", label, encoding, err)
	}
	if int64(len(text)) > state.limits.message {
		return fmt.Errorf("mailparse: %s decoded text exceeds maxMessageBytes", label)
	}
	if mediaType == "text/plain" {
		if _, empty := state.text.(runtime.Null); empty {
			state.text = runtime.String{Value: string(text)}
		}
	} else if _, empty := state.html.(runtime.Null); empty {
		state.html = runtime.String{Value: string(text)}
	}
	return nil
}

func mailTransferReader(body io.Reader, encoding, label string, maxBytes int64) (io.Reader, error) {
	switch strings.ToLower(strings.TrimSpace(encoding)) {
	case "", "7bit", "8bit", "binary":
		return body, nil
	case "base64":
		return base64.NewDecoder(base64.StdEncoding.Strict(), body), nil
	case "quoted-printable":
		raw, err := io.ReadAll(io.LimitReader(body, maxBytes+1))
		if err != nil {
			return nil, fmt.Errorf("mailparse: %s transfer encoding: %w", label, err)
		}
		if int64(len(raw)) > maxBytes {
			return nil, fmt.Errorf("mailparse: %s exceeds maxMessageBytes", label)
		}
		if err := validateMailQuotedPrintable(raw); err != nil {
			return nil, fmt.Errorf("mailparse: %s transfer encoding: %w", label, err)
		}
		return quotedprintable.NewReader(bytes.NewReader(raw)), nil
	default:
		return nil, fmt.Errorf("mailparse: %s unsupported transfer encoding %s", label, encoding)
	}
}

func validateMailQuotedPrintable(data []byte) error {
	isHex := func(value byte) bool {
		return value >= '0' && value <= '9' || value >= 'a' && value <= 'f' || value >= 'A' && value <= 'F'
	}
	for i := 0; i < len(data); i++ {
		if data[i] != '=' {
			continue
		}
		if i+1 < len(data) && data[i+1] == '\n' {
			i++
			continue
		}
		if i+2 < len(data) && data[i+1] == '\r' && data[i+2] == '\n' {
			i += 2
			continue
		}
		if i+2 >= len(data) || !isHex(data[i+1]) || !isHex(data[i+2]) {
			return fmt.Errorf("invalid quoted-printable escape at byte %d", i)
		}
		i += 2
	}
	return nil
}

func mailHeaderSize(headers textproto.MIMEHeader) int64 {
	var total int64
	for name, values := range headers {
		for _, value := range values {
			total += int64(len(name) + len(value) + 4)
		}
	}
	return total
}

func mailDict(values map[string]runtime.Value) runtime.Dict {
	entries := make(map[string]runtime.DictEntry, len(values))
	for key, value := range values {
		name := runtime.String{Value: key}
		entries[DictKey(name)] = runtime.DictEntry{Key: name, Value: value}
	}
	return runtime.Dict{Entries: entries}
}
