# Mailer And SMTP

Geblang includes two layers for email:

- `mailer`: source-level classes for application code.
- `smtp`: low-level native functions for MIME rendering and SMTP delivery.

Most applications should import `mailer`. It gives you typed message,
attachment, transport, and sender objects while still producing plain MIME and
using SMTP underneath.

## Quick Start

```gb
import io;
import mailer;

let message = mailer.Message("Welcome")
    .fromAddress("App <noreply@example.com>")
    .toAddress("Ada <ada@example.com>")
    .withText("Hello Ada,\n\nYour account is ready.")
    .withHtml("<h1>Hello Ada</h1><p>Your account is ready.</p>");

let transport = mailer.smtpTransport("smtp.example.com", 587, "user", "secret")
    .fromAddress("App <noreply@example.com>");

let client = mailer.Mailer(transport);
client.send(message);
```

`fromAddress` may be set on the message or the transport. The message-level
sender wins; the transport sender is useful when most mail from an application
uses the same address.

## Messages

`mailer.Message(subject)` starts an email message. It supports fluent mutation:

```gb
let message = mailer.Message("Invoice")
    .fromAddress("Billing <billing@example.com>")
    .toAddress("customer@example.com")
    .ccAddress("accounts@example.com")
    .bccAddress("audit@example.com")
    .withReplyTo("support@example.com")
    .withHeader("X-Customer-Id", "cust_123")
    .withText("Your invoice is attached.")
    .withHtml("<p>Your invoice is attached.</p>");
```

Methods:

| Method | Description |
|--------|-------------|
| `fromAddress(address)` | Sets the sender. |
| `toAddress(address)` | Adds a `To` recipient. |
| `ccAddress(address)` | Adds a `Cc` recipient. |
| `bccAddress(address)` | Adds a blind-copy recipient used for SMTP delivery but omitted from rendered headers. |
| `withReplyTo(address)` | Sets `Reply-To`. |
| `withText(text)` | Sets the plain text body. |
| `withHtml(html)` | Sets the HTML body. |
| `withHeader(name, value)` | Adds a custom header. Reserved MIME and address headers are controlled by the message object. |
| `attach(attachment)` | Adds an attachment. |
| `render()` | Returns the MIME message as a string without sending. |
| `toDict()` | Returns the low-level dictionary shape consumed by `smtp`. |

When both text and HTML are present, Geblang renders a
`multipart/alternative` body. When attachments are present, the message becomes
`multipart/mixed` with the text/HTML body as the first part.

## Attachments

Use `mailer.Attachment(filename, content)` for in-memory content. `content` may
be a `string` or `bytes`.

```gb
import bytes;
import mailer;

let report = mailer.Attachment("report.txt", "daily summary")
    .withContentType("text/plain");

let image = mailer.Attachment("logo.png", bytes.fromBase64("..."))
    .withContentType("image/png")
    .asInline("logo");
```

Use `attachmentFromPath(path, contentType)` when the content should be read from
disk as the message is rendered or sent:

```gb
let pdf = mailer.attachmentFromPath("build/invoice.pdf", "application/pdf");
message.attach(pdf);
```

Inline attachments set `Content-Disposition: inline` and a `Content-ID` header.
Reference them from HTML with `cid:<id>`:

```gb
let message = mailer.Message("Logo")
    .withHtml("<img src=\"cid:logo\">")
    .attach(mailer.Attachment("logo.png", imageBytes).withContentType("image/png").asInline("logo"));
```

## SMTP Transport

`mailer.SmtpTransport(host, port, username, password)` (class constructor) and
`mailer.smtpTransport(host, port, username, password)` (factory function) are
interchangeable - both return a `SmtpTransport`.

For a one-call setup (transport + mailer in one), use
`mailer.smtpMailer(host, port, username, password)`, which returns a ready-to-use
`Mailer` and is equivalent to `mailer.Mailer(mailer.smtpTransport(...))`.

`mailer.SmtpTransport(host, port, username, password)` configures SMTP.

```gb
let transport = mailer.SmtpTransport("smtp.example.com", 587, "user", "secret")
    .fromAddress("App <noreply@example.com>")
    .withStartTLS(true)
    .withTLS(false)
    .withHello("app.example.com");
```

Transport methods:

| Method | Description |
|--------|-------------|
| `fromAddress(address)` | Default sender when the message has no sender. |
| `withStartTLS(enabled)` | Enables STARTTLS upgrade when the server advertises it. Enabled by default. |
| `withTLS(enabled)` | Uses implicit TLS for providers that expect TLS from connection start, commonly port `465`. |
| `withHello(name)` | Sends a custom SMTP HELO/EHLO hostname. |
| `allowInsecureTLS(enabled)` | Disables TLS certificate verification. Use only for local development or controlled test servers. |
| `toDict()` | Returns the low-level dictionary shape consumed by `smtp.send`. |

