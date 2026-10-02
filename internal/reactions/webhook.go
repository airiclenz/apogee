package reactions

import (
	"context"
	"errors"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/security"
	"github.com/airiclenz/apogee/internal/webhook"
)

// webhookSender POSTs the payload to an entry's `run: url:` webhook through internal/webhook — the
// one request both lanes send — and reads NOTHING of the reply. Its one field is the url-safety
// guard every post goes through; the client is built per send by the shared package because its
// Timeout is the ENTRY's. The zero value posts under the zero URLGuard: no host lists.
//
// It is safe for concurrent use, which the Executor contract requires: the guard is a value.
type webhookSender struct {
	guard security.URLGuard
}

// Run POSTs the payload as JSON and reports anything that was not a 2xx — the retry, header and
// failure-wording decisions are the shared package's (webhook.Post). What this half adds is the
// observe lane's own rule about the reply: its content is never looked at, because nothing a
// Reaction answers may reach the model, the conversation or the Session record (ADR 0073 §1), so
// the body is drained up to webhook.MaxResponseDrain — so the endpoint finishes its answer — and
// then hung up on.
func (s webhookSender) Run(ctx context.Context, r domain.Reaction, p domain.SeamPayload) error {
	handler, ok := r.Handler.(domain.WebhookHandler)
	if !ok {
		return errors.New("no webhook to POST to")
	}
	body, err := encodePayload(p)
	if err != nil {
		return err
	}
	response, err := webhook.Post(ctx, s.guard, handler, r.Timeout, body)
	if err != nil {
		return err
	}
	webhook.Drain(response)
	return nil
}
