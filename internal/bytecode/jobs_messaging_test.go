package bytecode_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestParityManagedSQSRetainsFailedDeliveryUntilSuccessfulDispatch(t *testing.T) {
	var mu sync.Mutex
	pending := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.Header.Get("Authorization"), "AWS4-HMAC-SHA256 ") {
			http.Error(w, "signature required", http.StatusForbidden)
			return
		}
		if err := r.ParseForm(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		mu.Lock()
		defer mu.Unlock()
		switch r.Form.Get("Action") {
		case "SendMessage":
			pending = true
			fmt.Fprint(w, "<SendMessageResponse/>")
		case "ReceiveMessage":
			if pending {
				fmt.Fprint(w, "<ReceiveMessageResponse><Message><Body>delivery</Body><MessageId>id</MessageId><ReceiptHandle>receipt</ReceiptHandle></Message></ReceiveMessageResponse>")
			} else {
				fmt.Fprint(w, "<ReceiveMessageResponse/>")
			}
		case "DeleteMessage":
			if r.Form.Get("ReceiptHandle") != "receipt" {
				http.Error(w, "unknown receipt", http.StatusBadRequest)
				return
			}
			pending = false
			fmt.Fprint(w, "<DeleteMessageResponse/>")
		default:
			http.Error(w, "unknown action", http.StatusBadRequest)
		}
	}))
	defer server.Close()
	source := fmt.Sprintf(`import messaging;
import io;
import store;
let queue = messaging.connect({"driver": "sqs", "queueUrl": %q, "region": "local", "accessKey": "test", "secretKey": "test"});
queue.publish("delivery");
try {
    (queue as messaging.ManagedMessageQueue).consumeUntil(func(any msg): void { throw RuntimeError("dispatch failed"); }, func(): bool { return false; });
} catch (Error e) { io.println(e.message); }
let state = store.Store();
(queue as messaging.ManagedMessageQueue).consumeUntil(func(any msg): void {
    io.println(msg["body"]);
    state.set("stop", true);
}, func(): bool { return state.get("stop") == true; });
io.println(queue.receive(0) == null);
`, server.URL+"/queue")

	runParityWithStdlib(t, source, "dispatch failed\ndelivery\ntrue\n")
}