## Low-Level `smtp`

The native `smtp` module accepts dictionaries. This is useful for framework
code, tests, or adapter modules.

```gb
import smtp;

let message = {
    "from": "App <noreply@example.com>",
    "to": ["Ada <ada@example.com>"],
    "subject": "Welcome",
    "text": "Hello Ada",
    "html": "<p>Hello Ada</p>",
    "attachments": [
        {
            "filename": "welcome.txt",
            "contentType": "text/plain",
            "content": "Thanks for trying Geblang."
        }
    ]
};

let raw = smtp.message(message);
```

`smtp.send(config, message)` sends through an SMTP server and returns a dict:

```gb
let result = smtp.send({
    "host": "smtp.example.com",
    "port": 587,
    "username": "user",
    "password": "secret",
    "from": "App <noreply@example.com>",
    "startTLS": true
}, message);

io.println(result["ok"]);
io.println(result["recipients"]);
```

Message dictionary keys:

| Key | Type | Description |
|-----|------|-------------|
| `from` | `string` | Sender address. Optional if config has `from`. |
| `to` | `string` or `list<string>` | Primary recipients. |
| `cc` | `string` or `list<string>` | Carbon-copy recipients. |
| `bcc` | `string` or `list<string>` | Blind-copy recipients. Not rendered as a header. |
| `replyTo` | `string` | Reply-To address. |
| `subject` | `string` | Subject line. |
| `text` | `string` | Plain text body. |
| `html` | `string` | HTML body. |
| `headers` | `dict<string, string|list<string>>` | Custom non-reserved headers. |
| `attachments` | `list<dict>` | Attachment dictionaries. |

Attachment dictionary keys:

| Key | Type | Description |
|-----|------|-------------|
| `filename` | `string` | Filename shown to recipients. Required for direct content. |
| `content` | `string` or `bytes` | In-memory attachment content. |
| `path` | `string` | File path to read. If present, `filename` defaults to the file base name. |
| `contentType` | `string` | MIME type. Defaults to `application/octet-stream`. |
| `inline` | `bool` | Uses inline disposition instead of attachment. |
| `contentId` | `string` | Content ID for inline attachments. |

Config dictionary keys:

| Key | Type | Description |
|-----|------|-------------|
| `host` | `string` | SMTP server hostname. Required. |
| `port` | `int` | SMTP server port. Defaults to `587`. |
| `username` | `string` | SMTP username. Optional. |
| `password` | `string` | SMTP password. Optional. |
| `from` | `string` | Default sender. |
| `startTLS` | `bool` | Upgrade with STARTTLS when available. Defaults to `true`. |
| `tls` | `bool` | Use implicit TLS from connection start. Defaults to `false`. |
| `hello` | `string` | Custom HELO/EHLO hostname. |
| `insecureSkipVerify` | `bool` | Skip certificate verification. Defaults to `false`. |

## Testing Mail

For tests and previews, render instead of sending:

```gb
let raw = client.render(message);
assert(raw.contains("multipart/alternative"));
assert(raw.contains("Subject: Welcome"));
```

This avoids network access and makes assertions deterministic. A later
application framework can wrap `mailer.Mailer` behind an interface and provide
an in-memory test transport.

## Parsing received mail

`mailparse.parse(data, opts = {})` parses a complete RFC 5322/MIME message
from a string, bytes value, or binary stream. It is separate from SMTP
delivery. A caller-supplied stream remains open. Parsing does not sanitize
HTML, fetch remote content, or write attachments.

`Message` has `headers()` (lowercase names with repeated values in arrival
order), case-insensitive `header(name)` and `headerAll(name)`, `subject()`,
`from()`, `to()`, `cc()`, `date()`, `textBody()`, `htmlBody()`, and
`attachments()`. Missing bodies and dates return `null`. An attachment has
`filename`, `contentType`, `disposition`, `contentId`, and exact `data` bytes.
Display names and quoted commas are parsed as mailboxes.

```gb
import io;
import mailer;
import mailparse;

let outgoing = mailer.Message("Report")
    .fromAddress("sender@example.com")
    .toAddress("reader@example.com")
    .withText("ready");
let incoming = mailparse.parse(outgoing.render());
io.println(incoming.subject());
io.println(incoming.textBody());
```

The default limits are 10 MiB per message, 100 MIME parts, 64 KiB of headers,
and 5 MiB per attachment. Override them with positive integer keys
`maxMessageBytes`, `maxParts`, `maxHeaderBytes`, and `maxAttachmentBytes`.
Unknown keys and invalid values raise errors. Headers, encoded words,
base64, quoted-printable, and supported character sets are decoded. Parsing
rejects malformed input and unsupported character sets with a part location.
